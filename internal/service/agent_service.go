package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cubetiqlabs/runway/internal/cache"
	"github.com/cubetiqlabs/runway/internal/crypto"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/eventstore"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type AgentService struct {
	log        *zap.Logger
	agentRepo  domain.AgentRepository
	runnerRepo domain.RunnerRepository
	jobRepo    domain.JobRepository
	auditRepo  domain.AuditRepository
	poolRepo   domain.PoolRepository
	eventStore eventstore.Store
	cache      cache.Store
	encKey     []byte

	hookMu      sync.RWMutex
	onSlotFreed func()
}

func NewAgentService(
	log *zap.Logger,
	agentRepo domain.AgentRepository,
	runnerRepo domain.RunnerRepository,
	jobRepo domain.JobRepository,
	auditRepo domain.AuditRepository,
	poolRepo domain.PoolRepository,
	encKey []byte,
) *AgentService {
	return &AgentService{
		log:        log,
		agentRepo:  agentRepo,
		runnerRepo: runnerRepo,
		jobRepo:    jobRepo,
		auditRepo:  auditRepo,
		poolRepo:   poolRepo,
		encKey:     encKey,
	}
}

func (s *AgentService) SetEventStore(es eventstore.Store) {
	s.hookMu.Lock()
	s.eventStore = es
	s.hookMu.Unlock()
}

func (s *AgentService) SetCache(c cache.Store) {
	s.hookMu.Lock()
	s.cache = c
	s.hookMu.Unlock()
}

func (s *AgentService) getCache() cache.Store {
	s.hookMu.RLock()
	c := s.cache
	s.hookMu.RUnlock()
	return c
}

func (s *AgentService) invalidateAgentCache(ctx context.Context, agentID uuid.UUID, tokenHash string) {
	c := s.getCache()
	if c == nil {
		return
	}
	if tokenHash != "" {
		_ = c.Delete(ctx, "auth:agent:"+tokenHash)
	}
	_ = c.DeletePrefix(ctx, "agent:detail:"+agentID.String()+":")
	_ = c.Delete(ctx, "dashboard:overview")
}

func (s *AgentService) emitEvent(ev domain.Event) {
	s.hookMu.RLock()
	es := s.eventStore
	s.hookMu.RUnlock()
	if es != nil {
		es.Publish(ev)
	}
}

func (s *AgentService) SetSlotFreedHook(fn func()) {
	s.hookMu.Lock()
	s.onSlotFreed = fn
	s.hookMu.Unlock()
}

func (s *AgentService) notifySlotFreed() {
	s.hookMu.RLock()
	fn := s.onSlotFreed
	s.hookMu.RUnlock()
	if fn != nil {
		fn()
	}
}

type RegisterAgentRequest struct {
	Name           string     `json:"name"`
	OS             string     `json:"os"`
	Arch           string     `json:"arch"`
	Runtime        string     `json:"runtime"`
	Labels         []string   `json:"labels"`
	PoolID         *uuid.UUID `json:"pool_id"`
	MaxConcurrency int        `json:"max_concurrency"`
}

type RegisterAgentResponse struct {
	Agent domain.Agent `json:"agent"`
	Token string       `json:"token"`
}

func (s *AgentService) RegisterAgent(ctx context.Context, req RegisterAgentRequest, actor *domain.User, ip string) (*RegisterAgentResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: agent name is required", domain.ErrValidation)
	}
	if req.PoolID != nil && s.poolRepo != nil {
		if _, err := s.poolRepo.GetByID(ctx, *req.PoolID); err != nil {
			return nil, fmt.Errorf("%w: referenced pool_id not found", domain.ErrValidation)
		}
	}

	rawSuffix, err := crypto.GenerateRandomToken(32)
	if err != nil {
		return nil, err
	}
	rawToken := "rwa_" + rawSuffix
	prefix := rawToken
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}

	osVal := strings.ToLower(strings.TrimSpace(req.OS))
	if osVal == "" {
		osVal = "linux"
	}
	archVal := strings.ToLower(strings.TrimSpace(req.Arch))
	if archVal == "" {
		archVal = "x64"
	}
	rtVal := strings.ToLower(strings.TrimSpace(req.Runtime))
	if rtVal == "" {
		rtVal = "native"
	}
	maxConc := req.MaxConcurrency
	if maxConc <= 0 {
		maxConc = 2
	}

	agent := &domain.Agent{
		Name:           name,
		TokenPrefix:    prefix,
		TokenHash:      crypto.HashTokenSHA256(rawToken),
		OS:             osVal,
		Arch:           archVal,
		Runtime:        rtVal,
		Labels:         req.Labels,
		PoolID:         req.PoolID,
		MaxConcurrency: maxConc,
		Status:         domain.AgentOffline,
	}
	if err := s.agentRepo.Create(ctx, agent); err != nil {
		return nil, err
	}

	s.appendAudit(ctx, actor, "agent.registered", "agent", agent.ID.String(), map[string]any{
		"name":    agent.Name,
		"os":      agent.OS,
		"arch":    agent.Arch,
		"runtime": agent.Runtime,
	}, ip)

	out, err := s.agentRepo.GetByID(ctx, agent.ID)
	if err != nil {
		out = agent
	}
	return &RegisterAgentResponse{Agent: *out, Token: rawToken}, nil
}

