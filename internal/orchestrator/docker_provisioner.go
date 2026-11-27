package orchestrator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultRunnerImage = "ghcr.io/cubetiqlabs/runway-runner:latest"

const defaultRunnerCmd = `set -e; JIT="$ENCODED_JIT_CONFIG"; unset ENCODED_JIT_CONFIG; if [ -n "${DOCKER_HOST:-}" ] && [ -d /home/runner ]; then touch /home/runner/.env 2>/dev/null || true; if ! grep -q '^DOCKER_HOST=' /home/runner/.env 2>/dev/null; then printf 'DOCKER_HOST=%s\n' "$DOCKER_HOST" >> /home/runner/.env 2>/dev/null || true; fi; fi; if [ -x /home/runner/run.sh ]; then exec /home/runner/run.sh --jitconfig "$JIT"; elif [ -x ./run.sh ]; then exec ./run.sh --jitconfig "$JIT"; else exec run.sh --jitconfig "$JIT"; fi`

// DockerOptions configures how spawned runner containers access Docker for CI steps.
type DockerOptions struct {
	RunnerDockerHost  string
	RunnerNetwork     string
	MountDockerSocket bool
	RunnerDockerGID   string
}

// DockerProvisioner speaks directly to the Docker Engine socket or TCP/HTTP API with zero heavy SDK deps.
type DockerProvisioner struct {
	dockerHost string
	baseURL    string
	opts       DockerOptions
	client     *http.Client
	pullClient *http.Client
}

// NewDockerProvisioner creates a Docker Engine provisioner supporting Unix sockets or HTTP/TCP endpoints.
func NewDockerProvisioner(dockerHost string) *DockerProvisioner {
	return NewDockerProvisionerWithOptions(dockerHost, DockerOptions{})
}

// NewDockerProvisionerWithOptions creates a Docker Engine provisioner configured with runner Docker/network options.
func NewDockerProvisionerWithOptions(dockerHost string, opts DockerOptions) *DockerProvisioner {
	host := strings.TrimSpace(dockerHost)
	if host == "" {
		host = "/var/run/docker.sock"
	}

	var transport *http.Transport
	var baseURL string

	switch {
	case strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://"):
		baseURL = strings.TrimRight(host, "/")
		transport = &http.Transport{
			DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		}
	case strings.HasPrefix(host, "tcp://"):
		baseURL = "http://" + strings.TrimRight(strings.TrimPrefix(host, "tcp://"), "/")
		transport = &http.Transport{
			DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		}
	default:
		socketPath := strings.TrimPrefix(host, "unix://")
		baseURL = "http://docker"
		transport = &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socketPath)
			},
		}
	}

	return &DockerProvisioner{
		dockerHost: host,
		baseURL:    baseURL,
		opts:       opts,
		client:     &http.Client{Transport: transport, Timeout: 30 * time.Second},
		pullClient: &http.Client{Transport: transport, Timeout: 5 * time.Minute},
	}
}

func (d *DockerProvisioner) Name() string { return "docker" }

