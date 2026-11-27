package repository

import (
	"context"

	"github.com/cubetiqlabs/runway/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type InstallationRepo struct {
	db *gorm.DB
}

func NewInstallationRepo(db *gorm.DB) *InstallationRepo {
	return &InstallationRepo{db: db}
}

func (r *InstallationRepo) Upsert(ctx context.Context, inst *domain.Installation) error {
	m := InstallationModel{
		ID:                    inst.ID,
		AccountID:             inst.AccountID,
		AccountLogin:          inst.AccountLogin,
		AccountType:           inst.AccountType,
		TargetType:            inst.TargetType,
		RepositoriesSelection: inst.RepositoriesSelection,
		Permissions:           nonEmptyJSON(inst.Permissions),
		SuspendedAt:           inst.SuspendedAt,
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"account_id", "account_login", "account_type", "target_type",
			"repositories_selection", "permissions", "suspended_at", "updated_at",
		}),
	}).Create(&m).Error
	if err != nil {
		return mapGormError(err)
	}
	*inst = m.toDomain()
	return nil
}

func (r *InstallationRepo) List(ctx context.Context, page domain.PageParams) ([]domain.Installation, int, error) {
	var total int64
	q := r.db.WithContext(ctx).Model(&InstallationModel{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, mapGormError(err)
	}
	var models []InstallationModel
	if err := q.Order("updated_at DESC").Scopes(paginate(page)).Find(&models).Error; err != nil {
		return nil, 0, mapGormError(err)
	}
	items := make([]domain.Installation, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, int(total), nil
}

func (r *InstallationRepo) GetByID(ctx context.Context, id int64) (*domain.Installation, error) {
	var m InstallationModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, mapGormError(err)
	}
	out := m.toDomain()
	return &out, nil
}

func (r *InstallationRepo) DeleteByID(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&InstallationModel{}, "id = ?", id)
	if res.Error != nil {
		return mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
