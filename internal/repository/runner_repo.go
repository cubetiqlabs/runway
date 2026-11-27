package repository

import (
	"context"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

type RunnerRepo struct {
	db *gorm.DB
}

func NewRunnerRepo(db *gorm.DB) *RunnerRepo {
	return &RunnerRepo{db: db}
}

func (r *RunnerRepo) runnerQuery(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).
		Table("runners r").
		Select(`r.id, r.pool_id, COALESCE(p.name, '') AS pool_name, r.installation_id, r.github_runner_id,
			r.name, r.provider, r.provider_ref, r.labels, r.status, r.ephemeral,
			r.current_job_id, r.host_info, r.last_heartbeat_at, r.created_at, r.updated_at`).
		Joins("LEFT JOIN runner_pools p ON p.id = r.pool_id")
}

func (r *RunnerRepo) Create(ctx context.Context, rn *domain.Runner) error {
	m := RunnerModel{
		PoolID:          rn.PoolID,
		InstallationID:  rn.InstallationID,
		GitHubRunnerID:  rn.GitHubRunnerID,
		Name:            rn.Name,
		Provider:        rn.Provider,
		ProviderRef:     rn.ProviderRef,
		Labels:          pq.StringArray(nonNilSlice(rn.Labels)),
		Status:          string(rn.Status),
		Ephemeral:       rn.Ephemeral,
		CurrentJobID:    rn.CurrentJobID,
		HostInfo:        nonEmptyJSON(rn.HostInfo),
		LastHeartbeatAt: rn.LastHeartbeatAt,
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapGormError(err)
	}
	*rn = m.toDomain()
	return nil
}

func (r *RunnerRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.RunnerStatus, githubRunnerID *int64, providerRef string, currentJobID *int64) error {
	updates := map[string]any{
		"status":     string(status),
		"updated_at": time.Now().UTC(),
	}
	if githubRunnerID != nil {
		updates["github_runner_id"] = *githubRunnerID
	}
	if providerRef != "" {
		updates["provider_ref"] = providerRef
	}
	if currentJobID != nil {
		updates["current_job_id"] = *currentJobID
	}
	if status == domain.RunnerOnline || status == domain.RunnerBusy {
		updates["last_heartbeat_at"] = time.Now().UTC()
	}

	res := r.db.WithContext(ctx).Model(&RunnerModel{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *RunnerRepo) Heartbeat(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&RunnerModel{}).Where("id = ?", id).Updates(map[string]any{
		"last_heartbeat_at": now,
		"updated_at":        now,
	})
	if res.Error != nil {
		return mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *RunnerRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Runner, error) {
	var m RunnerModel
	if err := r.runnerQuery(ctx).Where("r.id = ?", id).Take(&m).Error; err != nil {
		return nil, mapGormError(err)
	}
	out := m.toDomain()
	return &out, nil
}

func (r *RunnerRepo) GetByName(ctx context.Context, name string) (*domain.Runner, error) {
	var m RunnerModel
	if err := r.runnerQuery(ctx).Where("r.name = ?", name).Take(&m).Error; err != nil {
		return nil, mapGormError(err)
	}
	out := m.toDomain()
	return &out, nil
}

func (r *RunnerRepo) List(ctx context.Context, filter domain.RunnerFilter) ([]domain.Runner, int, error) {
	q := r.db.WithContext(ctx).Table("runners r")
	if filter.Status != "" {
		q = q.Where("r.status = ?", filter.Status)
	}
	if filter.PoolID != nil {
		q = q.Where("r.pool_id = ?", *filter.PoolID)
	}
	if like := sanitizeLike(filter.Search); like != "" {
		q = q.Where(`r.name ILIKE ? ESCAPE '\'`, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, mapGormError(err)
	}

	var models []RunnerModel
	err := q.Select(`r.id, r.pool_id, COALESCE(p.name, '') AS pool_name, r.installation_id, r.github_runner_id,
			r.name, r.provider, r.provider_ref, r.labels, r.status, r.ephemeral,
			r.current_job_id, r.host_info, r.last_heartbeat_at, r.created_at, r.updated_at`).
		Joins("LEFT JOIN runner_pools p ON p.id = r.pool_id").
		Order("r.created_at DESC").
		Scopes(paginate(filter.PageParams)).
		Scan(&models).Error
	if err != nil {
		return nil, 0, mapGormError(err)
	}

	items := make([]domain.Runner, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, int(total), nil
}

func (r *RunnerRepo) ListActiveByProvider(ctx context.Context, provider string) ([]domain.Runner, error) {
	var models []RunnerModel
	err := r.runnerQuery(ctx).
		Where("r.provider = ? AND r.status IN ?", provider, []string{"online", "busy", "draining"}).
		Scan(&models).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	items := make([]domain.Runner, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, nil
}

func (r *RunnerRepo) CountActiveInPool(ctx context.Context, poolID uuid.UUID) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&RunnerModel{}).
		Where("pool_id = ? AND status IN ?", poolID, []string{"provisioning", "online", "busy", "draining"}).
		Count(&count).Error
	return int(count), mapGormError(err)
}

func (r *RunnerRepo) FindStaleRunners(ctx context.Context, provisioningBefore, heartbeatBefore time.Time) ([]domain.Runner, error) {
	var models []RunnerModel
	err := r.runnerQuery(ctx).
		Where("(r.status = 'provisioning' AND r.created_at < ?) OR (r.status IN ('online', 'busy', 'draining') AND COALESCE(r.last_heartbeat_at, r.created_at) < ?)",
			provisioningBefore, heartbeatBefore).
		Scan(&models).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	items := make([]domain.Runner, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, nil
}
