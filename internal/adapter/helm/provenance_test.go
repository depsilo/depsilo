package helm

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
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/quarantine"
	"depsilo/internal/upstream"
)

const helmTestIndex = `apiVersion: v1
entries:
  acme-demo:
  - apiVersion: v2
    appVersion: "1.0"
    created: "2026-09-01T00:00:00.000000000Z"
    description: "A demo chart: with punctuation"
    digest: sha256:aaa
    maintainers:
    - email: chart@example.com
      name: Maintainer Name
    name: acme-demo
    urls:
    - https://charts.example.com/acme-demo-1.0.0.tgz
    version: 1.0.0
  - apiVersion: v2
    created: "2026-08-01T00:00:00.000000000Z"
    description: A demo chart
    digest: sha256:bbb
    name: acme-demo
    urls:
    - acme-demo-2.0.0.tgz
    version: 2.0.0
  - apiVersion: v2
    created: "2026-09-01T00:00:00.000000000Z"
    digest: sha256:ccc
    name: acme-demo
    urls:
    - acme-demo-3.0.0-1.tgz
    version: 3.0.0-1
  - apiVersion: v2
    created: "2026-08-01T00:00:00.000000000Z"
    digest: sha256:ddd
    name: acme-demo
    urls:
    - acme-demo-4.0.0.tgz
    version: 4.0.0
  acme-demo-lib:
  - apiVersion: v2
    created: "2026-08-01T00:00:00.000000000Z"
    digest: sha256:eee
    name: acme-demo-lib
    urls:
    - charts/acme-demo-lib-1.0.0.tgz
    version: 1.0.0
generated: "2026-10-06T00:00:00.000000000Z"
`

func helmTestFixture(t *testing.T, now time.Time) (*httptest.Server, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	var indexHits, headHits atomic.Int64
	young := now.Add(-time.Hour).UTC().Format(http.TimeFormat)
	old := now.Add(-30 * 24 * time.Hour).UTC().Format(http.TimeFormat)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.yaml":
			indexHits.Add(1)
			w.Header().Set("Content-Type", "text/yaml")
			_, _ = io.WriteString(w, helmTestIndex)
			return
		case "/acme-demo-1.0.0.tgz":
			w.Header().Set("Last-Modified", young)
		case "/acme-demo-2.0.0.tgz":
			w.Header().Set("Last-Modified", old)
		case "/acme-demo-3.0.0-1.tgz":
			w.Header().Set("Last-Modified", young)
		case "/acme-demo-4.0.0.tgz":
			// No Last-Modified: the approximate timestamp is unavailable.
		case "/charts/acme-demo-lib-1.0.0.tgz":
			w.Header().Set("Last-Modified", old)
		case "/rogue-1.0.0.tgz":
			// Not declared in index.yaml; the old timestamp must not help.
			w.Header().Set("Last-Modified", old)
		default:
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodHead {
			headHits.Add(1)
			return
		}
		_, _ = io.WriteString(w, "helm-bytes"+strings.TrimSuffix(r.URL.Path, ".tgz"))
	}))
	t.Cleanup(upstreamServer.Close)
	return upstreamServer, &indexHits, &headHits
}

func newHelmTestHandler(t *testing.T, upstreamURL string, cacheConfig config.CacheConfig) (http.Handler, *gorm.DB) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "helm-provenance.db"))
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
	handler.Register(router.Group("/helm"))

	enabled := true
	policy, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"helm": "168h"},
		ApproximateSources:   []string{"helm"},
	}, nil)
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance: %v", err)
	}
	store := quarantine.NewStore(database)
	checker, err := quarantine.NewChecker(policy, quarantine.NewLookup(store, nil), store)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	return adapter.NewRequestScope(nil, nil, quarantine.Wrap(checker), nil).Wrap(router), database
}

func requestHelmArtifact(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/helm/"+path, nil))
	return response
}

