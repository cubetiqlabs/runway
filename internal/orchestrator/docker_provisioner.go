package orchestrator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
)

const (
	defaultRunnerImage    = "ghcr.io/cubetiqlabs/runway-runner:latest"
	defaultDinDImage      = "docker:dind"
	latestRefreshInterval = 30 * time.Minute
)

const defaultRunnerCmd = `set -e; umask 0022; JIT="$ENCODED_JIT_CONFIG"; unset ENCODED_JIT_CONFIG;
is_rw_dir() { d="$1"; [ -n "$d" ] || return 1; mkdir -p "$d" 2>/dev/null || return 1; t="$d/.rw-probe-$$"; touch "$t" 2>/dev/null || return 1; rm -f "$t" 2>/dev/null || true; return 0; };
is_rw_exec_dir() { d="$1"; is_rw_dir "$d" || return 1; t="$d/.rw-exec-$$"; printf '#!/bin/sh\nexit 0\n' > "$t" 2>/dev/null || return 1; chmod 0755 "$t" 2>/dev/null || { rm -f "$t"; return 1; }; "$t" >/dev/null 2>&1 || { rm -f "$t"; return 1; }; rm -f "$t" 2>/dev/null || true; return 0; };
FALLBACK_TOOL_CACHE=/home/runner/_work/_tool; [ -d /home/runner ] || FALLBACK_TOOL_CACHE="$(pwd)/_work/_tool";
FALLBACK_CACHE_BASE=/tmp/runway-cache;
if [ -n "${RUNWAY_CACHE_DIR:-}" ] && ! is_rw_dir "${RUNWAY_CACHE_DIR}"; then RUNWAY_CACHE_DIR="$FALLBACK_CACHE_BASE"; export RUNWAY_CACHE_DIR; fi;
ensure_cache_var() { k="$1"; sub="$2"; eval "cur=\${$k:-}"; [ -n "$cur" ] || return 0; if ! is_rw_dir "$cur"; then nv="$FALLBACK_CACHE_BASE/$sub"; mkdir -p "$nv" 2>/dev/null || true; eval "export $k=\"\$nv\""; fi; };
ensure_cache_var GOMODCACHE gomod;
ensure_cache_var GOCACHE gocache;
ensure_cache_var NPM_CONFIG_CACHE npm;
ensure_cache_var BUN_INSTALL_CACHE_DIR bun;
ensure_cache_var PIP_CACHE_DIR pip;
ensure_cache_var CARGO_HOME cargo;
ensure_cache_var CCACHE_DIR ccache;
ensure_cache_var SCCACHE_DIR sccache;
if [ -n "${RUNNER_TOOL_CACHE:-}" ]; then
  if ! is_rw_exec_dir "${RUNNER_TOOL_CACHE}"; then
    export RUNNER_TOOL_CACHE="$FALLBACK_TOOL_CACHE" AGENT_TOOLSDIRECTORY="$FALLBACK_TOOL_CACHE";
    mkdir -p "$RUNNER_TOOL_CACHE" 2>/dev/null || true;
  fi;
elif [ -n "${AGENT_TOOLSDIRECTORY:-}" ] && ! is_rw_exec_dir "${AGENT_TOOLSDIRECTORY}"; then
  export RUNNER_TOOL_CACHE="$FALLBACK_TOOL_CACHE" AGENT_TOOLSDIRECTORY="$FALLBACK_TOOL_CACHE";
  mkdir -p "$RUNNER_TOOL_CACHE" 2>/dev/null || true;
fi;
TC_ACTIVE="${RUNNER_TOOL_CACHE:-${AGENT_TOOLSDIRECTORY:-}}";
if [ -n "$TC_ACTIVE" ] && [ -d "$TC_ACTIVE" ]; then
  chmod -R u+rwX "$TC_ACTIVE" 2>/dev/null || true;
  for vdir in "$TC_ACTIVE"/*/*/*; do
    [ -d "$vdir" ] || continue;
    if [ ! -f "${vdir}.complete" ]; then
      chmod -R u+rwX "$vdir" 2>/dev/null || true;
      rm -rf "$vdir" 2>/dev/null || true;
    fi;
  done;
fi;
if [ -n "${DOCKER_HOST:-}" ] && command -v docker >/dev/null 2>&1; then i=0; while [ "$i" -lt 240 ]; do if docker info >/dev/null 2>&1; then break; fi; i=$((i+1)); sleep 0.1 2>/dev/null || sleep 1; done; fi;
if [ -d /home/runner ]; then touch /home/runner/.env 2>/dev/null || true; for k in DOCKER_HOST RUNWAY_CACHE_DIR RUNNER_TOOL_CACHE AGENT_TOOLSDIRECTORY GOMODCACHE GOCACHE NPM_CONFIG_CACHE BUN_INSTALL_CACHE_DIR PIP_CACHE_DIR CARGO_HOME CCACHE_DIR SCCACHE_DIR DOCKER_BUILDKIT COMPOSE_DOCKER_CLI_BUILD BUILDX_NO_DEFAULT_ATTESTATIONS MAKEFLAGS GOMAXPROCS CARGO_BUILD_JOBS; do eval "val=\${$k:-}"; if [ -n "$val" ]; then if [ -f /home/runner/.env ] && grep -q "^${k}=" /home/runner/.env 2>/dev/null; then grep -v "^${k}=" /home/runner/.env > /home/runner/.env.tmp 2>/dev/null && mv /home/runner/.env.tmp /home/runner/.env 2>/dev/null || true; fi; printf '%s=%s\n' "$k" "$val" >> /home/runner/.env 2>/dev/null || true; fi; done; fi;
if [ "${RUNWAY_DIND_PORT_PROXY:-}" = "1" ] && [ -x /usr/local/bin/runway-portfwd ]; then /usr/local/bin/runway-portfwd >/tmp/runway-portfwd.log 2>&1 & fi;
if [ -x /home/runner/run.sh ]; then exec /home/runner/run.sh --jitconfig "$JIT"; elif [ -x ./run.sh ]; then exec ./run.sh --jitconfig "$JIT"; else exec run.sh --jitconfig "$JIT"; fi`

