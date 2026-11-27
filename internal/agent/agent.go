package agent

import (
	"context"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Executor interface {
	Execute(ctx context.Context, task *TaskPayload) error
}

type LogAwareExecutor interface {
	ExecuteWithLogs(ctx context.Context, task *TaskPayload, logSink func([]domain.RunnerLogEntry)) error
}

type CacheStatsProvider interface {
	CacheStats() (hits, misses int64)
}

type Agent struct {
	cfg        Config
	log        *zap.Logger
	client     *Client
	executor   Executor
	updater    *Updater
	metrics    *MetricsCollector
	runtime    string
	hostname   string
	dockerOpts orchestrator.DockerOptions

	activeCount         atomic.Int32
	draining            atomic.Bool
	tasksCompleted      atomic.Int64
	tasksFailed         atomic.Int64
	tasksCancelled      atomic.Int64
	totalTaskDurationMS atomic.Int64
	sem                 chan struct{}
	jobsWG              sync.WaitGroup

	cancelMu sync.Mutex
	cancels  map[uuid.UUID]context.CancelFunc

	pollNowCh       chan struct{}
	updateRestartCh chan string
}

func New(cfg Config, log *zap.Logger) *Agent {
	hostname, _ := os.Hostname()
	client := NewClient(cfg.ServerURL, cfg.Token, cfg.Version)

	rt := cfg.Runtime
	dockerOpts := ResolveDockerOptions(cfg)
	var dockerExec *DockerExecutor
	if rt == "docker" || (rt == "auto" && runtime.GOOS == "linux") {
		dockerExec = NewDockerExecutor(cfg.DockerSocket, dockerOpts)
		pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := dockerExec.Ping(pingCtx)
		cancel()
		if err != nil {
			if rt == "docker" {
				log.Warn("configured docker runtime failed ping check", zap.Error(err))
			} else {
				log.Info("docker daemon unavailable; falling back to native actions-runner runtime")
				dockerExec = nil
			}
		}
	}

	var execImpl Executor
	effectiveRT := "native"
	if dockerExec != nil && (rt == "docker" || (rt == "auto" && runtime.GOOS == "linux")) {
		execImpl = dockerExec
		effectiveRT = "docker"
	} else {
		execImpl = NewNativeExecutor(cfg.WorkDir, cfg.RunnerVersion, cfg.CacheEnabled)
	}

	return &Agent{
		cfg:             cfg,
		log:             log,
		client:          client,
		executor:        execImpl,
		updater:         NewUpdater(cfg.UpdateRepo),
		metrics:         NewMetricsCollector(cfg.WorkDir),
		runtime:         effectiveRT,
		hostname:        hostname,
		dockerOpts:      dockerOpts,
		sem:             make(chan struct{}, cfg.Concurrency),
		cancels:         make(map[uuid.UUID]context.CancelFunc),
		pollNowCh:       make(chan struct{}, 1),
		updateRestartCh: make(chan string, 1),
	}
}

func (a *Agent) Run(ctx context.Context) error {
	a.log.Info("starting runway-agent",
		zap.String("server", a.cfg.ServerURL),
		zap.String("os", NormalizeOS()),
		zap.String("arch", NormalizeArch()),
		zap.String("runtime", a.runtime),
		zap.String("sandbox_runtime", a.dockerOpts.SandboxRuntime),
		zap.Bool("isolated_dind", a.dockerOpts.IsolatedDinD),
		zap.Int("concurrency", a.cfg.Concurrency),
		zap.String("work_dir", a.cfg.WorkDir),
		zap.String("log_dir", a.cfg.LogDir),
		zap.Bool("mount_docker_socket", a.dockerOpts.MountDockerSocket),
		zap.String("runner_docker_gid", a.dockerOpts.RunnerDockerGID),
		zap.String("runner_docker_host", a.dockerOpts.RunnerDockerHost),
		zap.String("version", a.cfg.Version),
		zap.Bool("auto_update", a.cfg.AutoUpdate),
	)

	// Initial heartbeat so control plane sees the agent immediately.
	a.sendHeartbeat(ctx)

	loopCtx, cancelLoop := context.WithCancel(ctx)
	defer cancelLoop()

	if a.cfg.PrewarmImages {
		if de, ok := a.executor.(*DockerExecutor); ok {
			go func() {
				preCtx, preCancel := context.WithTimeout(loopCtx, 4*time.Minute)
				defer preCancel()
				imgs := []string{"ghcr.io/cubetiqlabs/runway-runner:latest"}
				if a.dockerOpts.IsolatedDinD && a.dockerOpts.DinDImage != "" {
					imgs = append(imgs, a.dockerOpts.DinDImage)
				}
				de.PrewarmImages(preCtx, imgs)
			}()
		}
	}

	var bgWG sync.WaitGroup
	bgWG.Add(1)
	go func() {
		defer bgWG.Done()
		a.heartbeatLoop(loopCtx)
	}()

	if a.cfg.AutoUpdate {
		bgWG.Add(1)
		go func() {
			defer bgWG.Done()
			a.autoUpdateLoop(loopCtx, cancelLoop)
		}()
	}

	pollTicker := time.NewTicker(a.cfg.PollInterval)
	defer pollTicker.Stop()

claimLoop:
	for {
		select {
		case <-loopCtx.Done():
			break claimLoop
		case <-pollTicker.C:
			a.drainClaims(loopCtx, ctx)
		case <-a.pollNowCh:
			a.drainClaims(loopCtx, ctx)
		}
	}

	cancelLoop()
	bgWG.Wait()

	a.log.Info("draining active jobs before exit", zap.Int32("active_jobs", a.activeCount.Load()))
	a.jobsWG.Wait()

	select {
	case newVer := <-a.updateRestartCh:
		a.log.Info("restarting cleanly after self-update", zap.String("new_version", newVer))
	default:
	}
	return nil
}

func (a *Agent) drainClaims(loopCtx, rootCtx context.Context) {
	if a.draining.Load() {
		return
	}
	for i := 0; i < a.cfg.Concurrency; i++ {
		if !a.tryClaimAndStart(loopCtx, rootCtx) {
			return
		}
	}
}

func (a *Agent) tryClaimAndStart(loopCtx, rootCtx context.Context) bool {
	select {
	case a.sem <- struct{}{}:
	default:
		return false
	}

	claimCtx, cancel := context.WithTimeout(loopCtx, 15*time.Second)
	task, err := a.client.ClaimTask(claimCtx)
	cancel()
	if err != nil {
		<-a.sem
		if loopCtx.Err() == nil {
			a.log.Warn("claim task failed", zap.Error(err))
		}
		return false
	}
	if task == nil {
		<-a.sem
		return false
	}

	a.jobsWG.Add(1)
	a.activeCount.Add(1)

	jobCtx, cancelJob := context.WithCancel(rootCtx)
	a.cancelMu.Lock()
	a.cancels[task.TaskID] = cancelJob
	a.cancelMu.Unlock()

	go func() {
		defer func() {
			cancelJob()
			a.cancelMu.Lock()
			delete(a.cancels, task.TaskID)
			a.cancelMu.Unlock()
			a.activeCount.Add(-1)
			<-a.sem
			select {
			case a.pollNowCh <- struct{}{}:
			default:
			}
			a.jobsWG.Done()
		}()

		a.log.Info("executing runner task",
			zap.String("task_id", task.TaskID.String()),
			zap.String("runner", task.RunnerName),
			zap.String("repo", task.RepoFullName),
			zap.Int64("github_job_id", task.GitHubJobID),
		)

		startedAt := time.Now()
		var execErr error
		if logExec, ok := a.executor.(LogAwareExecutor); ok {
			execErr = logExec.ExecuteWithLogs(jobCtx, task, func(entries []domain.RunnerLogEntry) {
				shipCtx, shipCancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = a.client.ShipTaskLogs(shipCtx, task.TaskID, entries)
				shipCancel()
			})
		} else {
			execErr = a.executor.Execute(jobCtx, task)
		}
		durMS := time.Since(startedAt).Milliseconds()
		a.totalTaskDurationMS.Add(durMS)

		status := "completed"
		errMsg := ""
		if execErr != nil {
			errMsg = execErr.Error()
			if jobCtx.Err() != nil {
				status = "cancelled"
				a.tasksCancelled.Add(1)
				a.log.Info("runner task cancelled", zap.String("runner", task.RunnerName), zap.String("reason", errMsg))
			} else {
				status = "failed"
				a.tasksFailed.Add(1)
				a.log.Error("runner task failed", zap.String("runner", task.RunnerName), zap.Error(execErr))
			}
		} else {
			a.tasksCompleted.Add(1)
			a.log.Info("runner task completed", zap.String("runner", task.RunnerName))
		}

		compCtx, compCancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := a.client.CompleteTask(compCtx, task.TaskID, status, errMsg); err != nil {
			a.log.Warn("failed to report task completion", zap.String("task_id", task.TaskID.String()), zap.Error(err))
		}
		compCancel()
	}()
	return true
}

func (a *Agent) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.sendHeartbeat(ctx)
		}
	}
}

