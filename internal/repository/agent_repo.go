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
		Select(`a.id, a.name, a.token_prefix, a.token_hash, a.os, a.arch, a.runtime, a.labels,
			a.pool_id, COALESCE(p.name, '') AS pool_name, a.version, a.hostname, a.cpu_count,
			a.memory_mb, a.max_concurrency, a.active_jobs, a.status, a.last_heartbeat_at,
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
		Labels:          pq.StringArray(nonNilSlice(agent.Labels)),
		PoolID:          agent.PoolID,
		Version:         defaultStr(agent.Version, "dev"),
		Hostname:        agent.Hostname,
		CPUCount:        agent.CPUCount,
		MemoryMB:        agent.MemoryMB,
		MaxConcurrency:  maxInt(agent.MaxConcurrency, 2),
		ActiveJobs:      agent.ActiveJobs,
		Status:          defaultStr(string(agent.Status), string(domain.AgentOffline)),
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

func (r *AgentRepo) List(ctx context.Context, page domain.PageParams) ([]domain.Agent, int, error) {
	var total int64
	if err := r.db.WithContext(ctx).Table("agents").Count(&total).Error; err != nil {
		return nil, 0, mapGormError(err)
	}
	var models []AgentModel
	err := r.agentQuery(ctx).
		Order("a.created_at DESC").
		Scopes(paginate(page)).
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

func (r *AgentRepo) CountOnline(ctx context.Context) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&AgentModel{}).
		Where("status IN ?", []string{string(domain.AgentOnline), string(domain.AgentDraining)}).
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
	// Preserve draining status until an operator explicitly drains or deletes; offline wakes back to online.
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
	if a.PoolID != nil && (t.PoolID == nil || *t.PoolID != *a.PoolID) {
		return false
	}
	if t.OS != "" && a.OS != "" && !strings.EqualFold(t.OS, a.OS) {
		return false
	}
	if t.Arch != "" && a.Arch != "" && !strings.EqualFold(t.Arch, a.Arch) {
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
	if slug := strings.ToLower(strings.TrimSpace(t.PoolSlug)); slug != "" {
		if _, ok := advertised[slug]; ok {
			return true
		}
	}
	for _, req := range t.Labels {
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
