package pypi

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
	"depsilo/internal/upstream"
)

// recordingArtifactChecker captures the identity the adapter passes to the
// quarantine gate and can be switched into "block" mode.
type recordingArtifactChecker struct {
	calls     []artifactIdentity
	ecosystem []string
	block     bool
}

func (checker *recordingArtifactChecker) Check(
	_ context.Context,
	ecosystem, pkg, version, _ string,
) adapter.QuarantineDecision {
	checker.calls = append(checker.calls, artifactIdentity{pkg: pkg, version: version})
	checker.ecosystem = append(checker.ecosystem, ecosystem)
	if checker.block {
		return adapter.QuarantineDecision{Allowed: false, Code: "MALICIOUS_BLOCKED", Reason: "blocked by test dataset"}
	}
	return adapter.QuarantineDecision{Allowed: true}
}

const pypiIdentityJSONIndex = `{
  "meta": {"api-version": "1.0"},
  "name": "acme-demo",
  "files": [
    {"filename": "acme-demo-1.0.0.zip", "url": "https://files.example.com/packages/ab/cd/acme-demo-1.0.0.zip", "hashes": {}},
    {"filename": "acme-demo-1.0.0-2.zip", "url": "https://files.example.com/packages/ab/cd/acme-demo-1.0.0-2.zip", "hashes": {}}
  ]
}`

const pypiAmbiguousIndex = `{
  "files": [
    {"filename": "acme-demo-1.0.0-2.zip", "url": "https://files.example.com/packages/ab/cd/acme-demo-1.0.0-2.zip"}
  ]
}`

const pypiIdentityHTMLIndex = `<!DOCTYPE html>
<html><body>
<h1>Links for zope.interface</h1>
<a href="../../packages/ab/cd/zope.interface-4.0.0.zip#sha256=abc">zope.interface-4.0.0.zip</a><br/>
</body></html>`

func pypiIdentityFixture(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var indexHits atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/simple/acme-demo/":
			indexHits.Add(1)
			w.Header().Set("Content-Type", "application/vnd.pypi.simple.v1+json")
			_, _ = io.WriteString(w, pypiIdentityJSONIndex)
		case r.URL.Path == "/simple/acme-demo-1.0.0/":
			indexHits.Add(1)
			w.Header().Set("Content-Type", "application/vnd.pypi.simple.v1+json")
			_, _ = io.WriteString(w, pypiAmbiguousIndex)
		case r.URL.Path == "/simple/zope.interface/", r.URL.Path == "/simple/zope-interface/":
			indexHits.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, pypiIdentityHTMLIndex)
		case strings.HasPrefix(r.URL.Path, "/packages/"):
			_, _ = io.WriteString(w, "pypi-bytes:"+lastPathSegment(r.URL.Path))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstreamServer.Close)
	return upstreamServer, &indexHits
}

func newIdentityTestHandler(t *testing.T, upstreamURL string) (*Handler, http.Handler, *recordingArtifactChecker) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "pypi-identity.db"))
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
	handler, err := NewWithOptions(manager, upstream.NewPassiveRecoverySelector(pool), config.CacheConfig{
		TTLIndex: time.Hour,
		TTLBlob:  time.Hour,
	}, database, Options{PathPrefix: "/pypi", AdapterID: "pypi"})
	if err != nil {
		t.Fatal(err)
	}
	checker := &recordingArtifactChecker{}
	router := gin.New()
	handler.Register(router.Group("/pypi"))
	return handler, adapter.NewRequestScope(nil, nil, checker, nil).Wrap(router), checker
}

func requestPypiFile(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/pypi/files/"+path, nil))
	return response
}

func TestPypiLegacyArtifactResolvesIdentityFromJSONIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, indexHits := pypiIdentityFixture(t)
	handler, scoped, checker := newIdentityTestHandler(t, upstreamServer.URL)
	handler.SetBlocklistCovered(true)

	response := requestPypiFile(t, scoped, "packages/ab/cd/acme-demo-1.0.0.zip")
	if response.Code != http.StatusOK || response.Body.String() != "pypi-bytes:acme-demo-1.0.0.zip" {
		t.Fatalf("legacy zip status=%d body=%q", response.Code, response.Body.String())
	}
	if len(checker.calls) != 1 || checker.calls[0] != (artifactIdentity{pkg: "acme-demo", version: "1.0.0"}) {
		t.Fatalf("gate identities = %+v", checker.calls)
	}
	if got := indexHits.Load(); got != 1 {
		t.Fatalf("index fetches = %d, want 1", got)
	}
}

func TestPypiLegacyArtifactResolvesIdentityFromHTMLIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, _ := pypiIdentityFixture(t)
	handler, scoped, checker := newIdentityTestHandler(t, upstreamServer.URL)
	handler.SetBlocklistCovered(true)

	response := requestPypiFile(t, scoped, "packages/ab/cd/zope.interface-4.0.0.zip")
	if response.Code != http.StatusOK {
		t.Fatalf("legacy HTML zip status=%d body=%s", response.Code, response.Body.String())
	}
	// The index page is addressed by the normalized name, but the reported
	// identity keeps the declared coordinate used by the dataset rows.
	if len(checker.calls) != 1 || checker.calls[0] != (artifactIdentity{pkg: "zope-interface", version: "4.0.0"}) {
		t.Fatalf("gate identities = %+v", checker.calls)
	}
}

