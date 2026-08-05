//go:build linux || darwin

package backup

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func filesystemFreeSpace(path string) (string, uint64, error) {
	existing, err := nearestExistingDirectory(path)
	if err != nil {
		return "", 0, err
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(existing, &stat); err != nil {
		return "", 0, err
	}
	if stat.Bsize <= 0 || stat.Bavail < 0 {
		return "", 0, fmt.Errorf("filesystem reported invalid free-space counters")
	}
	info, err := os.Stat(existing)
	if err != nil {
		return "", 0, err
	}
	device := "unknown"
	if raw, ok := info.Sys().(*syscall.Stat_t); ok {
		device = fmt.Sprintf("%d", raw.Dev)
	}
	blockSize := uint64(stat.Bsize)
	availableBlocks := uint64(stat.Bavail)
	if availableBlocks != 0 && blockSize > ^uint64(0)/availableBlocks {
		return "dev:" + device, ^uint64(0), nil
	}
	return "dev:" + device, availableBlocks * blockSize, nil
}
