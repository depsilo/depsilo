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
func HoldDatabase(databaseDSN string) (io.Closer, error) {
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
	if err := ensurePrivateLockDirectory(lockDirectory); err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(pathIdentityKey(databasePath)))
	lockPath := filepath.Join(lockDirectory, hex.EncodeToString(digest[:16])+".lock")
	f, err := openLockFile(lockPath)
	if err != nil {
		return nil, fmt.Errorf("open database lease: %w", err)
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

func ensurePrivateLockDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create private database lease directory: %w", err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return fmt.Errorf("inspect private database lease directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("database lease directory %q must be a real directory", path)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure database lease directory: %w", err)
	}
	return nil
}