func TestPypiAmbiguousLegacyFilenameIsRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, _ := pypiIdentityFixture(t)
	handler, scoped, checker := newIdentityTestHandler(t, upstreamServer.URL)
	handler.SetBlocklistCovered(true)

	response := requestPypiFile(t, scoped, "packages/ab/cd/acme-demo-1.0.0-2.zip")
	if response.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(response.Body.String(), `"code":"QUARANTINED"`) {
		t.Fatalf("ambiguous status=%d body=%s", response.Code, response.Body.String())
	}
	if len(checker.calls) != 0 {
		t.Fatalf("gate identities = %+v, want none", checker.calls)
	}
}

func TestPypiUndeclaredLegacyFilenameIsRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, _ := pypiIdentityFixture(t)
	handler, scoped, checker := newIdentityTestHandler(t, upstreamServer.URL)
	handler.SetBlocklistCovered(true)

	response := requestPypiFile(t, scoped, "packages/ab/cd/rogue-1.0.0.zip")
	if response.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(response.Body.String(), "identity is unavailable") {
		t.Fatalf("undeclared status=%d body=%s", response.Code, response.Body.String())
	}
	if len(checker.calls) != 0 {
		t.Fatalf("gate identities = %+v, want none", checker.calls)
	}
}

func TestPypiStrictWheelIdentitySkipsTheIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, indexHits := pypiIdentityFixture(t)
	handler, scoped, checker := newIdentityTestHandler(t, upstreamServer.URL)
	handler.SetBlocklistCovered(true)

	response := requestPypiFile(t, scoped, "packages/ab/cd/acme_demo-1.0.0-py3-none-any.whl")
	if response.Code != http.StatusOK {
		t.Fatalf("wheel status=%d body=%s", response.Code, response.Body.String())
	}
	if len(checker.calls) != 1 || checker.calls[0] != (artifactIdentity{pkg: "acme_demo", version: "1.0.0"}) {
		t.Fatalf("gate identities = %+v", checker.calls)
	}
	if got := indexHits.Load(); got != 0 {
		t.Fatalf("index fetches = %d, want 0", got)
	}
}

func TestPypiLegacyArtifactWithoutCoverageKeepsServing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, indexHits := pypiIdentityFixture(t)
	_, scoped, checker := newIdentityTestHandler(t, upstreamServer.URL)

	response := requestPypiFile(t, scoped, "packages/ab/cd/acme-demo-1.0.0.zip")
	if response.Code != http.StatusOK {
		t.Fatalf("uncovered legacy status=%d body=%s", response.Code, response.Body.String())
	}
	if len(checker.calls) != 0 {
		t.Fatalf("gate identities = %+v, want none", checker.calls)
	}
	if got := indexHits.Load(); got != 0 {
		t.Fatalf("index fetches = %d, want 0", got)
	}
}

func TestPypiDatasetBlockStopsLegacyArtifact(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, _ := pypiIdentityFixture(t)
	handler, scoped, checker := newIdentityTestHandler(t, upstreamServer.URL)
	handler.SetBlocklistCovered(true)
	checker.block = true

	response := requestPypiFile(t, scoped, "packages/ab/cd/acme-demo-1.0.0.zip")
	if response.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(response.Body.String(), `"code":"MALICIOUS_BLOCKED"`) {
		t.Fatalf("blocked status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSimpleIndexDeclaresFileShape(t *testing.T) {
	t.Parallel()
	jsonBody := []byte(`{"files":[{"filename":"pkg-1.0.0.zip","url":"https://x/packages/pkg-1.0.0.zip"}]}`)
	declared, err := simpleIndexDeclaresFile(jsonBody, "application/vnd.pypi.simple.v1+json", "pkg-1.0.0.zip")
	if err != nil || !declared {
		t.Fatalf("json declared=%v err=%v", declared, err)
	}
	declared, err = simpleIndexDeclaresFile(jsonBody, "", "pkg-1.0.0.zip")
	if err != nil || !declared {
		t.Fatalf("sniffed json declared=%v err=%v", declared, err)
	}
	htmlBody := []byte(`<a href="../../packages/ab/cd/pkg-1.0.0.tar.gz#sha256=abc">pkg-1.0.0.tar.gz</a>`)
	declared, err = simpleIndexDeclaresFile(htmlBody, "text/html", "pkg-1.0.0.tar.gz")
	if err != nil || !declared {
		t.Fatalf("html declared=%v err=%v", declared, err)
	}
	declared, err = simpleIndexDeclaresFile(htmlBody, "text/html", "other-1.0.0.tar.gz")
	if err != nil || declared {
		t.Fatalf("html other declared=%v err=%v", declared, err)
	}
}
