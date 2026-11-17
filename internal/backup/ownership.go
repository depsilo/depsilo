package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// entryOwnership decides who owns the entries this package creates in an
// instance's directories. Root working in a directory that belongs to another
// account (an operator running an offline command with sudo, or as UID 0 in
// a container) would leave entries the service cannot open: such entries are
// given to the owner of the directory that receives them. Only tests replace
// processEntryOwnership.
type entryOwnership struct {
	// newEntryOwner reports the owner and group to give an entry this
	// process created, from the directory that receives it. It reports false
	// when the entry stays as it was created.
	newEntryOwner func(receivingDirectory os.FileInfo) (uid, gid int, change bool)
	// chown changes the owner of an open entry. Working on the open entry,
	// not on its name, means a link put at that name is never followed.
	chown func(entry *os.File, uid, gid int) error
}

var processEntryOwnership = entryOwnership{
	newEntryOwner: processNewEntryOwner,
	chown:         (*os.File).Chown,
}

// giveNewDirectory hands a directory this call created to the owner of the
// directory that receives it, when the ownership rule asks for that.
//
// A directory that could not be handed over is removed again. Left behind,
// it would be an existing entry for the next call, which never changes the
// owner of existing entries: the failure would be reported once and the
// creator would keep the directory. Rmdir removes an empty directory only,
// never a file and never what a link names.
func (ownership entryOwnership) giveNewDirectory(path, receivingDirectory string) (err error) {
	defer func() {
		if err != nil {
			_ = syscall.Rmdir(path)
		}
	}()
	uid, gid, change, err := ownership.ownerOfNewEntries(receivingDirectory)
	if err != nil || !change {
		return err
	}
	directory, err := openNewDirectory(path)
	if err != nil {
		return fmt.Errorf("open new directory %q to give it to uid %d, gid %d: %w", path, uid, gid, err)
	}
	defer directory.Close()
	// Creating a directory does not open it, so the owner of the receiving
	// directory can put another directory at this name before it is opened
	// here. What is given away must be the directory this call created.
	if err := verifyNewDirectory(directory); err != nil {
		return fmt.Errorf("new directory %q changed after it was created: %w", path, err)
	}
	return ownership.give(directory, receivingDirectory, uid, gid)
}

// giveNewFile hands a file this process created, and still has open, to the
// owner of the directory it was created in, when the ownership rule asks for
// that. A file that is renamed into place afterwards arrives with its final
// owner. The caller removes a file that could not be handed over.
func (ownership entryOwnership) giveNewFile(file *os.File) error {
	receivingDirectory := filepath.Dir(file.Name())
	uid, gid, change, err := ownership.ownerOfNewEntries(receivingDirectory)
	if err != nil || !change {
		return err
	}
	return ownership.give(file, receivingDirectory, uid, gid)
}

// makeDirectories is os.MkdirAll(path, 0o700) with the ownership rule: every
// directory it creates is handed to the owner of the directory that receives
// it, and directories that exist keep their owner.
func (ownership entryOwnership) makeDirectories(path string) error {
	var missing []string
	for current := filepath.Clean(path); ; {
		if _, err := os.Lstat(current); !errors.Is(err, os.ErrNotExist) {
			break
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	for index := len(missing) - 1; index >= 0; index-- {
		err := os.Mkdir(missing[index], 0o700)
		if errors.Is(err, os.ErrExist) {
			// Another process created it in the meantime: an existing entry.
			continue
		}
		if err != nil {
			return err
		}
		if err := ownership.giveNewDirectory(missing[index], filepath.Dir(missing[index])); err != nil {
			return handoverError{err}
		}
	}
	// Whatever is at path now gets os.MkdirAll's verdict.
	return os.MkdirAll(path, 0o700)
}

// handoverError marks a directory makeDirectories created and could not hand
// over, as distinct from one it could not create.
type handoverError struct{ error }

func (err handoverError) Unwrap() error { return err.error }

// keepingEveryEntry returns the rule under which every entry stays with the
// user who created it.
func (ownership entryOwnership) keepingEveryEntry() entryOwnership {
	ownership.newEntryOwner = func(os.FileInfo) (uid, gid int, change bool) { return 0, 0, false }
	return ownership
}

// insideStateDirectory reports whether directory is the directory of one of
// the instance's state files, or lies beneath one. Links are resolved first,
// so a link inside the state that names a directory elsewhere is elsewhere.
func insideStateDirectory(directory string, stateFiles ...string) bool {
	directory, err := canonicalDirectory(directory)
	if err != nil {
		return false
	}
	for _, file := range stateFiles {
		state, err := canonicalDirectory(filepath.Dir(file))
		if err == nil && pathInsideDirectory(directory, state) {
			return true
		}
	}
	return false
}

func (ownership entryOwnership) ownerOfNewEntries(receivingDirectory string) (uid, gid int, change bool, err error) {
	info, err := os.Stat(receivingDirectory)
	if err != nil {
		return 0, 0, false, fmt.Errorf("inspect owner of directory %q: %w", receivingDirectory, err)
	}
	uid, gid, change = ownership.newEntryOwner(info)
	return uid, gid, change, nil
}

func (ownership entryOwnership) give(entry *os.File, receivingDirectory string, uid, gid int) error {
	if err := ownership.chown(entry, uid, gid); err != nil {
		return fmt.Errorf("give new entry %q to uid %d, gid %d, the owner of directory %q: %w", entry.Name(), uid, gid, receivingDirectory, err)
	}
	return nil
}
