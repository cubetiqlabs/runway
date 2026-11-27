//go:build !windows

package agent

import (
	"os"
	"strconv"
	"syscall"
)

func detectSocketGID(socketPath string) string {
	info, err := os.Stat(socketPath)
	if err != nil {
		return ""
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return ""
	}
	return strconv.FormatUint(uint64(st.Gid), 10)
}
