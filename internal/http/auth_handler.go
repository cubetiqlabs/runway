package http

import (
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/service"
	"github.com/gofiber/fiber/v3"
)

type AuthHandler struct {
	cfg     *config.Config
	authSvc *service.AuthService
}

func NewAuthHandler(cfg *config.Config, authSvc *service.AuthService) *AuthHandler {
	return &AuthHandler{cfg: cfg, authSvc: authSvc}
}

func (h *AuthHandler) Login(c fiber.Ctx) error {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return sendError(c, fiber.StatusBadRequest, "bad_request", "invalid login payload")
	}

	res, err := h.authSvc.Login(c.Context(), req.Email, req.Password, c.IP(), c.Get("User-Agent"))
	if err != nil {
		return err
	}

	c.Cookie(&fiber.Cookie{
		Name:     sessionCookieName,
		Value:    res.SessionToken,
		Path:     "/",
		Expires:  res.ExpiresAt,
		HTTPOnly: true,
		Secure:   h.cfg.Security.CookieSecure,
		SameSite: h.cfg.Security.CookieSameSite,
	})

	return c.JSON(fiber.Map{
		"user":       res.User,
		"csrf_token": res.CSRFToken,
		"expires_at": res.ExpiresAt,
	})
}

func (h *AuthHandler) Logout(c fiber.Ctx) error {
	rawCookie := c.Cookies(sessionCookieName)
	_ = h.authSvc.Logout(c.Context(), rawCookie)

	c.Cookie(&fiber.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HTTPOnly: true,
		Secure:   h.cfg.Security.CookieSecure,
		SameSite: h.cfg.Security.CookieSameSite,
	})
	return c.JSON(fiber.Map{"ok": true})
}

func (h *AuthHandler) Me(c fiber.Ctx) error {
	user := currentUser(c)
	if user == nil {
		return domain.ErrUnauthorized
	}
	var csrf string
	if sess := currentSession(c); sess != nil {
		csrf = sess.CSRFToken
	}
	return c.JSON(fiber.Map{
		"user":       user,
		"csrf_token": csrf,
	})
}

func (h *AuthHandler) ListAPITokens(c fiber.Ctx) error {
	user := currentUser(c)
	tokens, err := h.authSvc.ListAPITokens(c.Context(), user.ID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": tokens})
}

func (h *AuthHandler) CreateAPIToken(c fiber.Ctx) error {
	user := currentUser(c)
	var req struct {
		Name    string   `json:"name"`
		Scopes  []string `json:"scopes"`
		TTLDays int      `json:"ttl_days"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return sendError(c, fiber.StatusBadRequest, "bad_request", "invalid token payload")
	}
	plain, tok, err := h.authSvc.CreateAPIToken(c.Context(), user, req.Name, req.Scopes, req.TTLDays)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"token":     plain,
		"api_token": tok,
	})
}

func (h *AuthHandler) DeleteAPIToken(c fiber.Ctx) error {
	user := currentUser(c)
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	if err := h.authSvc.DeleteAPIToken(c.Context(), id, user.ID); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}
