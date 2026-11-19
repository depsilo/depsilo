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

	"depsilo/internal/api/contract"
	"depsilo/internal/db"
)

func onboardingDeliveryPollURL(previous contract.OnboardingStatusResponse) string {
	return onboardingPollURL(previous.NextAfterID, previous.StartedAt) +
		"&delivery_after_id=" + url.QueryEscape(previous.NextDeliveryAfterID)
}

// onboardingDeliveryFixture is one package request the access log recorded:
// it started at at and completed a second later.
func onboardingDeliveryFixture(requestID string, at time.Time) db.AccessLog {
	return db.AccessLog{
		RequestID: requestID, AdapterType: "npm", PackageName: "fixture", Method: "GET",
		CacheKey: "npm-exact-v1/fixture/-/fixture-1.0.0.tgz", CacheResult: "miss",
		StatusCode: 200, BytesSent: 128, LatencyMs: 1000, DeliveryResult: "completed",
		CreatedAt: at.Add(time.Second),
	}
}

func TestOnboardingDeliveriesUseCompletedArtifactEvidence(t *testing.T) {
	startedAt := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	handler, database := newOnboardingTestHandler(t, startedAt)
	baseline := onboardingStatusRequest(t, handler, "/onboarding/status")
	cases := []struct {
		name    string
		change  func(*db.AccessLog)
		want    bool
		version string
	}{
		{"download", func(*db.AccessLog) {}, true, "1.0.0"},
		{"hit", func(row *db.AccessLog) { row.CacheResult, row.Hit = "hit", true }, true, "1.0.0"},
		{"prerelease", func(row *db.AccessLog) { row.CacheKey = "npm-exact-v1/fixture/-/fixture-1.0.0-beta.1.tgz" }, true, "1.0.0-beta.1"},
		{"partial response delivered", func(row *db.AccessLog) { row.StatusCode = 206 }, true, "1.0.0"},
		{"metadata hit", func(row *db.AccessLog) { row.CacheKey, row.CacheResult = "npm-exact-v1/fixture/metadata.json", "hit" }, false, ""},
		{"head", func(row *db.AccessLog) { row.Method = "HEAD" }, false, "1.0.0"},
		{"interrupted stream", func(row *db.AccessLog) { row.DeliveryResult = "failed" }, false, "1.0.0"},
		{"old completion unknown", func(row *db.AccessLog) { row.DeliveryResult = "" }, false, "1.0.0"},
		{"zero bytes", func(row *db.AccessLog) { row.BytesSent = 0 }, false, "1.0.0"},
		// A failed response proved no artifact identity: no version.
		{"http failure", func(row *db.AccessLog) { row.StatusCode = 502 }, false, ""},
		{"refused", func(row *db.AccessLog) { row.StatusCode = 404 }, false, ""},
		{"unknown cache outcome", func(row *db.AccessLog) { row.CacheResult = "" }, false, "1.0.0"},
		{"empty key", func(row *db.AccessLog) { row.CacheKey = "" }, false, ""},
		{"unknown payload", func(row *db.AccessLog) {
			row.AdapterType, row.CacheKey = "maven", "maven/org/fixture/1/fixture-1.custom"
		}, false, "1"},
		{"immutable manifest is metadata", func(row *db.AccessLog) {
			row.AdapterType, row.CacheKey, row.CacheResult = "docker", "docker/hub/manifests/fixture/sha256__abc", "hit"
		}, false, "sha256__abc"},
		{"mutable snapshot is artifact", func(row *db.AccessLog) {
			row.AdapterType, row.CacheKey = "maven", "maven/org/fixture/1-SNAPSHOT/fixture-1-SNAPSHOT.jar"
		}, true, "1-SNAPSHOT"},
	}
	for index, test := range cases {
		access := onboardingDeliveryFixture(fmt.Sprintf("request-%d", index), startedAt.Add(time.Second))
		test.change(&access)
		if err := database.Create(&access).Error; err != nil {
			t.Fatal(err)
		}
	}
	response := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(baseline))
	if len(response.DeliveryEvents) != len(cases) || len(response.Events) != 0 {
		t.Fatalf("delivery evidence = %+v, request events = %+v", response.DeliveryEvents, response.Events)
	}
	for index, test := range cases {
		event := response.DeliveryEvents[index]
		if event.SuccessfulArtifactDownload != test.want {
			t.Errorf("%s evidence = %+v, want success=%t", test.name, event, test.want)
		}
		// created_at is when the request started, not when it completed.
		if event.RequestID == "" || event.ID == 0 || event.Ecosystem == "" ||
			event.PackageName != "fixture" || event.Version != test.version ||
			!event.CreatedAt.Equal(startedAt.Add(time.Second)) {
			t.Errorf("%s lost request facts: %+v, want version %q", test.name, event, test.version)
		}
	}
	again := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(response))
	if len(again.DeliveryEvents) != 0 || again.DeliveriesHasMore {
		t.Fatalf("delivery replayed after advancing cursor: %+v", again)
	}
}

