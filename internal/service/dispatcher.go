package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/github"
	"github.com/cubetiqlabs/runway/internal/moderation"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	defaultDispatcherSweepInterval = 5 * time.Second
	maxQueuedJobAge                = 24 * time.Hour
	remoteVerifyMinAge             = 10 * time.Second
)

// JobDispatcher serializes capacity checks across pool concurrency and online runway-agent slots,
// and drains queued workflow jobs whenever capacity frees up or agents reconnect.
type JobDispatcher struct {
	cfg        *config.Config
	log        *zap.Logger
	ghClient   *github.Client
	instRepo   domain.InstallationRepository
	poolRepo   domain.PoolRepository
	jobRepo    domain.JobRepository
	runnerRepo domain.RunnerRepository
	agentRepo  domain.AgentRepository
	auditRepo  domain.AuditRepository
	orch       *orchestrator.Orchestrator

	dispatchMu    sync.Mutex
	drainMu       sync.Mutex
	backoffMu     sync.Mutex
	retryAfter    map[uuid.UUID]time.Time
	retryCount    map[uuid.UUID]int
	notifyCh      chan struct{}
	sweepInterval time.Duration
	wg            sync.WaitGroup
}

func NewJobDispatcher(
	cfg *config.Config,
	log *zap.Logger,
	ghClient *github.Client,
	instRepo domain.InstallationRepository,
	poolRepo domain.PoolRepository,
	jobRepo domain.JobRepository,
	runnerRepo domain.RunnerRepository,
	agentRepo domain.AgentRepository,
	auditRepo domain.AuditRepository,
	orch *orchestrator.Orchestrator,
) *JobDispatcher {
	if log == nil {
		log = zap.NewNop()
	}
	return &JobDispatcher{
		cfg:           cfg,
		log:           log,
		ghClient:      ghClient,
		instRepo:      instRepo,
		poolRepo:      poolRepo,
		jobRepo:       jobRepo,
		runnerRepo:    runnerRepo,
		agentRepo:     agentRepo,
		auditRepo:     auditRepo,
		orch:          orch,
		retryAfter:    make(map[uuid.UUID]time.Time),
		retryCount:    make(map[uuid.UUID]int),
		notifyCh:      make(chan struct{}, 1),
		sweepInterval: defaultDispatcherSweepInterval,
	}
}

func (d *JobDispatcher) Start(ctx context.Context) {
	if d == nil {
		return
	}
	interval := d.sweepInterval
	if interval <= 0 {
		interval = defaultDispatcherSweepInterval
	}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		// Drain any backlog leftover from an earlier restart promptly.
		d.drainQueuedOnce(ctx)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-d.notifyCh:
				d.drainQueuedOnce(ctx)
			case <-ticker.C:
				d.drainQueuedOnce(ctx)
			}
		}
	}()
}

func (d *JobDispatcher) Wait() {
	if d != nil {
		d.wg.Wait()
	}
}

// Trigger wakes the background dispatcher immediately to drain queued jobs.
func (d *JobDispatcher) Trigger() {
	if d == nil {
		return
	}
	select {
	case d.notifyCh <- struct{}{}:
	default:
	}
}

