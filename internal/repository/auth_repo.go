package repository

import (
	"context"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

type AuthRepo struct {
	db *gorm.DB
}

func NewAuthRepo(db *gorm.DB) *AuthRepo {
	return &AuthRepo{db: db}
}

func (r *AuthRepo) CountUsers(ctx context.Context) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&UserModel{}).Count(&count).Error
	return int(count), mapGormError(err)
}

func (r *AuthRepo) CreateUser(ctx context.Context, user *domain.User) error {
	m := UserModel{
		Email:        user.Email,
		Name:         user.Name,
		PasswordHash: user.PasswordHash,
		Role:         string(user.Role),
		IsActive:     user.IsActive,
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapGormError(err)
	}
	*user = m.toDomain()
	return nil
}

func (r *AuthRepo) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	var m UserModel
	err := r.db.WithContext(ctx).Where("LOWER(email) = LOWER(?)", email).First(&m).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	u := m.toDomain()
	return &u, nil
}

func (r *AuthRepo) GetUserByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	var m UserModel
	err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	u := m.toDomain()
	return &u, nil
}

func (r *AuthRepo) CreateSession(ctx context.Context, session *domain.Session) error {
	m := SessionModel{
		UserID:    session.UserID,
		TokenHash: session.TokenHash,
		CSRFToken: session.CSRFToken,
		IPAddress: session.IPAddress,
		UserAgent: session.UserAgent,
		ExpiresAt: session.ExpiresAt,
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapGormError(err)
	}
	*session = m.toDomain()
	return nil
}

func (r *AuthRepo) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*domain.Session, *domain.User, error) {
	var sm SessionModel
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND expires_at > ?", tokenHash, time.Now().UTC()).
		First(&sm).Error
	if err != nil {
		return nil, nil, mapGormError(err)
	}
	var um UserModel
	err = r.db.WithContext(ctx).
		Where("id = ? AND is_active = TRUE", sm.UserID).
		First(&um).Error
	if err != nil {
		return nil, nil, mapGormError(err)
	}
	s := sm.toDomain()
	u := um.toDomain()
	return &s, &u, nil
}

func (r *AuthRepo) DeleteSessionByTokenHash(ctx context.Context, tokenHash string) error {
	err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).Delete(&SessionModel{}).Error
	return mapGormError(err)
}

func (r *AuthRepo) DeleteExpiredSessions(ctx context.Context) error {
	err := r.db.WithContext(ctx).Where("expires_at <= ?", time.Now().UTC()).Delete(&SessionModel{}).Error
	return mapGormError(err)
}

func (r *AuthRepo) CreateAPIToken(ctx context.Context, token *domain.APIToken) error {
	m := APITokenModel{
		UserID:    token.UserID,
		Name:      token.Name,
		Prefix:    token.Prefix,
		TokenHash: token.TokenHash,
		Scopes:    pq.StringArray(nonNilSlice(token.Scopes)),
		ExpiresAt: token.ExpiresAt,
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return mapGormError(err)
	}
	*token = m.toDomain()
	return nil
}

func (r *AuthRepo) ListAPITokensByUser(ctx context.Context, userID uuid.UUID) ([]domain.APIToken, error) {
	var models []APITokenModel
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&models).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	out := make([]domain.APIToken, 0, len(models))
	for _, m := range models {
		out = append(out, m.toDomain())
	}
	return out, nil
}

func (r *AuthRepo) GetAPITokenByHash(ctx context.Context, tokenHash string) (*domain.APIToken, *domain.User, error) {
	var tm APITokenModel
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND (expires_at IS NULL OR expires_at > ?)", tokenHash, time.Now().UTC()).
		First(&tm).Error
	if err != nil {
		return nil, nil, mapGormError(err)
	}
	var um UserModel
	err = r.db.WithContext(ctx).
		Where("id = ? AND is_active = TRUE", tm.UserID).
		First(&um).Error
	if err != nil {
		return nil, nil, mapGormError(err)
	}
	t := tm.toDomain()
	u := um.toDomain()
	return &t, &u, nil
}

func (r *AuthRepo) TouchAPIToken(ctx context.Context, id uuid.UUID) error {
	err := r.db.WithContext(ctx).Model(&APITokenModel{}).
		Where("id = ?", id).
		Update("last_used_at", time.Now().UTC()).Error
	return mapGormError(err)
}

func (r *AuthRepo) DeleteAPIToken(ctx context.Context, id, userID uuid.UUID) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&APITokenModel{})
	if res.Error != nil {
		return mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
