package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/logger"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type stubExecutor struct {
	ran atomic.Int32
}

func (s *stubExecutor) Execute(ctx context.Context, task *TaskPayload) error {
	s.ran.Add(1)
	return nil
}

func TestAgentHeartbeatClaimAndCompleteLoop(t *testing.T) {
	taskID := uuid.New()
	runnerID := uuid.New()
	var heartbeats atomic.Int32
	var claimed atomic.Bool
	var completedStatus atomic.Value

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rwa_testtoken" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/agent/heartbeat":
			heartbeats.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(HeartbeatResponse{Status: "online", CancelTaskIDs: []uuid.UUID{}})
		case "/api/v1/agent/claim":
			if claimed.Swap(true) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(TaskPayload{
				TaskID:           taskID,
				RunnerID:         runnerID,
				GitHubJobID:      9001,
				RepoFullName:     "cubetiqlabs/cambill",
				RunnerName:       "runway-macos-9001-abcdef",
				PoolSlug:         "runway-macos",
				EncodedJITConfig: "fake-jit",
			})
		case "/api/v1/agent/tasks/" + taskID.String() + "/complete":
			var body struct {
				Status string `json:"status"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			completedStatus.Store(body.Status)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cfg := DefaultConfig("1.0.0")
	cfg.ServerURL = ts.URL
	cfg.Token = "rwa_testtoken"
	cfg.PollInterval = 50 * time.Millisecond
	cfg.HeartbeatInterval = 100 * time.Millisecond
	cfg.AutoUpdate = false

	exec := &stubExecutor{}
	ag := &Agent{
		cfg:             cfg,
		log:             zap.NewNop(),
		client:          NewClient(cfg.ServerURL, cfg.Token, cfg.Version),
		executor:        exec,
		updater:         NewUpdater(cfg.UpdateRepo),
		metrics:         NewMetricsCollector(t.TempDir()),
		runtime:         "native",
		hostname:        "test-host",
		sem:             make(chan struct{}, 2),
		cancels:         make(map[uuid.UUID]context.CancelFunc),
		pollNowCh:       make(chan struct{}, 1),
		updateRestartCh: make(chan string, 1),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- ag.Run(ctx)
	}()

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if v, ok := completedStatus.Load().(string); ok && v == "completed" {
			cancel()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := <-done; err != nil {
		t.Fatalf("agent.Run failed: %v", err)
	}
	if heartbeats.Load() < 1 {
		t.Fatal("expected at least 1 heartbeat")
	}
	if exec.ran.Load() != 1 {
		t.Fatalf("expected executor to run 1 task, got %d", exec.ran.Load())
	}
	if got, _ := completedStatus.Load().(string); got != "completed" {
		t.Fatalf("expected task completion status 'completed', got %q", got)
	}
}

func TestResolveDockerOptionsAndLogFile(t *testing.T) {
	cfg := DefaultConfig("1.0.0")
	cfg.DockerSocket = "/var/run/docker.sock"
	opts := ResolveDockerOptions(cfg)
	if !opts.IsolatedDinD || opts.MountDockerSocket || opts.SandboxRuntime != "auto" {
		t.Fatalf("expected secure defaults (IsolatedDinD=true, MountDockerSocket=false, SandboxRuntime=auto), got %+v", opts)
	}

	// Explicit --no-isolated-dind disables per-job sidecar.
	cfg.IsolatedDinD = false
	cfg.IsolatedDinDSet = true
	opts = ResolveDockerOptions(cfg)
	if opts.IsolatedDinD || opts.MountDockerSocket {
		t.Fatalf("expected IsolatedDinD=false and MountDockerSocket=false, got %+v", opts)
	}

	// Explicit host socket mount opt-in disables isolated DinD.
	cfg.IsolatedDinDSet = false
	cfg.MountDockerSocket = true
	cfg.MountDockerSocketSet = true
	opts = ResolveDockerOptions(cfg)
	if !opts.MountDockerSocket || opts.IsolatedDinD {
		t.Fatalf("expected MountDockerSocket=true and IsolatedDinD=false, got %+v", opts)
	}

	// External DinD RunnerDockerHost disables isolated DinD and socket mounting.
	cfg.MountDockerSocket = false
	cfg.MountDockerSocketSet = false
	cfg.RunnerDockerHost = "tcp://runner-dind:2375"
	opts = ResolveDockerOptions(cfg)
	if opts.MountDockerSocket || opts.IsolatedDinD || opts.RunnerDockerHost != "tcp://runner-dind:2375" {
		t.Fatalf("unexpected external DinD docker options: %+v", opts)
	}

	if !IsLocalUnixSocket("unix:///var/run/docker.sock") || IsLocalUnixSocket("tcp://127.0.0.1:2375") {
		t.Fatal("unexpected IsLocalUnixSocket classification")
	}

	logPath := filepath.Join(t.TempDir(), "logs", "agent.log")
	log, closer, fileErr := logger.NewWithFile("production", "info", "json", logPath)
	if fileErr != nil {
		t.Fatalf("expected log file open without error, got: %v", fileErr)
	}
	log.Info("agent-file-log-check")
	_ = log.Sync()
	if closer != nil {
		_ = closer.Close()
	}
	raw, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(raw), "agent-file-log-check") {
		t.Fatalf("expected agent.log to contain test message, got %q err=%v", string(raw), err)
	}
}

func TestAgentDrainAndResumeViaHeartbeat(t *testing.T) {
	var serverStatus atomic.Value
	serverStatus.Store("draining")
	var gotMetrics atomic.Bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/agent/heartbeat" {
			var req HeartbeatRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Metrics != nil && !req.Metrics.CollectedAt.IsZero() {
				gotMetrics.Store(true)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(HeartbeatResponse{
				Status:        serverStatus.Load().(string),
				CancelTaskIDs: []uuid.UUID{},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := DefaultConfig("1.0.0")
	cfg.ServerURL = ts.URL
	cfg.Token = "rwa_test"

	ag := &Agent{
		cfg:             cfg,
		log:             zap.NewNop(),
		client:          NewClient(cfg.ServerURL, cfg.Token, cfg.Version),
		executor:        &stubExecutor{},
		metrics:         NewMetricsCollector(t.TempDir()),
		runtime:         "native",
		hostname:        "test-host",
		sem:             make(chan struct{}, 1),
		cancels:         make(map[uuid.UUID]context.CancelFunc),
		pollNowCh:       make(chan struct{}, 1),
		updateRestartCh: make(chan string, 1),
	}

	ag.sendHeartbeat(context.Background())
	if !ag.draining.Load() {
		t.Fatal("expected agent draining=true when server returns draining")
	}
	if !gotMetrics.Load() {
		t.Fatal("expected heartbeat to include collected metrics snapshot")
	}

	serverStatus.Store("online")
	ag.sendHeartbeat(context.Background())
	if ag.draining.Load() {
		t.Fatal("expected agent draining=false when server returns online")
	}

	snap := ag.metrics.Collect("auto", true, 1, 0, 2, 0, 0, 1200)
	if snap.CollectedAt.IsZero() || snap.MemoryTotalMB <= 0 || snap.DiskTotalGB <= 0 {
		t.Fatalf("expected real host metrics collected, got %+v", snap)
	}
}

func TestDockerExecutorStreamsLogsBeforeTerminate(t *testing.T) {
	var deleted atomic.Bool
	var healthyCalls atomic.Int32
	line := "2026-11-28T00:00:01Z runner live output secret-jit\n"
	frame := append([]byte{1, 0, 0, 0, 0, 0, 0, byte(len(line))}, []byte(line)...)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"NCPU": 2, "MemTotal": int64(4) * 1024 * 1024 * 1024})
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"cid-stream-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/containers/cid-stream-1/start":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/containers/cid-stream-1/logs":
			if deleted.Load() {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(frame)
		case r.Method == http.MethodGet && r.URL.Path == "/containers/cid-stream-1/json":
			running := healthyCalls.Add(1) == 1
			_ = json.NewEncoder(w).Encode(map[string]any{"State": map[string]any{"Running": running}})
		case r.Method == http.MethodDelete && r.URL.Path == "/containers/cid-stream-1":
			deleted.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected docker request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	exec := NewDockerExecutor(ts.URL, orchestrator.DockerOptions{IsolatedDinD: false})
	var collected []domain.RunnerLogEntry
	err := exec.ExecuteWithLogs(context.Background(), &TaskPayload{
		TaskID:           uuid.New(),
		RunnerID:         uuid.New(),
		RunnerName:       "runway-stream-test",
		EncodedJITConfig: "secret-jit",
		VCPU:             2,
		MemoryMB:         1024,
	}, func(entries []domain.RunnerLogEntry) {
		collected = append(collected, entries...)
	})
	if err != nil {
		t.Fatalf("ExecuteWithLogs failed: %v", err)
	}
	if !deleted.Load() {
		t.Fatal("expected container Terminate after execution")
	}
	if len(collected) == 0 || !strings.Contains(collected[0].Line, "runner live output") || strings.Contains(collected[0].Line, "secret-jit") {
		t.Fatalf("expected redacted live log entry, got %+v", collected)
	}
}
