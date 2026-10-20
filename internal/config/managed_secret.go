package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ManagedJWTSecretPath anchors signing state to the instance, never to its
// disposable artifact cache or to a process's changing working directory.
func ManagedJWTSecretPath(configPath string) (string, error) {
	if configPath == "" {
		return "", errors.New("cannot resolve instance signing key without a config path")
	}
	dir, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		return "", fmt.Errorf("resolve instance directory: %w", err)
	}
	return filepath.Join(dir, "data", "secrets", "jwt.key"), nil
}

func resolveManagedJWTSecret(cfg *Config, create bool) error {
	path, err := ManagedJWTSecretPath(cfg.ConfigPath)
	if err != nil {
		return err
	}
	secret, err := readManagedJWTSecret(path)
	if errors.Is(err, os.ErrNotExist) {
		if !create {
			return errors.New("instance signing key is missing; start a new instance with depsilo serve, or restore its original data/secrets/jwt.key or auth.jwt_secret")
		}
		databasePath, pathErr := sqliteDocumentFilePath(cfg.Database.DSN)
		if pathErr != nil {
			return errors.New("automatic signing key initialization requires a file-backed SQLite database; configure auth.jwt_secret for other database paths")
		}
		if _, statErr := os.Stat(databasePath); !errors.Is(statErr, os.ErrNotExist) {
			if statErr != nil {
				return fmt.Errorf("inspect database before initializing signing key: %w", statErr)
			}
			return errors.New("instance signing key is missing for an existing database; restore the original data/secrets/jwt.key or configure its original auth.jwt_secret; a new key would invalidate sessions and signed download links")
		}
		secret, err = createManagedJWTSecret(path)
	}
	if err != nil {
		return fmt.Errorf("read instance signing key: %w", err)
	}
	cfg.Auth.JWTSecret = secret
	cfg.ManagedJWTSecretPath = path
	return nil
}

func readManagedJWTSecret(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("managed signing key must be a regular file, not a symlink or directory")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("managed signing key permissions must be 0600; restrict data/secrets/jwt.key before restarting")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", errors.New("managed signing key changed while opening it")
	}
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return "", err
	}
	if len(data) > 4096 {
		return "", errors.New("managed signing key exceeds 4096 bytes")
	}
	secret := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if !validSigningSecret(secret) {
		return "", errors.New("managed signing key is empty or invalid; restore its original value instead of regenerating it")
	}
	return secret, nil
}

func validSigningSecret(secret string) bool {
	return secret != "change-me-in-production" && len([]byte(secret)) >= secureTokenBytes &&
		secret == strings.TrimSpace(secret) && !strings.ContainsAny(secret, "\r\n")
}

func createManagedJWTSecret(path string) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("managed signing key directory must not be a symlink")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	secret, err := NewSecureToken()
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".jwt-key-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(0o600); err != nil {
		return "", err
	}
	if _, err := tmp.WriteString(secret + "\n"); err != nil {
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	// Publish a complete, synced file without replacing a concurrent winner.
	if err := os.Link(tmp.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return readManagedJWTSecret(path)
		}
		return "", err
	}
	// Windows has no directory fsync. The key bytes were synced before the
	// exclusive hard-link publication above; POSIX also syncs its directory tree.
	if runtime.GOOS != "windows" {
		for _, parent := range []string{dir, filepath.Dir(dir), filepath.Dir(filepath.Dir(dir))} {
			if err := syncDirectory(parent); err != nil {
				return "", fmt.Errorf("sync signing key directory: %w", err)
			}
		}
	}
	return secret, nil
}
