package adapter_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/adapter"
	"depsilo/internal/adapter/adaptertest"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestTransparentProxyOwnsFetchCacheStreamAndAccessLogPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamCalls atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstreamCalls.Add(1)
		if request.URL.Path != "/packages/example.bin" {
			t.Errorf("upstream path = %q, want /packages/example.bin", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/x-example")
		writer.Header().Set("ETag", `"artifact-v1"`)
		writer.Header().Set("Last-Modified", "Sat, 01 Aug 2026 08:00:00 GMT")
		_, _ = writer.Write([]byte("artifact"))
	}))
	t.Cleanup(upstreamServer.Close)

	manager, database, _ := adaptertest.NewManager(t, time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "fixture", URL: upstreamServer.URL, Priority: 1}})
	if err != nil {
		t.Fatalf("create upstream pool: %v", err)
	}
	proxy := adapter.NewTransparentProxy("fixture", manager, upstream.NewPassiveRecoverySelector(pool), database)
	engine := gin.New()
	engine.GET("/fixture/*path", func(c *gin.Context) {
		proxy.Serve(c, adapter.TransparentPlan{Path: "packages/example.bin", CacheKey: "fixture/packages/example.bin", TTL: time.Hour})
	})

	for requestNumber := 1; requestNumber <= 2; requestNumber++ {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/fixture/packages/example.bin", nil))
		if response.Code != http.StatusOK || response.Body.String() != "artifact" {
			t.Fatalf("request %d response = %d %q", requestNumber, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Content-Type"); got != "application/x-example" {
			t.Fatalf("request %d content type = %q", requestNumber, got)
		}
		if got := response.Header().Get("ETag"); got != `"artifact-v1"` {
			t.Fatalf("request %d ETag = %q", requestNumber, got)
		}
		if got := response.Header().Get("Last-Modified"); got != "Sat, 01 Aug 2026 08:00:00 GMT" {
			t.Fatalf("request %d Last-Modified = %q", requestNumber, got)
		}
	}
	if calls := upstreamCalls.Load(); calls != 1 {
		t.Fatalf("upstream calls = %d, want one miss followed by one hit", calls)
	}

	var logs []db.AccessLog
	if err := database.Order("id ASC").Find(&logs).Error; err != nil {
		t.Fatalf("read access logs: %v", err)
	}
	if len(logs) != 2 || logs[0].Hit || !logs[1].Hit {
		t.Fatalf("access logs = %+v, want miss then hit", logs)
	}
	for _, log := range logs {
		if log.DeliveryResult != "completed" {
			t.Fatalf("successful transfer delivery = %q", log.DeliveryResult)
		}
	}

	// A 200 header and a cache hit do not prove a completed client download.
	interrupted := &disconnectedWriter{ResponseRecorder: httptest.NewRecorder()}
	engine.ServeHTTP(interrupted, httptest.NewRequest(http.MethodGet, "/fixture/packages/example.bin", nil))
	var failed db.AccessLog
	if err := database.Order("id DESC").First(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if failed.DeliveryResult != "failed" {
		t.Fatalf("interrupted cache hit delivery = %q", failed.DeliveryResult)
	}
}

type disconnectedWriter struct {
	*httptest.ResponseRecorder
}

func (writer *disconnectedWriter) Write(body []byte) (int, error) {
	written, _ := writer.ResponseRecorder.Write(body[:1])
	return written, errors.New("client disconnected")
}

// A failed miss answers the client the way the hand-rolled package routes do:
// a member's 404 or 410 says the file does not exist, anything else is a
// gateway failure. The access record carries the status the client saw.
func TestTransparentProxyRecordsFailedUpstreamRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		upstream int
		status   int
		code     string
	}{
		{"member unavailable", http.StatusServiceUnavailable, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"file missing", http.StatusNotFound, http.StatusNotFound, "NOT_FOUND"},
		// proxy.golang.org answers a missing module version with 410: the go
		// command reports it as not found and falls back to its next GOPROXY.
		{"file gone", http.StatusGone, http.StatusGone, "GONE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstreamServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				http.Error(writer, "upstream answer", tc.upstream)
			}))
			t.Cleanup(upstreamServer.Close)

			manager, database, _ := adaptertest.NewManager(t, time.Hour)
			pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "broken", URL: upstreamServer.URL, Priority: 1}})
			if err != nil {
				t.Fatalf("create upstream pool: %v", err)
			}
			proxy := adapter.NewTransparentProxy("fixture", manager, upstream.NewPassiveRecoverySelector(pool), database)
			engine := gin.New()
			engine.GET("/fixture/*path", func(c *gin.Context) {
				proxy.Serve(c, adapter.TransparentPlan{Path: "packages/missing.bin", CacheKey: "fixture/packages/missing.bin", TTL: time.Hour})
			})

			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/fixture/packages/missing.bin", nil))
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error body %q: %v", response.Body.String(), err)
			}
			if response.Code != tc.status || body.Code != tc.code {
				t.Fatalf("response = %d %s, want %d %s", response.Code, body.Code, tc.status, tc.code)
			}

			var logs []db.AccessLog
			if err := database.Find(&logs).Error; err != nil {
				t.Fatalf("read access logs: %v", err)
			}
			if len(logs) != 1 || logs[0].StatusCode != tc.status || logs[0].Hit || logs[0].Upstream != "broken" {
				t.Fatalf("access logs = %+v, want one failed miss answered %d", logs, tc.status)
			}
		})
	}
}

