package server

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/compilecache"
	"depsilo/internal/config"
	"depsilo/internal/db"
)

func TestOpenCompileCacheRuntimeDisabledSuppliesDisabledHandlers(t *testing.T) {
	database := openCompileCacheRuntimeTestDB(t)
	runtime, err := openCompileCacheRuntime(
		context.Background(),
		config.StorageConfig{Type: "local", Path: filepath.Join(t.TempDir(), "packages")},
		config.CompileCacheConfig{Enabled: false},
		database,
	)
	if err != nil {
		t.Fatalf("open disabled compiler cache: %v", err)
	}
	dependencies := runtime.handlerDependencies()
	if dependencies.Enabled || dependencies.Service != nil || dependencies.Authorizer == nil {
		t.Fatalf("disabled handler dependencies = %+v", dependencies)
	}
	if err := runtime.Close(nil); err != nil {
		t.Fatalf("close disabled compiler cache: %v", err)
	}
}

func TestCompileCacheRuntimeCloseFlushesTouches(t *testing.T) {
	database := openCompileCacheRuntimeTestDB(t)
	root := t.TempDir()
	runtime, err := openCompileCacheRuntime(
		context.Background(),
		config.StorageConfig{Type: "local", Path: filepath.Join(root, "packages")},
		compileCacheRuntimeTestConfig(filepath.Join(root, "compiler")),
		database,
	)
	if err != nil {
		t.Fatalf("open compiler cache: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	dependencies := runtime.handlerDependencies()
	if !dependencies.Enabled || dependencies.Service == nil || dependencies.Authorizer == nil {
		t.Fatalf("enabled handler dependencies = %+v", dependencies)
	}
	if dependencies.PublicURL != "http://127.0.0.1:23333" {
		t.Fatalf("PublicURL = %q", dependencies.PublicURL)
	}
	select {
	case <-runtime.maintenanceDone:
		t.Fatal("maintenance stopped before runtime close")
	default:
	}

	id, err := compilecache.ParseCCacheArtifact("team", strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	payload := "compiler output"
	if _, err := dependencies.Service.Put(
		context.Background(), id, strings.NewReader(payload), int64(len(payload)),
	); err != nil {
		t.Fatalf("put compiler artifact: %v", err)
	}
	entry, err := dependencies.Service.Open(context.Background(), id)
	if err != nil {
		t.Fatalf("open compiler artifact: %v", err)
	}
	if _, err := io.Copy(io.Discard, entry.Body); err != nil {
		t.Fatalf("read compiler artifact: %v", err)
	}
	if err := entry.Body.Close(); err != nil {
		t.Fatalf("close compiler artifact: %v", err)
	}

	closeContext, cancelClose := context.WithTimeout(context.Background(), time.Second)
	defer cancelClose()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("close compiler cache: %v", err)
	}
	select {
	case <-runtime.maintenanceDone:
	default:
		t.Fatal("Close returned before maintenance stopped")
	}

	var stored db.CompileCacheEntry
	if err := database.Where("namespace = ? AND key = ?", "team", strings.Repeat("a", 40)).First(&stored).Error; err != nil {
		t.Fatalf("read compiler-cache metadata: %v", err)
	}
	if stored.HitCount != 1 {
		t.Fatalf("HitCount after runtime close = %d, want 1", stored.HitCount)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Close(canceled); err != nil {
		t.Fatalf("repeat close after completion: %v", err)
	}
}

func TestOpenCompileCacheRuntimeFailureStartsNoRuntime(t *testing.T) {
	database := openCompileCacheRuntimeTestDB(t)
	root := t.TempDir()
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	runtime, err := openCompileCacheRuntime(
		parent,
		config.StorageConfig{Type: "local", Path: filepath.Join(root, "packages")},
		compileCacheRuntimeTestConfig(filepath.Join(root, "compiler")),
		database,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("open canceled compiler cache error = %v, want context cancellation", err)
	}
	if runtime != nil {
		t.Fatalf("failed open returned runtime %+v", runtime)
	}
}

func TestValidateCompileCacheStorageSeparation(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name     string
		packages config.StorageConfig
		compiler config.StorageConfig
		wantErr  bool
	}{
		{
			name:     "nested local roots",
			packages: config.StorageConfig{Type: "local", Path: filepath.Join(root, "cache")},
			compiler: config.StorageConfig{Type: "local", Path: filepath.Join(root, "cache", "compiler")},
			wantErr:  true,
		},
		{
			name:     "same S3 endpoint and bucket",
			packages: config.StorageConfig{Type: "s3", Endpoint: "HTTPS://s3.example.test/", Bucket: "packages"},
			compiler: config.StorageConfig{Type: "s3", Endpoint: "https://s3.example.test", Bucket: "packages"},
			wantErr:  true,
		},
		{
			name:     "separate S3 bucket",
			packages: config.StorageConfig{Type: "s3", Endpoint: "https://s3.example.test", Bucket: "packages"},
			compiler: config.StorageConfig{Type: "s3", Endpoint: "https://s3.example.test", Bucket: "compiler"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateCompileCacheStorageSeparation(test.packages, test.compiler)
			if (err != nil) != test.wantErr {
				t.Fatalf("validate storage separation error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestLocalStoragePathsOverlapResolvesMissingLeafUnderSymlink(t *testing.T) {
	realParent := t.TempDir()
	aliasParent := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(realParent, aliasParent); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("create symlink without elevated token: %v", err)
		}
		t.Fatal(err)
	}

	overlaps, err := localStoragePathsOverlap(
		filepath.Join(realParent, "cache"),
		filepath.Join(aliasParent, "cache", "compiler"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !overlaps {
		t.Fatal("missing child below a symlink alias was not recognized as overlapping")
	}
}

func compileCacheRuntimeTestConfig(storagePath string) config.CompileCacheConfig {
	return config.CompileCacheConfig{
		Enabled:                 true,
		PublicURL:               "http://127.0.0.1:23333",
		AllowInsecureHTTP:       true,
		MaxSizeGB:               1,
		MaxEntries:              100,
		MaxEntrySizeMB:          1,
		NamespaceMaxSizeGB:      1,
		NamespaceMaxEntries:     100,
		MaxConcurrentUploads:    1,
		MaxQueuedUploads:        1,
		MaxInflightUploadSizeMB: 2,
		UploadTimeout:           time.Minute,
		MaxConcurrentDownloads:  1,
		DownloadTimeout:         time.Minute,
		LRUThreshold:            90,
		Storage: config.StorageConfig{
			Type: "local",
			Path: storagePath,
		},
	}
}

func openCompileCacheRuntimeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "depsilo.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(
		&db.CompileCacheEntry{},
		&db.CompileCacheCredential{},
		&db.CompileCacheDeletion{},
	); err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	return database
}
