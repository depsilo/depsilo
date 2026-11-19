package goproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/accesslog"
	"depsilo/internal/adapter"
	"depsilo/internal/adapter/adaptertest"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/tamper"
	"depsilo/internal/upstream"
)

const (
	sumDBLatest = "go.sum database tree\n3\nq0nmUoeTeW4ey0g5GDEL1o0TDJr4CL+a3JzBfwrSxPo=\n\n— sum.golang.org signature\n"
	sumDBGrown  = "go.sum database tree\n4\nLtVHmbFdYTHPBKzUUDMNrWHqMAzhnJHG/vFp0RHQbcg=\n\n— sum.golang.org signature\n"
	sumDBRecord = "2\nexample.com/mod v1.0.0 h1:aaaa=\nexample.com/mod v1.0.0/go.mod h1:bbbb=\n\n"
	sumDBLookup = sumDBRecord + sumDBLatest
)

// sumDBMirror is a module proxy that also mirrors sum.golang.org, as
// goproxy.cn does. While offline is set it answers that status instead.
type sumDBMirror struct {
	server  *httptest.Server
	mu      sync.Mutex
	paths   []string
	offline int
	head    string
}

func newSumDBMirror(t *testing.T) *sumDBMirror {
	t.Helper()
	mirror := &sumDBMirror{head: sumDBLatest}
	mirror.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirror.mu.Lock()
		mirror.paths = append(mirror.paths, r.URL.Path)
		offline, head := mirror.offline, mirror.head
		mirror.mu.Unlock()
		if offline != 0 {
			w.WriteHeader(offline)
			return
		}
		rest, ok := strings.CutPrefix(r.URL.Path, "/sumdb/sum.golang.org/")
		switch {
		case !ok:
			http.NotFound(w, r)
		case rest == "supported":
			// goproxy.cn answers 200 with an empty body.
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
		case rest == "latest":
			_, _ = w.Write([]byte(head))
		case rest == "lookup/example.com/mod@v1.0.0", rest == "lookup/example.com/mod@v1.0.0-a.zip":
			// Every answer ends in the tree head of the moment.
			_, _ = w.Write([]byte(sumDBRecord + head))
		case strings.HasPrefix(rest, "tile/"):
			_, _ = w.Write([]byte("tile:" + rest))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(mirror.server.Close)
	return mirror
}

func (mirror *sumDBMirror) requests() []string {
	mirror.mu.Lock()
	defer mirror.mu.Unlock()
	return append([]string(nil), mirror.paths...)
}

func (mirror *sumDBMirror) answer(status int) {
	mirror.mu.Lock()
	defer mirror.mu.Unlock()
	mirror.offline = status
}

// grow moves the database on to a newer signed tree head.
func (mirror *sumDBMirror) grow(head string) {
	mirror.mu.Lock()
	defer mirror.mu.Unlock()
	mirror.head = head
}

// newSumDBRoute mounts the Go adapter at /go over the given pool members, in
// priority order.
func newSumDBRoute(t *testing.T, ttl config.CacheConfig, members ...*sumDBMirror) (*gin.Engine, *gorm.DB) {
	t.Helper()
	engine, database, _ := newSumDBRelay(t, ttl, members...)
	return engine, database
}

// newSumDBRelay is newSumDBRoute with the cache manager behind the route. As
// in a server with default settings, the tamper threshold is the blob TTL.
func newSumDBRelay(t *testing.T, ttl config.CacheConfig, members ...*sumDBMirror) (*gin.Engine, *gorm.DB, *cache.Manager) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	manager, database, _ := adaptertest.NewManager(t, ttl.TTLBlob)
	upstreams := make([]config.UpstreamConfig, 0, len(members))
	for i, member := range members {
		upstreams = append(upstreams, config.UpstreamConfig{
			Name: "member-" + string(rune('a'+i)), URL: member.server.URL, Priority: i + 1, ProbeMode: "passive",
		})
	}
	pool, err := upstream.NewPool(upstreams)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	New(manager, upstream.NewPassiveRecoverySelector(pool), ttl, database).Register(engine.Group("/go"))
	return engine, database, manager
}

