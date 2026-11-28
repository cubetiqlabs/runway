package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cubetiqlabs/runway/internal/cache"
	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/crypto"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/moderation"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type cacheTestAuthRepo struct {
	mu       sync.Mutex
	sessions map[string]*domain.Session
	tokens   map[string]*domain.APIToken
	users    map[uuid.UUID]*domain.User

	sessQueries  atomic.Int32
	tokenQueries atomic.Int32
	touchCalls   atomic.Int32
}

func newCacheTestAuthRepo() *cacheTestAuthRepo {
	return &cacheTestAuthRepo{
		sessions: make(map[string]*domain.Session),
		tokens:   make(map[string]*domain.APIToken),
		users:    make(map[uuid.UUID]*domain.User),
	}
}

func (r *cacheTestAuthRepo) CountUsers(context.Context) (int, error)          { return len(r.users), nil }
func (r *cacheTestAuthRepo) CreateUser(context.Context, *domain.User) error { return nil }
func (r *cacheTestAuthRepo) GetUserByEmail(context.Context, string) (*domain.User, error) {
	return nil, domain.ErrNotFound
}
func (r *cacheTestAuthRepo) GetUserByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[id]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, domain.ErrNotFound
}
func (r *cacheTestAuthRepo) CreateSession(_ context.Context, s *domain.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *s
	r.sessions[s.TokenHash] = &cp
	return nil
}
func (r *cacheTestAuthRepo) GetSessionByTokenHash(_ context.Context, hash string) (*domain.Session, *domain.User, error) {
	r.sessQueries.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[hash]
	if !ok {
		return nil, nil, domain.ErrNotFound
	}
	u := r.users[s.UserID]
	sCp := *s
	uCp := *u
	return &sCp, &uCp, nil
}
func (r *cacheTestAuthRepo) DeleteSessionByTokenHash(_ context.Context, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, hash)
	return nil
}
func (r *cacheTestAuthRepo) DeleteExpiredSessions(context.Context) error { return nil }
func (r *cacheTestAuthRepo) CreateAPIToken(_ context.Context, tok *domain.APIToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *tok
	r.tokens[tok.TokenHash] = &cp
	return nil
}
func (r *cacheTestAuthRepo) ListAPITokensByUser(context.Context, uuid.UUID) ([]domain.APIToken, error) {
	return nil, nil
}
func (r *cacheTestAuthRepo) GetAPITokenByHash(_ context.Context, hash string) (*domain.APIToken, *domain.User, error) {
	r.tokenQueries.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	tok, ok := r.tokens[hash]
	if !ok {
		return nil, nil, domain.ErrNotFound
	}
	u := r.users[tok.UserID]
	tCp := *tok
	uCp := *u
	return &tCp, &uCp, nil
}
func (r *cacheTestAuthRepo) TouchAPIToken(context.Context, uuid.UUID) error {
	r.touchCalls.Add(1)
	return nil
}
func (r *cacheTestAuthRepo) DeleteAPIToken(_ context.Context, id, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for h, tok := range r.tokens {
		if tok.ID == id && tok.UserID == userID {
			delete(r.tokens, h)
			return nil
		}
	}
	return domain.ErrNotFound
}

type cacheTestAuditRepo struct {
	deliveries   map[string]bool
	insertCalls  atomic.Int32
	overviewCall atomic.Int32
}

func newCacheTestAuditRepo() *cacheTestAuditRepo {
	return &cacheTestAuditRepo{deliveries: make(map[string]bool)}
}

