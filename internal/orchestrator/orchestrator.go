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
	runner           *domain.Runner
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

	queue           chan dispatchTask
	wg              sync.WaitGroup
	baseMu          sync.RWMutex
	baseCtx         context.Context
	hookMu          sync.RWMutex
	onSlotFreed     func()
	onRunnerCleanup func(context.Context, *domain.Runner)
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
		"docker": NewDockerProvisionerWithOptions(cfg.DockerSocket, DockerOptions{
			RunnerDockerHost:  cfg.RunnerDockerHost,
			RunnerNetwork:     cfg.RunnerNetwork,
			MountDockerSocket: cfg.RunnerMountDockerSocket,
			RunnerDockerGID:   cfg.RunnerDockerGID,
		}),
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
		baseCtx:      context.Background(),
	}
}

func (o *Orchestrator) Start(ctx context.Context) {
	o.baseMu.Lock()
	o.baseCtx = ctx
	o.baseMu.Unlock()

	for i := 0; i < o.cfg.WorkerCount; i++ {
		o.wg.Add(1)
		go o.worker(ctx, i)
	}
	o.wg.Add(1)
	go o.reconcileLoop(ctx)
}

func (o *Orchestrator) lifecycleCtx() context.Context {
	o.baseMu.RLock()
	ctx := o.baseCtx
	o.baseMu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (o *Orchestrator) Wait() {
	o.wg.Wait()
}

func (o *Orchestrator) HealthCheck(ctx context.Context) error {
	return o.selectProvisioner(o.cfg.Provider).Ping(ctx)
}

func (o *Orchestrator) SetSlotFreedHook(fn func()) {
	o.hookMu.Lock()
	o.onSlotFreed = fn
	o.hookMu.Unlock()
}

func (o *Orchestrator) SetRunnerCleanupHook(fn func(context.Context, *domain.Runner)) {
	o.hookMu.Lock()
	o.onRunnerCleanup = fn
	o.hookMu.Unlock()
}

func (o *Orchestrator) notifySlotFreed() {
	o.hookMu.RLock()
	fn := o.onSlotFreed
	o.hookMu.RUnlock()
	if fn != nil {
		fn()
	}
}

func (o *Orchestrator) cleanupGitHubRunner(ctx context.Context, rn *domain.Runner) {
	if rn == nil || rn.GitHubRunnerID == nil {
		return
	}
	o.hookMu.RLock()
	fn := o.onRunnerCleanup
	o.hookMu.RUnlock()
	if fn != nil {
		fn(ctx, rn)
	}
}

func (o *Orchestrator) EnqueueProvision(job *domain.WorkflowJob, pool domain.RunnerPool, runnerName, encodedJITConfig string, githubRunnerID *int64) bool {
	return o.enqueueTask(dispatchTask{
		job:              job,
		pool:             pool,
		runnerName:       runnerName,
		encodedJITConfig: encodedJITConfig,
		githubRunnerID:   githubRunnerID,
	})
}

func (o *Orchestrator) EnqueueProvisionWithRunner(runner *domain.Runner, job *domain.WorkflowJob, pool domain.RunnerPool, encodedJITConfig string) bool {
	var runnerName string
	var ghRunnerID *int64
	if runner != nil {
		runnerName = runner.Name
		ghRunnerID = runner.GitHubRunnerID
	}
	return o.enqueueTask(dispatchTask{
		job:              job,
		pool:             pool,
		runner:           runner,
		runnerName:       runnerName,
		encodedJITConfig: encodedJITConfig,
		githubRunnerID:   ghRunnerID,
	})
}

func (o *Orchestrator) enqueueTask(task dispatchTask) bool {
	select {
	case o.queue <- task:
		return true
	default:
		o.log.Warn("orchestrator queue full; dropping provision task", zap.Int64("github_job_id", task.job.GitHubJobID))
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
	if err := o.runnerRepo.UpdateStatus(ctx, runner.ID, domain.RunnerTerminated, nil, "", nil); err != nil {
		return err
	}
	o.notifySlotFreed()
	return nil
}

// ReleaseCompletedRunner marks an ephemeral runner terminated immediately for webhooks.
// For Docker runners, exited containers are cleaned up asynchronously without blocking the webhook response.
// For Agent runners, an in-flight task is only signalled to cancel when cancelled == true; normal completion lets the runner process finish cleanly.
func (o *Orchestrator) ReleaseCompletedRunner(ctx context.Context, runner *domain.Runner, cancelled bool) error {
	if err := o.runnerRepo.UpdateStatus(ctx, runner.ID, domain.RunnerTerminated, nil, "", nil); err != nil {
		return err
	}
	defer o.notifySlotFreed()
	if runner.ProviderRef == "" {
		return nil
	}
	prov := o.selectProvisioner(runner.Provider)
	if runner.Provider == "agent" {
		if cancelled {
			if err := prov.Terminate(ctx, runner.ProviderRef); err != nil {
				o.log.Warn("agent cancel task error", zap.String("runner", runner.Name), zap.Error(err))
			}
		}
		return nil
	}
	if runner.Provider != "docker" {
		if err := prov.Terminate(ctx, runner.ProviderRef); err != nil {
			o.log.Warn("provisioner terminate error", zap.String("runner", runner.Name), zap.Error(err))
		}
		return nil
	}

	ref := runner.ProviderRef
	name := runner.Name
	baseCtx := o.lifecycleCtx()
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		select {
		case <-baseCtx.Done():
			return
		case <-time.After(4 * time.Second):
		}

		checkCtx, cancelCheck := context.WithTimeout(baseCtx, 5*time.Second)
		healthy, healthErr := prov.Healthy(checkCtx, ref)
		cancelCheck()
		if healthErr == nil && !healthy {
			return
		}

		termCtx, cancelTerm := context.WithTimeout(baseCtx, 15*time.Second)
		defer cancelTerm()
		if err := prov.Terminate(termCtx, ref); err != nil {
			o.log.Warn("delayed docker runner cleanup failed", zap.String("runner", name), zap.Error(err))
		}
	}()
	return nil
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
	providerName := task.pool.Provider
	if providerName == "" {
		providerName = o.cfg.Provider
	}
	prov := o.selectProvisioner(providerName)

	rn := task.runner
	if rn != nil {
		if runnerName == "" {
			runnerName = rn.Name
		}
		if task.githubRunnerID == nil {
			task.githubRunnerID = rn.GitHubRunnerID
		}
	} else {
		if runnerName == "" {
			runnerName = fmt.Sprintf("runway-%s-%s", task.pool.Slug, uuid.NewString()[:8])
		}
		now := time.Now().UTC()
		rn = &domain.Runner{
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
		if o.jobRepo != nil {
			_ = o.jobRepo.UpdateModerationAndRunner(provCtx, task.job.ID, domain.JobProvisioning, task.job.ModerationDecision, task.job.ModerationReason, task.job.ModeratedBy, &task.pool.ID, &rn.ID)
		}
	}

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
			_, _ = o.jobRepo.RequeueIfProvisioning(provCtx, task.job.ID, "Provisioning failed: "+err.Error())
		}
		o.cleanupGitHubRunner(provCtx, rn)
		o.notifySlotFreed()
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
	_ = o.runnerRepo.SetProvisioned(provCtx, rn.ID, initialStatus, task.githubRunnerID, res.ProviderRef, &task.job.GitHubJobID)
	if o.auditRepo != nil {
		_ = o.auditRepo.AppendAuditLog(provCtx, &domain.AuditLog{
			ActorLabel:   "orchestrator",
			Action:       "runner.provisioned",
			ResourceType: "runner",
			ResourceID:   rn.ID.String(),
			Metadata:     res.HostInfo,
		})
	}
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

func (o *Orchestrator) requeueLostRunnerJob(ctx context.Context, rn *domain.Runner, reason string) {
	if o.jobRepo == nil || rn == nil || rn.CurrentJobID == nil {
		return
	}
	job, err := o.jobRepo.GetByGitHubJobID(ctx, *rn.CurrentJobID)
	if err != nil || job == nil || job.Status != domain.JobProvisioning {
		return
	}
	_, _ = o.jobRepo.RequeueIfProvisioning(ctx, job.ID, reason)
}

func (o *Orchestrator) reconcileOnce(ctx context.Context) {
	now := time.Now().UTC()
	seen := make(map[uuid.UUID]struct{})

	if o.runnerRepo != nil {
		stale, err := o.runnerRepo.FindStaleRunners(
			ctx,
			now.Add(-o.cfg.ProvisionTimeout),
			now.Add(-o.cfg.StaleHeartbeatTTL),
		)
		if err != nil {
			o.log.Warn("reconcile query stale runners failed", zap.Error(err))
		} else {
			for i := range stale {
				rn := stale[i]
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
				o.requeueLostRunnerJob(ctx, &rn, fmt.Sprintf("Runner %s became unresponsive; requeued", rn.Name))
				o.cleanupGitHubRunner(ctx, &rn)
				o.notifySlotFreed()
				o.log.Info("marked stale runner offline", zap.String("runner", rn.Name))
			}
		}

		// Promptly detect exited ephemeral Docker containers so concurrency slots recover before StaleHeartbeatTTL.
		dockerActive, err := o.runnerRepo.ListActiveByProvider(ctx, "docker")
		if err != nil {
			o.log.Warn("reconcile query active docker runners failed", zap.Error(err))
		} else {
			dockerProv := o.selectProvisioner("docker")
			for i := range dockerActive {
				rn := dockerActive[i]
				if _, already := seen[rn.ID]; already {
					continue
				}
				healthy, healthErr := dockerProv.Healthy(ctx, rn.ProviderRef)
				if healthErr != nil || healthy {
					continue
				}
				_ = o.runnerRepo.UpdateStatus(ctx, rn.ID, domain.RunnerTerminated, nil, "", nil)
				o.requeueLostRunnerJob(ctx, &rn, fmt.Sprintf("Runner %s exited before starting job; requeued", rn.Name))
				o.notifySlotFreed()
				o.log.Info("reconciled exited docker runner", zap.String("runner", rn.Name))
			}
		}
	}

	if o.agentRepo != nil {
		if marked, err := o.agentRepo.MarkStaleOffline(ctx, now.Add(-2*time.Minute)); err != nil {
			o.log.Warn("reconcile mark stale agents failed", zap.Error(err))
		} else if marked > 0 {
			o.log.Info("marked stale agents offline", zap.Int64("count", marked))
		}

		if failedTasks, err := o.agentRepo.FailTasksForOfflineAgents(ctx); err != nil {
			o.log.Warn("reconcile fail tasks for offline agents failed", zap.Error(err))
		} else {
			for _, task := range failedTasks {
				if o.runnerRepo != nil {
					_ = o.runnerRepo.UpdateStatus(ctx, task.RunnerID, domain.RunnerOffline, nil, "", nil)
				}
				if o.jobRepo != nil {
					_, _ = o.jobRepo.RequeueIfProvisioning(ctx, task.JobID, "Assigned runway-agent went offline; requeued")
				}
				if o.runnerRepo != nil {
					if rn, getErr := o.runnerRepo.GetByID(ctx, task.RunnerID); getErr == nil {
						o.cleanupGitHubRunner(ctx, rn)
					}
				}
				o.notifySlotFreed()
				o.log.Warn("recovered task from offline agent", zap.String("runner", task.RunnerName), zap.Int64("github_job_id", task.GitHubJobID))
			}
		}

		expired, err := o.agentRepo.ExpirePendingTasks(ctx, now.Add(-o.cfg.ProvisionTimeout))
		if err != nil {
			o.log.Warn("reconcile expire pending agent tasks failed", zap.Error(err))
		} else {
			for _, task := range expired {
				if o.runnerRepo != nil {
					_ = o.runnerRepo.UpdateStatus(ctx, task.RunnerID, domain.RunnerOffline, nil, "", nil)
				}
				if o.jobRepo != nil {
					_, _ = o.jobRepo.RequeueIfProvisioning(ctx, task.JobID, "Provisioning timed out waiting for an online runway-agent; requeued")
				}
				if o.runnerRepo != nil {
					if rn, getErr := o.runnerRepo.GetByID(ctx, task.RunnerID); getErr == nil {
						o.cleanupGitHubRunner(ctx, rn)
					}
				}
				o.notifySlotFreed()
				o.log.Warn("expired unclaimed agent task", zap.String("runner", task.RunnerName), zap.Int64("github_job_id", task.GitHubJobID))
			}
		}
	}

	if o.jobRepo != nil {
		orphans, err := o.jobRepo.FindOrphanedProvisioningJobs(ctx, now.Add(-15*time.Second), 50)
		if err != nil {
			o.log.Warn("reconcile find orphaned provisioning jobs failed", zap.Error(err))
		} else {
			for _, job := range orphans {
				if requeued, reqErr := o.jobRepo.RequeueIfProvisioning(ctx, job.ID, "Previous runner went offline; requeued"); reqErr == nil && requeued {
					o.notifySlotFreed()
					o.log.Info("requeued orphaned provisioning job", zap.Int64("github_job_id", job.GitHubJobID))
				}
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