func getSumDB(engine http.Handler, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

// sumDBRecords captures what the Go route reports about its requests.
type sumDBRecords struct {
	mu     sync.Mutex
	audit  []db.AuditLog
	events []accesslog.Event
}

func (records *sumDBRecords) Log(entry db.AuditLog) {
	records.mu.Lock()
	defer records.mu.Unlock()
	records.audit = append(records.audit, entry)
}

func (records *sumDBRecords) Record(event accesslog.Event) {
	records.mu.Lock()
	defer records.mu.Unlock()
	records.events = append(records.events, event)
}

func (*sumDBRecords) Flush(context.Context) error { return nil }
func (*sumDBRecords) Close(context.Context) error { return nil }

var sumDBTTL = config.CacheConfig{TTLIndex: 5 * time.Minute, TTLBlob: 72 * time.Hour}

func TestSumDBRelaysEveryEndpointToTheModuleProxy(t *testing.T) {
	mirror := newSumDBMirror(t)
	engine, _ := newSumDBRoute(t, sumDBTTL, mirror)

	for _, tc := range []struct{ path, body string }{
		{"/sumdb/sum.golang.org/supported", ""},
		{"/sumdb/sum.golang.org/latest", sumDBLatest},
		{"/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0", sumDBLookup},
		{"/sumdb/sum.golang.org/tile/8/0/000", "tile:tile/8/0/000"},
		{"/sumdb/sum.golang.org/tile/8/1/x001/234.p/56", "tile:tile/8/1/x001/234.p/56"},
		{"/sumdb/sum.golang.org/tile/8/data/x071/x475/204", "tile:tile/8/data/x071/x475/204"},
	} {
		response := getSumDB(engine, "/go"+tc.path)
		if response.Code != http.StatusOK || response.Body.String() != tc.body {
			t.Errorf("GET /go%s = %d %q, want 200 %q", tc.path, response.Code, response.Body.String(), tc.body)
		}
		if requests := mirror.requests(); len(requests) == 0 || requests[len(requests)-1] != tc.path {
			t.Errorf("GET /go%s reached the upstream as %v, want a final request for %s", tc.path, requests, tc.path)
		}
	}

	// Every spelling of a recorded version is asked for as it was written.
	// The mirror knows none of these records.
	for _, lookup := range []string{
		"example.com/!mod@v1.0.0-!r!c.1",
		"example.com/mod@v0.0.0-20191109021931-daa7c04131f5",
		"example.com/mod@v2.0.0+incompatible",
		"example.com/mod@v2.0.0-pre.1+incompatible",
	} {
		path := "/sumdb/sum.golang.org/lookup/" + lookup
		if response := getSumDB(engine, "/go"+path); response.Code != http.StatusNotFound {
			t.Errorf("GET /go%s = %d, want the mirror's 404", path, response.Code)
		}
		if requests := mirror.requests(); requests[len(requests)-1] != path {
			t.Errorf("GET /go%s reached the upstream as %s", path, requests[len(requests)-1])
		}
	}
}

// Records and tiles never change, so they stay for the blob TTL. Whether the
// database is mirrored and what its newest tree head is are asked again after
// the index TTL.
func TestSumDBCachesRecordsAndTilesForTheBlobTTLAndTheRestForTheIndexTTL(t *testing.T) {
	mirror := newSumDBMirror(t)
	engine, database := newSumDBRoute(t, sumDBTTL, mirror)

	paths := map[string]string{
		"go/sumdb/sum.golang.org/supported":                     "/go/sumdb/sum.golang.org/supported",
		"go/sumdb/sum.golang.org/latest":                        "/go/sumdb/sum.golang.org/latest",
		"go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0": "/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0",
		"go/sumdb/sum.golang.org/tile/8/0/x001/234":             "/go/sumdb/sum.golang.org/tile/8/0/x001/234",
	}
	for _, path := range paths {
		first := getSumDB(engine, path)
		asked := len(mirror.requests())
		second := getSumDB(engine, path)
		if first.Code != http.StatusOK || second.Code != http.StatusOK || second.Body.String() != first.Body.String() {
			t.Fatalf("GET %s = %d %q then %d %q", path, first.Code, first.Body.String(), second.Code, second.Body.String())
		}
		if again := len(mirror.requests()); again != asked {
			t.Errorf("second GET %s asked the upstream (%d requests, then %d)", path, asked, again)
		}
	}

	for key := range paths {
		var entry db.CacheEntry
		if err := database.Where("key = ?", key).First(&entry).Error; err != nil {
			t.Fatalf("cache entry %s: %v", key, err)
		}
		want := sumDBTTL.TTLIndex
		if strings.Contains(key, "/tile/") || strings.Contains(key, "/lookup/") {
			want = sumDBTTL.TTLBlob
		}
		if lifetime := time.Until(entry.ExpiresAt); lifetime > want || lifetime < want-time.Minute {
			t.Errorf("%s expires in %v, want %v", key, lifetime.Round(time.Second), want)
		}
		if entry.PackageName != "" {
			t.Errorf("%s is filed under package %q; checksum data belongs to no package", key, entry.PackageName)
		}
	}

	var logs []db.AccessLog
	if err := database.Where("cache_key = ?", "go/sumdb/sum.golang.org/tile/8/0/x001/234").Order("id ASC").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[0].Hit || !logs[1].Hit || logs[0].AdapterType != "go" || logs[0].Upstream != "member-a" || logs[0].PackageName != "" {
		t.Errorf("tile access logs = %+v, want a miss from member-a and a hit under the go adapter with no package", logs)
	}
}

// The checksum database is verification data that the go command checks
// itself. Reading it downloads no dependency, so no request for it may show
// up as a download in the audit feed or as a delivered artifact.
func TestSumDBRequestsAreRecordedAsMetadataNotAsDownloads(t *testing.T) {
	mirror := newSumDBMirror(t)
	engine, database := newSumDBRoute(t, sumDBTTL, mirror)
	records := &sumDBRecords{}
	route := adapter.NewRequestScope(records, records, nil, nil).Wrap(engine)

	keys := []string{
		"go/sumdb/sum.golang.org/supported",
		"go/sumdb/sum.golang.org/latest",
		"go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0",
		// A pre-release may be spelled like the name of a module archive.
		"go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0-a.zip",
		"go/sumdb/sum.golang.org/tile/8/0/x001/234",
		"go/sumdb/sum.golang.org/tile/8/data/x071/x475/204",
	}
	for _, key := range keys {
		// A miss, then a hit.
		for attempt := 0; attempt < 2; attempt++ {
			if response := getSumDB(route, "/"+key); response.Code != http.StatusOK {
				t.Fatalf("GET /%s = %d, want 200", key, response.Code)
			}
		}
		var entry db.CacheEntry
		if err := database.Where("key = ?", key).First(&entry).Error; err != nil {
			t.Fatalf("cache entry %s: %v", key, err)
		}
		if entry.CacheKind != db.CacheKindMetadata {
			t.Errorf("%s is cached as %q, want %q", key, entry.CacheKind, db.CacheKindMetadata)
		}
	}
	// A record the mirror lacks is audited the same way.
	if response := getSumDB(route, "/go/sumdb/sum.golang.org/lookup/example.com/absent@v1.0.0"); response.Code != http.StatusNotFound {
		t.Fatalf("lookup of an unknown record = %d, want 404", response.Code)
	}

	if want := 2*len(keys) + 1; len(records.audit) != want || len(records.events) != want {
		t.Fatalf("recorded %d audit entries and %d access events, want %d of each", len(records.audit), len(records.events), want)
	}
	for _, entry := range records.audit {
		if entry.Action != "metadata" || entry.Ecosystem != "go" || entry.PackageName != "" || entry.Version != "" {
			t.Errorf("audit entry = %s %s %q %q (status %d), want a go metadata request that names no package",
				entry.Action, entry.Ecosystem, entry.PackageName, entry.Version, entry.StatusCode)
		}
	}
	for _, event := range records.events {
		if event.DependencyKind != db.CacheKindMetadata || accesslog.SuccessfulArtifactDownload(event) {
			t.Errorf("%s is accounted as %q (delivered artifact: %v), want metadata",
				event.CacheKey, event.DependencyKind, accesslog.SuccessfulArtifactDownload(event))
		}
	}
}

// proxy.golang.org does not mirror the checksum database. Its 404 tells the go
// command to contact the database itself and must stay a 404; an error would
// make the go command give up instead.
func TestSumDBRelaysTheAnswerOfAPoolThatLacksTheDatabase(t *testing.T) {
	mirror := newSumDBMirror(t)
	mirror.answer(http.StatusNotFound)
	engine, _ := newSumDBRoute(t, sumDBTTL, mirror)

	for _, path := range []string{
		"/go/sumdb/sum.golang.org/supported",
		"/go/sumdb/sum.golang.org/latest",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0",
		"/go/sumdb/sum.golang.org/tile/8/0/000",
	} {
		if response := getSumDB(engine, path); response.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, response.Code)
		}
	}

	mirror.answer(http.StatusGone)
	if response := getSumDB(engine, "/go/sumdb/sum.golang.org/supported"); response.Code != http.StatusGone {
		t.Errorf("supported on a pool answering 410 = %d, want 410", response.Code)
	}

	// The refusal is not remembered: the next request finds the database.
	mirror.answer(0)
	if response := getSumDB(engine, "/go/sumdb/sum.golang.org/supported"); response.Code != http.StatusOK {
		t.Errorf("supported once the pool mirrors the database = %d, want 200", response.Code)
	}
}

