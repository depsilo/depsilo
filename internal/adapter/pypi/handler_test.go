package pypi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

func TestPackageIndexUsesTTLAndSupportsForcedConditionalRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var mu sync.Mutex
	var validators []string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		validators = append(validators, r.Header.Get("If-None-Match"))
		mu.Unlock()
		if r.Header.Get("If-None-Match") == `"pillow-v2"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("ETag", `"pillow-v2"`)
		_, _ = io.WriteString(w, `<html><a href="https://files.pythonhosted.org/packages/aa/pillow-12.3.0.whl">pillow-12.3.0.whl</a></html>`)
	}))
	defer upstreamServer.Close()

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "pypi.db"))
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
	key := IndexCacheKey("pypi", "pillow")
	old := `<html><a href="/pypi/files/packages/aa/pillow-12.2.0.whl">pillow-12.2.0.whl</a></html>`
	if err := storage.Put(context.Background(), key, io.NopCloser(strings.NewReader(old)), int64(len(old)), "text/html"); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		PackageName: "pillow", StoragePath: key, ContentType: "text/html",
		ETag: `"pillow-v1"`, ExpiresAt: time.Now().Add(time.Hour), LastAccessed: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive"}})
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
	handler := New(manager, upstream.NewPrioritySelector(pool), config.CacheConfig{TTLIndex: time.Hour, TTLBlob: 72 * time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/pypi"))

	request := func(forced bool) string {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/pypi/simple/pillow/", nil)
		if forced {
			req = req.WithContext(cache.WithForceRefresh(req.Context()))
		}
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}

	if body := request(false); !strings.Contains(body, "pillow-12.2.0") {
		t.Fatalf("fresh TTL cache was not served directly: %s", body)
	}
	mu.Lock()
	if len(validators) != 0 {
		t.Fatalf("fresh TTL cache unexpectedly contacted upstream: %#v", validators)
	}
	mu.Unlock()

	if body := request(true); !strings.Contains(body, "pillow-12.3.0") {
		t.Fatalf("forced refresh did not return current project index: %s", body)
	}
	deadline := time.Now().Add(time.Second)
	for {
		var entry db.CacheEntry
		if err := database.Where("key = ?", key).First(&entry).Error; err != nil {
			t.Fatal(err)
		}
		if entry.ETag == `"pillow-v2"` {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("new validator was not persisted: %+v", entry)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if body := request(false); !strings.Contains(body, "pillow-12.3.0") {
		t.Fatalf("refreshed index was not served from TTL cache: %s", body)
	}
	if body := request(true); !strings.Contains(body, "pillow-12.3.0") {
		t.Fatalf("304 response did not serve cached current index: %s", body)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(validators) != 2 || validators[0] != `"pillow-v1"` || validators[1] != `"pillow-v2"` {
		t.Fatalf("conditional validators = %#v", validators)
	}
}
