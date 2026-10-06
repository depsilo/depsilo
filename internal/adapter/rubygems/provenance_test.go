package rubygems

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

func TestParseRubyGemsInfoLine(t *testing.T) {
	t.Parallel()
	version, cksum, createdAt, ok := parseRubyGemsInfoLine(
		"8.1.4 actionpack:= 8.1.4|checksum:5f3ecdf9,ruby:>= 3.1.0,created_at:2026-09-24T14:22:14Z",
	)
	if !ok || version != "8.1.4" || cksum != "5f3ecdf9" || createdAt != "2026-09-24T14:22:14Z" {
		t.Fatalf("parsed = (%q, %q, %q, %v)", version, cksum, createdAt, ok)
	}

	version, cksum, createdAt, ok = parseRubyGemsInfoLine(
		"1.11.0.rc2-x86_64-linux |checksum:102089c2,created_at:2020-04-01T19:18:29Z",
	)
	if !ok || version != "1.11.0.rc2-x86_64-linux" || cksum != "102089c2" ||
		createdAt != "2020-04-01T19:18:29Z" {
		t.Fatalf("platform parsed = (%q, %q, %q, %v)", version, cksum, createdAt, ok)
	}

	if _, _, createdAt, ok := parseRubyGemsInfoLine("3.0.0 |checksum:ccc"); !ok || createdAt != "" {
		t.Fatalf("missing created_at parse = (%q, %v)", createdAt, ok)
	}
	if _, _, _, ok := parseRubyGemsInfoLine("garbage"); ok {
		t.Fatal("malformed line was accepted")
	}
}

func TestRubyGemsArtifactSourceIDIsStableAndChecksumSpecific(t *testing.T) {
	t.Parallel()
	first := rubygemsArtifactSourceID("acme-demo", "1.0.0", "AAA")
	if len(first) != 43 {
		t.Fatalf("source id length = %d, want 43", len(first))
	}
	if again := rubygemsArtifactSourceID("acme-demo", "1.0.0", "aaa"); again != first {
		t.Fatalf("source id is case-sensitive: %q vs %q", first, again)
	}
	if other := rubygemsArtifactSourceID("acme-demo", "2.0.0", "aaa"); other == first {
		t.Fatal("source id ignores the version")
	}
}

func rubyGemsTestFixture(t *testing.T, now time.Time) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var infoHits atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/acme-demo":
			infoHits.Add(1)
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w,
				"1.0.0 |checksum:aaa,created_at:"+now.Add(-time.Hour).Format(time.RFC3339)+"\n"+
					"1.0.0-x86_64-linux |checksum:ddd,created_at:"+now.Add(-time.Hour).Format(time.RFC3339)+"\n"+
					"2.0.0 some:dep|checksum:bbb,created_at:"+now.Add(-30*24*time.Hour).Format(time.RFC3339)+"\n"+
					"3.0.0 |checksum:ccc")
		case "/gems/acme-demo-1.0.0.gem",
			"/gems/acme-demo-1.0.0-x86_64-linux.gem",
			"/gems/acme-demo-2.0.0.gem",
			"/gems/acme-demo-3.0.0.gem":
			_, _ = io.WriteString(w, "gem-bytes")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstreamServer.Close)
	return upstreamServer, &infoHits
}

func newRubyGemsTestHandler(t *testing.T, upstreamURL string, cacheConfig config.CacheConfig) (http.Handler, *quarantine.Checker) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "rubygems-provenance.db"))
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
	handler.Register(router.Group("/rubygems"))

	enabled := true
	policy, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"rubygems": "168h"},
	}, func(ecosystem string) bool { return ecosystem == "rubygems" })
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

func requestGem(t *testing.T, handler http.Handler, filename string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/rubygems/gems/"+filename, nil))
	return response
}

func TestRubyGemsDownloadUsesCompactIndexCreatedAt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, _ := rubyGemsTestFixture(t, time.Now().UTC())
	scoped, _ := newRubyGemsTestHandler(t, upstreamServer.URL, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour})

	young := requestGem(t, scoped, "acme-demo-1.0.0.gem")
	if young.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(young.Body.String(), `"code":"QUARANTINED"`) {
		t.Fatalf("young gem status=%d body=%s", young.Code, young.Body.String())
	}
	old := requestGem(t, scoped, "acme-demo-2.0.0.gem")
	if old.Code != http.StatusOK || old.Body.String() != "gem-bytes" {
		t.Fatalf("old gem status=%d body=%q", old.Code, old.Body.String())
	}
	platform := requestGem(t, scoped, "acme-demo-1.0.0-x86_64-linux.gem")
	if platform.Code != http.StatusUnavailableForLegalReasons {
		t.Fatalf("platform gem status=%d body=%s", platform.Code, platform.Body.String())
	}
	missing := requestGem(t, scoped, "acme-demo-3.0.0.gem")
	if missing.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(missing.Body.String(), "source-bound publish time") {
		t.Fatalf("missing created_at status=%d body=%s", missing.Code, missing.Body.String())
	}
	unknown := requestGem(t, scoped, "unknown-gem-1.0.0.gem")
	if unknown.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(unknown.Body.String(), "identity is unavailable") {
		t.Fatalf("unknown gem status=%d body=%s", unknown.Code, unknown.Body.String())
	}
}

func TestRubyGemsProvenanceMemoizesInfoLookups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, infoHits := rubyGemsTestFixture(t, time.Now().UTC())
	// TTLIndex 0 expires metadata immediately, so a second resolution would
	// hit the upstream again without the in-process memo.
	scoped, _ := newRubyGemsTestHandler(t, upstreamServer.URL, config.CacheConfig{TTLIndex: 0, TTLBlob: time.Hour})

	for attempt := 0; attempt < 2; attempt++ {
		response := requestGem(t, scoped, "acme-demo-2.0.0.gem")
		if response.Code != http.StatusOK {
			t.Fatalf("attempt %d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	if got := infoHits.Load(); got != 1 {
		t.Fatalf("info hits = %d, want 1", got)
	}
}
