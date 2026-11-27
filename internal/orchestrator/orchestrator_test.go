package orchestrator

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type stubRunnerRepo struct {
	mu      sync.Mutex
	runners map[uuid.UUID]*domain.Runner
}

func newStubRunnerRepo() *stubRunnerRepo {
	return &stubRunnerRepo{runners: make(map[uuid.UUID]*domain.Runner)}
}

func (s *stubRunnerRepo) Create(_ context.Context, rn *domain.Runner) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rn.ID == uuid.Nil {
		rn.ID = uuid.New()
	}
	cp := *rn
	s.runners[cp.ID] = &cp
	return nil
}

func (s *stubRunnerRepo) UpdateStatus(_ context.Context, id uuid.UUID, status domain.RunnerStatus, ghID *int64, ref string, jobID *int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rn, ok := s.runners[id]
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

func (s *stubRunnerRepo) SetProvisioned(_ context.Context, id uuid.UUID, status domain.RunnerStatus, ghID *int64, ref string, jobID *int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rn, ok := s.runners[id]
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

func (s *stubRunnerRepo) Heartbeat(context.Context, uuid.UUID) error { return nil }
func (s *stubRunnerRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Runner, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rn, ok := s.runners[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *rn
	return &cp, nil
}
func (s *stubRunnerRepo) GetByName(context.Context, string) (*domain.Runner, error) {
	return nil, domain.ErrNotFound
}
func (s *stubRunnerRepo) List(context.Context, domain.RunnerFilter) ([]domain.Runner, int, error) {
	return nil, 0, nil
}
func (s *stubRunnerRepo) ListActiveByProvider(context.Context, string) ([]domain.Runner, error) {
	return nil, nil
}
func (s *stubRunnerRepo) CountActiveInPool(context.Context, uuid.UUID) (int, error) { return 0, nil }
func (s *stubRunnerRepo) FindStaleRunners(context.Context, time.Time, time.Time) ([]domain.Runner, error) {
	return nil, nil
}

type stubJobRepo struct {
	mu      sync.Mutex
	jobs    map[uuid.UUID]*domain.WorkflowJob
	orphans []domain.WorkflowJob
}

func newStubJobRepo() *stubJobRepo {
	return &stubJobRepo{jobs: make(map[uuid.UUID]*domain.WorkflowJob)}
}

func (s *stubJobRepo) UpsertFromWebhook(_ context.Context, j *domain.WorkflowJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *j
	s.jobs[cp.ID] = &cp
	return nil
}
func (s *stubJobRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.WorkflowJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *j
	return &cp, nil
}
func (s *stubJobRepo) GetByGitHubJobID(_ context.Context, ghID int64) (*domain.WorkflowJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.GitHubJobID == ghID {
			cp := *j
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (s *stubJobRepo) List(context.Context, domain.JobFilter) ([]domain.WorkflowJob, int, error) {
	return nil, 0, nil
}
func (s *stubJobRepo) ListDispatchableQueued(context.Context, int) ([]domain.WorkflowJob, error) {
	return nil, nil
}
func (s *stubJobRepo) CountActiveForRepo(context.Context, string) (int, error) { return 0, nil }
func (s *stubJobRepo) ClaimForProvisioning(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID, string) (bool, error) {
	return true, nil
}
func (s *stubJobRepo) RequeueIfProvisioning(_ context.Context, id uuid.UUID, reason string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok || (j.Status != domain.JobQueued && j.Status != domain.JobProvisioning) {
		return false, nil
	}
	j.Status = domain.JobQueued
	j.AssignedRunnerID = nil
	j.ModerationReason = reason
	return true, nil
}
func (s *stubJobRepo) FindOrphanedProvisioningJobs(context.Context, time.Time, int) ([]domain.WorkflowJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.WorkflowJob(nil), s.orphans...), nil
}
func (s *stubJobRepo) UpdateModerationAndRunner(context.Context, uuid.UUID, domain.JobStatus, string, string, *uuid.UUID, *uuid.UUID, *uuid.UUID) error {
	return nil
}
func (s *stubJobRepo) UpdateLifecycle(context.Context, int64, domain.JobStatus, string, *time.Time, *time.Time) error {
	return nil
}

type stubAgentRepo struct {
	mu           sync.Mutex
	agents       map[uuid.UUID]*domain.Agent
	tasks        map[uuid.UUID]*domain.AgentTask
	failedTasks  []domain.AgentTask
	expiredTasks []domain.AgentTask
}

func newStubAgentRepo() *stubAgentRepo {
	return &stubAgentRepo{
		agents: make(map[uuid.UUID]*domain.Agent),
		tasks:  make(map[uuid.UUID]*domain.AgentTask),
	}
}

func (s *stubAgentRepo) Create(context.Context, *domain.Agent) error { return nil }
func (s *stubAgentRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ag, ok := s.agents[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *ag
	return &cp, nil
}
func (s *stubAgentRepo) GetByTokenHash(context.Context, string) (*domain.Agent, error) {
	return nil, domain.ErrNotFound
}
func (s *stubAgentRepo) List(context.Context, domain.PageParams) ([]domain.Agent, int, error) {
	return nil, 0, nil
}
func (s *stubAgentRepo) CountOnline(context.Context) (int, error)                          { return 1, nil }
func (s *stubAgentRepo) UpdateHeartbeat(context.Context, *domain.Agent) error              { return nil }
func (s *stubAgentRepo) UpdateStatus(context.Context, uuid.UUID, domain.AgentStatus) error { return nil }
func (s *stubAgentRepo) UpdateToken(context.Context, uuid.UUID, string, string) error      { return nil }
func (s *stubAgentRepo) Delete(context.Context, uuid.UUID) error                           { return nil }
func (s *stubAgentRepo) MarkStaleOffline(context.Context, time.Time) (int64, error)        { return 0, nil }
func (s *stubAgentRepo) CreateTask(context.Context, *domain.AgentTask) error               { return nil }
func (s *stubAgentRepo) GetTaskByID(_ context.Context, id uuid.UUID) (*domain.AgentTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *t
	return &cp, nil
}
func (s *stubAgentRepo) ClaimNextTask(context.Context, *domain.Agent) (*domain.AgentTask, error) {
	return nil, nil
}
func (s *stubAgentRepo) CompleteTask(context.Context, uuid.UUID, uuid.UUID, domain.AgentTaskStatus, string) (*domain.AgentTask, error) {
	return nil, nil
}
func (s *stubAgentRepo) CancelTasksByRunner(context.Context, uuid.UUID) error { return nil }
func (s *stubAgentRepo) CancelTaskByID(context.Context, uuid.UUID) error      { return nil }
func (s *stubAgentRepo) ListCancelledTaskIDsForAgent(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (s *stubAgentRepo) ExpirePendingTasks(context.Context, time.Time) ([]domain.AgentTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]domain.AgentTask(nil), s.expiredTasks...)
	s.expiredTasks = nil
	return out, nil
}
func (s *stubAgentRepo) CountAvailableSlotsForPool(context.Context, domain.RunnerPool) (int, int, error) {
	return 1, 1, nil
}
func (s *stubAgentRepo) FailTasksForOfflineAgents(context.Context) ([]domain.AgentTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]domain.AgentTask(nil), s.failedTasks...)
	s.failedTasks = nil
	return out, nil
}

func TestAgentProvisionerHealthyDetectsOfflineAgent(t *testing.T) {
	repo := newStubAgentRepo()
	agentID := uuid.New()
	taskID := uuid.New()
	repo.agents[agentID] = &domain.Agent{ID: agentID, Status: domain.AgentOnline}
	repo.tasks[taskID] = &domain.AgentTask{ID: taskID, AgentID: &agentID, Status: domain.AgentTaskRunning}

	p := NewAgentProvisioner(repo, nil)
	ctx := context.Background()

	healthy, err := p.Healthy(ctx, agentTaskRefPrefix+taskID.String())
	if err != nil || !healthy {
		t.Fatalf("expected healthy=true when agent is online, got healthy=%v err=%v", healthy, err)
	}

	repo.mu.Lock()
	repo.agents[agentID].Status = domain.AgentOffline
	repo.mu.Unlock()

	healthy, err = p.Healthy(ctx, "agent:"+agentID.String()+":"+taskID.String())
	if err != nil || healthy {
		t.Fatalf("expected healthy=false when assigned agent went offline, got healthy=%v err=%v", healthy, err)
	}
}

func TestReconcileRecoversOfflineAgentAndExpiredTasksAndSetProvisioned(t *testing.T) {
	runnerRepo := newStubRunnerRepo()
	jobRepo := newStubJobRepo()
	agentRepo := newStubAgentRepo()

	r1 := &domain.Runner{ID: uuid.New(), Name: "runner-offline-agent", Status: domain.RunnerOnline, Ephemeral: true}
	r2 := &domain.Runner{ID: uuid.New(), Name: "runner-expired-task", Status: domain.RunnerProvisioning, Ephemeral: true}
	r3 := &domain.Runner{ID: uuid.New(), Name: "runner-already-claimed", Status: domain.RunnerOnline, Ephemeral: true}
	runnerRepo.runners[r1.ID] = r1
	runnerRepo.runners[r2.ID] = r2
	runnerRepo.runners[r3.ID] = r3

	j1 := &domain.WorkflowJob{ID: uuid.New(), GitHubJobID: 1, Status: domain.JobProvisioning}
	j2 := &domain.WorkflowJob{ID: uuid.New(), GitHubJobID: 2, Status: domain.JobInProgress} // Already running; must NOT revert to queued.
	j3 := &domain.WorkflowJob{ID: uuid.New(), GitHubJobID: 3, Status: domain.JobProvisioning}
	jobRepo.jobs[j1.ID] = j1
	jobRepo.jobs[j2.ID] = j2
	jobRepo.jobs[j3.ID] = j3

	agentRepo.failedTasks = []domain.AgentTask{
		{ID: uuid.New(), RunnerID: r1.ID, RunnerName: r1.Name, JobID: j1.ID, GitHubJobID: 1},
		{ID: uuid.New(), RunnerID: uuid.New(), RunnerName: "runner-inprogress", JobID: j2.ID, GitHubJobID: 2},
	}
	agentRepo.expiredTasks = []domain.AgentTask{
		{ID: uuid.New(), RunnerID: r2.ID, RunnerName: r2.Name, JobID: j3.ID, GitHubJobID: 3},
	}

	orch := NewOrchestrator(config.OrchestratorConfig{
		Provider:          "agent",
		WorkerCount:       1,
		QueueSize:         8,
		ReconcileInterval: time.Minute,
		ProvisionTimeout:  time.Minute,
		StaleHeartbeatTTL: time.Minute,
	}, zap.NewNop(), runnerRepo, jobRepo, nil, agentRepo, nil)

	var freed atomic.Int32
	orch.SetSlotFreedHook(func() { freed.Add(1) })

	ctx := context.Background()
	orch.reconcileOnce(ctx)

	gotJ1, _ := jobRepo.GetByID(ctx, j1.ID)
	gotJ2, _ := jobRepo.GetByID(ctx, j2.ID)
	gotJ3, _ := jobRepo.GetByID(ctx, j3.ID)
	if gotJ1.Status != domain.JobQueued {
		t.Fatalf("expected j1 requeued after agent went offline, got %s", gotJ1.Status)
	}
	if gotJ2.Status != domain.JobInProgress {
		t.Fatalf("expected in-progress j2 to remain in_progress, got %s", gotJ2.Status)
	}
	if gotJ3.Status != domain.JobQueued {
		t.Fatalf("expected j3 requeued after task expired, got %s", gotJ3.Status)
	}
	if freed.Load() < 2 {
		t.Fatalf("expected slotFreedHook triggered on recovery, got %d", freed.Load())
	}

	// SetProvisioned must not downgrade an already-claimed RunnerOnline back to RunnerProvisioning.
	if err := runnerRepo.SetProvisioned(ctx, r3.ID, domain.RunnerProvisioning, nil, "agent-task:ignored", nil); err != nil {
		t.Fatalf("SetProvisioned error: %v", err)
	}
	gotR3, _ := runnerRepo.GetByID(ctx, r3.ID)
	if gotR3.Status != domain.RunnerOnline {
		t.Fatalf("expected SetProvisioned not to overwrite online runner, got %s", gotR3.Status)
	}
}
