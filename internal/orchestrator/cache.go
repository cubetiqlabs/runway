package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// CacheManager manages scoped persistent build caches, exclusive per-job toolcache slots,
// and concurrency-safe DinD layer cache slot leases.
type CacheManager struct {
	mu           sync.Mutex
	dindSlots    map[string]map[int]bool
	dindVolIndex map[string]struct {
		scopeKey string
		slot     int
	}
	toolSlots     map[string]map[int]bool
	toolSlotIndex map[string]struct {
		scopeKey string
		slot     int
	}
	knownVolumes  map[string]bool
	healedVolumes map[string]bool

	hits   atomic.Int64
	misses atomic.Int64
}

func NewCacheManager() *CacheManager {
	return &CacheManager{
		dindSlots: make(map[string]map[int]bool),
		dindVolIndex: make(map[string]struct {
			scopeKey string
			slot     int
		}),
		toolSlots: make(map[string]map[int]bool),
		toolSlotIndex: make(map[string]struct {
			scopeKey string
			slot     int
		}),
		knownVolumes:  make(map[string]bool),
		healedVolumes: make(map[string]bool),
	}
}

// ScopeKey derives a deterministic 12-char key for either repo-isolated or pool-wide caching.
func (c *CacheManager) ScopeKey(poolSlug, repoFullName, cacheScope string) string {
	scope := strings.ToLower(strings.TrimSpace(cacheScope))
	if scope == "" {
		scope = "repo"
	}
	var seed string
	switch scope {
	case "none":
		return ""
	case "pool":
		seed = "pool:" + strings.ToLower(strings.TrimSpace(poolSlug))
	default:
		repo := strings.ToLower(strings.TrimSpace(repoFullName))
		if repo == "" {
			seed = "pool:" + strings.ToLower(strings.TrimSpace(poolSlug))
		} else {
			seed = "repo:" + repo
		}
	}
	sum := sha256.Sum256([]byte(seed))
	return string(scope[0]) + "-" + hex.EncodeToString(sum[:6])
}

func (c *CacheManager) BuildCacheVolumeName(scopeKey string) string {
	return "runway-cache-" + scopeKey
}

// TrackVolumeHitMiss records whether a cache volume was already provisioned on this host.
func (c *CacheManager) TrackVolumeHitMiss(volName string, existed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existed || c.knownVolumes[volName] {
		c.hits.Add(1)
	} else {
		c.misses.Add(1)
	}
	c.knownVolumes[volName] = true
}

func (c *CacheManager) IsVolumeHealed(volName string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.healedVolumes[volName]
}

func (c *CacheManager) MarkVolumeHealed(volName string) {
	if volName == "" {
		return
	}
	c.mu.Lock()
	c.healedVolumes[volName] = true
	c.mu.Unlock()
}

func (c *CacheManager) RecordHit()  { c.hits.Add(1) }
func (c *CacheManager) RecordMiss() { c.misses.Add(1) }

func (c *CacheManager) Stats() (hits, misses int64) {
	return c.hits.Load(), c.misses.Load()
}

// AcquireToolCacheSlot leases an exclusive toolcache subdirectory slot per cache scope
// so concurrent runner jobs never corrupt or race on @actions/tool-cache extractions.
func (c *CacheManager) AcquireToolCacheSlot(scopeKey string) (leaseKey, relDir string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	used := c.toolSlots[scopeKey]
	if used == nil {
		used = make(map[int]bool)
		c.toolSlots[scopeKey] = used
	}
	slot := 0
	for used[slot] {
		slot++
	}
	used[slot] = true
	leaseKey = fmt.Sprintf("%s:%d", scopeKey, slot)
	relDir = fmt.Sprintf("slot-%d", slot)
	c.toolSlotIndex[leaseKey] = struct {
		scopeKey string
		slot     int
	}{scopeKey: scopeKey, slot: slot}
	return leaseKey, relDir
}

