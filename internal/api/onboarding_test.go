package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

func newOnboardingTestHandler(t *testing.T, startedAt time.Time) (*OnboardingHandler, *gorm.DB) {
	t.Helper()
	database := newAPIAuthTestDB(t)
	if err := database.AutoMigrate(&db.ControlPlaneState{}, &db.AuditLog{}); err != nil {
		t.Fatalf("migrate onboarding models: %v", err)
	}
	handler := NewOnboardingHandler(database)
	handler.now = func() time.Time { return startedAt }
	return handler, database
}

func onboardingStatusRequest(t *testing.T, handler *OnboardingHandler, target string) onboardingStatusResponse {
	t.Helper()
	router := gin.New()
	router.GET("/onboarding/status", handler.Status)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, body = %s", target, response.Code, response.Body.String())
	}
	var payload onboardingStatusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode onboarding status: %v", err)
	}
	return payload
}

func onboardingPollURL(afterID uint, startedAt time.Time) string {
	return fmt.Sprintf(
		"/onboarding/status?after_id=%d&started_at=%s",
		afterID,
		url.QueryEscape(startedAt.Format(time.RFC3339Nano)),
	)
}

func TestOnboardingStatusInitializesCursorWithoutReturningHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	startedAt := time.Date(2026, 8, 20, 1, 2, 3, 456, time.UTC)
	handler, database := newOnboardingTestHandler(t, startedAt)
	history := []db.AuditLog{
		{Ecosystem: "pypi", PackageName: "requests", Action: "metadata", CacheResult: "miss", StatusCode: http.StatusOK, CreatedAt: startedAt.Add(-time.Minute)},
		{Ecosystem: "npm", PackageName: "react", Version: "19.0.0", Action: "download", CacheResult: "hit", StatusCode: http.StatusOK, CreatedAt: startedAt.Add(-time.Minute)},
	}
	if err := database.Create(&history).Error; err != nil {
		t.Fatalf("seed audit history: %v", err)
	}

	response := onboardingStatusRequest(t, handler, "/onboarding/status")
	if response.Status != onboardingStatusCompleted {
		t.Fatalf("missing persisted state status = %q, want completed", response.Status)
	}
	if !response.StartedAt.Equal(startedAt) {
		t.Fatalf("started_at = %s, want %s", response.StartedAt, startedAt)
	}
	if len(response.Events) != 0 || response.HasMore {
		t.Fatalf("initial response returned historical events: %#v", response)
	}
	if response.NextAfterID != history[len(history)-1].ID {
		t.Fatalf("next_after_id = %d, want current max %d", response.NextAfterID, history[len(history)-1].ID)
	}
}

func TestOnboardingStatusPollReturnsOnlyNewDependencyEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	startedAt := time.Date(2026, 8, 20, 1, 2, 3, 0, time.UTC)
	handler, database := newOnboardingTestHandler(t, startedAt)
	if err := saveOnboardingStatus(t.Context(), database, onboardingStatusNotStarted); err != nil {
		t.Fatal(err)
	}
	baseline := onboardingStatusRequest(t, handler, "/onboarding/status")

	rows := []db.AuditLog{
		{Ecosystem: "pypi", PackageName: "old", Action: "download", CacheResult: "hit", StatusCode: http.StatusOK, CreatedAt: startedAt.Add(-time.Nanosecond)},
		{Ecosystem: "system", PackageName: "operator", Action: "login", CacheResult: "", StatusCode: http.StatusOK, CreatedAt: startedAt.Add(time.Second)},
		{Ecosystem: "cargo", PackageName: "", Action: "metadata", CacheResult: "miss", StatusCode: http.StatusOK, CreatedAt: startedAt.Add(2 * time.Second)},
		{Ecosystem: "pypi", PackageName: "requests", Action: "metadata", CacheResult: "miss", StatusCode: http.StatusOK, CreatedAt: startedAt.Add(3 * time.Second)},
		{Ecosystem: "npm", PackageName: "blocked-package", Version: "1.0.0", Action: "download", CacheResult: "blocked", StatusCode: http.StatusForbidden, CreatedAt: startedAt.Add(4 * time.Second)},
		{Ecosystem: "npm", PackageName: "broken-package", Version: "2.0.0", Action: "download", CacheResult: "error", StatusCode: http.StatusBadGateway, CreatedAt: startedAt.Add(5 * time.Second)},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed audit events: %v", err)
	}

	response := onboardingStatusRequest(t, handler, onboardingPollURL(baseline.NextAfterID, baseline.StartedAt))
	if response.Status != onboardingStatusNotStarted {
		t.Fatalf("status = %q, want not_started", response.Status)
	}
	if response.HasMore || response.NextAfterID != rows[len(rows)-1].ID {
		t.Fatalf("cursor did not advance to current max: %#v", response)
	}
	if len(response.Events) != 3 {
		t.Fatalf("events = %#v, want only three dependency events", response.Events)
	}
	wantOutcomes := []string{"miss", "blocked", "error"}
	for index, want := range wantOutcomes {
		if response.Events[index].Outcome != want {
			t.Errorf("event %d outcome = %q, want %q", index, response.Events[index].Outcome, want)
		}
		if response.Events[index].PackageName == "" {
			t.Errorf("event %d included an empty package name", index)
		}
	}
	encoded, err := json.Marshal(response.Events[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"client_ip", "user_agent", "upstream_url", "latency_ms", "bytes_sent"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("narrow event exposed %q: %s", forbidden, encoded)
		}
	}
}

