package docker

import (
	"context"
	"crypto/sha256"
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

// Clients such as containerd resolve a tag with HEAD before pulling. While
// the registry is unreachable, the cached manifest answers that HEAD, so a
// previously pulled image keeps pulling offline.
func TestManifestHeadAnswersFromCacheWhileTheRegistryIsUnreachable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const mediaType = "application/vnd.oci.image.index.v1+json"
	manifest := `{"schemaVersion":2,"mediaType":"` + mediaType + `","manifests":[]}`
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(manifest)))
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/":
			_, _ = io.WriteString(w, `{}`)
		case "/v2/library/widget/manifests/1.0":
			w.Header().Set("Content-Type", mediaType)
			w.Header().Set("Docker-Content-Digest", digest)
			w.Header().Set("Content-Length", fmt.Sprint(len(manifest)))
			if request.Method == http.MethodGet {
				_, _ = io.WriteString(w, manifest)
			}
		default:
			http.NotFound(w, request)
		}
	}))
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "docker-head.db"))
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
		_ = manager.Close(ctx)
	})
	// An expired index TTL makes every offline read take the stale path.
	handler := New(manager, config.CacheConfig{TTLIndex: time.Millisecond, TTLBlob: time.Hour}, database, config.DockerConfig{
		DefaultRegistry: "test",
		Registries:      []config.RegistryConfig{{Name: "test", URL: registry.URL}},
	})
	router := gin.New()
	handler.Register(router.Group("/v2"))
	request := func(method, target string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, target, nil))
		return response
	}

	if got := request(http.MethodGet, "/v2/library/widget/manifests/1.0"); got.Code != http.StatusOK || got.Body.String() != manifest {
		t.Fatalf("online GET = %d %q", got.Code, got.Body.String())
	}
	if got := request(http.MethodHead, "/v2/library/widget/manifests/1.0"); got.Code != http.StatusOK || got.Header().Get("Docker-Content-Digest") != digest {
		t.Fatalf("online HEAD = %d digest %q", got.Code, got.Header().Get("Docker-Content-Digest"))
	}

	registry.Close()
	time.Sleep(5 * time.Millisecond)
	head := request(http.MethodHead, "/v2/library/widget/manifests/1.0")
	if head.Code != http.StatusOK || head.Header().Get("Docker-Content-Digest") != digest ||
		head.Header().Get("Content-Type") != mediaType || head.Header().Get("Content-Length") != fmt.Sprint(len(manifest)) || head.Body.Len() != 0 {
		t.Fatalf("offline HEAD = %d headers=%v body=%q", head.Code, head.Header(), head.Body.String())
	}
	if got := request(http.MethodGet, "/v2/library/widget/manifests/1.0"); got.Code != http.StatusOK || got.Body.String() != manifest {
		t.Fatalf("offline GET = %d %q", got.Code, got.Body.String())
	}
	if got := request(http.MethodHead, "/v2/library/widget/manifests/2.0"); got.Code != http.StatusBadGateway {
		t.Fatalf("offline HEAD of an uncached tag = %d, want 502", got.Code)
	}
}