// ReleaseToolCacheSlot releases the concurrency lease on a toolcache slot.
func (c *CacheManager) ReleaseToolCacheSlot(leaseKey string) {
	if leaseKey == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	info, ok := c.toolSlotIndex[leaseKey]
	if !ok {
		return
	}
	if used := c.toolSlots[info.scopeKey]; used != nil {
		delete(used, info.slot)
	}
	delete(c.toolSlotIndex, leaseKey)
}

// AcquireDinDCacheSlot leases the lowest available slot index for a DinD /var/lib/docker volume
// so concurrent builds never share the same overlay2/containerd store simultaneously.
func (c *CacheManager) AcquireDinDCacheSlot(scopeKey string) (volName string, slot int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	used := c.dindSlots[scopeKey]
	if used == nil {
		used = make(map[int]bool)
		c.dindSlots[scopeKey] = used
	}
	slot = 0
	for used[slot] {
		slot++
	}
	used[slot] = true
	volName = fmt.Sprintf("runway-dind-cache-%s-%d", scopeKey, slot)
	c.dindVolIndex[volName] = struct {
		scopeKey string
		slot     int
	}{scopeKey: scopeKey, slot: slot}
	return volName, slot
}

// ReleaseDinDCacheSlot releases the concurrency lease on a DinD layer cache volume without deleting it.
func (c *CacheManager) ReleaseDinDCacheSlot(volName string) {
	if volName == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	info, ok := c.dindVolIndex[volName]
	if !ok {
		return
	}
	if used := c.dindSlots[info.scopeKey]; used != nil {
		delete(used, info.slot)
	}
	delete(c.dindVolIndex, volName)
}

// CacheEnvVars returns build-tool cache and parallelism environment variables.
func CacheEnvVars(cacheRoot, buildProfile string, vcpu int) []string {
	root := strings.TrimRight(strings.TrimSpace(cacheRoot), "/")
	if root == "" {
		return nil
	}
	return CacheEnvVarsWithToolCache(root, root+"/toolcache", buildProfile, vcpu)
}

// CacheEnvVarsWithToolCache returns build-tool cache variables with an explicit toolcache path.
func CacheEnvVarsWithToolCache(cacheRoot, toolCacheDir, buildProfile string, vcpu int) []string {
	root := strings.TrimRight(strings.TrimSpace(cacheRoot), "/")
	if root == "" {
		return nil
	}
	toolDir := strings.TrimRight(strings.TrimSpace(toolCacheDir), "/")
	if toolDir == "" {
		toolDir = root + "/toolcache"
	}
	if vcpu <= 0 {
		vcpu = 2
	}
	envs := []string{
		"RUNWAY_CACHE_DIR=" + root,
		"GOMODCACHE=" + root + "/gomod",
		"GOCACHE=" + root + "/gocache",
		"NPM_CONFIG_CACHE=" + root + "/npm",
		"BUN_INSTALL_CACHE_DIR=" + root + "/bun",
		"PIP_CACHE_DIR=" + root + "/pip",
		"CARGO_HOME=" + root + "/cargo",
		"CCACHE_DIR=" + root + "/ccache",
		"SCCACHE_DIR=" + root + "/sccache",
		"RUNNER_TOOL_CACHE=" + toolDir,
		"AGENT_TOOLSDIRECTORY=" + toolDir,
	}
	profile := strings.ToLower(strings.TrimSpace(buildProfile))
	if profile == "fast" || profile == "ultra" {
		envs = append(envs,
			"DOCKER_BUILDKIT=1",
			"COMPOSE_DOCKER_CLI_BUILD=1",
			"BUILDX_NO_DEFAULT_ATTESTATIONS=1",
			fmt.Sprintf("MAKEFLAGS=-j%d", vcpu),
			fmt.Sprintf("GOMAXPROCS=%d", vcpu),
			fmt.Sprintf("CARGO_BUILD_JOBS=%d", vcpu),
		)
	}
	return envs
}
