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
	if len(capturedCreate.Cmd) != 1 || !strings.Contains(capturedCreate.Cmd[0], "--jitconfig") || !strings.Contains(capturedCreate.Cmd[0], "unset ENCODED_JIT_CONFIG") {
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
	if !capturedCreate.HostConfig.Init || capturedCreate.HostConfig.PidsLimit != 4096 {
		t.Fatalf("expected Init=true and PidsLimit=4096, got %+v", capturedCreate.HostConfig)
	}
}