// An apt pool of two archive mirrors: the preferred mirror lacks one package
// and the InRelease file, and its Packages index is temporarily unavailable.
func TestTransparentProxyMovesOnOnlyWhereAnotherMirrorMayAnswer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var mirrorHits, officialHits atomic.Int64
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits.Add(1)
		switch r.URL.Path {
		case "/pool/main/h/hello/hello_2.10-3_amd64.deb", "/pool/main/m/missing/missing_1.0_amd64.deb", "/dists/jammy/InRelease":
			http.NotFound(w, r)
		case "/pool/main/g/gone/gone_1.0_amd64.deb":
			w.WriteHeader(http.StatusGone)
		case "/dists/jammy/main/binary-amd64/Packages.gz":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte("mirror:" + r.URL.Path))
		}
	}))
	t.Cleanup(mirror.Close)
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		officialHits.Add(1)
		if r.URL.Path == "/pool/main/m/missing/missing_1.0_amd64.deb" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("official:" + r.URL.Path))
	}))
	t.Cleanup(official.Close)

	manager, database, _ := adaptertest.NewManager(t, time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "mirror", URL: mirror.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "official", URL: official.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy := adapter.NewTransparentProxy("apt", manager, upstream.NewPassiveRecoverySelector(pool), database)
	proxy.RetryMissingArtifacts = true
	engine := gin.New()
	engine.GET("/apt/*path", func(c *gin.Context) {
		path := c.Param("path")
		proxy.Serve(c, adapter.TransparentPlan{Path: path, CacheKey: "apt/ubuntu" + path, TTL: time.Hour})
	})

	for _, tc := range []struct {
		name, path      string
		status          int
		body            string
		officialRequest bool
	}{
		{"package missing on the mirror", "/pool/main/h/hello/hello_2.10-3_amd64.deb", http.StatusOK, "official:/pool/main/h/hello/hello_2.10-3_amd64.deb", true},
		// Metadata stays on one mirror's snapshot: a 404 there is the answer.
		{"metadata missing on the mirror", "/dists/jammy/InRelease", http.StatusNotFound, "", false},
		{"metadata unavailable on the mirror", "/dists/jammy/main/binary-amd64/Packages.gz", http.StatusOK, "official:/dists/jammy/main/binary-amd64/Packages.gz", true},
		{"package missing everywhere", "/pool/main/m/missing/missing_1.0_amd64.deb", http.StatusNotFound, "", true},
		// Only a 404 says the mirror lacks the file: a 410 is its answer.
		{"package gone on the mirror", "/pool/main/g/gone/gone_1.0_amd64.deb", http.StatusGone, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := officialHits.Load()
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/apt"+tc.path, nil))
			if response.Code != tc.status || (tc.body != "" && response.Body.String() != tc.body) {
				t.Fatalf("status=%d body=%q, want %d %q", response.Code, response.Body.String(), tc.status, tc.body)
			}
			if asked := officialHits.Load() > before; asked != tc.officialRequest {
				t.Fatalf("official member asked = %v, want %v", asked, tc.officialRequest)
			}
		})
	}
}

