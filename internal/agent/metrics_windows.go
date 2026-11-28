//go:build windows

package agent

import (
	"syscall"
	"unsafe"

	"github.com/cubetiqlabs/runway/internal/domain"
)

type memoryStatusEx struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

func (m *MetricsCollector) initPlatformMetrics() {}

func (m *MetricsCollector) fillPlatformMetrics(snap *domain.AgentMetricsSnapshot) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")

	var mem memoryStatusEx
	mem.dwLength = uint32(unsafe.Sizeof(mem))
	if r, _, _ := kernel32.NewProc("GlobalMemoryStatusEx").Call(uintptr(unsafe.Pointer(&mem))); r != 0 {
		snap.MemoryTotalMB = int(mem.ullTotalPhys / (1024 * 1024))
		if mem.ullTotalPhys >= mem.ullAvailPhys {
			snap.MemoryUsedMB = int((mem.ullTotalPhys - mem.ullAvailPhys) / (1024 * 1024))
		}
		snap.MemoryUsagePercent = float64(mem.dwMemoryLoad)
	}

	if r, _, _ := kernel32.NewProc("GetTickCount64").Call(); r != 0 {
		snap.HostUptimeSec = int64(r / 1000)
	}

	target := m.workDir
	if target == "" {
		target = `C:\`
	}
	if ptr, err := syscall.UTF16PtrFromString(target); err == nil {
		var freeBytesAvail, totalBytes, totalFreeBytes uint64
		if r, _, _ := kernel32.NewProc("GetDiskFreeSpaceExW").Call(
			uintptr(unsafe.Pointer(ptr)),
			uintptr(unsafe.Pointer(&freeBytesAvail)),
			uintptr(unsafe.Pointer(&totalBytes)),
			uintptr(unsafe.Pointer(&totalFreeBytes)),
		); r != 0 && totalBytes > 0 {
			const gb = 1024.0 * 1024.0 * 1024.0
			snap.DiskTotalGB = round2(float64(totalBytes) / gb)
			snap.DiskFreeGB = round2(float64(freeBytesAvail) / gb)
			if totalBytes >= totalFreeBytes {
				snap.DiskUsedGB = round2(float64(totalBytes-totalFreeBytes) / gb)
			}
		}
	}
}
