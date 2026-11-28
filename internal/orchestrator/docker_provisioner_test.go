package orchestrator

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	if len(capturedCreate.Cmd) != 1 || !strings.Contains(capturedCreate.Cmd[0], "--jitconfig") || !strings.Contains(capturedCreate.Cmd[0], "unset ENCODED_JIT_CONFIG") || !strings.Contains(capturedCreate.Cmd[0], "/home/runner/.env") || !strings.Contains(capturedCreate.Cmd[0], "RUNNER_TOOL_CACHE") || !strings.Contains(capturedCreate.Cmd[0], "is_rw_exec_dir") {
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
	if !containsString(capturedCreate.Env, "CI=true") {
		t.Fatalf("expected CI=true in Env, got %v", capturedCreate.Env)
	}
	if containsString(capturedCreate.Env, "RUNNER_TOOL_CACHE=/opt/hostedtoolcache") {
		t.Fatalf("expected RUNNER_TOOL_CACHE not forced in Env, got %v", capturedCreate.Env)
	}
}

func TestDockerProvisionerMaybeRefreshLatest(t *testing.T) {
	pulled := make(chan string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/images/create" {
			pulled <- r.URL.Query().Get("fromImage") + ":" + r.URL.Query().Get("tag")
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"Pull complete"}`+"\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := NewDockerProvisioner(srv.URL)
	p.pullMu.Lock()
	p.lastPulled["ghcr.io/cubetiqlabs/runway-runner:latest"] = time.Now().Add(-latestRefreshInterval - time.Minute)
	p.pullMu.Unlock()

	p.recordAndMaybeRefreshLatest("ghcr.io/cubetiqlabs/runway-runner:latest", false)

	select {
	case got := <-pulled:
		if got != "ghcr.io/cubetiqlabs/runway-runner:latest" {
			t.Fatalf("unexpected refreshed image: %s", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for background :latest refresh pull")
	}

	// Non-latest tag should not trigger a background pull.
	p.pullMu.Lock()
	p.lastPulled["ghcr.io/cubetiqlabs/runway-runner:v1.0.0"] = time.Now().Add(-latestRefreshInterval - time.Minute)
	p.pullMu.Unlock()
	p.recordAndMaybeRefreshLatest("ghcr.io/cubetiqlabs/runway-runner:v1.0.0", false)

	select {
	case got := <-pulled:
		t.Fatalf("unexpected pull for non-latest tag: %s", got)
	case <-time.After(100 * time.Millisecond):
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

func TestDockerProvisionerIsolatedDinDRuncPod(t *testing.T) {
	var createdVolume string
	var deletes []string
	creates := make(map[string]map[string]any)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"NCPU":     4,
				"MemTotal": int64(8) * 1024 * 1024 * 1024,
				"Runtimes": map[string]any{"runc": map[string]any{}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/volumes/create":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			createdVolume, _ = req["Name"].(string)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"Name": createdVolume})
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			name := r.URL.Query().Get("name")
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			creates[name] = req
			id := "id-" + name
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": id})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"State": map[string]any{"Running": true}})
		case r.Method == http.MethodDelete:
			deletes = append(deletes, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected Docker API request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	runnerID := uuid.New()
	prov := NewDockerProvisionerWithOptions(ts.URL, DockerOptions{
		IsolatedDinD:   true,
		SandboxRuntime: "auto",
	})
	res, err := prov.Provision(context.Background(), ProvisionRequest{
		RunnerID:         runnerID,
		RunnerName:       "runner-iso-runc",
		EncodedJITConfig: "jit-secret",
		Pool:             domain.RunnerPool{Slug: "pool", VCPU: 2, MemoryMB: 2048},
	})
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	if !strings.HasPrefix(res.ProviderRef, "pod:") {
		t.Fatalf("expected pod:... ProviderRef, got %q", res.ProviderRef)
	}
	if createdVolume == "" {
		t.Fatal("expected per-job volume creation")
	}

	runnerReq := creates["runner-iso-runc"]
	if runnerReq == nil {
		t.Fatal("expected runner container create payload")
	}
	hc := runnerReq["HostConfig"].(map[string]any)
	if !strings.HasPrefix(hc["NetworkMode"].(string), "container:id-runway-dind-") {
		t.Fatalf("expected runner NetworkMode attached to DinD sidecar, got %v", hc["NetworkMode"])
	}
	if int64(hc["MemorySwap"].(float64)) != int64(2048)*1024*1024 {
		t.Fatalf("expected MemorySwap locked equal to Memory, got %v", hc["MemorySwap"])
	}
	var envList []string
	for _, e := range runnerReq["Env"].([]any) {
		envList = append(envList, e.(string))
	}
	if !containsString(envList, "DOCKER_HOST=tcp://127.0.0.1:2375") {
		t.Fatalf("expected DOCKER_HOST=tcp://127.0.0.1:2375, got %v", envList)
	}

	healthy, err := prov.Healthy(context.Background(), res.ProviderRef)
	if err != nil || !healthy {
		t.Fatalf("expected Healthy=true, got healthy=%v err=%v", healthy, err)
	}
	if err := prov.Terminate(context.Background(), res.ProviderRef); err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}
	joined := strings.Join(deletes, ",")
	if !strings.Contains(joined, "/containers/id-runner-iso-runc") || !strings.Contains(joined, "/containers/id-runway-dind-") || !strings.Contains(joined, "/volumes/"+createdVolume) {
		t.Fatalf("expected runner, sidecar, and volume deleted, got %v", deletes)
	}
}

func TestDockerProvisionerIsolatedDinDGVisorSandbox(t *testing.T) {
	var deletes []string
	creates := make(map[string]map[string]any)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"NCPU":     4,
				"MemTotal": int64(8) * 1024 * 1024 * 1024,
				"Runtimes": map[string]any{"runc": map[string]any{}, "runsc": map[string]any{}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/volumes/create":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"Name": req["Name"]})
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			name := r.URL.Query().Get("name")
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			creates[name] = req
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "id-" + name})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/containers/id-runway-dind-") && strings.HasSuffix(r.URL.Path, "/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"State": map[string]any{"Running": true},
				"NetworkSettings": map[string]any{
					"IPAddress": "172.17.0.9",
					"Networks": map[string]any{
						"bridge": map[string]any{"IPAddress": "172.17.0.9"},
					},
				},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/wait"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"StatusCode":0}`))
		case r.Method == http.MethodDelete:
			deletes = append(deletes, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected Docker API request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	prov := NewDockerProvisionerWithOptions(ts.URL, DockerOptions{
		IsolatedDinD:   true,
		SandboxRuntime: "auto",
	})
	res, err := prov.Provision(context.Background(), ProvisionRequest{
		RunnerID:         uuid.New(),
		RunnerName:       "runner-gvisor-1",
		EncodedJITConfig: "jit-secret",
		Pool:             domain.RunnerPool{Slug: "pool", VCPU: 2, MemoryMB: 2048},
	})
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	runnerReq := creates["runner-gvisor-1"]
	hc := runnerReq["HostConfig"].(map[string]any)
	if hc["Runtime"] != "runsc" || hc["NetworkMode"] != "bridge" {
		t.Fatalf("expected Runtime=runsc and NetworkMode=bridge, got Runtime=%v NetworkMode=%v", hc["Runtime"], hc["NetworkMode"])
	}
	var dnsList []string
	for _, d := range hc["Dns"].([]any) {
		dnsList = append(dnsList, d.(string))
	}
	if len(dnsList) != 2 || dnsList[0] != "1.1.1.1" || dnsList[1] != "8.8.8.8" {
		t.Fatalf("expected default gVisor Dns [1.1.1.1 8.8.8.8], got %v", dnsList)
	}
	var hosts []string
	for _, h := range hc["ExtraHosts"].([]any) {
		hosts = append(hosts, h.(string))
	}
	if !containsString(hosts, "dind:172.17.0.9") {
		t.Fatalf("expected ExtraHosts [dind:172.17.0.9], got %v", hosts)
	}
	var envList []string
	for _, e := range runnerReq["Env"].([]any) {
		envList = append(envList, e.(string))
	}
	if !containsString(envList, "DOCKER_HOST=tcp://172.17.0.9:2375") || !containsString(envList, "RUNWAY_DIND_PORT_PROXY=1") {
		t.Fatalf("expected DOCKER_HOST=tcp://172.17.0.9:2375 and RUNWAY_DIND_PORT_PROXY=1, got %v", envList)
	}

	if err := prov.Terminate(context.Background(), res.ProviderRef); err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}
	joined := strings.Join(deletes, ",")
	if !strings.Contains(joined, "/containers/id-runner-gvisor-1") || !strings.Contains(joined, "/containers/id-runway-dind-") {
		t.Fatalf("expected runner and sidecar deleted on Terminate, got %v", deletes)
	}

	// Verify explicit RunnerDNS override is honored.
	provCustomDNS := NewDockerProvisionerWithOptions(ts.URL, DockerOptions{
		IsolatedDinD:   true,
		SandboxRuntime: "gvisor",
		RunnerDNS:      []string{"10.0.0.53"},
	})
	if _, err := provCustomDNS.Provision(context.Background(), ProvisionRequest{
		RunnerID:         uuid.New(),
		RunnerName:       "runner-gvisor-custom-dns",
		EncodedJITConfig: "jit-secret",
		Pool:             domain.RunnerPool{Slug: "pool", VCPU: 2, MemoryMB: 2048},
	}); err != nil {
		t.Fatalf("Provision with custom DNS failed: %v", err)
	}
	customHC := creates["runner-gvisor-custom-dns"]["HostConfig"].(map[string]any)
	customDNS := customHC["Dns"].([]any)
	if len(customDNS) != 1 || customDNS[0].(string) != "10.0.0.53" {
		t.Fatalf("expected custom RunnerDNS [10.0.0.53], got %v", customDNS)
	}
}

