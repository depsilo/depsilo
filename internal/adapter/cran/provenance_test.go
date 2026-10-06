package cran

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

func TestIsCurrentSourcePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path         string
		pkg, version string
		want         bool
	}{
		{path: "src/contrib/acme-demo_1.0.0.tar.gz", pkg: "acme-demo", version: "1.0.0", want: true},
		{path: "src/contrib/Archive/acme-demo/acme-demo_1.0.0.tar.gz", pkg: "acme-demo", version: "1.0.0"},
		{path: "bin/windows/contrib/4.5/acme-demo_1.0.0.zip", pkg: "acme-demo", version: "1.0.0"},
		{path: "src/contrib/acme-demo_2.0.0.tar.gz", pkg: "acme-demo", version: "1.0.0"},
	}
	for _, testCase := range cases {
		if got := isCurrentSourcePath(testCase.path, testCase.pkg, testCase.version); got != testCase.want {
			t.Errorf("isCurrentSourcePath(%q) = %v, want %v", testCase.path, got, testCase.want)
		}
	}
}

func TestParseCranPublished(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"2026-09-01 12:30:45 UTC", "2026-09-01 12:30:45", "2026-09-01"} {
		if _, err := parseCranPublished(raw); err != nil {
			t.Errorf("parseCranPublished(%q): %v", raw, err)
		}
	}
	if _, err := parseCranPublished("not-a-date"); err == nil {
		t.Fatal("invalid date was accepted")
	}
}

func cranTestFixture(t *testing.T, now time.Time) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var headHits atomic.Int64
	young := now.Add(-time.Hour).UTC().Format(http.TimeFormat)
	old := now.Add(-30 * 24 * time.Hour).UTC().Format(http.TimeFormat)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/web/packages/acme-demo/DESCRIPTION":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "Type: Package\nPackage: acme-demo\nVersion: 1.0.0\nDate/Publication: "+
				now.Add(-time.Hour).UTC().Format("2006-01-02 15:04:05")+" UTC\n")
			return
		case "/src/contrib/acme-demo_1.0.0.tar.gz":
			w.Header().Set("Last-Modified", old)
		case "/src/contrib/acme-demo_2.0.0.tar.gz":
			w.Header().Set("Last-Modified", old)
		case "/src/contrib/acme-demo_3.0.0.tar.gz":
			// No Last-Modified: the resolver must fail closed.
		case "/src/contrib/Archive/acme-demo/acme-demo_0.9.0.tar.gz":
			w.Header().Set("Last-Modified", old)
		case "/bin/windows/contrib/4.5/acme-demo_1.0.0.zip":
			w.Header().Set("Last-Modified", young)
		default:
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodHead {
			headHits.Add(1)
			return
		}
		_, _ = io.WriteString(w, "cran-bytes")
	}))
	t.Cleanup(upstreamServer.Close)
	return upstreamServer, &headHits
}

func newCranTestHandler(t *testing.T, upstreamURL string) http.Handler {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "cran-provenance.db"))
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
	handler.Register(router.Group("/cran"))

	enabled := true
	policy, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"cran": "168h"},
		ApproximateSources:   []string{"cran"},
	}, nil)
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance: %v", err)
	}
	store := quarantine.NewStore(database)
	checker, err := quarantine.NewChecker(policy, quarantine.NewLookup(store, nil), store)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	return adapter.NewRequestScope(nil, nil, quarantine.Wrap(checker), nil).Wrap(router)
}

func requestCranArtifact(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/cran/"+path, nil))
	return response
}

func TestCranArtifactUsesDescriptionAndLastModified(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, _ := cranTestFixture(t, time.Now().UTC())
	scoped := newCranTestHandler(t, upstreamServer.URL)

	// Current source version: DESCRIPTION says it is 1 hour old, while the
	// tarball's Last-Modified is 30 days old. The exact DESCRIPTION timestamp
	// must win and block.
	current := requestCranArtifact(t, scoped, "src/contrib/acme-demo_1.0.0.tar.gz")
	if current.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(current.Body.String(), `"code":"QUARANTINED"`) {
		t.Fatalf("current source status=%d body=%s", current.Code, current.Body.String())
	}

	// Archive version: no matching DESCRIPTION version, so the 30-day-old
	// Last-Modified is used and the artifact is served.
	archive := requestCranArtifact(t, scoped, "src/contrib/Archive/acme-demo/acme-demo_0.9.0.tar.gz")
	if archive.Code != http.StatusOK || archive.Body.String() != "cran-bytes" {
		t.Fatalf("archive status=%d body=%q", archive.Code, archive.Body.String())
	}

	// Binary artifact of the current version must not reuse the source
	// DESCRIPTION date; its own young Last-Modified blocks.
	binary := requestCranArtifact(t, scoped, "bin/windows/contrib/4.5/acme-demo_1.0.0.zip")
	if binary.Code != http.StatusUnavailableForLegalReasons {
		t.Fatalf("binary status=%d body=%s", binary.Code, binary.Body.String())
	}

	// A newer source version that the DESCRIPTION does not describe falls back
	// to the artifact's old Last-Modified.
	fallback := requestCranArtifact(t, scoped, "src/contrib/acme-demo_2.0.0.tar.gz")
	if fallback.Code != http.StatusOK {
		t.Fatalf("fallback status=%d body=%s", fallback.Code, fallback.Body.String())
	}

	// No usable timestamp anywhere: fail closed.
	missing := requestCranArtifact(t, scoped, "src/contrib/acme-demo_3.0.0.tar.gz")
	if missing.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(missing.Body.String(), "source-bound publish time") {
		t.Fatalf("missing provenance status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestCranProvenanceMemoizesHeadLookups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, headHits := cranTestFixture(t, time.Now().UTC())
	scoped := newCranTestHandler(t, upstreamServer.URL)

	for attempt := 0; attempt < 2; attempt++ {
		response := requestCranArtifact(t, scoped, "src/contrib/Archive/acme-demo/acme-demo_0.9.0.tar.gz")
		if response.Code != http.StatusOK {
			t.Fatalf("attempt %d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	if got := headHits.Load(); got != 1 {
		t.Fatalf("HEAD hits = %d, want 1", got)
	}
}
