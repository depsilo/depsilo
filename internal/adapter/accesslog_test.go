package adapter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/accesslog"
	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/db"
	"depsilo/internal/requestid"
)

type capturedHookCalls struct {
	mu       sync.Mutex
	recorder map[string]int
	audit    map[string]int
}

func newCapturedHookCalls() *capturedHookCalls {
	return &capturedHookCalls{
		recorder: make(map[string]int),
		audit:    make(map[string]int),
	}
}

type captureAccessRecorder struct {
	owner    int
	calls    *capturedHookCalls
	onRecord func()
}

func (r *captureAccessRecorder) Record(event accesslog.Event) {
	r.calls.mu.Lock()
	r.calls.recorder[event.ClientIP] = r.owner
	r.calls.mu.Unlock()
	if r.onRecord != nil {
		r.onRecord()
	}
}

func (*captureAccessRecorder) Flush(context.Context) error { return nil }
func (*captureAccessRecorder) Close(context.Context) error { return nil }

type captureAuditLogger struct {
	owner int
	calls *capturedHookCalls
}

type captureAuditEntries struct {
	entries []db.AuditLog
}

type timestampAccessRecorder struct {
	event accesslog.Event
}

func (recorder *timestampAccessRecorder) Record(event accesslog.Event) {
	recorder.event = event
}

func (*timestampAccessRecorder) Flush(context.Context) error { return nil }
func (*timestampAccessRecorder) Close(context.Context) error { return nil }

func (l *captureAuditEntries) Log(entry db.AuditLog) {
	l.entries = append(l.entries, entry)
}

func (l *captureAuditLogger) Log(entry db.AuditLog) {
	l.calls.mu.Lock()
	l.calls.audit[entry.ClientIP] = l.owner
	l.calls.mu.Unlock()
}

// scopedContext returns base as a handler behind scope.Wrap sees it, the same
// way the server attaches its RequestScope to every request.
func scopedContext(base context.Context, scope *RequestScope) context.Context {
	var ctx context.Context
	scope.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		ctx = request.Context()
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(base))
	return ctx
}

// Package request outcomes are request history in the access log. Whatever
// the outcome (hit, miss, server error, refusal, interrupted body), none is
// chained into the audit log; only policy blocks and management events are.
func TestPackageRequestOutcomesStayOutOfTheAuditLog(t *testing.T) {
	recorder := &timestampAccessRecorder{}
	audit := &captureAuditEntries{}
	ctx := scopedContext(requestid.With(context.Background(), "req-outcomes"), NewRequestScope(recorder, audit, nil, nil))

	LogAccess(ctx, nil, "npm", "GET", "npm-exact-v1/react/metadata.json", true, "npmjs", time.Millisecond, http.StatusOK, "192.0.2.1", 10)
	LogAccess(ctx, nil, "pypi", "GET", "pypi/files/requests-2.32.0-py3-none-any.whl", false, "pypi", time.Millisecond, http.StatusOK, "192.0.2.2", 10)
	LogAccess(ctx, nil, "npm", "GET", "npm-exact-v1/react", false, "npmjs", time.Millisecond, http.StatusBadGateway, "192.0.2.3", 0)
	LogRefusedAccess(ctx, nil, "npm", "GET", "npm-exact-v1/fixture/-/fixture-9.9.9.tgz", time.Millisecond, http.StatusNotFound, "192.0.2.4")
	LogAccessDelivery(ctx, nil, "npm", "GET", "npm-exact-v1/react/-/react-19.0.0.tgz", false, "npmjs", time.Millisecond, http.StatusOK, "192.0.2.5", 10, fmt.Errorf("client disconnected"))

	if recorder.event.ClientIP != "192.0.2.5" || recorder.event.RequestID != "req-outcomes" {
		t.Fatalf("access recorder did not receive the requests: %+v", recorder.event)
	}
	if len(audit.entries) != 0 {
		t.Fatalf("package request outcomes reached the audit log: %#v", audit.entries)
	}
}

