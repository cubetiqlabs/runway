package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/cubetiqlabs/runway/internal/agent"
	"github.com/cubetiqlabs/runway/internal/logger"
	"go.uber.org/zap"
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
	case "logs":
		os.Exit(runLogs(args))
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
	sandbox := fs.String("sandbox", "", "OCI sandbox runtime for Docker runners: auto, gvisor (runsc), or runc")
	dockerSock := fs.String("docker-socket", "", "Docker socket path or tcp:// endpoint")
	isolatedDinD := fs.Bool("isolated-dind", false, "Run an isolated per-job DinD sidecar to avoid port conflicts (default: true)")
	noIsolatedDinD := fs.Bool("no-isolated-dind", false, "Disable isolated per-job DinD sidecars")
	dindImage := fs.String("dind-image", "", "Docker-in-Docker sidecar image (default: docker:dind)")
	runnerDockerHost := fs.String("runner-docker-host", "", "DOCKER_HOST injected into runner containers (disables isolated DinD)")
	runnerNetwork := fs.String("runner-network", "", "Docker NetworkMode for spawned runner containers")
	mountDockerSock := fs.Bool("mount-docker-socket", false, "Bind mount /var/run/docker.sock into spawned runner containers")
	noMountDockerSock := fs.Bool("no-mount-docker-socket", false, "Disable automatic docker.sock bind mounting into runner containers")
	runnerDockerGID := fs.String("runner-docker-gid", "", "Optional host docker GID passed as GroupAdd when mounting docker.sock")
	runnerDNS := fs.String("runner-dns", "", "Comma-separated DNS servers for runner containers (default for gVisor: 1.1.1.1,8.8.8.8)")
	workDir := fs.String("work-dir", "", "Working directory for runner cache and ephemeral jobs")
	logDir := fs.String("log-dir", "", "Directory for persistent agent.log output")
	concurrency := fs.Int("concurrency", 0, "Maximum concurrent runner jobs on this host")
	labels := fs.String("labels", "", "Comma-separated extra labels advertised by this host")
	runnerVer := fs.String("runner-version", "", "Pin actions/runner version (default: latest)")
	updateRepo := fs.String("update-repo", "", "GitHub repository for self-updates (owner/repo)")
	noAutoUpdate := fs.Bool("no-auto-update", false, "Disable automatic background self-updates")
	noCache := fs.Bool("no-cache", false, "Disable persistent build caches on this agent")
	noPrewarm := fs.Bool("no-prewarm-images", false, "Disable background runner container image prewarming")
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
	if *sandbox != "" {
		cfg.SandboxRuntime = *sandbox
	}
	if *dockerSock != "" {
		cfg.DockerSocket = *dockerSock
	}
	if *isolatedDinD {
		cfg.IsolatedDinD = true
		cfg.IsolatedDinDSet = true
	}
	if *noIsolatedDinD {
		cfg.IsolatedDinD = false
		cfg.IsolatedDinDSet = true
	}
	if *dindImage != "" {
		cfg.DinDImage = *dindImage
	}
	if *runnerDockerHost != "" {
		cfg.RunnerDockerHost = *runnerDockerHost
	}
	if *runnerNetwork != "" {
		cfg.RunnerNetwork = *runnerNetwork
	}
	if *mountDockerSock {
		cfg.MountDockerSocket = true
		cfg.MountDockerSocketSet = true
	}
	if *noMountDockerSock {
		cfg.MountDockerSocket = false
		cfg.MountDockerSocketSet = true
	}
	if *runnerDockerGID != "" {
		cfg.RunnerDockerGID = *runnerDockerGID
	}
	if *runnerDNS != "" {
		cfg.RunnerDNS = nil
		for _, p := range strings.Split(*runnerDNS, ",") {
			if s := strings.TrimSpace(p); s != "" {
				cfg.RunnerDNS = append(cfg.RunnerDNS, s)
			}
		}
	}
	if *workDir != "" {
		cfg.WorkDir = *workDir
	}
	if *logDir != "" {
		cfg.LogDir = *logDir
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
	if *noCache {
		cfg.CacheEnabled = false
	}
	if *noPrewarm {
		cfg.PrewarmImages = false
	}

	if err := cfg.NormalizeAndValidate(true); err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		return 1
	}

	logPath := filepath.Join(cfg.LogDir, "agent.log")
	log, logCloser, fileErr := logger.NewWithFile("production", *logLevel, *logFormat, logPath)
	if logCloser != nil {
		defer func() { _ = logCloser.Close() }()
	}
	defer func() { _ = log.Sync() }()
	if fileErr != nil {
		log.Warn("unable to open log file; continuing with stdout only", zap.String("log_file", logPath), zap.Error(fileErr))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ag := agent.New(cfg, log)
	if err := ag.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "agent error: %v\n", err)
		return 1
	}
	return 0
}

