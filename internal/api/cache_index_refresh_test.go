package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter/pypi"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

func TestCacheIndexProxyPathSupportsConfiguredExtraPyPIIndex(t *testing.T) {
	for _, key := range []string{
		"extra:private/simple/pillow/index.html",
		"extra:private/simple/pillow/_signed/v1/" + strings.Repeat("a", 64) + "/index.html",
	} {
		entry := db.CacheEntry{
			AdapterType: "extra:private",
			CacheKind:   db.CacheKindMetadata,
			Key:         key,
		}
		got, err := cacheIndexProxyPath(entry, []config.ExtraIndexConfig{{Name: "private", Path: "python/private"}}, config.DockerConfig{})
		if err != nil {
			t.Fatal(err)
		}
		if got != "/python/private/simple/pillow/" {
			t.Fatalf("extra index proxy path = %q", got)
		}
	}

	entry := db.CacheEntry{AdapterType: "extra:private", CacheKind: db.CacheKindMetadata, Key: "extra:private/simple/pillow/index.html"}
	if _, err := cacheIndexProxyPath(entry, []config.ExtraIndexConfig{{Name: "private", Path: "../admin"}}, config.DockerConfig{}); err == nil {
		t.Fatal("unsafe configured route was accepted")
	}
}

func TestCacheIndexProxyPathSupportsPyTorchChannelFamily(t *testing.T) {
	entry := db.CacheEntry{
		AdapterType: "extra:pytorch",
		CacheKind:   db.CacheKindMetadata,
		Key:         "extra:pytorch/channels/rocm6.4/simple/torch/index.html",
	}
	indexes := []config.ExtraIndexConfig{{
		Name: "pytorch", Kind: config.ExtraIndexKindPyTorch, Path: "pypi-torch",
	}}
	got, err := cacheIndexProxyPath(entry, indexes, config.DockerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "/pypi-torch/rocm6.4/simple/torch/" {
		t.Fatalf("PyTorch channel proxy path = %q", got)
	}

	entry.Key = "extra:pytorch/channels/../simple/torch/index.html"
	if _, err := cacheIndexProxyPath(entry, indexes, config.DockerConfig{}); err == nil {
		t.Fatal("unsafe PyTorch channel cache key was accepted")
	}
}

func TestCacheIndexProxyPathSupportsDockerMetadata(t *testing.T) {
	dockerConfig := config.DockerConfig{
		DefaultRegistry: "dockerhub",
		Registries: []config.RegistryConfig{
			{Name: "dockerhub", URL: "https://registry-1.docker.io"},
			{Name: "private", URL: "https://registry.example.com:5443"},
		},
	}
	tests := []struct {
		name  string
		entry db.CacheEntry
		want  string
	}{
		{
			name: "tag list",
			entry: db.CacheEntry{AdapterType: "docker", CacheKind: db.CacheKindMetadata,
				Key: "docker/dockerhub/tags/library/alpine/list"},
			want: "/v2/registry-1.docker.io/library/alpine/tags/list",
		},
		{
			name: "tagged manifest in non-default registry",
			entry: db.CacheEntry{AdapterType: "docker", CacheKind: db.CacheKindMetadata,
				Key: "docker/private/manifests/team/app/v1.2.3"},
			want: "/v2/registry.example.com:5443/team/app/manifests/v1.2.3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cacheIndexProxyPath(tt.entry, nil, dockerConfig)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("docker proxy path = %q, want %q", got, tt.want)
			}
		})
	}

	digest := db.CacheEntry{AdapterType: "docker", CacheKind: db.CacheKindMetadata,
		Key: "docker/dockerhub/manifests/library/alpine/sha256__abc"}
	if _, err := cacheIndexProxyPath(digest, nil, dockerConfig); err == nil {
		t.Fatal("digest-addressed Docker manifest was accepted as mutable metadata")
	}
}

