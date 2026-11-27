package http

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/service"
	"github.com/cubetiqlabs/runway/internal/webui"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"go.uber.org/zap"
)

type ReadinessChecker func(ctx context.Context) error

type ServerDeps struct {
	Config        *config.Config
	Log           *zap.Logger
	ReadyCheck    ReadinessChecker
	AuthSvc       *service.AuthService
	WebhookSvc    *service.WebhookService
	RunnerSvc     *service.RunnerService
	ModerationSvc *service.ModerationService
	DashboardSvc  *service.DashboardService
	AgentSvc      *service.AgentService
}

// NewServer constructs and wires the GoFiber v3 application, security middlewares, API routes, and embedded SPA.
func NewServer(deps ServerDeps) (*fiber.App, error) {
	cfg := deps.Config
	app := fiber.New(fiber.Config{
		AppName:      cfg.App.Name,
		BodyLimit:    cfg.Server.BodyLimitBytes,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
		ProxyHeader:  "X-Forwarded-For",
		ErrorHandler: func(c fiber.Ctx, err error) error {
			var fiberErr *fiber.Error
			if errors.As(err, &fiberErr) {
				return sendError(c, fiberErr.Code, "http_error", fiberErr.Message)
			}
			status, code, msg := mapDomainError(err)
			if status >= 500 {
				deps.Log.Error("unhandled server error",
					zap.String("request_id", requestid.FromContext(c)),
					zap.String("path", c.Path()),
					zap.Error(err),
				)
			}
			return sendError(c, status, code, msg)
		},
	})

	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(zapLoggerMiddleware(deps.Log))
	app.Use(helmet.New(helmet.Config{
		XSSProtection:         "0",
		ContentTypeNosniff:    "nosniff",
		XFrameOptions:         "DENY",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
		ContentSecurityPolicy: "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'",
	}))

	if len(cfg.Server.AllowedOrigins) > 0 {
		app.Use(cors.New(cors.Config{
			AllowOrigins:     cfg.Server.AllowedOrigins,
			AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-CSRF-Token"},
			AllowCredentials: true,
		}))
	}

	// Health & readiness probes (unauthenticated)
	app.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	app.Get("/readyz", func(c fiber.Ctx) error {
		if deps.ReadyCheck != nil {
			ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
			defer cancel()
			if err := deps.ReadyCheck(ctx); err != nil {
				return sendError(c, fiber.StatusServiceUnavailable, "not_ready", "database connection unavailable")
			}
		}
		return c.JSON(fiber.Map{"status": "ready"})
	})

	// GitHub App Webhook endpoint (HMAC-SHA256 verified inside service)
	webhookHandler := NewWebhookHandler(deps.WebhookSvc)
	webhookLimiter := limiter.New(limiter.Config{
		Max:        cfg.Security.WebhookRateMax,
		Expiration: time.Minute,
	})
	app.Post("/webhooks/github", webhookLimiter, webhookHandler.HandleGitHub)

	// Public auth endpoint with brute-force rate limit
	authHandler := NewAuthHandler(cfg, deps.AuthSvc)
	loginLimiter := limiter.New(limiter.Config{
		Max:        cfg.Security.LoginRateMax,
		Expiration: time.Minute,
	})

	api := app.Group("/api/v1")
	api.Post("/auth/login", loginLimiter, authHandler.Login)

	// Distributed runway-agent worker protocol (Bearer rwa_... token auth, no CSRF cookie).
	// Registered before `protected` so user authMiddleware does not intercept agent bearer tokens.
	agentHandler := NewAgentHandler(deps.AgentSvc)
	agentLimiter := limiter.New(limiter.Config{
		Max:        600,
		Expiration: time.Minute,
	})
	agentGroup := api.Group("/agent", agentLimiter, agentAuthMiddleware(deps.AgentSvc))
	agentGroup.Post("/heartbeat", agentHandler.Heartbeat)
	agentGroup.Post("/claim", agentHandler.ClaimTask)
	agentGroup.Post("/tasks/:id/complete", agentHandler.CompleteTask)
	agentGroup.Post("/tasks/:id/logs", agentHandler.IngestTaskLogs)

	// Protected API group
	protected := api.Group("", authMiddleware(deps.AuthSvc))
	protected.Post("/auth/logout", authHandler.Logout)
	protected.Get("/auth/me", authHandler.Me)
	protected.Get("/auth/tokens", authHandler.ListAPITokens)
	protected.Post("/auth/tokens", authHandler.CreateAPIToken)
	protected.Delete("/auth/tokens/:id", authHandler.DeleteAPIToken)

	fleetHandler := NewFleetHandler(deps.RunnerSvc, deps.DashboardSvc)
	modHandler := NewModerationHandler(deps.ModerationSvc, deps.DashboardSvc)

	canOperate := requireRoles(domain.RoleAdmin, domain.RoleModerator)
	adminOnly := requireRoles(domain.RoleAdmin)

	protected.Get("/dashboard/overview", fleetHandler.Overview)

	protected.Get("/pools", fleetHandler.ListPools)
	protected.Post("/pools", canOperate, fleetHandler.CreatePool)
	protected.Put("/pools/:id", canOperate, fleetHandler.UpdatePool)
	protected.Delete("/pools/:id", adminOnly, fleetHandler.DeletePool)

	protected.Get("/runners", fleetHandler.ListRunners)
	protected.Get("/runners/:id/logs", fleetHandler.GetRunnerLogs)
	protected.Post("/runners/:id/drain", canOperate, fleetHandler.DrainRunner)
	protected.Post("/runners/:id/terminate", canOperate, fleetHandler.TerminateRunner)

	protected.Get("/installations", fleetHandler.ListInstallations)
	protected.Post("/installations/sync", canOperate, fleetHandler.SyncInstallations)

	protected.Get("/policies", modHandler.ListPolicies)
	protected.Post("/policies", canOperate, modHandler.CreatePolicy)
	protected.Put("/policies/:id", canOperate, modHandler.UpdatePolicy)
	protected.Delete("/policies/:id", adminOnly, modHandler.DeletePolicy)
	protected.Post("/policies/simulate", modHandler.SimulatePolicy)

	protected.Get("/jobs", modHandler.ListJobs)
	protected.Post("/jobs/:id/approve", canOperate, modHandler.ApproveJob)
	protected.Post("/jobs/:id/reject", canOperate, modHandler.RejectJob)

	protected.Get("/webhooks/deliveries", modHandler.ListDeliveries)
	protected.Get("/audit-logs", modHandler.ListAuditLogs)
	protected.Get("/events", modHandler.ListEvents)
	protected.Get("/events/summary", modHandler.EventSummary)
	protected.Get("/settings/github-app", modHandler.SettingsStatus)

	protected.Get("/agents", agentHandler.ListAgents)
	protected.Post("/agents", canOperate, agentHandler.RegisterAgent)
	protected.Get("/agents/:id", agentHandler.GetAgent)
	protected.Get("/agents/:id/metrics", agentHandler.GetAgentMetrics)
	protected.Get("/agents/:id/tasks", agentHandler.ListAgentTasks)
	protected.Post("/agents/:id/drain", canOperate, agentHandler.DrainAgent)
	protected.Post("/agents/:id/resume", canOperate, agentHandler.ResumeAgent)
	protected.Post("/agents/:id/rotate-token", canOperate, agentHandler.RotateToken)
	protected.Delete("/agents/:id", adminOnly, agentHandler.DeleteAgent)

	// Never fall through to SPA HTML for unknown API or webhook routes.
	app.All("/api/*", func(c fiber.Ctx) error {
		return sendError(c, fiber.StatusNotFound, "not_found", "API endpoint not found")
	})
	app.All("/webhooks/*", func(c fiber.Ctx) error {
		return sendError(c, fiber.StatusNotFound, "not_found", "webhook endpoint not found")
	})

	// Compact embedded TanStack Start SPA server
	spaFS, err := webui.FS()
	if err != nil {
		return nil, err
	}
	app.Get("/*", newEmbeddedSPAHandler(spaFS))

	return app, nil
}