func (s *AgentService) ListAgents(ctx context.Context, filter domain.AgentFilter) ([]domain.Agent, int, error) {
	return s.agentRepo.List(ctx, filter)
}

func (s *AgentService) DrainAgent(ctx context.Context, id uuid.UUID, actor *domain.User, ip string) (*domain.Agent, error) {
	ag, err := s.agentRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	activeTasks, _ := s.agentRepo.CountActiveTasksByAgent(ctx, id)
	nextStatus := domain.AgentDraining
	auditAction := "agent.draining"
	if activeTasks == 0 && ag.ActiveJobs == 0 {
		nextStatus = domain.AgentDrained
		auditAction = "agent.drained"
	}
	if err := s.agentRepo.UpdateStatus(ctx, id, nextStatus); err != nil {
		return nil, err
	}
	s.appendAudit(ctx, actor, auditAction, "agent", id.String(), map[string]any{"name": ag.Name, "status": nextStatus}, ip)
	s.emitEvent(domain.Event{
		Category:     domain.EventCategoryAgentLifecycle,
		EventType:    auditAction,
		Severity:     "info",
		Source:       "control-plane",
		ActorLabel:   actorEmail(actor),
		ResourceType: "agent",
		ResourceID:   id.String(),
		Status:       string(nextStatus),
		Message:      fmt.Sprintf("Agent %s transitioned to %s", ag.Name, nextStatus),
		IPAddress:    ip,
	})
	s.invalidateAgentCache(ctx, id, ag.TokenHash)
	return s.agentRepo.GetByID(ctx, id)
}

func (s *AgentService) ResumeAgent(ctx context.Context, id uuid.UUID, actor *domain.User, ip string) (*domain.Agent, error) {
	ag, err := s.agentRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	nextStatus := domain.AgentOffline
	if ag.LastHeartbeatAt != nil && time.Since(*ag.LastHeartbeatAt) < 2*time.Minute {
		nextStatus = domain.AgentOnline
	}
	if err := s.agentRepo.UpdateStatus(ctx, id, nextStatus); err != nil {
		return nil, err
	}
	s.appendAudit(ctx, actor, "agent.resumed", "agent", id.String(), map[string]any{"name": ag.Name, "status": nextStatus}, ip)
	s.emitEvent(domain.Event{
		Category:     domain.EventCategoryAgentLifecycle,
		EventType:    "agent.resumed",
		Severity:     "info",
		Source:       "control-plane",
		ActorLabel:   actorEmail(actor),
		ResourceType: "agent",
		ResourceID:   id.String(),
		Status:       string(nextStatus),
		Message:      fmt.Sprintf("Agent %s resumed (%s)", ag.Name, nextStatus),
		IPAddress:    ip,
	})
	s.invalidateAgentCache(ctx, id, ag.TokenHash)
	if nextStatus == domain.AgentOnline {
		s.notifySlotFreed()
	}
	return s.agentRepo.GetByID(ctx, id)
}

