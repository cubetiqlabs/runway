package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/crypto"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/github"
	"github.com/cubetiqlabs/runway/internal/moderation"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type WebhookService struct {
	cfg        *config.Config
	log        *zap.Logger
	ghClient   *github.Client
	instRepo   domain.InstallationRepository
	poolRepo   domain.PoolRepository
	policyRepo domain.PolicyRepository
	jobRepo    domain.JobRepository
	runnerRepo domain.RunnerRepository
	auditRepo  domain.AuditRepository
	engine     *moderation.Engine
	orch       *orchestrator.Orchestrator
	dispatcher *JobDispatcher
}

func NewWebhookService(
	cfg *config.Config,
	log *zap.Logger,
	ghClient *github.Client,
	instRepo domain.InstallationRepository,
	poolRepo domain.PoolRepository,
	policyRepo domain.PolicyRepository,
	jobRepo domain.JobRepository,
	runnerRepo domain.RunnerRepository,
	auditRepo domain.AuditRepository,
	engine *moderation.Engine,
	orch *orchestrator.Orchestrator,
	dispatcher *JobDispatcher,
) *WebhookService {
	return &WebhookService{
		cfg:        cfg,
		log:        log,
		ghClient:   ghClient,
		instRepo:   instRepo,
		poolRepo:   poolRepo,
		policyRepo: policyRepo,
		jobRepo:    jobRepo,
		runnerRepo: runnerRepo,
		auditRepo:  auditRepo,
		engine:     engine,
		orch:       orch,
		dispatcher: dispatcher,
	}
}

type WebhookInput struct {
	DeliveryID   string
	EventName    string
	Signature256 string
	RawBody      []byte
}

