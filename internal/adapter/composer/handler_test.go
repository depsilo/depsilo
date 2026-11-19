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
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/adapter/adaptertest"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

func TestPackageUpstreamFailuresAreRecordedInTheAccessLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	scope := adapter.NewRequestScope(nil, nil, nil, nil)

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

	rows := adaptertest.AccessRows(t, database)
	if len(rows) != 2 {
		t.Fatalf("access rows = %#v", rows)
	}
	for index, row := range rows {
		if row.AdapterType != "composer" || row.PackageName != "monolog/monolog" || row.Hit || row.StatusCode != http.StatusBadGateway {
			t.Errorf("access row %d = %#v", index, row)
		}
	}
	if db.ClassifyCacheKind("composer", rows[0].CacheKey) != db.CacheKindMetadata {
		t.Errorf("metadata access row = %#v", rows[0])
	}
	if db.ClassifyCacheKind("composer", rows[1].CacheKey) != db.CacheKindArtifact {
		t.Errorf("dist access row = %#v", rows[1])
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

func newPoolComposerRouter(t *testing.T, selector upstream.Selector) (http.Handler, *gorm.DB) {
	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	handler := New(manager, selector, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: 72 * time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/composer"))
	return router, database
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