// DispatchJob enforces pool concurrency and agent slot capacity, reserves a runner record,
// requests a GitHub JIT config if configured, and enqueues provisioning.
func (d *JobDispatcher) DispatchJob(ctx context.Context, job *domain.WorkflowJob, pool domain.RunnerPool, ownerType string) error {
	if d == nil || d.runnerRepo == nil || d.orch == nil || job == nil {
		return fmt.Errorf("job dispatcher not configured")
	}

	provider := strings.TrimSpace(pool.Provider)
	if provider == "" && d.cfg != nil {
		provider = strings.TrimSpace(d.cfg.Orchestrator.Provider)
	}
	if provider == "" {
		provider = "simulated"
	}

	d.dispatchMu.Lock()
	activeInPool, err := d.runnerRepo.CountActiveInPool(ctx, pool.ID)
	if err != nil {
		d.dispatchMu.Unlock()
		return err
	}
	if activeInPool >= pool.MaxConcurrency {
		d.dispatchMu.Unlock()
		return fmt.Errorf("%w: pool %q reached max_concurrency (%d)", domain.ErrCapacityFull, pool.Name, pool.MaxConcurrency)
	}

	if provider == "agent" && d.agentRepo != nil {
		freeSlots, onlineAgents, err := d.agentRepo.CountAvailableSlotsForPool(ctx, pool)
		if err != nil {
			d.dispatchMu.Unlock()
			return err
		}
		if onlineAgents == 0 {
			d.dispatchMu.Unlock()
			return fmt.Errorf("%w: no online runway-agent matching pool %q", domain.ErrCapacityFull, pool.Name)
		}
		if freeSlots <= 0 {
			d.dispatchMu.Unlock()
			return fmt.Errorf("%w: all online runway-agent workers for pool %q are at capacity", domain.ErrCapacityFull, pool.Name)
		}
	}

	runnerName := buildRunnerName(pool.Slug, job.GitHubJobID)
	now := time.Now().UTC()
	rn := &domain.Runner{
		PoolID:          &pool.ID,
		InstallationID:  job.InstallationID,
		Name:            runnerName,
		Provider:        provider,
		Labels:          pool.Labels,
		Status:          domain.RunnerProvisioning,
		Ephemeral:       true,
		CurrentJobID:    &job.GitHubJobID,
		LastHeartbeatAt: &now,
	}
	if err := d.runnerRepo.Create(ctx, rn); err != nil {
		d.dispatchMu.Unlock()
		return err
	}

	if d.jobRepo != nil {
		reason := fmt.Sprintf("Provisioning runner %s on pool %s", runnerName, pool.Name)
		claimed, claimErr := d.jobRepo.ClaimForProvisioning(ctx, job.ID, &pool.ID, &rn.ID, reason)
		if claimErr != nil {
			_ = d.runnerRepo.UpdateStatus(ctx, rn.ID, domain.RunnerTerminated, nil, "", nil)
			d.dispatchMu.Unlock()
			return claimErr
		}
		if !claimed {
			_ = d.runnerRepo.UpdateStatus(ctx, rn.ID, domain.RunnerTerminated, nil, "", nil)
			d.dispatchMu.Unlock()
			return nil
		}
	}
	d.dispatchMu.Unlock()

	if ownerType == "" && job.InstallationID != nil && d.instRepo != nil {
		if inst, instErr := d.instRepo.GetByID(ctx, *job.InstallationID); instErr == nil && inst != nil {
			ownerType = inst.AccountType
		}
	}

	var encodedJIT string
	var ghRunnerID *int64
	if d.ghClient != nil && d.ghClient.IsConfigured() && job.InstallationID != nil {
		parts := strings.SplitN(job.RepoFullName, "/", 2)
		if len(parts) == 2 {
			groupID := pool.RunnerGroupID
			if groupID <= 0 {
				groupID = 1
			}
			req := github.JITConfigRequest{
				Name:          runnerName,
				RunnerGroupID: groupID,
				Labels:        mergeRunnerLabels(pool, job.Labels),
			}
			var res *github.JITConfigResponse
			if strings.EqualFold(ownerType, "User") {
				res, err = d.ghClient.GenerateRepoJITConfig(ctx, *job.InstallationID, parts[0], parts[1], req)
				if err != nil {
					err = fmt.Errorf("generate github jit config: %w", err)
				}
			} else {
				var orgErr error
				res, orgErr = d.ghClient.GenerateOrgJITConfig(ctx, *job.InstallationID, parts[0], req)
				if orgErr != nil {
					var repoErr error
					res, repoErr = d.ghClient.GenerateRepoJITConfig(ctx, *job.InstallationID, parts[0], parts[1], req)
					if repoErr != nil {
						err = fmt.Errorf("generate github jit config: org (%v); repo (%w)", orgErr, repoErr)
					}
				}
			}
			if err != nil {
				_ = d.runnerRepo.UpdateStatus(ctx, rn.ID, domain.RunnerTerminated, nil, "", nil)
				if d.jobRepo != nil {
					_, _ = d.jobRepo.RequeueIfProvisioning(ctx, job.ID, "Dispatch failed: "+err.Error())
				}
				d.recordBackoff(job.ID)
				return err
			}
			encodedJIT = res.EncodedJITConfig
			id := res.Runner.ID
			ghRunnerID = &id
			rn.GitHubRunnerID = ghRunnerID
			_ = d.runnerRepo.UpdateStatus(ctx, rn.ID, domain.RunnerProvisioning, ghRunnerID, "", &job.GitHubJobID)
		}
	}

	if ok := d.orch.EnqueueProvisionWithRunner(rn, job, pool, encodedJIT); !ok {
		d.CleanupGitHubRunner(ctx, rn)
		_ = d.runnerRepo.UpdateStatus(ctx, rn.ID, domain.RunnerTerminated, nil, "", nil)
		if d.jobRepo != nil {
			_, _ = d.jobRepo.RequeueIfProvisioning(ctx, job.ID, "Waiting for orchestrator queue capacity")
		}
		return fmt.Errorf("%w: orchestrator provision queue is full", domain.ErrCapacityFull)
	}

	d.clearBackoff(job.ID)
	return nil
}

