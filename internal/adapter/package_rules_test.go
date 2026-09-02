package adapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type fixedPackageRuleChecker struct {
	decision PackageRuleDecision
}

func (checker fixedPackageRuleChecker) EvaluatePackageRule(
	context.Context, string, string, string,
) PackageRuleDecision {
	return checker.decision
}

func TestPackageRuleGateRecordsExplicitDenyAsPolicyBlock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	audit := &captureAuditEntries{}
	handler := NewRequestScope(
		nil,
		audit,
		nil,
		fixedPackageRuleChecker{decision: PackageRuleDecision{
			Outcome: PackageRuleDeny,
			Reason:  "operator npm deny",
		}},
	).Wrap(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		c, _ := gin.CreateTestContext(writer)
		c.Request = request
		if !PackageRuleGate(c, "npm", "fixture", "1.0.0") {
			writer.WriteHeader(http.StatusNoContent)
		}
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/artifact", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(audit.entries) != 1 {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
	entry := audit.entries[0]
	if entry.Ecosystem != "npm" || entry.PackageName != "fixture" ||
		entry.Version != "1.0.0" || entry.CacheResult != "blocked" ||
		entry.StatusCode != http.StatusForbidden {
		t.Fatalf("audit entry = %#v", entry)
	}
}
