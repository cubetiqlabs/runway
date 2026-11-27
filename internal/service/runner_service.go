package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/eventstore"
	"github.com/cubetiqlabs/runway/internal/github"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
	"github.com/google/uuid"
)

type RunnerService struct {
	cfg        *config.Config
	ghClient   *github.Client
	instRepo   domain.InstallationRepository
	poolRepo   domain.PoolRepository
	runnerRepo domain.RunnerRepository
	jobRepo    domain.JobRepository
	auditRepo  domain.AuditRepository
	orch       *orchestrator.Orchestrator
	eventStore eventstore.Store

	hookMu      sync.RWMutex
	onSlotFreed func()
}

func NewRunnerService(
	cfg *config.Config,
	ghClient *github.Client,
	instRepo domain.InstallationRepository,
	poolRepo domain.PoolRepository,
	runnerRepo domain.RunnerRepository,
	auditRepo domain.AuditRepository,
	orch *orchestrator.Orchestrator,
) *RunnerService {
	return &RunnerService{
		cfg:        cfg,
		ghClient:   ghClient,
		instRepo:   instRepo,
		poolRepo:   poolRepo,
		runnerRepo: runnerRepo,
		auditRepo:  auditRepo,
		orch:       orch,
	}
}

func (s *RunnerService) SetJobRepo(jobRepo domain.JobRepository) {
	s.hookMu.Lock()
	s.jobRepo = jobRepo
	s.hookMu.Unlock()
}

func (s *RunnerService) SetEventStore(es eventstore.Store) {
	s.hookMu.Lock()
	s.eventStore = es
	s.hookMu.Unlock()
}

func (s *RunnerService) SetSlotFreedHook(fn func()) {
	s.hookMu.Lock()
	s.onSlotFreed = fn
	s.hookMu.Unlock()
}

func (s *RunnerService) notifySlotFreed() {
	s.hookMu.RLock()
	fn := s.onSlotFreed
	s.hookMu.RUnlock()
	if fn != nil {
		fn()
	}
}

const legacyBareRunnerImage = "ghcr.io/actions/actions-runner:latest"

// SeedDefaultPools creates initial Blacksmith-style pools on first boot and upgrades legacy bare runner images.
func (s *RunnerService) SeedDefaultPools(ctx context.Context) error {
	count, err := s.poolRepo.Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		targetImage := strings.TrimSpace(s.cfg.Orchestrator.DefaultImage)
		if targetImage != "" && targetImage != legacyBareRunnerImage {
			pools, _, listErr := s.poolRepo.List(ctx, domain.PoolFilter{PageParams: domain.PageParams{Limit: 200, Offset: 0}})
			if listErr != nil {
				return listErr
			}
			for i := range pools {
				if pools[i].ContainerImage == legacyBareRunnerImage {
					pools[i].ContainerImage = targetImage
					if upErr := s.poolRepo.Update(ctx, &pools[i]); upErr != nil {
						return upErr
					}
				}
			}
		}
		return nil
	}

	defaults := []domain.RunnerPool{
		{
			Name:           "Ubuntu 24.04 · 4 vCPU",
			Slug:           "runway-ubuntu-4vcpu",
			Description:    "Fast ephemeral x64 Linux runner with persistent repo cache and tmpfs scratch acceleration.",
			Labels:         []string{"self-hosted", "linux", "x64", "runway-ubuntu-4vcpu"},
			OS:             "linux",
			Arch:           "x64",
			VCPU:           4,
			MemoryMB:       8192,
			DiskGB:         40,
			MinIdle:        0,
			MaxConcurrency: 16,
			IdleTimeoutSec: 300,
			RunnerGroupID:  1,
			Provider:       s.cfg.Orchestrator.Provider,
			ContainerImage: s.cfg.Orchestrator.DefaultImage,
			BuildProfile:   "fast",
			CacheEnabled:   true,
			CacheScope:     "repo",
			CacheMaxGB:     20,
			SandboxMode:    "auto",
			Enabled:        true,
		},
		{
			Name:           "Ubuntu 24.04 · 8 vCPU Ultra",
			Slug:           "runway-ubuntu-8vcpu",
			Description:    "Ultra Build ephemeral runner with DinD layer cache, tmpfs workspace, BuildKit, and high parallelism.",
			Labels:         []string{"self-hosted", "linux", "x64", "runway-ubuntu-8vcpu"},
			OS:             "linux",
			Arch:           "x64",
			VCPU:           8,
			MemoryMB:       16384,
			DiskGB:         80,
			MinIdle:        0,
			MaxConcurrency: 8,
			IdleTimeoutSec: 300,
			RunnerGroupID:  1,
			Provider:       s.cfg.Orchestrator.Provider,
			ContainerImage: s.cfg.Orchestrator.DefaultImage,
			BuildProfile:   "ultra",
			CacheEnabled:   true,
			CacheScope:     "repo",
			CacheMaxGB:     40,
			SandboxMode:    "auto",
			Enabled:        true,
		},
	}

	for i := range defaults {
		if err := s.poolRepo.Create(ctx, &defaults[i]); err != nil {
			return err
		}
	}
	return nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9-]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlug.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func (s *RunnerService) ListPools(ctx context.Context, filter domain.PoolFilter) ([]domain.RunnerPool, int, error) {
	return s.poolRepo.List(ctx, filter)
}

