package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/cubetiqlabs/runway/internal/agent"
	"github.com/cubetiqlabs/runway/internal/logger"
)

var version = "dev"

func main() {
	agent.CleanupOldBinary()

	cmd := "run"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "version":
		fmt.Printf("runway-agent %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	case "help", "-h", "--help":
		printUsage()
	case "update":
		os.Exit(runUpdate(args))
	case "run":
		os.Exit(runWorker(args))
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q\n\n", cmd)
		printUsage()
		os.Exit(2)
	}
}

func runWorker(args []string) int {
	cfg := agent.DefaultConfig(version)

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to optional env config file")
	server := fs.String("server", "", "Runway control plane URL (e.g. https://runway.cubis.tech)")
	token := fs.String("token", "", "Agent registration token (rwa_...)")
	rt := fs.String("runtime", "", "Execution runtime: auto, native, or docker")
	dockerSock := fs.String("docker-socket", "", "Docker socket path or tcp:// endpoint")
	workDir := fs.String("work-dir", "", "Working directory for runner cache and ephemeral jobs")
	concurrency := fs.Int("concurrency", 0, "Maximum concurrent runner jobs on this host")
	labels := fs.String("labels", "", "Comma-separated extra labels advertised by this host")
	runnerVer := fs.String("runner-version", "", "Pin actions/runner version (default: latest)")
	updateRepo := fs.String("update-repo", "", "GitHub repository for self-updates (owner/repo)")
	noAutoUpdate := fs.Bool("no-auto-update", false, "Disable automatic background self-updates")
	logLevel := fs.String("log-level", "info", "Log level (debug, info, warn, error)")
	logFormat := fs.String("log-format", "console", "Log format (console or json)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if err := cfg.ApplyEnvAndFile(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		return 1
	}
	if *server != "" {
		cfg.ServerURL = *server
	}
	if *token != "" {
		cfg.Token = *token
	}
	if *rt != "" {
		cfg.Runtime = *rt
	}
	if *dockerSock != "" {
		cfg.DockerSocket = *dockerSock
	}
	if *workDir != "" {
		cfg.WorkDir = *workDir
	}
	if *concurrency > 0 {
		cfg.Concurrency = *concurrency
	}
	if *labels != "" {
		cfg.Labels = nil
		for _, p := range strings.Split(*labels, ",") {
			if s := strings.TrimSpace(p); s != "" {
				cfg.Labels = append(cfg.Labels, s)
			}
		}
	}
	if *runnerVer != "" {
		cfg.RunnerVersion = strings.TrimPrefix(*runnerVer, "v")
	}
	if *updateRepo != "" {
		cfg.UpdateRepo = *updateRepo
	}
	if *noAutoUpdate {
		cfg.AutoUpdate = false
	}

	if err := cfg.NormalizeAndValidate(true); err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		return 1
	}

	log, err := logger.New("production", *logLevel, *logFormat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger error: %v\n", err)
		return 1
	}
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ag := agent.New(cfg, log)
	if err := ag.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "agent error: %v\n", err)
		return 1
	}
	return 0
}

func runUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	repo := fs.String("repo", "cubetiqlabs/runway", "GitHub repository (owner/repo)")
	force := fs.Bool("force", false, "Install latest release even if current version is dev or equal")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	updater := agent.NewUpdater(*repo)
	res, err := updater.CheckAndApply(version, *force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update failed: %v\n", err)
		return 1
	}
	if !res.Updated {
		fmt.Printf("runway-agent is already up to date (current=%s, latest=%s)\n", version, res.LatestVersion)
		return 0
	}
	fmt.Printf("Updated runway-agent to %s\n", res.LatestVersion)
	return 0
}

func printUsage() {
	fmt.Printf(`Runway Agent (%s) — Distributed ephemeral self-hosted runner worker

Usage:
  runway-agent [run] [flags]
  runway-agent update [--repo cubetiqlabs/runway] [--force]
  runway-agent version

Flags (run):
  --server <url>             Runway control plane URL (RUNWAY_AGENT_SERVER)
  --token <rwa_...>          Agent bearer token (RUNWAY_AGENT_TOKEN)
  --runtime <mode>           auto | native | docker (RUNWAY_AGENT_RUNTIME)
  --docker-socket <addr>     Docker socket or tcp:// endpoint (RUNWAY_AGENT_DOCKER_SOCKET)
  --work-dir <path>          Working cache & job directory (RUNWAY_AGENT_WORK_DIR)
  --concurrency <n>          Max concurrent runner jobs (RUNWAY_AGENT_CONCURRENCY)
  --labels <a,b>             Extra comma-separated labels (RUNWAY_AGENT_LABELS)
  --no-auto-update           Disable background auto-updates
  --config <path>            Optional env config file
`, version)
}