// With proxy.golang.org ahead of goproxy.cn, every endpoint has to reach the
// member that mirrors the database: a go command told "supported" never falls
// back to the database itself. The GOPROXY protocol gives 404 and 410 the
// same meaning here, so a member may decline with either.
func TestSumDBAsksTheNextMemberWhenOneLacksTheDatabase(t *testing.T) {
	for _, declined := range []int{http.StatusNotFound, http.StatusGone} {
		t.Run(http.StatusText(declined), func(t *testing.T) {
			plain, mirror := newSumDBMirror(t), newSumDBMirror(t)
			plain.answer(declined)
			engine, _ := newSumDBRoute(t, sumDBTTL, plain, mirror)

			for _, path := range []string{
				"/sumdb/sum.golang.org/supported",
				"/sumdb/sum.golang.org/latest",
				"/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0",
				"/sumdb/sum.golang.org/tile/8/0/000",
			} {
				if response := getSumDB(engine, "/go"+path); response.Code != http.StatusOK {
					t.Errorf("GET /go%s = %d, want 200 from the member that mirrors the database", path, response.Code)
				}
				if requests := mirror.requests(); len(requests) == 0 || requests[len(requests)-1] != path {
					t.Errorf("GET /go%s: mirroring member saw %v", path, requests)
				}
			}
		})
	}
}