// Optional content, such as a relayed Go checksum database, is offered by
// some pool members only: its absence is an answer the client acts on.
func TestTransparentProxyTreatsTheAbsenceOfOptionalContentAsTheAnswer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var firstStatus, secondStatus atomic.Int64
	member := func(status *atomic.Int64, name string) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if code := int(status.Load()); code != http.StatusOK {
				w.WriteHeader(code)
				return
			}
			_, _ = w.Write([]byte(name))
		}))
		t.Cleanup(server.Close)
		return server
	}
	first, second := member(&firstStatus, "first"), member(&secondStatus, "second")

	manager, database, _ := adaptertest.NewManager(t, time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "first", URL: first.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "second", URL: second.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy := adapter.NewTransparentProxy("apt", manager, upstream.NewPassiveRecoverySelector(pool), database)
	const ttl = 20 * time.Millisecond
	engine := gin.New()
	// InRelease is metadata: unless it is optional a 404 or 410 stays on the
	// member that gave it and is the client's answer.
	engine.GET("/:mode/*path", func(c *gin.Context) {
		mode := c.Param("mode")
		proxy.Serve(c, adapter.TransparentPlan{
			Path:             c.Param("path"),
			CacheKey:         "apt/" + mode + c.Param("path") + "/InRelease",
			TTL:              ttl,
			Optional:         mode != "plain",
			PresenceIsAnswer: mode == "revoking" || mode == "revoking-gone",
		})
	})
	get := func(path string) (int, string) {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response.Code, response.Body.String()
	}
	answer := func(firstCode, secondCode int) {
		firstStatus.Store(int64(firstCode))
		secondStatus.Store(int64(secondCode))
	}
	cached := func(mode string) int64 {
		var entries int64
		if err := database.Model(&db.CacheEntry{}).Where("key = ?", "apt/"+mode+"/expires/InRelease").Count(&entries).Error; err != nil {
			t.Fatal(err)
		}
		return entries
	}

	answer(http.StatusNotFound, http.StatusOK)
	if code, body := get("/optional/moves-on"); code != http.StatusOK || body != "second" {
		t.Errorf("optional content missing on the first member = %d %q, want 200 from the second", code, body)
	}
	if code, _ := get("/plain/moves-on"); code != http.StatusNotFound {
		t.Errorf("metadata missing on the first member = %d, want its 404", code)
	}

	// For optional content 410 means what 404 means: not offered here.
	answer(http.StatusGone, http.StatusOK)
	if code, body := get("/optional/gone"); code != http.StatusOK || body != "second" {
		t.Errorf("optional content gone on the first member = %d %q, want 200 from the second", code, body)
	}
	if code, _ := get("/plain/gone"); code != http.StatusGone {
		t.Errorf("metadata gone on the first member = %d, want its 410", code)
	}
	answer(http.StatusGone, http.StatusGone)
	if code, _ := get("/optional/gone-everywhere"); code != http.StatusGone {
		t.Errorf("optional content gone on every member = %d, want 410", code)
	}

	// A copy outlives the pool's offer unless the absence revokes it.
	for _, mode := range []string{"optional", "revoking", "revoking-gone", "plain"} {
		answer(http.StatusOK, http.StatusOK)
		if code, body := get("/" + mode + "/expires"); code != http.StatusOK || body != "first" {
			t.Fatalf("%s content = %d %q, want 200 from the first member", mode, code, body)
		}
	}
	time.Sleep(2 * ttl)
	answer(http.StatusServiceUnavailable, http.StatusServiceUnavailable)
	if code, body := get("/revoking/expires"); code != http.StatusOK || body != "first" {
		t.Errorf("expired revocable content while the pool is unavailable = %d %q, want the stale copy", code, body)
	}
	answer(http.StatusNotFound, http.StatusNotFound)
	if code, _ := get("/revoking/expires"); code != http.StatusNotFound || cached("revoking") != 0 {
		t.Errorf("expired content revoked by the pool = %d with %d cached copies, want 404 and none", code, cached("revoking"))
	}
	for _, mode := range []string{"optional", "plain"} {
		if code, body := get("/" + mode + "/expires"); code != http.StatusOK || body != "first" || cached(mode) != 1 {
			t.Errorf("expired %s content missing upstream = %d %q with %d cached copies, want the stale copy kept", mode, code, body, cached(mode))
		}
	}
	answer(http.StatusGone, http.StatusGone)
	if code, _ := get("/revoking-gone/expires"); code != http.StatusGone || cached("revoking-gone") != 0 {
		t.Errorf("expired content the pool calls gone = %d with %d cached copies, want 410 and none", code, cached("revoking-gone"))
	}
	if code, body := get("/plain/expires"); code != http.StatusOK || body != "first" || cached("plain") != 1 {
		t.Errorf("expired metadata gone upstream = %d %q with %d cached copies, want the stale copy kept", code, body, cached("plain"))
	}

	// Without a copy, a pool that cannot say whether it offers the content is
	// a gateway error, except where the offer itself is the question.
	answer(http.StatusServiceUnavailable, http.StatusServiceUnavailable)
	if code, _ := get("/optional/unanswered"); code != http.StatusBadGateway {
		t.Errorf("optional content nobody answers for = %d, want 502", code)
	}
	if code, _ := get("/revoking/unanswered"); code != http.StatusNotFound {
		t.Errorf("an offer nobody answers for = %d, want 404", code)
	}
	first.Close()
	second.Close()
	if code, _ := get("/revoking/unreachable"); code != http.StatusNotFound {
		t.Errorf("an offer no member can be asked about = %d, want 404", code)
	}
}

