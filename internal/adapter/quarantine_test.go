package adapter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

type recordingQuarantineChecker struct {
	owner   int
	allowed bool
	calls   *atomic.Int64
	onCheck func()
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
	if checker.onCheck != nil {
		checker.onCheck()
	}
	return QuarantineDecision{
		Allowed: checker.allowed,
		Code:    fmt.Sprintf("OWNER_%d", checker.owner),
		Reason:  fmt.Sprintf("owner %d decision", checker.owner),
	}
}

func runQuarantineGate(t *testing.T, pkg string) (bool, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/artifact", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	ginContext.Request = request
	return QuarantineGate(ginContext, "npm", pkg, "1.0.0"), recorder
}

func TestInstallQuarantineCheckerReleaseIsOwnerAware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	quarantineHooks.Store(nil)
	t.Cleanup(func() { quarantineHooks.Store(nil) })

	second := &recordingQuarantineChecker{owner: 2}
	var releaseFirst func()
	var releaseSecond func()
	var replaceOnce sync.Once
	first := &recordingQuarantineChecker{owner: 1}
	first.onCheck = func() {
		replaceOnce.Do(func() {
			releaseSecond = InstallQuarantineChecker(second)
			// Releasing the replaced owner must not clear owner 2.
			releaseFirst()
		})
	}
	releaseFirst = InstallQuarantineChecker(first)

	blocked, recorder := runQuarantineGate(t, "first")
	if !blocked || recorder.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(recorder.Body.String(), `"code":"OWNER_1"`) {
		t.Fatalf("first owner response: blocked=%v status=%d body=%s", blocked, recorder.Code, recorder.Body.String())
	}

	blocked, recorder = runQuarantineGate(t, "second")
	if !blocked || recorder.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(recorder.Body.String(), `"code":"OWNER_2"`) {
		t.Fatalf("replacement owner response: blocked=%v status=%d body=%s", blocked, recorder.Code, recorder.Body.String())
	}

	releaseFirst()
	if quarantineHooks.Load() == nil {
		t.Fatal("releasing replaced owner cleared the current checker")
	}
	releaseSecond()
	releaseSecond()
	if quarantineHooks.Load() != nil {
		t.Fatal("current owner release did not clear its checker")
	}
	blocked, _ = runQuarantineGate(t, "after-release")
	if blocked {
		t.Fatal("gate remained active after current owner release")
	}
}

func TestQuarantineCheckerConcurrentInstallAndGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	quarantineHooks.Store(nil)
	t.Cleanup(func() { quarantineHooks.Store(nil) })

	var calls atomic.Int64
	InstallQuarantineChecker(&recordingQuarantineChecker{owner: 0, calls: &calls})

	const (
		installers = 8
		installs   = 250
		gates      = 1000
	)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for installer := range installers {
		wait.Add(1)
		go func(ownerBase int) {
			defer wait.Done()
			<-start
			for offset := range installs {
				owner := ownerBase*installs + offset + 1
				InstallQuarantineChecker(&recordingQuarantineChecker{
					owner:   owner,
					allowed: owner%2 == 0,
					calls:   &calls,
				})
			}
		}(installer)
	}
	for index := range gates {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			blocked, recorder := runQuarantineGate(t, fmt.Sprintf("pkg-%d", index))
			if blocked && recorder.Code != http.StatusUnavailableForLegalReasons {
				t.Errorf("blocked gate status = %d, want 451", recorder.Code)
			}
			if !blocked && recorder.Code != http.StatusOK {
				t.Errorf("allowed gate status = %d, want 200", recorder.Code)
			}
		}()
	}
	close(start)
	wait.Wait()

	if got := calls.Load(); got != gates {
		t.Fatalf("checker calls = %d, want %d", got, gates)
	}
}

func TestSetQuarantineCheckerCompatibilityIsConcurrentSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	quarantineHooks.Store(nil)
	t.Cleanup(func() { quarantineHooks.Store(nil) })

	var calls atomic.Int64
	SetQuarantineChecker(&recordingQuarantineChecker{owner: 0, calls: &calls})

	const iterations = 400
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := range iterations {
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			SetQuarantineChecker(&recordingQuarantineChecker{owner: index + 1, calls: &calls})
		}()
		go func() {
			defer wait.Done()
			<-start
			runQuarantineGate(t, fmt.Sprintf("compat-%d", index))
		}()
	}
	close(start)
	wait.Wait()

	SetQuarantineChecker(nil)
	if quarantineHooks.Load() != nil {
		t.Fatal("SetQuarantineChecker(nil) did not disable the gate")
	}
}

func TestQuarantineGateScopedNilCheckerDoesNotFallBackToGlobal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	quarantineHooks.Store(nil)
	t.Cleanup(func() { quarantineHooks.Store(nil) })

	var globalCalls atomic.Int64
	InstallQuarantineChecker(&recordingQuarantineChecker{owner: 9, calls: &globalCalls})
	emptyScope := NewRequestScope(nil, nil, nil)
	blockedResult := make(chan bool, 1)
	handler := emptyScope.Wrap(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ginContext, _ := gin.CreateTestContext(writer)
		ginContext.Request = request
		blockedResult <- QuarantineGate(ginContext, "npm", "scoped-package", "1.0.0")
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/artifact", nil))
	if blocked := <-blockedResult; blocked {
		t.Fatalf("scoped nil checker blocked request: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := globalCalls.Load(); got != 0 {
		t.Fatalf("scoped nil checker fell back to global checker: calls=%d", got)
	}
}

func TestQuarantineGateRecordsBlockedAuditOutcome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	quarantineHooks.Store(nil)
	accessHooks.Store(nil)
	t.Cleanup(func() {
		quarantineHooks.Store(nil)
		accessHooks.Store(nil)
	})

	audit := &captureAuditEntries{}
	InstallAccessHooks(nil, audit)
	InstallQuarantineChecker(&recordingQuarantineChecker{owner: 7})
	blocked, recorder := runQuarantineGate(t, "unsafe-package")
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
