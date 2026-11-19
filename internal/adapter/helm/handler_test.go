package helm

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter"
	"depsilo/internal/adapter/adaptertest"
	"depsilo/internal/config"
	"depsilo/internal/quarantine"
	"depsilo/internal/upstream"
)

// helmMirror is an Upstream repository under /charts whose index declares
// absolute chart URLs on its own origin, as public mirrors do, plus one chart
// hosted elsewhere.
type helmMirror struct {
	server     *httptest.Server
	indexHits  atomic.Int64
	mu         sync.Mutex
	chartPaths []string
}

func newHelmMirror(t *testing.T) *helmMirror {
	t.Helper()
	mirror := &helmMirror{}
	old := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(http.TimeFormat)
	mirror.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/charts/index.yaml":
			mirror.indexHits.Add(1)
			w.Header().Set("Content-Type", "text/yaml")
			_, _ = io.WriteString(w, "apiVersion: v1\nentries:\n  demo:\n"+
				"  - digest: sha256:aaa\n    name: demo\n    urls:\n"+
				"    - "+mirror.server.URL+"/charts/demo-1.0.0.tgz\n    version: 1.0.0\n"+
				"  - digest: sha256:bbb\n    name: demo\n    urls:\n"+
				"    - https://elsewhere.example/demo-0.9.0.tgz\n    version: 0.9.0\n"+
				"generated: \"2026-11-19T00:00:00Z\"\n")
		case strings.HasSuffix(r.URL.Path, ".tgz"):
			w.Header().Set("Last-Modified", old)
			if r.Method == http.MethodGet {
				mirror.mu.Lock()
				mirror.chartPaths = append(mirror.chartPaths, r.URL.Path)
				mirror.mu.Unlock()
				_, _ = io.WriteString(w, "chart"+r.URL.Path)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(mirror.server.Close)
	return mirror
}

func (mirror *helmMirror) fetchedCharts() []string {
	mirror.mu.Lock()
	defer mirror.mu.Unlock()
	return append([]string(nil), mirror.chartPaths...)
}

func getHelm(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	return response
}

// helmChartURLs returns the block-list chart URLs of an index.
func helmChartURLs(index string) []string {
	var urls []string
	inURLs := false
	for _, line := range strings.Split(index, "\n") {
		switch trimmed := strings.TrimSpace(line); {
		case trimmed == "urls:":
			inURLs = true
		case inURLs && strings.HasPrefix(trimmed, "- "):
			urls = append(urls, strings.TrimPrefix(trimmed, "- "))
		default:
			inURLs = false
		}
	}
	return urls
}

// resolveLikeHelm resolves a chart URL against the repository URL a client
// added, the way Helm's repo.ResolveReferenceURL does.
func resolveLikeHelm(t *testing.T, repoURL, chartURL string) *url.URL {
	t.Helper()
	base, err := url.Parse(strings.TrimSuffix(repoURL, "/") + "/")
	if err != nil {
		t.Fatal(err)
	}
	reference, err := url.Parse(chartURL)
	if err != nil {
		t.Fatal(err)
	}
	return base.ResolveReference(reference)
}

func TestHelmIndexSendsChartDownloadsThroughTheProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mirror := newHelmMirror(t)
	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mirror", URL: mirror.server.URL + "/charts", Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(manager, upstream.NewPassiveRecoverySelector(pool), config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/helm"))
	handler.Register(router.Group("/p/team/helm"))

	for _, repo := range []string{"http://depsilo.test/helm", "http://depsilo.test/p/team/helm"} {
		repoPath := strings.TrimPrefix(repo, "http://depsilo.test")
		index := getHelm(t, router, repoPath+"/index.yaml")
		if index.Code != http.StatusOK {
			t.Fatalf("%s/index.yaml status=%d body=%s", repoPath, index.Code, index.Body.String())
		}
		urls := helmChartURLs(index.Body.String())
		if len(urls) != 2 || urls[0] != "demo-1.0.0.tgz" || urls[1] != "https://elsewhere.example/demo-0.9.0.tgz" {
			t.Fatalf("index chart URLs = %q, want the mirror's chart relative and the foreign one kept", urls)
		}
		if !strings.Contains(index.Body.String(), "digest: sha256:aaa\n") {
			t.Fatalf("index lost its digests:\n%s", index.Body.String())
		}

		download := resolveLikeHelm(t, repo, urls[0])
		if download.Host != "depsilo.test" {
			t.Fatalf("chart download %s leaves the proxy", download)
		}
		chart := getHelm(t, router, download.RequestURI())
		if chart.Code != http.StatusOK || chart.Body.String() != "chart/charts/demo-1.0.0.tgz" {
			t.Fatalf("GET %s status=%d body=%q", download.RequestURI(), chart.Code, chart.Body.String())
		}
	}
	if got := mirror.indexHits.Load(); got != 1 {
		t.Fatalf("upstream index fetches = %d, want one fill shared by both routes", got)
	}
	if got := mirror.fetchedCharts(); len(got) != 1 || got[0] != "/charts/demo-1.0.0.tgz" {
		t.Fatalf("upstream chart fetches = %q, want the cached chart fetched once", got)
	}
}

