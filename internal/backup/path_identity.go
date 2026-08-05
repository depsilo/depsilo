package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const restoreJournalSuffix = ".depsilo.restore.json"

// canonicalRestoreTarget resolves every existing parent symlink while keeping
// a possibly nonexistent leaf. Restore never follows a final symlink: doing so
// would make the file replaced depend on mutable namespace state.
func canonicalRestoreTarget(path, label string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%s restore target is empty", label)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s restore target: %w", label, err)
	}
	abs = filepath.Clean(abs)
	info, err := os.Lstat(abs)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s restore target %q must not be a symbolic link", label, path)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%s restore target %q is not a regular file", label, path)
		}
	case errors.Is(err, os.ErrNotExist):
		// A missing leaf is expected for first-time restores.
	case err != nil:
		return "", fmt.Errorf("inspect %s restore target %q: %w", label, path, err)
	}
	parent, err := canonicalDirectory(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("resolve parent of %s restore target %q: %w", label, path, err)
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}

// canonicalDatabasePath follows an existing database symlink so all aliases
// contend on one lease. A missing database uses the canonical parent plus leaf.
func canonicalDatabasePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	info, err := os.Lstat(abs)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return "", err
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return "", err
		}
		return filepath.Clean(resolved), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent, err := canonicalDirectory(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}

func canonicalDirectory(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(abs)
	missing := make([]string, 0, 4)
	for {
		info, statErr := os.Stat(current)
		if statErr == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("%q is not a directory", current)
			}
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", statErr
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func sameCanonicalPath(left, right string) bool {
	if pathIdentityKey(left) == pathIdentityKey(right) {
		return true
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func pathIdentityKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(path)
	}
	return path
}

func validateRestoreTargetPair(configTarget, databaseTarget string) error {
	if sameCanonicalPath(configTarget, databaseTarget) {
		return errors.New("restore config and database paths must be different")
	}
	reserved := []string{
		databaseTarget + "-wal",
		databaseTarget + "-shm",
		databaseTarget + ".depsilo.lock",
		databaseTarget + restoreJournalSuffix,
	}
	for _, path := range reserved {
		if sameCanonicalPath(configTarget, path) {
			return fmt.Errorf("restore config target collides with reserved SQLite state path %q", path)
		}
	}
	databaseBase := filepath.Base(databaseTarget)
	configBase := filepath.Base(configTarget)
	if reservedRestoreBasename(databaseBase) {
		return fmt.Errorf("restore database target %q uses a reserved restore or SQLite filename", databaseTarget)
	}
	if reservedRestoreBasename(configBase) || strings.HasPrefix(pathIdentityKey(configBase), pathIdentityKey("."+databaseBase+".restore-")) ||
		strings.HasPrefix(pathIdentityKey(configBase), pathIdentityKey(databaseBase+".pre-restore-")) {
		return fmt.Errorf("restore config target %q uses a reserved restore or SQLite filename", configTarget)
	}
	lockDirectory := filepath.Join(filepath.Dir(databaseTarget), ".depsilo-locks")
	if pathInsideDirectory(configTarget, lockDirectory) || pathInsideDirectory(databaseTarget, lockDirectory) {
		return errors.New("restore targets must not be placed inside the reserved .depsilo-locks directory")
	}
	return nil
}

func reservedRestoreBasename(base string) bool {
	key := pathIdentityKey(base)
	return strings.HasSuffix(key, "-wal") || strings.HasSuffix(key, "-shm") ||
		strings.HasSuffix(key, ".depsilo.lock") || strings.HasSuffix(key, restoreJournalSuffix) ||
		strings.Contains(key, ".restore-") || strings.Contains(key, ".pre-restore-") || key == ".depsilo-locks"
}

func pathInsideDirectory(path, directory string) bool {
	pathKey := pathIdentityKey(path)
	directoryKey := pathIdentityKey(directory)
	return pathKey == directoryKey || strings.HasPrefix(pathKey, directoryKey+string(filepath.Separator))
}

// samePath is a best-effort identity comparison for Create's source/output
// guard. Restore uses the stricter, error-returning canonical target helpers.
func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil && rightErr == nil && sameCanonicalPath(leftAbs, rightAbs) {
		return true
	}
	leftInfo, leftStatErr := os.Stat(left)
	rightInfo, rightStatErr := os.Stat(right)
	return leftStatErr == nil && rightStatErr == nil && os.SameFile(leftInfo, rightInfo)
}
