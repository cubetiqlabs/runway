package service

import (
	"context"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/eventstore"
	"github.com/cubetiqlabs/runway/internal/github"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
)

type DashboardService struct {
	cfg        *config.Config
	ghClient   *github.Client
	auditRepo  domain.AuditRepository
	orch       *orchestrator.Orchestrator
	eventStore eventstore.Store
}

func NewDashboardService(cfg *config.Config, ghClient *github.Client, auditRepo domain.AuditRepository, orch *orchestrator.Orchestrator) *DashboardService {
	return &DashboardService{cfg: cfg, ghClient: ghClient, auditRepo: auditRepo, orch: orch}
}

func (s *DashboardService) SetEventStore(es eventstore.Store) {
	s.eventStore = es
}

func (s *DashboardService) GetOverview(ctx context.Context) (*domain.DashboardOverview, error) {
	return s.auditRepo.GetDashboardOverview(ctx)
}

func (s *DashboardService) ListDeliveries(ctx context.Context, filter domain.DeliveryFilter) ([]domain.WebhookDelivery, int, error) {
	return s.auditRepo.ListDeliveries(ctx, filter)
}

func (s *DashboardService) ListAuditLogs(ctx context.Context, filter domain.AuditFilter) ([]domain.AuditLog, int, error) {
	return s.auditRepo.ListAuditLogs(ctx, filter)
}

func (s *DashboardService) ListEvents(ctx context.Context, filter domain.EventFilter) ([]domain.Event, int, error) {
	if s.eventStore == nil {
		return []domain.Event{}, 0, nil
	}
	return s.eventStore.Query(ctx, filter)
}

func (s *DashboardService) GetEventSummary(ctx context.Context, since time.Time) (*domain.EventSummary, error) {
	if s.eventStore == nil {
		return &domain.EventSummary{
			Driver:     "disabled",
			ByCategory: map[string]int64{},
			BySeverity: map[string]int64{},
		}, nil
	}
	return s.eventStore.GetSummary(ctx, since)
}

type GitHubAppSettingsStatus struct {
	AppID                int64                      `json:"app_id"`
	AppSlug              string                     `json:"app_slug"`
	APIBaseURL           string                     `json:"api_base_url"`
	WebhookSecretSet     bool                       `json:"webhook_secret_set"`
	PrivateKeyConfigured bool                       `json:"private_key_configured"`
	OrchestratorProvider string                     `json:"orchestrator_provider"`
	OrchestratorHealthy  bool                       `json:"orchestrator_healthy"`
	OrchestratorError    string                     `json:"orchestrator_error,omitempty"`
	ModerationDefault    string                     `json:"moderation_default_action"`
	EventStoreDriver     string                     `json:"eventstore_driver"`
	EventStoreHealthy    bool                       `json:"eventstore_healthy"`
	EventStoreError      string                     `json:"eventstore_error,omitempty"`
	EventStoreStats      domain.EventIngestionStats `json:"eventstore_stats"`
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

	esDriver := "disabled"
	esHealthy := true
	esErr := ""
	var esStats domain.EventIngestionStats
	if s.eventStore != nil {
		esDriver = s.eventStore.Driver()
		esStats = s.eventStore.Stats()
		if esDriver != "disabled" {
			esCtx, esCancel := context.WithTimeout(ctx, 2*time.Second)
			if err := s.eventStore.Ping(esCtx); err != nil {
				esHealthy = false
				esErr = err.Error()
			}
			esCancel()
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
		EventStoreDriver:     esDriver,
		EventStoreHealthy:    esHealthy,
		EventStoreError:      esErr,
		EventStoreStats:      esStats,
	}
}
