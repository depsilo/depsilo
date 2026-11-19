package cargo

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/adapter/adaptertest"
	"depsilo/internal/adapter/packagekey"
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
	handler.Register(router.Group("/crates"))
	for _, target := range []string{
		"/crates/se/rd/serde",
		"/crates/api/v1/crates/serde/1.0.219/download",
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
		if row.AdapterType != "cargo" || row.Hit || row.StatusCode != http.StatusBadGateway {
			t.Errorf("access row %d = %#v", index, row)
		}
	}
	if db.ClassifyCacheKind("cargo", rows[0].CacheKey) != db.CacheKindMetadata || rows[0].PackageName != "" {
		t.Errorf("index access row = %#v", rows[0])
	}
	if db.ClassifyCacheKind("cargo", rows[1].CacheKey) != db.CacheKindArtifact || rows[1].PackageName != "serde" ||
		packagekey.ExtractVersion("cargo", rows[1].CacheKey) != "1.0.219" {
		t.Errorf("download access row = %#v", rows[1])
	}
}

// X-Forwarded-Proto reaches the download URL written into config.json. Two
// chained proxies send a list, and any client can send anything, so the value
// must never become the URL scheme as it is.
func TestConfigDownloadURLTakesOnlyAnHTTPSchemeFromForwardedProto(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"dl":"https://static.crates.io/crates","api":"https://crates.io"}`)
	}))
	t.Cleanup(upstreamServer.Close)
	handler, _ := newCargoTestHandler(t, upstreamServer.URL, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour})

	for _, test := range []struct {
		forwarded string
		want      string
	}{
		{forwarded: "https, http", want: "https://depsilo.example.test/crates/api/v1/crates"},
		{forwarded: "gopher", want: "http://depsilo.example.test/crates/api/v1/crates"},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/crates/config.json", nil)
		request.Host = "depsilo.example.test"
		request.Header.Set("X-Forwarded-Proto", test.forwarded)
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("X-Forwarded-Proto %q: status = %d, body=%s", test.forwarded, response.Code, response.Body.String())
		}
		var document struct {
			DL string `json:"dl"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
			t.Fatalf("X-Forwarded-Proto %q: decode config.json: %v", test.forwarded, err)
		}
		if document.DL != test.want {
			t.Errorf("X-Forwarded-Proto %q: dl = %q, want %q", test.forwarded, document.DL, test.want)
		}
	}
}

func newPoolCargoRouter(t *testing.T, selector upstream.Selector) (http.Handler, *gorm.DB) {
	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	handler := New(manager, selector, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: 72 * time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/crates"))
	return router, database
}

// Cargo reads a 404 or 410 for an index file as "no such crate"; a 502 makes
// it report a registry outage instead.
func TestIndexReportsThePoolAnswer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptertest.CheckPoolAnswers(t, adaptertest.MetadataPoolAnswers, "/crates/se/rd/serde", `{"name":"serde","vers":"1.0.0"}`, newPoolCargoRouter)
	adaptertest.CheckPoolAnswers(t, adaptertest.MetadataPoolAnswers, "/crates/config.json", `{"dl":"https://static.crates.io/crates"}`, newPoolCargoRouter)
}

// A .crate is immutable and Cargo checks it against the index checksum, so a
// member that lacks it is passed over like an unavailable one.
func TestDownloadMovesOnFromAMemberThatLacksTheCrate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptertest.CheckPoolAnswers(t, []adaptertest.PoolAnswer{
		{Name: "missing on one member", First: http.StatusNotFound, Second: http.StatusOK, Status: http.StatusOK, SecondAsked: true},
		{Name: "missing everywhere", First: http.StatusNotFound, Second: http.StatusNotFound, Status: http.StatusNotFound, Code: "NOT_FOUND", SecondAsked: true},
		{Name: "gone", First: http.StatusGone, Second: http.StatusOK, Status: http.StatusGone, Code: "GONE"},
		{Name: "failing member", First: http.StatusInternalServerError, Second: http.StatusOK, Status: http.StatusOK, SecondAsked: true},
		{Name: "failing pool", First: http.StatusInternalServerError, Second: http.StatusBadGateway, Status: http.StatusBadGateway, Code: "UPSTREAM_UNAVAILABLE", SecondAsked: true},
	}, "/crates/api/v1/crates/serde/1.0.0/download", "crate bytes", newPoolCargoRouter)
}

// An index file cached before the pool began answering 404 is still served
// from that copy once it is stale, as the transparent proxies serve stale
// metadata: only content whose presence is the answer withdraws its copy.
func TestStaleIndexCoversALater404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const body = `{"name":"serde","vers":"1.0.0"}`
	var missing atomic.Bool
	var requests atomic.Int32
	origin := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		if missing.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, body)
	})
	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	handler := New(manager, adaptertest.NewPoolSelector(t, origin), config.CacheConfig{TTLIndex: 0, TTLBlob: time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/crates"))
	request := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/crates/se/rd/serde", nil))
		return response
	}

	if response := request(); response.Code != http.StatusOK {
		t.Fatalf("first request status = %d, body=%s", response.Code, response.Body.String())
	}
	for deadline := time.Now().Add(time.Second); ; time.Sleep(2 * time.Millisecond) {
		var count int64
		if err := database.Model(&db.CacheEntry{}).Where("key = ?", IndexCacheKey("se/rd", "serde")).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("index entry was not committed")
		}
	}
	missing.Store(true)
	response := request()
	if response.Code != http.StatusOK || response.Body.String() != body {
		t.Fatalf("stale request status = %d, body=%s", response.Code, response.Body.String())
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("origin requests = %d, want the stale entry refreshed once", got)
	}
}
