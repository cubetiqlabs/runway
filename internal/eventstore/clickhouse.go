package eventstore

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
)

var validCHIdent = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ClickHouseBackend stores events in ClickHouse using MergeTree with TTL and HTTP JSONEachRow batching.
type ClickHouseBackend struct {
	baseURL       string
	database      string
	username      string
	password      string
	retentionDays int
	client        *http.Client
}

func NewClickHouseBackend(cfg config.EventStoreConfig) (*ClickHouseBackend, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.ClickHouseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("clickhouse_url is required")
	}
	db := strings.TrimSpace(cfg.ClickHouseDatabase)
	if db == "" {
		db = "runway"
	}
	if !validCHIdent.MatchString(db) {
		return nil, fmt.Errorf("invalid clickhouse database identifier %q", db)
	}
	ret := cfg.RetentionDays
	if ret <= 0 {
		ret = 90
	}
	return &ClickHouseBackend{
		baseURL:       base,
		database:      db,
		username:      cfg.ClickHouseUsername,
		password:      cfg.ClickHousePassword,
		retentionDays: ret,
		client:        &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (b *ClickHouseBackend) Driver() string { return "clickhouse" }

func (b *ClickHouseBackend) EnsureSchema(ctx context.Context) error {
	stmts := []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", b.database),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.events (
			id UUID,
			occurred_at DateTime64(3, 'UTC'),
			category LowCardinality(String),
			event_type LowCardinality(String),
			severity LowCardinality(String),
			source LowCardinality(String),
			actor_type LowCardinality(String),
			actor_id String,
			actor_label String,
			resource_type LowCardinality(String),
			resource_id String,
			correlation_id String,
			repo_full_name String,
			duration_ms Int64,
			status LowCardinality(String),
			message String,
			payload String,
			ip_address String,
			schema_version UInt8
		) ENGINE = MergeTree()
		PARTITION BY toYYYYMM(occurred_at)
		ORDER BY (category, resource_type, resource_id, occurred_at, id)
		TTL toDateTime(occurred_at) + INTERVAL %d DAY DELETE
		SETTINGS index_granularity = 8192`, b.database, b.retentionDays),
		fmt.Sprintf("ALTER TABLE %s.events ADD INDEX IF NOT EXISTS idx_event_type event_type TYPE bloom_filter(0.01) GRANULARITY 4", b.database),
		fmt.Sprintf("ALTER TABLE %s.events ADD INDEX IF NOT EXISTS idx_correlation correlation_id TYPE bloom_filter(0.01) GRANULARITY 4", b.database),
		fmt.Sprintf("ALTER TABLE %s.events ADD INDEX IF NOT EXISTS idx_repo repo_full_name TYPE bloom_filter(0.01) GRANULARITY 4", b.database),
		fmt.Sprintf("ALTER TABLE %s.events ADD INDEX IF NOT EXISTS idx_message message TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 4", b.database),
	}

	for _, stmt := range stmts {
		if _, err := b.execQuery(ctx, stmt, nil, false); err != nil {
			return fmt.Errorf("clickhouse ensure schema: %w", err)
		}
	}
	return nil
}

type chEventRow struct {
	ID            string `json:"id"`
	OccurredAt    string `json:"occurred_at"`
	Category      string `json:"category"`
	EventType     string `json:"event_type"`
	Severity      string `json:"severity"`
	Source        string `json:"source"`
	ActorType     string `json:"actor_type"`
	ActorID       string `json:"actor_id"`
	ActorLabel    string `json:"actor_label"`
	ResourceType  string `json:"resource_type"`
	ResourceID    string `json:"resource_id"`
	CorrelationID string `json:"correlation_id"`
	RepoFullName  string `json:"repo_full_name"`
	DurationMS    int64  `json:"duration_ms"`
	Status        string `json:"status"`
	Message       string `json:"message"`
	Payload       string `json:"payload"`
	IPAddress     string `json:"ip_address"`
	SchemaVersion int    `json:"schema_version"`
}

func (b *ClickHouseBackend) InsertBatch(ctx context.Context, events []domain.Event) error {
	if len(events) == 0 {
		return nil
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	enc := json.NewEncoder(gz)
	enc.SetEscapeHTML(false)

	for _, ev := range events {
		payloadStr := "{}"
		if len(ev.Payload) > 0 {
			payloadStr = string(ev.Payload)
		}
		row := chEventRow{
			ID:            ev.ID.String(),
			OccurredAt:    ev.OccurredAt.UTC().Format("2006-01-02 15:04:05.000"),
			Category:      ev.Category,
			EventType:     ev.EventType,
			Severity:      ev.Severity,
			Source:        ev.Source,
			ActorType:     ev.ActorType,
			ActorID:       ev.ActorID,
			ActorLabel:    ev.ActorLabel,
			ResourceType:  ev.ResourceType,
			ResourceID:    ev.ResourceID,
			CorrelationID: ev.CorrelationID,
			RepoFullName:  ev.RepoFullName,
			DurationMS:    ev.DurationMS,
			Status:        ev.Status,
			Message:       ev.Message,
			Payload:       payloadStr,
			IPAddress:     ev.IPAddress,
			SchemaVersion: ev.SchemaVersion,
		}
		if err := enc.Encode(row); err != nil {
			_ = gz.Close()
			return err
		}
	}
	if err := gz.Close(); err != nil {
		return err
	}

	q := url.Values{}
	q.Set("query", fmt.Sprintf("INSERT INTO %s.events FORMAT JSONEachRow", b.database))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/?"+q.Encode(), &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Content-Type", "application/x-ndjson")
	b.applyAuthHeaders(req)

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("clickhouse insert batch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("clickhouse insert failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func buildClickHouseWhere(filter domain.EventFilter) (string, map[string]string) {
	var conds []string
	params := make(map[string]string)

	if filter.Category != "" {
		params["param_category"] = filter.Category
		conds = append(conds, "category = {category:String}")
	}
	if filter.EventType != "" {
		params["param_event_type"] = filter.EventType
		conds = append(conds, "event_type = {event_type:String}")
	}
	if filter.Severity != "" {
		params["param_severity"] = filter.Severity
		conds = append(conds, "severity = {severity:String}")
	}
	if filter.ResourceType != "" {
		params["param_resource_type"] = filter.ResourceType
		conds = append(conds, "resource_type = {resource_type:String}")
	}
	if filter.ResourceID != "" {
		params["param_resource_id"] = filter.ResourceID
		conds = append(conds, "resource_id = {resource_id:String}")
	}
	if filter.CorrelationID != "" {
		params["param_correlation_id"] = filter.CorrelationID
		conds = append(conds, "correlation_id = {correlation_id:String}")
	}
	if filter.Repo != "" {
		params["param_repo"] = filter.Repo
		conds = append(conds, "repo_full_name = {repo:String}")
	}
	if filter.Since != nil {
		params["param_since"] = filter.Since.UTC().Format("2006-01-02 15:04:05.000")
		conds = append(conds, "occurred_at >= parseDateTime64BestEffort({since:String}, 3, 'UTC')")
	}
	if filter.Until != nil {
		params["param_until"] = filter.Until.UTC().Format("2006-01-02 15:04:05.000")
		conds = append(conds, "occurred_at <= parseDateTime64BestEffort({until:String}, 3, 'UTC')")
	}
	if s := strings.TrimSpace(filter.Search); s != "" {
		params["param_search"] = s
		conds = append(conds, `(positionCaseInsensitive(message, {search:String}) > 0 OR positionCaseInsensitive(event_type, {search:String}) > 0 OR positionCaseInsensitive(actor_label, {search:String}) > 0 OR positionCaseInsensitive(resource_id, {search:String}) > 0 OR positionCaseInsensitive(repo_full_name, {search:String}) > 0)`)
	}
	if len(conds) == 0 {
		return "", params
	}
	return " WHERE " + strings.Join(conds, " AND "), params
}

func (b *ClickHouseBackend) Query(ctx context.Context, filter domain.EventFilter) ([]domain.Event, int, error) {
	where, params := buildClickHouseWhere(filter)
	countSQL := fmt.Sprintf("SELECT count() AS cnt FROM %s.events%s FORMAT JSON", b.database, where)
	countRaw, err := b.execQuery(ctx, countSQL, params, false)
	if err != nil {
		return nil, 0, fmt.Errorf("clickhouse count events: %w", err)
	}
	var countResp struct {
		Data []struct {
			Cnt json.Number `json:"cnt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(countRaw, &countResp); err != nil {
		return nil, 0, err
	}
	total := 0
	if len(countResp.Data) > 0 {
		if n, parseErr := countResp.Data[0].Cnt.Int64(); parseErr == nil {
			total = int(n)
		}
	}

	limit, offset := clampLimitOffset(filter.PageParams, 50, 200)
	params["param_limit"] = strconv.Itoa(limit)
	params["param_offset"] = strconv.Itoa(offset)

	dataSQL := fmt.Sprintf(`SELECT
		toString(id) AS id,
		formatDateTime(occurred_at, '%%Y-%%m-%%dT%%H:%%i:%%S.%%fZ', 'UTC') AS occurred_at,
		category, event_type, severity, source, actor_type, actor_id, actor_label,
		resource_type, resource_id, correlation_id, repo_full_name, duration_ms,
		status, message, payload, ip_address, schema_version
		FROM %s.events%s
		ORDER BY %s
		LIMIT {limit:UInt32} OFFSET {offset:UInt32}
		FORMAT JSON`, b.database, where, postgresOrderBy(filter.PageParams),
	)

	dataRaw, err := b.execQuery(ctx, dataSQL, params, false)
	if err != nil {
		return nil, 0, fmt.Errorf("clickhouse query events: %w", err)
	}
	var dataResp struct {
		Data []chEventRow `json:"data"`
	}
	if err := json.Unmarshal(dataRaw, &dataResp); err != nil {
		return nil, 0, err
	}

	items := make([]domain.Event, 0, len(dataResp.Data))
	for _, r := range dataResp.Data {
		id, _ := uuid.Parse(r.ID)
		ts := parseCHTime(r.OccurredAt)
		p := r.Payload
		if p == "" {
			p = "{}"
		}
		items = append(items, domain.Event{
			ID:            id,
			OccurredAt:    ts,
			Category:      r.Category,
			EventType:     r.EventType,
			Severity:      r.Severity,
			Source:        r.Source,
			ActorType:     r.ActorType,
			ActorID:       r.ActorID,
			ActorLabel:    r.ActorLabel,
			ResourceType:  r.ResourceType,
			ResourceID:    r.ResourceID,
			CorrelationID: r.CorrelationID,
			RepoFullName:  r.RepoFullName,
			DurationMS:    r.DurationMS,
			Status:        r.Status,
			Message:       r.Message,
			Payload:       json.RawMessage(p),
			IPAddress:     r.IPAddress,
			SchemaVersion: r.SchemaVersion,
		})
	}
	return items, total, nil
}