// Ping checks connectivity to the configured Docker Engine endpoint.
func (d *DockerProvisioner) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+"/_ping", nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("docker ping (%s): %w", d.dockerHost, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("docker ping failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (d *DockerProvisioner) hostInfo(ctx context.Context) (ncpu int, memTotal int64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+"/info", nil)
	if err != nil {
		return 0, 0, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("docker info failed (%d)", resp.StatusCode)
	}
	var info struct {
		NCPU     int   `json:"NCPU"`
		MemTotal int64 `json:"MemTotal"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return 0, 0, err
	}
	return info.NCPU, info.MemTotal, nil
}

func (d *DockerProvisioner) Provision(ctx context.Context, req ProvisionRequest) (*ProvisionResult, error) {
	if strings.TrimSpace(req.EncodedJITConfig) == "" {
		return nil, fmt.Errorf("docker provisioner requires encoded_jit_config (ensure GitHub App credentials and installation permissions are configured)")
	}

	image := strings.TrimSpace(req.Pool.ContainerImage)
	if image == "" {
		image = defaultRunnerImage
	}

	vcpu := req.Pool.VCPU
	if vcpu <= 0 {
		vcpu = 1
	}
	memBytes := int64(req.Pool.MemoryMB) * 1024 * 1024
	if memBytes <= 0 {
		memBytes = 512 * 1024 * 1024
	}

	if ncpu, memTotal, err := d.hostInfo(ctx); err == nil {
		if ncpu > 0 && vcpu > ncpu {
			vcpu = ncpu
		}
		if memTotal > 0 {
			maxMem := int64(float64(memTotal) * 0.85)
			if maxMem < 256*1024*1024 && memTotal >= 256*1024*1024 {
				maxMem = 256 * 1024 * 1024
			} else if maxMem <= 0 {
				maxMem = memTotal
			}
			if memBytes > maxMem {
				memBytes = maxMem
			}
		}
	}

	shmSize := memBytes / 2
	const minShm = int64(64) * 1024 * 1024
	const maxShm = int64(2) * 1024 * 1024 * 1024
	if shmSize < minShm {
		shmSize = minShm
	} else if shmSize > maxShm {
		shmSize = maxShm
	}

	pidsLimit := int64(4096)
	env := []string{
		"CI=true",
		"RUNNER_NAME=" + req.RunnerName,
		"RUNNER_TOOL_CACHE=/opt/hostedtoolcache",
		"AGENT_TOOLSDIRECTORY=/opt/hostedtoolcache",
		"ENCODED_JIT_CONFIG=" + req.EncodedJITConfig,
	}
	hostConfig := map[string]any{
		"AutoRemove":  true,
		"Init":        true,
		"CapDrop":     []string{"ALL"},
		"SecurityOpt": []string{"no-new-privileges:true"},
		"PidsLimit":   pidsLimit,
		"NanoCpus":    int64(vcpu) * 1_000_000_000,
		"Memory":      memBytes,
		"ShmSize":     shmSize,
	}

	if runnerDockerHost := strings.TrimSpace(d.opts.RunnerDockerHost); runnerDockerHost != "" {
		env = append(env, "DOCKER_HOST="+runnerDockerHost)
	} else if d.opts.MountDockerSocket {
		env = append(env, "DOCKER_HOST=unix:///var/run/docker.sock")
		hostConfig["Binds"] = []string{"/var/run/docker.sock:/var/run/docker.sock"}
		if gid := strings.TrimSpace(d.opts.RunnerDockerGID); gid != "" {
			hostConfig["GroupAdd"] = []string{gid}
		}
	}
	if netName := strings.TrimSpace(d.opts.RunnerNetwork); netName != "" {
		hostConfig["NetworkMode"] = netName
	}

	payload := map[string]any{
		"Image":      image,
		"Entrypoint": []string{"/bin/sh", "-c"},
		"Cmd":        []string{defaultRunnerCmd},
		"Env":        env,
		"Labels": map[string]string{
			"runway.managed":   "true",
			"runway.runner_id": req.RunnerID.String(),
			"runway.pool_slug": req.Pool.Slug,
		},
		"HostConfig": hostConfig,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	containerID, err := d.createContainer(ctx, req.RunnerName, body)
	if err != nil && isImageNotFoundError(err) {
		if pullErr := d.pullImage(ctx, image); pullErr != nil {
			return nil, pullErr
		}
		containerID, err = d.createContainer(ctx, req.RunnerName, body)
	}
	if err != nil {
		return nil, err
	}

	startReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/containers/%s/start", d.baseURL, url.PathEscape(containerID)), nil)
	if err != nil {
		_ = d.Terminate(ctx, containerID)
		return nil, err
	}
	startResp, err := d.client.Do(startReq)
	if err != nil {
		_ = d.Terminate(ctx, containerID)
		return nil, fmt.Errorf("docker start container: %w", err)
	}
	startRaw, _ := io.ReadAll(io.LimitReader(startResp.Body, 1<<20))
	startResp.Body.Close()
	if startResp.StatusCode < 200 || startResp.StatusCode >= 300 {
		_ = d.Terminate(ctx, containerID)
		return nil, fmt.Errorf("docker start container failed (%d): %s", startResp.StatusCode, strings.TrimSpace(string(startRaw)))
	}

	hostInfoMap := map[string]any{
		"provider":     "docker",
		"container_id": containerID,
		"image":        image,
		"vcpu":         vcpu,
		"memory_mb":    memBytes / (1024 * 1024),
	}
	if s := strings.TrimSpace(d.opts.RunnerDockerHost); s != "" {
		hostInfoMap["runner_docker_host"] = s
	} else if d.opts.MountDockerSocket {
		hostInfoMap["runner_docker_host"] = "unix:///var/run/docker.sock"
	}
	if s := strings.TrimSpace(d.opts.RunnerNetwork); s != "" {
		hostInfoMap["runner_network"] = s
	}
	hostInfo, _ := json.Marshal(hostInfoMap)
	return &ProvisionResult{ProviderRef: containerID, HostInfo: hostInfo}, nil
}

type dockerStatusError struct {
	statusCode int
	message    string
}

func (e *dockerStatusError) Error() string {
	return fmt.Sprintf("docker create container failed (%d): %s", e.statusCode, e.message)
}

func isImageNotFoundError(err error) bool {
	de, ok := err.(*dockerStatusError)
	return ok && de.statusCode == http.StatusNotFound
}

func (d *DockerProvisioner) createContainer(ctx context.Context, runnerName string, body []byte) (string, error) {
	createURL := d.baseURL + "/containers/create?name=" + url.QueryEscape(runnerName)
	createReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	createReq.Header.Set("Content-Type", "application/json")

	createResp, err := d.client.Do(createReq)
	if err != nil {
		return "", fmt.Errorf("docker create container: %w", err)
	}
	defer createResp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(createResp.Body, 1<<20))
	if createResp.StatusCode < 200 || createResp.StatusCode >= 300 {
		return "", &dockerStatusError{
			statusCode: createResp.StatusCode,
			message:    strings.TrimSpace(string(raw)),
		}
	}

	var created struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		return "", fmt.Errorf("decode docker create response: %w", err)
	}
	if created.ID == "" {
		return "", fmt.Errorf("docker create container returned empty container Id")
	}
	return created.ID, nil
}

func (d *DockerProvisioner) pullImage(ctx context.Context, image string) error {
	repo, tag := splitImageRepoTag(image)
	q := url.Values{}
	q.Set("fromImage", repo)
	if tag != "" {
		q.Set("tag", tag)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/images/create?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := d.pullClient.Do(req)
	if err != nil {
		return fmt.Errorf("docker pull image %q: %w", image, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("docker pull image %q failed (%d): %s", image, resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 16<<20))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var evt struct {
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}
		if json.Unmarshal([]byte(line), &evt) == nil {
			errMsg := evt.Error
			if errMsg == "" {
				errMsg = evt.ErrorDetail.Message
			}
			if errMsg != "" {
				return fmt.Errorf("docker pull image %q: %s", image, errMsg)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("docker pull image %q stream error: %w", image, err)
	}
	return nil
}

func splitImageRepoTag(image string) (repo, tag string) {
	image = strings.TrimSpace(image)
	if strings.Contains(image, "@") {
		return image, ""
	}
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon > lastSlash {
		return image[:lastColon], image[lastColon+1:]
	}
	return image, "latest"
}

func (d *DockerProvisioner) Terminate(ctx context.Context, providerRef string) error {
	if strings.TrimSpace(providerRef) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/containers/%s?force=true", d.baseURL, url.PathEscape(providerRef)), nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound, http.StatusConflict:
		return nil
	default:
		return fmt.Errorf("docker terminate container failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
}

func (d *DockerProvisioner) Healthy(ctx context.Context, providerRef string) (bool, error) {
	if providerRef == "" {
		return false, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/containers/%s/json", d.baseURL, url.PathEscape(providerRef)), nil)
	if err != nil {
		return false, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("docker inspect container failed (%d)", resp.StatusCode)
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