// DockerOptions configures runner sandboxing and how spawned runner containers access Docker.
type DockerOptions struct {
	RunnerDockerHost  string
	RunnerNetwork     string
	MountDockerSocket bool
	RunnerDockerGID   string
	RunnerDNS         []string
	SandboxRuntime    string // auto | gvisor | runsc | kata | runc | none | <custom>
	IsolatedDinD      bool   // spin up an isolated per-job docker:dind sidecar to eliminate port collisions
	DinDImage         string
	CacheEnabled      bool
	CacheMaxGB        int
}

type dockerHostInfo struct {
	ncpu     int
	memTotal int64
	runtimes map[string]string // lower-case name -> exact registered runtime name
}

type dockerPodRef struct {
	containerID   string
	sidecarID     string
	networkID     string
	volumeName    string
	dindSlotVol   string
	toolSlotLease string
}

// DockerProvisioner speaks directly to the Docker Engine socket or TCP/HTTP API with zero heavy SDK deps.
type DockerProvisioner struct {
	dockerHost   string
	baseURL      string
	opts         DockerOptions
	client       *http.Client
	pullClient   *http.Client
	streamClient *http.Client
	cache        *CacheManager

	pullMu     sync.Mutex
	lastPulled map[string]time.Time
	pulling    map[string]bool
}

// NewDockerProvisioner creates a Docker Engine provisioner supporting Unix sockets or HTTP/TCP endpoints.
func NewDockerProvisioner(dockerHost string) *DockerProvisioner {
	return NewDockerProvisionerWithOptions(dockerHost, DockerOptions{})
}

// NewDockerProvisionerWithOptions creates a Docker Engine provisioner configured with sandbox and Docker options.
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
		dockerHost:   host,
		baseURL:      baseURL,
		opts:         opts,
		client:       &http.Client{Transport: transport, Timeout: 30 * time.Second},
		pullClient:   &http.Client{Transport: transport, Timeout: 5 * time.Minute},
		streamClient: &http.Client{Transport: transport},
		cache:        NewCacheManager(),
		lastPulled:   make(map[string]time.Time),
		pulling:      make(map[string]bool),
	}
}

func (d *DockerProvisioner) Name() string { return "docker" }

func (d *DockerProvisioner) CacheStats() (hits, misses int64) {
	if d.cache == nil {
		return 0, 0
	}
	return d.cache.Stats()
}

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

