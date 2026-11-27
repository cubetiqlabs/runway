package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type ProvisionRequest struct {
	RunnerID         uuid.UUID
	JobID            uuid.UUID
	RunnerName       string
	Pool             domain.RunnerPool
	EncodedJITConfig string
	GitHubJobID      int64
	RepoFullName     string
}

type ProvisionResult struct {
	ProviderRef   string
	InitialStatus domain.RunnerStatus
	HostInfo      json.RawMessage
}

type Provisioner interface {
	Name() string
	Ping(ctx context.Context) error
	Provision(ctx context.Context, req ProvisionRequest) (*ProvisionResult, error)
	Terminate(ctx context.Context, providerRef string) error
	Healthy(ctx context.Context, providerRef string) (bool, error)
}

type dispatchTask struct {
	job              *domain.WorkflowJob
	pool             domain.RunnerPool
	runnerName       string
	encodedJITConfig string
	githubRunnerID   *int64
}

// Orchestrator manages a bounded worker pool for runner provisioning and periodic reconciliation.
type Orchestrator struct {
	cfg          config.OrchestratorConfig
	log          *zap.Logger
	runnerRepo   domain.RunnerRepository
	jobRepo      domain.JobRepository
	auditRepo    domain.AuditRepository
	agentRepo    domain.AgentRepository
	provisioners map[string]Provisioner

	queue chan dispatchTask
	wg    sync.WaitGroup
}

func NewOrchestrator(
	cfg config.OrchestratorConfig,
	log *zap.Logger,
	runnerRepo domain.RunnerRepository,
	jobRepo domain.JobRepository,
	auditRepo domain.AuditRepository,
	agentRepo domain.AgentRepository,
	encKey []byte,
) *Orchestrator {
	provs := map[string]Provisioner{
		"simulated": NewSimulatedProvisioner(),
		"docker":    NewDockerProvisioner(cfg.DockerSocket),
	}
	if agentRepo != nil {
		provs["agent"] = NewAgentProvisioner(agentRepo, encKey)
	}
	return &Orchestrator{
		cfg:          cfg,
		log:          log,
		runnerRepo:   runnerRepo,
		jobRepo:      jobRepo,
		auditRepo:    auditRepo,
		agentRepo:    agentRepo,
		provisioners: provs,
		queue:        make(chan dispatchTask, cfg.QueueSize),
	}
}

func (o *Orchestrator) Start(ctx context.Context) {
	for i := 0; i < o.cfg.WorkerCount; i++ {
		o.wg.Add(1)
		go o.worker(ctx, i)
	}
	o.wg.Add(1)
	go o.reconcileLoop(ctx)
}

func (o *Orchestrator) Wait() {
	o.wg.Wait()
}

func (o *Orchestrator) HealthCheck(ctx context.Context) error {
	return o.selectProvisioner(o.cfg.Provider).Ping(ctx)
}

func (o *Orchestrator) EnqueueProvision(job *domain.WorkflowJob, pool domain.RunnerPool, runnerName, encodedJITConfig string, githubRunnerID *int64) bool {
	select {
	case o.queue <- dispatchTask{
		job:              job,
		pool:             pool,
		runnerName:       runnerName,
		encodedJITConfig: encodedJITConfig,
		githubRunnerID:   githubRunnerID,
	}:
		return true
	default:
		o.log.Warn("orchestrator queue full; dropping provision task", zap.Int64("github_job_id", job.GitHubJobID))
		return false
	}
}

func (o *Orchestrator) TerminateRunner(ctx context.Context, runner *domain.Runner) error {
	prov := o.selectProvisioner(runner.Provider)
	if runner.ProviderRef != "" {
		if err := prov.Terminate(ctx, runner.ProviderRef); err != nil {
			o.log.Warn("provisioner terminate error", zap.String("runner", runner.Name), zap.Error(err))
		}
	}
	return o.runnerRepo.UpdateStatus(ctx, runner.ID, domain.RunnerTerminated, nil, "", nil)
}

