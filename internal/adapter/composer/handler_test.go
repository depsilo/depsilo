package composer

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter"
	"depsilo/internal/adapter/adaptertest"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

func TestPackageUpstreamFailuresAreRecordedAsAuditErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	audit := &adaptertest.AuditCapture{}
	scope := adapter.NewRequestScope(nil, audit, nil, nil)

	handler := New(manager, adaptertest.UnavailableSelector{}, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: 72 * time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/composer"))
	for _, target := range []string{
		"/composer/p2/monolog/monolog.json",
		"/composer/dist/monolog/monolog/3.8.1.0/abcdef.zip",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		scope.Wrap(router).ServeHTTP(response, request)
		if response.Code != http.StatusBadGateway {
			t.Fatalf("GET %s status = %d, body=%s", target, response.Code, response.Body.String())
		}
	}

	if len(audit.Entries) != 2 {
		t.Fatalf("audit entries = %#v", audit.Entries)
	}
	for index, entry := range audit.Entries {
		if entry.Ecosystem != "composer" || entry.PackageName != "monolog/monolog" || entry.CacheResult != "error" || entry.StatusCode != http.StatusBadGateway {
			t.Errorf("audit entry %d = %#v", index, entry)
		}
	}
	if audit.Entries[0].Action != "metadata" {
		t.Errorf("metadata audit entry = %#v", audit.Entries[0])
	}
	if audit.Entries[1].Action != "download" {
		t.Errorf("dist audit entry = %#v", audit.Entries[1])
	}
}

// X-Forwarded-Proto reaches the metadata and mirror URLs written into
// packages.json. Two chained proxies send a list, and any client can send
// anything, so the value must never become the URL scheme as it is.
func TestPackagesJSONURLsTakeOnlyAnHTTPSchemeFromForwardedProto(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/packages.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"metadata-url":"/p2/%package%.json","packages":[]}`)
	}))
	t.Cleanup(upstreamServer.Close)
	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(manager, upstream.NewPassiveRecoverySelector(pool), config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/composer"))

	for _, test := range []struct {
		forwarded string
		want      string
	}{
		{forwarded: "https, http", want: "https://depsilo.example.test/composer/"},
		{forwarded: "gopher", want: "http://depsilo.example.test/composer/"},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/composer/packages.json", nil)
		request.Host = "depsilo.example.test"
		request.Header.Set("X-Forwarded-Proto", test.forwarded)
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("X-Forwarded-Proto %q: status = %d, body=%s", test.forwarded, response.Code, response.Body.String())
		}
		var document struct {
			MetadataURL string `json:"metadata-url"`
			Mirrors     []struct {
				DistURL string `json:"dist-url"`
			} `json:"mirrors"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
			t.Fatalf("X-Forwarded-Proto %q: decode packages.json: %v", test.forwarded, err)
		}
		if document.MetadataURL != test.want+"p2/%package%.json" {
			t.Errorf("X-Forwarded-Proto %q: metadata-url = %q, want prefix %q", test.forwarded, document.MetadataURL, test.want)
		}
		if len(document.Mirrors) != 1 || !strings.HasPrefix(document.Mirrors[0].DistURL, test.want+"dist/") {
			t.Errorf("X-Forwarded-Proto %q: mirrors = %#v, want one dist-url under %q", test.forwarded, document.Mirrors, test.want)
		}
	}
}

func newPoolComposerRouter(t *testing.T, selector upstream.Selector) http.Handler {
	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	handler := New(manager, selector, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: 72 * time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/composer"))
	return router
}

// Composer reads a 404 for package metadata as "no such package"; a 502 makes
// it report a repository outage instead.
func TestMetadataReportsThePoolAnswer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptertest.CheckPoolAnswers(t, adaptertest.MetadataPoolAnswers, "/composer/p2/monolog/monolog.json", `{"packages":{}}`, newPoolComposerRouter)
	adaptertest.CheckPoolAnswers(t, adaptertest.MetadataPoolAnswers, "/composer/packages.json", `{"packages":[]}`, newPoolComposerRouter)
}

// A dist for a package whose metadata the pool reports missing is missing too.
func TestDistOfAMissingPackageIsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptertest.CheckPoolAnswers(t, adaptertest.MetadataPoolAnswers[:1], "/composer/dist/monolog/monolog/3.8.1.0/abcdef.zip", `{"packages":{}}`, newPoolComposerRouter)
}
