package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/upstreamupdates"
)

type upstreamUpdateListResponse struct {
	Items      []upstreamupdates.HistoryEvent `json:"items"`
	Total      int64                          `json:"total"`
	NextCursor *string                        `json:"next_cursor"`
}

func TestUpstreamUpdateHandlerFiltersAndOrdersDeterministically(t *testing.T) {
	database, router := newUpstreamUpdateTestRouter(t)
	now := time.Now().UTC()
	events := []db.UpstreamUpdateEvent{
		{Ecosystem: "pypi", Upstream: "primary", Package: "pillow", Result: upstreamupdates.ResultUpdated, CreatedAt: now},
		{Ecosystem: "pypi", Upstream: "primary", Package: "pillow", Result: upstreamupdates.ResultUpdated, CreatedAt: now},
		{Ecosystem: "npm", Upstream: "npmjs", Package: "react", Result: upstreamupdates.ResultUnchanged, CreatedAt: now.Add(time.Second)},
	}
	if err := database.Create(&events).Error; err != nil {
		t.Fatal(err)
	}

	response := requestUpstreamUpdates(t, router, "/upstream-updates?ecosystem=pypi&result=updated&package=pill")
	if response.Total != 2 || len(response.Items) != 2 {
		t.Fatalf("response = %+v", response)
	}
	if response.Items[0].ID != events[1].ID || response.Items[1].ID != events[0].ID {
		t.Fatalf("equal-timestamp order = [%d %d], want [%d %d]", response.Items[0].ID, response.Items[1].ID, events[1].ID, events[0].ID)
	}
	if response.NextCursor != nil {
		t.Fatalf("next_cursor = %q, want null", *response.NextCursor)
	}
}

func TestUpstreamUpdateHandlerKeepsExplicitOffsetCompatibility(t *testing.T) {
	database, router := newUpstreamUpdateTestRouter(t)
	now := time.Now().UTC()
	events := []db.UpstreamUpdateEvent{
		{Ecosystem: "pypi", Upstream: "primary", Package: "first", Result: upstreamupdates.ResultUpdated, CreatedAt: now},
		{Ecosystem: "pypi", Upstream: "primary", Package: "second", Result: upstreamupdates.ResultUpdated, CreatedAt: now.Add(time.Second)},
		{Ecosystem: "pypi", Upstream: "primary", Package: "third", Result: upstreamupdates.ResultUpdated, CreatedAt: now.Add(2 * time.Second)},
	}
	if err := database.Create(&events).Error; err != nil {
		t.Fatal(err)
	}

	response := requestUpstreamUpdates(t, router, "/upstream-updates?limit=1&offset=1")
	if response.Total != 3 || len(response.Items) != 1 || response.Items[0].ID != events[1].ID {
		t.Fatalf("offset response = %+v", response)
	}
	if response.NextCursor != nil {
		t.Fatalf("legacy offset next_cursor = %q, want null", *response.NextCursor)
	}
}

func TestUpstreamUpdateHandlerCursorKeepsASnapshotAcrossPages(t *testing.T) {
	database, router := newUpstreamUpdateTestRouter(t)
	now := time.Now().UTC()
	events := make([]db.UpstreamUpdateEvent, 5)
	for i := range events {
		events[i] = db.UpstreamUpdateEvent{
			Ecosystem: "pypi",
			Upstream:  "primary",
			Package:   "package-" + string(rune('a'+i)),
			Result:    upstreamupdates.ResultUpdated,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
	}
	if err := database.Create(&events).Error; err != nil {
		t.Fatal(err)
	}

	first := requestUpstreamUpdates(t, router, "/upstream-updates?limit=2")
	if first.Total != 5 || len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("first page = %+v", first)
	}
	if got := []uint{first.Items[0].ID, first.Items[1].ID}; got[0] != events[4].ID || got[1] != events[3].ID {
		t.Fatalf("first page ids = %v", got)
	}

	newer := db.UpstreamUpdateEvent{
		Ecosystem: "pypi",
		Upstream:  "primary",
		Package:   "new-after-snapshot",
		Result:    upstreamupdates.ResultUpdated,
		CreatedAt: now.Add(time.Minute),
	}
	if err := database.Create(&newer).Error; err != nil {
		t.Fatal(err)
	}

	secondPath := "/upstream-updates?limit=2&cursor=" + url.QueryEscape(*first.NextCursor)
	second := requestUpstreamUpdates(t, router, secondPath)
	if second.Total != 5 || len(second.Items) != 2 || second.NextCursor == nil {
		t.Fatalf("second page = %+v", second)
	}
	if got := []uint{second.Items[0].ID, second.Items[1].ID}; got[0] != events[2].ID || got[1] != events[1].ID {
		t.Fatalf("second page ids = %v", got)
	}

	thirdPath := "/upstream-updates?limit=2&cursor=" + url.QueryEscape(*second.NextCursor)
	third := requestUpstreamUpdates(t, router, thirdPath)
	if third.Total != 5 || len(third.Items) != 1 || third.Items[0].ID != events[0].ID || third.NextCursor != nil {
		t.Fatalf("third page = %+v", third)
	}
}

func TestUpstreamUpdateHandlerRejectsInvalidQueries(t *testing.T) {
	database, router := newUpstreamUpdateTestRouter(t)
	event := db.UpstreamUpdateEvent{
		Ecosystem: "pypi", Upstream: "primary", Package: "pillow",
		Result: upstreamupdates.ResultUpdated, CreatedAt: time.Now().UTC(),
	}
	if err := database.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	first := requestUpstreamUpdates(t, router, "/upstream-updates?limit=1&result=updated")
	if first.NextCursor != nil {
		// One item cannot produce a next page, so add another row and recapture a cursor.
		t.Fatal("single-item result unexpectedly returned a cursor")
	}
	event.ID = 0
	event.Package = "numpy"
	event.CreatedAt = event.CreatedAt.Add(time.Second)
	if err := database.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	first = requestUpstreamUpdates(t, router, "/upstream-updates?limit=1&result=updated")
	if first.NextCursor == nil {
		t.Fatal("first page did not return a cursor")
	}

	tests := []struct {
		name string
		path string
	}{
		{name: "unknown result", path: "/upstream-updates?result=surprise"},
		{name: "damaged cursor", path: "/upstream-updates?cursor=not-a-history-cursor"},
		{name: "cursor and offset", path: "/upstream-updates?offset=0&cursor=" + url.QueryEscape(*first.NextCursor)},
		{name: "cursor filter mismatch", path: "/upstream-updates?result=unchanged&cursor=" + url.QueryEscape(*first.NextCursor)},
		{name: "oversized filter", path: "/upstream-updates?ecosystem=" + strings.Repeat("e", 33)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
			}
			if strings.Contains(strings.ToLower(recorder.Body.String()), "cursor") {
				t.Fatalf("response leaks cursor details: %s", recorder.Body.String())
			}
		})
	}
}

func TestUpstreamUpdateHandlerDoesNotExposeStorageErrors(t *testing.T) {
	database, router := newUpstreamUpdateTestRouter(t)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/upstream-updates", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if got, want := recorder.Body.String(), `{"code":"INTERNAL","message":"failed to list upstream update history"}`; strings.TrimSpace(got) != want {
		t.Fatalf("body = %s, want generic response %s", got, want)
	}
}

func newUpstreamUpdateTestRouter(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "updates.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/upstream-updates", NewUpstreamUpdateHandler(database).List)
	return database, router
}

func requestUpstreamUpdates(t *testing.T, router http.Handler, path string) upstreamUpdateListResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var response upstreamUpdateListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}
