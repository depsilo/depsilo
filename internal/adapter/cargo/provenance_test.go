package cargo

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

func TestCargoIndexPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		prefix, pkg string
		ok          bool
	}{
		{name: "a", prefix: "1", pkg: "a", ok: true},
		{name: "ab", prefix: "2", pkg: "ab", ok: true},
		{name: "abc", prefix: "3/a", pkg: "abc", ok: true},
		{name: "Serde", prefix: "se/rd", pkg: "serde", ok: true},
		{name: "acme-demo", prefix: "ac/me", pkg: "acme-demo", ok: true},
		{name: "", ok: false},
		{name: "bad/name", ok: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			prefix, pkg, ok := cargoIndexPath(testCase.name)
			if prefix != testCase.prefix || pkg != testCase.pkg || ok != testCase.ok {
				t.Fatalf("cargoIndexPath(%q) = (%q, %q, %v), want (%q, %q, %v)",
					testCase.name, prefix, pkg, ok, testCase.prefix, testCase.pkg, testCase.ok)
			}
		})
	}
}

func TestCargoArtifactSourceIDIsStableAndChecksumSpecific(t *testing.T) {
	t.Parallel()
	first := cargoArtifactSourceID("Serde", "1.0.0", "ABC")
	if len(first) != 43 {
		t.Fatalf("source id length = %d, want 43", len(first))
	}
	if again := cargoArtifactSourceID("serde", "1.0.0", "abc"); again != first {
		t.Fatalf("source id is case-sensitive: %q vs %q", first, again)
	}
	if other := cargoArtifactSourceID("serde", "1.0.0", "def"); other == first {
		t.Fatal("source id ignores the checksum")
	}
}

func cargoTestFixture(t *testing.T, now time.Time) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var indexHits atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ac/me/acme-demo":
			indexHits.Add(1)
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w,
				`{"name":"acme-demo","vers":"1.0.0","cksum":"aaa","pubtime":"`+
					now.Add(-time.Hour).Format(time.RFC3339)+`"}`+"\n"+
					`{"name":"acme-demo","vers":"2.0.0","cksum":"bbb","pubtime":"`+
					now.Add(-30*24*time.Hour).Format(time.RFC3339)+`"}`+"\n"+
					`{"name":"acme-demo","vers":"3.0.0","cksum":"ccc"}`)
		case "/api/v1/crates/acme-demo/1.0.0/download",
			"/api/v1/crates/acme-demo/2.0.0/download",
			"/api/v1/crates/acme-demo/3.0.0/download":
			_, _ = io.WriteString(w, "crate-bytes")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstreamServer.Close)
	return upstreamServer, &indexHits
}

func newCargoTestHandler(t *testing.T, upstreamURL string, cacheConfig config.CacheConfig) (http.Handler, *quarantine.Checker) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "cargo-provenance.db"))
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
	handler := New(manager, upstream.NewPassiveRecoverySelector(pool), cacheConfig, database)
	handler.SetProvenanceRequired(true)
	router := gin.New()
	handler.Register(router.Group("/crates"))

	enabled := true
	policy, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"cargo": "168h"},
	}, func(ecosystem string) bool { return ecosystem == "cargo" })
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

func requestCrate(t *testing.T, handler http.Handler, version string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/crates/api/v1/crates/acme-demo/"+version+"/download",
		nil,
	))
	return response
}

func TestCargoDownloadUsesIndexPubTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	upstreamServer, _ := cargoTestFixture(t, now)
	scoped, _ := newCargoTestHandler(t, upstreamServer.URL, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour})

	young := requestCrate(t, scoped, "1.0.0")
	if young.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(young.Body.String(), `"code":"QUARANTINED"`) {
		t.Fatalf("young crate status=%d body=%s", young.Code, young.Body.String())
	}
	old := requestCrate(t, scoped, "2.0.0")
	if old.Code != http.StatusOK || old.Body.String() != "crate-bytes" {
		t.Fatalf("old crate status=%d body=%q", old.Code, old.Body.String())
	}
	missing := requestCrate(t, scoped, "3.0.0")
	if missing.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(missing.Body.String(), "source-bound publish time") {
		t.Fatalf("missing pubtime status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestCargoProvenanceMemoizesIndexLookups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	upstreamServer, indexHits := cargoTestFixture(t, now)
	// TTLIndex 0 expires metadata immediately, so a second resolution would
	// hit the upstream again without the in-process memo.
	scoped, _ := newCargoTestHandler(t, upstreamServer.URL, config.CacheConfig{TTLIndex: 0, TTLBlob: time.Hour})

	for attempt := 0; attempt < 2; attempt++ {
		response := requestCrate(t, scoped, "2.0.0")
		if response.Code != http.StatusOK {
			t.Fatalf("attempt %d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	if got := indexHits.Load(); got != 1 {
		t.Fatalf("index hits = %d, want 1", got)
	}
}
