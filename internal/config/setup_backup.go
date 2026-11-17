package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// setupBackupTimestamp is the UTC layout embedded in a pre-setup backup name,
// for example config.toml.pre-setup-20261116T101500Z.bak.
const setupBackupTimestamp = "20060102T150405Z"

// SetupReplacesConfig reports whether completing a pending setup will replace
// a configuration file that already exists. That is the reopened-setup case: a
// document was loaded at startup, but no local administrator can sign in.
func SetupReplacesConfig(cfg *Config) bool {
	if cfg == nil || !cfg.IsDefault || cfg.ConfigPath == "" {
		return false
	}
	_, err := os.Stat(cfg.ConfigPath)
	return err == nil
}

// BackupConfigBeforeSetup copies an existing configuration file beside itself
// before setup replaces it with a generated document. It returns the path of
// the copy, or an empty path when there is no file to keep (a first run).
//
// The copy is private (0600) and synced, together with its directory where the
// platform can sync one, before this function returns, so the caller may
// publish the new configuration afterwards. An existing file is never overwritten: a name collision is an
// error, and the caller must leave the configuration untouched.
func BackupConfigBeforeSetup(path string, now time.Time) (backupPath string, err error) {
	source, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("open existing config: %w", err)
	}
	defer source.Close()

	backupPath = path + ".pre-setup-" + now.UTC().Format(setupBackupTimestamp) + ".bak"
	backup, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create config backup: %w", err)
	}
	defer func() {
		backup.Close()
		// O_EXCL guarantees this call created the file, so removing an
		// incomplete copy cannot delete an earlier backup.
		if err != nil {
			os.Remove(backupPath)
		}
	}()
	if err = backup.Chmod(0o600); err != nil {
		return "", fmt.Errorf("protect config backup: %w", err)
	}
	if _, err = io.Copy(backup, source); err != nil {
		return "", fmt.Errorf("copy existing config: %w", err)
	}
	if err = backup.Sync(); err != nil {
		return "", fmt.Errorf("sync config backup: %w", err)
	}
	if err = backup.Close(); err != nil {
		return "", fmt.Errorf("close config backup: %w", err)
	}
	// Windows has no directory fsync; the copy's bytes were synced above. A
	// failure here would otherwise delete the copy and block every retry.
	if err = syncBootstrapDirectory(filepath.Dir(backupPath)); err != nil {
		return "", fmt.Errorf("sync config backup directory: %w", err)
	}
	return backupPath, nil
}
