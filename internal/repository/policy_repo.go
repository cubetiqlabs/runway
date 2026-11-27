package repository

import (
	"context"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

type PolicyRepo struct {
	db *gorm.DB
}

func NewPolicyRepo(db *gorm.DB) *PolicyRepo {
	return &PolicyRepo{db: db}
}

func policyToModel(p *domain.ModerationPolicy) ModerationPolicyModel {
	return ModerationPolicyModel{
		ID:                   p.ID,
		InstallationID:       p.InstallationID,
		Name:                 p.Name,
		Description:          p.Description,
		Priority:             p.Priority,
		Enabled:              p.Enabled,
		Action:               string(p.Action),
		RepoPatterns:         pq.StringArray(nonNilSlice(p.RepoPatterns)),
		BranchPatterns:       pq.StringArray(nonNilSlice(p.BranchPatterns)),
		ActorPatterns:        pq.StringArray(nonNilSlice(p.ActorPatterns)),
		LabelPatterns:        pq.StringArray(nonNilSlice(p.LabelPatterns)),
		BlockForkPRs:         p.BlockForkPRs,
		MaxConcurrentPerRepo: p.MaxConcurrentPerRepo,
		MaxJobDurationMin:    p.MaxJobDurationMin,
	}
}

func (r *PolicyRepo) Count(ctx context.Context) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&ModerationPolicyModel{}).Count(&count).Error
	return int(count), mapGormError(err)
}

func (r *PolicyRepo) Create(ctx context.Context, p *domain.ModerationPolicy) error {
	m := policyToModel(p)
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapGormError(err)
	}
	*p = m.toDomain()
	return nil
}

func (r *PolicyRepo) Update(ctx context.Context, p *domain.ModerationPolicy) error {
	m := policyToModel(p)
	res := r.db.WithContext(ctx).Model(&ModerationPolicyModel{}).
		Where("id = ?", p.ID).
		Select(
			"installation_id", "name", "description", "priority", "enabled", "action",
			"repo_patterns", "branch_patterns", "actor_patterns", "label_patterns",
			"block_fork_prs", "max_concurrent_per_repo", "max_job_duration_min",
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

func (r *PolicyRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Delete(&ModerationPolicyModel{}, "id = ?", id)
	if res.Error != nil {
		return mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PolicyRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.ModerationPolicy, error) {
	var m ModerationPolicyModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, mapGormError(err)
	}
	out := m.toDomain()
	return &out, nil
}

func (r *PolicyRepo) List(ctx context.Context, filter domain.PolicyFilter) ([]domain.ModerationPolicy, int, error) {
	q := r.db.WithContext(ctx).Model(&ModerationPolicyModel{})
	if filter.Action != "" {
		q = q.Where("action = ?", filter.Action)
	}
	if filter.Enabled != nil {
		q = q.Where("enabled = ?", *filter.Enabled)
	}
	if like := sanitizeLike(filter.Search); like != "" {
		q = q.Where(`(name ILIKE ? ESCAPE '\' OR description ILIKE ? ESCAPE '\')`, like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, mapGormError(err)
	}
	sortCols := map[string]string{
		"priority":   "priority",
		"name":       "name",
		"action":     "action",
		"enabled":    "enabled",
		"created_at": "created_at",
	}
	var models []ModerationPolicyModel
	if err := q.Scopes(applySort(filter.PageParams, sortCols, "priority ASC, created_at ASC"), paginate(filter.PageParams)).Find(&models).Error; err != nil {
		return nil, 0, mapGormError(err)
	}
	items := make([]domain.ModerationPolicy, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, int(total), nil
}

func (r *PolicyRepo) ListEnabledOrdered(ctx context.Context) ([]domain.ModerationPolicy, error) {
	var models []ModerationPolicyModel
	err := r.db.WithContext(ctx).Where("enabled = TRUE").Order("priority ASC, created_at ASC").Find(&models).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	items := make([]domain.ModerationPolicy, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, nil
}
