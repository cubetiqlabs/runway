package eventstore

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type memBackend struct {
	mu      sync.Mutex
	batches [][]domain.Event
	events  []domain.Event
	failN   int
}

func (m *memBackend) Driver() string                     { return "memory" }
func (m *memBackend) EnsureSchema(context.Context) error { return nil }
func (m *memBackend) InsertBatch(_ context.Context, events []domain.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failN > 0 {
		m.failN--
		return context.DeadlineExceeded
	}
	cp := append([]domain.Event(nil), events...)
	m.batches = append(m.batches, cp)
	m.events = append(m.events, cp...)
	return nil
}
func (m *memBackend) Query(_ context.Context, _ domain.EventFilter) ([]domain.Event, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.Event(nil), m.events...), len(m.events), nil
}
func (m *memBackend) QueryAgentMetrics(_ context.Context, _ uuid.UUID, _ time.Time, _ int) ([]domain.AgentMetricPoint, error) {
	return nil, nil
}
func (m *memBackend) GetSummary(_ context.Context, _ time.Time) (*domain.EventSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &domain.EventSummary{
		Driver:      "memory",
		TotalEvents: int64(len(m.events)),
		ByCategory:  map[string]int64{},
		BySeverity:  map[string]int64{},
	}, nil
}
func (m *memBackend) PruneExpired(context.Context, int) error { return nil }
func (m *memBackend) Ping(context.Context) error              { return nil }
func (m *memBackend) Close(context.Context) error             { return nil }

