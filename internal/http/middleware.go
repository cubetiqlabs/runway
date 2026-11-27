package http

import (
	"crypto/subtle"
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/service"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"go.uber.org/zap"
)

const (
	sessionCookieName = "runway_session"
	ctxUserKey        = "runway_user"
	ctxSessionKey     = "runway_session"
	ctxAgentKey       = "runway_agent"
)

func zapLoggerMiddleware(log *zap.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		status := c.Response().StatusCode()
		fields := []zap.Field{
			zap.String("request_id", requestid.FromContext(c)),
			zap.String("method", c.Method()),
			zap.String("path", c.Path()),
			zap.Int("status", status),
			zap.Duration("latency", time.Since(start)),
			zap.String("ip", c.IP()),
		}
		if status >= 500 {
			log.Error("http request", fields...)
		} else if status >= 400 {
			log.Warn("http request", fields...)
		} else if !strings.HasPrefix(c.Path(), "/assets/") {
			log.Info("http request", fields...)
		}
		return err
	}
}

func authMiddleware(authSvc *service.AuthService) fiber.Handler {
	return func(c fiber.Ctx) error {
		authHeader := strings.TrimSpace(c.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			rawBearer := strings.TrimSpace(authHeader[7:])
			_, user, err := authSvc.AuthenticateBearerToken(c.Context(), rawBearer)
			if err != nil {
				return domain.ErrUnauthorized
			}
			c.Locals(ctxUserKey, user)
			return c.Next()
		}

		rawCookie := c.Cookies(sessionCookieName)
		if rawCookie == "" {
			return domain.ErrUnauthorized
		}
		sess, user, err := authSvc.AuthenticateSession(c.Context(), rawCookie)
		if err != nil {
			return domain.ErrUnauthorized
		}

		// Require CSRF header on cookie-authenticated mutations.
		switch c.Method() {
		case fiber.MethodPost, fiber.MethodPut, fiber.MethodPatch, fiber.MethodDelete:
			provided := c.Get("X-CSRF-Token")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(sess.CSRFToken)) != 1 {
				return sendError(c, fiber.StatusForbidden, "csrf_invalid", "missing or invalid X-CSRF-Token header")
			}
		}

		c.Locals(ctxUserKey, user)
		c.Locals(ctxSessionKey, sess)
		return c.Next()
	}
}

func requireRoles(roles ...domain.UserRole) fiber.Handler {
	allowed := make(map[domain.UserRole]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(c fiber.Ctx) error {
		user := currentUser(c)
		if user == nil {
			return domain.ErrUnauthorized
		}
		if _, ok := allowed[user.Role]; !ok {
			return domain.ErrForbidden
		}
		return c.Next()
	}
}

func currentUser(c fiber.Ctx) *domain.User {
	u, _ := c.Locals(ctxUserKey).(*domain.User)
	return u
}

func currentSession(c fiber.Ctx) *domain.Session {
	s, _ := c.Locals(ctxSessionKey).(*domain.Session)
	return s
}

func agentAuthMiddleware(agentSvc *service.AgentService) fiber.Handler {
	return func(c fiber.Ctx) error {
		if agentSvc == nil {
			return domain.ErrUnauthorized
		}
		authHeader := strings.TrimSpace(c.Get("Authorization"))
		if !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			return domain.ErrUnauthorized
		}
		rawBearer := strings.TrimSpace(authHeader[7:])
		agent, err := agentSvc.AuthenticateAgent(c.Context(), rawBearer)
		if err != nil {
			return domain.ErrUnauthorized
		}
		c.Locals(ctxAgentKey, agent)
		return c.Next()
	}
}

func currentAgent(c fiber.Ctx) *domain.Agent {
	a, _ := c.Locals(ctxAgentKey).(*domain.Agent)
	return a
}
