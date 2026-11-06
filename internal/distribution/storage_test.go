package distribution_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/distribution"
)

func TestDistributionStorageHasPrivateIndependentLocalAccounting(t *testing.T) {
	base := t.TempDir()
	packageConfig := config.StorageConfig{Type: "local", Path: filepath.Join(base, "packages")}
	cfg := config.DistributionConfig{Enabled: true, MaxSizeBytes: 4096, MaxObjectBytes: 1024,
		Storage: config.StorageConfig{Type: "local", Path: filepath.Join(base, "retained")}}
	packages, err := cache.NewLocalStorage(packageConfig.Path)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := distribution.NewStorage(cfg, packageConfig, config.StorageConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, item := range []struct {
		storage cache.Storage
		key     string
		body    string
	}{{packages, "cached.whl", "cache"}, {retained, "sha256/object", "retained-content"}} {
		if err := item.storage.Put(ctx, item.key, strings.NewReader(item.body), int64(len(item.body)), "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
	}
	if total, err := packages.TotalSize(ctx); err != nil || total != 5 {
		t.Fatalf("package cache capacity includes retained content: total %d, error %v", total, err)
	}
	if total, err := retained.TotalSize(ctx); err != nil || total != 16 {
		t.Fatalf("retained capacity includes package cache: total %d, error %v", total, err)
	}
	if err := packages.Delete(ctx, "cached.whl"); err != nil {
		t.Fatal(err)
	}
	reopened, err := distribution.NewStorage(cfg, packageConfig, config.StorageConfig{})
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := reopened.Get(ctx, "sha256/object")
	if err != nil {
		t.Fatalf("retained object missing after ordinary eviction/reopen: %v", err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "retained-content" {
		t.Fatalf("reopened content = %q, error %v", body, err)
	}
	if runtime.GOOS != "windows" {
		for path, mode := range map[string]os.FileMode{
			cfg.Storage.Path: 0o700, filepath.Join(cfg.Storage.Path, "sha256"): 0o700,
			filepath.Join(cfg.Storage.Path, "sha256", "object"): 0o600,
		} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != mode {
				t.Fatalf("mode of %q = %o, want %o", path, info.Mode().Perm(), mode)
			}
		}
	}
}

func TestDistributionStorageRejectsUnsafeRootsBeforeMutation(t *testing.T) {
	base := t.TempDir()
	packages := config.StorageConfig{Type: "local", Path: filepath.Join(base, "packages")}
	if err := os.Mkdir(packages.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(packages.Path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DistributionConfig{Enabled: true, MaxSizeBytes: 1024, MaxObjectBytes: 1024, Storage: packages}
	if _, err := distribution.NewStorage(cfg, packages, config.StorageConfig{}); err == nil {
		t.Fatal("constructed retained storage inside ordinary cache")
	}
	after, err := os.Stat(packages.Path)
	if err != nil || before.Mode() != after.Mode() {
		t.Fatalf("failed constructor mutated existing cache permissions: %v", err)
	}
	cfg.Enabled = false
	cfg.Storage.Path = filepath.Join(base, "disabled")
	if _, err := distribution.NewStorage(cfg, packages, config.StorageConfig{}); err == nil {
		t.Fatal("constructed disabled storage")
	}
	if _, err := os.Stat(cfg.Storage.Path); !os.IsNotExist(err) {
		t.Fatalf("disabled constructor created storage: %v", err)
	}
}