func (s *WebhookService) HandleGitHubWebhook(ctx context.Context, in WebhookInput) error {
	start := time.Now()
	if strings.TrimSpace(in.DeliveryID) == "" || strings.TrimSpace(in.EventName) == "" {
		return fmt.Errorf("%w: missing X-GitHub-Delivery or X-GitHub-Event header", domain.ErrValidation)
	}

	sigValid := true
	if strings.TrimSpace(s.cfg.GitHub.WebhookSecret) != "" {
		sigValid = crypto.VerifyGitHubSignature256(in.RawBody, in.Signature256, s.cfg.GitHub.WebhookSecret)
	} else if s.cfg.IsProduction() {
		sigValid = false
	}

	payloadHash := crypto.HashTokenSHA256(string(in.RawBody))
	delivery := &domain.WebhookDelivery{
		DeliveryID:     in.DeliveryID,
		EventName:      in.EventName,
		SignatureValid: sigValid,
		Status:         "received",
		PayloadHash:    payloadHash,
	}

	var meta struct {
		Action       string `json:"action"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	_ = json.Unmarshal(in.RawBody, &meta)
	delivery.Action = meta.Action
	delivery.RepoFullName = meta.Repository.FullName
	if meta.Installation.ID > 0 {
		id := meta.Installation.ID
		delivery.InstallationID = &id
	}

	inserted, err := s.auditRepo.InsertDeliveryIfAbsent(ctx, delivery)
	if err != nil {
		return err
	}
	if !inserted {
		// Duplicate delivery (idempotent retry from GitHub)
		return nil
	}

	if !sigValid {
		_ = s.auditRepo.UpdateDeliveryStatus(ctx, in.DeliveryID, "failed", "invalid HMAC-SHA256 signature", int(time.Since(start).Milliseconds()))
		return domain.ErrUnauthorized
	}

	var procErr error
	status := "processed"
	detail := ""
	switch in.EventName {
	case "ping":
		status = "processed"
	case "installation":
		procErr = s.handleInstallationEvent(ctx, in.RawBody)
	case "workflow_job":
		status, detail, procErr = s.handleWorkflowJobEvent(ctx, in.RawBody)
	default:
		status = "ignored"
		detail = fmt.Sprintf("ignored event %q", in.EventName)
	}

	ms := int(time.Since(start).Milliseconds())
	if procErr != nil {
		_ = s.auditRepo.UpdateDeliveryStatus(ctx, in.DeliveryID, "failed", procErr.Error(), ms)
		return procErr
	}
	_ = s.auditRepo.UpdateDeliveryStatus(ctx, in.DeliveryID, status, detail, ms)
	return nil
}

func (s *WebhookService) handleInstallationEvent(ctx context.Context, raw []byte) error {
	var ev github.InstallationEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return fmt.Errorf("%w: invalid installation payload", domain.ErrValidation)
	}
	if ev.Action == "deleted" {
		err := s.instRepo.DeleteByID(ctx, ev.Installation.ID)
		if err == domain.ErrNotFound {
			return nil
		}
		return err
	}
	inst := &domain.Installation{
		ID:                    ev.Installation.ID,
		AccountID:             ev.Installation.Account.ID,
		AccountLogin:          ev.Installation.Account.Login,
		AccountType:           ev.Installation.Account.Type,
		TargetType:            ev.Installation.TargetType,
		RepositoriesSelection: ev.Installation.RepositorySelection,
		Permissions:           ev.Installation.Permissions,
		SuspendedAt:           ev.Installation.SuspendedAt,
	}
	return s.instRepo.Upsert(ctx, inst)
}

func (s *WebhookService) handleWorkflowJobEvent(ctx context.Context, raw []byte) (string, string, error) {
	var ev github.WorkflowJobEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return "failed", "", fmt.Errorf("%w: invalid workflow_job payload", domain.ErrValidation)
	}

	switch ev.Action {
	case "queued":
		return s.onJobQueued(ctx, &ev)
	case "in_progress":
		now := time.Now().UTC()
		if ev.WorkflowJob.StartedAt != nil {
			now = *ev.WorkflowJob.StartedAt
		}
		if err := s.jobRepo.UpdateLifecycle(ctx, ev.WorkflowJob.ID, domain.JobInProgress, "", &now, nil); err != nil && err != domain.ErrNotFound {
			return "failed", "", err
		}
		if ev.WorkflowJob.RunnerName != "" {
			if rn, err := s.runnerRepo.GetByName(ctx, ev.WorkflowJob.RunnerName); err == nil {
				_ = s.runnerRepo.UpdateStatus(ctx, rn.ID, domain.RunnerBusy, ev.WorkflowJob.RunnerID, "", &ev.WorkflowJob.ID)
			}
		}
		return "processed", "", nil
	case "completed":
		now := time.Now().UTC()
		if ev.WorkflowJob.CompletedAt != nil {
			now = *ev.WorkflowJob.CompletedAt
		}
		status := domain.JobCompleted
		if ev.WorkflowJob.Conclusion == "cancelled" {
			status = domain.JobCancelled
		}
		if err := s.jobRepo.UpdateLifecycle(ctx, ev.WorkflowJob.ID, status, ev.WorkflowJob.Conclusion, nil, &now); err != nil && err != domain.ErrNotFound {
			return "failed", "", err
		}
		cancelled := ev.WorkflowJob.Conclusion == "cancelled"
		var released bool
		if ev.WorkflowJob.RunnerName != "" && s.runnerRepo != nil {
			if rn, err := s.runnerRepo.GetByName(ctx, ev.WorkflowJob.RunnerName); err == nil && rn.Ephemeral {
				_ = s.orch.ReleaseCompletedRunner(ctx, rn, cancelled)
				released = true
			}
		}
		if !released && s.runnerRepo != nil {
			if job, err := s.jobRepo.GetByGitHubJobID(ctx, ev.WorkflowJob.ID); err == nil && job.AssignedRunnerID != nil {
				if rn, rErr := s.runnerRepo.GetByID(ctx, *job.AssignedRunnerID); rErr == nil && rn.Ephemeral {
					_ = s.orch.ReleaseCompletedRunner(ctx, rn, cancelled)
				}
			}
		}
		if s.dispatcher != nil {
			s.dispatcher.Trigger()
		}
		return "processed", "", nil
	default:
		return "ignored", fmt.Sprintf("ignored workflow_job action %q", ev.Action), nil
	}
}

func (s *WebhookService) onJobQueued(ctx context.Context, ev *github.WorkflowJobEvent) (string, string, error) {
	var instID *int64
	if ev.Installation.ID > 0 {
		id := ev.Installation.ID
		instID = &id
		if _, err := s.instRepo.GetByID(ctx, id); err == domain.ErrNotFound {
			login := ev.Repository.Owner.Login
			if login == "" {
				login = "unknown"
			}
			ownerType := ev.Repository.Owner.Type
			if ownerType == "" {
				ownerType = "Organization"
			}
			_ = s.instRepo.Upsert(ctx, &domain.Installation{
				ID:                    id,
				AccountID:             0,
				AccountLogin:          login,
				AccountType:           ownerType,
				TargetType:            ownerType,
				RepositoriesSelection: "selected",
			})
		}
	}

	pools, err := s.poolRepo.ListEnabled(ctx)
	if err != nil {
		return "failed", "", err
	}
	matchedPool := moderation.MatchPool(pools, instID, ev.WorkflowJob.Labels)
	if matchedPool == nil {
		// Not targeted at any Runway-managed runner pool; ignore cleanly with reason for operators.
		return "ignored", fmt.Sprintf("no enabled runner pool matched job labels %v", ev.WorkflowJob.Labels), nil
	}

	isForkPR := ev.Repository.Fork
	if !isForkPR {
		for _, pr := range ev.PullRequests {
			if pr.Head.Repo.FullName != "" && pr.Base.Repo.FullName != "" && pr.Head.Repo.FullName != pr.Base.Repo.FullName {
				isForkPR = true
				break
			}
		}
	}

	policies, err := s.policyRepo.ListEnabledOrdered(ctx)
	if err != nil {
		return "failed", "", err
	}
	activeRepoCount, err := s.jobRepo.CountActiveForRepo(ctx, ev.Repository.FullName)
	if err != nil {
		return "failed", "", err
	}

	decision := s.engine.Evaluate(policies, moderation.JobContext{
		InstallationID:     instID,
		RepoFullName:       ev.Repository.FullName,
		HeadBranch:         ev.WorkflowJob.HeadBranch,
		Actor:              ev.Sender.Login,
		Labels:             ev.WorkflowJob.Labels,
		IsForkPR:           isForkPR,
		ActiveRepoJobCount: activeRepoCount,
	})

	queuedAt := ev.WorkflowJob.CreatedAt
	if queuedAt.IsZero() {
		queuedAt = time.Now().UTC()
	}

	jobStatus := domain.JobQueued
	switch decision.Action {
	case domain.ActionDeny:
		jobStatus = domain.JobRejected
	case domain.ActionRequireApproval:
		jobStatus = domain.JobAwaitingApproval
	case domain.ActionAllow:
		jobStatus = domain.JobQueued
	}

	job := &domain.WorkflowJob{
		GitHubJobID:        ev.WorkflowJob.ID,
		GitHubRunID:        ev.WorkflowJob.RunID,
		InstallationID:     instID,
		RepoFullName:       ev.Repository.FullName,
		WorkflowName:       ev.WorkflowJob.WorkflowName,
		JobName:            ev.WorkflowJob.Name,
		HeadBranch:         ev.WorkflowJob.HeadBranch,
		HeadSHA:            ev.WorkflowJob.HeadSHA,
		Actor:              ev.Sender.Login,
		IsForkPR:           isForkPR,
		Labels:             ev.WorkflowJob.Labels,
		Status:             jobStatus,
		MatchedPoolID:      &matchedPool.ID,
		MatchedPolicyID:    decision.PolicyID,
		ModerationDecision: string(decision.Action),
		ModerationReason:   decision.Reason,
		QueuedAt:           queuedAt,
	}

	if err := s.jobRepo.UpsertFromWebhook(ctx, job); err != nil {
		return "failed", "", err
	}

	meta, _ := json.Marshal(map[string]any{
		"repo":     job.RepoFullName,
		"decision": decision.Action,
		"policy":   decision.PolicyName,
		"pool":     matchedPool.Name,
	})
	_ = s.auditRepo.AppendAuditLog(ctx, &domain.AuditLog{
		ActorLabel:   "moderation-engine",
		Action:       "job." + string(decision.Action),
		ResourceType: "workflow_job",
		ResourceID:   job.ID.String(),
		Metadata:     meta,
	})

	if decision.Action == domain.ActionAllow && s.dispatcher != nil {
		if err := s.dispatcher.DispatchJob(ctx, job, *matchedPool, ev.Repository.Owner.Type); err != nil {
			if errors.Is(err, domain.ErrCapacityFull) {
				_, _ = s.jobRepo.RequeueIfProvisioning(ctx, job.ID, err.Error())
				return "processed", fmt.Sprintf("queued for pool %q (%s)", matchedPool.Slug, err.Error()), nil
			}
			s.log.Warn("failed to dispatch runner provision for queued job", zap.Error(err), zap.Int64("github_job_id", job.GitHubJobID))
			failMeta, _ := json.Marshal(map[string]any{
				"repo":          job.RepoFullName,
				"pool":          matchedPool.Name,
				"github_job_id": job.GitHubJobID,
				"error":         err.Error(),
			})
			_ = s.auditRepo.AppendAuditLog(ctx, &domain.AuditLog{
				ActorLabel:   "webhook-dispatcher",
				Action:       "job.provision_failed",
				ResourceType: "workflow_job",
				ResourceID:   job.ID.String(),
				Metadata:     failMeta,
			})
			_, _ = s.jobRepo.RequeueIfProvisioning(ctx, job.ID, "Dispatch failed: "+err.Error())
			return "failed", "", err
		}
	}

	return "processed", fmt.Sprintf("matched pool %q (%s)", matchedPool.Slug, decision.Action), nil
}

func buildRunnerName(poolSlug string, githubJobID int64) string {
	slug := strings.ToLower(strings.TrimSpace(poolSlug))
	slug = strings.TrimPrefix(slug, "runway-")
	var clean strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			clean.WriteRune(r)
		} else {
			clean.WriteByte('-')
		}
	}
	base := strings.Trim(clean.String(), "-")
	if base == "" {
		base = "runner"
	}

	suffix := fmt.Sprintf("-%d-%s", githubJobID, strings.ReplaceAll(uuid.NewString(), "-", "")[:6])
	prefix := "runway-"
	maxBase := 64 - len(prefix) - len(suffix)
	if maxBase < 1 {
		maxBase = 1
	}
	if len(base) > maxBase {
		base = strings.TrimRight(base[:maxBase], "-")
		if base == "" {
			base = "r"
		}
	}
	return prefix + base + suffix
}

func mergeRunnerLabels(pool domain.RunnerPool, jobLabels []string) []string {
	out := make([]string, 0, len(pool.Labels)+len(jobLabels)+2)
	seen := make(map[string]struct{}, len(pool.Labels)+len(jobLabels)+2)
	add := func(raw string) {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			out = append(out, trimmed)
		}
	}
	for _, l := range pool.Labels {
		add(l)
	}
	add(pool.Slug)
	for _, l := range jobLabels {
		add(l)
	}
	if len(out) == 0 {
		out = append(out, "self-hosted")
	}
	return out
}