func (d *DockerProvisioner) hostInfo(ctx context.Context) (dockerHostInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+"/info", nil)
	if err != nil {
		return dockerHostInfo{}, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return dockerHostInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return dockerHostInfo{}, fmt.Errorf("docker info failed (%d)", resp.StatusCode)
	}
	var raw struct {
		NCPU     int            `json:"NCPU"`
		MemTotal int64          `json:"MemTotal"`
		Runtimes map[string]any `json:"Runtimes"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&raw); err != nil {
		return dockerHostInfo{}, err
	}
	runtimes := make(map[string]string, len(raw.Runtimes))
	for k := range raw.Runtimes {
		runtimes[strings.ToLower(strings.TrimSpace(k))] = k
	}
	return dockerHostInfo{
		ncpu:     raw.NCPU,
		memTotal: raw.MemTotal,
		runtimes: runtimes,
	}, nil
}

func (d *DockerProvisioner) resolveSandboxRuntime(info dockerHostInfo, poolMode string) (string, error) {
	mode := strings.TrimSpace(poolMode)
	if mode == "" || strings.EqualFold(mode, "auto") {
		mode = strings.TrimSpace(d.opts.SandboxRuntime)
	}
	lower := strings.ToLower(mode)

	switch lower {
	case "", "auto":
		if exact, ok := info.runtimes["runsc"]; ok {
			return exact, nil
		}
		return "", nil
	case "runc", "none", "default":
		return "", nil
	case "gvisor", "runsc":
		if len(info.runtimes) > 0 {
			if exact, ok := info.runtimes["runsc"]; ok {
				return exact, nil
			}
			return "", fmt.Errorf("gvisor runtime (runsc) requested but not configured in Docker daemon (available: %s)", formatRuntimes(info.runtimes))
		}
		return "runsc", nil
	case "kata", "kata-runtime":
		if len(info.runtimes) > 0 {
			for _, candidate := range []string{"kata", "kata-runtime"} {
				if exact, ok := info.runtimes[candidate]; ok {
					return exact, nil
				}
			}
			return "", fmt.Errorf("kata runtime requested but not configured in Docker daemon (available: %s)", formatRuntimes(info.runtimes))
		}
		return "kata", nil
	default:
		if len(info.runtimes) > 0 {
			if exact, ok := info.runtimes[lower]; ok {
				return exact, nil
			}
			return "", fmt.Errorf("sandbox runtime %q requested but not configured in Docker daemon (available: %s)", mode, formatRuntimes(info.runtimes))
		}
		return mode, nil
	}
}

func formatRuntimes(runtimes map[string]string) string {
	if len(runtimes) == 0 {
		return "none"
	}
	names := make([]string, 0, len(runtimes))
	for _, v := range runtimes {
		names = append(names, v)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
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

	info, infoErr := d.hostInfo(ctx)
	if infoErr == nil {
		if info.ncpu > 0 && vcpu > info.ncpu {
			vcpu = info.ncpu
		}
		if info.memTotal > 0 {
			maxMem := int64(float64(info.memTotal) * 0.85)
			if maxMem < 256*1024*1024 && info.memTotal >= 256*1024*1024 {
				maxMem = 256 * 1024 * 1024
			} else if maxMem <= 0 {
				maxMem = info.memTotal
			}
			if memBytes > maxMem {
				memBytes = maxMem
			}
		}
	} else if lower := strings.ToLower(strings.TrimSpace(d.opts.SandboxRuntime)); lower == "gvisor" || lower == "runsc" {
		return nil, fmt.Errorf("verify gvisor runtime via docker info: %w", infoErr)
	}

	sandboxRuntime, rtErr := d.resolveSandboxRuntime(info, req.Pool.SandboxMode)
	if rtErr != nil {
		return nil, rtErr
	}

	buildProfile := strings.ToLower(strings.TrimSpace(req.Pool.BuildProfile))
	if buildProfile == "" {
		buildProfile = "standard"
	}

	shmSize := memBytes / 2
	const minShm = int64(64) * 1024 * 1024
	maxShm := int64(2) * 1024 * 1024 * 1024
	if buildProfile == "ultra" {
		maxShm = int64(4) * 1024 * 1024 * 1024
	}
	if shmSize < minShm {
		shmSize = minShm
	} else if shmSize > maxShm {
		shmSize = maxShm
	}

	pidsLimit := int64(4096)
	if buildProfile == "ultra" {
		pidsLimit = 8192
	}
	managedLabels := map[string]string{
		"runway.managed":   "true",
		"runway.runner_id": req.RunnerID.String(),
		"runway.pool_slug": req.Pool.Slug,
	}

	env := []string{
		"CI=true",
		"RUNNER_NAME=" + req.RunnerName,
		"ENCODED_JIT_CONFIG=" + req.EncodedJITConfig,
	}
	hostConfig := map[string]any{
		"AutoRemove":  false,
		"Init":        true,
		"CapDrop":     []string{"ALL"},
		"SecurityOpt": []string{"no-new-privileges:true"},
		"PidsLimit":   pidsLimit,
		"NanoCpus":    int64(vcpu) * 1_000_000_000,
		"Memory":      memBytes,
		"MemorySwap":  memBytes,
		"ShmSize":     shmSize,
	}
	if buildProfile == "fast" || buildProfile == "ultra" {
		hostConfig["Ulimits"] = []map[string]any{
			{"Name": "nofile", "Soft": 65536, "Hard": 65536},
		}
		if buildProfile == "ultra" {
			hostConfig["CpuShares"] = 2048
			tmpSize := memBytes / 5
			if tmpSize > 4*1024*1024*1024 {
				tmpSize = 4 * 1024 * 1024 * 1024
			}
			workTmpSize := memBytes / 6
			if workTmpSize > 2*1024*1024*1024 {
				workTmpSize = 2 * 1024 * 1024 * 1024
			}
			if tmpSize >= 128*1024*1024 {
				tmpfs := map[string]string{
					"/tmp": fmt.Sprintf("rw,exec,nosuid,nodev,mode=1777,size=%d", tmpSize),
				}
				if !d.opts.IsolatedDinD {
					tmpfs["/home/runner/_work/_temp"] = fmt.Sprintf("rw,exec,nosuid,nodev,mode=1777,size=%d", workTmpSize)
				}
				hostConfig["Tmpfs"] = tmpfs
			}
		} else {
			hostConfig["CpuShares"] = 1536
			tmpSize := memBytes / 6
			if tmpSize > 2*1024*1024*1024 {
				tmpSize = 2 * 1024 * 1024 * 1024
			}
			if tmpSize >= 128*1024*1024 {
				hostConfig["Tmpfs"] = map[string]string{
					"/tmp": fmt.Sprintf("rw,exec,nosuid,nodev,mode=1777,size=%d", tmpSize),
				}
			}
		}
	}
	if sandboxRuntime != "" {
		hostConfig["Runtime"] = sandboxRuntime
	}

	isGVisor := strings.EqualFold(sandboxRuntime, "runsc")
	dnsServers := d.resolveDNS(sandboxRuntime)

	var pod dockerPodRef
	var binds []string

	cacheScope := strings.ToLower(strings.TrimSpace(req.Pool.CacheScope))
	if cacheScope == "" {
		cacheScope = "repo"
	}
	var cacheScopeKey string
	var cacheVolume string
	if req.Pool.CacheEnabled && cacheScope != "none" && d.cache != nil {
		cacheScopeKey = d.cache.ScopeKey(req.Pool.Slug, req.RepoFullName, cacheScope)
		if cacheScopeKey != "" {
			volCandidate := d.cache.BuildCacheVolumeName(cacheScopeKey)
			existed := d.volumeExists(ctx, volCandidate)
			if createdCacheVol, volErr := d.createVolume(ctx, volCandidate, map[string]string{
				"runway.managed":   "true",
				"runway.cache":     "true",
				"runway.pool_slug": req.Pool.Slug,
				"runway.cache_key": cacheScopeKey,
			}); volErr == nil && createdCacheVol != "" {
				cacheVolume = createdCacheVol
				d.cache.TrackVolumeHitMiss(cacheVolume, existed)
				if req.IsForkPR {
					binds = append(binds, cacheVolume+":/runway-cache:ro")
					env = append(env, CacheEnvVarsWithToolCache("/runway-cache", "/home/runner/_work/_tool", buildProfile, vcpu)...)
				} else {
					d.ensureCacheVolumePermissions(ctx, image, cacheVolume)
					leaseKey, slotDir := d.cache.AcquireToolCacheSlot(cacheScopeKey)
					pod.toolSlotLease = leaseKey
					binds = append(binds, cacheVolume+":/runway-cache")
					env = append(env, CacheEnvVarsWithToolCache("/runway-cache", "/runway-cache/toolcache/"+slotDir, buildProfile, vcpu)...)
				}
			}
		}
	} else if buildProfile == "fast" || buildProfile == "ultra" {
		env = append(env,
			"DOCKER_BUILDKIT=1",
			"COMPOSE_DOCKER_CLI_BUILD=1",
			"BUILDX_NO_DEFAULT_ATTESTATIONS=1",
			fmt.Sprintf("MAKEFLAGS=-j%d", vcpu),
			fmt.Sprintf("GOMAXPROCS=%d", vcpu),
			fmt.Sprintf("CARGO_BUILD_JOBS=%d", vcpu),
		)
	}

	if d.opts.IsolatedDinD && strings.TrimSpace(d.opts.RunnerDockerHost) == "" {
		shortID := strings.ReplaceAll(req.RunnerID.String(), "-", "")
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		if shortID == "" {
			shortID = "job"
		}

		volName := "runway-work-" + shortID
		if createdVol, volErr := d.createVolume(ctx, volName, managedLabels); volErr == nil && createdVol != "" {
			pod.volumeName = createdVol
			binds = append(binds, createdVol+":/home/runner/_work")
		}

		dindImage := strings.TrimSpace(d.opts.DinDImage)
		if dindImage == "" {
			dindImage = defaultDinDImage
		}
		sidecarName := "runway-dind-" + shortID
		sidecarBinds := []string{}
		if pod.volumeName != "" {
			sidecarBinds = append(sidecarBinds, pod.volumeName+":/home/runner/_work")
		}
		if buildProfile == "ultra" && req.Pool.CacheEnabled && !req.IsForkPR && cacheScopeKey != "" && d.cache != nil {
			dindVolCandidate, _ := d.cache.AcquireDinDCacheSlot(cacheScopeKey)
			if createdDinDVol, dindVolErr := d.createVolume(ctx, dindVolCandidate, map[string]string{
				"runway.managed":   "true",
				"runway.cache":     "dind",
				"runway.cache_key": cacheScopeKey,
			}); dindVolErr == nil && createdDinDVol != "" {
				pod.dindSlotVol = dindVolCandidate
				sidecarBinds = append(sidecarBinds, createdDinDVol+":/var/lib/docker")
			} else {
				d.cache.ReleaseDinDCacheSlot(dindVolCandidate)
			}
		}

		sidecarHostConfig := map[string]any{
			"AutoRemove": true,
			"Init":       true,
			"Privileged": !strings.EqualFold(sandboxRuntime, "sysbox-runc"),
			"PidsLimit":  pidsLimit,
			"NanoCpus":   int64(vcpu) * 1_000_000_000,
			"Memory":     memBytes,
			"MemorySwap": memBytes,
		}
		if len(dnsServers) > 0 {
			sidecarHostConfig["Dns"] = dnsServers
		}
		if len(sidecarBinds) > 0 {
			sidecarHostConfig["Binds"] = sidecarBinds
		}

		sidecarCmd := []string{"--host=tcp://127.0.0.1:2375", "--tls=false"}
		if isGVisor {
			sidecarCmd[0] = "--host=tcp://0.0.0.0:2375"
			// Default bridge avoids Docker's embedded 127.0.0.11 DNS iptables DNAT, which gVisor cannot route.
			sidecarHostConfig["NetworkMode"] = "bridge"
		}
		sidecarPayload := map[string]any{
			"Image":      dindImage,
			"Cmd":        sidecarCmd,
			"Env":        []string{"DOCKER_TLS_CERTDIR="},
			"Labels":     managedLabels,
			"HostConfig": sidecarHostConfig,
		}

		sidecarID, sidecarErr := d.createAndStartContainer(ctx, sidecarName, dindImage, sidecarPayload)
		if sidecarErr != nil {
			_ = d.Terminate(context.Background(), formatProviderRef(pod))
			return nil, fmt.Errorf("start isolated dind sidecar: %w", sidecarErr)
		}
		pod.sidecarID = sidecarID

		if isGVisor {
			dindIP, ipErr := d.inspectContainerIP(ctx, sidecarID, "bridge")
			if ipErr != nil {
				_ = d.Terminate(context.Background(), formatProviderRef(pod))
				return nil, fmt.Errorf("resolve isolated dind bridge IP: %w", ipErr)
			}
			hostConfig["NetworkMode"] = "bridge"
			if len(dnsServers) > 0 {
				hostConfig["Dns"] = dnsServers
			}
			hostConfig["ExtraHosts"] = []string{"dind:" + dindIP}
			env = append(env,
				"DOCKER_HOST=tcp://"+dindIP+":2375",
				"RUNWAY_DIND_PORT_PROXY=1",
			)
		} else {
			hostConfig["NetworkMode"] = "container:" + sidecarID
			env = append(env, "DOCKER_HOST=tcp://127.0.0.1:2375")
		}
	} else {
		if runnerDockerHost := strings.TrimSpace(d.opts.RunnerDockerHost); runnerDockerHost != "" {
			env = append(env, "DOCKER_HOST="+runnerDockerHost)
		} else if d.opts.MountDockerSocket {
			env = append(env, "DOCKER_HOST=unix:///var/run/docker.sock")
			binds = append(binds, "/var/run/docker.sock:/var/run/docker.sock")
			if gid := strings.TrimSpace(d.opts.RunnerDockerGID); gid != "" {
				hostConfig["GroupAdd"] = []string{gid}
			}
		}
		if netName := strings.TrimSpace(d.opts.RunnerNetwork); netName != "" {
			hostConfig["NetworkMode"] = netName
		} else if isGVisor {
			hostConfig["NetworkMode"] = "bridge"
		}
		if len(dnsServers) > 0 {
			hostConfig["Dns"] = dnsServers
		}
	}

	if len(binds) > 0 {
		hostConfig["Binds"] = binds
	}

	payload := map[string]any{
		"Image":      image,
		"Entrypoint": []string{"/bin/sh", "-c"},
		"Cmd":        []string{defaultRunnerCmd},
		"Env":        env,
		"Labels":     managedLabels,
		"HostConfig": hostConfig,
	}

	containerID, err := d.createAndStartContainer(ctx, req.RunnerName, image, payload)
	if err != nil {
		if pod.sidecarID != "" || pod.networkID != "" || pod.volumeName != "" || pod.toolSlotLease != "" {
			_ = d.Terminate(context.Background(), formatProviderRef(pod))
		}
		return nil, err
	}
	pod.containerID = containerID

	effectiveRuntime := sandboxRuntime
	if effectiveRuntime == "" {
		effectiveRuntime = "runc"
	}
	hostInfoMap := map[string]any{
		"provider":        "docker",
		"container_id":    containerID,
		"image":           image,
		"vcpu":            vcpu,
		"memory_mb":       memBytes / (1024 * 1024),
		"sandbox_runtime": effectiveRuntime,
		"isolated_dind":   pod.sidecarID != "",
		"build_profile":   buildProfile,
		"cache_volume":    cacheVolume,
		"cache_readonly":  req.IsForkPR && cacheVolume != "",
	}
	if len(dnsServers) > 0 {
		hostInfoMap["runner_dns"] = dnsServers
	}
	if pod.sidecarID != "" {
		hostInfoMap["sidecar_id"] = pod.sidecarID
	}
	if pod.networkID != "" {
		hostInfoMap["isolated_network_id"] = pod.networkID
	}
	if s := strings.TrimSpace(d.opts.RunnerDockerHost); s != "" {
		hostInfoMap["runner_docker_host"] = s
	} else if d.opts.MountDockerSocket && pod.sidecarID == "" {
		hostInfoMap["runner_docker_host"] = "unix:///var/run/docker.sock"
	}
	if s := strings.TrimSpace(d.opts.RunnerNetwork); s != "" && pod.networkID == "" {
		hostInfoMap["runner_network"] = s
	}
	hostInfoBytes, _ := json.Marshal(hostInfoMap)
	return &ProvisionResult{
		ProviderRef: formatProviderRef(pod),
		HostInfo:    hostInfoBytes,
	}, nil
}

func (d *DockerProvisioner) resolveDNS(sandboxRuntime string) []string {
	out := make([]string, 0, len(d.opts.RunnerDNS)+2)
	for _, raw := range d.opts.RunnerDNS {
		if s := strings.TrimSpace(raw); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 && strings.EqualFold(sandboxRuntime, "runsc") {
		out = append(out, "1.1.1.1", "8.8.8.8")
	}
	return out
}

func (d *DockerProvisioner) inspectContainerIP(ctx context.Context, containerID, networkName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/containers/%s/json", d.baseURL, url.PathEscape(containerID)), nil)
	if err != nil {
		return "", err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("docker inspect container IP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("docker inspect container IP failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var inspect struct {
		NetworkSettings struct {
			IPAddress string `json:"IPAddress"`
			Networks  map[string]struct {
				IPAddress string `json:"IPAddress"`
			} `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&inspect); err != nil {
		return "", err
	}
	if networkName != "" {
		if nw, ok := inspect.NetworkSettings.Networks[networkName]; ok && strings.TrimSpace(nw.IPAddress) != "" {
			return strings.TrimSpace(nw.IPAddress), nil
		}
	}
	if ip := strings.TrimSpace(inspect.NetworkSettings.IPAddress); ip != "" {
		return ip, nil
	}
	for _, nw := range inspect.NetworkSettings.Networks {
		if ip := strings.TrimSpace(nw.IPAddress); ip != "" {
			return ip, nil
		}
	}
	return "", fmt.Errorf("docker container %s returned empty IP address", containerID)
}

