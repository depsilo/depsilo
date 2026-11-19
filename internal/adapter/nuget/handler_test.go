package nuget

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter"
	"depsilo/internal/adapter/adaptertest"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

func TestPackageUpstreamFailureIsRecordedAsAuditError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	audit := &adaptertest.AuditCapture{}
	scope := adapter.NewRequestScope(nil, audit, nil, nil)

	handler := New(manager, adaptertest.UnavailableSelector{}, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: 72 * time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/nuget"))
	response := httptest.NewRecorder()
	target := "/nuget/v3-flatcontainer/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg"
	request := httptest.NewRequest(http.MethodGet, target, nil)
	scope.Wrap(router).ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("GET %s status = %d, body=%s", target, response.Code, response.Body.String())
	}
	if len(audit.Entries) != 1 {
		t.Fatalf("audit entries = %#v", audit.Entries)
	}
	entry := audit.Entries[0]
	if entry.Ecosystem != "nuget" || entry.PackageName != "newtonsoft.json" || entry.Version != "13.0.3" ||
		entry.Action != "download" || entry.CacheResult != "error" || entry.StatusCode != http.StatusBadGateway {
		t.Fatalf("audit entry = %#v", entry)
	}
}

func TestPassthroughPreservesRawQueryAndIsolatesCachedResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var requests atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"query":%q}`, request.URL.RawQuery)
	}))
	t.Cleanup(upstreamServer.Close)

	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(
		manager,
		upstream.NewPassiveRecoverySelector(pool),
		config.CacheConfig{TTLIndex: time.Hour, TTLBlob: 72 * time.Hour},
		database,
	)
	router := gin.New()
	handler.Register(router.Group("/nuget"))

	requestQuery := func(rawQuery string) string {
		t.Helper()
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/nuget/v3/search?"+rawQuery, nil)
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("query %q status = %d, body=%s", rawQuery, response.Code, response.Body.String())
		}
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}

	const first = "q=A%2FB+tool&packageType=DotnetTool&packageType=Template&skip=20"
	const second = "q=Other&skip=0&take=10"
	if got := requestQuery(first); got != `{"query":"`+first+`"}` {
		t.Fatalf("first query response = %s", got)
	}
	if got := requestQuery(second); got != `{"query":"`+second+`"}` {
		t.Fatalf("second query response = %s", got)
	}
	if got := requestQuery(first); got != `{"query":"`+first+`"}` {
		t.Fatalf("cached first query response = %s", got)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("upstream requests = %d, want 2 distinct query variants", got)
	}
}

// X-Forwarded-Proto reaches every resource URL written into the service
// index. Two chained proxies send a list, and any client can send anything, so
// the value must never become the URL scheme as it is.
func TestServiceIndexURLsTakeOnlyAnHTTPSchemeFromForwardedProto(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/index.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"version":"3.0.0","resources":[`+
			`{"@id":"https://api.nuget.org/v3-flatcontainer/","@type":"PackageBaseAddress/3.0.0"},`+
			`{"@id":"https://api.nuget.org/v3/registration5-gz-semver2/","@type":"RegistrationsBaseUrl/3.6.0"}]}`)
	}))
	t.Cleanup(upstreamServer.Close)
	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(
		manager,
		upstream.NewPassiveRecoverySelector(pool),
		config.CacheConfig{TTLIndex: time.Hour, TTLBlob: 72 * time.Hour},
		database,
	)
	router := gin.New()
	handler.Register(router.Group("/nuget"))

	for _, test := range []struct {
		forwarded string
		want      string
	}{
		{forwarded: "https, http", want: "https://depsilo.example.test/nuget/"},
		{forwarded: "gopher", want: "http://depsilo.example.test/nuget/"},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/nuget/v3/index.json", nil)
		request.Host = "depsilo.example.test"
		request.Header.Set("X-Forwarded-Proto", test.forwarded)
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("X-Forwarded-Proto %q: status = %d, body=%s", test.forwarded, response.Code, response.Body.String())
		}
		var document struct {
			Resources []struct {
				ID string `json:"@id"`
			} `json:"resources"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
			t.Fatalf("X-Forwarded-Proto %q: decode service index: %v", test.forwarded, err)
		}
		if len(document.Resources) != 2 {
			t.Fatalf("X-Forwarded-Proto %q: resources = %#v, want the fixture's 2", test.forwarded, document.Resources)
		}
		for _, resource := range document.Resources {
			if !strings.HasPrefix(resource.ID, test.want) {
				t.Errorf("X-Forwarded-Proto %q: @id = %q, want prefix %q", test.forwarded, resource.ID, test.want)
			}
		}
	}
}

func newPoolNugetRouter(t *testing.T, selector upstream.Selector) http.Handler {
	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	handler := New(manager, selector, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: 72 * time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/nuget"))
	return router
}

// NuGet reads a 404 as "not on this source" and moves on to the next package
// source; a 502 fails the whole restore.
func TestRoutesReportThePoolAnswer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, target := range []string{
		"/nuget/v3-flatcontainer/newtonsoft.json/index.json",
		"/nuget/v3-flatcontainer/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg",
	} {
		adaptertest.CheckPoolAnswers(t, adaptertest.MetadataPoolAnswers, target, `{"versions":[]}`, newPoolNugetRouter)
	}
	adaptertest.CheckPoolAnswers(t, adaptertest.MetadataPoolAnswers, "/nuget/v3/index.json", `{"version":"3.0.0","resources":[]}`, newPoolNugetRouter)
}
