package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// DockerProvisioner speaks directly to the Docker Engine Unix socket with zero heavy SDK deps.
type DockerProvisioner struct {
	socketPath string
	client     *http.Client
}

func NewDockerProvisioner(socketPath string) *DockerProvisioner {
	if strings.TrimSpace(socketPath) == "" {
		socketPath = "/var/run/docker.sock"
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socketPath)
		},
	}
	return &DockerProvisioner{
		socketPath: socketPath,
		client:     &http.Client{Transport: transport, Timeout: 30 * time.Second},
	}
}

func (d *DockerProvisioner) Name() string { return "docker" }

func (d *DockerProvisioner) Provision(ctx context.Context, req ProvisionRequest) (*ProvisionResult, error) {
	image := req.Pool.ContainerImage
	if strings.TrimSpace(image) == "" {
		image = "ghcr.io/actions/actions-runner:latest"
	}

	pidsLimit := int64(512)
	payload := map[string]any{
		"Image": image,
		"Env": []string{
			"RUNNER_NAME=" + req.RunnerName,
			"ENCODED_JIT_CONFIG=" + req.EncodedJITConfig,
		},
		"Labels": map[string]string{
			"runway.managed":   "true",
			"runway.runner_id": req.RunnerID.String(),
			"runway.pool_slug": req.Pool.Slug,
		},
		"HostConfig": map[string]any{
			"AutoRemove":  true,
			"CapDrop":     []string{"ALL"},
			"SecurityOpt": []string{"no-new-privileges:true"},
			"PidsLimit":   pidsLimit,
			"NanoCpus":    int64(req.Pool.VCPU) * 1_000_000_000,
			"Memory":      int64(req.Pool.MemoryMB) * 1024 * 1024,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	createReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker/containers/create?name="+req.RunnerName, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	createReq.Header.Set("Content-Type", "application/json")

	createResp, err := d.client.Do(createReq)
	if err != nil {
		return nil, fmt.Errorf("docker create container: %w", err)
	}
	defer createResp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(createResp.Body, 1<<20))
	if createResp.StatusCode < 200 || createResp.StatusCode >= 300 {
		return nil, fmt.Errorf("docker create container failed (%d): %s", createResp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var created struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		return nil, fmt.Errorf("decode docker create response: %w", err)
	}

	startReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://docker/containers/%s/start", created.ID), nil)
	if err != nil {
		return nil, err
	}
	startResp, err := d.client.Do(startReq)
	if err != nil {
		return nil, fmt.Errorf("docker start container: %w", err)
	}
	startResp.Body.Close()
	if startResp.StatusCode < 200 || startResp.StatusCode >= 300 {
		return nil, fmt.Errorf("docker start container failed (%d)", startResp.StatusCode)
	}

	hostInfo, _ := json.Marshal(map[string]any{
		"provider":     "docker",
		"container_id": created.ID,
		"image":        image,
		"vcpu":         req.Pool.VCPU,
		"memory_mb":    req.Pool.MemoryMB,
	})
	return &ProvisionResult{ProviderRef: created.ID, HostInfo: hostInfo}, nil
}

func (d *DockerProvisioner) Terminate(ctx context.Context, providerRef string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("http://docker/containers/%s?force=true", providerRef), nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (d *DockerProvisioner) Healthy(ctx context.Context, providerRef string) (bool, error) {
	if providerRef == "" {
		return false, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://docker/containers/%s/json", providerRef), nil)
	if err != nil {
		return false, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	var inspect struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&inspect); err != nil {
		return false, err
	}
	return inspect.State.Running, nil
}