// Three classifiers decide what a Go cache key is: the cache kind, the
// dependency kind of an access event, and the package-file test behind SBOM
// and project records. The checksum database prefix must mean the same to all
// of them, including for a lookup whose version ends in ".zip".
func TestGoChecksumDatabaseKeysAreMetadataToEveryClassifier(t *testing.T) {
	for _, test := range []struct {
		key      string
		artifact bool
	}{
		{key: db.GoSumDBKeyPrefix + "sum.golang.org/lookup/example.com/mod@v1.0.0-a.zip"},
		{key: db.GoSumDBKeyPrefix + "sum.golang.org/lookup/example.com/mod@v1.0.0"},
		{key: db.GoSumDBKeyPrefix + "sum.golang.org/tile/8/0/x001/234"},
		{key: "go/example.com/mod/@v/v1.0.0.zip", artifact: true},
		{key: "go/Sumdb/mod/@v/v1.0.0.zip", artifact: true},
	} {
		wantKind := db.CacheKindMetadata
		if test.artifact {
			wantKind = db.CacheKindArtifact
		}
		if got := db.ClassifyCacheKind("go", test.key); got != wantKind {
			t.Errorf("db.ClassifyCacheKind(go, %q) = %q, want %q", test.key, got, wantKind)
		}
		if got := accesslog.DependencyRequestKind("go", test.key); got != wantKind {
			t.Errorf("accesslog.DependencyRequestKind(go, %q) = %q, want %q", test.key, got, wantKind)
		}
		if got := packagekey.IsPackageFile("go", test.key); got != test.artifact {
			t.Errorf("packagekey.IsPackageFile(go, %q) = %v, want %v", test.key, got, test.artifact)
		}
	}
}

func TestLogAccessRecordsCorrelatedDiagnosticFacts(t *testing.T) {
	recorder := &timestampAccessRecorder{}
	ctx := scopedContext(requestid.With(context.Background(), "req-facts-1"), NewRequestScope(recorder, nil, nil, nil))
	LogAccess(ctx, nil, "npm", "GET", "npm/react", false, "npmjs", time.Millisecond, http.StatusBadGateway, "127.0.0.1", 0)
	if recorder.event.RequestID != "req-facts-1" || recorder.event.CacheResult != "unknown" ||
		recorder.event.CacheReason != "not_recorded" || recorder.event.PolicyDecision != "unknown" ||
		recorder.event.DeliveryResult != "unknown" {
		t.Fatalf("diagnostic event = %+v", recorder.event)
	}
}

func TestLogAccessPrefersAuthenticatedArtifactCoordinate(t *testing.T) {
	recorder := &timestampAccessRecorder{}
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := httptest.NewRequest(http.MethodGet, "/npm/fixture/-/opaque/pkg-custom.tgz", nil)
	ginContext.Request = request.WithContext(scopedContext(request.Context(), NewRequestScope(recorder, nil, nil, nil)))
	BindAuthenticatedArtifactCoordinate(ginContext, "npm", "fixture", "1.0.0")
	LogAccess(
		ginContext.Request.Context(), nil, "npm", http.MethodGet,
		"npm-exact-v1/fixture/-/__depsilo_tarball_v1/objects/digest/pkg-custom.tgz",
		false, "primary", time.Millisecond, http.StatusOK, "192.0.2.10", 10,
	)

	if recorder.event.PackageName != "fixture" {
		t.Fatalf("recorded package = %q, want authenticated fixture", recorder.event.PackageName)
	}
}

func TestLogRefusedAccessRecordsTheRequestedKey(t *testing.T) {
	recorder := &timestampAccessRecorder{}
	ctx := scopedContext(context.Background(), NewRequestScope(recorder, nil, nil, nil))

	const key = "npm-exact-v1/fixture/-/fixture-9.9.9.tgz"
	LogRefusedAccess(ctx, nil, "npm", http.MethodGet, key, time.Millisecond, http.StatusNotFound, "192.0.2.10")

	event := recorder.event
	if event.AdapterType != "npm" || event.CacheKey != key || event.PackageName != "fixture" ||
		event.StatusCode != http.StatusNotFound || event.Hit || event.Upstream != "" || event.BytesSent != 0 ||
		event.CacheResult != "unknown" {
		t.Fatalf("access event = %+v", event)
	}
	// Readers name no version for a refused response: 9.9.9 is the client's
	// claim, not an identity Depsilo verified.
	if version := accesslog.RecordedVersion(event.AdapterType, event.CacheKey, event.StatusCode); version != "" {
		t.Fatalf("refused request reads as version %q", version)
	}
}