// Request outcomes come from the access log alone. A request row an earlier
// release chained into the audit log neither appears as a request event nor
// pairs with its access row, so a session spanning the upgrade counts each
// request once.
func TestOnboardingRequestOutcomesComeOnlyFromTheAccessLog(t *testing.T) {
	startedAt := time.Now().UTC()
	handler, database := newOnboardingTestHandler(t, startedAt)
	baseline := onboardingStatusRequest(t, handler, "/onboarding/status")
	legacy := db.AuditLog{
		RequestID: "upgraded", Ecosystem: "npm", PackageName: "fixture", Version: "1.0.0",
		Action: "download", CacheResult: "miss", StatusCode: 200, CreatedAt: startedAt.Add(time.Second),
	}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	pending := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(baseline))
	if len(pending.Events) != 0 || len(pending.DeliveryEvents) != 0 {
		t.Fatalf("audit request row was read as a request outcome: %+v", pending)
	}
	access := onboardingDeliveryFixture("upgraded", startedAt.Add(time.Second))
	if err := database.Create(&access).Error; err != nil {
		t.Fatal(err)
	}
	complete := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(pending))
	if len(complete.Events) != 0 || len(complete.DeliveryEvents) != 1 || !complete.DeliveryEvents[0].SuccessfulArtifactDownload {
		t.Fatalf("access row was not the one request outcome: %+v", complete)
	}
}

func TestOnboardingDeliveryRequiresNewRequestStart(t *testing.T) {
	startedAt := time.Now().UTC()
	handler, database := newOnboardingTestHandler(t, startedAt)
	historical := onboardingDeliveryFixture("historical", startedAt.Add(-time.Minute))
	if err := database.Create(&historical).Error; err != nil {
		t.Fatal(err)
	}
	baseline := onboardingStatusRequest(t, handler, "/onboarding/status")
	for index, test := range []struct {
		name   string
		change func(*db.AccessLog)
	}{
		{"in flight before baseline", func(access *db.AccessLog) {
			access.CreatedAt = startedAt.Add(time.Second)
			access.LatencyMs = 1001
		}},
		{"completed before baseline", func(access *db.AccessLog) {
			access.CreatedAt, access.LatencyMs = startedAt.Add(-time.Millisecond), 0
		}},
		{"uncorrelated row", func(access *db.AccessLog) { access.RequestID = "" }},
		{"no package", func(access *db.AccessLog) { access.PackageName = "" }},
	} {
		access := onboardingDeliveryFixture(fmt.Sprintf("rejected-%d", index), startedAt.Add(time.Second))
		test.change(&access)
		if err := database.Create(&access).Error; err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
	response := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(baseline))
	if len(response.DeliveryEvents) != 0 {
		t.Fatalf("non-session evidence = %+v", response.DeliveryEvents)
	}
	// A poll without a delivery cursor does not opt in to a delivery scan.
	current := onboardingDeliveryFixture("current", startedAt.Add(time.Second))
	if err := database.Create(&current).Error; err != nil {
		t.Fatal(err)
	}
	withoutCursor := onboardingStatusRequest(t, handler, onboardingPollURL(baseline.NextAfterID, baseline.StartedAt))
	if len(withoutCursor.DeliveryEvents) != 0 || withoutCursor.NextDeliveryAfterID != "" {
		t.Fatalf("poll without a delivery cursor scanned deliveries: %+v", withoutCursor)
	}
}

