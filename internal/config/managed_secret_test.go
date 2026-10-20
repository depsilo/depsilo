package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func managedSecretFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	database := filepath.Join(root, "data", "depsilo.db")
	document := fmt.Sprintf("config_version = 1\n[server]\nhost = \"0.0.0.0\"\n[database]\ndsn = %q\n", database)
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", "")
	t.Setenv("DEPSILO_DATABASE_DSN", "")
	t.Setenv("DEPSILO_SERVER_HOST", "")
	return path, database
}

func TestManagedSigningKeyStartupAndReadOnlyCommands(t *testing.T) {
	configPath, database := managedSecretFixture(t)
	path, err := ManagedJWTSecretPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, read := range []func() (*Config, error){Load, LoadForRestore} {
		_, _ = read()
		if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("read-only config parsing created signing state")
		}
	}
	cfg, err := LoadForServer()
	if err != nil {
		t.Fatal(err)
	}
	if !validSigningSecret(cfg.Auth.JWTSecret) || cfg.ManagedJWTSecretPath != path {
		t.Fatal("startup did not select the persistent instance key")
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		dir, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if before.Mode().Perm() != 0o600 || dir.Mode().Perm() != 0o700 {
			t.Fatal("signing state is not private")
		}
	}
	// Once other state exists, both restart and read-only commands reuse the
	// original key. A changed working directory must not change its location.
	if err := os.WriteFile(database, []byte("existing database"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	for _, read := range []func() (*Config, error){Load, LoadForServer} {
		reloaded, err := read()
		if err != nil || reloaded.Auth.JWTSecret != cfg.Auth.JWTSecret {
			t.Fatal("restarting or inspecting an instance changed its signing key")
		}
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("loading an existing key rewrote the file")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadForServer(); err == nil || !strings.Contains(err.Error(), "existing database") {
		t.Fatal("startup silently replaced a missing signing key")
	}
}

func TestManagedSigningKeyConcurrentFirstStartup(t *testing.T) {
	configPath, _ := managedSecretFixture(t)
	const count = 12
	keys := make(chan string, count)
	errs := make(chan error, count)
	var group sync.WaitGroup
	for range count {
		group.Go(func() {
			cfg, err := LoadForServer()
			if err != nil {
				errs <- err
				return
			}
			keys <- cfg.Auth.JWTSecret
		})
	}
	group.Wait()
	close(keys)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var first string
	for key := range keys {
		if first == "" {
			first = key
		}
		if key != first {
			t.Fatal("concurrent startups selected different signing keys")
		}
	}
	path, _ := ManagedJWTSecretPath(configPath)
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "jwt.key" {
		t.Fatal("key publication left temporary files behind")
	}
}

func TestManagedSigningKeyNeverReplacesInvalidState(t *testing.T) {
	for _, kind := range []string{"empty", "short", "space", "newline", "oversized", "permissions", "symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			configPath, _ := managedSecretFixture(t)
			path, _ := ManagedJWTSecretPath(configPath)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			value := strings.Repeat("x", 43)
			mode := os.FileMode(0o600)
			switch kind {
			case "empty":
				value = ""
			case "short":
				value = "short"
			case "space":
				value = " " + value
			case "newline":
				value += "\n\n"
			case "oversized":
				value = strings.Repeat("x", 4097)
			case "permissions":
				if runtime.GOOS == "windows" {
					t.Skip("POSIX permissions")
				}
				mode = 0o644
			}
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(t.TempDir(), "private-key")
				if err := os.WriteFile(target, []byte(value), mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			default:
				if err := os.WriteFile(path, []byte(value), mode); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, read := range []func() (*Config, error){Load, LoadForServer} {
				if _, err := read(); err == nil {
					t.Fatal("invalid managed key was accepted")
				}
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("invalid key was overwritten")
			}
		})
	}
}

func TestManagedSigningKeyDoesNotOverrideExplicitSources(t *testing.T) {
	configPath, _ := managedSecretFixture(t)
	inline := "inline-test-key-with-at-least-32-bytes"
	environment := "environment-test-key-with-at-least-32-bytes"
	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(f, "[auth]\njwt_secret = %q\n", inline)
	if closeErr := f.Close(); err != nil || closeErr != nil {
		t.Fatal("write config")
	}
	for _, override := range []string{"", environment} {
		t.Setenv("DEPSILO_AUTH_JWT_SECRET", override)
		cfg, err := LoadForServer()
		if err != nil {
			t.Fatal(err)
		}
		want := inline
		if override != "" {
			want = override
		}
		if cfg.Auth.JWTSecret != want || cfg.ManagedJWTSecretPath != "" {
			t.Fatal("explicit signing authority changed")
		}
		path, _ := ManagedJWTSecretPath(configPath)
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("explicit configuration generated a second key")
		}
	}
}

func TestManagedSigningKeyRecognizesExistingSQLitePaths(t *testing.T) {
	for _, kind := range []string{"plain", "query", "uri", "escaped opaque URI"} {
		t.Run(kind, func(t *testing.T) {
			_, _ = managedSecretFixture(t)
			path := filepath.Join(t.TempDir(), "database with spaces.db ")
			if err := os.WriteFile(path, []byte("existing database"), 0o600); err != nil {
				t.Fatal(err)
			}
			dsn := path
			switch kind {
			case "query":
				dsn += "?cache=shared"
			case "uri":
				dsn = (&url.URL{Scheme: "file", Path: path, RawQuery: "cache=shared"}).String()
			case "escaped opaque URI":
				t.Chdir(filepath.Dir(path))
				dsn = "file:" + url.PathEscape(filepath.Base(path)) + "?cache=shared"
			}
			t.Setenv("DEPSILO_DATABASE_DSN", dsn)
			if _, err := LoadForServer(); err == nil || !strings.Contains(err.Error(), "existing database") {
				t.Fatal("an existing SQLite path was mistaken for a new instance")
			}
		})
	}
}
