package adapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/accesslog"
	"depsilo/internal/db"
)

type requestScopeCalls struct {
	mu       sync.Mutex
	recorder map[string]int
	audit    map[string]int
	checker  map[string]int
}

func newRequestScopeCalls() *requestScopeCalls {
	return &requestScopeCalls{
		recorder: make(map[string]int),
		audit:    make(map[string]int),
		checker:  make(map[string]int),
	}
}

type requestScopeRecorder struct {
	owner int
	calls *requestScopeCalls
}

func (recorder *requestScopeRecorder) Record(event accesslog.Event) {
	recorder.calls.mu.Lock()
	recorder.calls.recorder[event.ClientIP] = recorder.owner
	recorder.calls.mu.Unlock()
}

func (*requestScopeRecorder) Flush(context.Context) error { return nil }
func (*requestScopeRecorder) Close(context.Context) error { return nil }

type requestScopeAudit struct {
	owner int
	calls *requestScopeCalls
}

func (audit *requestScopeAudit) Log(entry db.AuditLog) {
	audit.calls.mu.Lock()
	audit.calls.audit[entry.ClientIP] = audit.owner
	audit.calls.mu.Unlock()
}

type requestScopeChecker struct {
	owner int
	calls *requestScopeCalls
}

func (checker *requestScopeChecker) Check(_ context.Context, _, pkg, _, _ string) QuarantineDecision {
	checker.calls.mu.Lock()
	checker.calls.checker[pkg] = checker.owner
	checker.calls.mu.Unlock()
	return QuarantineDecision{Allowed: true}
}

func TestRequestScopesCanServeConcurrentlyWithoutMixingOwners(t *testing.T) {
	gin.SetMode(gin.TestMode)
	accessHooks.Store(nil)
	quarantineHooks.Store(nil)
	t.Cleanup(func() {
		accessHooks.Store(nil)
		quarantineHooks.Store(nil)
	})

	calls := newRequestScopeCalls()
	handler := requestScopeExerciseHandler(nil, nil)
	first := NewRequestScope(
		&requestScopeRecorder{owner: 1, calls: calls},
		&requestScopeAudit{owner: 1, calls: calls},
		&requestScopeChecker{owner: 1, calls: calls},
	).Wrap(handler)
	second := NewRequestScope(
		&requestScopeRecorder{owner: 2, calls: calls},
		&requestScopeAudit{owner: 2, calls: calls},
		&requestScopeChecker{owner: 2, calls: calls},
	).Wrap(handler)

	start := make(chan struct{})
	var wait sync.WaitGroup
	for index, scoped := range []struct {
		owner   int
		client  string
		handler http.Handler
	}{
		{owner: 1, client: "scope-one", handler: first},
		{owner: 2, client: "scope-two", handler: second},
	} {
		index, scoped := index, scoped
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			request := httptest.NewRequest(http.MethodGet, "/artifact?client="+scoped.client, nil)
			recorder := httptest.NewRecorder()
			scoped.handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusNoContent {
				t.Errorf("request %d status = %d, want 204", index, recorder.Code)
			}
		}()
	}
	close(start)
	wait.Wait()

	assertRequestScopeOwner(t, calls, "scope-one", 1)
	assertRequestScopeOwner(t, calls, "scope-two", 2)
}

func TestRequestScopeRemainsFixedDuringLegacyAndOtherScopeReplacement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	accessHooks.Store(nil)
	quarantineHooks.Store(nil)
	t.Cleanup(func() {
		accessHooks.Store(nil)
		quarantineHooks.Store(nil)
	})

	calls := newRequestScopeCalls()
	InstallAccessHooks(
		&requestScopeRecorder{owner: 9, calls: calls},
		&requestScopeAudit{owner: 9, calls: calls},
	)
	InstallQuarantineChecker(&requestScopeChecker{owner: 9, calls: calls})
	entered := make(chan struct{})
	release := make(chan struct{})
	firstHandler := NewRequestScope(
		&requestScopeRecorder{owner: 1, calls: calls},
		&requestScopeAudit{owner: 1, calls: calls},
		&requestScopeChecker{owner: 1, calls: calls},
	).Wrap(requestScopeExerciseHandler(entered, release))
	secondHandler := NewRequestScope(
		&requestScopeRecorder{owner: 2, calls: calls},
		&requestScopeAudit{owner: 2, calls: calls},
		&requestScopeChecker{owner: 2, calls: calls},
	).Wrap(requestScopeExerciseHandler(nil, nil))

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		firstHandler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/artifact?client=long-request", nil))
		firstDone <- recorder
	}()
	awaitRequestScopeSignal(t, entered, "long request entry")

	InstallAccessHooks(
		&requestScopeRecorder{owner: 3, calls: calls},
		&requestScopeAudit{owner: 3, calls: calls},
	)
	InstallQuarantineChecker(&requestScopeChecker{owner: 3, calls: calls})
	secondRecorder := httptest.NewRecorder()
	secondHandler.ServeHTTP(secondRecorder, httptest.NewRequest(http.MethodGet, "/artifact?client=other-scope", nil))
	if secondRecorder.Code != http.StatusNoContent {
		t.Fatalf("other scope status = %d, want 204", secondRecorder.Code)
	}
	legacyRecorder := httptest.NewRecorder()
	requestScopeExerciseHandler(nil, nil).ServeHTTP(
		legacyRecorder,
		httptest.NewRequest(http.MethodGet, "/artifact?client=legacy-replacement", nil),
	)
	if legacyRecorder.Code != http.StatusNoContent {
		t.Fatalf("legacy replacement status = %d, want 204", legacyRecorder.Code)
	}

	close(release)
	select {
	case recorder := <-firstDone:
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("long request status = %d, want 204", recorder.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for long request")
	}

	assertRequestScopeOwner(t, calls, "long-request", 1)
	assertRequestScopeOwner(t, calls, "other-scope", 2)
	assertRequestScopeOwner(t, calls, "legacy-replacement", 3)
}

func requestScopeExerciseHandler(entered chan<- struct{}, release <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if entered != nil {
			close(entered)
		}
		if release != nil {
			<-release
		}
		client := request.URL.Query().Get("client")
		LogAccess(request.Context(), nil, "npm", "GET", "npm/"+client, true, "upstream", time.Millisecond, http.StatusOK, client, 10)

		ginContext, _ := gin.CreateTestContext(writer)
		ginContext.Request = request
		if !QuarantineGate(ginContext, "npm", client, "1.0.0") {
			writer.WriteHeader(http.StatusNoContent)
		}
	})
}

func assertRequestScopeOwner(t *testing.T, calls *requestScopeCalls, key string, want int) {
	t.Helper()
	calls.mu.Lock()
	defer calls.mu.Unlock()
	if got := calls.recorder[key]; got != want {
		t.Errorf("%s recorder owner = %d, want %d", key, got, want)
	}
	if got := calls.audit[key]; got != want {
		t.Errorf("%s audit owner = %d, want %d", key, got, want)
	}
	if got := calls.checker[key]; got != want {
		t.Errorf("%s checker owner = %d, want %d", key, got, want)
	}
}

func awaitRequestScopeSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
