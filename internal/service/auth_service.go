package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/crypto"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type AuthService struct {
	cfg       *config.Config
	log       *zap.Logger
	authRepo  domain.AuthRepository
	auditRepo domain.AuditRepository
}

func NewAuthService(cfg *config.Config, log *zap.Logger, authRepo domain.AuthRepository, auditRepo domain.AuditRepository) *AuthService {
	return &AuthService{cfg: cfg, log: log, authRepo: authRepo, auditRepo: auditRepo}
}

// BootstrapAdmin seeds the initial admin account when the users table is empty.
func (s *AuthService) BootstrapAdmin(ctx context.Context) error {
	count, err := s.authRepo.CountUsers(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	email := strings.TrimSpace(s.cfg.Admin.Email)
	if email == "" {
		email = "admin@runway.local"
	}
	name := strings.TrimSpace(s.cfg.Admin.Name)
	if name == "" {
		name = "Runway Admin"
	}

	password := strings.TrimSpace(s.cfg.Admin.Password)
	generated := false
	if password == "" {
		if s.cfg.IsProduction() {
			return errors.New("admin.password (RUNWAY_ADMIN_PASSWORD) is required on initial bootstrap in production")
		}
		tok, err := crypto.GenerateRandomToken(12)
		if err != nil {
			return err
		}
		password = tok
		generated = true
	}

	hash, err := crypto.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash bootstrap admin password: %w", err)
	}

	u := &domain.User{
		Email:        email,
		Name:         name,
		PasswordHash: hash,
		Role:         domain.RoleAdmin,
		IsActive:     true,
	}
	if err := s.authRepo.CreateUser(ctx, u); err != nil {
		return err
	}

	if generated {
		s.log.Warn("seeded initial development admin account",
			zap.String("email", email),
			zap.String("password", password),
		)
	} else {
		s.log.Info("seeded initial admin account", zap.String("email", email))
	}
	return nil
}

type LoginResult struct {
	User         *domain.User `json:"user"`
	SessionToken string       `json:"-"`
	CSRFToken    string       `json:"csrf_token"`
	ExpiresAt    time.Time    `json:"expires_at"`
}

func (s *AuthService) Login(ctx context.Context, email, password, ip, userAgent string) (*LoginResult, error) {
	email = strings.TrimSpace(email)
	if email == "" || password == "" {
		return nil, domain.ErrUnauthorized
	}

	user, err := s.authRepo.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	if !user.IsActive || !crypto.VerifyPassword(password, user.PasswordHash) {
		return nil, domain.ErrUnauthorized
	}

	rawToken, err := crypto.GenerateRandomToken(32)
	if err != nil {
		return nil, err
	}
	csrfToken, err := crypto.GenerateRandomToken(24)
	if err != nil {
		return nil, err
	}

	sess := &domain.Session{
		UserID:    user.ID,
		TokenHash: crypto.HashTokenSHA256(rawToken),
		CSRFToken: csrfToken,
		IPAddress: ip,
		UserAgent: truncate(userAgent, 256),
		ExpiresAt: time.Now().UTC().Add(s.cfg.Security.SessionTTL),
	}
	if err := s.authRepo.CreateSession(ctx, sess); err != nil {
		return nil, err
	}

	meta, _ := json.Marshal(map[string]string{"email": user.Email})
	_ = s.auditRepo.AppendAuditLog(ctx, &domain.AuditLog{
		ActorID:      &user.ID,
		ActorLabel:   user.Email,
		Action:       "auth.login",
		ResourceType: "session",
		ResourceID:   sess.ID.String(),
		Metadata:     meta,
		IPAddress:    ip,
	})

	return &LoginResult{
		User:         user,
		SessionToken: rawToken,
		CSRFToken:    csrfToken,
		ExpiresAt:    sess.ExpiresAt,
	}, nil
}

func (s *AuthService) AuthenticateSession(ctx context.Context, rawToken string) (*domain.Session, *domain.User, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, nil, domain.ErrUnauthorized
	}
	return s.authRepo.GetSessionByTokenHash(ctx, crypto.HashTokenSHA256(rawToken))
}

func (s *AuthService) AuthenticateBearerToken(ctx context.Context, rawToken string) (*domain.APIToken, *domain.User, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, nil, domain.ErrUnauthorized
	}
	tok, user, err := s.authRepo.GetAPITokenByHash(ctx, crypto.HashTokenSHA256(rawToken))
	if err != nil {
		return nil, nil, domain.ErrUnauthorized
	}
	_ = s.authRepo.TouchAPIToken(ctx, tok.ID)
	return tok, user, nil
}

func (s *AuthService) Logout(ctx context.Context, rawToken string) error {
	if strings.TrimSpace(rawToken) == "" {
		return nil
	}
	return s.authRepo.DeleteSessionByTokenHash(ctx, crypto.HashTokenSHA256(rawToken))
}

func (s *AuthService) CreateAPIToken(ctx context.Context, user *domain.User, name string, scopes []string, ttlDays int) (string, *domain.APIToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, fmt.Errorf("%w: token name is required", domain.ErrValidation)
	}
	rawSecret, err := crypto.GenerateRandomToken(32)
	if err != nil {
		return "", nil, err
	}
	plain := "rwy_" + rawSecret
	var expiresAt *time.Time
	if ttlDays > 0 {
		exp := time.Now().UTC().Add(time.Duration(ttlDays) * 24 * time.Hour)
		expiresAt = &exp
	}
	if len(scopes) == 0 {
		scopes = []string{"read"}
	}
	tok := &domain.APIToken{
		UserID:    user.ID,
		Name:      name,
		Prefix:    plain[:12],
		TokenHash: crypto.HashTokenSHA256(plain),
		Scopes:    scopes,
		ExpiresAt: expiresAt,
	}
	if err := s.authRepo.CreateAPIToken(ctx, tok); err != nil {
		return "", nil, err
	}
	return plain, tok, nil
}

func (s *AuthService) ListAPITokens(ctx context.Context, userID uuid.UUID) ([]domain.APIToken, error) {
	return s.authRepo.ListAPITokensByUser(ctx, userID)
}

func (s *AuthService) DeleteAPIToken(ctx context.Context, id, userID uuid.UUID) error {
	return s.authRepo.DeleteAPIToken(ctx, id, userID)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