func (o *Orchestrator) worker(ctx context.Context, workerID int) {
	defer o.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-o.queue:
			o.handleTask(ctx, workerID, task)
		}
	}
}

func (o *Orchestrator) handleTask(ctx context.Context, workerID int, task dispatchTask) {
	provCtx, cancel := context.WithTimeout(ctx, o.cfg.ProvisionTimeout)
	defer cancel()

	runnerName := task.runnerName
	if runnerName == "" {
		runnerName = fmt.Sprintf("runway-%s-%s", task.pool.Slug, uuid.NewString()[:8])
	}
	providerName := task.pool.Provider
	if providerName == "" {
		providerName = o.cfg.Provider
	}
	prov := o.selectProvisioner(providerName)

	now := time.Now().UTC()
	rn := &domain.Runner{
		PoolID:          &task.pool.ID,
		InstallationID:  task.job.InstallationID,
		GitHubRunnerID:  task.githubRunnerID,
		Name:            runnerName,
		Provider:        prov.Name(),
		Labels:          task.pool.Labels,
		Status:          domain.RunnerProvisioning,
		Ephemeral:       true,
		CurrentJobID:    &task.job.GitHubJobID,
		LastHeartbeatAt: &now,
	}
	if err := o.runnerRepo.Create(provCtx, rn); err != nil {
		o.log.Error("create runner record failed", zap.Int("worker", workerID), zap.Error(err))
		return
	}

	_ = o.jobRepo.UpdateModerationAndRunner(provCtx, task.job.ID, domain.JobProvisioning, task.job.ModerationDecision, task.job.ModerationReason, task.job.ModeratedBy, &task.pool.ID, &rn.ID)

	res, err := prov.Provision(provCtx, ProvisionRequest{
		RunnerID:         rn.ID,
		JobID:            task.job.ID,
		RunnerName:       runnerName,
		Pool:             task.pool,
		EncodedJITConfig: task.encodedJITConfig,
		GitHubJobID:      task.job.GitHubJobID,
		RepoFullName:     task.job.RepoFullName,
	})
	if err != nil {
		o.log.Error("runner provisioning failed", zap.String("runner", runnerName), zap.Error(err))
		_ = o.runnerRepo.UpdateStatus(provCtx, rn.ID, domain.RunnerOffline, nil, "", nil)
		if o.jobRepo != nil {
			_ = o.jobRepo.UpdateModerationAndRunner(provCtx, task.job.ID, domain.JobQueued, task.job.ModerationDecision, "Provisioning failed: "+err.Error(), task.job.ModeratedBy, &task.pool.ID, &rn.ID)
		}
		if o.auditRepo != nil {
			meta, _ := json.Marshal(map[string]any{
				"runner":        runnerName,
				"provider":      prov.Name(),
				"github_job_id": task.job.GitHubJobID,
				"error":         err.Error(),
			})
			_ = o.auditRepo.AppendAuditLog(provCtx, &domain.AuditLog{
				ActorLabel:   "orchestrator",
				Action:       "runner.provision_failed",
				ResourceType: "runner",
				ResourceID:   rn.ID.String(),
				Metadata:     meta,
			})
		}
		return
	}

	initialStatus := res.InitialStatus
	if initialStatus == "" {
		initialStatus = domain.RunnerOnline
	}
	_ = o.runnerRepo.UpdateStatus(provCtx, rn.ID, initialStatus, task.githubRunnerID, res.ProviderRef, &task.job.GitHubJobID)
	_ = o.auditRepo.AppendAuditLog(provCtx, &domain.AuditLog{
		ActorLabel:   "orchestrator",
		Action:       "runner.provisioned",
		ResourceType: "runner",
		ResourceID:   rn.ID.String(),
		Metadata:     res.HostInfo,
	})
}

