package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/adapter/pypi"
	"depsilo/internal/cache"
	"depsilo/internal/db"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestStartServerRecordsProactiveMetadataRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamCalls atomic.Int64
	var artifactCalls atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, ".whl") {
			artifactCalls.Add(1)
		}
		upstreamCalls.Add(1)
		if request.URL.Path != "/simple/pillow/" {
			http.NotFound(writer, request)
			return
		}
		switch got := request.Header.Get("If-None-Match"); got {
		case `"pillow-v1"`:
		case `"pillow-v2"`:
			writer.WriteHeader(http.StatusNotModified)
			return
		default:
			t.Errorf("If-None-Match = %q, want a persisted pillow validator", got)
		}
		writer.Header().Set("Content-Type", "text/html")
		writer.Header().Set("ETag", `"pillow-v2"`)
		_, _ = io.WriteString(writer, `<a href="pillow-12.3.0.whl">pillow-12.3.0</a>`)
	}))
	defer upstreamServer.Close()

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(dir, "server.db")
	storagePath := filepath.Join(dir, "cache")
	configPath := filepath.Join(dir, "config.toml")
	configDocument := fmt.Sprintf(`
[server]
host = "127.0.0.1"
port = 0
log_level = "error"

[database]
driver = "sqlite"
dsn = %q

[storage]
type = "local"
path = %q

[cache]
max_size_gb = 1
ttl_index = "1h"
ttl_blob = "72h"
lru_threshold = 90

[access_log]
rollup_enabled = false
backfill_on_start = false
retention_days = 0
rollup_retention_days = 0

[upstream_updates]
enabled = true
check_interval = "10ms"

[security]
enabled = false

[supply_chain.blocklist]
enabled = false

[supply_chain.tamper_detection]
enabled = false

[auth]
enabled = true
jwt_secret = "integration-test-secret-with-sufficient-length"
token_ttl = "1h"

[[pypi.upstreams]]
name = "mock"
url = %q
priority = 1
probe_mode = "passive"
probe_interval = "30m"
`, databasePath, storagePath, upstreamServer.URL)
	if err := os.WriteFile(configPath, []byte(configDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_ADMIN_PASSWORD", "integration-Test-password-123")

	seedDatabase, err := db.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(seedDatabase); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(storagePath)
	if err != nil {
		t.Fatal(err)
	}
	key := pypi.IndexCacheKey("pypi", "pillow")
	oldBody := `<a href="pillow-12.2.0.whl">pillow-12.2.0</a>`
	if err := storage.Put(context.Background(), key, io.NopCloser(
		strings.NewReader(oldBody),
	), int64(len(oldBody)), "text/html"); err != nil {
		t.Fatal(err)
	}
	entry := db.CacheEntry{
		Key: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		PackageName: "pillow", StoragePath: key, ContentType: "text/html",
		ETag: `"pillow-v1"`, ExpiresAt: time.Now().Add(time.Hour), LastAccessed: time.Now(),
	}
	if err := seedDatabase.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	artifactKey := "pypi/files/pillow-12.2.0.whl"
	artifactBody := "trusted artifact"
	if err := storage.Put(context.Background(), artifactKey, strings.NewReader(artifactBody), int64(len(artifactBody)), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	artifact := db.CacheEntry{
		Key: artifactKey, AdapterType: "pypi", CacheKind: db.CacheKindArtifact,
		PackageName: "pillow", StoragePath: artifactKey, ContentType: "application/octet-stream",
		ExpiresAt: time.Now().Add(time.Hour), LastAccessed: time.Now(),
	}
	if err := seedDatabase.Create(&artifact).Error; err != nil {
		t.Fatal(err)
	}
	seedSQL, err := seedDatabase.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := seedSQL.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	srv, err := StartServer(ctx, zap.NewAtomicLevel())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := Shutdown(shutdownCtx, srv); err != nil {
			t.Errorf("shutdown server: %v", err)
		}
	})

	readDatabase, err := db.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	readSQL, err := readDatabase.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = readSQL.Close() })

	deadline := time.Now().Add(2 * time.Second)
	for {
		var count int64
		if err := readDatabase.Model(&db.UpstreamUpdateEvent{}).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			var event db.UpstreamUpdateEvent
			if err := readDatabase.Order("id ASC").First(&event).Error; err != nil {
				t.Fatal(err)
			}
			if event.Ecosystem != "pypi" || event.Upstream != "mock" || event.Package != "pillow" || event.Result != "updated" {
				t.Fatalf("upstream update event = %+v", event)
			}
			if upstreamCalls.Load() == 0 {
				t.Fatal("event was persisted without contacting the upstream")
			}
			if artifactCalls.Load() != 0 {
				t.Fatalf("proactive updater requested %d immutable artifacts", artifactCalls.Load())
			}
			var refreshed db.CacheEntry
			if err := readDatabase.First(&refreshed, entry.ID).Error; err != nil {
				t.Fatal(err)
			}
			if refreshed.ETag != `"pillow-v2"` {
				t.Fatalf("event preceded durable cache metadata: ETag = %q", refreshed.ETag)
			}
			reader, _, err := storage.Get(context.Background(), key)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if readErr != nil || closeErr != nil {
				t.Fatalf("read refreshed cache: read=%v close=%v", readErr, closeErr)
			}
			if !strings.Contains(string(body), "pillow-12.3.0") {
				t.Fatalf("event preceded durable cache body: %s", body)
			}
			for model, name := range map[any]string{&db.AccessLog{}: "access", &db.AuditLog{}: "audit"} {
				var internalRows int64
				if err := readDatabase.Model(model).Count(&internalRows).Error; err != nil {
					t.Fatal(err)
				}
				if internalRows != 0 {
					t.Fatalf("proactive refresh polluted %s logs with %d rows", name, internalRows)
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no proactive update event after 2s; upstream calls = %d", upstreamCalls.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
