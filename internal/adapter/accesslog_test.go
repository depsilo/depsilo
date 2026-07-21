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

	"depsilo/internal/accesslog"
	"depsilo/internal/db"
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

func (l *captureAuditEntries) Log(entry db.AuditLog) {
	l.entries = append(l.entries, entry)
}

func (l *captureAuditLogger) Log(entry db.AuditLog) {
	l.calls.mu.Lock()
	l.calls.audit[entry.ClientIP] = l.owner
	l.calls.mu.Unlock()
}

func TestInstallAccessHooksReleaseIsOwnerAwareAndLogUsesOneSnapshot(t *testing.T) {
	accessHooks.Store(nil)
	t.Cleanup(func() { accessHooks.Store(nil) })

	calls := newCapturedHookCalls()
	secondRecorder := &captureAccessRecorder{owner: 2, calls: calls}
	secondAudit := &captureAuditLogger{owner: 2, calls: calls}

	var releaseFirst func()
	var releaseSecond func()
	firstRecorder := &captureAccessRecorder{owner: 1, calls: calls}
	firstAudit := &captureAuditLogger{owner: 1, calls: calls}
	firstRecorder.onRecord = func() {
		releaseSecond = InstallAccessHooks(secondRecorder, secondAudit)
		// The first owner no longer owns the installed snapshot, so its release
		// must not clear the replacement.
		releaseFirst()
	}
	releaseFirst = InstallAccessHooks(firstRecorder, firstAudit)

	LogAccess(context.Background(), nil, "pypi", "GET", "pypi/files/pkg.whl", true, "upstream", time.Millisecond, 200, "first", 10)
	LogAccess(context.Background(), nil, "pypi", "GET", "pypi/files/pkg.whl", true, "upstream", time.Millisecond, 200, "second", 10)

	calls.mu.Lock()
	firstRecorderOwner := calls.recorder["first"]
	firstAuditOwner := calls.audit["first"]
	secondRecorderOwner := calls.recorder["second"]
	secondAuditOwner := calls.audit["second"]
	calls.mu.Unlock()
	if firstRecorderOwner != 1 {
		t.Fatalf("first call recorder owner = %d, want 1", firstRecorderOwner)
	}
	if firstAuditOwner != 1 {
		t.Fatalf("first call audit owner = %d, want the same immutable snapshot owner 1", firstAuditOwner)
	}
	if secondRecorderOwner != 2 {
		t.Fatalf("second call recorder owner = %d, want replacement owner 2", secondRecorderOwner)
	}
	if secondAuditOwner != 2 {
		t.Fatalf("second call audit owner = %d, want replacement owner 2", secondAuditOwner)
	}
	releaseFirst() // idempotent and still must not clear owner 2
	if accessHooks.Load() == nil {
		t.Fatal("releasing replaced owner cleared the current snapshot")
	}
	releaseSecond()
	releaseSecond()
	if accessHooks.Load() != nil {
		t.Fatal("current owner release did not clear its snapshot")
	}
}

func TestLogAccessUsesCanonicalCacheKindForAuditAction(t *testing.T) {
	accessHooks.Store(nil)
	t.Cleanup(func() { accessHooks.Store(nil) })

	calls := newCapturedHookCalls()
	audit := &captureAuditEntries{}
	InstallAccessHooks(&captureAccessRecorder{owner: 1, calls: calls}, audit)

	tests := []struct {
		ecosystem string
		cacheKey  string
		want      string
	}{
		{ecosystem: "apt", cacheKey: "apt/ubuntu/dists/jammy/InRelease", want: "metadata"},
		{ecosystem: "apt", cacheKey: "apt/ubuntu/dists/jammy/main/binary-amd64/Packages.gz", want: "metadata"},
		{ecosystem: "alpine", cacheKey: "alpine/v3.20/main/x86_64/APKINDEX.tar.gz", want: "metadata"},
		{ecosystem: "pypi", cacheKey: "pypi/simple/requests/index.html", want: "metadata"},
		{ecosystem: "pypi", cacheKey: "pypi/files/index-helper-1.0.0.whl", want: "download"},
		{ecosystem: "npm", cacheKey: "npm/react/-/react-19.0.0.tgz", want: "download"},
	}

	for index, test := range tests {
		LogAccess(context.Background(), nil, test.ecosystem, "GET", test.cacheKey, false, "upstream", time.Millisecond, 200, fmt.Sprintf("classification-%d", index), 10)
	}
	if len(audit.entries) != len(tests) {
		t.Fatalf("audit entries = %d, want %d", len(audit.entries), len(tests))
	}
	for index, test := range tests {
		if got := audit.entries[index].Action; got != test.want {
			t.Errorf("%s %q action = %q, want %q", test.ecosystem, test.cacheKey, got, test.want)
		}
		if audit.entries[index].CreatedAt.IsZero() {
			t.Errorf("%s %q has zero event timestamp", test.ecosystem, test.cacheKey)
		}
	}
}