func TestCacheIndexRefresherForcesFreshPyPIEntryThroughProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamCalls atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		switch got := r.Header.Get("If-None-Match"); got {
		case `"pillow-v1"`:
		case `"pillow-v2"`:
			w.WriteHeader(http.StatusNotModified)
			return
		default:
			t.Errorf("If-None-Match = %q", got)
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("ETag", `"pillow-v2"`)
		_, _ = io.WriteString(w, `<html><a href="https://files.pythonhosted.org/packages/aa/pillow-12.3.0.whl">pillow-12.3.0.whl</a></html>`)
	}))
	defer upstreamServer.Close()

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "refresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	key := pypi.IndexCacheKey("pypi", "pillow")
	old := `<html><a href="/pypi/files/packages/aa/pillow-12.2.0.whl">pillow-12.2.0.whl</a></html>`
	if err := storage.Put(context.Background(), key, strings.NewReader(old), int64(len(old)), "text/html"); err != nil {
		t.Fatal(err)
	}
	entry := db.CacheEntry{
		Key: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		PackageName: "pillow", StoragePath: key, ContentType: "text/html",
		ETag: `"pillow-v1"`, ExpiresAt: time.Now().Add(time.Hour), LastAccessed: time.Now(),
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	originalExpiry := entry.ExpiresAt

	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), 72*time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})
	handler := pypi.New(manager, upstream.NewPrioritySelector(pool), config.CacheConfig{
		TTLIndex: 5 * time.Minute,
		TTLBlob:  72 * time.Hour,
	}, database)
	router := gin.New()
	handler.Register(router.Group("/pypi"))

	refresher := NewCacheIndexRefresher(router, nil, config.DockerConfig{})
	outcome, err := refresher(context.Background(), entry)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Upstream != "mock" || !outcome.Changed {
		t.Fatalf("refresh outcome = %+v", outcome)
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1", upstreamCalls.Load())
	}

	var refreshed db.CacheEntry
	if err := database.First(&refreshed, entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if refreshed.ExpiresAt.Equal(originalExpiry) || refreshed.ETag != `"pillow-v2"` {
		t.Fatalf("refreshed entry = %+v", refreshed)
	}
	reader, _, err := storage.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "pillow-12.3.0") {
		t.Fatalf("stored refreshed index = %s", body)
	}

	firstRefreshExpiry := refreshed.ExpiresAt
	outcome, err = refresher(context.Background(), refreshed)
	if err != nil {
		t.Fatalf("conditional 304 refresh: %v", err)
	}
	if outcome.Upstream != "mock" || outcome.Changed {
		t.Fatalf("304 refresh outcome = %+v", outcome)
	}
	if upstreamCalls.Load() != 2 {
		t.Fatalf("upstream calls after 304 = %d, want 2", upstreamCalls.Load())
	}
	var internalAccessLogs int64
	if err := database.Model(&db.AccessLog{}).Count(&internalAccessLogs).Error; err != nil {
		t.Fatal(err)
	}
	if internalAccessLogs != 0 {
		t.Fatalf("internal index refresh wrote %d access logs", internalAccessLogs)
	}
	var revalidated db.CacheEntry
	if err := database.First(&revalidated, entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !revalidated.ExpiresAt.After(firstRefreshExpiry) {
		t.Fatalf("304 did not extend expiry: before=%v after=%v", firstRefreshExpiry, revalidated.ExpiresAt)
	}
}

func TestCacheIndexRefresherReturnsProxyFailureWithoutTouchingExpiry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "https://alice:secret@example.test/private?token=hidden", http.StatusBadGateway)
	}))
	defer upstreamServer.Close()

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "failure-cache"))
	if err != nil {
		t.Fatal(err)
	}
	key := pypi.IndexCacheKey("pypi", "pillow")
	old := "<html>pillow-12.2.0</html>"
	if err := storage.Put(context.Background(), key, strings.NewReader(old), int64(len(old)), "text/html"); err != nil {
		t.Fatal(err)
	}
	entry := db.CacheEntry{
		Key: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		PackageName: "pillow", StoragePath: key, ContentType: "text/html", ETag: `"pillow-v1"`,
		ExpiresAt: time.Now().Add(time.Hour), LastAccessed: time.Now(),
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}

	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), 72*time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})
	handler := pypi.New(manager, upstream.NewPrioritySelector(pool), config.CacheConfig{
		TTLIndex: 5 * time.Minute,
		TTLBlob:  72 * time.Hour,
	}, database)
	router := gin.New()
	handler.Register(router.Group("/pypi"))

	refresher := NewCacheIndexRefresher(router, nil, config.DockerConfig{})
	outcome, err := refresher(context.Background(), entry)
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("refresh error = %v", err)
	}
	if outcome.Upstream != "mock" {
		t.Fatalf("failure outcome = %+v, want selected upstream mock", outcome)
	}
	for _, secret := range []string{"alice", "secret", "private", "token", "hidden"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("refresh error disclosed %q: %v", secret, err)
		}
	}
	var after db.CacheEntry
	if err := database.First(&after, entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !after.ExpiresAt.Equal(entry.ExpiresAt) {
		t.Fatalf("failed refresh changed expiry from %v to %v", entry.ExpiresAt, after.ExpiresAt)
	}
}