func (d *DockerProvisioner) createAndStartContainer(ctx context.Context, name, image string, payload map[string]any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	containerID, err := d.createContainer(ctx, name, body)
	pulledNow := false
	if err != nil && isImageNotFoundError(err) {
		if pullErr := d.pullImage(ctx, image); pullErr != nil {
			return "", pullErr
		}
		pulledNow = true
		containerID, err = d.createContainer(ctx, name, body)
	}
	if err != nil {
		return "", err
	}
	d.recordAndMaybeRefreshLatest(image, pulledNow)

	startReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/containers/%s/start", d.baseURL, url.PathEscape(containerID)), nil)
	if err != nil {
		_ = d.removeContainer(ctx, containerID)
		return "", err
	}
	startResp, err := d.client.Do(startReq)
	if err != nil {
		_ = d.removeContainer(ctx, containerID)
		return "", fmt.Errorf("docker start container: %w", err)
	}
	startRaw, _ := io.ReadAll(io.LimitReader(startResp.Body, 1<<20))
	startResp.Body.Close()
	if startResp.StatusCode < 200 || startResp.StatusCode >= 300 {
		_ = d.removeContainer(ctx, containerID)
		return "", fmt.Errorf("docker start container failed (%d): %s", startResp.StatusCode, strings.TrimSpace(string(startRaw)))
	}
	return containerID, nil
}

