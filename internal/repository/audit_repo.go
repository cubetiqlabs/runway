package repository

import (
	"context"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AuditRepo struct {
	db *gorm.DB
}

func NewAuditRepo(db *gorm.DB) *AuditRepo {
	return &AuditRepo{db: db}
}

func (r *AuditRepo) InsertDeliveryIfAbsent(ctx context.Context, d *domain.WebhookDelivery) (bool, error) {
	m := WebhookDeliveryModel{
		DeliveryID:     d.DeliveryID,
		EventName:      d.EventName,
		Action:         d.Action,
		InstallationID: d.InstallationID,
		RepoFullName:   d.RepoFullName,
		SignatureValid: d.SignatureValid,
		Status:         d.Status,
		ErrorMessage:   d.ErrorMessage,
		PayloadHash:    d.PayloadHash,
		ProcessingMS:   d.ProcessingMS,
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "delivery_id"}},
		DoNothing: true,
	}).Create(&m)
	if res.Error != nil {
		return false, mapGormError(res.Error)
	}
	return res.RowsAffected > 0, nil
}

func (r *AuditRepo) UpdateDeliveryStatus(ctx context.Context, deliveryID, status, errMsg string, processingMS int) error {
	err := r.db.WithContext(ctx).Model(&WebhookDeliveryModel{}).
		Where("delivery_id = ?", deliveryID).
		Updates(map[string]any{
			"status":        status,
			"error_message": errMsg,
			"processing_ms": processingMS,
		}).Error
	return mapGormError(err)
}