func TestDockerProvisionerStrictGVisorFailsWhenUnavailable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/info" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"NCPU":     4,
				"MemTotal": int64(8) * 1024 * 1024 * 1024,
				"Runtimes": map[string]any{"runc": map[string]any{}},
			})
			return
		}
		t.Fatalf("expected no resource creation when strict runsc unavailable, got %s %s", r.Method, r.URL.Path)
	}))
	defer ts.Close()

	prov := NewDockerProvisionerWithOptions(ts.URL, DockerOptions{
		IsolatedDinD:   true,
		SandboxRuntime: "gvisor",
	})
	_, err := prov.Provision(context.Background(), ProvisionRequest{
		RunnerID:         uuid.New(),
		RunnerName:       "runner-strict-fail",
		EncodedJITConfig: "jit-secret",
		Pool:             domain.RunnerPool{Slug: "pool", VCPU: 2, MemoryMB: 1024},
	})
	if err == nil || !strings.Contains(err.Error(), "runsc") {
		t.Fatalf("expected error mentioning runsc when strict gvisor unavailable, got %v", err)
	}
}

func TestDockerProvisionerUltraBuildCacheAndForkRO(t *testing.T) {
	var createdVolumes []string
	var deletedPaths []string
	creates := make(map[string]map[string]any)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"NCPU":     8,
				"MemTotal": int64(16) * 1024 * 1024 * 1024,
				"Runtimes": map[string]any{"runc": map[string]any{}},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/volumes/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/volumes/create":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			name, _ := req["Name"].(string)
			createdVolumes = append(createdVolumes, name)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"Name": name})
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			name := r.URL.Query().Get("name")
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			creates[name] = req
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "id-" + name})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/wait"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"StatusCode":0}`))
		case r.Method == http.MethodDelete:
			deletedPaths = append(deletedPaths, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected Docker API request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	prov := NewDockerProvisionerWithOptions(ts.URL, DockerOptions{
		IsolatedDinD:   true,
		SandboxRuntime: "auto",
		CacheEnabled:   true,
	})

	// 1. Trusted ultra build mounts RW repo cache + DinD slot cache + tmpfs + ulimits.
	res, err := prov.Provision(context.Background(), ProvisionRequest{
		RunnerID:         uuid.New(),
		RunnerName:       "runner-ultra-1",
		EncodedJITConfig: "jit-1",
		RepoFullName:     "cubetiqlabs/runway",
		IsForkPR:         false,
		Pool: domain.RunnerPool{
			Slug:         "ultra-8",
			VCPU:         8,
			MemoryMB:     8192,
			BuildProfile: "ultra",
			CacheEnabled: true,
			CacheScope:   "repo",
		},
	})
	if err != nil {
		t.Fatalf("Ultra Provision failed: %v", err)
	}
	if !strings.Contains(res.ProviderRef, "d=runway-dind-cache-") {
		t.Fatalf("expected DinD slot cache in ProviderRef, got %q", res.ProviderRef)
	}
	hc := creates["runner-ultra-1"]["HostConfig"].(map[string]any)
	if int64(hc["PidsLimit"].(float64)) != 8192 {
		t.Fatalf("expected PidsLimit=8192 for ultra build, got %v", hc["PidsLimit"])
	}
	if _, ok := hc["Tmpfs"].(map[string]any); !ok {
		t.Fatalf("expected Tmpfs mounts for ultra build, got %v", hc["Tmpfs"])
	}
	var binds []string
	for _, b := range hc["Binds"].([]any) {
		binds = append(binds, b.(string))
	}
	hasRWCache := false
	for _, b := range binds {
		if strings.HasSuffix(b, ":/runway-cache") {
			hasRWCache = true
		}
	}
	if !hasRWCache {
		t.Fatalf("expected RW /runway-cache bind, got %v", binds)
	}

	// Terminate must keep persistent cache volumes intact while deleting ephemeral work volume.
	if err := prov.Terminate(context.Background(), res.ProviderRef); err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}
	for _, dp := range deletedPaths {
		if strings.Contains(dp, "runway-cache-") || strings.Contains(dp, "runway-dind-cache-") {
			t.Fatalf("persistent cache volume must not be deleted on Terminate, deleted %s", dp)
		}
	}

	// 2. Fork PR must mount cache read-only and skip DinD cache lease.
	resFork, err := prov.Provision(context.Background(), ProvisionRequest{
		RunnerID:         uuid.New(),
		RunnerName:       "runner-ultra-fork",
		EncodedJITConfig: "jit-2",
		RepoFullName:     "cubetiqlabs/runway",
		IsForkPR:         true,
		Pool: domain.RunnerPool{
			Slug:         "ultra-8",
			VCPU:         4,
			MemoryMB:     4096,
			BuildProfile: "ultra",
			CacheEnabled: true,
			CacheScope:   "repo",
		},
	})
	if err != nil {
		t.Fatalf("Fork ultra Provision failed: %v", err)
	}
	if strings.Contains(resFork.ProviderRef, "d=runway-dind-cache-") {
		t.Fatalf("fork PR must not lease DinD cache, got %q", resFork.ProviderRef)
	}
	forkHC := creates["runner-ultra-fork"]["HostConfig"].(map[string]any)
	hasROCache := false
	for _, b := range forkHC["Binds"].([]any) {
		if strings.HasSuffix(b.(string), ":/runway-cache:ro") {
			hasROCache = true
		}
	}
	if !hasROCache {
		t.Fatalf("expected read-only :/runway-cache:ro for fork PR, got %v", forkHC["Binds"])
	}
	_ = createdVolumes
}

