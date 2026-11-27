package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cubetiqlabs/runway/internal/cache"
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

	cacheMu sync.RWMutex
	cache   cache.Store
}

func NewDashboardService(cfg *config.Config, ghClient *github.Client, auditRepo domain.AuditRepository, orch *orchestrator.Orchestrator) *DashboardService {
	return &DashboardService{cfg: cfg, ghClient: ghClient, auditRepo: auditRepo, orch: orch}
}

func (s *DashboardService) SetEventStore(es eventstore.Store) {
	s.cacheMu.Lock()
	s.eventStore = es
	s.cacheMu.Unlock()
}

func (s *DashboardService) SetCache(c cache.Store) {
	s.cacheMu.Lock()
	s.cache = c
	s.cacheMu.Unlock()
}

func (s *DashboardService) getCache() cache.Store {
	s.cacheMu.RLock()
	c := s.cache
	s.cacheMu.RUnlock()
	return c
}

func (s *DashboardService) getEventStore() eventstore.Store {
	s.cacheMu.RLock()
	es := s.eventStore
	s.cacheMu.RUnlock()
	return es
}

func (s *DashboardService) GetOverview(ctx context.Context) (*domain.DashboardOverview, error) {
	return cache.GetOrLoad(ctx, s.getCache(), "dashboard:overview", 5*time.Second, func(c context.Context) (*domain.DashboardOverview, error) {
		return s.auditRepo.GetDashboardOverview(c)
	})
}

func (s *DashboardService) ListDeliveries(ctx context.Context, filter domain.DeliveryFilter) ([]domain.WebhookDelivery, int, error) {
	return s.auditRepo.ListDeliveries(ctx, filter)
}

func (s *DashboardService) ListAuditLogs(ctx context.Context, filter domain.AuditFilter) ([]domain.AuditLog, int, error) {
	return s.auditRepo.ListAuditLogs(ctx, filter)
}

func (s *DashboardService) ListEvents(ctx context.Context, filter domain.EventFilter) ([]domain.Event, int, error) {
	es := s.getEventStore()
	if es == nil {
		return []domain.Event{}, 0, nil
	}
	return es.Query(ctx, filter)
}

func (s *DashboardService) GetEventSummary(ctx context.Context, since time.Time) (*domain.EventSummary, error) {
	es := s.getEventStore()
	if es == nil {
		return &domain.EventSummary{
			Driver:     "disabled",
			ByCategory: map[string]int64{},
			BySeverity: map[string]int64{},
		}, nil
	}
	mins := int(time.Since(since).Round(time.Minute).Minutes())
	key := fmt.Sprintf("dashboard:event_summary:%dm", mins)
	return cache.GetOrLoad(ctx, s.getCache(), key, 5*time.Second, func(c context.Context) (*domain.EventSummary, error) {
		return es.GetSummary(c, since)
	})
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
	CacheDriver          string                     `json:"cache_driver"`
	CacheHealthy         bool                       `json:"cache_healthy"`
	CacheError           string                     `json:"cache_error,omitempty"`
	CacheClusterEnabled  bool                       `json:"cache_cluster_enabled"`
	CacheStats           domain.CacheStats          `json:"cache_stats"`
	InstanceID           string                     `json:"instance_id"`
	ClusterNodes         []domain.ClusterNode       `json:"cluster_nodes"`
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
	if es := s.getEventStore(); es != nil {
		esDriver = es.Driver()
		esStats = es.Stats()
		if esDriver != "disabled" {
			esCtx, esCancel := context.WithTimeout(ctx, 2*time.Second)
			if err := es.Ping(esCtx); err != nil {
				esHealthy = false
				esErr = err.Error()
			}
			esCancel()
		}
	}

	cacheDriver := "disabled"
	cacheHealthy := true
	cacheErr := ""
	cacheCluster := false
	instanceID := ""
	var cacheStats domain.CacheStats
	clusterNodes := []domain.ClusterNode{}

	if c := s.getCache(); c != nil {
		cacheDriver = c.Driver()
		cacheCluster = c.ClusterEnabled()
		instanceID = c.InstanceID()
		cacheStats = c.Stats()
		if cacheDriver != "disabled" {
			cCtx, cCancel := context.WithTimeout(ctx, 2*time.Second)
			if err := c.Ping(cCtx); err != nil {
				cacheHealthy = false
				cacheErr = err.Error()
			} else if nodes, nErr := c.ListNodes(cCtx); nErr == nil && len(nodes) > 0 {
				clusterNodes = nodes
			}
			cCancel()
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
		CacheDriver:          cacheDriver,
		CacheHealthy:         cacheHealthy,
		CacheError:           cacheErr,
		CacheClusterEnabled:  cacheCluster,
		CacheStats:           cacheStats,
		InstanceID:           instanceID,
		ClusterNodes:         clusterNodes,
	}
}
