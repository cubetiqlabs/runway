package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/orchestrator"
)

type Config struct {
	ServerURL            string
	Token                string
	Runtime              string // auto | native | docker
	DockerSocket         string
	RunnerDockerHost     string
	RunnerNetwork        string
	MountDockerSocket    bool
	MountDockerSocketSet bool
	RunnerDockerGID      string
	WorkDir              string
	LogDir               string
	Concurrency       int
	Labels            []string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	AutoUpdate        bool
	UpdateInterval    time.Duration
	UpdateRepo        string
	RunnerVersion     string
	Version           string
}

func DefaultConfig(version string) Config {
	wd := defaultWorkDir()
	return Config{
		Runtime:           "auto",
		DockerSocket:      defaultDockerSocket(),
		WorkDir:           wd,
		LogDir:            DefaultLogDir(wd),
		Concurrency:       2,
		PollInterval:      3 * time.Second,
		HeartbeatInterval: 15 * time.Second,
		AutoUpdate:        true,
		UpdateInterval:    15 * time.Minute,
		UpdateRepo:        "cubetiqlabs/runway",
		Version:           version,
	}
}

func (c *Config) ApplyEnvAndFile(configPath string) error {
	if configPath != "" {
		if err := loadEnvFile(configPath); err != nil {
			return fmt.Errorf("load config file %q: %w", configPath, err)
		}
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_SERVER")); v != "" {
		c.ServerURL = v
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_TOKEN")); v != "" {
		c.Token = v
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_RUNTIME")); v != "" {
		c.Runtime = strings.ToLower(v)
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_DOCKER_SOCKET")); v != "" {
		c.DockerSocket = v
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_RUNNER_DOCKER_HOST")); v != "" {
		c.RunnerDockerHost = v
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_RUNNER_NETWORK")); v != "" {
		c.RunnerNetwork = v
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_MOUNT_DOCKER_SOCKET")); v != "" {
		c.MountDockerSocket = strings.EqualFold(v, "true") || v == "1"
		c.MountDockerSocketSet = true
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_RUNNER_DOCKER_GID")); v != "" {
		c.RunnerDockerGID = v
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_WORK_DIR")); v != "" {
		c.WorkDir = v
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_LOG_DIR")); v != "" {
		c.LogDir = v
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_LABELS")); v != "" {
		c.Labels = splitLabels(v)
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_CONCURRENCY")); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			c.Concurrency = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_POLL_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Second {
			c.PollInterval = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_AUTO_UPDATE")); v != "" {
		c.AutoUpdate = !strings.EqualFold(v, "false") && v != "0"
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_UPDATE_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Minute {
			c.UpdateInterval = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_UPDATE_REPO")); v != "" {
		c.UpdateRepo = v
	}
	if v := strings.TrimSpace(os.Getenv("RUNWAY_AGENT_RUNNER_VERSION")); v != "" {
		c.RunnerVersion = strings.TrimPrefix(v, "v")
	}
	return nil
}

func (c *Config) NormalizeAndValidate(requireAuth bool) error {
	c.ServerURL = strings.TrimRight(strings.TrimSpace(c.ServerURL), "/")
	c.Token = strings.TrimSpace(c.Token)
	c.Runtime = strings.ToLower(strings.TrimSpace(c.Runtime))
	switch c.Runtime {
	case "", "auto", "native", "docker":
		if c.Runtime == "" {
			c.Runtime = "auto"
		}
	default:
		return fmt.Errorf("invalid runtime %q: must be 'auto', 'native', or 'docker'", c.Runtime)
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 1
	}
	if c.PollInterval < time.Second {
		c.PollInterval = 3 * time.Second
	}
	if c.HeartbeatInterval < 5*time.Second {
		c.HeartbeatInterval = 15 * time.Second
	}
	if c.UpdateInterval < time.Minute {
		c.UpdateInterval = 15 * time.Minute
	}
	if c.WorkDir == "" {
		c.WorkDir = defaultWorkDir()
	}
	if c.LogDir == "" {
		c.LogDir = DefaultLogDir(c.WorkDir)
	}
	if requireAuth {
		if c.ServerURL == "" {
			return fmt.Errorf("--server / RUNWAY_AGENT_SERVER is required")
		}
		if c.Token == "" {
			return fmt.Errorf("--token / RUNWAY_AGENT_TOKEN is required")
		}
	}
	return nil
}

func loadEnvFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+1:])
		v = strings.Trim(v, `"'`)
		if k != "" && os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
	return nil
}

func splitLabels(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func defaultDockerSocket() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return "/var/run/docker.sock"
}

func defaultWorkDir() string {
	if runtime.GOOS == "windows" {
		if pd := strings.TrimSpace(os.Getenv("ProgramData")); pd != "" {
			return filepath.Join(pd, "RunwayAgent")
		}
		return filepath.Join(os.TempDir(), "runway-agent")
	}
	if os.Geteuid() == 0 {
		return "/var/lib/runway-agent"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "share", "runway-agent")
	}
	return filepath.Join(os.TempDir(), "runway-agent")
}

// DefaultLogDir returns /var/log/runway-agent for root on Unix or <workDir>/logs otherwise.
func DefaultLogDir(workDir string) string {
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		return "/var/log/runway-agent"
	}
	if workDir == "" {
		workDir = defaultWorkDir()
	}
	return filepath.Join(workDir, "logs")
}

// IsLocalUnixSocket reports whether dockerHost targets a local Unix domain socket.
func IsLocalUnixSocket(dockerHost string) bool {
	h := strings.TrimSpace(dockerHost)
	return h == "" || strings.HasPrefix(h, "/") || strings.HasPrefix(h, "unix://")
}

// ResolveDockerOptions resolves Docker access inside spawned runner containers, auto-mounting the local Unix Docker socket when appropriate.
func ResolveDockerOptions(cfg Config) orchestrator.DockerOptions {
	mountSock := cfg.MountDockerSocket
	gid := strings.TrimSpace(cfg.RunnerDockerGID)
	if strings.TrimSpace(cfg.RunnerDockerHost) == "" && IsLocalUnixSocket(cfg.DockerSocket) {
		if !cfg.MountDockerSocketSet {
			mountSock = true
		}
		if mountSock && gid == "" {
			sockPath := strings.TrimPrefix(strings.TrimSpace(cfg.DockerSocket), "unix://")
			if sockPath == "" {
				sockPath = "/var/run/docker.sock"
			}
			gid = detectSocketGID(sockPath)
		}
	}
	return orchestrator.DockerOptions{
		RunnerDockerHost:  cfg.RunnerDockerHost,
		RunnerNetwork:     cfg.RunnerNetwork,
		MountDockerSocket: mountSock,
		RunnerDockerGID:   gid,
	}
}

func NormalizeOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

func NormalizeArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	case "386":
		return "x86"
	default:
		return runtime.GOARCH
	}
}