func TestLogPolicyBlockUsesUnifiedAuditOutcome(t *testing.T) {
	audit := &captureAuditEntries{}
	ctx := scopedContext(context.Background(), NewRequestScope(nil, audit, nil, nil))
	LogPolicyBlock(ctx, "pypi", "requests", "", http.StatusForbidden, "192.0.2.1")
	LogPolicyBlock(ctx, "npm", "blocked-package", "1.0.0", http.StatusUnavailableForLegalReasons, "192.0.2.2")

	if len(audit.entries) != 2 {
		t.Fatalf("audit entries = %d, want 2", len(audit.entries))
	}
	for index, entry := range audit.entries {
		if entry.CacheResult != "blocked" || entry.PackageName == "" || entry.CreatedAt.IsZero() {
			t.Errorf("entry %d = %#v", index, entry)
		}
	}
	if audit.entries[0].Action != "metadata" || audit.entries[0].StatusCode != http.StatusForbidden {
		t.Errorf("metadata block = %#v", audit.entries[0])
	}
	if audit.entries[1].Action != "download" || audit.entries[1].StatusCode != http.StatusUnavailableForLegalReasons {
		t.Errorf("artifact block = %#v", audit.entries[1])
	}
}

// A RequestScope is the only source of access hooks. A scope that binds no
// recorder, and a request with no scope at all, both write the raw access row.
func TestLogAccessWithoutScopedRecorderWritesRawRow(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "scoped-access.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&db.AccessLog{}); err != nil {
		t.Fatalf("migrate access log: %v", err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatalf("access database pool: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDatabase.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	logRaw := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		clientIP := request.URL.Query().Get("client")
		LogAccess(request.Context(), database, "npm", "GET", "npm/scoped-package", false, "upstream", time.Millisecond, 200, clientIP, 12)
	})
	NewRequestScope(nil, nil, nil, nil).Wrap(logRaw).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/package?client=scoped-raw", nil))
	logRaw.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/package?client=unscoped-raw", nil))

	for _, clientIP := range []string{"scoped-raw", "unscoped-raw"} {
		var count int64
		if err := database.Model(&db.AccessLog{}).Where("client_ip = ?", clientIP).Count(&count).Error; err != nil {
			t.Fatalf("count raw access logs: %v", err)
		}
		if count != 1 {
			t.Fatalf("%s raw access log count = %d, want 1", clientIP, count)
		}
	}
}

func TestSuppressAccessLoggingSkipsRawAndScopedHooks(t *testing.T) {
	calls := newCapturedHookCalls()
	scope := NewRequestScope(
		&captureAccessRecorder{owner: 9, calls: calls},
		&captureAuditLogger{owner: 9, calls: calls},
		nil, nil,
	)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "suppressed-access.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.AccessLog{}); err != nil {
		t.Fatal(err)
	}

	handler := scope.Wrap(SuppressAccessLogging(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		LogAccess(request.Context(), database, "pypi", "GET", "pypi/simple/pillow/index.html", false, "primary", time.Millisecond, 200, "internal-refresh", 12)
		LogPolicyBlock(request.Context(), "pypi", "pillow", "", http.StatusForbidden, "internal-refresh")
	})))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/internal-refresh", nil))

	var rawRows int64
	if err := database.Model(&db.AccessLog{}).Count(&rawRows).Error; err != nil {
		t.Fatal(err)
	}
	if rawRows != 0 {
		t.Fatalf("suppressed request wrote %d raw access rows", rawRows)
	}
	calls.mu.Lock()
	defer calls.mu.Unlock()
	if len(calls.recorder) != 0 || len(calls.audit) != 0 {
		t.Fatalf("suppressed request reached scoped hooks: recorder=%v audit=%v", calls.recorder, calls.audit)
	}
}

func TestLogAccessDeliveryCarriesArtifactKindAndActualCompletion(t *testing.T) {
	recorder := &timestampAccessRecorder{}
	ctx := scopedContext(context.Background(), NewRequestScope(recorder, nil, nil, nil))
	for _, test := range []struct {
		key    string
		err    error
		kind   string
		result string
	}{
		{"npm/pkg/-/pkg-1.0.0.tgz", nil, db.CacheKindArtifact, "completed"},
		{"npm/pkg/-/pkg-1.0.0.tgz", fmt.Errorf("client disconnected"), db.CacheKindArtifact, "failed"},
		{"npm/pkg/metadata.json", nil, db.CacheKindMetadata, "completed"},
	} {
		LogAccessDelivery(ctx, nil, "npm", "GET", test.key, false, "npmjs", time.Millisecond, http.StatusOK, "127.0.0.1", 100, test.err)
		if recorder.event.DependencyKind != test.kind || recorder.event.DeliveryResult != test.result {
			t.Fatalf("event = %+v, want kind %s result %s", recorder.event, test.kind, test.result)
		}
	}
}