func (s *AgentService) GetAgentDetail(ctx context.Context, id uuid.UUID, window time.Duration) (*domain.AgentDetail, error) {
	if window <= 0 {
		window = time.Hour
	}
	key := fmt.Sprintf("agent:detail:%s:%s", id.String(), window.String())
	return cache.GetOrLoad(ctx, s.getCache(), key, 3*time.Second, func(c context.Context) (*domain.AgentDetail, error) {
		ag, err := s.agentRepo.GetByID(c, id)
		if err != nil {
			return nil, err
		}
		stats, err := s.agentRepo.GetAgentTaskStats(c, id)
		if err != nil || stats == nil {
			stats = &domain.AgentTaskStats{}
		}
		tasks, _, _ := s.agentRepo.ListTasksByAgent(c, id, domain.AgentTaskFilter{
			PageParams: domain.PageParams{Limit: 10, Offset: 0},
		})
		if tasks == nil {
			tasks = []domain.AgentTask{}
		}
		history, _ := s.GetAgentMetrics(c, id, window, 240)
		if history == nil {
			history = []domain.AgentMetricPoint{}
		}
		return &domain.AgentDetail{
			Agent:         *ag,
			Stats:         *stats,
			MetricHistory: history,
			RecentTasks:   tasks,
		}, nil
	})
}

func (s *AgentService) ListAgentTasks(ctx context.Context, id uuid.UUID, filter domain.AgentTaskFilter) ([]domain.AgentTask, int, error) {
	if _, err := s.agentRepo.GetByID(ctx, id); err != nil {
		return nil, 0, err
	}
	return s.agentRepo.ListTasksByAgent(ctx, id, filter)
}

func (s *AgentService) GetAgentMetrics(ctx context.Context, id uuid.UUID, window time.Duration, limit int) ([]domain.AgentMetricPoint, error) {
	if window <= 0 {
		window = time.Hour
	}
	s.hookMu.RLock()
	es := s.eventStore
	s.hookMu.RUnlock()
	if es == nil {
		return []domain.AgentMetricPoint{}, nil
	}
	return es.QueryAgentMetrics(ctx, id, time.Now().UTC().Add(-window), limit)
}

func actorEmail(u *domain.User) string {
	if u != nil {
		return u.Email
	}
	return "system"
}

func (s *AgentService) RotateToken(ctx context.Context, id uuid.UUID, actor *domain.User, ip string) (*RegisterAgentResponse, error) {
	existing, err := s.agentRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	rawSuffix, err := crypto.GenerateRandomToken(32)
	if err != nil {
		return nil, err
	}
	rawToken := "rwa_" + rawSuffix
	prefix := rawToken[:12]
	if err := s.agentRepo.UpdateToken(ctx, id, prefix, crypto.HashTokenSHA256(rawToken)); err != nil {
		return nil, err
	}
	s.invalidateAgentCache(ctx, id, existing.TokenHash)
	s.appendAudit(ctx, actor, "agent.token_rotated", "agent", id.String(), nil, ip)
	agent, err := s.agentRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &RegisterAgentResponse{Agent: *agent, Token: rawToken}, nil
}

func (s *AgentService) DeleteAgent(ctx context.Context, id uuid.UUID, actor *domain.User, ip string) error {
	existing, _ := s.agentRepo.GetByID(ctx, id)
	if err := s.agentRepo.Delete(ctx, id); err != nil {
		return err
	}
	var oldHash string
	if existing != nil {
		oldHash = existing.TokenHash
	}
	s.invalidateAgentCache(ctx, id, oldHash)
	s.appendAudit(ctx, actor, "agent.deleted", "agent", id.String(), nil, ip)
	return nil
}

func (s *AgentService) AuthenticateAgent(ctx context.Context, rawBearer string) (*domain.Agent, error) {
	if !strings.HasPrefix(rawBearer, "rwa_") {
		return nil, domain.ErrUnauthorized
	}
	hash := crypto.HashTokenSHA256(rawBearer)
	c := s.getCache()
	if c != nil {
		if cached, ok, err := cache.GetJSON[domain.Agent](ctx, c, "auth:agent:"+hash); err == nil && ok {
			ag := cached
			ag.TokenHash = hash
			return &ag, nil
		}
	}
	agent, err := s.agentRepo.GetByTokenHash(ctx, hash)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	if c != nil && agent != nil {
		agCopy := *agent
		agCopy.TokenHash = ""
		_ = cache.SetJSON(ctx, c, "auth:agent:"+hash, agCopy, 30*time.Second)
	}
	return agent, nil
}