func (a *Agent) sendHeartbeat(ctx context.Context) {
	hbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var cacheHits, cacheMisses int64
	if csp, ok := a.executor.(CacheStatsProvider); ok {
		cacheHits, cacheMisses = csp.CacheStats()
	}
	completed := a.tasksCompleted.Load()
	failed := a.tasksFailed.Load()
	cancelled := a.tasksCancelled.Load()
	totalDone := completed + failed + cancelled
	var avgDurMS int64
	if totalDone > 0 {
		avgDurMS = a.totalTaskDurationMS.Load() / totalDone
	}

	var snapPtr *domain.AgentMetricsSnapshot
	var memTotalMB int
	if a.metrics != nil {
		snap := a.metrics.Collect(
			a.dockerOpts.SandboxRuntime,
			a.dockerOpts.IsolatedDinD,
			cacheHits, cacheMisses,
			completed, failed, cancelled, avgDurMS,
		)
		memTotalMB = snap.MemoryTotalMB
		snapPtr = &snap
	}

	res, err := a.client.Heartbeat(hbCtx, HeartbeatRequest{
		OS:             NormalizeOS(),
		Arch:           NormalizeArch(),
		Runtime:        a.runtime,
		SandboxRuntime: a.dockerOpts.SandboxRuntime,
		Version:        a.cfg.Version,
		Hostname:       a.hostname,
		CPUCount:       runtime.NumCPU(),
		MemoryMB:       memTotalMB,
		MaxConcurrency: a.cfg.Concurrency,
		ActiveJobs:     int(a.activeCount.Load()),
		Labels:         a.cfg.Labels,
		Metrics:        snapPtr,
	})
	if err != nil {
		if ctx.Err() == nil {
			a.log.Warn("agent heartbeat failed", zap.Error(err))
		}
		return
	}

	switch res.Status {
	case "draining", "drained":
		if !a.draining.Swap(true) {
			a.log.Info("agent marked draining by control plane; pausing new task claims", zap.String("status", res.Status))
		}
	case "online":
		if a.draining.Swap(false) {
			a.log.Info("agent resumed by control plane; accepting new task claims")
			select {
			case a.pollNowCh <- struct{}{}:
			default:
			}
		}
	}

	for _, id := range res.CancelTaskIDs {
		a.cancelMu.Lock()
		cancelFn, ok := a.cancels[id]
		a.cancelMu.Unlock()
		if ok {
			a.log.Info("cancelling active task per control plane", zap.String("task_id", id.String()))
			cancelFn()
		}
	}
}

func (a *Agent) autoUpdateLoop(ctx context.Context, cancelLoop context.CancelFunc) {
	ticker := time.NewTicker(a.cfg.UpdateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rel, err := a.updater.fetchLatestRelease()
			if err != nil {
				a.log.Debug("auto-update check failed", zap.Error(err))
				continue
			}
			if !isNewerVersion(a.cfg.Version, rel.TagName) {
				continue
			}

			a.log.Info("new runway-agent version available; draining to apply update",
				zap.String("current", a.cfg.Version),
				zap.String("latest", rel.TagName),
			)
			a.draining.Store(true)
			a.jobsWG.Wait()

			res, applyErr := a.updater.CheckAndApply(a.cfg.Version, false)
			if applyErr != nil {
				a.log.Error("auto-update failed; resuming task claims", zap.Error(applyErr))
				a.draining.Store(false)
				continue
			}
			if res.Updated {
				select {
				case a.updateRestartCh <- res.LatestVersion:
				default:
				}
				cancelLoop()
				return
			}
			a.draining.Store(false)
		}
	}
}
