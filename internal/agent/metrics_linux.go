//go:build linux

package agent

import (
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/cubetiqlabs/runway/internal/domain"
)

func (m *MetricsCollector) initPlatformMetrics() {
	if raw, err := os.ReadFile("/proc/stat"); err == nil {
		if sample, ok := parseLinuxCPUStat(string(raw)); ok {
			m.mu.Lock()
			m.prevCPU = sample
			m.hasPrev = true
			m.mu.Unlock()
		}
	}
}

func parseLinuxCPUStat(raw string) (cpuSample, bool) {
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				var total, idle uint64
				for i := 1; i < len(fields); i++ {
					v, _ := strconv.ParseUint(fields[i], 10, 64)
					total += v
					if i == 4 || i == 5 { // idle + iowait
						idle += v
					}
				}
				return cpuSample{total: total, idle: idle}, true
			}
			break
		}
	}
	return cpuSample{}, false
}

func (m *MetricsCollector) fillPlatformMetrics(snap *domain.AgentMetricsSnapshot) {
	if raw, err := os.ReadFile("/proc/stat"); err == nil {
		if cur, ok := parseLinuxCPUStat(string(raw)); ok {
			m.mu.Lock()
			if m.hasPrev && cur.total > m.prevCPU.total {
				dt := float64(cur.total - m.prevCPU.total)
				di := float64(cur.idle - m.prevCPU.idle)
				snap.CPUUsagePercent = round1((1.0 - di/dt) * 100.0)
			}
			m.prevCPU = cur
			m.hasPrev = true
			m.mu.Unlock()
		}
	}

	if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
		var memTotalKB, memAvailKB, memFreeKB, buffersKB, cachedKB uint64
		var hasAvail bool
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			switch fields[0] {
			case "MemTotal:":
				memTotalKB, _ = strconv.ParseUint(fields[1], 10, 64)
			case "MemAvailable:":
				memAvailKB, _ = strconv.ParseUint(fields[1], 10, 64)
				hasAvail = true
			case "MemFree:":
				memFreeKB, _ = strconv.ParseUint(fields[1], 10, 64)
			case "Buffers:":
				buffersKB, _ = strconv.ParseUint(fields[1], 10, 64)
			case "Cached:":
				cachedKB, _ = strconv.ParseUint(fields[1], 10, 64)
			}
		}
		if !hasAvail {
			memAvailKB = memFreeKB + buffersKB + cachedKB
		}
		if memTotalKB > 0 {
			snap.MemoryTotalMB = int(memTotalKB / 1024)
			if memAvailKB <= memTotalKB {
				snap.MemoryUsedMB = int((memTotalKB - memAvailKB) / 1024)
			}
		}
	}

	if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) >= 3 {
			snap.LoadAvg1, _ = strconv.ParseFloat(fields[0], 64)
			snap.LoadAvg5, _ = strconv.ParseFloat(fields[1], 64)
			snap.LoadAvg15, _ = strconv.ParseFloat(fields[2], 64)
			if snap.CPUUsagePercent == 0 {
				snap.CPUUsagePercent = normalizeCPUFallback(snap.LoadAvg1)
			}
		}
	}

	if raw, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) >= 1 {
			if up, parseErr := strconv.ParseFloat(fields[0], 64); parseErr == nil {
				snap.HostUptimeSec = int64(up)
			}
		}
	}

	var st syscall.Statfs_t
	target := m.workDir
	if target == "" {
		target = "/"
	}
	if err := syscall.Statfs(target, &st); err != nil {
		_ = syscall.Statfs("/", &st)
	}
	if st.Blocks > 0 {
		bsize := uint64(st.Bsize)
		totalBytes := st.Blocks * bsize
		freeBytes := st.Bavail * bsize
		usedBytes := uint64(0)
		if totalBytes > st.Bfree*bsize {
			usedBytes = totalBytes - st.Bfree*bsize
		}
		const gb = 1024.0 * 1024.0 * 1024.0
		snap.DiskTotalGB = round2(float64(totalBytes) / gb)
		snap.DiskUsedGB = round2(float64(usedBytes) / gb)
		snap.DiskFreeGB = round2(float64(freeBytes) / gb)
	}
}