func (r *AuditRepo) ListDeliveries(ctx context.Context, page domain.PageParams) ([]domain.WebhookDelivery, int, error) {
	var total int64
	q := r.db.WithContext(ctx).Model(&WebhookDeliveryModel{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, mapGormError(err)
	}
	var models []WebhookDeliveryModel
	if err := q.Order("received_at DESC").Scopes(paginate(page)).Find(&models).Error; err != nil {
		return nil, 0, mapGormError(err)
	}
	items := make([]domain.WebhookDelivery, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, int(total), nil
}

func (r *AuditRepo) AppendAuditLog(ctx context.Context, log *domain.AuditLog) error {
	m := AuditLogModel{
		ActorID:      log.ActorID,
		ActorLabel:   log.ActorLabel,
		Action:       log.Action,
		ResourceType: log.ResourceType,
		ResourceID:   log.ResourceID,
		Metadata:     nonEmptyJSON(log.Metadata),
		IPAddress:    log.IPAddress,
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapGormError(err)
	}
	*log = m.toDomain()
	return nil
}

func (r *AuditRepo) ListAuditLogs(ctx context.Context, page domain.PageParams) ([]domain.AuditLog, int, error) {
	var total int64
	q := r.db.WithContext(ctx).Model(&AuditLogModel{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, mapGormError(err)
	}
	var models []AuditLogModel
	if err := q.Order("created_at DESC").Scopes(paginate(page)).Find(&models).Error; err != nil {
		return nil, 0, mapGormError(err)
	}
	items := make([]domain.AuditLog, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, int(total), nil
}

func (r *AuditRepo) GetDashboardOverview(ctx context.Context) (*domain.DashboardOverview, error) {
	ov := &domain.DashboardOverview{
		PoolUtilization:  []domain.PoolUtilization{},
		RecentJobs:       []domain.WorkflowJob{},
		RecentDeliveries: []domain.WebhookDelivery{},
	}

	type countsRow struct {
		TotalRunners      int `gorm:"column:total_runners"`
		OnlineRunners     int `gorm:"column:online_runners"`
		BusyRunners       int `gorm:"column:busy_runners"`
		ProvisioningCount int `gorm:"column:provisioning_count"`
		ActiveJobs        int `gorm:"column:active_jobs"`
		AwaitingApproval  int `gorm:"column:awaiting_approval"`
		DeniedJobs24h     int `gorm:"column:denied_jobs24h"`
		CompletedJobs24h  int `gorm:"column:completed_jobs24h"`
		AvgQueueLatencyMS int `gorm:"column:avg_queue_latency_ms"`
		Installations     int `gorm:"column:installations"`
	}
	var counts countsRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			(SELECT COUNT(*) FROM runners WHERE status <> 'terminated') AS total_runners,
			(SELECT COUNT(*) FROM runners WHERE status = 'online') AS online_runners,
			(SELECT COUNT(*) FROM runners WHERE status = 'busy') AS busy_runners,
			(SELECT COUNT(*) FROM runners WHERE status = 'provisioning') AS provisioning_count,
			(SELECT COUNT(*) FROM workflow_jobs WHERE status IN ('queued', 'provisioning', 'in_progress')) AS active_jobs,
			(SELECT COUNT(*) FROM workflow_jobs WHERE status = 'awaiting_approval') AS awaiting_approval,
			(SELECT COUNT(*) FROM workflow_jobs WHERE status = 'rejected' AND created_at > NOW() - INTERVAL '24 hours') AS denied_jobs24h,
			(SELECT COUNT(*) FROM workflow_jobs WHERE status = 'completed' AND created_at > NOW() - INTERVAL '24 hours') AS completed_jobs24h,
			COALESCE((SELECT ROUND(AVG(EXTRACT(EPOCH FROM (started_at - queued_at)) * 1000))::INT FROM workflow_jobs WHERE started_at IS NOT NULL AND queued_at > NOW() - INTERVAL '24 hours'), 0) AS avg_queue_latency_ms,
			(SELECT COUNT(*) FROM installations WHERE suspended_at IS NULL) AS installations
	`).Scan(&counts).Error
	if err != nil {
		return nil, mapGormError(err)
	}

	ov.TotalRunners = counts.TotalRunners
	ov.OnlineRunners = counts.OnlineRunners
	ov.BusyRunners = counts.BusyRunners
	ov.ProvisioningCount = counts.ProvisioningCount
	ov.ActiveJobs = counts.ActiveJobs
	ov.AwaitingApproval = counts.AwaitingApproval
	ov.DeniedJobs24h = counts.DeniedJobs24h
	ov.CompletedJobs24h = counts.CompletedJobs24h
	ov.AvgQueueLatencyMS = counts.AvgQueueLatencyMS
	ov.Installations = counts.Installations

	type poolUtilRow struct {
		PoolID         string `gorm:"column:pool_id"`
		Name           string `gorm:"column:name"`
		OS             string `gorm:"column:os"`
		Arch           string `gorm:"column:arch"`
		VCPU           int    `gorm:"column:vcpu"`
		MemoryMB       int    `gorm:"column:memory_mb"`
		OnlineRunners  int    `gorm:"column:online_runners"`
		BusyRunners    int    `gorm:"column:busy_runners"`
		MaxConcurrency int    `gorm:"column:max_concurrency"`
		Enabled        bool   `gorm:"column:enabled"`
	}
	var poolRows []poolUtilRow
	err = r.db.WithContext(ctx).Raw(`
		SELECT
			p.id::text AS pool_id, p.name, p.os, p.arch, p.vcpu, p.memory_mb,
			COUNT(r.id) FILTER (WHERE r.status = 'online')::INT AS online_runners,
			COUNT(r.id) FILTER (WHERE r.status = 'busy')::INT AS busy_runners,
			p.max_concurrency, p.enabled
		FROM runner_pools p
		LEFT JOIN runners r ON r.pool_id = p.id
		GROUP BY p.id
		ORDER BY p.created_at ASC
	`).Scan(&poolRows).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	for _, pr := range poolRows {
		pid, parseErr := uuid.Parse(pr.PoolID)
		if parseErr != nil {
			continue
		}
		ov.PoolUtilization = append(ov.PoolUtilization, domain.PoolUtilization{
			PoolID:         pid,
			Name:           pr.Name,
			OS:             pr.OS,
			Arch:           pr.Arch,
			VCPU:           pr.VCPU,
			MemoryMB:       pr.MemoryMB,
			OnlineRunners:  pr.OnlineRunners,
			BusyRunners:    pr.BusyRunners,
			MaxConcurrency: pr.MaxConcurrency,
			Enabled:        pr.Enabled,
		})
	}

	jobRepo := NewJobRepo(r.db)
	recentJobs, _, err := jobRepo.List(ctx, domain.JobFilter{PageParams: domain.PageParams{Limit: 8}})
	if err != nil {
		return nil, err
	}
	ov.RecentJobs = recentJobs

	recentDeliveries, _, err := r.ListDeliveries(ctx, domain.PageParams{Limit: 8})
	if err != nil {
		return nil, err
	}
	ov.RecentDeliveries = recentDeliveries

	return ov, nil
}
