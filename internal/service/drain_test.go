package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type drainAgentRepo struct {
	mu     sync.Mutex
	agents map[uuid.UUID]*domain.Agent
	tasks  map[uuid.UUID]*domain.AgentTask
}

func newDrainAgentRepo() *drainAgentRepo {
	return &drainAgentRepo{
		agents: make(map[uuid.UUID]*domain.Agent),
		tasks:  make(map[uuid.UUID]*domain.AgentTask),
	}
}

func (r *drainAgentRepo) Create(context.Context, *domain.Agent) error { return nil }
func (r *drainAgentRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Agent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ag, ok := r.agents[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *ag
	return &cp, nil
}
func (r *drainAgentRepo) GetByTokenHash(context.Context, string) (*domain.Agent, error) {
	return nil, domain.ErrNotFound
}
func (r *drainAgentRepo) List(context.Context, domain.AgentFilter) ([]domain.Agent, int, error) {
	return nil, 0, nil
}
func (r *drainAgentRepo) ListByStatus(context.Context, domain.AgentStatus) ([]domain.Agent, error) {
	return nil, nil
}
func (r *drainAgentRepo) CountOnline(context.Context) (int, error)                { return 1, nil }
func (r *drainAgentRepo) UpdateHeartbeat(context.Context, *domain.Agent) error    { return nil }
func (r *drainAgentRepo) UpdateStatus(_ context.Context, id uuid.UUID, status domain.AgentStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ag, ok := r.agents[id]
	if !ok {
		return domain.ErrNotFound
	}
	ag.Status = status
	return nil
}
func (r *drainAgentRepo) UpdateToken(context.Context, uuid.UUID, string, string) error { return nil }
func (r *drainAgentRepo) Delete(context.Context, uuid.UUID) error                      { return nil }
func (r *drainAgentRepo) MarkStaleOffline(context.Context, time.Time) (int64, error)   { return 0, nil }
func (r *drainAgentRepo) CountActiveTasksByAgent(_ context.Context, agentID uuid.UUID) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cnt := 0
	for _, t := range r.tasks {
		if t.AgentID != nil && *t.AgentID == agentID && (t.Status == domain.AgentTaskClaimed || t.Status == domain.AgentTaskRunning) {
			cnt++
		}
	}
	return cnt, nil
}
func (r *drainAgentRepo) MarkDrainingCompleteIfIdle(_ context.Context, agentID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ag, ok := r.agents[agentID]
	if !ok || ag.Status != domain.AgentDraining {
		return false, nil
	}
	for _, t := range r.tasks {
		if t.AgentID != nil && *t.AgentID == agentID && (t.Status == domain.AgentTaskClaimed || t.Status == domain.AgentTaskRunning) {
			return false, nil
		}
	}
	ag.Status = domain.AgentDrained
	ag.ActiveJobs = 0
	return true, nil
}
func (r *drainAgentRepo) ListTasksByAgent(context.Context, uuid.UUID, domain.AgentTaskFilter) ([]domain.AgentTask, int, error) {
	return nil, 0, nil
}
func (r *drainAgentRepo) GetAgentTaskStats(context.Context, uuid.UUID) (*domain.AgentTaskStats, error) {
	return &domain.AgentTaskStats{}, nil
}
func (r *drainAgentRepo) CreateTask(context.Context, *domain.AgentTask) error { return nil }
func (r *drainAgentRepo) GetTaskByID(_ context.Context, id uuid.UUID) (*domain.AgentTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *t
	return &cp, nil
}
func (r *drainAgentRepo) ClaimNextTask(context.Context, *domain.Agent) (*domain.AgentTask, error) {
	return nil, nil
}
func (r *drainAgentRepo) CompleteTask(_ context.Context, taskID, agentID uuid.UUID, status domain.AgentTaskStatus, errMsg string) (*domain.AgentTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[taskID]
	if !ok || t.AgentID == nil || *t.AgentID != agentID {
		return nil, domain.ErrNotFound
	}
	t.Status = status
	t.ErrorMessage = errMsg
	cp := *t
	return &cp, nil
}
func (r *drainAgentRepo) CancelTasksByRunner(context.Context, uuid.UUID) error { return nil }
func (r *drainAgentRepo) CancelTaskByID(context.Context, uuid.UUID) error      { return nil }
func (r *drainAgentRepo) ListCancelledTaskIDsForAgent(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (r *drainAgentRepo) ExpirePendingTasks(context.Context, time.Time) ([]domain.AgentTask, error) {
	return nil, nil
}
func (r *drainAgentRepo) CountAvailableSlotsForPool(context.Context, domain.RunnerPool) (int, int, error) {
	return 1, 1, nil
}
func (r *drainAgentRepo) FailTasksForOfflineAgents(context.Context) ([]domain.AgentTask, error) {
	return nil, nil
}

func TestRunnerAndAgentDrainLifecycle(t *testing.T) {
	ctx := context.Background()
	runnerRepo := newMemRunnerRepo()
	jobRepo := newMemJobRepo()
	agentRepo := newDrainAgentRepo()
	auditRepo := noopAuditRepo{}
	cfg := &config.Config{Orchestrator: config.OrchestratorConfig{Provider: "simulated", WorkerCount: 1, QueueSize: 8}}
	orch := orchestrator.NewOrchestrator(cfg.Orchestrator, zap.NewNop(), runnerRepo, jobRepo, auditRepo, agentRepo, nil)

	runnerSvc := NewRunnerService(cfg, nil, nil, nil, runnerRepo, auditRepo, orch)
	runnerSvc.SetJobRepo(jobRepo)

	// 1. Idle runner drains directly to terminated.
	idleRunner := &domain.Runner{Name: "r-idle", Provider: "simulated", Status: domain.RunnerOnline, Ephemeral: true}
	_ = runnerRepo.Create(ctx, idleRunner)
	gotIdle, err := runnerSvc.DrainRunner(ctx, idleRunner.ID, nil, "127.0.0.1")
	if err != nil {
		t.Fatalf("DrainRunner idle failed: %v", err)
	}
	if gotIdle.Status != domain.RunnerTerminated {
		t.Fatalf("expected idle runner to terminate immediately on Drain, got %s", gotIdle.Status)
	}

	// 2. Busy runner enters draining while job is in_progress.
	ghJobID := int64(5050)
	jobRepo.add(domain.WorkflowJob{GitHubJobID: ghJobID, Status: domain.JobInProgress})
	busyRunner := &domain.Runner{Name: "r-busy", Provider: "simulated", Status: domain.RunnerBusy, CurrentJobID: &ghJobID, Ephemeral: true}
	_ = runnerRepo.Create(ctx, busyRunner)
	gotBusy, err := runnerSvc.DrainRunner(ctx, busyRunner.ID, nil, "127.0.0.1")
	if err != nil {
		t.Fatalf("DrainRunner busy failed: %v", err)
	}
	if gotBusy.Status != domain.RunnerDraining {
		t.Fatalf("expected busy runner to enter draining, got %s", gotBusy.Status)
	}

	// 3. Idle agent drains directly to drained; ResumeAgent restores online when heartbeat is recent.
	agentSvc := NewAgentService(zap.NewNop(), agentRepo, runnerRepo, jobRepo, auditRepo, nil, nil)
	now := time.Now().UTC()
	idleAgentID := uuid.New()
	agentRepo.agents[idleAgentID] = &domain.Agent{ID: idleAgentID, Name: "ag-idle", Status: domain.AgentOnline, LastHeartbeatAt: &now}
	agOut, err := agentSvc.DrainAgent(ctx, idleAgentID, nil, "127.0.0.1")
	if err != nil {
		t.Fatalf("DrainAgent idle failed: %v", err)
	}
	if agOut.Status != domain.AgentDrained {
		t.Fatalf("expected idle agent to transition directly to drained, got %s", agOut.Status)
	}
	resumed, err := agentSvc.ResumeAgent(ctx, idleAgentID, nil, "127.0.0.1")
	if err != nil || resumed.Status != domain.AgentOnline {
		t.Fatalf("expected ResumeAgent to restore online, got %+v err=%v", resumed, err)
	}

	// 4. Busy agent enters draining, then auto-transitions to drained when its last task completes.
	busyAgentID := uuid.New()
	taskID := uuid.New()
	taskRunner := &domain.Runner{Name: "r-task", Provider: "agent", Status: domain.RunnerBusy, Ephemeral: true}
	_ = runnerRepo.Create(ctx, taskRunner)
	agentRepo.agents[busyAgentID] = &domain.Agent{ID: busyAgentID, Name: "ag-busy", Status: domain.AgentOnline, ActiveJobs: 1, LastHeartbeatAt: &now}
	agentRepo.tasks[taskID] = &domain.AgentTask{ID: taskID, AgentID: &busyAgentID, RunnerID: taskRunner.ID, Status: domain.AgentTaskRunning}

	agBusy, err := agentSvc.DrainAgent(ctx, busyAgentID, nil, "127.0.0.1")
	if err != nil || agBusy.Status != domain.AgentDraining {
		t.Fatalf("expected busy agent to enter draining, got %+v err=%v", agBusy, err)
	}
	if err := agentSvc.CompleteTask(ctx, agBusy, taskID, domain.AgentTaskCompleted, ""); err != nil {
		t.Fatalf("CompleteTask failed: %v", err)
	}
	finalAg, _ := agentRepo.GetByID(ctx, busyAgentID)
	if finalAg.Status != domain.AgentDrained {
		t.Fatalf("expected draining agent to become drained after final task completion, got %s", finalAg.Status)
	}
}
