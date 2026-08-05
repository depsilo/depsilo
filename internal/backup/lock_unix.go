//go:build linux || darwin

package backup

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func openLockFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func validateLeaseParent(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect database lease parent: %w", err)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("database directory %q is group/world-writable (mode %04o), so its lease inode could be replaced; make the directory owned by the Depsilo service account and chmod it to 0750 or 0700", path, info.Mode().Perm())
	}
	return nil
}

func verifyLockedFile(file *os.File, path string) error {
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	named, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if named.Mode()&os.ModeSymlink != 0 || !opened.Mode().IsRegular() || !os.SameFile(opened, named) {
		return errors.New("lease path no longer names the locked regular file")
	}
	stat, ok := opened.Sys().(*syscall.Stat_t)
	if ok && stat.Nlink != 1 {
		return fmt.Errorf("locked lease inode has %d links, want exactly one", stat.Nlink)
	}
	return nil
}

func tryLockFile(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

func unlockFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
