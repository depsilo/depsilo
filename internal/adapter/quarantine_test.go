package adapter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

type recordingQuarantineChecker struct {
	owner   int
	allowed bool
	calls   *atomic.Int64
}

func (checker *recordingQuarantineChecker) Check(
	context.Context,
	string,
	string,
	string,
	string,
) QuarantineDecision {
	if checker.calls != nil {
		checker.calls.Add(1)
	}
	return QuarantineDecision{
		Allowed: checker.allowed,
		Code:    fmt.Sprintf("OWNER_%d", checker.owner),
		Reason:  fmt.Sprintf("owner %d decision", checker.owner),
	}
}

// runQuarantineGate runs the gate for one request behind scope.Wrap, the way
// the server attaches its RequestScope. A nil scope sends the request without
// one.
func runQuarantineGate(t *testing.T, scope *RequestScope, pkg string) (bool, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	var blocked bool
	handler := http.Handler(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ginContext, _ := gin.CreateTestContext(writer)
		ginContext.Request = request
		blocked = QuarantineGate(ginContext, "npm", pkg, "1.0.0")
	}))
	if scope != nil {
		handler = scope.Wrap(handler)
	}
	request := httptest.NewRequest(http.MethodGet, "/artifact", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	handler.ServeHTTP(recorder, request)
	return blocked, recorder
}

func TestQuarantineGateUsesScopedChecker(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var calls atomic.Int64
	scope := NewRequestScope(nil, nil, &recordingQuarantineChecker{owner: 1, calls: &calls}, nil)
	blocked, recorder := runQuarantineGate(t, scope, "scoped-package")
	if !blocked || recorder.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(recorder.Body.String(), `"code":"OWNER_1"`) {
		t.Fatalf("scoped checker response: blocked=%v status=%d body=%s", blocked, recorder.Code, recorder.Body.String())
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("checker calls = %d, want 1", got)
	}
}

// The RequestScope is the only source of a checker: a scope that binds none,
// and a request with no scope at all, both pass through unchecked.
func TestQuarantineGateWithoutScopedCheckerAllows(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for name, scope := range map[string]*RequestScope{
		"scoped nil checker": NewRequestScope(nil, nil, nil, nil),
		"no request scope":   nil,
	} {
		if blocked, recorder := runQuarantineGate(t, scope, "unchecked-package"); blocked {
			t.Fatalf("%s blocked request: status=%d body=%s", name, recorder.Code, recorder.Body.String())
		}
	}
}

func TestQuarantineGateRecordsBlockedAuditOutcome(t *testing.T) {
	gin.SetMode(gin.TestMode)

	audit := &captureAuditEntries{}
	scope := NewRequestScope(nil, audit, &recordingQuarantineChecker{owner: 7}, nil)
	blocked, recorder := runQuarantineGate(t, scope, "unsafe-package")
	if !blocked || recorder.Code != http.StatusUnavailableForLegalReasons {
		t.Fatalf("blocked=%v status=%d body=%s", blocked, recorder.Code, recorder.Body.String())
	}
	if len(audit.entries) != 1 {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
	entry := audit.entries[0]
	if entry.Ecosystem != "npm" || entry.PackageName != "unsafe-package" || entry.Version != "1.0.0" ||
		entry.Action != "download" || entry.CacheResult != "blocked" || entry.StatusCode != http.StatusUnavailableForLegalReasons {
		t.Fatalf("audit entry = %#v", entry)
	}
}