func (r *cacheTestAuditRepo) InsertDeliveryIfAbsent(_ context.Context, d *domain.WebhookDelivery) (bool, error) {
	r.insertCalls.Add(1)
	if r.deliveries[d.DeliveryID] {
		return false, nil
	}
	r.deliveries[d.DeliveryID] = true
	return true, nil
}
func (r *cacheTestAuditRepo) UpdateDeliveryStatus(context.Context, string, string, string, int) error {
	return nil
}
func (r *cacheTestAuditRepo) ListDeliveries(context.Context, domain.DeliveryFilter) ([]domain.WebhookDelivery, int, error) {
	return nil, 0, nil
}
func (r *cacheTestAuditRepo) AppendAuditLog(context.Context, *domain.AuditLog) error { return nil }
func (r *cacheTestAuditRepo) ListAuditLogs(context.Context, domain.AuditFilter) ([]domain.AuditLog, int, error) {
	return nil, 0, nil
}
func (r *cacheTestAuditRepo) GetDashboardOverview(context.Context) (*domain.DashboardOverview, error) {
	r.overviewCall.Add(1)
	return &domain.DashboardOverview{TotalRunners: 3, OnlineRunners: 2}, nil
}

type cacheTestAgentRepo struct {
	*drainAgentRepo
	tokenQueries atomic.Int32
}

