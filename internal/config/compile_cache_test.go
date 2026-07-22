package config

import (
	"strings"
	"testing"
	"time"
)

func validCompileCacheConfig() CompileCacheConfig {
	return CompileCacheConfig{
		Enabled: true, PublicURL: "https://depsilo.example.test",
		MaxSizeGB: 20, MaxEntries: 500000, MaxEntrySizeMB: 512,
		NamespaceMaxSizeGB: 20, NamespaceMaxEntries: 250000,
		MaxConcurrentUploads: 8, MaxQueuedUploads: 32, MaxInflightUploadSizeMB: 1024,
		UploadTimeout: 15 * time.Minute, MaxConcurrentDownloads: 64, DownloadTimeout: 15 * time.Minute,
		LRUThreshold: 90,
		Storage:      StorageConfig{Type: "local", Path: "./data/compile-cache"},
	}
}

func TestValidateCompileCacheConfigSecurePublicURL(t *testing.T) {
	for name, mutate := range map[string]func(*CompileCacheConfig){
		"missing public URL":           func(cfg *CompileCacheConfig) { cfg.PublicURL = "" },
		"remote plaintext":             func(cfg *CompileCacheConfig) { cfg.PublicURL = "http://cache.example.test" },
		"URL credentials":              func(cfg *CompileCacheConfig) { cfg.PublicURL = "https://user:pass@cache.example.test" },
		"URL path":                     func(cfg *CompileCacheConfig) { cfg.PublicURL = "https://cache.example.test/base" },
		"insufficient staging":         func(cfg *CompileCacheConfig) { cfg.MaxInflightUploadSizeMB = 128 },
		"invalid upload timeout":       func(cfg *CompileCacheConfig) { cfg.UploadTimeout = 0 },
		"excessive upload timeout":     func(cfg *CompileCacheConfig) { cfg.UploadTimeout = 25 * time.Hour },
		"invalid upload queue":         func(cfg *CompileCacheConfig) { cfg.MaxQueuedUploads = -1 },
		"invalid download concurrency": func(cfg *CompileCacheConfig) { cfg.MaxConcurrentDownloads = 0 },
		"invalid download timeout":     func(cfg *CompileCacheConfig) { cfg.DownloadTimeout = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := validCompileCacheConfig()
			mutate(&cfg)
			if err := validateCompileCacheConfig(cfg); err == nil {
				t.Fatal("validation unexpectedly succeeded")
			}
		})
	}

	for name, mutate := range map[string]func(*CompileCacheConfig){
		"HTTPS":         func(*CompileCacheConfig) {},
		"loopback HTTP": func(cfg *CompileCacheConfig) { cfg.PublicURL = "http://127.0.0.1:23333" },
		"explicit trusted network HTTP": func(cfg *CompileCacheConfig) {
			cfg.PublicURL = "http://cache.internal:23333"
			cfg.AllowInsecureHTTP = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := validCompileCacheConfig()
			mutate(&cfg)
			if err := validateCompileCacheConfig(cfg); err != nil {
				t.Fatalf("validation failed: %v", err)
			}
		})
	}
}

func TestDecodeNormalizesCompileCachePublicURL(t *testing.T) {
	document := []byte(`
[compile_cache]
enabled = true
public_url = "  https://cache.example.test/  "
max_size_gb = 20
max_entries = 500000
max_entry_size_mb = 512
namespace_max_size_gb = 20
namespace_max_entries = 250000
max_concurrent_uploads = 8
max_inflight_upload_size_mb = 1024
lru_threshold = 90

[compile_cache.storage]
type = "local"
path = "./data/compile-cache"
`)
	cfg, err := decodeConfigDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.CompileCache.PublicURL; got != "https://cache.example.test" {
		t.Fatalf("public URL = %q", got)
	}
	if strings.Contains(cfg.CompileCache.PublicURL, " ") {
		t.Fatal("normalized public URL contains whitespace")
	}
	if got := cfg.CompileCache.UploadTimeout; got != 15*time.Minute {
		t.Fatalf("upload timeout = %s", got)
	}
	if got := cfg.CompileCache.DownloadTimeout; got != 15*time.Minute {
		t.Fatalf("download timeout = %s", got)
	}
}