// A pool that does not offer optional content, such as proxy.golang.org for
// the checksum database, gives an expected answer: it is not logged as an
// error. Every other failure still is.
func TestTransparentProxyLogsTheAbsenceOfOptionalContentAtDebugLevel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var firstStatus, secondStatus atomic.Int64
	member := func(status *atomic.Int64) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(int(status.Load()))
		}))
		t.Cleanup(server.Close)
		return server
	}
	first, second := member(&firstStatus), member(&secondStatus)

	manager, database, _ := adaptertest.NewManager(t, time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "first", URL: first.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "second", URL: second.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy := adapter.NewTransparentProxy("go", manager, upstream.NewPassiveRecoverySelector(pool), database)
	engine := gin.New()
	engine.GET("/:mode/*path", func(c *gin.Context) {
		mode := c.Param("mode")
		proxy.Serve(c, adapter.TransparentPlan{
			Path:     c.Param("path"),
			CacheKey: "go/" + mode + c.Param("path"),
			TTL:      time.Hour,
			Optional: mode == "optional",
		})
	})

	core, observed := observer.New(zap.DebugLevel)
	restore := zap.ReplaceGlobals(zap.New(core, zap.AddStacktrace(zap.ErrorLevel)))
	t.Cleanup(restore)

	for _, tc := range []struct {
		name, path            string
		firstCode, secondCode int
		status                int
		level                 zapcore.Level
	}{
		{"optional content missing everywhere", "/optional/sumdb/sum.golang.org/supported", http.StatusNotFound, http.StatusNotFound, http.StatusNotFound, zap.DebugLevel},
		{"optional content gone everywhere", "/optional/sumdb/sum.golang.org/lookup/gone", http.StatusGone, http.StatusNotFound, http.StatusNotFound, zap.DebugLevel},
		{"optional content after an unavailable member", "/optional/sumdb/sum.golang.org/latest", http.StatusServiceUnavailable, http.StatusNotFound, http.StatusNotFound, zap.ErrorLevel},
		{"optional content nobody answers for", "/optional/sumdb/sum.golang.org/tile", http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusBadGateway, zap.ErrorLevel},
		{"required content missing", "/plain/example.com/m/@v/list", http.StatusNotFound, http.StatusNotFound, http.StatusNotFound, zap.ErrorLevel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			firstStatus.Store(int64(tc.firstCode))
			secondStatus.Store(int64(tc.secondCode))
			observed.TakeAll()
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d", response.Code, tc.status)
			}
			var failures []observer.LoggedEntry
			for _, entry := range observed.TakeAll() {
				if entry.Level >= zap.WarnLevel || entry.Message == "transparent upstream does not offer optional content" {
					failures = append(failures, entry)
				}
			}
			if len(failures) != 1 || failures[0].Level != tc.level {
				t.Fatalf("failure log entries = %+v, want one at %s", failures, tc.level)
			}
			if tc.level == zap.DebugLevel && failures[0].Stack != "" {
				t.Fatalf("absence logged with a stack trace: %s", failures[0].Stack)
			}
		})
	}
}