func runLogs(args []string) int {
	cfg := agent.DefaultConfig(version)
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to optional env config file")
	workDir := fs.String("work-dir", "", "Working directory")
	logDir := fs.String("log-dir", "", "Log directory")
	lines := fs.Int("n", 100, "Number of trailing log lines to print")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if err := cfg.ApplyEnvAndFile(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		return 1
	}
	if *workDir != "" {
		cfg.WorkDir = *workDir
	}
	if *logDir != "" {
		cfg.LogDir = *logDir
	}
	_ = cfg.NormalizeAndValidate(false)

	logPath := filepath.Join(cfg.LogDir, "agent.log")
	raw, err := os.ReadFile(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read %s: %v\n", logPath, err)
		return 1
	}
	fmt.Printf("==> %s <==\n", logPath)
	all := strings.Split(strings.TrimRight(string(raw), "\r\n"), "\n")
	if *lines > 0 && len(all) > *lines {
		all = all[len(all)-*lines:]
	}
	fmt.Println(strings.Join(all, "\n"))
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
  runway-agent logs [--log-dir <path>] [-n 100]
  runway-agent update [--repo cubetiqlabs/runway] [--force]
  runway-agent version

Flags (run):
  --server <url>             Runway control plane URL (RUNWAY_AGENT_SERVER)
  --token <rwa_...>          Agent bearer token (RUNWAY_AGENT_TOKEN)
  --runtime <mode>           auto | native | docker (RUNWAY_AGENT_RUNTIME)
  --sandbox <mode>           OCI sandbox runtime: auto | gvisor | runsc | runc (RUNWAY_AGENT_SANDBOX)
  --docker-socket <addr>     Docker socket or tcp:// endpoint (RUNWAY_AGENT_DOCKER_SOCKET)
  --isolated-dind            Per-job isolated DinD sidecar & network (default: true)
  --no-isolated-dind         Disable isolated DinD sidecars
  --dind-image <image>       DinD sidecar image (default: docker:dind)
  --mount-docker-socket      Mount /var/run/docker.sock into runners (disables isolated DinD)
  --no-mount-docker-socket   Disable docker.sock mounting
  --runner-docker-host <u>   External DOCKER_HOST inside runners (disables isolated DinD)
  --runner-network <net>     NetworkMode for Docker runners
  --runner-docker-gid <gid>  Host docker GID for GroupAdd
  --runner-dns <a,b>         Comma-separated DNS servers for runner containers
  --work-dir <path>          Working cache & job directory (RUNWAY_AGENT_WORK_DIR)
  --log-dir <path>           Log directory for agent.log (RUNWAY_AGENT_LOG_DIR)
  --concurrency <n>          Max concurrent runner jobs (RUNWAY_AGENT_CONCURRENCY)
  --labels <a,b>             Extra comma-separated labels (RUNWAY_AGENT_LABELS)
  --no-cache                 Disable persistent build caches on this host
  --no-prewarm-images        Disable background container image prewarming
  --no-auto-update           Disable background auto-updates
  --config <path>            Optional env config file
`, version)
}
