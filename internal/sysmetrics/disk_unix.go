//go:build !windows

package sysmetrics

import "golang.org/x/sys/unix"

// platformDisk returns total and available bytes for the filesystem holding
// path. It uses statfs, so it never walks the cache directory.
func platformDisk(path string) (total int64, free int64, err error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	blockSize := float64(stat.Bsize)
	return int64(float64(stat.Blocks) * blockSize), int64(float64(stat.Bavail) * blockSize), nil
}