func (s *RunnerService) CreatePool(ctx context.Context, p *domain.RunnerPool, actor *domain.User, ip string) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: pool name is required", domain.ErrValidation)
	}
	if p.Slug == "" {
		p.Slug = slugify(p.Name)
	} else {
		p.Slug = slugify(p.Slug)
	}
	if p.Slug == "" {
		return fmt.Errorf("%w: invalid pool slug", domain.ErrValidation)
	}
	if len(p.Labels) == 0 {
		p.Labels = []string{"self-hosted", "linux", p.Slug}
	}
	if p.OS == "" {
		p.OS = "linux"
	}
	if p.Arch == "" {
		p.Arch = "x64"
	}
	if p.VCPU <= 0 {
		p.VCPU = 4
	}
	if p.MemoryMB < 512 {
		p.MemoryMB = 8192
	}
	if p.DiskGB < 10 {
		p.DiskGB = 40
	}
	if p.MaxConcurrency <= 0 {
		p.MaxConcurrency = 10
	}
	if p.IdleTimeoutSec < 30 {
		p.IdleTimeoutSec = 300
	}
	if p.RunnerGroupID <= 0 {
		p.RunnerGroupID = 1
	}
	if p.Provider == "" {
		p.Provider = s.cfg.Orchestrator.Provider
	}
	if p.ContainerImage == "" {
		p.ContainerImage = s.cfg.Orchestrator.DefaultImage
	}
	p.BuildProfile = normalizeBuildProfile(p.BuildProfile)
	p.CacheScope = normalizeCacheScope(p.CacheScope)
	if p.CacheMaxGB <= 0 {
		p.CacheMaxGB = 20
	}
	if strings.TrimSpace(p.SandboxMode) == "" {
		p.SandboxMode = "auto"
	}

	if err := s.poolRepo.Create(ctx, p); err != nil {
		return err
	}
	s.appendAudit(ctx, actor, "pool.created", "runner_pool", p.ID.String(), map[string]any{"name": p.Name, "slug": p.Slug}, ip)
	return nil
}