func (r *cacheTestAgentRepo) GetByTokenHash(_ context.Context, hash string) (*domain.Agent, error) {
	r.tokenQueries.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ag := range r.agents {
		if ag.TokenHash == hash {
			cp := *ag
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *cacheTestAgentRepo) UpdateToken(_ context.Context, id uuid.UUID, prefix, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ag, ok := r.agents[id]
	if !ok {
		return domain.ErrNotFound
	}
	ag.TokenPrefix = prefix
	ag.TokenHash = hash
	return nil
}

func TestAuthServiceCachingAndInvalidation(t *testing.T) {
	ctx := context.Background()
	store := cache.NewMemoryStore("auth:", "api-1")
	defer store.Close()

	authRepo := newCacheTestAuthRepo()
	uid := uuid.New()
	authRepo.users[uid] = &domain.User{
		ID:       uid,
		Email:    "admin@runway.local",
		Role:     domain.RoleAdmin,
		IsActive: true,
	}

	rawSession := "sess_secret_value"
	_ = authRepo.CreateSession(ctx, &domain.Session{
		ID:        uuid.New(),
		UserID:    uid,
		TokenHash: crypto.HashTokenSHA256(rawSession),
		CSRFToken: "csrf123",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})

	rawAPI := "rwy_test_bearer_token"
	tokID := uuid.New()
	_ = authRepo.CreateAPIToken(ctx, &domain.APIToken{
		ID:        tokID,
		UserID:    uid,
		Name:      "ci",
		Prefix:    "rwy_test",
		TokenHash: crypto.HashTokenSHA256(rawAPI),
		Scopes:    []string{"read"},
	})

	svc := NewAuthService(&config.Config{}, zap.NewNop(), authRepo, newCacheTestAuditRepo())
	svc.SetCache(store)

	// Session cache hit & logout eviction
	if _, _, err := svc.AuthenticateSession(ctx, rawSession); err != nil {
		t.Fatalf("first AuthenticateSession failed: %v", err)
	}
	if _, _, err := svc.AuthenticateSession(ctx, rawSession); err != nil {
		t.Fatalf("second AuthenticateSession failed: %v", err)
	}
	if authRepo.sessQueries.Load() != 1 {
		t.Fatalf("expected 1 DB session lookup with cache enabled, got %d", authRepo.sessQueries.Load())
	}
	if err := svc.Logout(ctx, rawSession); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}
	if _, _, err := svc.AuthenticateSession(ctx, rawSession); err == nil {
		t.Fatalf("expected logged out session to fail immediately")
	}

	// Bearer token cache hit, touch throttling & DeleteAPIToken eviction
	if _, _, err := svc.AuthenticateBearerToken(ctx, rawAPI); err != nil {
		t.Fatalf("first AuthenticateBearerToken failed: %v", err)
	}
	if _, _, err := svc.AuthenticateBearerToken(ctx, rawAPI); err != nil {
		t.Fatalf("second AuthenticateBearerToken failed: %v", err)
	}
	if authRepo.tokenQueries.Load() != 1 {
		t.Fatalf("expected 1 DB token lookup, got %d", authRepo.tokenQueries.Load())
	}
	if authRepo.touchCalls.Load() != 1 {
		t.Fatalf("expected throttled TouchAPIToken (1 call), got %d", authRepo.touchCalls.Load())
	}
	if err := svc.DeleteAPIToken(ctx, tokID, uid); err != nil {
		t.Fatalf("DeleteAPIToken failed: %v", err)
	}
	if _, _, err := svc.AuthenticateBearerToken(ctx, rawAPI); err == nil {
		t.Fatalf("expected deleted API token to fail authentication immediately")
	}
}

func TestAgentServiceAuthCachingAndRotateInvalidation(t *testing.T) {
	ctx := context.Background()
	store := cache.NewMemoryStore("agent:", "api-1")
	defer store.Close()

	repo := &cacheTestAgentRepo{drainAgentRepo: newDrainAgentRepo()}
	agID := uuid.New()
	oldTok := "rwa_initial_token_value"
	repo.agents[agID] = &domain.Agent{
		ID:             agID,
		Name:           "mac-mini-1",
		TokenPrefix:    oldTok[:12],
		TokenHash:      crypto.HashTokenSHA256(oldTok),
		Status:         domain.AgentOnline,
		MaxConcurrency: 2,
	}

	svc := NewAgentService(zap.NewNop(), repo, nil, nil, newCacheTestAuditRepo(), nil, nil)
	svc.SetCache(store)

	if _, err := svc.AuthenticateAgent(ctx, oldTok); err != nil {
		t.Fatalf("first AuthenticateAgent failed: %v", err)
	}
	if _, err := svc.AuthenticateAgent(ctx, oldTok); err != nil {
		t.Fatalf("second AuthenticateAgent failed: %v", err)
	}
	if repo.tokenQueries.Load() != 1 {
		t.Fatalf("expected 1 DB agent token query, got %d", repo.tokenQueries.Load())
	}

	rotated, err := svc.RotateToken(ctx, agID, nil, "127.0.0.1")
	if err != nil {
		t.Fatalf("RotateToken failed: %v", err)
	}
	if _, err := svc.AuthenticateAgent(ctx, oldTok); err == nil {
		t.Fatalf("expected old agent token to be invalidated immediately after RotateToken")
	}
	if _, err := svc.AuthenticateAgent(ctx, rotated.Token); err != nil {
		t.Fatalf("expected new rotated token to authenticate, got %v", err)
	}
}

func TestDashboardAndWebhookCaching(t *testing.T) {
	ctx := context.Background()
	store := cache.NewMemoryStore("dash:", "api-node-1")
	store.SetClusterEnabled(true)
	defer store.Close()

	_ = store.RegisterNode(ctx, domain.ClusterNode{
		InstanceID: "api-node-1",
		Hostname:   "control-1",
		Version:    "1.2.3",
		Address:    ":8080",
		StartedAt:  time.Now().UTC(),
		LastSeenAt: time.Now().UTC(),
	}, time.Minute)

	auditRepo := newCacheTestAuditRepo()
	cfg := &config.Config{
		GitHub:     config.GitHubConfig{AppSlug: "runway", WebhookSecret: ""},
		Moderation: config.ModerationConfig{DefaultAction: "allow"},
		Orchestrator: config.OrchestratorConfig{
			Provider: "simulated",
		},
	}
	dash := NewDashboardService(cfg, nil, auditRepo, nil)
	dash.SetCache(store)

	ov1, err := dash.GetOverview(ctx)
	if err != nil || ov1.TotalRunners != 3 {
		t.Fatalf("expected overview TotalRunners=3, got %+v err=%v", ov1, err)
	}
	ov2, err := dash.GetOverview(ctx)
	if err != nil || ov2.TotalRunners != 3 {
		t.Fatalf("expected cached overview TotalRunners=3, got %+v err=%v", ov2, err)
	}
	if auditRepo.overviewCall.Load() != 1 {
		t.Fatalf("expected 1 DB overview call, got %d", auditRepo.overviewCall.Load())
	}

	status := dash.GetSettingsStatus(ctx)
	if status.CacheDriver != "memory" || !status.CacheHealthy || !status.CacheClusterEnabled {
		t.Fatalf("unexpected cache settings status: %+v", status)
	}
	if len(status.ClusterNodes) != 1 || status.ClusterNodes[0].InstanceID != "api-node-1" {
		t.Fatalf("expected 1 cluster node api-node-1, got %+v", status.ClusterNodes)
	}

	wh := NewWebhookService(cfg, zap.NewNop(), nil, nil, &memPoolRepo{}, nil, nil, nil, auditRepo, moderation.NewEngine("allow"), nil, nil)
	wh.SetCache(store)

	in := WebhookInput{
		DeliveryID: "delivery-cache-1",
		EventName:  "ping",
		RawBody:    []byte(`{"zen":"fast"}`),
	}
	if err := wh.HandleGitHubWebhook(ctx, in); err != nil {
		t.Fatalf("first webhook failed: %v", err)
	}
	if err := wh.HandleGitHubWebhook(ctx, in); err != nil {
		t.Fatalf("duplicate webhook failed: %v", err)
	}
	if auditRepo.insertCalls.Load() != 1 {
		t.Fatalf("expected SetNX fast-path dedup to skip second DB insert, inserts=%d", auditRepo.insertCalls.Load())
	}
}

func TestDispatcherClusterPubSubWakeup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := cache.NewMemoryStore("disp:", "api-1")
	store.SetClusterEnabled(true)
	defer store.Close()

	poolID := uuid.New()
	poolRepo := &memPoolRepo{
		pools: []domain.RunnerPool{{
			ID:             poolID,
			Name:           "Linux",
			Slug:           "runway-linux",
			Labels:         []string{"self-hosted", "linux"},
			MaxConcurrency: 4,
			Provider:       "simulated",
			Enabled:        true,
		}},
	}
	runnerRepo := newMemRunnerRepo()
	jobRepo := newMemJobRepo()
	jobID := uuid.New()
	jobRepo.jobs[jobID] = &domain.WorkflowJob{
		ID:                 jobID,
		GitHubJobID:        9901,
		RepoFullName:       "cubetiqlabs/runway",
		Labels:             []string{"self-hosted", "linux"},
		Status:             domain.JobQueued,
		MatchedPoolID:      &poolID,
		ModerationDecision: string(domain.ActionAllow),
		QueuedAt:           time.Now().UTC(),
	}

	orchCfg := config.OrchestratorConfig{
		Provider:          "simulated",
		WorkerCount:       1,
		QueueSize:         8,
		ReconcileInterval: time.Minute,
		ProvisionTimeout:  10 * time.Second,
		StaleHeartbeatTTL: time.Minute,
	}
	orch := orchestrator.NewOrchestrator(orchCfg, zap.NewNop(), runnerRepo, jobRepo, nil, nil, nil)
	orch.SetCache(store)
	orch.Start(ctx)

	disp := NewJobDispatcher(&config.Config{Orchestrator: orchCfg}, zap.NewNop(), nil, nil, poolRepo, jobRepo, runnerRepo, nil, nil, orch)
	disp.SetCache(store)
	disp.sweepInterval = time.Hour // Only drain on startup or explicit wakeup

	// Hold the distributed drain lock momentarily so startup sweep returns early,
	// leaving the job queued so only the Pub/Sub wakeup drains it.
	startupLock, acquired, _ := store.AcquireLock(ctx, "dispatcher:drain", time.Second)
	disp.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	if acquired && startupLock != nil {
		_ = startupLock.Release(ctx)
	}

	// Simulate remote cluster replica ("api-2") publishing slot_freed.
	if err := store.Publish(ctx, cacheClusterSlotFreed, "api-2"); err != nil {
		t.Fatalf("publish slot_freed failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, err := jobRepo.GetByID(ctx, jobID)
		if err == nil && j.Status == domain.JobProvisioning {
			cancel()
			disp.Wait()
			orch.Wait()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	disp.Wait()
	orch.Wait()
	t.Fatalf("expected job to transition to provisioning after cluster Pub/Sub slot_freed wakeup")
}