func TestDockerProvisionerFetchLogsDemux(t *testing.T) {
	// Build a multiplexed Docker frame: stream 1 (stdout) + stream 2 (stderr).
	line1 := "2026-11-28T00:00:01Z hello stdout\n"
	line2 := "2026-11-28T00:00:02Z error stderr\n"
	makeFrame := func(stream byte, data string) []byte {
		hdr := []byte{stream, 0, 0, 0, byte(len(data) >> 24), byte(len(data) >> 16), byte(len(data) >> 8), byte(len(data))}
		return append(hdr, []byte(data)...)
	}
	payload := append(makeFrame(1, line1), makeFrame(2, line2)...)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/logs") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	prov := NewDockerProvisioner(ts.URL)
	entries, err := prov.FetchLogs(context.Background(), "cid-1", 100)
	if err != nil {
		t.Fatalf("FetchLogs failed: %v", err)
	}
	if len(entries) != 2 || entries[0].Stream != "stdout" || entries[0].Line != "hello stdout" || entries[1].Stream != "stderr" || entries[1].Level != "error" {
		t.Fatalf("unexpected demuxed log entries: %+v", entries)
	}

	var streamed []domain.RunnerLogEntry
	if err := prov.StreamLogs(context.Background(), "cid-1", func(e domain.RunnerLogEntry) {
		streamed = append(streamed, e)
	}); err != nil {
		t.Fatalf("StreamLogs failed: %v", err)
	}
	if len(streamed) != 2 || streamed[0].Line != "hello stdout" || streamed[1].Stream != "stderr" {
		t.Fatalf("unexpected streamed log entries: %+v", streamed)
	}
}

