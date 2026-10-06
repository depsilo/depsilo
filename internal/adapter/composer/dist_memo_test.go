package composer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

func TestDistEntryMemoSkipsRepeatedMetadataParsing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	var metadataHits atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/p2/acme/demo.json":
			metadataHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"packages": map[string]any{
					"acme/demo": []any{map[string]any{
						"version":            "1.0.0",
						"version_normalized": "1.0.0.0",
						"time":               now.Add(-30 * 24 * time.Hour).Format(time.RFC3339),
						"dist": map[string]any{
							"url":       "http://" + r.Host + "/files/demo-1.0.0.zip",
							"type":      "zip",
							"reference": "ref1",
						},
					}},
				},
			})
		case "/files/demo-1.0.0.zip":
			_, _ = io.WriteString(w, "zip-bytes")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstreamServer.Close)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "composer-memo.db"))
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
	manager := cache.NewManager(storage, database, cache.NewEventBus(), 72*time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	// TTLIndex 0 expires metadata immediately, so a second resolution would
	// hit the upstream again without the in-process memo.
	handler := New(
		manager,
		upstream.NewPassiveRecoverySelector(pool),
		config.CacheConfig{TTLIndex: 0, TTLBlob: time.Hour},
		database,
	)
	router := gin.New()
	handler.Register(router.Group("/composer"))

	for attempt := 0; attempt < 2; attempt++ {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(
			http.MethodGet,
			"/composer/dist/acme/demo/1.0.0.0/ref1.zip",
			nil,
		))
		if response.Code != http.StatusOK || response.Body.String() != "zip-bytes" {
			t.Fatalf("attempt %d status=%d body=%q", attempt, response.Code, response.Body.String())
		}
	}
	if got := metadataHits.Load(); got != 1 {
		t.Fatalf("metadata hits = %d, want 1", got)
	}
}
