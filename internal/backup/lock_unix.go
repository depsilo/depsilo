//go:build linux || darwin

package backup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// openLockFile opens the lease file and reports whether this call created
// it. Creation is exclusive, so a created file is one nobody else has opened.
func openLockFile(path string) (file *os.File, created bool, err error) {
	const flags = unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW
	// The second round is for a file another process creates between the
	// two calls of the first.
	for attempt := 0; attempt < 2; attempt++ {
		var fd int
		fd, err = unix.Open(path, flags, 0)
		if errors.Is(err, unix.ENOENT) {
			fd, err = unix.Open(path, flags|unix.O_CREAT|unix.O_EXCL, 0o600)
			if errors.Is(err, unix.EEXIST) {
				continue
			}
			created = err == nil
		}
		if err != nil {
			return nil, false, err
		}
		return os.NewFile(uintptr(fd), path), created, nil
	}
	return nil, false, err
}

// openNewDirectory opens the directory itself: a symbolic link or anything
// else at that name is an error.
func openNewDirectory(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

// verifyNewDirectory checks that an opened directory can be the one this
// process created a moment ago.
func verifyNewDirectory(directory *os.File) error {
	return newDirectoryProblem(os.Geteuid(), directory)
}

// newDirectoryProblem reports why an opened directory is not one the
// process just created: a new directory belongs to its creator and is empty.
// A directory that passes and is not the created one is another empty
// directory of the creator's, which holds nothing to give away.
func newDirectoryProblem(effectiveUID int, directory *os.File) error {
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("its owner is unknown")
	}
	if int(stat.Uid) != effectiveUID {
		return fmt.Errorf("it belongs to uid %d, not to uid %d that created it", stat.Uid, effectiveUID)
	}
	names, err := directory.Readdirnames(1)
	if len(names) != 0 {
		return errors.New("it is not empty")
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func processNewEntryOwner(receivingDirectory os.FileInfo) (uid, gid int, change bool) {
	return newEntryOwnerFor(os.Geteuid(), receivingDirectory)
}

// newEntryOwnerFor decides who owns an entry the process created, from the
// directory that receives it. Root working in another account's directory
// (an operator running backup, restore or reset-password with sudo) would
// leave entries that account cannot open, and the service's next start would
// fail on them: such entries go to the directory's owner and group. Everyone
// else, and root in its own directory, keeps what it created.
func newEntryOwnerFor(effectiveUID int, receivingDirectory os.FileInfo) (uid, gid int, change bool) {
	stat, ok := receivingDirectory.Sys().(*syscall.Stat_t)
	if effectiveUID != 0 || !ok || stat.Uid == 0 {
		return 0, 0, false
	}
	return int(stat.Uid), int(stat.Gid), true
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
