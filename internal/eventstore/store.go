package eventstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Store is the high-throughput event store abstraction supporting PostgreSQL and ClickHouse.
type Store interface {
	Driver() string
	EnsureSchema(ctx context.Context) error
	Publish(ev domain.Event)
	AppendBatch(ctx context.Context, events []domain.Event) error
	Query(ctx context.Context, filter domain.EventFilter) ([]domain.Event, int, error)
	QueryAgentMetrics(ctx context.Context, agentID uuid.UUID, since time.Time, limit int) ([]domain.AgentMetricPoint, error)
	GetSummary(ctx context.Context, since time.Time) (*domain.EventSummary, error)
	Stats() domain.EventIngestionStats
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
}

// Backend defines the low-level storage implementation for a specific engine.
type Backend interface {
	Driver() string
	EnsureSchema(ctx context.Context) error
	InsertBatch(ctx context.Context, events []domain.Event) error
	Query(ctx context.Context, filter domain.EventFilter) ([]domain.Event, int, error)
	QueryAgentMetrics(ctx context.Context, agentID uuid.UUID, since time.Time, limit int) ([]domain.AgentMetricPoint, error)
	GetSummary(ctx context.Context, since time.Time) (*domain.EventSummary, error)
	PruneExpired(ctx context.Context, retentionDays int) error
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
}

// New constructs and initializes the configured EventStore backend.
func New(ctx context.Context, cfg config.EventStoreConfig, mainDB *gorm.DB, log *zap.Logger) (Store, error) {
	if log == nil {
		log = zap.NewNop()
	}
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = "postgres"
	}

	var backend Backend
	switch driver {
	case "disabled":
		return &NoopStore{}, nil
	case "postgres":
		var sqlDB *sql.DB
		var owns bool
		if strings.TrimSpace(cfg.PostgresURL) == "" && mainDB != nil {
			unwrapped, err := mainDB.DB()
			if err != nil {
				return nil, fmt.Errorf("unwrap postgres db for eventstore: %w", err)
			}
			sqlDB = unwrapped
		} else {
			dsn := strings.TrimSpace(cfg.PostgresURL)
			if dsn == "" {
				return nil, fmt.Errorf("eventstore postgres_url or main database connection required")
			}
			opened, err := openPostgresDB(ctx, dsn)
			if err != nil {
				return nil, err
			}
			sqlDB = opened
			owns = true
		}
		backend = NewPostgresBackend(sqlDB, owns)
	case "clickhouse":
		ch, err := NewClickHouseBackend(cfg)
		if err != nil {
			return nil, err
		}
		backend = ch
	default:
		return nil, fmt.Errorf("unsupported eventstore driver %q", driver)
	}

	return NewBufferedStore(backend, cfg, log), nil
}

// BufferedStore batches events in memory for high-throughput, low-latency ingestion.
type BufferedStore struct {
	backend       Backend
	log           *zap.Logger
	batchSize     int
	flushInterval time.Duration
	retentionDays int

	ch     chan domain.Event
	stopCh chan struct{}
	wg     sync.WaitGroup

	published   atomic.Int64
	flushed     atomic.Int64
	dropped     atomic.Int64
	batchErrors atomic.Int64
	closeOnce   sync.Once
	closed      atomic.Bool
}

// NewBufferedStore wraps a storage backend with bounded async batching, retries, and retention pruning.
func NewBufferedStore(backend Backend, cfg config.EventStoreConfig, log *zap.Logger) *BufferedStore {
	if log == nil {
		log = zap.NewNop()
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 256
	}
	flushInterval := cfg.FlushInterval
	if flushInterval <= 0 {
		flushInterval = 500 * time.Millisecond
	}
	bufferSize := cfg.BufferSize
	if bufferSize <= 0 {
		bufferSize = 8192
	}
	retentionDays := cfg.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 90
	}

	s := &BufferedStore{
		backend:       backend,
		log:           log.Named("eventstore"),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		retentionDays: retentionDays,
		ch:            make(chan domain.Event, bufferSize),
		stopCh:        make(chan struct{}),
	}
	s.wg.Add(1)
	go s.flushLoop()
	return s
}

func (s *BufferedStore) Driver() string { return s.backend.Driver() }

func (s *BufferedStore) EnsureSchema(ctx context.Context) error {
	return s.backend.EnsureSchema(ctx)
}

func (s *BufferedStore) Publish(ev domain.Event) {
	if s.closed.Load() {
		s.dropped.Add(1)
		return
	}
	ev = normalizeEvent(ev)
	s.published.Add(1)
	select {
	case s.ch <- ev:
	default:
		s.dropped.Add(1)
	}
}

func (s *BufferedStore) AppendBatch(ctx context.Context, events []domain.Event) error {
	if len(events) == 0 {
		return nil
	}
	normalized := make([]domain.Event, len(events))
	for i := range events {
		normalized[i] = normalizeEvent(events[i])
	}
	s.published.Add(int64(len(normalized)))
	if err := s.backend.InsertBatch(ctx, normalized); err != nil {
		s.batchErrors.Add(1)
		return err
	}
	s.flushed.Add(int64(len(normalized)))
	return nil
}