func TestCacheManagerToolCacheSlotLeasing(t *testing.T) {
	cm := NewCacheManager()
	k1, d1 := cm.AcquireToolCacheSlot("r-abc")
	k2, d2 := cm.AcquireToolCacheSlot("r-abc")
	if d1 == d2 {
		t.Fatalf("expected distinct toolcache dirs for concurrent jobs, both got %q", d1)
	}
	if k1 == k2 {
		t.Fatalf("expected distinct lease keys")
	}
	cm.ReleaseToolCacheSlot(k1)
	k3, d3 := cm.AcquireToolCacheSlot("r-abc")
	if d3 != d1 || k3 != k1 {
		t.Fatalf("expected released slot-0 to be reused, got %q (%q)", d3, k3)
	}
}

func TestDockerProvisionerRunnerNotAutoRemoved(t *testing.T) {
	var autoRemove bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"NCPU": 2, "MemTotal": int64(4) * 1024 * 1024 * 1024})
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			var body struct {
				HostConfig struct {
					AutoRemove bool `json:"AutoRemove"`
				} `json:"HostConfig"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			autoRemove = body.HostConfig.AutoRemove
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"cid-no-autorm"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/containers/cid-no-autorm/start":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	prov := NewDockerProvisioner(ts.URL)
	if _, err := prov.Provision(context.Background(), ProvisionRequest{
		RunnerID:         uuid.New(),
		RunnerName:       "runner-logs-preserved",
		EncodedJITConfig: "jit",
		Pool:             domain.RunnerPool{Slug: "test", VCPU: 2, MemoryMB: 1024},
	}); err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	if autoRemove {
		t.Fatal("expected AutoRemove=false on runner container so exit logs remain fetchable before Terminate")
	}
}