type AgentHeartbeatInput struct {
	OS             string                       `json:"os"`
	Arch           string                       `json:"arch"`
	Runtime        string                       `json:"runtime"`
	SandboxRuntime string                       `json:"sandbox_runtime,omitempty"`
	Version        string                       `json:"version"`
	Hostname       string                       `json:"hostname"`
	CPUCount       int                          `json:"cpu_count"`
	MemoryMB       int                          `json:"memory_mb"`
	MaxConcurrency int                          `json:"max_concurrency"`
	ActiveJobs     int                          `json:"active_jobs"`
	Labels         []string                     `json:"labels"`
	Metrics        *domain.AgentMetricsSnapshot `json:"metrics,omitempty"`
}

type AgentHeartbeatResponse struct {
	Status        domain.AgentStatus `json:"status"`
	CancelTaskIDs []uuid.UUID        `json:"cancel_task_ids"`
}

func (s *AgentService) Heartbeat(ctx context.Context, agent *domain.Agent, in AgentHeartbeatInput) (*AgentHeartbeatResponse, error) {
	update := *agent
	if in.OS != "" {
		update.OS = strings.ToLower(strings.TrimSpace(in.OS))
	}
	if in.Arch != "" {
		update.Arch = strings.ToLower(strings.TrimSpace(in.Arch))
	}
	if in.Runtime != "" {
		update.Runtime = strings.ToLower(strings.TrimSpace(in.Runtime))
	}
	if in.SandboxRuntime != "" {
		update.SandboxRuntime = strings.ToLower(strings.TrimSpace(in.SandboxRuntime))
	}
	if in.Version != "" {
		update.Version = strings.TrimSpace(in.Version)
	}
	if in.Hostname != "" {
		update.Hostname = strings.TrimSpace(in.Hostname)
	}
	if in.CPUCount > 0 {
		update.CPUCount = in.CPUCount
	}
	if in.MemoryMB > 0 {
		update.MemoryMB = in.MemoryMB
	}
	if in.MaxConcurrency > 0 {
		update.MaxConcurrency = in.MaxConcurrency
	}
	update.ActiveJobs = in.ActiveJobs
	if len(in.Labels) > 0 {
		update.Labels = in.Labels
	}
	if in.Metrics != nil {
		if rawMetrics, mErr := json.Marshal(in.Metrics); mErr == nil {
			update.Metrics = rawMetrics
		}
	}

	wasOffline := agent.Status == domain.AgentOffline
	if err := s.agentRepo.UpdateHeartbeat(ctx, &update); err != nil {
		return nil, err
	}
	latest, err := s.agentRepo.GetByID(ctx, agent.ID)
	status := agent.Status
	if err == nil {
		status = latest.Status
	} else if status == domain.AgentOffline {
		status = domain.AgentOnline
	}

	if status == domain.AgentDraining && in.ActiveJobs == 0 {
		if transitioned, trErr := s.agentRepo.MarkDrainingCompleteIfIdle(ctx, agent.ID); trErr == nil && transitioned {
			status = domain.AgentDrained
			s.invalidateAgentCache(ctx, agent.ID, agent.TokenHash)
			s.emitEvent(domain.Event{
				Category:     domain.EventCategoryAgentLifecycle,
				EventType:    "agent.drained",
				Severity:     "info",
				Source:       "agent-heartbeat",
				ResourceType: "agent",
				ResourceID:   agent.ID.String(),
				Status:       string(domain.AgentDrained),
				Message:      fmt.Sprintf("Agent %s completed draining", agent.Name),
			})
		}
	}

	if in.Metrics != nil {
		metricPayload, _ := json.Marshal(map[string]any{
			"cpu_usage_percent":    in.Metrics.CPUUsagePercent,
			"memory_usage_percent": in.Metrics.MemoryUsagePercent,
			"disk_usage_percent":   in.Metrics.DiskUsagePercent,
			"memory_used_mb":       in.Metrics.MemoryUsedMB,
			"active_jobs":          in.ActiveJobs,
			"load_avg_1":           in.Metrics.LoadAvg1,
			"cache_hit_count":      in.Metrics.CacheHitCount,
			"cache_miss_count":     in.Metrics.CacheMissCount,
		})
		s.emitEvent(domain.Event{
			Category:     domain.EventCategoryAgentMetric,
			EventType:    "agent.metrics",
			Severity:     "info",
			Source:       "runway-agent",
			ActorLabel:   agent.Name,
			ResourceType: "agent",
			ResourceID:   agent.ID.String(),
			Status:       string(status),
			Message:      fmt.Sprintf("CPU %.1f%% · RAM %.1f%% · Disk %.1f%%", in.Metrics.CPUUsagePercent, in.Metrics.MemoryUsagePercent, in.Metrics.DiskUsagePercent),
			Payload:      metricPayload,
		})
	}

	if wasOffline && status == domain.AgentOnline {
		s.notifySlotFreed()
	}
	cancelIDs, _ := s.agentRepo.ListCancelledTaskIDsForAgent(ctx, agent.ID)
	if cancelIDs == nil {
		cancelIDs = []uuid.UUID{}
	}
	return &AgentHeartbeatResponse{
		Status:        status,
		CancelTaskIDs: cancelIDs,
	}, nil
}