func TestOnboardingStatusPaginatesByAuditPrimaryKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	startedAt := time.Now().UTC().Add(-time.Second)
	handler, database := newOnboardingTestHandler(t, startedAt)
	baseline := onboardingStatusRequest(t, handler, "/onboarding/status")

	rows := make([]db.AuditLog, onboardingEventPageSize+1)
	for index := range rows {
		rows[index] = db.AuditLog{
			Ecosystem:   "npm",
			PackageName: fmt.Sprintf("package-%d", index),
			Action:      "download",
			CacheResult: "miss",
			StatusCode:  http.StatusOK,
			CreatedAt:   startedAt.Add(time.Second),
		}
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	irrelevant := db.AuditLog{Ecosystem: "system", Action: "login", CreatedAt: startedAt.Add(time.Second)}
	if err := database.Create(&irrelevant).Error; err != nil {
		t.Fatal(err)
	}

	first := onboardingStatusRequest(t, handler, onboardingPollURL(baseline.NextAfterID, baseline.StartedAt))
	if len(first.Events) != onboardingEventPageSize || !first.HasMore {
		t.Fatalf("first page = %d events, has_more=%v", len(first.Events), first.HasMore)
	}
	if first.NextAfterID != first.Events[len(first.Events)-1].ID {
		t.Fatalf("first page cursor = %d, want last returned ID %d", first.NextAfterID, first.Events[len(first.Events)-1].ID)
	}

	second := onboardingStatusRequest(t, handler, onboardingPollURL(first.NextAfterID, first.StartedAt))
	if len(second.Events) != 1 || second.HasMore {
		t.Fatalf("second page = %#v", second)
	}
	if second.NextAfterID != irrelevant.ID {
		t.Fatalf("short page cursor = %d, want current max %d", second.NextAfterID, irrelevant.ID)
	}
}

func TestOnboardingStatusRejectsIncompleteOrInvalidCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := newOnboardingTestHandler(t, time.Now())
	router := gin.New()
	router.GET("/onboarding/status", handler.Status)

	for _, target := range []string{
		"/onboarding/status?after_id=0",
		"/onboarding/status?started_at=2026-08-20T00%3A00%3A00Z",
		"/onboarding/status?after_id=-1&started_at=2026-08-20T00%3A00%3A00Z",
		"/onboarding/status?after_id=0&started_at=not-a-time",
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("GET %s status = %d, want 400; body=%s", target, response.Code, response.Body.String())
		}
	}
}

func TestOnboardingUpdatePersistsOnlyTerminalStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, database := newOnboardingTestHandler(t, time.Now())
	router := gin.New()
	router.PUT("/onboarding", handler.Update)

	for _, status := range []string{onboardingStatusSkipped, onboardingStatusCompleted} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/onboarding", bytes.NewBufferString(`{"status":"`+status+`"}`))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("PUT %s status = %d, body = %s", status, response.Code, response.Body.String())
		}
		persisted, err := loadOnboardingStatus(t.Context(), database)
		if err != nil || persisted != status {
			t.Fatalf("persisted status = %q, err=%v, want %q", persisted, err, status)
		}
	}

	for _, body := range []string{`{"status":"not_started"}`, `{"status":"unknown"}`, `{}`} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/onboarding", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("PUT body %s status = %d, want 400", body, response.Code)
		}
	}
}
