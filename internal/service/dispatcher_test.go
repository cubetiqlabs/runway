package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/github"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type memPoolRepo struct {
	mu    sync.Mutex
	pools []domain.RunnerPool
}

func (m *memPoolRepo) Count(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pools), nil
}
func (m *memPoolRepo) Create(_ context.Context, p *domain.RunnerPool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pools = append(m.pools, *p)
	return nil
}
func (m *memPoolRepo) Update(_ context.Context, p *domain.RunnerPool) error { return nil }
func (m *memPoolRepo) Delete(_ context.Context, id uuid.UUID) error         { return nil }
func (m *memPoolRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.RunnerPool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pools {
		if p.ID == id {
			cp := p
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (m *memPoolRepo) List(_ context.Context, page domain.PageParams) ([]domain.RunnerPool, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.RunnerPool(nil), m.pools...), len(m.pools), nil
}
func (m *memPoolRepo) ListEnabled(context.Context) ([]domain.RunnerPool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.RunnerPool
	for _, p := range m.pools {
		if p.Enabled {
			out = append(out, p)
		}
	}
	return out, nil
}

type memRunnerRepo struct {
	mu      sync.Mutex
	runners map[uuid.UUID]*domain.Runner
}

func newMemRunnerRepo() *memRunnerRepo {
	return &memRunnerRepo{runners: make(map[uuid.UUID]*domain.Runner)}
}

func (m *memRunnerRepo) Create(_ context.Context, rn *domain.Runner) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rn.ID == uuid.Nil {
		rn.ID = uuid.New()
	}
	cp := *rn
	m.runners[rn.ID] = &cp
	return nil
}

func (m *memRunnerRepo) UpdateStatus(_ context.Context, id uuid.UUID, status domain.RunnerStatus, ghID *int64, ref string, jobID *int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rn, ok := m.runners[id]
	if !ok {
		return domain.ErrNotFound
	}
	rn.Status = status
	if ghID != nil {
		rn.GitHubRunnerID = ghID
	}
	if ref != "" {
		rn.ProviderRef = ref
	}
	if jobID != nil {
		rn.CurrentJobID = jobID
	}
	return nil
}

func (m *memRunnerRepo) SetProvisioned(ctx context.Context, id uuid.UUID, status domain.RunnerStatus, ghID *int64, ref string, jobID *int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rn, ok := m.runners[id]
	if !ok || rn.Status != domain.RunnerProvisioning {
		return nil
	}
	rn.Status = status
	if ghID != nil {
		rn.GitHubRunnerID = ghID
	}
	if ref != "" {
		rn.ProviderRef = ref
	}
	if jobID != nil {
		rn.CurrentJobID = jobID
	}
	return nil
}

func (m *memRunnerRepo) Heartbeat(context.Context, uuid.UUID) error { return nil }
func (m *memRunnerRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Runner, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rn, ok := m.runners[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *rn
	return &cp, nil
}
func (m *memRunnerRepo) GetByName(_ context.Context, name string) (*domain.Runner, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rn := range m.runners {
		if rn.Name == name {
			cp := *rn
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (m *memRunnerRepo) List(context.Context, domain.RunnerFilter) ([]domain.Runner, int, error) {
	return nil, 0, nil
}
func (m *memRunnerRepo) ListActiveByProvider(context.Context, string) ([]domain.Runner, error) {
	return nil, nil
}
func (m *memRunnerRepo) CountActiveInPool(_ context.Context, poolID uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cnt := 0
	for _, rn := range m.runners {
		if rn.PoolID != nil && *rn.PoolID == poolID {
			switch rn.Status {
			case domain.RunnerProvisioning, domain.RunnerOnline, domain.RunnerBusy, domain.RunnerDraining:
				cnt++
			}
		}
	}
	return cnt, nil
}
func (m *memRunnerRepo) FindStaleRunners(context.Context, time.Time, time.Time) ([]domain.Runner, error) {
	return nil, nil
}

func (m *memRunnerRepo) terminateOldestActive() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rn := range m.runners {
		if rn.Status == domain.RunnerProvisioning || rn.Status == domain.RunnerOnline || rn.Status == domain.RunnerBusy {
			rn.Status = domain.RunnerTerminated
			return
		}
	}
}

type memJobRepo struct {
	mu   sync.Mutex
	jobs map[uuid.UUID]*domain.WorkflowJob
}

func newMemJobRepo() *memJobRepo {
	return &memJobRepo{jobs: make(map[uuid.UUID]*domain.WorkflowJob)}
}

func (m *memJobRepo) add(j domain.WorkflowJob) *domain.WorkflowJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	cp := j
	m.jobs[cp.ID] = &cp
	out := cp
	return &out
}

func (m *memJobRepo) UpsertFromWebhook(_ context.Context, j *domain.WorkflowJob) error {
	m.add(*j)
	return nil
}
func (m *memJobRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.WorkflowJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *j
	return &cp, nil
}
func (m *memJobRepo) GetByGitHubJobID(_ context.Context, ghID int64) (*domain.WorkflowJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.GitHubJobID == ghID {
			cp := *j
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (m *memJobRepo) List(context.Context, domain.JobFilter) ([]domain.WorkflowJob, int, error) {
	return nil, 0, nil
}
func (m *memJobRepo) ListDispatchableQueued(_ context.Context, limit int) ([]domain.WorkflowJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.WorkflowJob
	for _, j := range m.jobs {
		if j.Status == domain.JobQueued && (j.ModerationDecision == string(domain.ActionAllow) || j.ModerationDecision == "approved") {
			out = append(out, *j)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].QueuedAt.Before(out[j].QueuedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (m *memJobRepo) CountActiveForRepo(context.Context, string) (int, error) { return 0, nil }
func (m *memJobRepo) ClaimForProvisioning(_ context.Context, id uuid.UUID, poolID, runnerID *uuid.UUID, reason string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok || j.Status != domain.JobQueued {
		return false, nil
	}
	j.Status = domain.JobProvisioning
	j.ModerationReason = reason
	if poolID != nil {
		j.MatchedPoolID = poolID
	}
	if runnerID != nil {
		j.AssignedRunnerID = runnerID
	}
	return true, nil
}
func (m *memJobRepo) RequeueIfProvisioning(_ context.Context, id uuid.UUID, reason string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok || (j.Status != domain.JobQueued && j.Status != domain.JobProvisioning) {
		return false, nil
	}
	j.Status = domain.JobQueued
	j.AssignedRunnerID = nil
	j.ModerationReason = reason
	return true, nil
}
func (m *memJobRepo) FindOrphanedProvisioningJobs(context.Context, time.Time, int) ([]domain.WorkflowJob, error) {
	return nil, nil
}
func (m *memJobRepo) UpdateModerationAndRunner(_ context.Context, id uuid.UUID, status domain.JobStatus, decision, reason string, modBy, poolID, runnerID *uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return domain.ErrNotFound
	}
	j.Status = status
	j.ModerationDecision = decision
	j.ModerationReason = reason
	if poolID != nil {
		j.MatchedPoolID = poolID
	}
	if runnerID != nil {
		j.AssignedRunnerID = runnerID
	}
	return nil
}
func (m *memJobRepo) UpdateLifecycle(_ context.Context, ghID int64, status domain.JobStatus, conclusion string, startedAt, completedAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.GitHubJobID == ghID {
			j.Status = status
			j.Conclusion = conclusion
			j.StartedAt = startedAt
			j.CompletedAt = completedAt
			return nil
		}
	}
	return domain.ErrNotFound
}

type memAgentRepo struct {
	mu           sync.Mutex
	freeSlots    int
	onlineAgents int
}

func (m *memAgentRepo) Create(context.Context, *domain.Agent) error                      { return nil }
func (m *memAgentRepo) GetByID(context.Context, uuid.UUID) (*domain.Agent, error)       { return nil, domain.ErrNotFound }
func (m *memAgentRepo) GetByTokenHash(context.Context, string) (*domain.Agent, error)   { return nil, domain.ErrNotFound }
func (m *memAgentRepo) List(context.Context, domain.PageParams) ([]domain.Agent, int, error) {
	return nil, 0, nil
}
func (m *memAgentRepo) CountOnline(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.onlineAgents, nil
}
func (m *memAgentRepo) UpdateHeartbeat(context.Context, *domain.Agent) error               { return nil }
func (m *memAgentRepo) UpdateStatus(context.Context, uuid.UUID, domain.AgentStatus) error  { return nil }
func (m *memAgentRepo) UpdateToken(context.Context, uuid.UUID, string, string) error       { return nil }
func (m *memAgentRepo) Delete(context.Context, uuid.UUID) error                            { return nil }
func (m *memAgentRepo) MarkStaleOffline(context.Context, time.Time) (int64, error)         { return 0, nil }
func (m *memAgentRepo) CreateTask(context.Context, *domain.AgentTask) error                { return nil }
func (m *memAgentRepo) GetTaskByID(context.Context, uuid.UUID) (*domain.AgentTask, error)  { return nil, domain.ErrNotFound }
func (m *memAgentRepo) ClaimNextTask(context.Context, *domain.Agent) (*domain.AgentTask, error) {
	return nil, nil
}
func (m *memAgentRepo) CompleteTask(context.Context, uuid.UUID, uuid.UUID, domain.AgentTaskStatus, string) (*domain.AgentTask, error) {
	return nil, nil
}
func (m *memAgentRepo) CancelTasksByRunner(context.Context, uuid.UUID) error                  { return nil }
func (m *memAgentRepo) CancelTaskByID(context.Context, uuid.UUID) error                       { return nil }
func (m *memAgentRepo) ListCancelledTaskIDsForAgent(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (m *memAgentRepo) ExpirePendingTasks(context.Context, time.Time) ([]domain.AgentTask, error) {
	return nil, nil
}
func (m *memAgentRepo) CountAvailableSlotsForPool(context.Context, domain.RunnerPool) (int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.freeSlots, m.onlineAgents, nil
}
func (m *memAgentRepo) FailTasksForOfflineAgents(context.Context) ([]domain.AgentTask, error) {
	return nil, nil
}

type noopAuditRepo struct{}

func (noopAuditRepo) InsertDeliveryIfAbsent(context.Context, *domain.WebhookDelivery) (bool, error) {
	return true, nil
}
func (noopAuditRepo) UpdateDeliveryStatus(context.Context, string, string, string, int) error {
	return nil
}
func (noopAuditRepo) ListDeliveries(context.Context, domain.PageParams) ([]domain.WebhookDelivery, int, error) {
	return nil, 0, nil
}
func (noopAuditRepo) AppendAuditLog(context.Context, *domain.AuditLog) error { return nil }
func (noopAuditRepo) ListAuditLogs(context.Context, domain.PageParams) ([]domain.AuditLog, int, error) {
	return nil, 0, nil
}
func (noopAuditRepo) GetDashboardOverview(context.Context) (*domain.DashboardOverview, error) {
	return &domain.DashboardOverview{}, nil
}

func TestJobDispatcherPoolConcurrencyBacklogAndDrain(t *testing.T) {
	poolID := uuid.New()
	pool := domain.RunnerPool{
		ID:             poolID,
		Name:           "Default 4vCPU",
		Slug:           "runway-ubuntu-4vcpu",
		MaxConcurrency: 1,
		Provider:       "simulated",
		Enabled:        true,
	}
	poolRepo := &memPoolRepo{pools: []domain.RunnerPool{pool}}
	runnerRepo := newMemRunnerRepo()
	jobRepo := newMemJobRepo()
	auditRepo := noopAuditRepo{}
	cfg := &config.Config{
		Orchestrator: config.OrchestratorConfig{
			Provider:     "simulated",
			WorkerCount:  1,
			QueueSize:    16,
		},
	}
	orch := orchestrator.NewOrchestrator(cfg.Orchestrator, zap.NewNop(), runnerRepo, jobRepo, auditRepo, nil, nil)
	d := NewJobDispatcher(cfg, zap.NewNop(), nil, nil, poolRepo, jobRepo, runnerRepo, nil, auditRepo, orch)

	base := time.Now().UTC()
	j1 := jobRepo.add(domain.WorkflowJob{GitHubJobID: 101, Status: domain.JobQueued, ModerationDecision: "allow", MatchedPoolID: &poolID, QueuedAt: base})
	j2 := jobRepo.add(domain.WorkflowJob{GitHubJobID: 102, Status: domain.JobQueued, ModerationDecision: "allow", MatchedPoolID: &poolID, QueuedAt: base.Add(time.Second)})
	j3 := jobRepo.add(domain.WorkflowJob{GitHubJobID: 103, Status: domain.JobQueued, ModerationDecision: "allow", MatchedPoolID: &poolID, QueuedAt: base.Add(2 * time.Second)})

	ctx := context.Background()
	if err := d.DispatchJob(ctx, j1, pool, ""); err != nil {
		t.Fatalf("expected 1st job to dispatch cleanly, got %v", err)
	}
	if err := d.DispatchJob(ctx, j2, pool, ""); !errors.Is(err, domain.ErrCapacityFull) {
		t.Fatalf("expected 2nd job to return ErrCapacityFull, got %v", err)
	}
	if err := d.DispatchJob(ctx, j3, pool, ""); !errors.Is(err, domain.ErrCapacityFull) {
		t.Fatalf("expected 3rd job to return ErrCapacityFull, got %v", err)
	}
	if len(runnerRepo.runners) != 1 {
		t.Fatalf("expected only 1 runner created while at capacity, got %d", len(runnerRepo.runners))
	}

	// Free slot 1 and drain -> j2 should dispatch (and j3 remains queued).
	runnerRepo.terminateOldestActive()
	d.drainQueuedOnce(ctx)

	got2, _ := jobRepo.GetByID(ctx, j2.ID)
	got3, _ := jobRepo.GetByID(ctx, j3.ID)
	if got2.Status != domain.JobProvisioning || got3.Status != domain.JobQueued {
		t.Fatalf("expected j2=provisioning and j3=queued, got j2=%s j3=%s", got2.Status, got3.Status)
	}

	// Free slot 2 and drain -> j3 should dispatch.
	runnerRepo.terminateOldestActive()
	d.drainQueuedOnce(ctx)

	got3, _ = jobRepo.GetByID(ctx, j3.ID)
	if got3.Status != domain.JobProvisioning {
		t.Fatalf("expected j3=provisioning after 2nd drain, got %s", got3.Status)
	}
}

func TestJobDispatcherAgentCapacityGate(t *testing.T) {
	poolID := uuid.New()
	pool := domain.RunnerPool{
		ID:             poolID,
		Name:           "Agent Pool",
		Slug:           "runway-agent-pool",
		MaxConcurrency: 10,
		Provider:       "agent",
		Enabled:        true,
	}
	runnerRepo := newMemRunnerRepo()
	jobRepo := newMemJobRepo()
	agentRepo := &memAgentRepo{freeSlots: 2, onlineAgents: 1}
	poolRepo := &memPoolRepo{pools: []domain.RunnerPool{pool}}
	auditRepo := noopAuditRepo{}
	cfg := &config.Config{Orchestrator: config.OrchestratorConfig{Provider: "agent", WorkerCount: 1, QueueSize: 16}}
	orch := orchestrator.NewOrchestrator(cfg.Orchestrator, zap.NewNop(), runnerRepo, jobRepo, auditRepo, agentRepo, nil)
	d := NewJobDispatcher(cfg, zap.NewNop(), nil, nil, poolRepo, jobRepo, runnerRepo, agentRepo, auditRepo, orch)

	ctx := context.Background()
	for i := 1; i <= 2; i++ {
		j := jobRepo.add(domain.WorkflowJob{GitHubJobID: int64(200 + i), Status: domain.JobQueued, ModerationDecision: "allow", MatchedPoolID: &poolID, QueuedAt: time.Now()})
		agentRepo.mu.Lock()
		agentRepo.freeSlots = 3 - i
		agentRepo.mu.Unlock()
		if err := d.DispatchJob(ctx, j, pool, ""); err != nil {
			t.Fatalf("job %d failed unexpectedly: %v", i, err)
		}
	}

	agentRepo.mu.Lock()
	agentRepo.freeSlots = 0
	agentRepo.mu.Unlock()
	j3 := jobRepo.add(domain.WorkflowJob{GitHubJobID: 203, Status: domain.JobQueued, ModerationDecision: "allow", MatchedPoolID: &poolID, QueuedAt: time.Now()})
	if err := d.DispatchJob(ctx, j3, pool, ""); !errors.Is(err, domain.ErrCapacityFull) {
		t.Fatalf("expected ErrCapacityFull when agent freeSlots=0, got %v", err)
	}
	if len(runnerRepo.runners) != 2 {
		t.Fatalf("expected 2 runners created without exhausting agent slots, got %d", len(runnerRepo.runners))
	}

	// Simulate an agent finishing a job and freeing 1 slot.
	agentRepo.mu.Lock()
	agentRepo.freeSlots = 1
	agentRepo.mu.Unlock()
	d.drainQueuedOnce(ctx)

	got3, _ := jobRepo.GetByID(ctx, j3.ID)
	if got3.Status != domain.JobProvisioning {
		t.Fatalf("expected j3=provisioning after agent slot freed, got %s", got3.Status)
	}
}

func TestJobDispatcherConcurrentBurstRespectsConcurrency(t *testing.T) {
	poolID := uuid.New()
	pool := domain.RunnerPool{
		ID:             poolID,
		Name:           "Burst Pool",
		Slug:           "burst",
		MaxConcurrency: 2,
		Provider:       "simulated",
		Enabled:        true,
	}
	runnerRepo := newMemRunnerRepo()
	jobRepo := newMemJobRepo()
	auditRepo := noopAuditRepo{}
	cfg := &config.Config{Orchestrator: config.OrchestratorConfig{Provider: "simulated", WorkerCount: 1, QueueSize: 32}}
	orch := orchestrator.NewOrchestrator(cfg.Orchestrator, zap.NewNop(), runnerRepo, jobRepo, auditRepo, nil, nil)
	d := NewJobDispatcher(cfg, zap.NewNop(), nil, nil, &memPoolRepo{pools: []domain.RunnerPool{pool}}, jobRepo, runnerRepo, nil, auditRepo, orch)

	const total = 8
	jobs := make([]*domain.WorkflowJob, total)
	for i := 0; i < total; i++ {
		jobs[i] = jobRepo.add(domain.WorkflowJob{GitHubJobID: int64(300 + i), Status: domain.JobQueued, ModerationDecision: "allow", MatchedPoolID: &poolID, QueuedAt: time.Now()})
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	capFull := 0
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(job *domain.WorkflowJob) {
			defer wg.Done()
			err := d.DispatchJob(context.Background(), job, pool, "")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				succeeded++
			} else if errors.Is(err, domain.ErrCapacityFull) {
				capFull++
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}(jobs[i])
	}
	wg.Wait()

	if succeeded != 2 || capFull != total-2 {
		t.Fatalf("expected 2 succeeded and %d ErrCapacityFull, got succeeded=%d capFull=%d", total-2, succeeded, capFull)
	}
	if len(runnerRepo.runners) != 2 {
		t.Fatalf("expected 2 runner records total, got %d", len(runnerRepo.runners))
	}
}

func TestJobDispatcherSkipsCompletedRemoteJob(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa generate: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/app/installations/42/access_tokens":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      "ghs_test_token",
				"expires_at": time.Now().Add(time.Hour).UTC(),
			})
		case "/repos/cubetiqlabs/runway/actions/jobs/999":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           999,
				"run_id":       10,
				"status":       "completed",
				"conclusion":   "cancelled",
				"completed_at": time.Now().UTC(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	auth, err := github.NewAppAuth(123, string(pemBytes), "", srv.URL, nil)
	if err != nil {
		t.Fatalf("NewAppAuth: %v", err)
	}
	ghClient := github.NewClient(auth, srv.URL)

	poolID := uuid.New()
	instID := int64(42)
	pool := domain.RunnerPool{ID: poolID, Name: "Default", Slug: "default", MaxConcurrency: 2, Provider: "simulated", Enabled: true}
	runnerRepo := newMemRunnerRepo()
	jobRepo := newMemJobRepo()
	auditRepo := noopAuditRepo{}
	cfg := &config.Config{Orchestrator: config.OrchestratorConfig{Provider: "simulated", WorkerCount: 1, QueueSize: 8}}
	orch := orchestrator.NewOrchestrator(cfg.Orchestrator, zap.NewNop(), runnerRepo, jobRepo, auditRepo, nil, nil)
	d := NewJobDispatcher(cfg, zap.NewNop(), ghClient, nil, &memPoolRepo{pools: []domain.RunnerPool{pool}}, jobRepo, runnerRepo, nil, auditRepo, orch)

	j := jobRepo.add(domain.WorkflowJob{
		GitHubJobID:        999,
		InstallationID:     &instID,
		RepoFullName:       "cubetiqlabs/runway",
		Status:             domain.JobQueued,
		ModerationDecision: "allow",
		MatchedPoolID:      &poolID,
		QueuedAt:           time.Now().Add(-time.Minute),
	})

	d.drainQueuedOnce(context.Background())

	got, _ := jobRepo.GetByID(context.Background(), j.ID)
	if got.Status != domain.JobCancelled || got.Conclusion != "cancelled" {
		t.Fatalf("expected completed remote job to be marked cancelled locally, got status=%s conclusion=%s", got.Status, got.Conclusion)
	}
	if len(runnerRepo.runners) != 0 {
		t.Fatalf("expected 0 runners provisioned for already-completed job, got %d", len(runnerRepo.runners))
	}
}
