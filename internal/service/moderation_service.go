package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/github"
	"github.com/cubetiqlabs/runway/internal/moderation"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
	"github.com/google/uuid"
)

type ModerationService struct {
	ghClient   *github.Client
	policyRepo domain.PolicyRepository
	jobRepo    domain.JobRepository
	poolRepo   domain.PoolRepository
	runnerRepo domain.RunnerRepository
	auditRepo  domain.AuditRepository
	engine     *moderation.Engine
	orch       *orchestrator.Orchestrator
}

func NewModerationService(
	ghClient *github.Client,
	policyRepo domain.PolicyRepository,
	jobRepo domain.JobRepository,
	poolRepo domain.PoolRepository,
	runnerRepo domain.RunnerRepository,
	auditRepo domain.AuditRepository,
	engine *moderation.Engine,
	orch *orchestrator.Orchestrator,
) *ModerationService {
	return &ModerationService{
		ghClient:   ghClient,
		policyRepo: policyRepo,
		jobRepo:    jobRepo,
		poolRepo:   poolRepo,
		runnerRepo: runnerRepo,
		auditRepo:  auditRepo,
		engine:     engine,
		orch:       orch,
	}
}

// SeedDefaultPolicies inserts a starter fork-PR protection policy if no policies exist.
func (s *ModerationService) SeedDefaultPolicies(ctx context.Context) error {
	count, err := s.policyRepo.Count(ctx)
	if err != nil || count > 0 {
		return err
	}

	starter := &domain.ModerationPolicy{
		Name:           "Protect from Untrusted Fork PRs",
		Description:    "Automatically denies workflow jobs originating from external fork pull requests while allowing internal repository runs.",
		Priority:       10,
		Enabled:        true,
		Action:         domain.ActionAllow,
		RepoPatterns:   []string{"*/*"},
		BranchPatterns: []string{"*"},
		ActorPatterns:  []string{"*"},
		BlockForkPRs:   true,
	}
	return s.policyRepo.Create(ctx, starter)
}

func (s *ModerationService) ListPolicies(ctx context.Context, page domain.PageParams) ([]domain.ModerationPolicy, int, error) {
	return s.policyRepo.List(ctx, page)
}

func (s *ModerationService) CreatePolicy(ctx context.Context, p *domain.ModerationPolicy, actor *domain.User, ip string) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: policy name is required", domain.ErrValidation)
	}
	switch p.Action {
	case domain.ActionAllow, domain.ActionDeny, domain.ActionRequireApproval:
	default:
		return fmt.Errorf("%w: invalid action %q", domain.ErrValidation, p.Action)
	}
	if err := s.policyRepo.Create(ctx, p); err != nil {
		return err
	}
	s.appendAudit(ctx, actor, "policy.created", "moderation_policy", p.ID.String(), map[string]any{"name": p.Name, "action": p.Action}, ip)
	return nil
}