func TestOnboardingDeliveryPagesByAccessPrimaryKey(t *testing.T) {
	startedAt := time.Now().UTC()
	handler, database := newOnboardingTestHandler(t, startedAt)
	baseline := onboardingStatusRequest(t, handler, "/onboarding/status")
	for index := 0; index <= onboardingEventPageSize; index++ {
		access := onboardingDeliveryFixture(fmt.Sprintf("page-%d", index), startedAt.Add(time.Second))
		if err := database.Create(&access).Error; err != nil {
			t.Fatal(err)
		}
	}
	first := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(baseline))
	if len(first.DeliveryEvents) != onboardingEventPageSize || !first.DeliveriesHasMore {
		t.Fatalf("first delivery page = %+v", first)
	}
	late := onboardingDeliveryFixture("late", startedAt.Add(time.Second))
	if err := database.Create(&late).Error; err != nil {
		t.Fatal(err)
	}
	second := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(first))
	if len(second.DeliveryEvents) != 2 || second.DeliveriesHasMore {
		t.Fatalf("second page = %+v, want the page remainder and the late request", second)
	}
	third := onboardingStatusRequest(t, handler, onboardingDeliveryPollURL(second))
	if len(third.DeliveryEvents) != 0 || third.DeliveriesHasMore {
		t.Fatalf("third page replayed deliveries: %+v", third)
	}
	seen := map[uint]bool{}
	for _, page := range []contract.OnboardingStatusResponse{first, second} {
		for _, event := range page.DeliveryEvents {
			if seen[event.ID] {
				t.Errorf("duplicate access ID %d", event.ID)
			}
			seen[event.ID] = true
		}
	}
}

// A browser session opened before the upgrade holds a version 1 cursor that
// paired audit and access rows. Its access watermark continues the session.
func TestOnboardingDeliveryContinuesFromVersionOneCursor(t *testing.T) {
	startedAt := time.Now().UTC()
	handler, database := newOnboardingTestHandler(t, startedAt)
	seen := onboardingDeliveryFixture("seen", startedAt.Add(time.Second))
	if err := database.Create(&seen).Error; err != nil {
		t.Fatal(err)
	}
	fresh := onboardingDeliveryFixture("fresh", startedAt.Add(time.Second))
	if err := database.Create(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	legacy := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"v":1,"a":7,"x":%d}`, seen.ID)))
	target := onboardingPollURL(0, startedAt) + "&delivery_after_id=" + url.QueryEscape(legacy)
	response := onboardingStatusRequest(t, handler, target)
	if len(response.DeliveryEvents) != 1 || response.DeliveryEvents[0].RequestID != "fresh" {
		t.Fatalf("version 1 cursor did not continue from its access watermark: %+v", response)
	}
}

func TestOnboardingDeliveryRejectsMalformedOrUnboundedCursors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := newOnboardingTestHandler(t, time.Now())
	router := gin.New()
	router.GET("/onboarding/status", handler.Status)
	tokens := []string{"", "not-base64!", strings.Repeat("a", maxOnboardingDeliveryCursorLength+1)}
	for _, raw := range []string{
		`{}`, `{"v":3,"x":0}`, `{"v":2,"a":1,"x":0}`, `{"v":2,"x":0,"p":true}`,
		`{"v":2,"x":9007199254740992}`, `{"v":1,"a":-1,"x":0}`,
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
