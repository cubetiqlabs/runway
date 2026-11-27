package eventstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const pgEventSchemaAdvisoryLockID int64 = 7491028374620

// PostgresBackend stores events in a monthly range-partitioned PostgreSQL table.
type PostgresBackend struct {
	db     *sql.DB
	ownsDB bool
}

func openPostgresDB(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres eventstore: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres eventstore: %w", err)
	}
	return db, nil
}

func NewPostgresBackend(db *sql.DB, ownsDB bool) *PostgresBackend {
	return &PostgresBackend{db: db, ownsDB: ownsDB}
}

func (b *PostgresBackend) Driver() string { return "postgres" }

func (b *PostgresBackend) EnsureSchema(ctx context.Context) error {
	conn, err := b.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire postgres connection for eventstore schema: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", pgEventSchemaAdvisoryLockID); err != nil {
		return fmt.Errorf("acquire eventstore schema advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", pgEventSchemaAdvisoryLockID)
	}()

	ddlStatements := []string{
		`CREATE TABLE IF NOT EXISTS events (
			id UUID NOT NULL,
			occurred_at TIMESTAMPTZ NOT NULL,
			category TEXT NOT NULL,
			event_type TEXT NOT NULL,
			severity TEXT NOT NULL DEFAULT 'info',
			source TEXT NOT NULL DEFAULT '',
			actor_type TEXT NOT NULL DEFAULT '',
			actor_id TEXT NOT NULL DEFAULT '',
			actor_label TEXT NOT NULL DEFAULT '',
			resource_type TEXT NOT NULL DEFAULT '',
			resource_id TEXT NOT NULL DEFAULT '',
			correlation_id TEXT NOT NULL DEFAULT '',
			repo_full_name TEXT NOT NULL DEFAULT '',
			duration_ms BIGINT NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			ip_address TEXT NOT NULL DEFAULT '',
			schema_version SMALLINT NOT NULL DEFAULT 1,
			PRIMARY KEY (occurred_at, id)
		) PARTITION BY RANGE (occurred_at)`,
		`CREATE TABLE IF NOT EXISTS events_default PARTITION OF events DEFAULT`,
		`CREATE INDEX IF NOT EXISTS idx_events_occurred_brin ON events USING BRIN (occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_events_cat_time ON events (category, occurred_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_events_res_time ON events (resource_type, resource_id, occurred_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_events_type_time ON events (event_type, occurred_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_events_corr ON events (correlation_id) WHERE correlation_id <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_events_payload_gin ON events USING GIN (payload)`,
	}

	for _, stmt := range ddlStatements {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("execute eventstore postgres ddl: %w", err)
		}
	}

	// Pre-create monthly partitions for previous month, current month, and next 2 months.
	now := time.Now().UTC()
	baseMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for offset := -1; offset <= 2; offset++ {
		m := baseMonth.AddDate(0, offset, 0)
		stmt := monthlyPartitionDDL(m)
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			// Skip if default partition already holds overlapping data or partition exists.
			if !strings.Contains(strings.ToLower(err.Error()), "already exists") && !strings.Contains(strings.ToLower(err.Error()), "overlap") {
				return fmt.Errorf("create monthly partition %s: %w", m.Format("2006_01"), err)
			}
		}
	}

	return nil
}

func monthlyPartitionDDL(monthStart time.Time) string {
	start := time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	partName := fmt.Sprintf("events_%04d_%02d", start.Year(), start.Month())
	return fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s PARTITION OF events FOR VALUES FROM ('%s') TO ('%s')",
		partName,
		start.Format("2006-01-02"),
		end.Format("2006-01-02"),
	)
}