func (s *BufferedStore) Query(ctx context.Context, filter domain.EventFilter) ([]domain.Event, int, error) {
	return s.backend.Query(ctx, filter)
}

func (s *BufferedStore) QueryAgentMetrics(ctx context.Context, agentID uuid.UUID, since time.Time, limit int) ([]domain.AgentMetricPoint, error) {
	return s.backend.QueryAgentMetrics(ctx, agentID, since, limit)
}

func (s *BufferedStore) GetSummary(ctx context.Context, since time.Time) (*domain.EventSummary, error) {
	sum, err := s.backend.GetSummary(ctx, since)
	if err != nil {
		return nil, err
	}
	sum.Ingestion = s.Stats()
	return sum, nil
}

func (s *BufferedStore) Stats() domain.EventIngestionStats {
	return domain.EventIngestionStats{
		Published:      s.published.Load(),
		Flushed:        s.flushed.Load(),
		Dropped:        s.dropped.Load(),
		BatchErrors:    s.batchErrors.Load(),
		QueueDepth:     len(s.ch),
		BufferCapacity: cap(s.ch),
	}
}

func (s *BufferedStore) Ping(ctx context.Context) error {
	return s.backend.Ping(ctx)
}

func (s *BufferedStore) Close(ctx context.Context) error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		close(s.stopCh)
	})

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}
	return s.backend.Close(ctx)
}

func (s *BufferedStore) flushLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	pruneTicker := time.NewTicker(6 * time.Hour)
	defer pruneTicker.Stop()

	batch := make([]domain.Event, 0, s.batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		s.writeWithRetry(batch)
		batch = batch[:0]
	}

	for {
		select {
		case <-s.stopCh:
			for {
				select {
				case ev := <-s.ch:
					batch = append(batch, ev)
					if len(batch) >= s.batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case ev := <-s.ch:
			batch = append(batch, ev)
			if len(batch) >= s.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-pruneTicker.C:
			pruneCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := s.backend.PruneExpired(pruneCtx, s.retentionDays); err != nil {
				s.log.Warn("eventstore retention prune failed", zap.Error(err))
			}
			cancel()
		}
	}
}

func (s *BufferedStore) writeWithRetry(batch []domain.Event) {
	backoffs := []time.Duration{0, 100 * time.Millisecond, 300 * time.Millisecond, 900 * time.Millisecond}
	var lastErr error
	for attempt, wait := range backoffs {
		if wait > 0 {
			time.Sleep(wait)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := s.backend.InsertBatch(ctx, batch)
		cancel()
		if err == nil {
			s.flushed.Add(int64(len(batch)))
			return
		}
		lastErr = err
		if attempt == len(backoffs)-1 {
			break
		}
	}
	s.batchErrors.Add(1)
	s.dropped.Add(int64(len(batch)))
	s.log.Warn("eventstore batch flush failed after retries", zap.Int("count", len(batch)), zap.Error(lastErr))
}

func normalizeEvent(ev domain.Event) domain.Event {
	if ev.ID == uuid.Nil {
		if v7, err := uuid.NewV7(); err == nil {
			ev.ID = v7
		} else {
			ev.ID = uuid.New()
		}
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	} else {
		ev.OccurredAt = ev.OccurredAt.UTC()
	}
	if strings.TrimSpace(ev.Category) == "" {
		ev.Category = domain.EventCategorySystem
	}
	if strings.TrimSpace(ev.EventType) == "" {
		ev.EventType = "system.event"
	}
	if strings.TrimSpace(ev.Severity) == "" {
		ev.Severity = "info"
	}
	if ev.SchemaVersion <= 0 {
		ev.SchemaVersion = 1
	}
	if len(ev.Payload) == 0 {
		ev.Payload = json.RawMessage(`{}`)
	}
	return ev
}

func reverseMetricPoints(points []domain.AgentMetricPoint) {
	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}
}

func clampLimitOffset(p domain.PageParams, def, max int) (int, int) {
	limit := p.Limit
	if limit <= 0 {
		limit = def
	}
	if limit > max {
		limit = max
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// NoopStore is used when eventstore.driver is set to "disabled".
type NoopStore struct{}

func (*NoopStore) Driver() string                       { return "disabled" }
func (*NoopStore) EnsureSchema(context.Context) error   { return nil }
func (*NoopStore) Publish(domain.Event)                 {}
func (*NoopStore) AppendBatch(context.Context, []domain.Event) error { return nil }
func (*NoopStore) Query(context.Context, domain.EventFilter) ([]domain.Event, int, error) {
	return []domain.Event{}, 0, nil
}
func (*NoopStore) QueryAgentMetrics(context.Context, uuid.UUID, time.Time, int) ([]domain.AgentMetricPoint, error) {
	return []domain.AgentMetricPoint{}, nil
}
func (*NoopStore) GetSummary(context.Context, time.Time) (*domain.EventSummary, error) {
	return &domain.EventSummary{
		Driver:     "disabled",
		ByCategory: map[string]int64{},
		BySeverity: map[string]int64{},
	}, nil
}
func (*NoopStore) Stats() domain.EventIngestionStats { return domain.EventIngestionStats{} }
func (*NoopStore) Ping(context.Context) error        { return nil }
func (*NoopStore) Close(context.Context) error       { return nil }