func (s *RunnerService) UpdatePool(ctx context.Context, p *domain.RunnerPool, actor *domain.User, ip string) error {
	existing, err := s.poolRepo.GetByID(ctx, p.ID)
	if err != nil {
		return err
	}
	if p.Name != "" {
		existing.Name = p.Name
	}
	if p.Slug != "" {
		existing.Slug = slugify(p.Slug)
	}
	existing.Description = p.Description
	if len(p.Labels) > 0 {
		existing.Labels = p.Labels
	}
	if p.OS != "" {
		existing.OS = p.OS
	}
	if p.Arch != "" {
		existing.Arch = p.Arch
	}
	if p.VCPU > 0 {
		existing.VCPU = p.VCPU
	}
	if p.MemoryMB >= 512 {
		existing.MemoryMB = p.MemoryMB
	}
	if p.DiskGB >= 10 {
		existing.DiskGB = p.DiskGB
	}
	if p.MinIdle >= 0 {
		existing.MinIdle = p.MinIdle
	}
	if p.MaxConcurrency > 0 {
		existing.MaxConcurrency = p.MaxConcurrency
	}
	if p.IdleTimeoutSec >= 30 {
		existing.IdleTimeoutSec = p.IdleTimeoutSec
	}
	if p.RunnerGroupID > 0 {
		existing.RunnerGroupID = p.RunnerGroupID
	}
	if p.Provider != "" {
		existing.Provider = p.Provider
	}
	if p.ContainerImage != "" {
		existing.ContainerImage = p.ContainerImage
	}
	if p.BuildProfile != "" {
		existing.BuildProfile = normalizeBuildProfile(p.BuildProfile)
	}
	existing.CacheEnabled = p.CacheEnabled
	if p.CacheScope != "" {
		existing.CacheScope = normalizeCacheScope(p.CacheScope)
	}
	if p.CacheMaxGB > 0 {
		existing.CacheMaxGB = p.CacheMaxGB
	}
	if p.SandboxMode != "" {
		existing.SandboxMode = strings.ToLower(strings.TrimSpace(p.SandboxMode))
	}
	existing.Enabled = p.Enabled
	existing.InstallationID = p.InstallationID

	if err := s.poolRepo.Update(ctx, existing); err != nil {
		return err
	}
	*p = *existing
	s.appendAudit(ctx, actor, "pool.updated", "runner_pool", p.ID.String(), map[string]any{"name": p.Name, "enabled": p.Enabled}, ip)
	if existing.Enabled {
		s.notifySlotFreed()
	}
	return nil
}

func (s *RunnerService) DeletePool(ctx context.Context, id uuid.UUID, actor *domain.User, ip string) error {
	if err := s.poolRepo.Delete(ctx, id); err != nil {
		return err
	}
	s.appendAudit(ctx, actor, "pool.deleted", "runner_pool", id.String(), nil, ip)
	return nil
}

func (s *RunnerService) ListRunners(ctx context.Context, filter domain.RunnerFilter) ([]domain.Runner, int, error) {
	return s.runnerRepo.List(ctx, filter)
}

func (s *RunnerService) DrainRunner(ctx context.Context, id uuid.UUID, actor *domain.User, ip string) (*domain.Runner, error) {
	rn, err := s.runnerRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if rn.Status == domain.RunnerTerminated {
		return rn, nil
	}

	hasActiveJob := rn.Status == domain.RunnerBusy
	if !hasActiveJob && rn.CurrentJobID != nil {
		s.hookMu.RLock()
		jr := s.jobRepo
		s.hookMu.RUnlock()
		if jr != nil {
			if job, jobErr := jr.GetByGitHubJobID(ctx, *rn.CurrentJobID); jobErr == nil && job != nil && job.Status == domain.JobInProgress {
				hasActiveJob = true
			}
		}
	}

	if !hasActiveJob {
		if rn.CurrentJobID != nil {
			s.hookMu.RLock()
			jr := s.jobRepo
			s.hookMu.RUnlock()
			if jr != nil {
				if job, jobErr := jr.GetByGitHubJobID(ctx, *rn.CurrentJobID); jobErr == nil && job != nil && job.Status == domain.JobProvisioning {
					_, _ = jr.RequeueIfProvisioning(ctx, job.ID, fmt.Sprintf("Runner %s drained before job started; requeued", rn.Name))
				}
			}
		}
		if err := s.orch.TerminateRunner(ctx, rn); err != nil {
			return nil, err
		}
		s.appendAudit(ctx, actor, "runner.drained", "runner", rn.ID.String(), map[string]any{"name": rn.Name, "immediate": true}, ip)
		return s.runnerRepo.GetByID(ctx, id)
	}

	if err := s.runnerRepo.UpdateStatus(ctx, rn.ID, domain.RunnerDraining, nil, "", nil); err != nil {
		return nil, err
	}
	s.appendAudit(ctx, actor, "runner.drained", "runner", rn.ID.String(), map[string]any{"name": rn.Name, "immediate": false}, ip)
	return s.runnerRepo.GetByID(ctx, id)
}

