package repository

import (
	"context"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

type PoolRepo struct {
	db *gorm.DB
}

func NewPoolRepo(db *gorm.DB) *PoolRepo {
	return &PoolRepo{db: db}
}

func poolToModel(p *domain.RunnerPool) RunnerPoolModel {
	return RunnerPoolModel{
		ID:             p.ID,
		InstallationID: p.InstallationID,
		Name:           p.Name,
		Slug:           p.Slug,
		Description:    p.Description,
		Labels:         pq.StringArray(nonNilSlice(p.Labels)),
		OS:             p.OS,
		Arch:           p.Arch,
		VCPU:           p.VCPU,
		MemoryMB:       p.MemoryMB,
		DiskGB:         p.DiskGB,
		MinIdle:        p.MinIdle,
		MaxConcurrency: p.MaxConcurrency,
		IdleTimeoutSec: p.IdleTimeoutSec,
		RunnerGroupID:  p.RunnerGroupID,
		Provider:       p.Provider,
		ContainerImage: p.ContainerImage,
		BuildProfile:   defaultStr(p.BuildProfile, "standard"),
		CacheEnabled:   p.CacheEnabled,
		CacheScope:     defaultStr(p.CacheScope, "repo"),
		CacheMaxGB:     maxInt(p.CacheMaxGB, 20),
		SandboxMode:    defaultStr(p.SandboxMode, "auto"),
		Enabled:        p.Enabled,
	}
}

func (r *PoolRepo) Count(ctx context.Context) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&RunnerPoolModel{}).Count(&count).Error
	return int(count), mapGormError(err)
}

func (r *PoolRepo) Create(ctx context.Context, p *domain.RunnerPool) error {
	m := poolToModel(p)
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapGormError(err)
	}
	*p = m.toDomain()
	return nil
}

func (r *PoolRepo) Update(ctx context.Context, p *domain.RunnerPool) error {
	m := poolToModel(p)
	res := r.db.WithContext(ctx).Model(&RunnerPoolModel{}).
		Where("id = ?", p.ID).
		Select(
			"installation_id", "name", "slug", "description", "labels",
			"os", "arch", "vcpu", "memory_mb", "disk_gb",
			"min_idle", "max_concurrency", "idle_timeout_sec", "runner_group_id",
			"provider", "container_image", "build_profile", "cache_enabled", "cache_scope", "cache_max_gb", "sandbox_mode", "enabled",
		).
		Updates(&m)
	if res.Error != nil {
		return mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	updated, err := r.GetByID(ctx, p.ID)
	if err != nil {
		return err
	}
	*p = *updated
	return nil
}

func (r *PoolRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Delete(&RunnerPoolModel{}, "id = ?", id)
	if res.Error != nil {
		return mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PoolRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.RunnerPool, error) {
	var m RunnerPoolModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, mapGormError(err)
	}
	out := m.toDomain()
	return &out, nil
}

func (r *PoolRepo) List(ctx context.Context, filter domain.PoolFilter) ([]domain.RunnerPool, int, error) {
	q := r.db.WithContext(ctx).Model(&RunnerPoolModel{})
	if filter.Provider != "" {
		q = q.Where("provider = ?", filter.Provider)
	}
	if filter.OS != "" {
		q = q.Where("os = ?", filter.OS)
	}
	if filter.Enabled != nil {
		q = q.Where("enabled = ?", *filter.Enabled)
	}
	if like := sanitizeLike(filter.Search); like != "" {
		q = q.Where(`(name ILIKE ? ESCAPE '\' OR slug ILIKE ? ESCAPE '\' OR description ILIKE ? ESCAPE '\')`, like, like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, mapGormError(err)
	}
	sortCols := map[string]string{
		"name":            "name",
		"vcpu":            "vcpu",
		"memory_mb":       "memory_mb",
		"max_concurrency": "max_concurrency",
		"provider":        "provider",
		"created_at":      "created_at",
	}
	var models []RunnerPoolModel
	if err := q.Scopes(applySort(filter.PageParams, sortCols, "created_at ASC"), paginate(filter.PageParams)).Find(&models).Error; err != nil {
		return nil, 0, mapGormError(err)
	}
	items := make([]domain.RunnerPool, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, int(total), nil
}

func (r *PoolRepo) ListEnabled(ctx context.Context) ([]domain.RunnerPool, error) {
	var models []RunnerPoolModel
	err := r.db.WithContext(ctx).Where("enabled = TRUE").Order("created_at ASC").Find(&models).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	items := make([]domain.RunnerPool, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, nil
}
