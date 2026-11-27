package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/crypto"
	"github.com/cubetiqlabs/runway/internal/database"
	"github.com/cubetiqlabs/runway/internal/github"
	apphttp "github.com/cubetiqlabs/runway/internal/http"
	"github.com/cubetiqlabs/runway/internal/logger"
	"github.com/cubetiqlabs/runway/internal/moderation"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
	"github.com/cubetiqlabs/runway/internal/repository"
	"github.com/cubetiqlabs/runway/internal/service"
	"github.com/gofiber/fiber/v3"
	"go.uber.org/zap"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "", "Path to YAML configuration file")
	healthcheck := flag.Bool("healthcheck", false, "Probe local /healthz endpoint and exit")
	flag.Parse()

	if *healthcheck {
		os.Exit(runHealthcheck())
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	log, err := logger.New(cfg.App.Environment, cfg.Logging.Level, cfg.Logging.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger initialization error: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = log.Sync() }()

	if cfg.Security.EphemeralKeysInDev {
		log.Warn("using ephemeral development security keys; set RUNWAY_SECURITY_SESSION_SECRET and RUNWAY_SECURITY_ENCRYPTION_KEY in production")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.NewGormDB(ctx, cfg.Database, log, cfg.IsProduction())
	if err != nil {
		log.Fatal("failed to connect to PostgreSQL via GORM", zap.Error(err))
	}
	defer func() { _ = database.Close(db) }()

	if err := database.AutoMigrate(ctx, db, log, repository.Models()...); err != nil {
		log.Fatal("failed to auto-migrate database schema", zap.Error(err))
	}

	ghAuth, err := github.NewAppAuth(cfg.GitHub.AppID, cfg.GitHub.PrivateKeyPEM, cfg.GitHub.PrivateKeyPath, cfg.GitHub.APIBaseURL, nil)
	if err != nil {
		log.Fatal("failed to initialize GitHub App auth", zap.Error(err))
	}
	ghClient := github.NewClient(ghAuth, cfg.GitHub.APIBaseURL)

	authRepo := repository.NewAuthRepo(db)
	instRepo := repository.NewInstallationRepo(db)
	poolRepo := repository.NewPoolRepo(db)
	runnerRepo := repository.NewRunnerRepo(db)
	jobRepo := repository.NewJobRepo(db)
	policyRepo := repository.NewPolicyRepo(db)
	auditRepo := repository.NewAuditRepo(db)
	agentRepo := repository.NewAgentRepo(db)

	encKey, err := crypto.ParseKey32(cfg.Security.EncryptionKey)
	if err != nil {
		log.Fatal("failed to parse encryption key", zap.Error(err))
	}

	engine := moderation.NewEngine(cfg.Moderation.DefaultAction)
	orch := orchestrator.NewOrchestrator(cfg.Orchestrator, log, runnerRepo, jobRepo, auditRepo, agentRepo, encKey)
	orch.Start(ctx)

	pingCtx, pingCancel := context.WithTimeout(ctx, 3*time.Second)
	if err := orch.HealthCheck(pingCtx); err != nil {
		log.Warn("orchestrator backend unreachable on startup; verify socket/endpoint configuration",
			zap.String("provider", cfg.Orchestrator.Provider),
			zap.Error(err),
		)
	}
	pingCancel()

	authSvc := service.NewAuthService(cfg, log, authRepo, auditRepo)
	if err := authSvc.BootstrapAdmin(ctx); err != nil {
		log.Fatal("failed to bootstrap initial admin account", zap.Error(err))
	}

	runnerSvc := service.NewRunnerService(cfg, ghClient, instRepo, poolRepo, runnerRepo, auditRepo, orch)
	if err := runnerSvc.SeedDefaultPools(ctx); err != nil {
		log.Fatal("failed to seed default runner pools", zap.Error(err))
	}

	dispatcher := service.NewJobDispatcher(cfg, log, ghClient, instRepo, poolRepo, jobRepo, runnerRepo, agentRepo, auditRepo, orch)
	agentSvc := service.NewAgentService(log, agentRepo, runnerRepo, jobRepo, auditRepo, poolRepo, encKey)
	orch.SetSlotFreedHook(dispatcher.Trigger)
	orch.SetRunnerCleanupHook(dispatcher.CleanupGitHubRunner)
	runnerSvc.SetSlotFreedHook(dispatcher.Trigger)
	agentSvc.SetSlotFreedHook(dispatcher.Trigger)

	modSvc := service.NewModerationService(ghClient, policyRepo, jobRepo, poolRepo, runnerRepo, auditRepo, engine, orch, dispatcher)
	if err := modSvc.SeedDefaultPolicies(ctx); err != nil {
		log.Fatal("failed to seed default moderation policies", zap.Error(err))
	}

	webhookSvc := service.NewWebhookService(cfg, log, ghClient, instRepo, poolRepo, policyRepo, jobRepo, runnerRepo, auditRepo, engine, orch, dispatcher)
	dashboardSvc := service.NewDashboardService(cfg, ghClient, auditRepo, orch)
	dispatcher.Start(ctx)

	app, err := apphttp.NewServer(apphttp.ServerDeps{
		Config:        cfg,
		Log:           log,
		ReadyCheck:    func(c context.Context) error { return database.Ping(c, db) },
		AuthSvc:       authSvc,
		WebhookSvc:    webhookSvc,
		RunnerSvc:     runnerSvc,
		ModerationSvc: modSvc,
		DashboardSvc:  dashboardSvc,
		AgentSvc:      agentSvc,
	})
	if err != nil {
		log.Fatal("failed to initialize HTTP server", zap.Error(err))
	}

	go func() {
		log.Info("starting Runway server",
			zap.String("version", version),
			zap.String("address", cfg.Address()),
			zap.String("environment", cfg.App.Environment),
			zap.String("provider", cfg.Orchestrator.Provider),
			zap.String("docker_socket", cfg.Orchestrator.DockerSocket),
			zap.String("runner_docker_host", cfg.Orchestrator.RunnerDockerHost),
			zap.String("runner_network", cfg.Orchestrator.RunnerNetwork),
			zap.Bool("github_app_configured", ghClient.IsConfigured()),
		)
		err := app.Listen(cfg.Address(), fiber.ListenConfig{
			DisableStartupMessage: true,
		})
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("HTTP server exited with error", zap.Error(err))
		}
	}()

	<-ctx.Done()
	log.Info("shutdown signal received; draining connections")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Error("HTTP server graceful shutdown failed", zap.Error(err))
	}
	dispatcher.Wait()
	orch.Wait()
	log.Info("Runway server stopped cleanly")
}

func runHealthcheck() int {
	port := os.Getenv("RUNWAY_SERVER_PORT")
	if port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
