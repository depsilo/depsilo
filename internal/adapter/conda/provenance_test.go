package conda

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

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/quarantine"
	"depsilo/internal/upstream"
)

func condaTestFixture(t *testing.T, now time.Time) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var headHits atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := ""
		switch r.URL.Path {
		case "/pkgs/main/linux-64/acme-demo-1.0.0-0.conda":
			version = "1.0.0"
			w.Header().Set("Last-Modified", now.Add(-time.Hour).UTC().Format(http.TimeFormat))
		case "/pkgs/main/linux-64/acme-demo-2.0.0-0.conda":
			version = "2.0.0"
			w.Header().Set("Last-Modified", now.Add(-30*24*time.Hour).UTC().Format(http.TimeFormat))
		case "/pkgs/main/linux-64/acme-demo-3.0.0-0.conda":
			version = "3.0.0"
		default:
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodHead {
			headHits.Add(1)
			return
		}
		_, _ = io.WriteString(w, "conda-bytes-"+version)
	}))
	t.Cleanup(upstreamServer.Close)
	return upstreamServer, &headHits
}

func newCondaTestHandler(t *testing.T, upstreamURL string) (http.Handler, *quarantine.Checker) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "conda-provenance.db"))
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
		Name: "mock", URL: upstreamURL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(manager, upstream.NewPassiveRecoverySelector(pool), config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour}, database)
	handler.SetProvenanceRequired(true)
	router := gin.New()
	handler.Register(router.Group("/conda"))

	enabled := true
	policy, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"conda": "168h"},
		ApproximateSources:   []string{"conda"},
	}, nil)
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance: %v", err)
	}
	store := quarantine.NewStore(database)
	checker, err := quarantine.NewChecker(policy, quarantine.NewLookup(store, nil), store)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	return adapter.NewRequestScope(nil, nil, quarantine.Wrap(checker), nil).Wrap(router), checker
}

func requestCondaArtifact(t *testing.T, handler http.Handler, version string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/conda/pkgs/main/linux-64/acme-demo-"+version+"-0.conda",
		nil,
	))
	return response
}

func TestCondaArtifactUsesLastModifiedProvenance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, _ := condaTestFixture(t, time.Now().UTC())
	scoped, _ := newCondaTestHandler(t, upstreamServer.URL)

	young := requestCondaArtifact(t, scoped, "1.0.0")
	if young.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(young.Body.String(), `"code":"QUARANTINED"`) {
		t.Fatalf("young conda status=%d body=%s", young.Code, young.Body.String())
	}
	old := requestCondaArtifact(t, scoped, "2.0.0")
	if old.Code != http.StatusOK || old.Body.String() != "conda-bytes-2.0.0" {
		t.Fatalf("old conda status=%d body=%q", old.Code, old.Body.String())
	}
	missing := requestCondaArtifact(t, scoped, "3.0.0")
	if missing.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(missing.Body.String(), "source-bound publish time") {
		t.Fatalf("missing Last-Modified status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestCondaProvenanceMemoizesHeadLookups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, headHits := condaTestFixture(t, time.Now().UTC())
	scoped, _ := newCondaTestHandler(t, upstreamServer.URL)

	for attempt := 0; attempt < 2; attempt++ {
		response := requestCondaArtifact(t, scoped, "2.0.0")
		if response.Code != http.StatusOK {
			t.Fatalf("attempt %d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	if got := headHits.Load(); got != 1 {
		t.Fatalf("HEAD hits = %d, want 1", got)
	}
}
