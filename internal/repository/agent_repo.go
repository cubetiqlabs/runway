package repository

import (
	"context"
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AgentRepo struct {
	db *gorm.DB
}

func NewAgentRepo(db *gorm.DB) *AgentRepo {
	return &AgentRepo{db: db}
}

func (r *AgentRepo) agentQuery(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).
		Table("agents a").
		Select(`a.id, a.name, a.token_prefix, a.token_hash, a.os, a.arch, a.runtime, a.sandbox_runtime, a.labels,
			a.pool_id, COALESCE(p.name, '') AS pool_name, a.version, a.hostname, a.cpu_count,
			a.memory_mb, a.max_concurrency, a.active_jobs, a.status, a.metrics, a.last_heartbeat_at,
			a.created_at, a.updated_at`).
		Joins("LEFT JOIN runner_pools p ON p.id = a.pool_id")
}

func (r *AgentRepo) Create(ctx context.Context, agent *domain.Agent) error {
	m := AgentModel{
		Name:            agent.Name,
		TokenPrefix:     agent.TokenPrefix,
		TokenHash:       agent.TokenHash,
		OS:              defaultStr(agent.OS, "linux"),
		Arch:            defaultStr(agent.Arch, "x64"),
		Runtime:         defaultStr(agent.Runtime, "native"),
		SandboxRuntime:  agent.SandboxRuntime,
		Labels:          pq.StringArray(nonNilSlice(agent.Labels)),
		PoolID:          agent.PoolID,
		Version:         defaultStr(agent.Version, "dev"),
		Hostname:        agent.Hostname,
		CPUCount:        agent.CPUCount,
		MemoryMB:        agent.MemoryMB,
		MaxConcurrency:  maxInt(agent.MaxConcurrency, 2),
		ActiveJobs:      agent.ActiveJobs,
		Status:          defaultStr(string(agent.Status), string(domain.AgentOffline)),
		Metrics:         nonEmptyJSON(agent.Metrics),
		LastHeartbeatAt: agent.LastHeartbeatAt,
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapGormError(err)
	}
	*agent = m.toDomain()
	return nil
}

func (r *AgentRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Agent, error) {
	var m AgentModel
	if err := r.agentQuery(ctx).Where("a.id = ?", id).Take(&m).Error; err != nil {
		return nil, mapGormError(err)
	}
	out := m.toDomain()
	return &out, nil
}

func (r *AgentRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.Agent, error) {
	var m AgentModel
	if err := r.agentQuery(ctx).Where("a.token_hash = ?", tokenHash).Take(&m).Error; err != nil {
		return nil, mapGormError(err)
	}
	out := m.toDomain()
	return &out, nil
}

