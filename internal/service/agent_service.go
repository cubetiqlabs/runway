package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cubetiqlabs/runway/internal/crypto"
	"github.com/cubetiqlabs/runway/internal/domain"
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
	encKey     []byte
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

func (s *AgentService) ListAgents(ctx context.Context, page domain.PageParams) ([]domain.Agent, int, error) {
	return s.agentRepo.List(ctx, page)
}

func (s *AgentService) DrainAgent(ctx context.Context, id uuid.UUID, actor *domain.User, ip string) (*domain.Agent, error) {
	if err := s.agentRepo.UpdateStatus(ctx, id, domain.AgentDraining); err != nil {
		return nil, err
	}
	s.appendAudit(ctx, actor, "agent.drained", "agent", id.String(), nil, ip)
	return s.agentRepo.GetByID(ctx, id)
}

func (s *AgentService) RotateToken(ctx context.Context, id uuid.UUID, actor *domain.User, ip string) (*RegisterAgentResponse, error) {
	rawSuffix, err := crypto.GenerateRandomToken(32)
	if err != nil {
		return nil, err
	}
	rawToken := "rwa_" + rawSuffix
	prefix := rawToken[:12]
	if err := s.agentRepo.UpdateToken(ctx, id, prefix, crypto.HashTokenSHA256(rawToken)); err != nil {
		return nil, err
	}
	s.appendAudit(ctx, actor, "agent.token_rotated", "agent", id.String(), nil, ip)
	agent, err := s.agentRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &RegisterAgentResponse{Agent: *agent, Token: rawToken}, nil
}

func (s *AgentService) DeleteAgent(ctx context.Context, id uuid.UUID, actor *domain.User, ip string) error {
	if err := s.agentRepo.Delete(ctx, id); err != nil {
		return err
	}
	s.appendAudit(ctx, actor, "agent.deleted", "agent", id.String(), nil, ip)
	return nil
}

func (s *AgentService) AuthenticateAgent(ctx context.Context, rawBearer string) (*domain.Agent, error) {
	if !strings.HasPrefix(rawBearer, "rwa_") {
		return nil, domain.ErrUnauthorized
	}
	agent, err := s.agentRepo.GetByTokenHash(ctx, crypto.HashTokenSHA256(rawBearer))
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	return agent, nil
}

type AgentHeartbeatInput struct {
	OS             string   `json:"os"`
	Arch           string   `json:"arch"`
	Runtime        string   `json:"runtime"`
	Version        string   `json:"version"`
	Hostname       string   `json:"hostname"`
	CPUCount       int      `json:"cpu_count"`
	MemoryMB       int      `json:"memory_mb"`
	MaxConcurrency int      `json:"max_concurrency"`
	ActiveJobs     int      `json:"active_jobs"`
	Labels         []string `json:"labels"`
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
	EncodedJITConfig string    `json:"encoded_jit_config"`
}

func (s *AgentService) ClaimTask(ctx context.Context, agent *domain.Agent) (*ClaimedTaskPayload, error) {
	if agent.Status == domain.AgentDraining {
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
		EncodedJITConfig: string(plainJIT),
	}, nil
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
				_ = s.jobRepo.UpdateModerationAndRunner(ctx, job.ID, job.Status, job.ModerationDecision, "Agent execution failed: "+strings.TrimSpace(errMsg), job.ModeratedBy, job.MatchedPoolID, &task.RunnerID)
			}
		}
		s.appendAudit(ctx, nil, "runner.agent_failed", "agent", agent.ID.String(), map[string]any{
			"agent":         agent.Name,
			"runner":        task.RunnerName,
			"github_job_id": task.GitHubJobID,
			"error":         strings.TrimSpace(errMsg),
		}, "")
	}
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