// A cached "supported" must not outlive a pool that stopped mirroring the
// database, or the go command keeps asking Depsilo for records nobody serves.
// Records and tiles fetched earlier stay verifiable and keep being served.
func TestSumDBSupportedFollowsAPoolThatStopsMirroring(t *testing.T) {
	const supported = "/go/sumdb/sum.golang.org/supported"
	const lookup = "/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0"
	expired := func(t *testing.T) (*gin.Engine, *sumDBMirror) {
		mirror := newSumDBMirror(t)
		engine, _ := newSumDBRoute(t, config.CacheConfig{TTLIndex: 20 * time.Millisecond, TTLBlob: 20 * time.Millisecond}, mirror)
		for _, path := range []string{supported, lookup} {
			if response := getSumDB(engine, path); response.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", path, response.Code)
			}
		}
		time.Sleep(40 * time.Millisecond)
		return engine, mirror
	}

	t.Run("pool stopped mirroring", func(t *testing.T) {
		engine, mirror := expired(t)
		mirror.answer(http.StatusNotFound)
		// An expired answer is asked for again before it is served, so the
		// very next go command learns that the database is gone from here.
		if response := getSumDB(engine, supported); response.Code != http.StatusNotFound {
			t.Fatalf("expired supported after the pool stopped mirroring = %d, want 404", response.Code)
		}
		if response := getSumDB(engine, lookup); response.Code != http.StatusOK || response.Body.String() != sumDBLookup {
			t.Fatalf("expired record fetched earlier = %d %q, want it served from the cache", response.Code, response.Body.String())
		}
		mirror.answer(0)
		if response := getSumDB(engine, supported); response.Code != http.StatusOK {
			t.Errorf("supported once the pool mirrors the database again = %d, want 200", response.Code)
		}
	})

	// An unreachable pool is not an answer: the cached one still stands, also
	// once the member is known to be failing.
	t.Run("pool unavailable", func(t *testing.T) {
		engine, mirror := expired(t)
		mirror.answer(http.StatusServiceUnavailable)
		for attempt := 0; attempt < 3; attempt++ {
			if response := getSumDB(engine, supported); response.Code != http.StatusOK {
				t.Fatalf("supported while the pool is unavailable, attempt %d = %d, want the cached 200", attempt+1, response.Code)
			}
		}
	})
}