// A plan's Rewrite sees the member that actually served the body, the cache
// stores the rewritten bytes, and Open hands a protocol reader the same
// entry a client request is served from.
func TestTransparentProxyRewriteStoresTheServingMembersRepresentation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var fallbackHits atomic.Int64
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(broken.Close)
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"index-v1"`)
		_, _ = w.Write([]byte("chart at " + "http://" + r.Host + "/c.tgz\n"))
	}))
	t.Cleanup(fallback.Close)

	manager, database, _ := adaptertest.NewManager(t, time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "broken", URL: broken.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "fallback", URL: fallback.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy := adapter.NewTransparentProxy("fixture", manager, upstream.NewPassiveRecoverySelector(pool), database)
	var servedBy []string
	plan := adapter.TransparentPlan{
		Path: "index.txt", CacheKey: "fixture/index.txt", TTL: time.Hour,
		Rewrite: func(body io.ReadCloser, served *upstream.Upstream) io.ReadCloser {
			servedBy = append(servedBy, served.Name)
			return struct {
				io.Reader
				io.Closer
			}{io.MultiReader(strings.NewReader("rewritten from "+served.URL+": "), body), body}
		},
	}
	engine := gin.New()
	engine.GET("/fixture/index.txt", func(c *gin.Context) { proxy.Serve(c, plan) })

	want := "rewritten from " + fallback.URL + ": chart at " + fallback.URL + "/c.tgz\n"
	for requestNumber := 1; requestNumber <= 2; requestNumber++ {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/fixture/index.txt", nil))
		if response.Code != http.StatusOK || response.Body.String() != want {
			t.Fatalf("request %d response = %d %q, want %q", requestNumber, response.Code, response.Body.String(), want)
		}
		if length := response.Header().Get("Content-Length"); length != "" && length != strconv.Itoa(len(want)) {
			t.Fatalf("request %d Content-Length = %s for a %d byte body", requestNumber, length, len(want))
		}
	}
	opened, err := proxy.Open(t.Context(), plan)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	body, err := io.ReadAll(opened.Reader)
	_ = opened.Reader.Close()
	if err != nil || string(body) != want || !opened.Hit {
		t.Fatalf("Open = hit %v %q (%v), want the cached %q", opened.Hit, body, err, want)
	}
	if len(servedBy) != 1 || servedBy[0] != "fallback" || fallbackHits.Load() != 1 {
		t.Fatalf("rewrites = %q, fallback fetches = %d; want one fill by the member that served", servedBy, fallbackHits.Load())
	}
}