func (r *AgentRepo) List(ctx context.Context, filter domain.AgentFilter) ([]domain.Agent, int, error) {
	q := r.db.WithContext(ctx).Table("agents a")
	if filter.Status != "" {
		q = q.Where("a.status = ?", filter.Status)
	}
	if filter.OS != "" {
		q = q.Where("a.os = ?", filter.OS)
	}
	if filter.Runtime != "" {
		q = q.Where("a.runtime = ?", filter.Runtime)
	}
	if filter.PoolID != nil {
		q = q.Where("a.pool_id = ?", *filter.PoolID)
	}
	if like := sanitizeLike(filter.Search); like != "" {
		q = q.Where(`(a.name ILIKE ? ESCAPE '\' OR a.hostname ILIKE ? ESCAPE '\')`, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, mapGormError(err)
	}

	sortCols := map[string]string{
		"name":              "a.name",
		"status":            "a.status",
		"os":                "a.os",
		"runtime":           "a.runtime",
		"active_jobs":       "a.active_jobs",
		"max_concurrency":   "a.max_concurrency",
		"last_heartbeat_at": "a.last_heartbeat_at",
		"created_at":        "a.created_at",
	}

	var models []AgentModel
	err := q.Select(`a.id, a.name, a.token_prefix, a.token_hash, a.os, a.arch, a.runtime, a.sandbox_runtime, a.labels,
			a.pool_id, COALESCE(p.name, '') AS pool_name, a.version, a.hostname, a.cpu_count,
			a.memory_mb, a.max_concurrency, a.active_jobs, a.status, a.metrics, a.last_heartbeat_at,
			a.created_at, a.updated_at`).
		Joins("LEFT JOIN runner_pools p ON p.id = a.pool_id").
		Scopes(applySort(filter.PageParams, sortCols, "a.created_at DESC"), paginate(filter.PageParams)).
		Scan(&models).Error
	if err != nil {
		return nil, 0, mapGormError(err)
	}
	items := make([]domain.Agent, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, int(total), nil
}

func (r *AgentRepo) ListByStatus(ctx context.Context, status domain.AgentStatus) ([]domain.Agent, error) {
	var models []AgentModel
	err := r.agentQuery(ctx).Where("a.status = ?", string(status)).Scan(&models).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	items := make([]domain.Agent, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, nil
}

func (r *AgentRepo) CountOnline(ctx context.Context) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&AgentModel{}).
		Where("status = ?", string(domain.AgentOnline)).
		Count(&count).Error
	return int(count), mapGormError(err)
}

func (r *AgentRepo) UpdateHeartbeat(ctx context.Context, agent *domain.Agent) error {
	now := time.Now().UTC()
	updates := map[string]any{
		"last_heartbeat_at": now,
		"updated_at":        now,
		"active_jobs":       agent.ActiveJobs,
	}
	if agent.OS != "" {
		updates["os"] = agent.OS
	}
	if agent.Arch != "" {
		updates["arch"] = agent.Arch
	}
	if agent.Runtime != "" {
		updates["runtime"] = agent.Runtime
	}
	if agent.SandboxRuntime != "" {
		updates["sandbox_runtime"] = agent.SandboxRuntime
	}
	if agent.Version != "" {
		updates["version"] = agent.Version
	}
	if agent.Hostname != "" {
		updates["hostname"] = agent.Hostname
	}
	if agent.CPUCount > 0 {
		updates["cpu_count"] = agent.CPUCount
	}
	if agent.MemoryMB > 0 {
		updates["memory_mb"] = agent.MemoryMB
	}
	if agent.MaxConcurrency > 0 {
		updates["max_concurrency"] = agent.MaxConcurrency
	}
	if len(agent.Labels) > 0 {
		updates["labels"] = pq.StringArray(agent.Labels)
	}
	if len(agent.Metrics) > 0 {
		updates["metrics"] = nonEmptyJSON(agent.Metrics)
	}
	// Preserve draining/drained status until resumed or drained complete; offline wakes back to online.
	res := r.db.WithContext(ctx).Model(&AgentModel{}).
		Where("id = ?", agent.ID).
		UpdateColumns(updates)
	if res.Error != nil {
		return mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	if err := r.db.WithContext(ctx).Model(&AgentModel{}).
		Where("id = ? AND status = ?", agent.ID, string(domain.AgentOffline)).
		Updates(map[string]any{"status": string(domain.AgentOnline), "updated_at": now}).Error; err != nil {
		return mapGormError(err)
	}
	agent.LastHeartbeatAt = &now
	return nil
}

func (r *AgentRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.AgentStatus) error {
	res := r.db.WithContext(ctx).Model(&AgentModel{}).Where("id = ?", id).Updates(map[string]any{
		"status":     string(status),
		"updated_at": time.Now().UTC(),
	})
	if res.Error != nil {
		return mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *AgentRepo) UpdateToken(ctx context.Context, id uuid.UUID, prefix, tokenHash string) error {
	res := r.db.WithContext(ctx).Model(&AgentModel{}).Where("id = ?", id).Updates(map[string]any{
		"token_prefix": prefix,
		"token_hash":   tokenHash,
		"updated_at":   time.Now().UTC(),
	})
	if res.Error != nil {
		return mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *AgentRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Delete(&AgentModel{}, "id = ?", id)
	if res.Error != nil {
		return mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *AgentRepo) MarkStaleOffline(ctx context.Context, before time.Time) (int64, error) {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&AgentModel{}).
		Where("status != ? AND (last_heartbeat_at IS NULL AND created_at < ? OR last_heartbeat_at < ?)",
			string(domain.AgentOffline), before, before).
		Updates(map[string]any{"status": string(domain.AgentOffline), "updated_at": now})
	return res.RowsAffected, mapGormError(res.Error)
}

func (r *AgentRepo) CountActiveTasksByAgent(ctx context.Context, agentID uuid.UUID) (int, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&AgentTaskModel{}).
		Where("agent_id = ? AND status IN ?", agentID, []string{string(domain.AgentTaskClaimed), string(domain.AgentTaskRunning)}).
		Count(&cnt).Error
	return int(cnt), mapGormError(err)
}

func (r *AgentRepo) MarkDrainingCompleteIfIdle(ctx context.Context, agentID uuid.UUID) (bool, error) {
	var transitioned bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ag AgentModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", agentID).Take(&ag).Error; err != nil {
			return err
		}
		if ag.Status != string(domain.AgentDraining) {
			return nil
		}
		var activeCnt int64
		if err := tx.Model(&AgentTaskModel{}).
			Where("agent_id = ? AND status IN ?", agentID, []string{string(domain.AgentTaskClaimed), string(domain.AgentTaskRunning)}).
			Count(&activeCnt).Error; err != nil {
			return err
		}
		if activeCnt > 0 {
			return nil
		}
		now := time.Now().UTC()
		res := tx.Model(&AgentModel{}).
			Where("id = ? AND status = ?", agentID, string(domain.AgentDraining)).
			Updates(map[string]any{
				"status":      string(domain.AgentDrained),
				"active_jobs": 0,
				"updated_at":  now,
			})
		if res.Error != nil {
			return res.Error
		}
		transitioned = res.RowsAffected > 0
		return nil
	})
	if err != nil {
		return false, mapGormError(err)
	}
	return transitioned, nil
}