// CleanupGitHubRunner deletes a pre-registered JIT runner on GitHub when provisioning failed or timed out
// before a container or agent could execute the job, preventing stale Offline entries in GitHub UI.
func (d *JobDispatcher) CleanupGitHubRunner(ctx context.Context, rn *domain.Runner) {
	if d == nil || rn == nil || rn.GitHubRunnerID == nil || rn.InstallationID == nil || d.ghClient == nil || !d.ghClient.IsConfigured() {
		return
	}

	cleanCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var owner, repo string
	if rn.CurrentJobID != nil && d.jobRepo != nil {
		if job, err := d.jobRepo.GetByGitHubJobID(cleanCtx, *rn.CurrentJobID); err == nil && job != nil {
			if parts := strings.SplitN(job.RepoFullName, "/", 2); len(parts) == 2 {
				owner, repo = parts[0], parts[1]
			}
		}
	}
	if owner == "" && d.instRepo != nil {
		if inst, err := d.instRepo.GetByID(cleanCtx, *rn.InstallationID); err == nil && inst != nil {
			owner = inst.AccountLogin
		}
	}
	if owner == "" {
		return
	}

	if err := d.ghClient.DeleteOrgRunner(cleanCtx, *rn.InstallationID, owner, *rn.GitHubRunnerID); err == nil {
		return
	} else if repo != "" {
		if repoErr := d.ghClient.DeleteRepoRunner(cleanCtx, *rn.InstallationID, owner, repo, *rn.GitHubRunnerID); repoErr != nil {
			d.log.Debug("best-effort cleanup of orphaned github runner failed",
				zap.String("runner", rn.Name),
				zap.Int64("github_runner_id", *rn.GitHubRunnerID),
				zap.Error(repoErr),
			)
		}
	} else {
		d.log.Debug("best-effort cleanup of orphaned github org runner failed",
			zap.String("runner", rn.Name),
			zap.Int64("github_runner_id", *rn.GitHubRunnerID),
			zap.Error(err),
		)
	}
}

func (d *JobDispatcher) drainQueuedOnce(ctx context.Context) {
	if d == nil || d.jobRepo == nil || d.poolRepo == nil {
		return
	}
	if !d.drainMu.TryLock() {
		return
	}
	defer d.drainMu.Unlock()

	sweepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	jobs, err := d.jobRepo.ListDispatchableQueued(sweepCtx, 50)
	if err != nil {
		if sweepCtx.Err() == nil {
			d.log.Warn("dispatcher failed to list queued jobs", zap.Error(err))
		}
		return
	}
	if len(jobs) == 0 {
		return
	}

	pools, err := d.poolRepo.ListEnabled(sweepCtx)
	if err != nil {
		if sweepCtx.Err() == nil {
			d.log.Warn("dispatcher failed to list enabled pools", zap.Error(err))
		}
		return
	}
	poolByID := make(map[uuid.UUID]domain.RunnerPool, len(pools))
	for _, p := range pools {
		poolByID[p.ID] = p
	}
	fullPools := make(map[uuid.UUID]bool)

	for i := range jobs {
		if sweepCtx.Err() != nil {
			return
		}
		job := jobs[i]
		if d.inBackoff(job.ID) {
			continue
		}

		if time.Since(job.QueuedAt) > maxQueuedJobAge {
			now := time.Now().UTC()
			_ = d.jobRepo.UpdateLifecycle(sweepCtx, job.GitHubJobID, domain.JobCancelled, "expired", nil, &now)
			d.clearBackoff(job.ID)
			continue
		}

		var pool *domain.RunnerPool
		if job.MatchedPoolID != nil {
			if p, ok := poolByID[*job.MatchedPoolID]; ok {
				poolCopy := p
				pool = &poolCopy
			}
		}
		if pool == nil {
			pool = moderation.MatchPool(pools, job.InstallationID, job.Labels)
		}
		if pool == nil || fullPools[pool.ID] {
			continue
		}

		if d.reconcileQueuedJobWithGitHub(sweepCtx, &job) {
			d.clearBackoff(job.ID)
			continue
		}

		err := d.DispatchJob(sweepCtx, &job, *pool, "")
		if err == nil {
			d.log.Info("dispatched queued workflow job",
				zap.Int64("github_job_id", job.GitHubJobID),
				zap.String("pool", pool.Slug),
			)
			continue
		}
		if errors.Is(err, domain.ErrCapacityFull) {
			fullPools[pool.ID] = true
			if job.ModerationReason != err.Error() {
				_, _ = d.jobRepo.RequeueIfProvisioning(sweepCtx, job.ID, err.Error())
			}
			continue
		}

		d.recordBackoff(job.ID)
		d.log.Warn("dispatcher failed to dispatch queued job",
			zap.Int64("github_job_id", job.GitHubJobID),
			zap.String("pool", pool.Slug),
			zap.Error(err),
		)
	}
}

