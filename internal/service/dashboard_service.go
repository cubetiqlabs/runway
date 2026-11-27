package service

import (
	"context"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/github"
)

type DashboardService struct {
	cfg       *config.Config
	ghClient  *github.Client
	auditRepo domain.AuditRepository
}

func NewDashboardService(cfg *config.Config, ghClient *github.Client, auditRepo domain.AuditRepository) *DashboardService {
	return &DashboardService{cfg: cfg, ghClient: ghClient, auditRepo: auditRepo}
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
	ModerationDefault    string `json:"moderation_default_action"`
}

func (s *DashboardService) GetSettingsStatus() GitHubAppSettingsStatus {
	return GitHubAppSettingsStatus{
		AppID:                s.cfg.GitHub.AppID,
		AppSlug:              s.cfg.GitHub.AppSlug,
		APIBaseURL:           s.cfg.GitHub.APIBaseURL,
		WebhookSecretSet:     s.cfg.GitHub.WebhookSecret != "",
		PrivateKeyConfigured: s.ghClient != nil && s.ghClient.IsConfigured(),
		OrchestratorProvider: s.cfg.Orchestrator.Provider,
		ModerationDefault:    s.cfg.Moderation.DefaultAction,
	}
}
