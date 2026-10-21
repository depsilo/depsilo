package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/db"
)

func onboardingDeliveryPollURL(previous onboardingStatusResponse) string {
	return onboardingPollURL(previous.NextAfterID, previous.StartedAt) +
		"&delivery_after_id=" + url.QueryEscape(previous.NextDeliveryAfterID)
}

func onboardingDeliveryFixture(requestID string, at time.Time) (db.AuditLog, db.AccessLog) {
	audit := db.AuditLog{
		RequestID: requestID, Ecosystem: "npm", PackageName: "fixture", Version: "1.0.0",
		Action: "download", CacheResult: "miss", StatusCode: 200, BytesSent: 128, CreatedAt: at,
	}
	access := db.AccessLog{
		RequestID: requestID, AdapterType: "npm", PackageName: "fixture", Method: "GET",
		CacheKey: "npm/fixture/-/fixture-1.0.0.tgz", CacheResult: "miss",
		StatusCode: 200, BytesSent: 128, DeliveryResult: "completed", CreatedAt: at.Add(time.Second),
	}
	return audit, access
}

func TestOnboardingDeliveriesUseCompletedArtifactEvidence(t *testing.T) {
	startedAt := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	handler, database := newOnboardingTestHandler(t, startedAt)
	baseline := onboardingStatusRequest(t, handler, "/onboarding/status")
	cases := []struct {
		name   string
		change func(*db.AccessLog)
		want   bool
	}{
		{"download", func(*db.AccessLog) {}, true},
		{"hit", func(row *db.AccessLog) { row.CacheResult, row.Hit = "hit", true }, true},
		{"partial response delivered", func(row *db.AccessLog) { row.StatusCode = 206 }, true},
		{"metadata hit", func(row *db.AccessLog) { row.CacheKey, row.CacheResult = "npm/fixture/metadata.json", "hit" }, false},
		{"head", func(row *db.AccessLog) { row.Method = "HEAD" }, false},
		{"interrupted stream", func(row *db.AccessLog) { row.DeliveryResult = "failed" }, false},
		{"old completion unknown", func(row *db.AccessLog) { row.DeliveryResult = "" }, false},
		{"zero bytes", func(row *db.AccessLog) { row.BytesSent = 0 }, false},
		{"http failure", func(row *db.AccessLog) { row.StatusCode = 502 }, false},
		{"unknown cache outcome", func(row *db.AccessLog) { row.CacheResult = "" }, false},
		{"empty key", func(row *db.AccessLog) { row.CacheKey = "" }, false},
		{"unknown payload", func(row *db.AccessLog) {
			row.AdapterType, row.CacheKey = "maven", "maven/org/fixture/1/fixture-1.custom"
		}, false},
		{"immutable manifest is metadata", func(row *db.AccessLog) {
			row.AdapterType, row.CacheKey, row.CacheResult = "docker", "docker/hub/manifests/fixture/sha256__abc", "hit"
		}, false},
		{"mutable snapshot is artifact", func(row *db.AccessLog) {
			row.AdapterType, row.CacheKey = "maven", "maven/org/fixture/1-SNAPSHOT/fixture-1-SNAPSHOT.jar"
		}, true},
	}
	for index, test := range cases {
		audit, access := onboardingDeliveryFixture(fmt.Sprintf("request-%d", index), startedAt.Add(time.Second))
		test.change(&access)
		audit.Ecosystem = access.AdapterType
		if err := database.Create(&audit).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Create(&access).Error; err != nil {
			t.Fatal(err)
		}
	}
	response := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(baseline))
	if len(response.DeliveryEvents) != len(cases) {
		t.Fatalf("delivery evidence = %+v", response.DeliveryEvents)
	}
	for index, test := range cases {
		event := response.DeliveryEvents[index]
		if event.SuccessfulArtifactDownload != test.want {
			t.Errorf("%s evidence = %+v, want success=%t", test.name, event, test.want)
		}
		if event.RequestID == "" || event.ID == 0 || event.Ecosystem == "" ||
			event.PackageName != "fixture" || event.Version != "1.0.0" ||
			!event.CreatedAt.Equal(startedAt.Add(time.Second)) {
			t.Errorf("%s lost correlated request facts: %+v", test.name, event)
		}
	}
	again := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(response))
	if len(again.DeliveryEvents) != 0 || again.DeliveriesHasMore {
		t.Fatalf("delivery replayed after advancing cursor: %+v", again)
	}
}

func TestOnboardingDeliveryCursorObservesEitherCommitOrder(t *testing.T) {
	for _, auditFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("audit_first=%t", auditFirst), func(t *testing.T) {
			startedAt := time.Now().UTC()
			handler, database := newOnboardingTestHandler(t, startedAt)
			baseline := onboardingStatusRequest(t, handler, "/onboarding/status")
			audit, access := onboardingDeliveryFixture("correlated", startedAt.Add(time.Second))
			first, second := any(&access), any(&audit)
			if auditFirst {
				first, second = &audit, &access
			}
			if err := database.Create(first).Error; err != nil {
				t.Fatal(err)
			}
			pending := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(baseline))
			if len(pending.DeliveryEvents) != 0 {
				t.Fatalf("partial evidence proved delivery: %+v", pending)
			}
			if err := database.Create(second).Error; err != nil {
				t.Fatal(err)
			}
			complete := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(pending))
			if len(complete.DeliveryEvents) != 1 || !complete.DeliveryEvents[0].SuccessfulArtifactDownload {
				t.Fatalf("late evidence was lost after advancing cursor: %+v", complete)
			}
		})
	}
}

