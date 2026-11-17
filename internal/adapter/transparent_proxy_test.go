package adapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/cache"
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

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "transparent.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&db.CacheEntry{}, &db.AccessLog{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	storage, err := cache.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTransparentTestManager(t, manager) })
	pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "fixture", URL: upstreamServer.URL, Priority: 1}})
	if err != nil {
		t.Fatalf("create upstream pool: %v", err)
	}
	proxy := NewTransparentProxy("fixture", manager, upstream.NewPrioritySelector(pool), database)
	engine := gin.New()
	engine.GET("/fixture/*path", func(c *gin.Context) {
		proxy.Serve(c, TransparentPlan{Path: "packages/example.bin", CacheKey: "fixture/packages/example.bin", TTL: time.Hour})
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

func TestTransparentProxyRecordsFailedUpstreamRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstreamServer.Close)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "transparent-error.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&db.CacheEntry{}, &db.AccessLog{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	storage, err := cache.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTransparentTestManager(t, manager) })
	pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "broken", URL: upstreamServer.URL, Priority: 1}})
	if err != nil {
		t.Fatalf("create upstream pool: %v", err)
	}
	proxy := NewTransparentProxy("fixture", manager, upstream.NewPrioritySelector(pool), database)
	engine := gin.New()
	engine.GET("/fixture/*path", func(c *gin.Context) {
		proxy.Serve(c, TransparentPlan{Path: "packages/missing.bin", CacheKey: "fixture/packages/missing.bin", TTL: time.Hour})
	})

	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/fixture/packages/missing.bin", nil))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusBadGateway)
	}

	var logs []db.AccessLog
	if err := database.Find(&logs).Error; err != nil {
		t.Fatalf("read access logs: %v", err)
	}
	if len(logs) != 1 || logs[0].StatusCode != http.StatusBadGateway || logs[0].Hit || logs[0].Upstream != "broken" {
		t.Fatalf("access logs = %+v, want one failed miss", logs)
	}
}

func closeTransparentTestManager(t *testing.T, manager *cache.Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatalf("close manager: %v", err)
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

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "transparent.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CacheEntry{}, &db.AccessLog{}); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTransparentTestManager(t, manager) })
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "mirror", URL: mirror.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "official", URL: official.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy := NewTransparentProxy("apt", manager, upstream.NewPassiveRecoverySelector(pool), database)
	proxy.RetryMissingArtifacts = true
	engine := gin.New()
	engine.GET("/apt/*path", func(c *gin.Context) {
		path := c.Param("path")
		proxy.Serve(c, TransparentPlan{Path: path, CacheKey: "apt/ubuntu" + path, TTL: time.Hour})
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
		{"package gone on the mirror", "/pool/main/g/gone/gone_1.0_amd64.deb", http.StatusBadGateway, "", false},
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

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "transparent.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CacheEntry{}, &db.AccessLog{}); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTransparentTestManager(t, manager) })
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "first", URL: first.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "second", URL: second.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy := NewTransparentProxy("apt", manager, upstream.NewPassiveRecoverySelector(pool), database)
	const ttl = 20 * time.Millisecond
	engine := gin.New()
	// InRelease is metadata: unless it is optional a 404 stays on the member
	// that gave it and a 410 is a gateway error.
	engine.GET("/:mode/*path", func(c *gin.Context) {
		mode := c.Param("mode")
		proxy.Serve(c, TransparentPlan{
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
	if code, _ := get("/plain/gone"); code != http.StatusBadGateway {
		t.Errorf("metadata gone = %d, want 502", code)
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

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "transparent.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CacheEntry{}, &db.AccessLog{}); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTransparentTestManager(t, manager) })
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "first", URL: first.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "second", URL: second.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy := NewTransparentProxy("go", manager, upstream.NewPassiveRecoverySelector(pool), database)
	engine := gin.New()
	engine.GET("/:mode/*path", func(c *gin.Context) {
		mode := c.Param("mode")
		proxy.Serve(c, TransparentPlan{
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