func (b *PostgresBackend) InsertBatch(ctx context.Context, events []domain.Event) error {
	if len(events) == 0 {
		return nil
	}
	const chunkSize = 500
	for start := 0; start < len(events); start += chunkSize {
		end := start + chunkSize
		if end > len(events) {
			end = len(events)
		}
		if err := b.insertChunk(ctx, events[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func (b *PostgresBackend) insertChunk(ctx context.Context, chunk []domain.Event) error {
	const cols = 19
	args := make([]any, 0, len(chunk)*cols)
	var sb strings.Builder
	sb.WriteString(`INSERT INTO events (
		id, occurred_at, category, event_type, severity, source,
		actor_type, actor_id, actor_label, resource_type, resource_id,
		correlation_id, repo_full_name, duration_ms, status, message,
		payload, ip_address, schema_version
	) VALUES `)

	for i, ev := range chunk {
		if i > 0 {
			sb.WriteByte(',')
		}
		base := i*cols + 1
		sb.WriteByte('(')
		for c := 0; c < cols; c++ {
			if c > 0 {
				sb.WriteByte(',')
			}
			sb.WriteByte('$')
			sb.WriteString(strconv.Itoa(base + c))
			if c == 16 {
				sb.WriteString("::jsonb")
			}
		}
		sb.WriteByte(')')

		payloadStr := "{}"
		if len(ev.Payload) > 0 && json.Valid(ev.Payload) {
			payloadStr = string(ev.Payload)
		}
		args = append(args,
			ev.ID, ev.OccurredAt, ev.Category, ev.EventType, ev.Severity, ev.Source,
			ev.ActorType, ev.ActorID, ev.ActorLabel, ev.ResourceType, ev.ResourceID,
			ev.CorrelationID, ev.RepoFullName, ev.DurationMS, ev.Status, ev.Message,
			payloadStr, ev.IPAddress, ev.SchemaVersion,
		)
	}
	sb.WriteString(` ON CONFLICT (occurred_at, id) DO NOTHING`)

	_, err := b.db.ExecContext(ctx, sb.String(), args...)
	if err != nil {
		return fmt.Errorf("postgres insert events batch: %w", err)
	}
	return nil
}

func buildPostgresWhere(filter domain.EventFilter) (string, []any) {
	var conds []string
	var args []any
	add := func(clause string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(clause, len(args)))
	}
	if filter.Category != "" {
		add("category = $%d", filter.Category)
	}
	if filter.EventType != "" {
		add("event_type = $%d", filter.EventType)
	}
	if filter.Severity != "" {
		add("severity = $%d", filter.Severity)
	}
	if filter.ResourceType != "" {
		add("resource_type = $%d", filter.ResourceType)
	}
	if filter.ResourceID != "" {
		add("resource_id = $%d", filter.ResourceID)
	}
	if filter.CorrelationID != "" {
		add("correlation_id = $%d", filter.CorrelationID)
	}
	if filter.Repo != "" {
		add("repo_full_name = $%d", filter.Repo)
	}
	if filter.Since != nil {
		add("occurred_at >= $%d", filter.Since.UTC())
	}
	if filter.Until != nil {
		add("occurred_at <= $%d", filter.Until.UTC())
	}
	if s := strings.TrimSpace(filter.Search); s != "" {
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `%`, `\%`)
		escaped = strings.ReplaceAll(escaped, `_`, `\_`)
		args = append(args, "%"+escaped+"%")
		n := len(args)
		conds = append(conds, fmt.Sprintf(`(message ILIKE $%d ESCAPE '\' OR event_type ILIKE $%d ESCAPE '\' OR actor_label ILIKE $%d ESCAPE '\' OR resource_id ILIKE $%d ESCAPE '\' OR repo_full_name ILIKE $%d ESCAPE '\')`, n, n, n, n, n))
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func postgresOrderBy(p domain.PageParams) string {
	allowed := map[string]string{
		"occurred_at":    "occurred_at",
		"category":       "category",
		"event_type":     "event_type",
		"severity":       "severity",
		"resource_type":  "resource_type",
		"duration_ms":    "duration_ms",
		"status":         "status",
		"repo_full_name": "repo_full_name",
	}
	col, ok := allowed[strings.ToLower(strings.TrimSpace(p.SortBy))]
	if !ok {
		return "occurred_at DESC, id DESC"
	}
	dir := "DESC"
	if strings.EqualFold(strings.TrimSpace(p.SortDir), "asc") {
		dir = "ASC"
	}
	return col + " " + dir + ", occurred_at DESC"
}

func (b *PostgresBackend) Query(ctx context.Context, filter domain.EventFilter) ([]domain.Event, int, error) {
	where, args := buildPostgresWhere(filter)
	var total int
	if err := b.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count postgres events: %w", err)
	}

	limit, offset := clampLimitOffset(filter.PageParams, 50, 200)
	queryArgs := append(append([]any(nil), args...), limit, offset)
	limitIdx := len(args) + 1
	offsetIdx := len(args) + 2

	query := fmt.Sprintf(`SELECT
		id, occurred_at, category, event_type, severity, source,
		actor_type, actor_id, actor_label, resource_type, resource_id,
		correlation_id, repo_full_name, duration_ms, status, message,
		payload::text, ip_address, schema_version
		FROM events%s ORDER BY %s LIMIT $%d OFFSET $%d`,
		where, postgresOrderBy(filter.PageParams), limitIdx, offsetIdx,
	)

	rows, err := b.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query postgres events: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Event, 0, limit)
	for rows.Next() {
		var ev domain.Event
		var payloadText string
		if err := rows.Scan(
			&ev.ID, &ev.OccurredAt, &ev.Category, &ev.EventType, &ev.Severity, &ev.Source,
			&ev.ActorType, &ev.ActorID, &ev.ActorLabel, &ev.ResourceType, &ev.ResourceID,
			&ev.CorrelationID, &ev.RepoFullName, &ev.DurationMS, &ev.Status, &ev.Message,
			&payloadText, &ev.IPAddress, &ev.SchemaVersion,
		); err != nil {
			return nil, 0, err
		}
		if payloadText == "" {
			payloadText = "{}"
		}
		ev.Payload = json.RawMessage(payloadText)
		items = append(items, ev)
	}
	return items, total, rows.Err()
}

func (b *PostgresBackend) QueryAgentMetrics(ctx context.Context, agentID uuid.UUID, since time.Time, limit int) ([]domain.AgentMetricPoint, error) {
	if limit <= 0 || limit > 1000 {
		limit = 240
	}
	rows, err := b.db.QueryContext(ctx, `
		SELECT occurred_at, payload::text
		FROM events
		WHERE category = 'agent_metric' AND resource_type = 'agent' AND resource_id = $1 AND occurred_at >= $2
		ORDER BY occurred_at ASC
		LIMIT $3`,
		agentID.String(), since.UTC(), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query postgres agent metrics: %w", err)
	}
	defer rows.Close()

	points := make([]domain.AgentMetricPoint, 0)
	for rows.Next() {
		var ts time.Time
		var raw string
		if err := rows.Scan(&ts, &raw); err != nil {
			return nil, err
		}
		pt := parseMetricPoint(ts, []byte(raw))
		points = append(points, pt)
	}
	return points, rows.Err()
}

func (b *PostgresBackend) GetSummary(ctx context.Context, since time.Time) (*domain.EventSummary, error) {
	sum := &domain.EventSummary{
		Driver:     "postgres",
		ByCategory: map[string]int64{},
		BySeverity: map[string]int64{},
	}

	rows, err := b.db.QueryContext(ctx, `SELECT category, COUNT(*) FROM events WHERE occurred_at >= $1 GROUP BY category`, since.UTC())
	if err != nil {
		return nil, fmt.Errorf("postgres event summary by category: %w", err)
	}
	for rows.Next() {
		var cat string
		var cnt int64
		if scanErr := rows.Scan(&cat, &cnt); scanErr == nil {
			sum.ByCategory[cat] = cnt
			sum.TotalEvents += cnt
		}
	}
	rows.Close()

	sevRows, err := b.db.QueryContext(ctx, `SELECT severity, COUNT(*) FROM events WHERE occurred_at >= $1 GROUP BY severity`, since.UTC())
	if err != nil {
		return nil, fmt.Errorf("postgres event summary by severity: %w", err)
	}
	for sevRows.Next() {
		var sev string
		var cnt int64
		if scanErr := sevRows.Scan(&sev, &cnt); scanErr == nil {
			sum.BySeverity[sev] = cnt
		}
	}
	sevRows.Close()

	return sum, nil
}

func (b *PostgresBackend) PruneExpired(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	if _, err := b.db.ExecContext(ctx, `DELETE FROM events_default WHERE occurred_at < $1`, cutoff); err != nil {
		return err
	}
	cutoffMonth := fmt.Sprintf("events_%04d_%02d", cutoff.Year(), cutoff.Month())
	rows, err := b.db.QueryContext(ctx, `
		SELECT c.relname
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = 'events' AND c.relname ~ '^events_[0-9]{4}_[0-9]{2}$' AND c.relname < $1`,
		cutoffMonth,
	)
	if err != nil {
		return err
	}
	var toDrop []string
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr == nil {
			toDrop = append(toDrop, name)
		}
	}
	rows.Close()
	for _, name := range toDrop {
		if _, err := b.db.ExecContext(ctx, "DROP TABLE IF EXISTS "+name); err != nil {
			return err
		}
	}
	return nil
}

func (b *PostgresBackend) Ping(ctx context.Context) error {
	return b.db.PingContext(ctx)
}

func (b *PostgresBackend) Close(context.Context) error {
	if b.ownsDB && b.db != nil {
		return b.db.Close()
	}
	return nil
}

func parseMetricPoint(ts time.Time, raw []byte) domain.AgentMetricPoint {
	var p struct {
		CPUUsagePercent    float64 `json:"cpu_usage_percent"`
		MemoryUsagePercent float64 `json:"memory_usage_percent"`
		DiskUsagePercent   float64 `json:"disk_usage_percent"`
		MemoryUsedMB       int     `json:"memory_used_mb"`
		ActiveJobs         int     `json:"active_jobs"`
		LoadAvg1           float64 `json:"load_avg_1"`
		CacheHitCount      int64   `json:"cache_hit_count"`
		CacheMissCount     int64   `json:"cache_miss_count"`
	}
	_ = json.Unmarshal(raw, &p)
	return domain.AgentMetricPoint{
		Timestamp:          ts.UTC(),
		CPUUsagePercent:    p.CPUUsagePercent,
		MemoryUsagePercent: p.MemoryUsagePercent,
		DiskUsagePercent:   p.DiskUsagePercent,
		MemoryUsedMB:       p.MemoryUsedMB,
		ActiveJobs:         p.ActiveJobs,
		LoadAvg1:           p.LoadAvg1,
		CacheHitCount:      p.CacheHitCount,
		CacheMissCount:     p.CacheMissCount,
	}
}