func (s *ModerationService) UpdatePolicy(ctx context.Context, p *domain.ModerationPolicy, actor *domain.User, ip string) error {
	existing, err := s.policyRepo.GetByID(ctx, p.ID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(p.Name) != "" {
		existing.Name = p.Name
	}
	existing.Description = p.Description
	existing.Priority = p.Priority
	existing.Enabled = p.Enabled
	if p.Action != "" {
		switch p.Action {
		case domain.ActionAllow, domain.ActionDeny, domain.ActionRequireApproval:
			existing.Action = p.Action
		default:
			return fmt.Errorf("%w: invalid action %q", domain.ErrValidation, p.Action)
		}
	}
	existing.InstallationID = p.InstallationID
	existing.RepoPatterns = p.RepoPatterns
	existing.BranchPatterns = p.BranchPatterns
	existing.ActorPatterns = p.ActorPatterns
	existing.LabelPatterns = p.LabelPatterns
	existing.BlockForkPRs = p.BlockForkPRs
	existing.MaxConcurrentPerRepo = p.MaxConcurrentPerRepo
	existing.MaxJobDurationMin = p.MaxJobDurationMin

	if err := s.policyRepo.Update(ctx, existing); err != nil {
		return err
	}
	*p = *existing
	s.appendAudit(ctx, actor, "policy.updated", "moderation_policy", p.ID.String(), map[string]any{"name": p.Name, "enabled": p.Enabled}, ip)
	return nil
}

func (s *ModerationService) DeletePolicy(ctx context.Context, id uuid.UUID, actor *domain.User, ip string) error {
	if err := s.policyRepo.Delete(ctx, id); err != nil {
		return err
	}
	s.appendAudit(ctx, actor, "policy.deleted", "moderation_policy", id.String(), nil, ip)
	return nil
}

func (s *ModerationService) Simulate(ctx context.Context, jobCtx moderation.JobContext) (*moderation.Decision, *domain.RunnerPool, error) {
	policies, err := s.policyRepo.ListEnabledOrdered(ctx)
	if err != nil {
		return nil, nil, err
	}
	activeCount := jobCtx.ActiveRepoJobCount
	if activeCount == 0 && jobCtx.RepoFullName != "" {
		activeCount, _ = s.jobRepo.CountActiveForRepo(ctx, jobCtx.RepoFullName)
		jobCtx.ActiveRepoJobCount = activeCount
	}
	dec := s.engine.Evaluate(policies, jobCtx)

	pools, err := s.poolRepo.ListEnabled(ctx)
	if err != nil {
		return nil, nil, err
	}
	pool := moderation.MatchPool(pools, jobCtx.InstallationID, jobCtx.Labels)
	return &dec, pool, nil
}

func (s *ModerationService) ListJobs(ctx context.Context, filter domain.JobFilter) ([]domain.WorkflowJob, int, error) {
	return s.jobRepo.List(ctx, filter)
}

func (s *ModerationService) ApproveJob(ctx context.Context, jobID uuid.UUID, reason string, actor *domain.User, ip string) (*domain.WorkflowJob, error) {
	job, err := s.jobRepo.GetByID(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if job.Status != domain.JobAwaitingApproval && job.Status != domain.JobQueued {
		return nil, fmt.Errorf("%w: job in status %q cannot be approved", domain.ErrConflict, job.Status)
	}
	if strings.TrimSpace(reason) == "" {
		reason = "Manually approved by " + actor.Email
	}

	var matchedPool *domain.RunnerPool
	if job.MatchedPoolID != nil {
		matchedPool, _ = s.poolRepo.GetByID(ctx, *job.MatchedPoolID)
	}
	if matchedPool == nil {
		pools, err := s.poolRepo.ListEnabled(ctx)
		if err != nil {
			return nil, err
		}
		matchedPool = moderation.MatchPool(pools, job.InstallationID, job.Labels)
	}

	var poolID *uuid.UUID
	if matchedPool != nil {
		poolID = &matchedPool.ID
	}

	if err := s.jobRepo.UpdateModerationAndRunner(ctx, job.ID, domain.JobQueued, "approved", reason, &actor.ID, poolID, nil); err != nil {
		return nil, err
	}
	job.Status = domain.JobQueued
	job.ModerationDecision = "approved"
	job.ModerationReason = reason
	job.ModeratedBy = &actor.ID

	if matchedPool != nil {
		if err := DispatchProvisionForJob(ctx, s.ghClient, s.runnerRepo, s.orch, job, *matchedPool, ""); err != nil {
			return nil, fmt.Errorf("%w: %v", domain.ErrConflict, err)
		}
	}

	s.appendAudit(ctx, actor, "job.approved", "workflow_job", job.ID.String(), map[string]any{"reason": reason, "repo": job.RepoFullName}, ip)
	return s.jobRepo.GetByID(ctx, job.ID)
}

func (s *ModerationService) RejectJob(ctx context.Context, jobID uuid.UUID, reason string, actor *domain.User, ip string) (*domain.WorkflowJob, error) {
	job, err := s.jobRepo.GetByID(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if job.Status == domain.JobCompleted || job.Status == domain.JobRejected {
		return nil, fmt.Errorf("%w: job is already finalized", domain.ErrConflict)
	}
	if strings.TrimSpace(reason) == "" {
		reason = "Manually rejected by " + actor.Email
	}
	if err := s.jobRepo.UpdateModerationAndRunner(ctx, job.ID, domain.JobRejected, "rejected", reason, &actor.ID, nil, nil); err != nil {
		return nil, err
	}
	s.appendAudit(ctx, actor, "job.rejected", "workflow_job", job.ID.String(), map[string]any{"reason": reason, "repo": job.RepoFullName}, ip)
	return s.jobRepo.GetByID(ctx, job.ID)
}

func (s *ModerationService) appendAudit(ctx context.Context, actor *domain.User, action, resType, resID string, metaMap map[string]any, ip string) {
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
