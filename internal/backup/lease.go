package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// ErrDatabaseInUse means a Depsilo server still owns the target database.
// Restore must not replace SQLite files until that owner has shut down.
var ErrDatabaseInUse = errors.New("database is in use by a running Depsilo server")

type databaseLease struct {
	file     *os.File
	lockPath string
	once     sync.Once
	err      error
}

// HoldDatabase acquires Depsilo's process-level lease for a SQLite database.
// A server holds the lease for its full lifetime; Restore uses the same
// interface to prove that replacing the database and its WAL sidecars is safe.
// On Unix, the database directory must not be group/world-writable because an
// unlinkable advisory-lock filename cannot be made trustworthy in a mutable
// shared namespace. Container volumes should be owned by the service account
// and initialized with mode 0750 or 0700.
//
// The lease directory and file are created on first use. When root creates
// them in a database directory that belongs to another account (an operator
// running an offline command with sudo), they are given to that account, so
// the service can still acquire its lease afterwards.
func HoldDatabase(databaseDSN string) (io.Closer, error) {
	return holdDatabase(databaseDSN, processLeaseOwnership)
}

// leaseOwnership decides who owns the lease entries HoldDatabase creates.
// Only tests replace processLeaseOwnership.
type leaseOwnership struct {
	// newEntryOwner reports the owner and group to give an entry this call
	// created, from the directory that holds the database. It reports false
	// when the entry stays as it was created.
	newEntryOwner func(databaseDirectory os.FileInfo) (uid, gid int, change bool)
	// chown changes the owner of an open entry. Working on the open entry,
	// not on its name, means a link put at that name is never followed.
	chown func(entry *os.File, uid, gid int) error
}

var processLeaseOwnership = leaseOwnership{
	newEntryOwner: processNewLeaseEntryOwner,
	chown:         (*os.File).Chown,
}

// giveNewDirectory hands a lease directory this call created to the owner
// of the database directory, when the ownership rule asks for that.
//
// A directory that could not be handed over is removed again. Left behind,
// it would be an existing entry for the next call, which never changes the
// owner of existing entries: the failure would be reported once and the
// creator would keep the directory. Rmdir removes an empty directory only,
// never a file and never what a link names.
func (ownership leaseOwnership) giveNewDirectory(path, databaseDirectory string) (err error) {
	defer func() {
		if err != nil {
			_ = syscall.Rmdir(path)
		}
	}()
	uid, gid, change, err := ownership.ownerOfNewEntries(databaseDirectory)
	if err != nil || !change {
		return err
	}
	directory, err := openLockDirectory(path)
	if err != nil {
		return fmt.Errorf("open new database lease directory %q to give it to uid %d, gid %d: %w", path, uid, gid, err)
	}
	defer directory.Close()
	// Creating a directory does not open it, so the owner of the database
	// directory can put another directory at this name before it is opened
	// here. What is given away must be the directory this call created.
	if err := verifyNewLockDirectory(directory); err != nil {
		return fmt.Errorf("database lease directory %q changed while acquiring it: %w", path, err)
	}
	return ownership.give(directory, uid, gid)
}

// giveNewFile does the same for a lease file this call created. The file was
// created exclusively and is open, so it is the one this call created; it is
// removed again, for the same reason, when it cannot be handed over.
func (ownership leaseOwnership) giveNewFile(file *os.File, path, databaseDirectory string) error {
	uid, gid, change, err := ownership.ownerOfNewEntries(databaseDirectory)
	if err == nil && change {
		err = ownership.give(file, uid, gid)
	}
	if err != nil && verifyLockedFile(file, path) == nil {
		_ = os.Remove(path)
	}
	return err
}

func (ownership leaseOwnership) ownerOfNewEntries(databaseDirectory string) (uid, gid int, change bool, err error) {
	info, err := os.Stat(databaseDirectory)
	if err != nil {
		return 0, 0, false, fmt.Errorf("inspect owner of database directory %q: %w", databaseDirectory, err)
	}
	uid, gid, change = ownership.newEntryOwner(info)
	return uid, gid, change, nil
}

func (ownership leaseOwnership) give(entry *os.File, uid, gid int) error {
	if err := ownership.chown(entry, uid, gid); err != nil {
		return fmt.Errorf("give new database lease entry %q to uid %d, gid %d, the owner of the database directory: %w", entry.Name(), uid, gid, err)
	}
	return nil
}