func TestScanHelmIndexResolvesAmbiguousFilenames(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path  string
		chart string
		ver   string
	}{
		{path: "acme-demo-1.0.0.tgz", chart: "acme-demo", ver: "1.0.0"},
		{path: "acme-demo-2.0.0.tgz", chart: "acme-demo", ver: "2.0.0"},
		// The filename splitter would read this as acme-demo-3.0.0@1.
		{path: "acme-demo-3.0.0-1.tgz", chart: "acme-demo", ver: "3.0.0-1"},
		{path: "charts/acme-demo-lib-1.0.0.tgz", chart: "acme-demo-lib", ver: "1.0.0"},
	}
	for _, testCase := range cases {
		base := testCase.path[strings.LastIndex(testCase.path, "/")+1:]
		matches, err := scanHelmIndex(strings.NewReader(helmTestIndex), testCase.path, base)
		if err != nil {
			t.Fatalf("scanHelmIndex(%q): %v", testCase.path, err)
		}
		if len(matches) != 1 || matches[0].chart != testCase.chart || matches[0].version != testCase.ver {
			t.Fatalf("scanHelmIndex(%q) = %+v, want %s@%s", testCase.path, matches, testCase.chart, testCase.ver)
		}
	}

	undeclared, err := scanHelmIndex(strings.NewReader(helmTestIndex), "rogue-1.0.0.tgz", "rogue-1.0.0.tgz")
	if err != nil {
		t.Fatalf("scanHelmIndex(rogue): %v", err)
	}
	if len(undeclared) != 0 {
		t.Fatalf("scanHelmIndex(rogue) = %+v, want no match", undeclared)
	}

	// A second entry declaring the same filename makes the identity ambiguous;
	// the caller fails closed instead of picking one.
	ambiguous := strings.Replace(helmTestIndex, "generated:",
		"  acme-demo-mirror:\n"+
			"  - name: acme-demo\n"+
			"    urls:\n"+
			"    - acme-demo-1.0.0.tgz\n"+
			"    version: 1.0.0\n"+
			"generated:", 1)
	matches, err := scanHelmIndex(strings.NewReader(ambiguous), "acme-demo-1.0.0.tgz", "acme-demo-1.0.0.tgz")
	if err != nil {
		t.Fatalf("scanHelmIndex(ambiguous): %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("ambiguous matches = %+v, want 2", matches)
	}
}

func TestHelmChartGateUsesIndexIdentityAndLastModified(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, _, _ := helmTestFixture(t, time.Now().UTC())
	scoped, database := newHelmTestHandler(t, upstreamServer.URL, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour})

	young := requestHelmArtifact(t, scoped, "acme-demo-1.0.0.tgz")
	if young.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(young.Body.String(), `"code":"QUARANTINED"`) {
		t.Fatalf("young chart status=%d body=%s", young.Code, young.Body.String())
	}

	old := requestHelmArtifact(t, scoped, "acme-demo-2.0.0.tgz")
	if old.Code != http.StatusOK || old.Body.String() != "helm-bytes/acme-demo-2.0.0" {
		t.Fatalf("old chart status=%d body=%q", old.Code, old.Body.String())
	}

	hyphenated := requestHelmArtifact(t, scoped, "charts/acme-demo-lib-1.0.0.tgz")
	if hyphenated.Code != http.StatusOK {
		t.Fatalf("hyphenated chart status=%d body=%s", hyphenated.Code, hyphenated.Body.String())
	}

	prerelease := requestHelmArtifact(t, scoped, "acme-demo-3.0.0-1.tgz")
	if prerelease.Code != http.StatusUnavailableForLegalReasons {
		t.Fatalf("prerelease chart status=%d body=%s", prerelease.Code, prerelease.Body.String())
	}
	var events []db.QuarantineEvent
	if err := database.Order("id").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("quarantine events = %+v, want two blocked decisions", events)
	}
	if events[0].Package != "acme-demo" || events[0].Version != "1.0.0" {
		t.Fatalf("young identity = %s@%s", events[0].Package, events[0].Version)
	}
	if events[1].Package != "acme-demo" || events[1].Version != "3.0.0-1" {
		t.Fatalf("prerelease identity = %s@%s, want the index-declared acme-demo@3.0.0-1",
			events[1].Package, events[1].Version)
	}

	undeclared := requestHelmArtifact(t, scoped, "rogue-1.0.0.tgz")
	if undeclared.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(undeclared.Body.String(), "provenance is unavailable") {
		t.Fatalf("undeclared chart status=%d body=%s", undeclared.Code, undeclared.Body.String())
	}

	missing := requestHelmArtifact(t, scoped, "acme-demo-4.0.0.tgz")
	if missing.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(missing.Body.String(), "source-bound publish time") {
		t.Fatalf("missing Last-Modified status=%d body=%s", missing.Code, missing.Body.String())
	}

	index := requestHelmArtifact(t, scoped, "index.yaml")
	if index.Code != http.StatusOK || !strings.Contains(index.Body.String(), "entries:") {
		t.Fatalf("index.yaml status=%d body=%q", index.Code, index.Body.String())
	}
}

func TestHelmIdentityMemoizesIndexScans(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, indexHits, headHits := helmTestFixture(t, time.Now().UTC())
	// TTLIndex stays zero so the cached index body cannot hide a missing memo.
	scoped, _ := newHelmTestHandler(t, upstreamServer.URL, config.CacheConfig{TTLIndex: 0, TTLBlob: time.Hour})

	for attempt := 0; attempt < 2; attempt++ {
		response := requestHelmArtifact(t, scoped, "acme-demo-2.0.0.tgz")
		if response.Code != http.StatusOK {
			t.Fatalf("attempt %d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	if got := indexHits.Load(); got != 1 {
		t.Fatalf("index.yaml fetches = %d, want 1", got)
	}
	if got := headHits.Load(); got != 1 {
		t.Fatalf("HEAD hits = %d, want 1", got)
	}
}
