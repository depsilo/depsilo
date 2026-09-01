package docker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
)

func TestTagListPreservesQueryAndIsolatesCachedPages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var tagRequests atomic.Int64
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
		case "/v2/library/widget/tags/list":
			tagRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"query":%q}`, request.URL.RawQuery)
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(registry.Close)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "docker-query.db"))
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
	handler := New(manager, config.CacheConfig{TTLIndex: time.Hour}, database, config.DockerConfig{
		DefaultRegistry: "test",
		Registries: []config.RegistryConfig{{
			Name: "test",
			URL:  registry.URL,
		}},
	})
	router := gin.New()
	handler.Register(router.Group("/v2"))

	requestPage := func(target string) string {
		t.Helper()
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, body=%s", target, response.Code, response.Body.String())
		}
		return response.Body.String()
	}

	if got := requestPage("/v2/library/widget/tags/list?n=1&last=alpha"); got != `{"query":"n=1&last=alpha"}` {
		t.Fatalf("first page body = %s", got)
	}
	if got := requestPage("/v2/library/widget/tags/list?n=1&last=beta"); got != `{"query":"n=1&last=beta"}` {
		t.Fatalf("second page body = %s", got)
	}
	if got := requestPage("/v2/library/widget/tags/list?n=1&last=alpha"); got != `{"query":"n=1&last=alpha"}` {
		t.Fatalf("cached first page body = %s", got)
	}
	if got := tagRequests.Load(); got != 2 {
		t.Fatalf("tag-list upstream requests = %d, want 2 distinct pages", got)
	}
}

func TestManifestBoundsUpstreamErrorBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/":
			_, _ = io.WriteString(w, `{}`)
		case "/v2/library/widget/manifests/latest":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, strings.Repeat("x", 256<<10)+"DO_NOT_INCLUDE")
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(registry.Close)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "docker-error.db"))
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
	handler := New(manager, config.CacheConfig{TTLIndex: time.Hour}, database, config.DockerConfig{
		DefaultRegistry: "test",
		Registries:      []config.RegistryConfig{{Name: "test", URL: registry.URL}},
	})
	router := gin.New()
	handler.Register(router.Group("/v2"))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v2/library/widget/manifests/latest", nil)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "DO_NOT_INCLUDE") {
		t.Fatal("public error included bytes beyond the bounded diagnostic prefix")
	}
	if response.Body.Len() > maxDockerErrorBodyBytes+1024 {
		t.Fatalf("public error bytes = %d, want bounded response", response.Body.Len())
	}
}