// reconcileQueuedJobWithGitHub verifies stale queued jobs against GitHub so missed webhooks
// (or jobs cancelled while waiting in the queue) are synced instead of provisioning useless runners.
// Returns true if the job is no longer queued.
func (d *JobDispatcher) reconcileQueuedJobWithGitHub(ctx context.Context, job *domain.WorkflowJob) bool {
	if d.ghClient == nil || !d.ghClient.IsConfigured() || job.InstallationID == nil {
		return false
	}
	if time.Since(job.QueuedAt) <= remoteVerifyMinAge {
		return false
	}
	parts := strings.SplitN(job.RepoFullName, "/", 2)
	if len(parts) != 2 {
		return false
	}

	remote, err := d.ghClient.GetWorkflowJob(ctx, *job.InstallationID, parts[0], parts[1], job.GitHubJobID)
	if err != nil {
		if strings.Contains(err.Error(), "github api error (404)") {
			now := time.Now().UTC()
			_ = d.jobRepo.UpdateLifecycle(ctx, job.GitHubJobID, domain.JobCancelled, "not_found", nil, &now)
			return true
		}
		d.log.Debug("github workflow job status check failed", zap.Int64("github_job_id", job.GitHubJobID), zap.Error(err))
		return false
	}

	switch strings.ToLower(strings.TrimSpace(remote.Status)) {
	case "completed":
		st := domain.JobCompleted
		if strings.EqualFold(remote.Conclusion, "cancelled") {
			st = domain.JobCancelled
		}
		now := time.Now().UTC()
		if remote.CompletedAt != nil {
			now = *remote.CompletedAt
		}
		_ = d.jobRepo.UpdateLifecycle(ctx, job.GitHubJobID, st, remote.Conclusion, remote.StartedAt, &now)
		return true
	case "in_progress":
		_ = d.jobRepo.UpdateLifecycle(ctx, job.GitHubJobID, domain.JobInProgress, "", remote.StartedAt, nil)
		return true
	default:
		return false
	}
}

func (d *JobDispatcher) inBackoff(jobID uuid.UUID) bool {
	d.backoffMu.Lock()
	defer d.backoffMu.Unlock()
	until, ok := d.retryAfter[jobID]
	return ok && time.Now().Before(until)
}

func (d *JobDispatcher) recordBackoff(jobID uuid.UUID) {
	d.backoffMu.Lock()
	defer d.backoffMu.Unlock()
	count := d.retryCount[jobID] + 1
	d.retryCount[jobID] = count
	delay := 15 * time.Second
	for i := 1; i < count && delay < 2*time.Minute; i++ {
		delay *= 2
	}
	if delay > 2*time.Minute {
		delay = 2 * time.Minute
	}
	d.retryAfter[jobID] = time.Now().Add(delay)
}

func (d *JobDispatcher) clearBackoff(jobID uuid.UUID) {
	d.backoffMu.Lock()
	defer d.backoffMu.Unlock()
	delete(d.retryAfter, jobID)
	delete(d.retryCount, jobID)
}