func (b *ClickHouseBackend) QueryAgentMetrics(ctx context.Context, agentID uuid.UUID, since time.Time, limit int) ([]domain.AgentMetricPoint, error) {
	if limit <= 0 || limit > 1000 {
		limit = 240
	}
	params := map[string]string{
		"param_agent_id": agentID.String(),
		"param_since":    since.UTC().Format("2006-01-02 15:04:05.000"),
		"param_limit":    strconv.Itoa(limit),
	}
	sqlText := fmt.Sprintf(`SELECT
		formatDateTime(occurred_at, '%%Y-%%m-%%dT%%H:%%i:%%S.%%fZ', 'UTC') AS occurred_at,
		payload
		FROM %s.events
		WHERE category = 'agent_metric' AND resource_type = 'agent' AND resource_id = {agent_id:String}
			AND occurred_at >= parseDateTime64BestEffort({since:String}, 3, 'UTC')
		ORDER BY occurred_at ASC
		LIMIT {limit:UInt32}
		FORMAT JSON`, b.database)

	raw, err := b.execQuery(ctx, sqlText, params, false)
	if err != nil {
		return nil, fmt.Errorf("clickhouse query agent metrics: %w", err)
	}
	var resp struct {
		Data []struct {
			OccurredAt string `json:"occurred_at"`
			Payload    string `json:"payload"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	points := make([]domain.AgentMetricPoint, 0, len(resp.Data))
	for _, r := range resp.Data {
		points = append(points, parseMetricPoint(parseCHTime(r.OccurredAt), []byte(r.Payload)))
	}
	return points, nil
}

func (b *ClickHouseBackend) GetSummary(ctx context.Context, since time.Time) (*domain.EventSummary, error) {
	sum := &domain.EventSummary{
		Driver:     "clickhouse",
		ByCategory: map[string]int64{},
		BySeverity: map[string]int64{},
	}
	params := map[string]string{
		"param_since": since.UTC().Format("2006-01-02 15:04:05.000"),
	}
	catRaw, err := b.execQuery(ctx, fmt.Sprintf(`SELECT category, count() AS cnt FROM %s.events WHERE occurred_at >= parseDateTime64BestEffort({since:String}, 3, 'UTC') GROUP BY category FORMAT JSON`, b.database), params, false)
	if err != nil {
		return nil, err
	}
	var catResp struct {
		Data []struct {
			Category string      `json:"category"`
			Cnt      json.Number `json:"cnt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(catRaw, &catResp); err == nil {
		for _, r := range catResp.Data {
			if n, convErr := r.Cnt.Int64(); convErr == nil {
				sum.ByCategory[r.Category] = n
				sum.TotalEvents += n
			}
		}
	}

	sevRaw, err := b.execQuery(ctx, fmt.Sprintf(`SELECT severity, count() AS cnt FROM %s.events WHERE occurred_at >= parseDateTime64BestEffort({since:String}, 3, 'UTC') GROUP BY severity FORMAT JSON`, b.database), params, false)
	if err != nil {
		return nil, err
	}
	var sevResp struct {
		Data []struct {
			Severity string      `json:"severity"`
			Cnt      json.Number `json:"cnt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(sevRaw, &sevResp); err == nil {
		for _, r := range sevResp.Data {
			if n, convErr := r.Cnt.Int64(); convErr == nil {
				sum.BySeverity[r.Severity] = n
			}
		}
	}
	return sum, nil
}

func (b *ClickHouseBackend) PruneExpired(context.Context, int) error {
	// ClickHouse handles TTL deletion natively via MergeTree table TTL.
	return nil
}

func (b *ClickHouseBackend) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/ping", nil)
	if err != nil {
		return err
	}
	b.applyAuthHeaders(req)
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("clickhouse ping: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("clickhouse ping failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (b *ClickHouseBackend) Close(context.Context) error { return nil }

func (b *ClickHouseBackend) execQuery(ctx context.Context, query string, params map[string]string, useDB bool) ([]byte, error) {
	q := url.Values{}
	if useDB {
		q.Set("database", b.database)
	}
	for k, v := range params {
		q.Set(k, v)
	}
	reqURL := b.baseURL + "/"
	if len(q) > 0 {
		reqURL += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	b.applyAuthHeaders(req)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("clickhouse error (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

func (b *ClickHouseBackend) applyAuthHeaders(req *http.Request) {
	if b.username != "" {
		req.Header.Set("X-ClickHouse-User", b.username)
	}
	if b.password != "" {
		req.Header.Set("X-ClickHouse-Key", b.password)
	}
}

func parseCHTime(raw string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
			return t.UTC()
		}
	}
	return time.Now().UTC()
}