func (r *AgentRepo) ListTasksByAgent(ctx context.Context, agentID uuid.UUID, filter domain.AgentTaskFilter) ([]domain.AgentTask, int, error) {
	q := r.db.WithContext(ctx).Model(&AgentTaskModel{}).Where("agent_id = ?", agentID)
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if like := sanitizeLike(filter.Search); like != "" {
		q = q.Where(`(repo_full_name ILIKE ? ESCAPE '\' OR runner_name ILIKE ? ESCAPE '\' OR pool_slug ILIKE ? ESCAPE '\')`, like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, mapGormError(err)
	}

	sortCols := map[string]string{
		"created_at":     "created_at",
		"claimed_at":     "claimed_at",
		"completed_at":   "completed_at",
		"status":         "status",
		"repo_full_name": "repo_full_name",
		"runner_name":    "runner_name",
	}

	var models []AgentTaskModel
	if err := q.Scopes(applySort(filter.PageParams, sortCols, "created_at DESC"), paginate(filter.PageParams)).Find(&models).Error; err != nil {
		return nil, 0, mapGormError(err)
	}
	items := make([]domain.AgentTask, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, int(total), nil
}

func (r *AgentRepo) GetAgentTaskStats(ctx context.Context, agentID uuid.UUID) (*domain.AgentTaskStats, error) {
	type statsRow struct {
		TotalTasks     int64   `gorm:"column:total_tasks"`
		CompletedTasks int64   `gorm:"column:completed_tasks"`
		FailedTasks    int64   `gorm:"column:failed_tasks"`
		CancelledTasks int64   `gorm:"column:cancelled_tasks"`
		ActiveTasks    int64   `gorm:"column:active_tasks"`
		Tasks24h       int64   `gorm:"column:tasks_24h"`
		AvgDurationMS  float64 `gorm:"column:avg_duration_ms"`
		P95DurationMS  float64 `gorm:"column:p95_duration_ms"`
	}
	var row statsRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*)::BIGINT AS total_tasks,
			COUNT(*) FILTER (WHERE status = 'completed')::BIGINT AS completed_tasks,
			COUNT(*) FILTER (WHERE status = 'failed')::BIGINT AS failed_tasks,
			COUNT(*) FILTER (WHERE status = 'cancelled')::BIGINT AS cancelled_tasks,
			COUNT(*) FILTER (WHERE status IN ('claimed', 'running'))::BIGINT AS active_tasks,
			COUNT(*) FILTER (WHERE created_at > NOW() - INTERVAL '24 hours')::BIGINT AS tasks_24h,
			COALESCE(AVG(EXTRACT(EPOCH FROM (completed_at - COALESCE(claimed_at, created_at))) * 1000) FILTER (WHERE completed_at IS NOT NULL), 0) AS avg_duration_ms,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (completed_at - COALESCE(claimed_at, created_at))) * 1000) FILTER (WHERE completed_at IS NOT NULL), 0) AS p95_duration_ms
		FROM agent_tasks
		WHERE agent_id = ?`, agentID,
	).Scan(&row).Error
	if err != nil {
		return nil, mapGormError(err)
	}

	finished := row.CompletedTasks + row.FailedTasks
	successRate := 0.0
	if finished > 0 {
		successRate = float64(row.CompletedTasks) * 100.0 / float64(finished)
	}
	return &domain.AgentTaskStats{
		TotalTasks:     row.TotalTasks,
		CompletedTasks: row.CompletedTasks,
		FailedTasks:    row.FailedTasks,
		CancelledTasks: row.CancelledTasks,
		ActiveTasks:    row.ActiveTasks,
		Tasks24h:       row.Tasks24h,
		SuccessRate:    successRate,
		AvgDurationMS:  int64(row.AvgDurationMS),
		P95DurationMS:  int64(row.P95DurationMS),
	}, nil
}

func (r *AgentRepo) CreateTask(ctx context.Context, task *domain.AgentTask) error {
	m := AgentTaskModel{
		AgentID:                   task.AgentID,
		RunnerID:                  task.RunnerID,
		PoolID:                    task.PoolID,
		JobID:                     task.JobID,
		GitHubJobID:               task.GitHubJobID,
		RepoFullName:              task.RepoFullName,
		RunnerName:                task.RunnerName,
		PoolSlug:                  task.PoolSlug,
		OS:                        defaultStr(task.OS, "linux"),
		Arch:                      defaultStr(task.Arch, "x64"),
		Labels:                    pq.StringArray(nonNilSlice(task.Labels)),
		ContainerImage:            task.ContainerImage,
		VCPU:                      maxInt(task.VCPU, 1),
		MemoryMB:                  maxInt(task.MemoryMB, 512),
		IsForkPR:                  task.IsForkPR,
		BuildProfile:              defaultStr(task.BuildProfile, "standard"),
		CacheEnabled:              task.CacheEnabled,
		CacheScope:                defaultStr(task.CacheScope, "repo"),
		SandboxMode:               defaultStr(task.SandboxMode, "auto"),
		EncodedJITConfigEncrypted: task.EncodedJITConfigEncrypted,
		Status:                    defaultStr(string(task.Status), string(domain.AgentTaskPending)),
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapGormError(err)
	}
	*task = m.toDomain()
	return nil
}

func (r *AgentRepo) GetTaskByID(ctx context.Context, id uuid.UUID) (*domain.AgentTask, error) {
	var m AgentTaskModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).Take(&m).Error; err != nil {
		return nil, mapGormError(err)
	}
	out := m.toDomain()
	return &out, nil
}

func (r *AgentRepo) ClaimNextTask(ctx context.Context, agent *domain.Agent) (*domain.AgentTask, error) {
	var claimed *domain.AgentTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidates []AgentTaskModel
		q := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ?", string(domain.AgentTaskPending)).
			Order("created_at ASC").
			Limit(25)
		if agent.PoolID != nil {
			q = q.Where("pool_id = ?", *agent.PoolID)
		}
		if err := q.Find(&candidates).Error; err != nil {
			return err
		}

		var chosen *AgentTaskModel
		for i := range candidates {
			if taskCompatibleWithAgent(&candidates[i], agent) {
				chosen = &candidates[i]
				break
			}
		}
		if chosen == nil {
			return nil
		}

		now := time.Now().UTC()
		if err := tx.Model(&AgentTaskModel{}).
			Where("id = ?", chosen.ID).
			Updates(map[string]any{
				"agent_id":   agent.ID,
				"status":     string(domain.AgentTaskClaimed),
				"claimed_at": now,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}

		chosen.AgentID = &agent.ID
		chosen.Status = string(domain.AgentTaskClaimed)
		chosen.ClaimedAt = &now
		chosen.UpdatedAt = now

		var activeCount int64
		if err := tx.Model(&AgentTaskModel{}).
			Where("agent_id = ? AND status IN ?", agent.ID, []string{string(domain.AgentTaskClaimed), string(domain.AgentTaskRunning)}).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if err := tx.Model(&AgentModel{}).
			Where("id = ?", agent.ID).
			Updates(map[string]any{"active_jobs": int(activeCount), "updated_at": now}).Error; err != nil {
			return err
		}

		dom := chosen.toDomain()
		claimed = &dom
		return nil
	})
	if err != nil {
		return nil, mapGormError(err)
	}
	return claimed, nil
}

func taskCompatibleWithAgent(t *AgentTaskModel, a *domain.Agent) bool {
	return agentMatchesPoolSpec(a, t.PoolID, t.PoolSlug, t.OS, t.Arch, []string(t.Labels))
}

func agentMatchesPoolSpec(a *domain.Agent, poolID *uuid.UUID, poolSlug, osName, archName string, labels []string) bool {
	if a.PoolID != nil && (poolID == nil || *a.PoolID != *poolID) {
		return false
	}
	if osName != "" && a.OS != "" && !strings.EqualFold(osName, a.OS) {
		return false
	}
	if archName != "" && a.Arch != "" && !strings.EqualFold(archName, a.Arch) {
		return false
	}
	if len(a.Labels) == 0 {
		return true
	}
	advertised := make(map[string]struct{}, len(a.Labels)+4)
	for _, lbl := range a.Labels {
		if norm := strings.ToLower(strings.TrimSpace(lbl)); norm != "" {
			advertised[norm] = struct{}{}
		}
	}
	for _, implicit := range []string{a.OS, a.Arch, "self-hosted"} {
		if norm := strings.ToLower(strings.TrimSpace(implicit)); norm != "" {
			advertised[norm] = struct{}{}
		}
	}
	if slug := strings.ToLower(strings.TrimSpace(poolSlug)); slug != "" {
		if _, ok := advertised[slug]; ok {
			return true
		}
	}
	for _, req := range labels {
		norm := strings.ToLower(strings.TrimSpace(req))
		if norm == "" {
			continue
		}
		if _, ok := advertised[norm]; !ok {
			return false
		}
	}
	return true
}

func (r *AgentRepo) CompleteTask(ctx context.Context, taskID, agentID uuid.UUID, status domain.AgentTaskStatus, errMsg string) (*domain.AgentTask, error) {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&AgentTaskModel{}).
		Where("id = ? AND agent_id = ?", taskID, agentID).
		Updates(map[string]any{
			"status":                       string(status),
			"error_message":                errMsg,
			"encoded_jit_config_encrypted": nil,
			"completed_at":                 now,
			"updated_at":                   now,
		})
	if res.Error != nil {
		return nil, mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, domain.ErrNotFound
	}
	var activeCount int64
	if countErr := r.db.WithContext(ctx).Model(&AgentTaskModel{}).
		Where("agent_id = ? AND status IN ?", agentID, []string{string(domain.AgentTaskClaimed), string(domain.AgentTaskRunning)}).
		Count(&activeCount).Error; countErr == nil {
		_ = r.db.WithContext(ctx).Model(&AgentModel{}).
			Where("id = ?", agentID).
			Updates(map[string]any{"active_jobs": int(activeCount), "updated_at": now}).Error
	}
	return r.GetTaskByID(ctx, taskID)
}

func (r *AgentRepo) CancelTasksByRunner(ctx context.Context, runnerID uuid.UUID) error {
	now := time.Now().UTC()
	err := r.db.WithContext(ctx).Model(&AgentTaskModel{}).
		Where("runner_id = ? AND status IN ?", runnerID, []string{string(domain.AgentTaskPending), string(domain.AgentTaskClaimed), string(domain.AgentTaskRunning)}).
		Updates(map[string]any{
			"status":                       string(domain.AgentTaskCancelled),
			"encoded_jit_config_encrypted": nil,
			"completed_at":                 now,
			"updated_at":                   now,
		}).Error
	return mapGormError(err)
}

func (r *AgentRepo) CancelTaskByID(ctx context.Context, taskID uuid.UUID) error {
	now := time.Now().UTC()
	err := r.db.WithContext(ctx).Model(&AgentTaskModel{}).
		Where("id = ? AND status IN ?", taskID, []string{string(domain.AgentTaskPending), string(domain.AgentTaskClaimed), string(domain.AgentTaskRunning)}).
		Updates(map[string]any{
			"status":                       string(domain.AgentTaskCancelled),
			"encoded_jit_config_encrypted": nil,
			"completed_at":                 now,
			"updated_at":                   now,
		}).Error
	return mapGormError(err)
}

func (r *AgentRepo) ListCancelledTaskIDsForAgent(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	since := time.Now().UTC().Add(-15 * time.Minute)
	err := r.db.WithContext(ctx).Model(&AgentTaskModel{}).
		Where("agent_id = ? AND status = ? AND updated_at > ?", agentID, string(domain.AgentTaskCancelled), since).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	return ids, nil
}

func (r *AgentRepo) ExpirePendingTasks(ctx context.Context, before time.Time) ([]domain.AgentTask, error) {
	var expired []domain.AgentTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []AgentTaskModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND created_at < ?", string(domain.AgentTaskPending), before).
			Find(&models).Error; err != nil {
			return err
		}
		if len(models) == 0 {
			return nil
		}
		now := time.Now().UTC()
		ids := make([]uuid.UUID, 0, len(models))
		for _, m := range models {
			ids = append(ids, m.ID)
			expired = append(expired, m.toDomain())
		}
		return tx.Model(&AgentTaskModel{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"status":                       string(domain.AgentTaskFailed),
				"error_message":                "Provisioning timed out waiting for an online runway-agent",
				"encoded_jit_config_encrypted": nil,
				"completed_at":                 now,
				"updated_at":                   now,
			}).Error
	})
	if err != nil {
		return nil, mapGormError(err)
	}
	return expired, nil
}

func (r *AgentRepo) CountAvailableSlotsForPool(ctx context.Context, pool domain.RunnerPool) (int, int, error) {
	var agentModels []AgentModel
	if err := r.db.WithContext(ctx).
		Where("status = ?", string(domain.AgentOnline)).
		Find(&agentModels).Error; err != nil {
		return 0, 0, mapGormError(err)
	}

	compatible := make([]domain.Agent, 0, len(agentModels))
	agentIDs := make([]uuid.UUID, 0, len(agentModels))
	for _, m := range agentModels {
		ag := m.toDomain()
		if agentMatchesPoolSpec(&ag, &pool.ID, pool.Slug, pool.OS, pool.Arch, pool.Labels) {
			compatible = append(compatible, ag)
			agentIDs = append(agentIDs, ag.ID)
		}
	}
	if len(compatible) == 0 {
		return 0, 0, nil
	}

	type agentCount struct {
		AgentID uuid.UUID `gorm:"column:agent_id"`
		Cnt     int       `gorm:"column:cnt"`
	}
	var counts []agentCount
	if err := r.db.WithContext(ctx).
		Model(&AgentTaskModel{}).
		Select("agent_id, COUNT(*) AS cnt").
		Where("status IN ? AND agent_id IN ?", []string{string(domain.AgentTaskClaimed), string(domain.AgentTaskRunning)}, agentIDs).
		Group("agent_id").
		Scan(&counts).Error; err != nil {
		return 0, 0, mapGormError(err)
	}
	activeByAgent := make(map[uuid.UUID]int, len(counts))
	for _, c := range counts {
		activeByAgent[c.AgentID] = c.Cnt
	}

	freeSlots := 0
	for _, ag := range compatible {
		maxConc := maxInt(ag.MaxConcurrency, 1)
		if rem := maxConc - activeByAgent[ag.ID]; rem > 0 {
			freeSlots += rem
		}
	}
	if freeSlots <= 0 {
		return 0, len(compatible), nil
	}

	type provisioningDemandRow struct {
		PoolID   *uuid.UUID     `gorm:"column:pool_id"`
		Labels   pq.StringArray `gorm:"column:labels;type:text[]"`
		PoolSlug string         `gorm:"column:pool_slug"`
		OS       string         `gorm:"column:os"`
		Arch     string         `gorm:"column:arch"`
		PoolLbls pq.StringArray `gorm:"column:pool_labels;type:text[]"`
	}
	var demands []provisioningDemandRow
	if err := r.db.WithContext(ctx).
		Table("runners r").
		Select(`r.pool_id, r.labels, COALESCE(p.slug, '') AS pool_slug, COALESCE(p.os, 'linux') AS os, COALESCE(p.arch, 'x64') AS arch, COALESCE(p.labels, '{}') AS pool_labels`).
		Joins("LEFT JOIN runner_pools p ON p.id = r.pool_id").
		Where("r.provider = ? AND r.status = ? AND NOT EXISTS (SELECT 1 FROM agent_tasks t WHERE t.runner_id = r.id AND t.status IN ?)",
			"agent", string(domain.RunnerProvisioning), []string{string(domain.AgentTaskClaimed), string(domain.AgentTaskRunning)}).
		Scan(&demands).Error; err != nil {
		return 0, 0, mapGormError(err)
	}

	for _, d := range demands {
		lbls := []string(d.Labels)
		if len(lbls) == 0 {
			lbls = []string(d.PoolLbls)
		}
		for i := range compatible {
			if agentMatchesPoolSpec(&compatible[i], d.PoolID, d.PoolSlug, d.OS, d.Arch, lbls) {
				freeSlots--
				break
			}
		}
		if freeSlots <= 0 {
			return 0, len(compatible), nil
		}
	}
	return freeSlots, len(compatible), nil
}

func (r *AgentRepo) FailTasksForOfflineAgents(ctx context.Context) ([]domain.AgentTask, error) {
	var failed []domain.AgentTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []AgentTaskModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status IN ? AND agent_id IN (SELECT id FROM agents WHERE status = ?)",
				[]string{string(domain.AgentTaskClaimed), string(domain.AgentTaskRunning)},
				string(domain.AgentOffline)).
			Find(&models).Error; err != nil {
			return err
		}
		if len(models) == 0 {
			return nil
		}
		now := time.Now().UTC()
		ids := make([]uuid.UUID, 0, len(models))
		agentIDSet := make(map[uuid.UUID]struct{}, len(models))
		for _, m := range models {
			ids = append(ids, m.ID)
			if m.AgentID != nil {
				agentIDSet[*m.AgentID] = struct{}{}
			}
			failed = append(failed, m.toDomain())
		}
		if err := tx.Model(&AgentTaskModel{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"status":                       string(domain.AgentTaskFailed),
				"error_message":                "Assigned runway-agent went offline",
				"encoded_jit_config_encrypted": nil,
				"completed_at":                 now,
				"updated_at":                   now,
			}).Error; err != nil {
			return err
		}
		if len(agentIDSet) > 0 {
			agentIDs := make([]uuid.UUID, 0, len(agentIDSet))
			for id := range agentIDSet {
				agentIDs = append(agentIDs, id)
			}
			if err := tx.Model(&AgentModel{}).
				Where("id IN ? AND status = ?", agentIDs, string(domain.AgentOffline)).
				Updates(map[string]any{"active_jobs": 0, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, mapGormError(err)
	}
	return failed, nil
}

func defaultStr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func maxInt(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}
