package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cubetiqlabs/runway/internal/cache"
	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/moderation"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
	"github.com/cubetiqlabs/runway/internal/service"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type stubAuditRepo struct{}

func (s *stubAuditRepo) InsertDeliveryIfAbsent(context.Context, *domain.WebhookDelivery) (bool, error) {
	return true, nil
}
func (s *stubAuditRepo) UpdateDeliveryStatus(context.Context, string, string, string, int) error {
	return nil
}
func (s *stubAuditRepo) ListDeliveries(context.Context, domain.DeliveryFilter) ([]domain.WebhookDelivery, int, error) {
	return nil, 0, nil
}
func (s *stubAuditRepo) AppendAuditLog(context.Context, *domain.AuditLog) error { return nil }
func (s *stubAuditRepo) ListAuditLogs(context.Context, domain.AuditFilter) ([]domain.AuditLog, int, error) {
	return nil, 0, nil
}
func (s *stubAuditRepo) GetDashboardOverview(context.Context) (*domain.DashboardOverview, error) {
	return &domain.DashboardOverview{}, nil
}

type stubAuthRepo struct{}

func (s *stubAuthRepo) CountUsers(context.Context) (int, error)          { return 1, nil }
func (s *stubAuthRepo) CreateUser(context.Context, *domain.User) error { return nil }
func (s *stubAuthRepo) GetUserByEmail(context.Context, string) (*domain.User, error) {
	return nil, domain.ErrNotFound
}
func (s *stubAuthRepo) GetUserByID(context.Context, uuid.UUID) (*domain.User, error) {
	return nil, domain.ErrNotFound
}
func (s *stubAuthRepo) CreateSession(context.Context, *domain.Session) error { return nil }
func (s *stubAuthRepo) GetSessionByTokenHash(context.Context, string) (*domain.Session, *domain.User, error) {
	return nil, nil, domain.ErrNotFound
}
func (s *stubAuthRepo) DeleteSessionByTokenHash(context.Context, string) error { return nil }
func (s *stubAuthRepo) DeleteExpiredSessions(context.Context) error            { return nil }
func (s *stubAuthRepo) CreateAPIToken(context.Context, *domain.APIToken) error { return nil }
func (s *stubAuthRepo) ListAPITokensByUser(context.Context, uuid.UUID) ([]domain.APIToken, error) {
	return nil, nil
}
func (s *stubAuthRepo) GetAPITokenByHash(context.Context, string) (*domain.APIToken, *domain.User, error) {
	return nil, nil, domain.ErrNotFound
}
func (s *stubAuthRepo) TouchAPIToken(context.Context, uuid.UUID) error            { return nil }
func (s *stubAuthRepo) DeleteAPIToken(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func signPayload(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestServerSecurityAndRouting(t *testing.T) {
	cfg := &config.Config{
		App: config.AppConfig{Name: "runway", Environment: "development"},
		Server: config.ServerConfig{
			BodyLimitBytes: 1 << 20,
			ReadTimeout:    5 * time.Second,
			WriteTimeout:   5 * time.Second,
			IdleTimeout:    5 * time.Second,
		},
		GitHub: config.GitHubConfig{WebhookSecret: "super-secret-webhook-key"},
		Security: config.SecurityConfig{
			LoginRateMax:   20,
			WebhookRateMax: 100,
		},
		Orchestrator: config.OrchestratorConfig{Provider: "simulated", WorkerCount: 1, QueueSize: 8},
	}

	log := zap.NewNop()
	auditRepo := &stubAuditRepo{}
	authSvc := service.NewAuthService(cfg, log, &stubAuthRepo{}, auditRepo)
	orch := orchestrator.NewOrchestrator(cfg.Orchestrator, log, nil, nil, auditRepo, nil, nil)
	webhookSvc := service.NewWebhookService(cfg, log, nil, nil, nil, nil, nil, nil, auditRepo, moderation.NewEngine("allow"), orch, nil)
	runnerSvc := service.NewRunnerService(cfg, nil, nil, nil, nil, auditRepo, orch)
	modSvc := service.NewModerationService(nil, nil, nil, nil, nil, auditRepo, moderation.NewEngine("allow"), orch, nil)
	dashSvc := service.NewDashboardService(cfg, nil, auditRepo, orch)

	app, err := NewServer(ServerDeps{
		Config:        cfg,
		Log:           log,
		ReadyCheck:    func(context.Context) error { return nil },
		AuthSvc:       authSvc,
		WebhookSvc:    webhookSvc,
		RunnerSvc:     runnerSvc,
		ModerationSvc: modSvc,
		DashboardSvc:  dashSvc,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// 1. Healthz returns 200
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /healthz, got %v %v", resp, err)
	}

	// 2. Protected API rejects unauthenticated access (including agents & agent worker heartbeat)
	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/overview", nil))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated protected endpoint, got %d %v", resp.StatusCode, err)
	}
	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated /api/v1/agents, got %d %v", resp.StatusCode, err)
	}
	resp, err = app.Test(httptest.NewRequest(http.MethodPost, "/api/v1/agent/heartbeat", nil))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated /api/v1/agent/heartbeat, got %d %v", resp.StatusCode, err)
	}

	// 3. Unknown API route outside v1 auth returns JSON 404 (not SPA HTML), and unauthenticated v1 returns 401
	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/nonexistent", nil))
	if err != nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on unknown /api route, got %d %v", resp.StatusCode, err)
	}
	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated /api/v1 route, got %d %v", resp.StatusCode, err)
	}

	// 4. Webhook rejects invalid signature with 401
	body, _ := json.Marshal(map[string]any{"zen": "Keep it logically awesome."})
	reqBad := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	reqBad.Header.Set("X-GitHub-Event", "ping")
	reqBad.Header.Set("X-GitHub-Delivery", "deliv-bad-1")
	reqBad.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	resp, err = app.Test(reqBad)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on invalid webhook signature, got %d %v", resp.StatusCode, err)
	}

	// 5. Webhook accepts valid signature with 202
	reqGood := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	reqGood.Header.Set("X-GitHub-Event", "ping")
	reqGood.Header.Set("X-GitHub-Delivery", "deliv-good-1")
	reqGood.Header.Set("X-Hub-Signature-256", signPayload("super-secret-webhook-key", body))
	resp, err = app.Test(reqGood)
	if err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 on valid webhook signature, got %d %v", resp.StatusCode, err)
	}

	// 6. Embedded SPA fallback serves HTML for client routes
	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/runners", nil))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on SPA route /runners, got %d %v", resp.StatusCode, err)
	}
}

