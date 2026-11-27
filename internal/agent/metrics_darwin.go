//go:build darwin

package agent

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
)

func (m *MetricsCollector) fillPlatformMetrics(snap *domain.AgentMetricsSnapshot) {
	if out, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
		if totalBytes, parseErr := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64); parseErr == nil && totalBytes > 0 {
			snap.MemoryTotalMB = int(totalBytes / (1024 * 1024))
		}
	}

	if out, err := exec.Command("vm_stat").Output(); err == nil {
		pageSize := uint64(4096)
		var active, wired, compressed uint64
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "page size of") {
				parts := strings.Fields(line)
				for i, p := range parts {
					if p == "of" && i+1 < len(parts) {
						if ps, pErr := strconv.ParseUint(parts[i+1], 10, 64); pErr == nil && ps > 0 {
							pageSize = ps
						}
					}
				}
			}
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			numStr := strings.TrimRight(strings.TrimSpace(v), ".")
			val, _ := strconv.ParseUint(numStr, 10, 64)
			switch strings.TrimSpace(k) {
			case "Pages active":
				active = val
			case "Pages wired down":
				wired = val
			case "Pages occupied by compressor":
				compressed = val
			}
		}
		usedBytes := (active + wired + compressed) * pageSize
		snap.MemoryUsedMB = int(usedBytes / (1024 * 1024))
	}

	if out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output(); err == nil {
		cleaned := strings.Trim(strings.TrimSpace(string(out)), "{}")
		fields := strings.Fields(cleaned)
		if len(fields) >= 3 {
			snap.LoadAvg1, _ = strconv.ParseFloat(fields[0], 64)
			snap.LoadAvg5, _ = strconv.ParseFloat(fields[1], 64)
			snap.LoadAvg15, _ = strconv.ParseFloat(fields[2], 64)
			snap.CPUUsagePercent = normalizeCPUFallback(snap.LoadAvg1)
		}
	}

	if out, err := exec.Command("sysctl", "-n", "kern.boottime").Output(); err == nil {
		// Format: { sec = 1732700000, usec = 0 } ...
		if idx := strings.Index(string(out), "sec = "); idx >= 0 {
			rest := string(out)[idx+6:]
			if comma := strings.IndexByte(rest, ','); comma > 0 {
				if sec, pErr := strconv.ParseInt(strings.TrimSpace(rest[:comma]), 10, 64); pErr == nil && sec > 0 {
					snap.HostUptimeSec = time.Now().Unix() - sec
				}
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