type dockerStatusError struct {
	op         string
	statusCode int
	message    string
}

func (e *dockerStatusError) Error() string {
	return fmt.Sprintf("docker %s failed (%d): %s", e.op, e.statusCode, e.message)
}

func isImageNotFoundError(err error) bool {
	var de *dockerStatusError
	return errors.As(err, &de) && de.statusCode == http.StatusNotFound
}

func isDockerForbiddenOrUnsupported(err error) bool {
	var de *dockerStatusError
	return errors.As(err, &de) && (de.statusCode == http.StatusForbidden || de.statusCode == http.StatusNotFound || de.statusCode == http.StatusMethodNotAllowed)
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
			op:         "create container",
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

func (d *DockerProvisioner) createNetwork(ctx context.Context, name string, labels map[string]string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"Name":   name,
		"Driver": "bridge",
		"Labels": labels,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/networks/create", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("docker create network: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &dockerStatusError{
			op:         "create network",
			statusCode: resp.StatusCode,
			message:    strings.TrimSpace(string(raw)),
		}
	}
	var out struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode docker network response: %w", err)
	}
	return out.ID, nil
}

func (d *DockerProvisioner) createVolume(ctx context.Context, name string, labels map[string]string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"Name":   name,
		"Labels": labels,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/volumes/create", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("docker create volume: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &dockerStatusError{
			op:         "create volume",
			statusCode: resp.StatusCode,
			message:    strings.TrimSpace(string(raw)),
		}
	}
	var out struct {
		Name string `json:"Name"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Name == "" {
		out.Name = name
	}
	return out.Name, nil
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

func formatProviderRef(pod dockerPodRef) string {
	if pod.sidecarID == "" && pod.networkID == "" && pod.volumeName == "" && pod.dindSlotVol == "" && pod.toolSlotLease == "" {
		return pod.containerID
	}
	parts := make([]string, 0, 6)
	if pod.containerID != "" {
		parts = append(parts, "c="+pod.containerID)
	}
	if pod.sidecarID != "" {
		parts = append(parts, "s="+pod.sidecarID)
	}
	if pod.networkID != "" {
		parts = append(parts, "n="+pod.networkID)
	}
	if pod.volumeName != "" {
		parts = append(parts, "v="+pod.volumeName)
	}
	if pod.dindSlotVol != "" {
		parts = append(parts, "d="+pod.dindSlotVol)
	}
	if pod.toolSlotLease != "" {
		parts = append(parts, "t="+pod.toolSlotLease)
	}
	return "pod:" + strings.Join(parts, ",")
}

func parseProviderRef(raw string) dockerPodRef {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "pod:") {
		return dockerPodRef{containerID: raw}
	}
	var pod dockerPodRef
	for _, item := range strings.Split(strings.TrimPrefix(raw, "pod:"), ",") {
		k, v, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		switch k {
		case "c":
			pod.containerID = v
		case "s":
			pod.sidecarID = v
		case "n":
			pod.networkID = v
		case "v":
			pod.volumeName = v
		case "d":
			pod.dindSlotVol = v
		case "t":
			pod.toolSlotLease = v
		}
	}
	return pod
}

func (d *DockerProvisioner) removeContainer(ctx context.Context, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/containers/%s?force=true", d.baseURL, url.PathEscape(containerID)), nil)
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

func (d *DockerProvisioner) removeNetwork(ctx context.Context, networkID string) error {
	if strings.TrimSpace(networkID) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/networks/%s", d.baseURL, url.PathEscape(networkID)), nil)
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
		return fmt.Errorf("docker remove network failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
}

func (d *DockerProvisioner) removeVolume(ctx context.Context, volumeName string) error {
	if strings.TrimSpace(volumeName) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/volumes/%s?force=true", d.baseURL, url.PathEscape(volumeName)), nil)
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
		return fmt.Errorf("docker remove volume failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
}

func (d *DockerProvisioner) volumeExists(ctx context.Context, volumeName string) bool {
	if strings.TrimSpace(volumeName) == "" {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/volumes/%s", d.baseURL, url.PathEscape(volumeName)), nil)
	if err != nil {
		return false
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (d *DockerProvisioner) ensureCacheVolumePermissions(ctx context.Context, image, cacheVolume string) {
	if d.cache == nil || cacheVolume == "" || d.cache.IsVolumeHealed(cacheVolume) {
		return
	}
	initCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	initName := "runway-cache-init-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	payload := map[string]any{
		"Image":      image,
		"User":       "0:0",
		"Entrypoint": []string{"/bin/sh", "-c"},
		"Cmd": []string{
			"mkdir -p /runway-cache/gomod /runway-cache/gocache /runway-cache/npm /runway-cache/bun /runway-cache/pip /runway-cache/cargo /runway-cache/ccache /runway-cache/sccache /runway-cache/toolcache && chown -R 1001:1001 /runway-cache && chmod -R u+rwX /runway-cache",
		},
		"HostConfig": map[string]any{
			"AutoRemove":  true,
			"NetworkMode": "none",
			"Binds":       []string{cacheVolume + ":/runway-cache"},
		},
	}
	cid, err := d.createAndStartContainer(initCtx, initName, image, payload)
	if err != nil {
		return
	}
	waitReq, reqErr := http.NewRequestWithContext(initCtx, http.MethodPost, fmt.Sprintf("%s/containers/%s/wait", d.baseURL, url.PathEscape(cid)), nil)
	if reqErr == nil {
		if resp, doErr := d.client.Do(waitReq); doErr == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
		}
	}
	d.cache.MarkVolumeHealed(cacheVolume)
}

func (d *DockerProvisioner) Terminate(ctx context.Context, providerRef string) error {
	pod := parseProviderRef(providerRef)
	if pod.dindSlotVol != "" && d.cache != nil {
		d.cache.ReleaseDinDCacheSlot(pod.dindSlotVol)
	}
	if pod.toolSlotLease != "" && d.cache != nil {
		d.cache.ReleaseToolCacheSlot(pod.toolSlotLease)
	}
	var errs []error
	if pod.containerID != "" {
		if err := d.removeContainer(ctx, pod.containerID); err != nil {
			errs = append(errs, err)
		}
	}
	if pod.sidecarID != "" {
		if err := d.removeContainer(ctx, pod.sidecarID); err != nil {
			errs = append(errs, err)
		}
	}
	if pod.networkID != "" {
		if err := d.removeNetwork(ctx, pod.networkID); err != nil {
			errs = append(errs, err)
		}
	}
	if pod.volumeName != "" {
		if err := d.removeVolume(ctx, pod.volumeName); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// FetchLogs retrieves trailing stdout/stderr logs from a runner container and demuxes Docker frames.
func (d *DockerProvisioner) FetchLogs(ctx context.Context, providerRef string, tail int) ([]domain.RunnerLogEntry, error) {
	pod := parseProviderRef(providerRef)
	if pod.containerID == "" {
		return nil, nil
	}
	if tail <= 0 || tail > 2000 {
		tail = 500
	}
	u := fmt.Sprintf("%s/containers/%s/logs?stdout=1&stderr=1&timestamps=1&tail=%s",
		d.baseURL, url.PathEscape(pod.containerID), strconv.Itoa(tail))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("docker logs failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return decodeDockerLogStream(raw), nil
}

// PrewarmImages pulls missing container images in the background to eliminate cold-start pull latency.
func (d *DockerProvisioner) PrewarmImages(ctx context.Context, images []string) {
	seen := make(map[string]struct{}, len(images))
	for _, img := range images {
		img = strings.TrimSpace(img)
		if img == "" {
			continue
		}
		if _, ok := seen[img]; ok {
			continue
		}
		seen[img] = struct{}{}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/images/%s/json", d.baseURL, url.PathEscape(img)), nil)
		if err != nil {
			continue
		}
		resp, err := d.client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				continue
			}
		}
		_ = d.pullImage(ctx, img)
	}
}

// StreamLogs follows stdout/stderr logs from a running container in real time until the container exits or ctx cancels.
func (d *DockerProvisioner) StreamLogs(ctx context.Context, providerRef string, onEntry func(domain.RunnerLogEntry)) error {
	pod := parseProviderRef(providerRef)
	if pod.containerID == "" || onEntry == nil {
		return nil
	}
	u := fmt.Sprintf("%s/containers/%s/logs?stdout=1&stderr=1&timestamps=1&follow=1&tail=all",
		d.baseURL, url.PathEscape(pod.containerID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	client := d.streamClient
	if client == nil {
		client = d.client
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("docker stream logs failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	br := bufio.NewReaderSize(resp.Body, 64*1024)
	var useRawFallback bool
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if useRawFallback {
			line, readErr := br.ReadString('\n')
			if strings.TrimSpace(line) != "" {
				if entry, ok := parseDockerLogLine("stdout", line); ok {
					onEntry(entry)
				}
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					return nil
				}
				return readErr
			}
			continue
		}

		hdr, peekErr := br.Peek(8)
		if peekErr != nil {
			if len(hdr) > 0 {
				if entry, ok := parseDockerLogLine("stdout", string(hdr)); ok {
					onEntry(entry)
				}
			}
			if errors.Is(peekErr, io.EOF) {
				return nil
			}
			return peekErr
		}
		streamType := hdr[0]
		if (streamType == 1 || streamType == 2) && hdr[1] == 0 && hdr[2] == 0 && hdr[3] == 0 {
			size := int(hdr[4])<<24 | int(hdr[5])<<16 | int(hdr[6])<<8 | int(hdr[7])
			if size >= 0 && size <= 4*1024*1024 {
				if _, discErr := br.Discard(8); discErr != nil {
					return discErr
				}
				payload := make([]byte, size)
				if _, fullErr := io.ReadFull(br, payload); fullErr != nil {
					if len(payload) > 0 {
						stream := "stdout"
						if streamType == 2 {
							stream = "stderr"
						}
						for _, e := range splitDockerLogLines(stream, payload) {
							onEntry(e)
						}
					}
					if errors.Is(fullErr, io.EOF) || errors.Is(fullErr, io.ErrUnexpectedEOF) {
						return nil
					}
					return fullErr
				}
				stream := "stdout"
				if streamType == 2 {
					stream = "stderr"
				}
				for _, e := range splitDockerLogLines(stream, payload) {
					onEntry(e)
				}
				continue
			}
		}

		useRawFallback = true
	}
}

func parseDockerLogLine(stream, rawLine string) (domain.RunnerLogEntry, bool) {
	line := strings.TrimRight(rawLine, "\r\n")
	if strings.TrimSpace(line) == "" {
		return domain.RunnerLogEntry{}, false
	}
	ts := time.Now().UTC()
	text := line
	if idx := strings.IndexByte(line, ' '); idx > 0 {
		if parsed, err := time.Parse(time.RFC3339Nano, line[:idx]); err == nil {
			ts = parsed.UTC()
			text = line[idx+1:]
		}
	}
	lvl := "info"
	if stream == "stderr" {
		lvl = "error"
	}
	return domain.RunnerLogEntry{
		Timestamp: ts,
		Stream:    stream,
		Level:     lvl,
		Line:      text,
	}, true
}

func splitDockerLogLines(stream string, chunk []byte) []domain.RunnerLogEntry {
	var out []domain.RunnerLogEntry
	for _, rawLine := range strings.Split(string(chunk), "\n") {
		if entry, ok := parseDockerLogLine(stream, rawLine); ok {
			out = append(out, entry)
		}
	}
	return out
}

func decodeDockerLogStream(raw []byte) []domain.RunnerLogEntry {
	var out []domain.RunnerLogEntry
	appendLines := func(stream string, chunk []byte) {
		out = append(out, splitDockerLogLines(stream, chunk)...)
	}

	// Attempt Docker 8-byte multiplexed frame decoding ([stream,0,0,0,size_be_u32]).
	i := 0
	framed := false
	for i+8 <= len(raw) {
		streamType := raw[i]
		if (streamType == 1 || streamType == 2) && raw[i+1] == 0 && raw[i+2] == 0 && raw[i+3] == 0 {
			size := int(raw[i+4])<<24 | int(raw[i+5])<<16 | int(raw[i+6])<<8 | int(raw[i+7])
			if size >= 0 && i+8+size <= len(raw) {
				framed = true
				stream := "stdout"
				if streamType == 2 {
					stream = "stderr"
				}
				appendLines(stream, raw[i+8:i+8+size])
				i += 8 + size
				continue
			}
		}
		break
	}
	if !framed {
		appendLines("stdout", raw)
	}
	return out
}

func (d *DockerProvisioner) Healthy(ctx context.Context, providerRef string) (bool, error) {
	pod := parseProviderRef(providerRef)
	if pod.containerID == "" {
		return false, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/containers/%s/json", d.baseURL, url.PathEscape(pod.containerID)), nil)
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

// recordAndMaybeRefreshLatest tracks when an image was last pulled and triggers
// a non-blocking background pull for :latest images older than latestRefreshInterval.
func (d *DockerProvisioner) recordAndMaybeRefreshLatest(image string, pulledNow bool) {
	_, tag := splitImageRepoTag(image)
	if tag != "latest" {
		return
	}

	now := time.Now()
	d.pullMu.Lock()
	if d.lastPulled == nil {
		d.lastPulled = make(map[string]time.Time)
	}
	if d.pulling == nil {
		d.pulling = make(map[string]bool)
	}
	if pulledNow {
		d.lastPulled[image] = now
		d.pullMu.Unlock()
		return
	}
	last, ok := d.lastPulled[image]
	if !ok {
		d.lastPulled[image] = now
		d.pullMu.Unlock()
		return
	}
	if now.Sub(last) < latestRefreshInterval || d.pulling[image] {
		d.pullMu.Unlock()
		return
	}
	d.pulling[image] = true
	d.pullMu.Unlock()

	go func() {
		refreshCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		err := d.pullImage(refreshCtx, image)
		d.pullMu.Lock()
		delete(d.pulling, image)
		if err == nil {
			d.lastPulled[image] = time.Now()
		}
		d.pullMu.Unlock()
	}()
}
