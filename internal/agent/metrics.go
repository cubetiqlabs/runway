package agent

import (
	"io/fs"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
)

// MetricsCollector samples host CPU, memory, disk, load, uptime, and cache telemetry.
type MetricsCollector struct {
	workDir   string
	startedAt time.Time

	mu       sync.Mutex
	prevCPU  cpuSample
	hasPrev  bool
	cacheDir string
}

type cpuSample struct {
	total uint64
	idle  uint64
}

func NewMetricsCollector(workDir string) *MetricsCollector {
	return &MetricsCollector{
		workDir:   workDir,
		cacheDir:  filepath.Join(workDir, "build-cache"),
		startedAt: time.Now(),
	}
}

func (m *MetricsCollector) Collect(sandboxRuntime string, isolatedDinD bool, cacheHits, cacheMisses, completed, failed, cancelled, avgDurMS int64) domain.AgentMetricsSnapshot {
	snap := domain.AgentMetricsSnapshot{
		UptimeSec:         int64(time.Since(m.startedAt).Seconds()),
		SandboxRuntime:    sandboxRuntime,
		IsolatedDinD:      isolatedDinD,
		CacheHitCount:     cacheHits,
		CacheMissCount:    cacheMisses,
		TasksCompleted:    completed,
		TasksFailed:       failed,
		TasksCancelled:    cancelled,
		AvgTaskDurationMS: avgDurMS,
		CollectedAt:       time.Now().UTC(),
	}

	m.fillPlatformMetrics(&snap)

	if snap.MemoryTotalMB > 0 && snap.MemoryUsagePercent <= 0 {
		snap.MemoryUsagePercent = round1(float64(snap.MemoryUsedMB) * 100.0 / float64(snap.MemoryTotalMB))
	}
	if snap.DiskTotalGB > 0 && snap.DiskUsagePercent <= 0 {
		snap.DiskUsagePercent = round1(snap.DiskUsedGB * 100.0 / snap.DiskTotalGB)
	}
	snap.CacheSizeMB = quickDirSizeMB(m.cacheDir, 2000)
	return snap
}

func round1(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return float64(int(v*10+0.5)) / 10
}

func round2(v float64) float64 {
	if v < 0 {
		return 0
	}
	return float64(int(v*100+0.5)) / 100
}

func quickDirSizeMB(root string, maxEntries int) int64 {
	var total int64
	count := 0
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		count++
		if maxEntries > 0 && count > maxEntries {
			return filepath.SkipAll
		}
		if !d.IsDir() {
			if info, infoErr := d.Info(); infoErr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total / (1024 * 1024)
}

func normalizeCPUFallback(load1 float64) float64 {
	cpus := runtime.NumCPU()
	if cpus <= 0 {
		cpus = 1
	}
	return round1((load1 / float64(cpus)) * 100.0)
}
