package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestDockerBootstrapEnablesHubWithoutChangingOtherSections(t *testing.T) {
	for _, document := range []string{
		"config_version = 1\n\n[auth]\njwt_secret = \"private-value\"\n\n[custom]\noperator = \"keep\"\n",
		"config_version = 1\r\n\r\n[docker]\r\ndefault_registry = \"docker.io\"\r\n\r\n[custom]\r\noperator = \"keep\"\r\n",
	} {
		t.Run(strings.ReplaceAll(document[:min(len(document), 24)], "\n", "-"), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
				t.Fatal(err)
			}
			store := NewStore(path, &Config{}, zap.NewAtomicLevel())
			state, err := store.DockerBootstrap(context.Background())
			if err != nil || state.Configured || state.Effective || state.PendingRestart {
				t.Fatalf("before = %+v, %v", state, err)
			}
			state, err = store.EnableDockerHub(context.Background())
			if err != nil || !state.Configured || state.Effective || state.DockerHub || !state.PendingRestart {
				t.Fatalf("after = %+v, %v", state, err)
			}
			written, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(written), `operator = "keep"`) || !strings.Contains(string(written), `url = "https://registry-1.docker.io"`) || strings.Count(string(written), "[[docker.registries]]") != 1 {
				t.Fatalf("wrong config: %s", written)
			}
			if strings.Contains(document, "private-value") && !strings.Contains(string(written), "private-value") {
				t.Fatal("unrelated credential was removed")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("file mode = %v, err = %v", info.Mode().Perm(), err)
			}
			second, err := store.EnableDockerHub(context.Background())
			if err != nil || !second.PendingRestart {
				t.Fatalf("idempotent enable = %+v, %v", second, err)
			}
			again, _ := os.ReadFile(path)
			if string(again) != string(written) {
				t.Fatal("second enable rewrote the config")
			}
			restartedCfg, err := decodeConfigDocument(written)
			if err != nil {
				t.Fatal(err)
			}
			restarted := NewStore(path, restartedCfg, zap.NewAtomicLevel())
			active, err := restarted.DockerBootstrap(context.Background())
			if err != nil || !active.DockerHub || !active.Effective || active.PendingRestart {
				t.Fatalf("after restart = %+v, %v", active, err)
			}
		})
	}
}

func TestDockerBootstrapMissingFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	store := NewStore(path, &Config{}, zap.NewAtomicLevel())
	if _, err := store.EnableDockerHub(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, err = %v", info.Mode().Perm(), err)
	}
}

func TestDockerBootstrapDetectsRegistryChangesAfterStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	initial := []byte("[docker]\ndefault_registry = \"docker.io\"\n[[docker.registries]]\nname = \"dockerhub\"\nurl = \"https://registry-1.docker.io\"\n")
	if err := os.WriteFile(path, initial, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := decodeConfigDocument(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(path, loaded, zap.NewAtomicLevel())
	updated := append(append([]byte{}, initial...), []byte("proxy = \"http://proxy.example:8080\"\n")...)
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := store.DockerBootstrap(context.Background())
	if err != nil || !state.Configured || !state.Effective || !state.PendingRestart {
		t.Fatalf("changed registry = %+v, %v", state, err)
	}
}

func TestDockerBootstrapRejectsCustomOrReadOnlyWithoutWrite(t *testing.T) {
	for _, document := range []string{
		"[docker]\ndefault_registry = \"private.example\"\n",
		"[docker]\n[[docker.registries]]\nname = \"private\"\nurl = \"https://private.example\"\npassword = \"keep-me\"\n",
		"docker = { default_registry = \"docker.io\" }\n",
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
		store := NewStore(path, &Config{}, zap.NewAtomicLevel())
		_, err := store.EnableDockerHub(context.Background())
		var storeErr *StoreError
		if !errors.As(err, &storeErr) || storeErr.Code != StoreInvalidSetting {
			t.Fatalf("document %q: err = %v", document, err)
		}
		written, _ := os.ReadFile(path)
		if string(written) != document {
			t.Fatal("invalid request modified the config")
		}
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[auth]\nenabled = true\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path, &Config{}, zap.NewAtomicLevel())
	_, err := store.EnableDockerHub(context.Background())
	var storeErr *StoreError
	if !errors.As(err, &storeErr) || storeErr.Code != StoreConfigReadOnly {
		t.Fatalf("read-only err = %v", err)
	}
}

func TestDockerBootstrapRejectsEnvironmentOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("DEPSILO_DOCKER_DEFAULT_REGISTRY", "other.example")
	store := NewStore(path, &Config{}, zap.NewAtomicLevel())
	state, err := store.DockerBootstrap(context.Background())
	if err != nil || !state.BlockedByOverride {
		t.Fatalf("state = %+v, %v", state, err)
	}
	_, err = store.EnableDockerHub(context.Background())
	var storeErr *StoreError
	if !errors.As(err, &storeErr) || storeErr.Code != StoreInvalidSetting {
		t.Fatalf("override err = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("override created config: %v", err)
	}
}
