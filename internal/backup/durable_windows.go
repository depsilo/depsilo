//go:build windows

package backup

import "golang.org/x/sys/windows"

func publishFile(source, target string) error {
	sourcePath, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetPath, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		sourcePath,
		targetPath,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}

// Windows has no directory fsync equivalent. MoveFileEx with WRITE_THROUGH
// covers archive publication; restored files themselves are synced before
// rename.
func syncDirectory(string) error { return nil }