// "latest" and lookup answers end in the tree head of the moment, so a newer
// answer differs in bytes although nothing was tampered with. Where the
// operator's tamper threshold reaches their TTLs, a refresh still replaces
// the copy: no baseline is taken and no alert is raised.
func TestSumDBRefreshReplacesAnswersWithoutATamperAlert(t *testing.T) {
	const ttl = 20 * time.Millisecond
	const latest = "/go/sumdb/sum.golang.org/latest"
	const lookup = "/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0"
	const tile = "/go/sumdb/sum.golang.org/tile/8/0/000"
	mirror := newSumDBMirror(t)
	// newSumDBRelay sets the tamper threshold to the blob TTL: here every
	// checksum database answer lives exactly that long.
	engine, database, manager := newSumDBRelay(t, config.CacheConfig{TTLIndex: ttl, TTLBlob: ttl}, mirror)
	manager.SetTamperRecorder(tamper.NewRecorder(database))

	expect := func(when string, bodies map[string]string) {
		t.Helper()
		for path, body := range bodies {
			if response := getSumDB(engine, path); response.Code != http.StatusOK || response.Body.String() != body {
				t.Errorf("GET %s %s = %d %q, want 200 %q", path, when, response.Code, response.Body.String(), body)
			}
		}
	}
	expect("at first", map[string]string{latest: sumDBLatest, lookup: sumDBLookup, tile: "tile:tile/8/0/000"})

	time.Sleep(2 * ttl)
	mirror.grow(sumDBGrown)
	grown := map[string]string{latest: sumDBGrown, lookup: sumDBRecord + sumDBGrown, tile: "tile:tile/8/0/000"}
	expect("once the tree has grown", grown)
	// The copies were replaced, not only passed through.
	expect("after the refresh", grown)

	var alerts, baselines int64
	if err := database.Model(&db.QuarantineEvent{}).Count(&alerts).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&db.TamperRecord{}).Count(&baselines).Error; err != nil {
		t.Fatal(err)
	}
	if alerts != 0 || baselines != 0 {
		t.Errorf("refreshing checksum database answers left %d tamper alerts and %d baselines, want none", alerts, baselines)
	}
}

// The go command gives up on any answer to "supported" other than 200, 404
// and 410. A pool that cannot be asked must leave it free to contact the
// database itself, as it did before Depsilo relayed the database.
func TestSumDBSupportedIsNotFoundWhenThePoolGivesNoAnswer(t *testing.T) {
	for name, fail := range map[string]func(*sumDBMirror){
		"every member answers 503": func(member *sumDBMirror) { member.answer(http.StatusServiceUnavailable) },
		"no member is reachable":   func(member *sumDBMirror) { member.server.Close() },
	} {
		t.Run(name, func(t *testing.T) {
			first, second := newSumDBMirror(t), newSumDBMirror(t)
			engine, _ := newSumDBRoute(t, sumDBTTL, first, second)
			fail(first)
			fail(second)
			// Later attempts also meet members already known to be failing.
			for attempt := 0; attempt < 3; attempt++ {
				if response := getSumDB(engine, "/go/sumdb/sum.golang.org/supported"); response.Code != http.StatusNotFound {
					t.Fatalf("supported, attempt %d = %d, want 404", attempt+1, response.Code)
				}
			}
			// A record the go command was promised is a different matter.
			for _, path := range []string{
				"/go/sumdb/sum.golang.org/latest",
				"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0",
				"/go/sumdb/sum.golang.org/tile/8/0/000",
			} {
				if response := getSumDB(engine, path); response.Code != http.StatusBadGateway {
					t.Errorf("GET %s = %d, want 502", path, response.Code)
				}
			}
		})
	}
}

