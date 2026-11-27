package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/eventstore"
)

// EventPublishingAuditRepo decorates domain.AuditRepository so every audit log
// and webhook delivery lifecycle transition is mirrored into the EventStore.
type EventPublishingAuditRepo struct {
	inner domain.AuditRepository
	store eventstore.Store

	mu          sync.Mutex
	deliveries  map[string]deliveryMeta
}

type deliveryMeta struct {
	eventName    string
	action       string
	repoFullName string
}

func NewEventPublishingAuditRepo(inner domain.AuditRepository, store eventstore.Store) *EventPublishingAuditRepo {
	return &EventPublishingAuditRepo{
		inner:      inner,
		store:      store,
		deliveries: make(map[string]deliveryMeta, 256),
	}
}

func (r *EventPublishingAuditRepo) InsertDeliveryIfAbsent(ctx context.Context, d *domain.WebhookDelivery) (bool, error) {
	inserted, err := r.inner.InsertDeliveryIfAbsent(ctx, d)
	if err == nil && inserted && r.store != nil && d != nil {
		r.mu.Lock()
		if len(r.deliveries) > 2048 {
			r.deliveries = make(map[string]deliveryMeta, 256)
		}
		r.deliveries[d.DeliveryID] = deliveryMeta{
			eventName:    d.EventName,
			action:       d.Action,
			repoFullName: d.RepoFullName,
		}
		r.mu.Unlock()

		payload, _ := json.Marshal(map[string]any{
			"event_name":      d.EventName,
			"action":          d.Action,
			"signature_valid": d.SignatureValid,
			"payload_hash":    d.PayloadHash,
			"installation_id": d.InstallationID,
		})
		sev := "info"
		if !d.SignatureValid {
			sev = "warn"
		}
		r.store.Publish(domain.Event{
			Category:      domain.EventCategoryWebhook,
			EventType:     "webhook.received",
			Severity:      sev,
			Source:        "github-webhook",
			ActorLabel:    "github",
			ResourceType:  "webhook_delivery",
			ResourceID:    d.DeliveryID,
			CorrelationID: d.DeliveryID,
			RepoFullName:  d.RepoFullName,
			Status:        d.Status,
			Message:       fmt.Sprintf("%s.%s received", d.EventName, d.Action),
			Payload:       payload,
		})
	}
	return inserted, err
}

func (r *EventPublishingAuditRepo) UpdateDeliveryStatus(ctx context.Context, deliveryID, status, errMsg string, processingMS int) error {
	err := r.inner.UpdateDeliveryStatus(ctx, deliveryID, status, errMsg, processingMS)
	if r.store != nil {
		r.mu.Lock()
		meta := r.deliveries[deliveryID]
		delete(r.deliveries, deliveryID)
		r.mu.Unlock()

		sev := "info"
		if status == "failed" {
			sev = "error"
		}
		payload, _ := json.Marshal(map[string]any{
			"event_name":    meta.eventName,
			"action":        meta.action,
			"processing_ms": processingMS,
			"error_message": errMsg,
		})
		msg := errMsg
		if msg == "" {
			msg = fmt.Sprintf("webhook %s (%s.%s)", status, meta.eventName, meta.action)
		}
		r.store.Publish(domain.Event{
			Category:      domain.EventCategoryWebhook,
			EventType:     "webhook." + status,
			Severity:      sev,
			Source:        "github-webhook",
			ActorLabel:    "github",
			ResourceType:  "webhook_delivery",
			ResourceID:    deliveryID,
			CorrelationID: deliveryID,
			RepoFullName:  meta.repoFullName,
			DurationMS:    int64(processingMS),
			Status:        status,
			Message:       msg,
			Payload:       payload,
		})
	}
	return err
}

func (r *EventPublishingAuditRepo) ListDeliveries(ctx context.Context, filter domain.DeliveryFilter) ([]domain.WebhookDelivery, int, error) {
	return r.inner.ListDeliveries(ctx, filter)
}

func (r *EventPublishingAuditRepo) AppendAuditLog(ctx context.Context, log *domain.AuditLog) error {
	err := r.inner.AppendAuditLog(ctx, log)
	if err == nil && r.store != nil && log != nil {
		actorID := ""
		actorType := "system"
		if log.ActorID != nil {
			actorID = log.ActorID.String()
			actorType = "user"
		}
		sev := "info"
		if containsErrorWord(log.Action) {
			sev = "error"
		}
		r.store.Publish(domain.Event{
			OccurredAt:   log.CreatedAt,
			Category:     domain.EventCategoryAudit,
			EventType:    log.Action,
			Severity:     sev,
			Source:       "control-plane",
			ActorType:    actorType,
			ActorID:      actorID,
			ActorLabel:   log.ActorLabel,
			ResourceType: log.ResourceType,
			ResourceID:   log.ResourceID,
			Status:       "recorded",
			Message:      fmt.Sprintf("%s on %s:%s by %s", log.Action, log.ResourceType, log.ResourceID, log.ActorLabel),
			Payload:      log.Metadata,
			IPAddress:    log.IPAddress,
		})
	}
	return err
}

func (r *EventPublishingAuditRepo) ListAuditLogs(ctx context.Context, filter domain.AuditFilter) ([]domain.AuditLog, int, error) {
	return r.inner.ListAuditLogs(ctx, filter)
}

func (r *EventPublishingAuditRepo) GetDashboardOverview(ctx context.Context) (*domain.DashboardOverview, error) {
	return r.inner.GetDashboardOverview(ctx)
}

func containsErrorWord(action string) bool {
	return len(action) >= 6 && (action[len(action)-6:] == "failed" || action[len(action)-6:] == "denied" || action[len(action)-8:] == "rejected")
}
