package maven

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter"
)

type recordingQuarantineChecker struct {
	ecosystem string
	packageID string
	version   string
}

func (checker *recordingQuarantineChecker) Check(
	_ context.Context,
	ecosystem, packageID, version, _ string,
) adapter.QuarantineDecision {
	checker.ecosystem = ecosystem
	checker.packageID = packageID
	checker.version = version
	return adapter.QuarantineDecision{Allowed: false, Code: "TEST_BLOCK", Reason: "blocked by test"}
}

func TestAARArtifactPassesThroughMavenQuarantineGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	checker := &recordingQuarantineChecker{}
	handler := &Handler{}
	router := gin.New()
	handler.Register(router.Group("/maven"))
	scoped := adapter.NewRequestScope(nil, nil, checker, nil).Wrap(router)

	recorder := httptest.NewRecorder()
	scoped.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet,
		"/maven/com/android/support/appcompat-v7/28.0.0/appcompat-v7-28.0.0.aar",
		nil,
	))

	if recorder.Code != http.StatusUnavailableForLegalReasons {
		t.Fatalf("AAR status = %d, want %d; body = %s", recorder.Code, http.StatusUnavailableForLegalReasons, recorder.Body.String())
	}
	if checker.ecosystem != "maven" || checker.packageID != "com.android.support:appcompat-v7" || checker.version != "28.0.0" {
		t.Fatalf("quarantine identity = %q/%q@%q", checker.ecosystem, checker.packageID, checker.version)
	}
}