func holdDatabase(databaseDSN string, ownership leaseOwnership) (io.Closer, error) {
	if isMemoryDatabase(databaseDSN) {
		return nopCloser{}, nil
	}
	databasePath, err := sqliteFilePath(databaseDSN)
	if err != nil {
		return nil, err
	}
	databasePath, err = canonicalDatabasePath(databasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve database lease identity: %w", err)
	}
	databaseDirectory := filepath.Dir(databasePath)
	if err := os.MkdirAll(databaseDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create database lease directory: %w", err)
	}
	if err := validateLeaseParent(databaseDirectory); err != nil {
		return nil, err
	}
	lockDirectory := filepath.Join(databaseDirectory, ".depsilo-locks")
	directoryCreated, err := ensurePrivateLockDirectory(lockDirectory)
	if err != nil {
		return nil, err
	}
	if directoryCreated {
		if err := ownership.giveNewDirectory(lockDirectory, databaseDirectory); err != nil {
			return nil, fmt.Errorf("acquire database lease: %w", err)
		}
	}
	digest := sha256.Sum256([]byte(pathIdentityKey(databasePath)))
	lockPath := filepath.Join(lockDirectory, hex.EncodeToString(digest[:16])+".lock")
	f, fileCreated, err := openLockFile(lockPath)
	if err != nil {
		return nil, fmt.Errorf("open database lease: %w", err)
	}
	if fileCreated {
		if err := ownership.giveNewFile(f, lockPath, databaseDirectory); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("acquire database lease: %w", err)
		}
	}
	locked, err := tryLockFile(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("acquire database lease: %w", err)
	}
	if !locked {
		_ = f.Close()
		return nil, ErrDatabaseInUse
	}
	if err := verifyLockedFile(f, lockPath); err != nil {
		_ = unlockFile(f)
		_ = f.Close()
		return nil, fmt.Errorf("database lease file changed while acquiring it: %w", err)
	}
	lease := &databaseLease{file: f, lockPath: lockPath}
	if err := recoverPendingRestoreWith(databasePath, fileOperations{publish: publishFile, validateLease: lease.validate}); err != nil {
		_ = lease.Close()
		return nil, fmt.Errorf("recover interrupted database restore: %w", err)
	}
	return lease, nil
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func (lease *databaseLease) Close() error {
	lease.once.Do(func() {
		lease.err = errors.Join(unlockFile(lease.file), lease.file.Close())
	})
	return lease.err
}

func (lease *databaseLease) validate() error {
	return verifyLockedFile(lease.file, lease.lockPath)
}

// DatabaseFilePath returns the absolute path of the file a SQLite DSN names,
// without the connection options that follow "?". It is the rule HoldDatabase,
// Create and Restore use to find the database file, for callers that must
// inspect that file before they open the database with the full DSN.
func DatabaseFilePath(databaseDSN string) (string, error) {
	return sqliteFilePath(databaseDSN)
}

func sqliteFilePath(dsn string) (string, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" || isMemoryDatabase(dsn) {
		return "", errors.New("database lease requires a file-backed SQLite DSN")
	}
	path := dsn
	if strings.HasPrefix(dsn, "file:") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("parse SQLite DSN: %w", err)
		}
		if parsed.Host != "" && parsed.Host != "localhost" {
			return "", errors.New("SQLite DSN must not use a remote file URI host")
		}
		switch {
		case parsed.Path != "":
			path = parsed.Path
		case parsed.Opaque != "":
			path = parsed.Opaque
		default:
			return "", errors.New("SQLite DSN has no database path")
		}
	} else if index := strings.IndexByte(path, '?'); index >= 0 {
		path = path[:index]
	}
	absolute, err := filepath.Abs(filepath.FromSlash(path))
	if err != nil {
		return "", fmt.Errorf("resolve SQLite database path: %w", err)
	}
	return filepath.Clean(absolute), nil
}

func isMemoryDatabase(dsn string) bool {
	dsn = strings.TrimSpace(dsn)
	if dsn == ":memory:" || strings.HasPrefix(dsn, "file::memory:") {
		return true
	}
	if !strings.HasPrefix(dsn, "file:") {
		return false
	}
	parsed, err := url.Parse(dsn)
	return err == nil && strings.EqualFold(parsed.Query().Get("mode"), "memory")
}

// ensurePrivateLockDirectory reports whether this call created the directory.
func ensurePrivateLockDirectory(path string) (created bool, err error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		mkdirErr := os.Mkdir(path, 0o700)
		if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
			return false, fmt.Errorf("create private database lease directory: %w", mkdirErr)
		}
		created = mkdirErr == nil
		info, err = os.Lstat(path)
	}
	if err != nil {
		return false, fmt.Errorf("inspect private database lease directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("database lease directory %q must be a real directory", path)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return false, fmt.Errorf("secure database lease directory: %w", err)
	}
	return created, nil
}