// A go command told "supported" never falls back to the database itself. When
// the mirroring member goes down while that answer is still fresh, the first
// record that cannot be served makes Depsilo ask the pool again.
func TestSumDBAsksForSupportedAgainOnceARecordCannotBeServed(t *testing.T) {
	const supported = "/go/sumdb/sum.golang.org/supported"
	const tile = "/go/sumdb/sum.golang.org/tile/8/0/000"
	for name, tc := range map[string]struct {
		others int
		record string
		want   int
	}{
		"record missing on the remaining member": {http.StatusNotFound, "/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0", http.StatusNotFound},
		"tile missing on the remaining member":   {http.StatusNotFound, "/go/sumdb/sum.golang.org/tile/8/1/000", http.StatusNotFound},
		"tree head nobody can serve":             {http.StatusServiceUnavailable, "/go/sumdb/sum.golang.org/latest", http.StatusBadGateway},
	} {
		t.Run(name, func(t *testing.T) {
			mirror, other := newSumDBMirror(t), newSumDBMirror(t)
			other.answer(tc.others)
			engine, _ := newSumDBRoute(t, sumDBTTL, mirror, other)
			for _, path := range []string{supported, tile} {
				if response := getSumDB(engine, path); response.Code != http.StatusOK {
					t.Fatalf("GET %s = %d, want 200", path, response.Code)
				}
			}

			mirror.answer(http.StatusServiceUnavailable)
			// What is cached keeps the remembered answer usable.
			for _, path := range []string{tile, supported} {
				if response := getSumDB(engine, path); response.Code != http.StatusOK {
					t.Fatalf("cached %s with the mirror down = %d, want 200", path, response.Code)
				}
			}
			if response := getSumDB(engine, tc.record); response.Code != tc.want {
				t.Fatalf("GET %s with the mirror down = %d, want %d", tc.record, response.Code, tc.want)
			}
			if response := getSumDB(engine, supported); response.Code != http.StatusNotFound {
				t.Errorf("supported after a record could not be served = %d, want 404", response.Code)
			}
		})
	}
}

func TestSumDBRejectsMalformedRequestsBeforeAnyUpstreamContact(t *testing.T) {
	mirror := newSumDBMirror(t)
	engine, database := newSumDBRoute(t, sumDBTTL, mirror)

	for _, path := range []string{
		"/go/sumdb",
		"/go/sumdb/",
		"/go/sumdb/sum.golang.org",
		"/go/sumdb/sum.golang.org/",
		"/go/sumdb/sum.golang.org/unknown",
		"/go/sumdb/sum.golang.org/supported/extra",
		"/go/sumdb/sum.golang.org/latest/",
		"/go/sumdb//latest",
		"/go/sumdb/../latest",
		"/go/sumdb/./latest",
		"/go/sumdb/sum.golang.org/../latest",
		"/go/sumdb/sum..golang.org/latest",
		"/go/sumdb/-sum.golang.org/latest",
		"/go/sumdb/sum-.golang.org/latest",
		"/go/sumdb/" + strings.Repeat("a", 64) + ".golang.org/latest",
		"/go/sumdb/sum.golang.org:443/latest",
		"/go/sumdb/sum_golang.org/latest",
		"/go/sumdb/user@sum.golang.org/latest",
		"/go/sumdb/sum.golang.org/lookup/",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@",
		"/go/sumdb/sum.golang.org/lookup/@v1.0.0",
		"/go/sumdb/sum.golang.org/lookup/example.com//mod@v1.0.0",
		"/go/sumdb/sum.golang.org/lookup/example.com/../mod@v1.0.0",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0/../../latest",
		"/go/sumdb/sum.golang.org/lookup/example.com/Mod@v1.0.0",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@master",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0@v1.0.0",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0+meta",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0-a/../../latest",
		// Versions arrive escaped: "!r!c1", never "RC1" or a stray "!".
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0-RC1",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0-rc1!",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0-!1rc",
		// Characters a URL reads as syntax would change the forwarded path.
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0-a%23b",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0-a%20b",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0-a%3Fb",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0-%2541",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0-%25zz",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0-a%252f..%252flatest",
		"/go/sumdb/sum.golang.org/tile/",
		"/go/sumdb/sum.golang.org/tile/8/0",
		"/go/sumdb/sum.golang.org/tile/8/0/00",
		"/go/sumdb/sum.golang.org/tile/8/0/../000",
		"/go/sumdb/sum.golang.org/tile/8/0//000",
		"/go/sumdb/sum.golang.org/tile/8/0/000/",
		"/go/sumdb/sum.golang.org/tile/8/0/000.p/",
		"/go/sumdb/sum.golang.org/tile/8/x/000",
		// "sumdb" is never a module path, whatever follows it.
		"/go/sumdb/sum.golang.org/@v/list",
		"/go/sumdb/sum.golang.org/latest/@latest",
		"/go/sumdb/sum.golang.org/lookup/example.com/mod/@v/v1.0.0.zip",
	} {
		if response := getSumDB(engine, path); response.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, response.Code)
		}
	}
	if requests := mirror.requests(); len(requests) != 0 {
		t.Errorf("malformed requests reached the upstream: %v", requests)
	}
	// Nothing is filed under a checksum database key or a module key either.
	var entries int64
	if err := database.Model(&db.CacheEntry{}).Count(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Errorf("malformed requests left %d cache entries", entries)
	}
}
