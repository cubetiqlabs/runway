package service

import (
	"context"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/github"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
)

type DashboardService struct {
	cfg       *config.Config
	ghClient  *github.Client
	auditRepo domain.AuditRepository
	orch      *orchestrator.Orchestrator
}

func NewDashboardService(cfg *config.Config, ghClient *github.Client, auditRepo domain.AuditRepository, orch *orchestrator.Orchestrator) *DashboardService {
	return &DashboardService{cfg: cfg, ghClient: ghClient, auditRepo: auditRepo, orch: orch}
}

func (s *DashboardService) GetOverview(ctx context.Context) (*domain.DashboardOverview, error) {
	return s.auditRepo.GetDashboardOverview(ctx)
}

func (s *DashboardService) ListDeliveries(ctx context.Context, page domain.PageParams) ([]domain.WebhookDelivery, int, error) {
	return s.auditRepo.ListDeliveries(ctx, page)
}

func (s *DashboardService) ListAuditLogs(ctx context.Context, page domain.PageParams) ([]domain.AuditLog, int, error) {
	return s.auditRepo.ListAuditLogs(ctx, page)
}

type GitHubAppSettingsStatus struct {
	AppID                int64  `json:"app_id"`
	AppSlug              string `json:"app_slug"`
	APIBaseURL           string `json:"api_base_url"`
	WebhookSecretSet     bool   `json:"webhook_secret_set"`
	PrivateKeyConfigured bool   `json:"private_key_configured"`
	OrchestratorProvider string `json:"orchestrator_provider"`
	OrchestratorHealthy  bool   `json:"orchestrator_healthy"`
	OrchestratorError    string `json:"orchestrator_error,omitempty"`
	ModerationDefault    string `json:"moderation_default_action"`
}

func (s *DashboardService) GetSettingsStatus(ctx context.Context) GitHubAppSettingsStatus {
	healthy := true
	orchErr := ""
	if s.orch != nil {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := s.orch.HealthCheck(checkCtx); err != nil {
			healthy = false
			orchErr = err.Error()
		}
	}

	return GitHubAppSettingsStatus{
		AppID:                s.cfg.GitHub.AppID,
		AppSlug:              s.cfg.GitHub.AppSlug,
		APIBaseURL:           s.cfg.GitHub.APIBaseURL,
		WebhookSecretSet:     s.cfg.GitHub.WebhookSecret != "",
		PrivateKeyConfigured: s.ghClient != nil && s.ghClient.IsConfigured(),
		OrchestratorProvider: s.cfg.Orchestrator.Provider,
		OrchestratorHealthy:  healthy,
		OrchestratorError:    orchErr,
		ModerationDefault:    s.cfg.Moderation.DefaultAction,
	}
}