func (s *RunnerService) GetRunnerLogs(ctx context.Context, id uuid.UUID, page domain.PageParams) ([]domain.Event, int, error) {
	rn, err := s.runnerRepo.GetByID(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	s.hookMu.RLock()
	es := s.eventStore
	s.hookMu.RUnlock()

	if es != nil {
		if page.Limit <= 0 {
			page.Limit = 200
		}
		if page.SortBy == "" {
			page.SortBy = "occurred_at"
			page.SortDir = "asc"
		}
		items, total, qErr := es.Query(ctx, domain.EventFilter{
			Category:     domain.EventCategoryRunnerLog,
			ResourceType: "runner",
			ResourceID:   rn.ID.String(),
			PageParams:   page,
		})
		if qErr == nil && total > 0 {
			return items, total, nil
		}
	}

	// Fallback to live Docker container logs while container is still running.
	if s.orch != nil && rn.Provider == "docker" && rn.ProviderRef != "" {
		entries, logErr := s.orch.FetchRunnerLogs(ctx, rn, 200)
		if logErr == nil && len(entries) > 0 {
			out := make([]domain.Event, 0, len(entries))
			for _, e := range entries {
				out = append(out, domain.Event{
					ID:           uuid.New(),
					OccurredAt:   e.Timestamp,
					Category:     domain.EventCategoryRunnerLog,
					EventType:    "runner.log." + e.Stream,
					Severity:     e.Level,
					Source:       "docker-live",
					ResourceType: "runner",
					ResourceID:   rn.ID.String(),
					Status:       string(rn.Status),
					Message:      e.Line,
				})
			}
			return out, len(out), nil
		}
	}
	return []domain.Event{}, 0, nil
}

func normalizeBuildProfile(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "fast", "ultra":
		return strings.ToLower(strings.TrimSpace(p))
	default:
		return "standard"
	}
}

func normalizeCacheScope(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pool", "none":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "repo"
	}
}

func (s *RunnerService) TerminateRunner(ctx context.Context, id uuid.UUID, actor *domain.User, ip string) error {
	rn, err := s.runnerRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.orch.TerminateRunner(ctx, rn); err != nil {
		return err
	}
	s.appendAudit(ctx, actor, "runner.terminated", "runner", rn.ID.String(), map[string]any{"name": rn.Name}, ip)
	s.notifySlotFreed()
	return nil
}

func (s *RunnerService) ListInstallations(ctx context.Context, filter domain.InstallationFilter) ([]domain.Installation, int, error) {
	return s.instRepo.List(ctx, filter)
}

func (s *RunnerService) SyncInstallations(ctx context.Context, actor *domain.User, ip string) (int, error) {
	if !s.ghClient.IsConfigured() {
		return 0, fmt.Errorf("%w: GitHub App credentials are not configured", domain.ErrValidation)
	}
	remote, err := s.ghClient.ListInstallations(ctx)
	if err != nil {
		return 0, err
	}
	for _, ri := range remote {
		inst := &domain.Installation{
			ID:                    ri.ID,
			AccountID:             ri.Account.ID,
			AccountLogin:          ri.Account.Login,
			AccountType:           ri.Account.Type,
			TargetType:            ri.TargetType,
			RepositoriesSelection: ri.RepositorySelection,
			Permissions:           ri.Permissions,
			SuspendedAt:           ri.SuspendedAt,
		}
		if err := s.instRepo.Upsert(ctx, inst); err != nil {
			return 0, err
		}
	}
	s.appendAudit(ctx, actor, "installation.synced", "installation", "all", map[string]any{"count": len(remote)}, ip)
	return len(remote), nil
}

func (s *RunnerService) appendAudit(ctx context.Context, actor *domain.User, action, resType, resID string, metaMap map[string]any, ip string) {
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