// The age gate reads index.yaml through the same cache entry clients get. It
// must fill that entry with the rewritten index, or a client arriving after a
// gated download would be sent straight to the Upstream.
func TestHelmIdentityLookupFillsTheRewrittenIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mirror := newHelmMirror(t)
	env := adaptertest.NewProvenanceEnv(t, mirror.server.URL+"/charts", "helm", adaptertest.ApproximateProvenance)
	handler := New(env.Manager, env.Selector, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour}, env.DB)
	handler.SetProvenanceRequired(true)
	router := gin.New()
	handler.Register(router.Group("/helm"))
	scoped := adapter.NewRequestScope(nil, nil, quarantine.Wrap(env.Checker), nil).Wrap(router)

	chart := getHelm(t, scoped, "/helm/demo-1.0.0.tgz")
	if chart.Code != http.StatusOK || chart.Body.String() != "chart/charts/demo-1.0.0.tgz" {
		t.Fatalf("gated chart status=%d body=%s", chart.Code, chart.Body.String())
	}
	index := getHelm(t, scoped, "/helm/index.yaml")
	if index.Code != http.StatusOK {
		t.Fatalf("index.yaml status=%d body=%s", index.Code, index.Body.String())
	}
	if urls := helmChartURLs(index.Body.String()); len(urls) != 2 || urls[0] != "demo-1.0.0.tgz" {
		t.Fatalf("index chart URLs after the gate filled the cache = %q", urls)
	}
	if got := mirror.indexHits.Load(); got != 1 {
		t.Fatalf("upstream index fetches = %d, want the gate's fill to serve the client", got)
	}
}

// A pool can join different repositories, as the default Helm pool does. When
// the primary fails an index fetch, the cached index is the secondary's, whose
// chart URLs are now relative; once the primary answers again it is selected
// for the chart and does not have it. The download must reach the member whose
// index named the chart instead of failing with 404.
func TestHelmChartFromAFailedOverIndexReachesTheMemberThatListedIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var primaryDown atomic.Bool
	primaryDown.Store(true)
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.yaml" && primaryDown.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/index.yaml" {
			_, _ = io.WriteString(w, "apiVersion: v1\nentries: {}\n")
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(primary.Close)
	var secondary *httptest.Server
	secondary = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.yaml":
			_, _ = io.WriteString(w, "apiVersion: v1\nentries:\n  other:\n  - name: other\n    urls:\n"+
				"    - "+secondary.URL+"/other-2.0.0.tgz\n    version: 2.0.0\n")
		case "/other-2.0.0.tgz":
			_, _ = io.WriteString(w, "secondary chart")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(secondary.Close)

	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "primary", URL: primary.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "secondary", URL: secondary.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(manager, upstream.NewPassiveRecoverySelector(pool), config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/helm"))

	index := getHelm(t, router, "/helm/index.yaml")
	if urls := helmChartURLs(index.Body.String()); index.Code != http.StatusOK || len(urls) != 1 || urls[0] != "other-2.0.0.tgz" {
		t.Fatalf("failed-over index status=%d urls=%q body=%s", index.Code, urls, index.Body.String())
	}
	// The primary recovers while the index is still fresh.
	primaryDown.Store(false)
	recovered := pool.Snapshot()[0]
	for attempt := 0; !recovered.IsHealthy() && attempt < 10; attempt++ {
		recovered.Report(time.Millisecond, true)
	}
	if recovered.Name != "primary" || !recovered.IsHealthy() {
		t.Fatalf("%s did not recover", recovered.Name)
	}

	chart := getHelm(t, router, resolveLikeHelm(t, "http://depsilo.test/helm", "other-2.0.0.tgz").RequestURI())
	if chart.Code != http.StatusOK || chart.Body.String() != "secondary chart" {
		t.Fatalf("chart from the failed-over index status=%d body=%q", chart.Code, chart.Body.String())
	}
}