func TestAccessHooksConcurrentReplacementAndLogging(t *testing.T) {
	accessHooks.Store(nil)
	t.Cleanup(func() { accessHooks.Store(nil) })

	calls := newCapturedHookCalls()
	InstallAccessHooks(
		&captureAccessRecorder{owner: 0, calls: calls},
		&captureAuditLogger{owner: 0, calls: calls},
	)

	const (
		installers = 8
		installs   = 200
		loggers    = 800
	)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for installer := range installers {
		wg.Add(1)
		go func(ownerBase int) {
			defer wg.Done()
			<-start
			for offset := range installs {
				owner := ownerBase*installs + offset + 1
				InstallAccessHooks(
					&captureAccessRecorder{owner: owner, calls: calls},
					&captureAuditLogger{owner: owner, calls: calls},
				)
			}
		}(installer)
	}
	for index := range loggers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			clientIP := fmt.Sprintf("client-%d", index)
			LogAccess(context.Background(), nil, "npm", "GET", "npm/pkg", false, "upstream", time.Millisecond, 200, clientIP, 10)
		}()
	}
	close(start)
	wg.Wait()

	calls.mu.Lock()
	defer calls.mu.Unlock()
	if len(calls.recorder) != loggers || len(calls.audit) != loggers {
		t.Fatalf("captured recorder/audit calls = %d/%d, want %d/%d", len(calls.recorder), len(calls.audit), loggers, loggers)
	}
	for clientIP, recorderOwner := range calls.recorder {
		if auditOwner := calls.audit[clientIP]; auditOwner != recorderOwner {
			t.Fatalf("%s mixed hook owners: recorder=%d audit=%d", clientIP, recorderOwner, auditOwner)
		}
	}
}

func TestCompatibilityAccessHookSettersAreConcurrentSafe(t *testing.T) {
	accessHooks.Store(nil)
	t.Cleanup(func() { accessHooks.Store(nil) })

	calls := newCapturedHookCalls()
	SetRecorder(&captureAccessRecorder{owner: 0, calls: calls})
	SetAuditLogger(&captureAuditLogger{owner: 0, calls: calls})

	const iterations = 300
	start := make(chan struct{})
	var wg sync.WaitGroup
	for index := range iterations {
		wg.Add(3)
		go func() {
			defer wg.Done()
			<-start
			SetRecorder(&captureAccessRecorder{owner: index + 1, calls: calls})
		}()
		go func() {
			defer wg.Done()
			<-start
			SetAuditLogger(&captureAuditLogger{owner: index + 1, calls: calls})
		}()
		go func() {
			defer wg.Done()
			<-start
			clientIP := fmt.Sprintf("compat-%d", index)
			LogAccess(context.Background(), nil, "npm", "GET", "npm/pkg", false, "upstream", time.Millisecond, 200, clientIP, 10)
		}()
	}
	close(start)
	wg.Wait()
}

func TestLogAccessScopedNilHooksUseRawDatabaseWithoutGlobalFallback(t *testing.T) {
	accessHooks.Store(nil)
	t.Cleanup(func() { accessHooks.Store(nil) })

	calls := newCapturedHookCalls()
	InstallAccessHooks(
		&captureAccessRecorder{owner: 9, calls: calls},
		&captureAuditLogger{owner: 9, calls: calls},
	)

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

	emptyScope := NewRequestScope(nil, nil, nil)
	handler := emptyScope.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		LogAccess(request.Context(), database, "npm", "GET", "npm/scoped-package", false, "upstream", time.Millisecond, 200, "scoped-raw", 12)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/package", nil))

	var count int64
	if err := database.Model(&db.AccessLog{}).Where("client_ip = ?", "scoped-raw").Count(&count).Error; err != nil {
		t.Fatalf("count raw access logs: %v", err)
	}
	if count != 1 {
		t.Fatalf("raw access log count = %d, want 1", count)
	}
	calls.mu.Lock()
	recorderOwner, recorded := calls.recorder["scoped-raw"]
	auditOwner, audited := calls.audit["scoped-raw"]
	calls.mu.Unlock()
	if recorded || recorderOwner != 0 {
		t.Fatalf("scoped nil recorder fell back to global owner %d", recorderOwner)
	}
	if audited || auditOwner != 0 {
		t.Fatalf("scoped nil audit logger fell back to global owner %d", auditOwner)
	}
}

func TestSuppressAccessLoggingSkipsRawAndInstalledHooks(t *testing.T) {
	accessHooks.Store(nil)
	t.Cleanup(func() { accessHooks.Store(nil) })

	calls := newCapturedHookCalls()
	InstallAccessHooks(
		&captureAccessRecorder{owner: 9, calls: calls},
		&captureAuditLogger{owner: 9, calls: calls},
	)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "suppressed-access.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.AccessLog{}); err != nil {
		t.Fatal(err)
	}

	handler := SuppressAccessLogging(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		LogAccess(request.Context(), database, "pypi", "GET", "pypi/simple/pillow/index.html", false, "primary", time.Millisecond, 200, "internal-refresh", 12)
	}))
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
		t.Fatalf("suppressed request reached installed hooks: recorder=%v audit=%v", calls.recorder, calls.audit)
	}
}
