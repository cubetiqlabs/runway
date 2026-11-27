package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cubetiqlabs/runway/internal/cache"
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

	cacheMu sync.RWMutex
	cache   cache.Store
}

func NewAuthService(cfg *config.Config, log *zap.Logger, authRepo domain.AuthRepository, auditRepo domain.AuditRepository) *AuthService {
	return &AuthService{cfg: cfg, log: log, authRepo: authRepo, auditRepo: auditRepo}
}

func (s *AuthService) SetCache(c cache.Store) {
	s.cacheMu.Lock()
	s.cache = c
	s.cacheMu.Unlock()
}

func (s *AuthService) getCache() cache.Store {
	s.cacheMu.RLock()
	c := s.cache
	s.cacheMu.RUnlock()
	return c
}

type cachedSessionEntry struct {
	Session domain.Session `json:"session"`
	User    domain.User    `json:"user"`
}

type cachedAPITokenEntry struct {
	Token domain.APIToken `json:"token"`
	User  domain.User     `json:"user"`
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
	hash := crypto.HashTokenSHA256(rawToken)
	cacheKey := "auth:session:" + hash
	c := s.getCache()
	if c != nil {
		if entry, ok, err := cache.GetJSON[cachedSessionEntry](ctx, c, cacheKey); err == nil && ok {
			if entry.Session.ExpiresAt.After(time.Now().UTC()) && entry.User.IsActive {
				sess := entry.Session
				user := entry.User
				return &sess, &user, nil
			}
			_ = c.Delete(ctx, cacheKey)
		}
	}

	sess, user, err := s.authRepo.GetSessionByTokenHash(ctx, hash)
	if err != nil {
		return nil, nil, err
	}
	if c != nil && sess != nil && user != nil && user.IsActive {
		remaining := time.Until(sess.ExpiresAt)
		if remaining > 0 {
			ttl := 2 * time.Minute
			if remaining < ttl {
				ttl = remaining
			}
			uCopy := *user
			uCopy.PasswordHash = ""
			sCopy := *sess
			sCopy.TokenHash = ""
			_ = cache.SetJSON(ctx, c, cacheKey, cachedSessionEntry{Session: sCopy, User: uCopy}, ttl)
		}
	}
	return sess, user, nil
}

func (s *AuthService) AuthenticateBearerToken(ctx context.Context, rawToken string) (*domain.APIToken, *domain.User, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, nil, domain.ErrUnauthorized
	}
	hash := crypto.HashTokenSHA256(rawToken)
	cacheKey := "auth:token:" + hash
	c := s.getCache()

	var tok *domain.APIToken
	var user *domain.User
	if c != nil {
		if entry, ok, err := cache.GetJSON[cachedAPITokenEntry](ctx, c, cacheKey); err == nil && ok {
			validExpiry := entry.Token.ExpiresAt == nil || entry.Token.ExpiresAt.After(time.Now().UTC())
			if validExpiry && entry.User.IsActive {
				tCopy := entry.Token
				uCopy := entry.User
				tok = &tCopy
				user = &uCopy
			} else {
				_ = c.Delete(ctx, cacheKey)
			}
		}
	}

	if tok == nil || user == nil {
		var err error
		tok, user, err = s.authRepo.GetAPITokenByHash(ctx, hash)
		if err != nil {
			return nil, nil, domain.ErrUnauthorized
		}
		if c != nil && tok != nil && user != nil && user.IsActive {
			ttl := 60 * time.Second
			if tok.ExpiresAt != nil {
				rem := time.Until(*tok.ExpiresAt)
				if rem <= 0 {
					return nil, nil, domain.ErrUnauthorized
				}
				if rem < ttl {
					ttl = rem
				}
			}
			tCopy := *tok
			tCopy.TokenHash = ""
			uCopy := *user
			uCopy.PasswordHash = ""
			_ = cache.SetJSON(ctx, c, cacheKey, cachedAPITokenEntry{Token: tCopy, User: uCopy}, ttl)
			_ = c.Set(ctx, "auth:token_id:"+tok.ID.String(), []byte(hash), ttl+5*time.Second)
		}
	}

	// Throttle TouchAPIToken writes under concurrent/clustered traffic.
	shouldTouch := true
	if c != nil {
		if ok, err := c.SetNX(ctx, "auth:token_touch:"+tok.ID.String(), []byte("1"), time.Minute); err == nil {
			shouldTouch = ok
		}
	}
	if shouldTouch {
		_ = s.authRepo.TouchAPIToken(ctx, tok.ID)
	}
	return tok, user, nil
}

func (s *AuthService) Logout(ctx context.Context, rawToken string) error {
	if strings.TrimSpace(rawToken) == "" {
		return nil
	}
	hash := crypto.HashTokenSHA256(rawToken)
	if err := s.authRepo.DeleteSessionByTokenHash(ctx, hash); err != nil {
		return err
	}
	if c := s.getCache(); c != nil {
		_ = c.Delete(ctx, "auth:session:"+hash)
	}
	return nil
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
	if err := s.authRepo.DeleteAPIToken(ctx, id, userID); err != nil {
		return err
	}
	if c := s.getCache(); c != nil {
		idKey := "auth:token_id:" + id.String()
		if rawHash, ok, err := c.Get(ctx, idKey); err == nil && ok && len(rawHash) > 0 {
			_ = c.Delete(ctx, "auth:token:"+string(rawHash), idKey, "auth:token_touch:"+id.String())
		} else {
			_ = c.Delete(ctx, idKey, "auth:token_touch:"+id.String())
		}
	}
	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