func (o *Orchestrator) reconcileLoop(ctx context.Context) {
	defer o.wg.Done()
	ticker := time.NewTicker(o.cfg.ReconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.reconcileOnce(ctx)
		}
	}
}

func (o *Orchestrator) reconcileOnce(ctx context.Context) {
	now := time.Now().UTC()
	seen := make(map[uuid.UUID]struct{})

	stale, err := o.runnerRepo.FindStaleRunners(
		ctx,
		now.Add(-o.cfg.ProvisionTimeout),
		now.Add(-o.cfg.StaleHeartbeatTTL),
	)
	if err != nil {
		o.log.Warn("reconcile query stale runners failed", zap.Error(err))
	} else {
		for _, rn := range stale {
			seen[rn.ID] = struct{}{}
			prov := o.selectProvisioner(rn.Provider)
			healthy, healthErr := prov.Healthy(ctx, rn.ProviderRef)
			if healthy {
				_ = o.runnerRepo.Heartbeat(ctx, rn.ID)
				continue
			}
			if healthErr != nil {
				o.log.Warn("reconcile health check error", zap.String("runner", rn.Name), zap.Error(healthErr))
				continue
			}
			_ = o.runnerRepo.UpdateStatus(ctx, rn.ID, domain.RunnerOffline, nil, "", nil)
			o.log.Info("marked stale runner offline", zap.String("runner", rn.Name))
		}
	}

	// Promptly detect exited ephemeral Docker containers so concurrency slots recover before StaleHeartbeatTTL.
	dockerActive, err := o.runnerRepo.ListActiveByProvider(ctx, "docker")
	if err != nil {
		o.log.Warn("reconcile query active docker runners failed", zap.Error(err))
		return
	}
	dockerProv := o.selectProvisioner("docker")
	for _, rn := range dockerActive {
		if _, already := seen[rn.ID]; already {
			continue
		}
		healthy, healthErr := dockerProv.Healthy(ctx, rn.ProviderRef)
		if healthErr != nil || healthy {
			continue
		}
		_ = o.runnerRepo.UpdateStatus(ctx, rn.ID, domain.RunnerTerminated, nil, "", nil)
		o.log.Info("reconciled exited docker runner", zap.String("runner", rn.Name))
	}

	if o.agentRepo != nil {
		if marked, err := o.agentRepo.MarkStaleOffline(ctx, now.Add(-2*time.Minute)); err != nil {
			o.log.Warn("reconcile mark stale agents failed", zap.Error(err))
		} else if marked > 0 {
			o.log.Info("marked stale agents offline", zap.Int64("count", marked))
		}

		expired, err := o.agentRepo.ExpirePendingTasks(ctx, now.Add(-o.cfg.ProvisionTimeout))
		if err != nil {
			o.log.Warn("reconcile expire pending agent tasks failed", zap.Error(err))
		} else {
			for _, task := range expired {
				_ = o.runnerRepo.UpdateStatus(ctx, task.RunnerID, domain.RunnerOffline, nil, "", nil)
				if o.jobRepo != nil {
					if job, getErr := o.jobRepo.GetByID(ctx, task.JobID); getErr == nil {
						_ = o.jobRepo.UpdateModerationAndRunner(ctx, job.ID, domain.JobQueued, job.ModerationDecision, "Provisioning timed out: no online runway-agent claimed the task", job.ModeratedBy, job.MatchedPoolID, &task.RunnerID)
					}
				}
				o.log.Warn("expired unclaimed agent task", zap.String("runner", task.RunnerName), zap.Int64("github_job_id", task.GitHubJobID))
			}
		}
	}
}

func (o *Orchestrator) selectProvisioner(name string) Provisioner {
	if p, ok := o.provisioners[name]; ok {
		return p
	}
	if p, ok := o.provisioners[o.cfg.Provider]; ok {
		return p
	}
	return o.provisioners["simulated"]
}
