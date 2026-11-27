package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
)

func TestDockerProvisionerPing(t *testing.T) {
	healthy := true
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			if healthy {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("daemon error"))
		}
	}))
	defer ts.Close()

	prov := NewDockerProvisioner(ts.URL)
	if err := prov.Ping(context.Background()); err != nil {
		t.Fatalf("expected Ping to succeed, got: %v", err)
	}

	healthy = false
	if err := prov.Ping(context.Background()); err == nil {
		t.Fatal("expected Ping to fail when daemon returns 500")
	}
}

func TestDockerProvisionerRequiresJITConfig(t *testing.T) {
	prov := NewDockerProvisioner("http://127.0.0.1:0")
	_, err := prov.Provision(context.Background(), ProvisionRequest{
		RunnerID:   uuid.New(),
		RunnerName: "runway-test-1",
		Pool: domain.RunnerPool{
			Slug:     "test",
			VCPU:     2,
			MemoryMB: 1024,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "encoded_jit_config") {
		t.Fatalf("expected missing encoded_jit_config error, got: %v", err)
	}
}

func TestDockerProvisionerPullOn404AndClampResources(t *testing.T) {
	var pulledImage string
	var pulledTag string
	var createCalls int
	var startedID string
	var capturedCreate struct {
		Image      string   `json:"Image"`
		Entrypoint []string `json:"Entrypoint"`
		Cmd        []string `json:"Cmd"`
		Env        []string `json:"Env"`
		HostConfig struct {
			AutoRemove bool  `json:"AutoRemove"`
			Init       bool  `json:"Init"`
			PidsLimit  int64 `json:"PidsLimit"`
			NanoCpus   int64 `json:"NanoCpus"`
			Memory     int64 `json:"Memory"`
			ShmSize    int64 `json:"ShmSize"`
		} `json:"HostConfig"`
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/info":
			w.Header().Set("Content-Type", "application/json")
			// 2 vCPU host with 4 GiB RAM
			_ = json.NewEncoder(w).Encode(map[string]any{
				"NCPU":     2,
				"MemTotal": int64(4) * 1024 * 1024 * 1024,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/images/create":
			pulledImage = r.URL.Query().Get("fromImage")
			pulledTag = r.URL.Query().Get("tag")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"Pulling from actions/actions-runner"}` + "\n" + `{"status":"Downloaded newer image"}` + "\n"))
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			createCalls++
			if err := json.NewDecoder(r.Body).Decode(&capturedCreate); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			if createCalls == 1 {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"No such image"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"container-abc123"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/containers/container-abc123/start":
			startedID = "container-abc123"
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected Docker API request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	prov := NewDockerProvisioner(ts.URL)
	res, err := prov.Provision(context.Background(), ProvisionRequest{
		RunnerID:         uuid.New(),
		RunnerName:       "runway-ubuntu-4vcpu-99-abcdef",
		EncodedJITConfig: "super-secret-jit-payload",
		Pool: domain.RunnerPool{
			Slug:           "runway-ubuntu-4vcpu",
			ContainerImage: "ghcr.io/actions/actions-runner:latest",
			VCPU:           4,    // exceeds host NCPU (2)
			MemoryMB:       8192, // exceeds 85% of 4 GiB
		},
	})
	if err != nil {
		t.Fatalf("expected Provision to succeed, got: %v", err)
	}
	if res.ProviderRef != "container-abc123" || startedID != "container-abc123" {
		t.Fatalf("unexpected container ref/start: %+v started=%q", res, startedID)
	}
	if createCalls != 2 {
		t.Fatalf("expected 2 create calls (404 -> pull -> 201), got %d", createCalls)
	}
	if pulledImage != "ghcr.io/actions/actions-runner" || pulledTag != "latest" {
		t.Fatalf("unexpected pull params: image=%q tag=%q", pulledImage, pulledTag)
	}
	if len(capturedCreate.Entrypoint) != 2 || capturedCreate.Entrypoint[0] != "/bin/sh" {
		t.Fatalf("expected /bin/sh -c entrypoint, got %v", capturedCreate.Entrypoint)
	}
	if len(capturedCreate.Cmd) != 1 || !strings.Contains(capturedCreate.Cmd[0], "--jitconfig") || !strings.Contains(capturedCreate.Cmd[0], "unset ENCODED_JIT_CONFIG") || !strings.Contains(capturedCreate.Cmd[0], "/home/runner/.env") {
		t.Fatalf("unexpected runner cmd: %v", capturedCreate.Cmd)
	}
	if capturedCreate.HostConfig.NanoCpus != 2_000_000_000 {
		t.Fatalf("expected NanoCpus clamped to 2 vCPU, got %d", capturedCreate.HostConfig.NanoCpus)
	}
	hostMemTotal := int64(4) * 1024 * 1024 * 1024
	wantMaxMem := int64(float64(hostMemTotal) * 0.85)
	if capturedCreate.HostConfig.Memory != wantMaxMem {
		t.Fatalf("expected Memory clamped to %d, got %d", wantMaxMem, capturedCreate.HostConfig.Memory)
	}
	if !capturedCreate.HostConfig.Init || capturedCreate.HostConfig.PidsLimit != 4096 || capturedCreate.HostConfig.ShmSize < 64*1024*1024 {
		t.Fatalf("expected Init=true, PidsLimit=4096, and ShmSize>=64MiB, got %+v", capturedCreate.HostConfig)
	}
	if !containsString(capturedCreate.Env, "CI=true") || !containsString(capturedCreate.Env, "RUNNER_TOOL_CACHE=/opt/hostedtoolcache") {
		t.Fatalf("expected CI=true and RUNNER_TOOL_CACHE in Env, got %v", capturedCreate.Env)
	}
}

func TestDockerProvisionerDinDAndHostSocketModes(t *testing.T) {
	var captured struct {
		Env        []string `json:"Env"`
		HostConfig struct {
			NetworkMode string   `json:"NetworkMode"`
			Binds       []string `json:"Binds"`
			GroupAdd    []string `json:"GroupAdd"`
		} `json:"HostConfig"`
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"NCPU": 4, "MemTotal": int64(8) * 1024 * 1024 * 1024})
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"cid-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/containers/cid-1/start":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	// 1. DinD sidecar mode
	provDinD := NewDockerProvisionerWithOptions(ts.URL, DockerOptions{
		RunnerDockerHost: "tcp://runner-dind:2375",
		RunnerNetwork:    "runway-runner-net",
	})
	_, err := provDinD.Provision(context.Background(), ProvisionRequest{
		RunnerID:         uuid.New(),
		RunnerName:       "runner-dind-1",
		EncodedJITConfig: "jit-1",
		Pool:             domain.RunnerPool{Slug: "test", VCPU: 2, MemoryMB: 1024},
	})
	if err != nil {
		t.Fatalf("DinD provision failed: %v", err)
	}
	if !containsString(captured.Env, "DOCKER_HOST=tcp://runner-dind:2375") {
		t.Fatalf("expected DOCKER_HOST=tcp://runner-dind:2375 in Env, got %v", captured.Env)
	}
	if captured.HostConfig.NetworkMode != "runway-runner-net" || len(captured.HostConfig.Binds) != 0 {
		t.Fatalf("unexpected DinD HostConfig: %+v", captured.HostConfig)
	}

	// 2. Host socket mount opt-in mode
	captured = struct {
		Env        []string `json:"Env"`
		HostConfig struct {
			NetworkMode string   `json:"NetworkMode"`
			Binds       []string `json:"Binds"`
			GroupAdd    []string `json:"GroupAdd"`
		} `json:"HostConfig"`
	}{}
	provSock := NewDockerProvisionerWithOptions(ts.URL, DockerOptions{
		MountDockerSocket: true,
		RunnerDockerGID:   "999",
	})
	_, err = provSock.Provision(context.Background(), ProvisionRequest{
		RunnerID:         uuid.New(),
		RunnerName:       "runner-sock-1",
		EncodedJITConfig: "jit-2",
		Pool:             domain.RunnerPool{Slug: "test", VCPU: 2, MemoryMB: 1024},
	})
	if err != nil {
		t.Fatalf("socket mount provision failed: %v", err)
	}
	if !containsString(captured.Env, "DOCKER_HOST=unix:///var/run/docker.sock") {
		t.Fatalf("expected unix socket DOCKER_HOST in Env, got %v", captured.Env)
	}
	if len(captured.HostConfig.Binds) != 1 || captured.HostConfig.Binds[0] != "/var/run/docker.sock:/var/run/docker.sock" {
		t.Fatalf("expected docker.sock bind, got %+v", captured.HostConfig.Binds)
	}
	if len(captured.HostConfig.GroupAdd) != 1 || captured.HostConfig.GroupAdd[0] != "999" {
		t.Fatalf("expected GroupAdd [999], got %+v", captured.HostConfig.GroupAdd)
	}
}

func containsString(items []string, want string) bool {
	for _, s := range items {
		if s == want {
			return true
		}
	}
	return false
}

func TestDockerProvisionerTerminateStatuses(t *testing.T) {
	statusCode := http.StatusNoContent
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		if statusCode >= 400 {
			_, _ = w.Write([]byte(`{"message":"status"}`))
		}
	}))
	defer ts.Close()

	prov := NewDockerProvisioner(ts.URL)
	for _, code := range []int{http.StatusOK, http.StatusNoContent, http.StatusNotFound, http.StatusConflict} {
		statusCode = code
		if err := prov.Terminate(context.Background(), "cid-test"); err != nil {
			t.Fatalf("expected Terminate to treat HTTP %d as nil, got %v", code, err)
		}
	}

	statusCode = http.StatusInternalServerError
	if err := prov.Terminate(context.Background(), "cid-test"); err == nil {
		t.Fatal("expected Terminate to fail on HTTP 500")
	}
}
