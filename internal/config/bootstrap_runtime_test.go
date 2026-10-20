package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBootstrapRuntimeTokenLifecycle(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "instance", "config.toml")
	path, err := BootstrapTokenPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(filepath.Dir(configPath), "data", "secrets", "bootstrap.token"); path != want {
		t.Fatalf("bootstrap token path = %s, want %s", path, want)
	}
	if _, err := ReadBootstrapToken(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read missing token error = %v, want not exist", err)
	}
	if err := ClearBootstrapToken(configPath); err != nil {
		t.Fatalf("clear missing token: %v", err)
	}
	const first = "first-process-bootstrap-token-0123456789"
	if err := WriteBootstrapToken(configPath, first); err != nil {
		t.Fatal(err)
	}
	if token, err := ReadBootstrapToken(configPath); err != nil || token != first {
		t.Fatalf("read published token mismatch; error = %v", err)
	}
	if runtime.GOOS != "windows" {
		for _, expected := range []struct {
			path string
			mode os.FileMode
		}{{path, 0o600}, {filepath.Dir(path), 0o700}} {
			info, err := os.Stat(expected.path)
			if err != nil || info.Mode().Perm() != expected.mode {
				t.Fatalf("private mode mismatch for %s; error = %v", expected.path, err)
			}
		}
	}
	keyPath, err := ManagedJWTSecretPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	const signingKey = "original-signing-key-is-preserved"
	if err := os.WriteFile(keyPath, []byte(signingKey), 0o600); err != nil {
		t.Fatal(err)
	}
	const second = "second-process-bootstrap-token-0123456789"
	if err := WriteBootstrapToken(configPath, second); err != nil {
		t.Fatal(err)
	}
	if token, err := ReadBootstrapToken(configPath); err != nil || token != second {
		t.Fatalf("runtime token was not replaced; error = %v", err)
	}
	if err := ClearBootstrapToken(configPath); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBootstrapToken(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read cleared token error = %v, want not exist", err)
	}
	if data, err := os.ReadFile(keyPath); err != nil || string(data) != signingKey {
		t.Fatalf("runtime token lifecycle changed signing key; error = %v", err)
	}
	if err := ClearBootstrapToken(configPath); err != nil {
		t.Fatalf("clear twice: %v", err)
	}
}

func TestBootstrapRuntimeTokenRejectsInvalidFiles(t *testing.T) {
	for _, contents := range []string{
		"", "\n", "too-short", "\nbootstrap-token-0123456789abcdef",
		"bootstrap-token-0123456789abcdef\n\n", "bootstrap-token-0123456789abcdef\r\n",
		"bootstrap-token-0123456789abcdef\x00", strings.Repeat("x", maxBootstrapTokenBytes+1),
	} {
		t.Run("invalid file", func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.toml")
			if err := WriteBootstrapToken(configPath, "valid-bootstrap-token-0123456789"); err != nil {
				t.Fatal(err)
			}
			path, _ := BootstrapTokenPath(configPath)
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadBootstrapToken(configPath); err == nil {
				t.Fatal("accepted invalid bootstrap token file")
			}
		})
	}
	if _, err := BootstrapTokenPath(""); err == nil {
		t.Fatal("accepted an absent config path")
	}
	if err := WriteBootstrapToken(filepath.Join(t.TempDir(), "config.toml"), ""); err == nil {
		t.Fatal("accepted an empty runtime token")
	}
}

func TestBootstrapRuntimeTokenRejectsPublicPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission check")
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := WriteBootstrapToken(configPath, "valid-bootstrap-token-0123456789"); err != nil {
		t.Fatal(err)
	}
	path, _ := BootstrapTokenPath(configPath)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBootstrapToken(configPath); err == nil {
		t.Fatal("accepted a publicly readable token file")
	}
}

func TestBootstrapRuntimeTokenRejectsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require additional privileges on Windows")
	}
	for _, component := range []string{"config", "data", "secrets", "token"} {
		t.Run(component, func(t *testing.T) {
			root := t.TempDir()
			instance := filepath.Join(root, "instance")
			outside := filepath.Join(root, "outside")
			if err := os.MkdirAll(outside, 0o700); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(instance, "config.toml")
			if err := WriteBootstrapToken(configPath, "valid-bootstrap-token-0123456789"); err != nil {
				t.Fatal(err)
			}
			path, _ := BootstrapTokenPath(configPath)
			link := map[string]string{
				"config": instance, "data": filepath.Join(instance, "data"),
				"secrets": filepath.Dir(path), "token": path,
			}[component]
			if err := os.RemoveAll(link); err != nil {
				t.Fatal(err)
			}
			target := outside
			if component == "token" {
				target = filepath.Join(outside, "victim")
				if err := os.WriteFile(target, []byte("external-token-must-not-be-touched"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if err := WriteBootstrapToken(configPath, "replacement-bootstrap-token-0123456789"); err == nil {
				t.Fatal("write followed a symlink")
			}
			if _, err := ReadBootstrapToken(configPath); err == nil {
				t.Fatal("read followed a symlink")
			}
			if err := ClearBootstrapToken(configPath); err == nil {
				t.Fatal("clear accepted a symlink")
			}
			entries, err := os.ReadDir(outside)
			want := 0
			if component == "token" {
				want = 1
				if data, err := os.ReadFile(target); err != nil || string(data) != "external-token-must-not-be-touched" {
					t.Fatal("symlink operation changed external token")
				}
			}
			if err != nil || len(entries) != want {
				t.Fatalf("symlink operation changed external directory; error = %v", err)
			}
		})
	}
}
