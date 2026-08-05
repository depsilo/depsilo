//go:build windows

package backup

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func filesystemFreeSpace(path string) (string, uint64, error) {
	existing, err := nearestExistingDirectory(path)
	if err != nil {
		return "", 0, err
	}
	pointer, err := windows.UTF16PtrFromString(existing)
	if err != nil {
		return "", 0, err
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(pointer, &available, nil, nil); err != nil {
		return "", 0, err
	}
	volume := strings.ToLower(filepath.VolumeName(existing))
	if volume == "" {
		volume = strings.ToLower(existing)
	}
	return volume, available, nil
}
