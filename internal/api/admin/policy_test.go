package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"depsilo/internal/rules"
	"github.com/gin-gonic/gin"
)

type policyStatusProviderFixture struct {
	status rules.PolicyStatus
}

func (fixture policyStatusProviderFixture) PolicyStatus() rules.PolicyStatus {
	return fixture.status
}

func TestPolicyStatusReturnsFreshnessAndFallbackDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	loadedAt := time.Now().Add(-3 * time.Minute).UTC()
	handler := NewPolicyHandler(policyStatusProviderFixture{status: rules.PolicyStatus{
		Status:                "degraded",
		Degraded:              true,
		UsingStaleSnapshot:    true,
		LastSuccessfulRefresh: loadedAt,
		RefreshFailures:       4,
		OnLoadError:           rules.OnLoadErrorUseStaleThenAllow,
	}})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/policy/status", nil)
	handler.Status(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Status                string  `json:"status"`
		UsingStaleSnapshot    bool    `json:"using_stale_snapshot"`
		LastSuccessfulRefresh *string `json:"last_successful_refresh"`
		SnapshotAgeSeconds    float64 `json:"snapshot_age_seconds"`
		RefreshFailures       uint64  `json:"refresh_failures"`
		OnLoadError           string  `json:"on_load_error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
	}
	if body.Status != "degraded" || !body.UsingStaleSnapshot {
		t.Fatalf("policy state = %#v, want degraded/stale", body)
	}
	if body.LastSuccessfulRefresh == nil || *body.LastSuccessfulRefresh != loadedAt.Format(time.RFC3339Nano) {
		t.Fatalf("last_successful_refresh = %#v, want %s", body.LastSuccessfulRefresh, loadedAt.Format(time.RFC3339Nano))
	}
	if body.SnapshotAgeSeconds < 179 || body.SnapshotAgeSeconds > 190 {
		t.Fatalf("snapshot_age_seconds = %v, want approximately 180", body.SnapshotAgeSeconds)
	}
	if body.RefreshFailures != 4 || body.OnLoadError != string(rules.OnLoadErrorUseStaleThenAllow) {
		t.Fatalf("fallback details = failures:%d mode:%q", body.RefreshFailures, body.OnLoadError)
	}
}

func TestPolicyStatusWithoutProviderIsExplicitlyUnknown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/policy/status", nil)
	NewPolicyHandler(nil).Status(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "unknown" || body["using_stale_snapshot"] != false {
		t.Fatalf("unknown provider response = %#v", body)
	}
}

func TestPolicyStatusWithProviderButNoSnapshotIsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/policy/status", nil)
	NewPolicyHandler(policyStatusProviderFixture{}).Status(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "unavailable" || body["using_stale_snapshot"] != false {
		t.Fatalf("empty provider response = %#v, want unavailable/non-stale", body)
	}
}