func TestOnboardingDeliveryRequiresNewCorrelatedRequestStart(t *testing.T) {
	startedAt := time.Now().UTC()
	handler, database := newOnboardingTestHandler(t, startedAt)
	oldAudit, oldAccess := onboardingDeliveryFixture("historical", startedAt.Add(-time.Minute))
	if err := database.Create(&oldAudit).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&oldAccess).Error; err != nil {
		t.Fatal(err)
	}
	baseline := onboardingStatusRequest(t, handler, "/onboarding/status")
	for index, test := range []struct {
		name   string
		change func(*db.AuditLog, *db.AccessLog)
	}{
		{"in flight before baseline", func(audit *db.AuditLog, access *db.AccessLog) {
			audit.CreatedAt = startedAt.Add(-time.Nanosecond)
			access.CreatedAt = startedAt.Add(time.Second)
		}},
		{"uncorrelated old rows", func(audit *db.AuditLog, access *db.AccessLog) {
			audit.RequestID, access.RequestID = "", ""
		}},
		{"different ecosystem", func(_ *db.AuditLog, access *db.AccessLog) { access.AdapterType = "pypi" }},
		{"different package", func(_ *db.AuditLog, access *db.AccessLog) { access.PackageName = "different" }},
		{"operator event", func(audit *db.AuditLog, _ *db.AccessLog) { audit.Action = "login" }},
	} {
		audit, access := onboardingDeliveryFixture(fmt.Sprintf("rejected-%d", index), startedAt.Add(time.Second))
		test.change(&audit, &access)
		if err := database.Create(&audit).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Create(&access).Error; err != nil {
			t.Fatal(err)
		}
	}
	response := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(baseline))
	if len(response.DeliveryEvents) != 0 {
		t.Fatalf("non-session evidence = %+v", response.DeliveryEvents)
	}
	// Existing consumers keep their audit cursor semantics and do not opt in
	// to a delivery scan by merely reading the new response fields.
	legacy := onboardingStatusRequest(t, handler, onboardingPollURL(baseline.NextAfterID, baseline.StartedAt))
	if len(legacy.Events) == 0 || len(legacy.DeliveryEvents) != 0 || legacy.NextDeliveryAfterID != "" {
		t.Fatalf("legacy polling contract changed: %+v", legacy)
	}
}

func TestOnboardingDeliveryPagesKeepLateAssociations(t *testing.T) {
	startedAt := time.Now().UTC()
	handler, database := newOnboardingTestHandler(t, startedAt)
	baseline := onboardingStatusRequest(t, handler, "/onboarding/status")
	lateAudit, earlyAccess := onboardingDeliveryFixture("late-audit", startedAt.Add(time.Second))
	earlyAudit, lateAccess := onboardingDeliveryFixture("late-access", startedAt.Add(time.Second))
	if err := database.Create(&earlyAccess).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&earlyAudit).Error; err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= onboardingEventPageSize; index++ {
		audit, access := onboardingDeliveryFixture(fmt.Sprintf("page-%d", index), startedAt.Add(time.Second))
		if err := database.Create(&audit).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Create(&access).Error; err != nil {
			t.Fatal(err)
		}
	}
	first := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(baseline))
	if len(first.DeliveryEvents) != onboardingEventPageSize || !first.DeliveriesHasMore {
		t.Fatalf("first delivery page = %+v", first)
	}
	// Both directions arrive after the first page took its upper bounds.
	if err := database.Create(&lateAudit).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&lateAccess).Error; err != nil {
		t.Fatal(err)
	}
	second := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(first))
	if len(second.DeliveryEvents) != 1 || second.DeliveriesHasMore {
		t.Fatalf("second page escaped its snapshot: %+v", second)
	}
	third := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(second))
	if len(third.DeliveryEvents) != 2 || third.DeliveriesHasMore {
		t.Fatalf("late paired evidence disappeared: %+v", third)
	}
	seen := map[uint]bool{}
	for _, page := range []onboardingStatusResponse{first, second, third} {
		for _, event := range page.DeliveryEvents {
			if seen[event.ID] {
				t.Errorf("duplicate access ID %d", event.ID)
			}
			seen[event.ID] = true
		}
	}
}

func TestOnboardingDeliveryRejectsMalformedOrUnboundedCursors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := newOnboardingTestHandler(t, time.Now())
	router := gin.New()
	router.GET("/onboarding/status", handler.Status)
	tokens := []string{"", "not-base64!", strings.Repeat("a", maxOnboardingDeliveryCursorLength+1)}
	for _, raw := range []string{
		`{}`, `{"v":2,"a":0,"x":0}`, `{"v":1,"a":-1,"x":0}`,
		`{"v":1,"a":0.5,"x":0}`, `{"v":1,"a":9007199254740992,"x":0}`,
		`{"v":1,"a":0,"x":0,"p":true}`,
		`{"v":1,"a":4,"x":0,"p":true,"at":3,"xt":1,"la":3,"lx":1}`,
		`{"v":1,"a":0,"x":0,"p":true,"at":1,"xt":1,"la":2,"lx":1}`,
		`{"v":1,"a":1,"x":1,"p":true,"at":1,"xt":1,"la":1,"lx":1}`,
		`{"v":1,"a":0,"x":0,"at":1}`,
	} {
		tokens = append(tokens, base64.RawURLEncoding.EncodeToString([]byte(raw)))
	}
	for _, token := range tokens {
		target := onboardingPollURL(0, time.Now().UTC()) + "&delivery_after_id=" + url.QueryEscape(token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("delivery cursor %q status=%d body=%s", token, response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/onboarding/status?delivery_after_id=invalid", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("partial delivery cursor status=%d", response.Code)
	}
}