type ClaimedTaskPayload struct {
	TaskID           uuid.UUID `json:"task_id"`
	RunnerID         uuid.UUID `json:"runner_id"`
	GitHubJobID      int64     `json:"github_job_id"`
	RepoFullName     string    `json:"repo_full_name"`
	RunnerName       string    `json:"runner_name"`
	PoolSlug         string    `json:"pool_slug"`
	OS               string    `json:"os"`
	Arch             string    `json:"arch"`
	Labels           []string  `json:"labels"`
	ContainerImage   string    `json:"container_image"`
	VCPU             int       `json:"vcpu"`
	MemoryMB         int       `json:"memory_mb"`
	IsForkPR         bool      `json:"is_fork_pr"`
	BuildProfile     string    `json:"build_profile"`
	CacheEnabled     bool      `json:"cache_enabled"`
	CacheScope       string    `json:"cache_scope"`
	SandboxMode      string    `json:"sandbox_mode"`
	EncodedJITConfig string    `json:"encoded_jit_config"`
}

func (s *AgentService) ClaimTask(ctx context.Context, agent *domain.Agent) (*ClaimedTaskPayload, error) {
	if agent.Status == domain.AgentDraining || agent.Status == domain.AgentDrained {
		return nil, nil
	}
	task, err := s.agentRepo.ClaimNextTask(ctx, agent)
	if err != nil || task == nil {
		return nil, err
	}
	if len(s.encKey) != 32 {
		_, _ = s.agentRepo.CompleteTask(ctx, task.ID, agent.ID, domain.AgentTaskFailed, "server encryption key unavailable")
		return nil, fmt.Errorf("agent service encryption key unavailable")
	}
	plainJIT, err := crypto.DecryptAESGCM(s.encKey, task.EncodedJITConfigEncrypted)
	if err != nil {
		_, _ = s.agentRepo.CompleteTask(ctx, task.ID, agent.ID, domain.AgentTaskFailed, "failed to decrypt jit config")
		return nil, fmt.Errorf("decrypt task jit config: %w", err)
	}

	if s.runnerRepo != nil {
		ref := fmt.Sprintf("agent:%s:%s", agent.ID.String(), task.ID.String())
		_ = s.runnerRepo.UpdateStatus(ctx, task.RunnerID, domain.RunnerOnline, nil, ref, &task.GitHubJobID)
	}
	if s.jobRepo != nil {
		if job, getErr := s.jobRepo.GetByID(ctx, task.JobID); getErr == nil {
			_ = s.jobRepo.UpdateModerationAndRunner(ctx, job.ID, domain.JobProvisioning, job.ModerationDecision, fmt.Sprintf("Claimed by agent %q (%s/%s)", agent.Name, agent.OS, agent.Arch), job.ModeratedBy, job.MatchedPoolID, &task.RunnerID)
		}
	}

	return &ClaimedTaskPayload{
		TaskID:           task.ID,
		RunnerID:         task.RunnerID,
		GitHubJobID:      task.GitHubJobID,
		RepoFullName:     task.RepoFullName,
		RunnerName:       task.RunnerName,
		PoolSlug:         task.PoolSlug,
		OS:               task.OS,
		Arch:             task.Arch,
		Labels:           task.Labels,
		ContainerImage:   task.ContainerImage,
		VCPU:             task.VCPU,
		MemoryMB:         task.MemoryMB,
		IsForkPR:         task.IsForkPR,
		BuildProfile:     task.BuildProfile,
		CacheEnabled:     task.CacheEnabled,
		CacheScope:       task.CacheScope,
		SandboxMode:      task.SandboxMode,
		EncodedJITConfig: string(plainJIT),
	}, nil
}

