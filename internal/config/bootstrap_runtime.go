package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

const maxBootstrapTokenBytes = 4096

// BootstrapTokenPath locates the current process's first-run credential beside
// the instance state. It is independent of the disposable artifact cache.
func BootstrapTokenPath(configPath string) (string, error) {
	if configPath == "" {
		return "", errors.New("cannot resolve bootstrap token without a config path")
	}
	dir, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		return "", fmt.Errorf("resolve bootstrap token directory: %w", err)
	}
	return filepath.Join(dir, "data", "secrets", "bootstrap.token"), nil
}

// WriteBootstrapToken publishes an already selected runtime credential. It
// never generates a token or changes the signing key, and replaces the prior
// process's token only after a complete private temporary file is synced.
func WriteBootstrapToken(configPath, token string) error {
	if !validRuntimeBootstrapToken(token) {
		return errors.New("bootstrap token is invalid")
	}
	path, err := BootstrapTokenPath(configPath)
	if err != nil {
		return err
	}
	if err := bootstrapTokenDirectories(path, true); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("bootstrap token must be a regular file, not a symlink or directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	writer := osAtomicFileWriter{syncDir: syncBootstrapDirectory}
	outcome, err := writer.Write(path, []byte(token+"\n"), 0o600)
	if err != nil {
		return fmt.Errorf("write bootstrap token: %w", err)
	}
	if outcome.durabilityErr != nil {
		return fmt.Errorf("sync bootstrap token directory: %w", outcome.durabilityErr)
	}
	return nil
}

// ReadBootstrapToken reads the credential published by the running server.
// Missing state remains os.ErrNotExist so callers can explain that setup may
// already be complete without loading configuration or creating new state.
func ReadBootstrapToken(configPath string) (string, error) {
	path, err := BootstrapTokenPath(configPath)
	if err != nil {
		return "", err
	}
	if err := bootstrapTokenDirectories(path, false); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("bootstrap token must be a regular file, not a symlink or directory")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("bootstrap token permissions must be 0600")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", errors.New("bootstrap token changed while opening it")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBootstrapTokenBytes+2))
	if err != nil {
		return "", err
	}
	token := strings.TrimSuffix(string(data), "\n")
	if !validRuntimeBootstrapToken(token) {
		return "", errors.New("bootstrap token file is empty or invalid")
	}
	return token, nil
}

// ClearBootstrapToken revokes the local retrieval copy after durable setup.
// Its absence is expected for configured installations and is not an error.
func ClearBootstrapToken(configPath string) error {
	path, err := BootstrapTokenPath(configPath)
	if err != nil {
		return err
	}
	if err := bootstrapTokenDirectories(path, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("bootstrap token must be a regular file, not a symlink or directory")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncBootstrapDirectory(filepath.Dir(path))
}

func validRuntimeBootstrapToken(token string) bool {
	return len(token) >= 24 && len(token) <= maxBootstrapTokenBytes &&
		token == strings.TrimSpace(token) && !strings.ContainsFunc(token, unicode.IsControl)
}

func bootstrapTokenDirectories(path string, create bool) error {
	secretsDir := filepath.Dir(path)
	dataDir := filepath.Dir(secretsDir)
	configDir := filepath.Dir(dataDir)
	for _, dir := range []string{configDir, dataDir, secretsDir} {
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) && create {
			if dir == configDir {
				err = os.MkdirAll(dir, 0o700)
			} else {
				err = os.Mkdir(dir, 0o700)
			}
			if err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("create bootstrap token directory: %w", err)
			}
			info, err = os.Lstat(dir)
		}
		if err != nil {
			return fmt.Errorf("inspect bootstrap token directory: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("bootstrap token directories must be directories, not symlinks")
		}
	}
	if create {
		if err := os.Chmod(secretsDir, 0o700); err != nil {
			return fmt.Errorf("protect bootstrap token directory: %w", err)
		}
	}
	return nil
}

func syncBootstrapDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return syncDirectory(path)
}
