//go:build unix

package cli

import (
	"os"
	"syscall"
)

// fileOwnerUID returns the numeric owner of a file.
func fileOwnerUID(info os.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Uid), true
}