func (s *AgentService) IngestTaskLogs(ctx context.Context, agent *domain.Agent, taskID uuid.UUID, entries []domain.RunnerLogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	task, err := s.agentRepo.GetTaskByID(ctx, taskID)
	if err != nil {
		return err
	}
	if task.AgentID == nil || *task.AgentID != agent.ID {
		return domain.ErrForbidden
	}
	for _, entry := range entries {
		stream := entry.Stream
		if stream == "" {
			stream = "stdout"
		}
		lvl := entry.Level
		if lvl == "" {
			lvl = "info"
		}
		s.emitEvent(domain.Event{
			OccurredAt:    entry.Timestamp,
			Category:      domain.EventCategoryRunnerLog,
			EventType:     "runner.log." + stream,
			Severity:      lvl,
			Source:        "runway-agent",
			ActorLabel:    agent.Name,
			ResourceType:  "runner",
			ResourceID:    task.RunnerID.String(),
			CorrelationID: task.JobID.String(),
			RepoFullName:  task.RepoFullName,
			Status:        string(task.Status),
			Message:       entry.Line,
		})
	}
	return nil
}

func (s *AgentService) CompleteTask(ctx context.Context, agent *domain.Agent, taskID uuid.UUID, status domain.AgentTaskStatus, errMsg string) error {
	switch status {
	case domain.AgentTaskCompleted, domain.AgentTaskFailed, domain.AgentTaskCancelled:
	default:
		return fmt.Errorf("%w: invalid completion status %q", domain.ErrValidation, status)
	}

	task, err := s.agentRepo.CompleteTask(ctx, taskID, agent.ID, status, strings.TrimSpace(errMsg))
	if err != nil {
		return err
	}

	runnerStatus := domain.RunnerTerminated
	if status == domain.AgentTaskFailed {
		runnerStatus = domain.RunnerOffline
	}
	if s.runnerRepo != nil {
		_ = s.runnerRepo.UpdateStatus(ctx, task.RunnerID, runnerStatus, nil, "", nil)
	}

	if status == domain.AgentTaskFailed {
		if s.jobRepo != nil {
			if job, getErr := s.jobRepo.GetByID(ctx, task.JobID); getErr == nil {
				if job.Status == domain.JobProvisioning {
					_, _ = s.jobRepo.RequeueIfProvisioning(ctx, job.ID, "Agent execution failed before job started: "+strings.TrimSpace(errMsg))
				} else {
					_ = s.jobRepo.UpdateModerationAndRunner(ctx, job.ID, job.Status, job.ModerationDecision, "Agent execution failed: "+strings.TrimSpace(errMsg), job.ModeratedBy, job.MatchedPoolID, &task.RunnerID)
				}
			}
		}
		s.appendAudit(ctx, nil, "runner.agent_failed", "agent", agent.ID.String(), map[string]any{
			"agent":         agent.Name,
			"runner":        task.RunnerName,
			"github_job_id": task.GitHubJobID,
			"error":         strings.TrimSpace(errMsg),
		}, "")
	}
	if transitioned, trErr := s.agentRepo.MarkDrainingCompleteIfIdle(ctx, agent.ID); trErr == nil && transitioned {
		s.invalidateAgentCache(ctx, agent.ID, agent.TokenHash)
		s.emitEvent(domain.Event{
			Category:     domain.EventCategoryAgentLifecycle,
			EventType:    "agent.drained",
			Severity:     "info",
			Source:       "control-plane",
			ResourceType: "agent",
			ResourceID:   agent.ID.String(),
			Status:       string(domain.AgentDrained),
			Message:      fmt.Sprintf("Agent %s completed draining after task %s", agent.Name, task.RunnerName),
		})
	} else {
		s.invalidateAgentCache(ctx, agent.ID, "")
	}
	s.notifySlotFreed()
	return nil
}

func (s *AgentService) appendAudit(ctx context.Context, actor *domain.User, action, resType, resID string, metaMap map[string]any, ip string) {
	if s.auditRepo == nil {
		return
	}
	var actorID *uuid.UUID
	actorLabel := "system"
	if actor != nil {
		actorID = &actor.ID
		actorLabel = actor.Email
	}
	meta, _ := json.Marshal(metaMap)
	_ = s.auditRepo.AppendAuditLog(ctx, &domain.AuditLog{
		ActorID:      actorID,
		ActorLabel:   actorLabel,
		Action:       action,
		ResourceType: resType,
		ResourceID:   resID,
		Metadata:     meta,
		IPAddress:    ip,
	})
}