func TestBufferedStoreBatchingRetryAndDrain(t *testing.T) {
	backend := &memBackend{failN: 1}
	store := NewBufferedStore(backend, config.EventStoreConfig{
		BatchSize:     3,
		FlushInterval: 40 * time.Millisecond,
		BufferSize:    32,
		RetentionDays: 30,
	}, zap.NewNop())

	for i := 0; i < 5; i++ {
		store.Publish(domain.Event{
			Category:  domain.EventCategoryAudit,
			EventType: "test.event",
			Message:   "hello",
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := store.Close(ctx); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	stats := store.Stats()
	if stats.Published != 5 || stats.Flushed != 5 || stats.Dropped != 0 {
		t.Fatalf("unexpected stats after retry & drain: %+v", stats)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.events) != 5 {
		t.Fatalf("expected 5 flushed events, got %d", len(backend.events))
	}
	for _, ev := range backend.events {
		if ev.ID == uuid.Nil || ev.SchemaVersion != 1 || ev.OccurredAt.IsZero() {
			t.Fatalf("expected normalized event metadata, got %+v", ev)
		}
	}
}

func TestClickHouseBackendSchemaInsertAndQuery(t *testing.T) {
	var queries []string
	var insertedLines []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ping":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Ok.\n"))
			return
		case "/":
			if q := r.URL.Query().Get("query"); strings.HasPrefix(q, "INSERT INTO runway.events FORMAT JSONEachRow") {
				var reader io.Reader = r.Body
				if r.Header.Get("Content-Encoding") == "gzip" {
					gz, err := gzip.NewReader(r.Body)
					if err != nil {
						t.Fatalf("gzip reader: %v", err)
					}
					defer gz.Close()
					reader = gz
				}
				raw, _ := io.ReadAll(reader)
				for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
					if line != "" {
						insertedLines = append(insertedLines, line)
					}
				}
				w.WriteHeader(http.StatusOK)
				return
			}
			body, _ := io.ReadAll(r.Body)
			sqlText := string(body)
			queries = append(queries, sqlText)

			w.Header().Set("Content-Type", "application/json")
			switch {
			case strings.Contains(sqlText, "SELECT count()"):
				_, _ = w.Write([]byte(`{"data":[{"cnt":"1"}]}`))
			case strings.Contains(sqlText, "FROM runway.events") && strings.Contains(sqlText, "LIMIT"):
				_, _ = w.Write([]byte(`{"data":[{"id":"` + uuid.NewString() + `","occurred_at":"2026-11-28T00:00:00.000000Z","category":"webhook","event_type":"webhook.processed","severity":"info","source":"webhook","actor_type":"","actor_id":"","actor_label":"github","resource_type":"delivery","resource_id":"del-1","correlation_id":"del-1","repo_full_name":"cubetiqlabs/runway","duration_ms":12,"status":"processed","message":"ok","payload":"{}","ip_address":"","schema_version":1}]}`))
			case strings.Contains(sqlText, "GROUP BY category"):
				_, _ = w.Write([]byte(`{"data":[{"category":"webhook","cnt":"1"}]}`))
			case strings.Contains(sqlText, "GROUP BY severity"):
				_, _ = w.Write([]byte(`{"data":[{"severity":"info","cnt":"1"}]}`))
			default:
				w.WriteHeader(http.StatusOK)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	ch, err := NewClickHouseBackend(config.EventStoreConfig{
		ClickHouseURL:      ts.URL,
		ClickHouseDatabase: "runway",
		ClickHouseUsername: "default",
		RetentionDays:      60,
	})
	if err != nil {
		t.Fatalf("NewClickHouseBackend: %v", err)
	}

	ctx := context.Background()
	if err := ch.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := ch.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if len(queries) < 2 || !strings.Contains(queries[1], "ENGINE = MergeTree()") || !strings.Contains(queries[1], "INTERVAL 60 DAY DELETE") {
		t.Fatalf("unexpected ClickHouse DDL queries: %v", queries)
	}

	err = ch.InsertBatch(ctx, []domain.Event{normalizeEvent(domain.Event{
		Category:      domain.EventCategoryWebhook,
		EventType:     "webhook.processed",
		CorrelationID: "del-1",
		RepoFullName:  "cubetiqlabs/runway",
		Payload:       json.RawMessage(`{"ok":true}`),
	})})
	if err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	if len(insertedLines) != 1 || !strings.Contains(insertedLines[0], `"correlation_id":"del-1"`) {
		t.Fatalf("unexpected inserted JSONEachRow payload: %v", insertedLines)
	}

	items, total, err := ch.Query(ctx, domain.EventFilter{Category: "webhook", Search: "del-1"})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("Query failed: items=%d total=%d err=%v", len(items), total, err)
	}

	sum, err := ch.GetSummary(ctx, time.Now().Add(-time.Hour))
	if err != nil || sum.TotalEvents != 1 || sum.ByCategory["webhook"] != 1 {
		t.Fatalf("GetSummary failed: %+v err=%v", sum, err)
	}
}

func TestPostgresWhereBuilderAndMonthlyPartitionDDL(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	ddl := monthlyPartitionDDL(now)
	if !strings.Contains(ddl, "events_2026_11") || !strings.Contains(ddl, "'2026-11-01'") || !strings.Contains(ddl, "'2026-12-01'") {
		t.Fatalf("unexpected partition DDL: %s", ddl)
	}

	where, args := buildPostgresWhere(domain.EventFilter{
		Category: "runner_log",
		Severity: "error",
		Search:   "100%_fail",
	})
	if !strings.Contains(where, "category = $1") || !strings.Contains(where, "severity = $2") || !strings.Contains(where, "ILIKE $3") {
		t.Fatalf("unexpected where clause: %s", where)
	}
	if len(args) != 3 || args[2] != `%100\%\_fail%` {
		t.Fatalf("unexpected escaped args: %v", args)
	}

	pts := []domain.AgentMetricPoint{
		{CPUUsagePercent: 30},
		{CPUUsagePercent: 20},
		{CPUUsagePercent: 10},
	}
	reverseMetricPoints(pts)
	if pts[0].CPUUsagePercent != 10 || pts[2].CPUUsagePercent != 30 {
		t.Fatalf("expected ascending chronological order after reverse, got %+v", pts)
	}
}
