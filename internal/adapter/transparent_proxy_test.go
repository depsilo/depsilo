package adapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
	"github.com/gin-gonic/gin"
)

func TestTransparentProxyOwnsFetchCacheStreamAndAccessLogPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamCalls atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstreamCalls.Add(1)
		if request.URL.Path != "/packages/example.bin" {
			t.Errorf("upstream path = %q, want /packages/example.bin", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/x-example")
		writer.Header().Set("ETag", `"artifact-v1"`)
		writer.Header().Set("Last-Modified", "Sat, 01 Aug 2026 08:00:00 GMT")
		_, _ = writer.Write([]byte("artifact"))
	}))
	t.Cleanup(upstreamServer.Close)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "transparent.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&db.CacheEntry{}, &db.AccessLog{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	storage, err := cache.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTransparentTestManager(t, manager) })
	pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "fixture", URL: upstreamServer.URL, Priority: 1}})
	if err != nil {
		t.Fatalf("create upstream pool: %v", err)
	}
	proxy := NewTransparentProxy("fixture", manager, upstream.NewPrioritySelector(pool), database)
	engine := gin.New()
	engine.GET("/fixture/*path", func(c *gin.Context) {
		proxy.Serve(c, TransparentPlan{Path: "packages/example.bin", CacheKey: "fixture/packages/example.bin", TTL: time.Hour})
	})

	for requestNumber := 1; requestNumber <= 2; requestNumber++ {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/fixture/packages/example.bin", nil))
		if response.Code != http.StatusOK || response.Body.String() != "artifact" {
			t.Fatalf("request %d response = %d %q", requestNumber, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Content-Type"); got != "application/x-example" {
			t.Fatalf("request %d content type = %q", requestNumber, got)
		}
		if got := response.Header().Get("ETag"); got != `"artifact-v1"` {
			t.Fatalf("request %d ETag = %q", requestNumber, got)
		}
		if got := response.Header().Get("Last-Modified"); got != "Sat, 01 Aug 2026 08:00:00 GMT" {
			t.Fatalf("request %d Last-Modified = %q", requestNumber, got)
		}
	}
	if calls := upstreamCalls.Load(); calls != 1 {
		t.Fatalf("upstream calls = %d, want one miss followed by one hit", calls)
	}

	var logs []db.AccessLog
	if err := database.Order("id ASC").Find(&logs).Error; err != nil {
		t.Fatalf("read access logs: %v", err)
	}
	if len(logs) != 2 || logs[0].Hit || !logs[1].Hit {
		t.Fatalf("access logs = %+v, want miss then hit", logs)
	}
	for _, log := range logs {
		if log.DeliveryResult != "completed" {
			t.Fatalf("successful transfer delivery = %q", log.DeliveryResult)
		}
	}

	// A 200 header and a cache hit do not prove a completed client download.
	interrupted := &disconnectedWriter{ResponseRecorder: httptest.NewRecorder()}
	engine.ServeHTTP(interrupted, httptest.NewRequest(http.MethodGet, "/fixture/packages/example.bin", nil))
	var failed db.AccessLog
	if err := database.Order("id DESC").First(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if failed.DeliveryResult != "failed" {
		t.Fatalf("interrupted cache hit delivery = %q", failed.DeliveryResult)
	}
}

type disconnectedWriter struct {
	*httptest.ResponseRecorder
}

func (writer *disconnectedWriter) Write(body []byte) (int, error) {
	written, _ := writer.ResponseRecorder.Write(body[:1])
	return written, errors.New("client disconnected")
}

func TestTransparentProxyRecordsFailedUpstreamRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstreamServer.Close)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "transparent-error.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&db.CacheEntry{}, &db.AccessLog{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	storage, err := cache.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTransparentTestManager(t, manager) })
	pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "broken", URL: upstreamServer.URL, Priority: 1}})
	if err != nil {
		t.Fatalf("create upstream pool: %v", err)
	}
	proxy := NewTransparentProxy("fixture", manager, upstream.NewPrioritySelector(pool), database)
	engine := gin.New()
	engine.GET("/fixture/*path", func(c *gin.Context) {
		proxy.Serve(c, TransparentPlan{Path: "packages/missing.bin", CacheKey: "fixture/packages/missing.bin", TTL: time.Hour})
	})

	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/fixture/packages/missing.bin", nil))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusBadGateway)
	}

	var logs []db.AccessLog
	if err := database.Find(&logs).Error; err != nil {
		t.Fatalf("read access logs: %v", err)
	}
	if len(logs) != 1 || logs[0].StatusCode != http.StatusBadGateway || logs[0].Hit || logs[0].Upstream != "broken" {
		t.Fatalf("access logs = %+v, want one failed miss", logs)
	}
}

func closeTransparentTestManager(t *testing.T, manager *cache.Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatalf("close manager: %v", err)
	}
}

// An apt pool of two archive mirrors: the preferred mirror lacks one package
// and the InRelease file, and its Packages index is temporarily unavailable.
func TestTransparentProxyMovesOnOnlyWhereAnotherMirrorMayAnswer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var mirrorHits, officialHits atomic.Int64
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits.Add(1)
		switch r.URL.Path {
		case "/pool/main/h/hello/hello_2.10-3_amd64.deb", "/pool/main/m/missing/missing_1.0_amd64.deb", "/dists/jammy/InRelease":
			http.NotFound(w, r)
		case "/dists/jammy/main/binary-amd64/Packages.gz":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte("mirror:" + r.URL.Path))
		}
	}))
	t.Cleanup(mirror.Close)
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		officialHits.Add(1)
		if r.URL.Path == "/pool/main/m/missing/missing_1.0_amd64.deb" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("official:" + r.URL.Path))
	}))
	t.Cleanup(official.Close)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "transparent.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CacheEntry{}, &db.AccessLog{}); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTransparentTestManager(t, manager) })
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "mirror", URL: mirror.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "official", URL: official.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy := NewTransparentProxy("apt", manager, upstream.NewPassiveRecoverySelector(pool), database)
	proxy.RetryMissingArtifacts = true
	engine := gin.New()
	engine.GET("/apt/*path", func(c *gin.Context) {
		path := c.Param("path")
		proxy.Serve(c, TransparentPlan{Path: path, CacheKey: "apt/ubuntu" + path, TTL: time.Hour})
	})

	for _, tc := range []struct {
		name, path      string
		status          int
		body            string
		officialRequest bool
	}{
		{"package missing on the mirror", "/pool/main/h/hello/hello_2.10-3_amd64.deb", http.StatusOK, "official:/pool/main/h/hello/hello_2.10-3_amd64.deb", true},
		// Metadata stays on one mirror's snapshot: a 404 there is the answer.
		{"metadata missing on the mirror", "/dists/jammy/InRelease", http.StatusNotFound, "", false},
		{"metadata unavailable on the mirror", "/dists/jammy/main/binary-amd64/Packages.gz", http.StatusOK, "official:/dists/jammy/main/binary-amd64/Packages.gz", true},
		{"package missing everywhere", "/pool/main/m/missing/missing_1.0_amd64.deb", http.StatusNotFound, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := officialHits.Load()
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/apt"+tc.path, nil))
			if response.Code != tc.status || (tc.body != "" && response.Body.String() != tc.body) {
				t.Fatalf("status=%d body=%q, want %d %q", response.Code, response.Body.String(), tc.status, tc.body)
			}
			if asked := officialHits.Load() > before; asked != tc.officialRequest {
				t.Fatalf("official member asked = %v, want %v", asked, tc.officialRequest)
			}
		})
	}
}