func TestClusterRateLimitStorageAndServer(t *testing.T) {
	store := cache.NewMemoryStore("http:", "api-1")
	store.SetClusterEnabled(true)
	defer store.Close()

	storage := newCacheFiberStorage(store, "ratelimit:test:")
	if v, err := storage.Get("missing"); err != nil || v != nil {
		t.Fatalf("expected nil, nil on missing key, got %v %v", v, err)
	}
	if err := storage.Set("ip1", []byte("5"), time.Minute); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if v, err := storage.Get("ip1"); err != nil || string(v) != "5" {
		t.Fatalf("expected 5, got %q %v", string(v), err)
	}
	if err := storage.Delete("ip1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if v, err := storage.Get("ip1"); err != nil || v != nil {
		t.Fatalf("expected nil after delete, got %v %v", v, err)
	}
	_ = storage.Set("ip2", []byte("2"), time.Minute)
	if err := storage.Reset(); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}
	if v, _ := storage.Get("ip2"); v != nil {
		t.Fatalf("expected nil after reset, got %v", v)
	}

	cfg := &config.Config{
		App: config.AppConfig{Name: "runway", Environment: "development"},
		Server: config.ServerConfig{
			BodyLimitBytes: 1 << 20,
			ReadTimeout:    5 * time.Second,
			WriteTimeout:   5 * time.Second,
			IdleTimeout:    5 * time.Second,
		},
		Security: config.SecurityConfig{
			LoginRateMax:   2,
			WebhookRateMax: 10,
		},
		Orchestrator: config.OrchestratorConfig{Provider: "simulated", WorkerCount: 1, QueueSize: 8},
	}

	log := zap.NewNop()
	auditRepo := &stubAuditRepo{}
	authSvc := service.NewAuthService(cfg, log, &stubAuthRepo{}, auditRepo)
	orch := orchestrator.NewOrchestrator(cfg.Orchestrator, log, nil, nil, auditRepo, nil, nil)
	webhookSvc := service.NewWebhookService(cfg, log, nil, nil, nil, nil, nil, nil, auditRepo, moderation.NewEngine("allow"), orch, nil)
	runnerSvc := service.NewRunnerService(cfg, nil, nil, nil, nil, auditRepo, orch)
	modSvc := service.NewModerationService(nil, nil, nil, nil, nil, auditRepo, moderation.NewEngine("allow"), orch, nil)
	dashSvc := service.NewDashboardService(cfg, nil, auditRepo, orch)
	dashSvc.SetCache(store)

	app, err := NewServer(ServerDeps{
		Config:        cfg,
		Log:           log,
		ReadyCheck:    func(context.Context) error { return nil },
		CacheStore:    store,
		AuthSvc:       authSvc,
		WebhookSvc:    webhookSvc,
		RunnerSvc:     runnerSvc,
		ModerationSvc: modSvc,
		DashboardSvc:  dashSvc,
	})
	if err != nil {
		t.Fatalf("NewServer with CacheStore failed: %v", err)
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		resp, reqErr := app.Test(req)
		if reqErr != nil {
			t.Fatalf("login request %d failed: %v", i, reqErr)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("unexpected 429 on request %d", i)
		}
	}

	reqLimited := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{}`)))
	reqLimited.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(reqLimited)
	if err != nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 3rd login request with shared limiter, got %d %v", resp.StatusCode, err)
	}
}

func TestParseWindowDuration(t *testing.T) {
	if got := parseWindowDuration("15m", time.Hour); got != 15*time.Minute {
		t.Fatalf("expected 15m, got %v", got)
	}
	if got := parseWindowDuration("7d", time.Hour); got != 7*24*time.Hour {
		t.Fatalf("expected 7d (168h), got %v", got)
	}
	if got := parseWindowDuration("30d", time.Hour); got != 30*24*time.Hour {
		t.Fatalf("expected 30d, got %v", got)
	}
	if got := parseWindowDuration("365d", time.Hour); got != 90*24*time.Hour {
		t.Fatalf("expected 365d clamped to 90d, got %v", got)
	}
}