func newEmbeddedSPAHandler(distFS fs.FS) fiber.Handler {
	shellName := resolveShellFile(distFS)

	return func(c fiber.Ctx) error {
		cleanPath := strings.TrimPrefix(path.Clean("/"+c.Path()), "/")
		if cleanPath != "" {
			if data, ok := readEmbeddedFile(distFS, cleanPath); ok {
				if ct := mime.TypeByExtension(path.Ext(cleanPath)); ct != "" {
					c.Set(fiber.HeaderContentType, ct)
				}
				if strings.HasPrefix(cleanPath, "assets/") {
					c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
				} else {
					c.Set(fiber.HeaderCacheControl, "public, max-age=3600")
				}
				return c.Send(data)
			}
		}

		// Fallback to SPA shell (_shell.html from TanStack Start SPA mode, or index.html)
		shellBytes, ok := readEmbeddedFile(distFS, shellName)
		if !ok {
			return sendError(c, fiber.StatusNotFound, "spa_missing", "embedded frontend shell not found")
		}
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
		c.Set(fiber.HeaderCacheControl, "no-cache, no-store, must-revalidate")
		return c.Send(shellBytes)
	}
}

func resolveShellFile(distFS fs.FS) string {
	for _, candidate := range []string{"_shell.html", "index.html"} {
		if _, ok := readEmbeddedFile(distFS, candidate); ok {
			return candidate
		}
	}
	return "index.html"
}

func readEmbeddedFile(distFS fs.FS, name string) ([]byte, bool) {
	f, err := distFS.Open(name)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		return nil, false
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, false
	}
	return data, true
}
