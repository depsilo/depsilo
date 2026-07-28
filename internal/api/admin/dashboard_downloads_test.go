package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
)

func TestDashboardRecentDownloadsReturnsSmallPrivateSnapshot(t *testing.T) {
	database := newRecentDownloadsTestDB(t)
	now := time.Date(2026, 7, 21, 10, 30, 0, 0, time.UTC)
	rows := []db.AuditLog{
		{Ecosystem: "pypi", PackageName: "oldest", Version: "1.0.0", Action: "download", CacheResult: "hit", ClientIP: "10.0.0.1", UserAgent: "secret-agent", UpstreamURL: "https://user:secret@example.test", LatencyMs: 8, BytesSent: 1200, StatusCode: 200, CreatedAt: now.Add(-3 * time.Minute)},
		{Ecosystem: "npm", PackageName: "metadata-only", Action: "metadata", CacheResult: "miss", CreatedAt: now.Add(-2 * time.Minute)},
		{Ecosystem: "npm", PackageName: "middle", Version: "2.0.0", Action: "download", CacheResult: "miss", LatencyMs: 31, BytesSent: 2400, StatusCode: 200, CreatedAt: now.Add(-time.Minute)},
		{Ecosystem: "cargo", PackageName: "newest", Version: "3.0.0", Action: "download", CacheResult: "error", LatencyMs: 55, BytesSent: 0, StatusCode: 502, CreatedAt: now},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	queryCount := 0
	if err := database.Callback().Query().Before("gorm:query").Register("test:count_recent_download_queries", func(*gorm.DB) {
		queryCount++
	}); err != nil {
		t.Fatalf("register query counter: %v", err)
	}

	recorder := performRecentDownloadsRequest(newRecentDownloadsTestRouter(database), "/dashboard/recent-downloads?limit=2")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if queryCount != 1 {
		t.Fatalf("database queries = %d, want one lightweight SELECT", queryCount)
	}
	if strings.Contains(recorder.Body.String(), "10.0.0.1") || strings.Contains(recorder.Body.String(), "secret-agent") || strings.Contains(recorder.Body.String(), "user:secret") {
		t.Fatalf("response leaked private audit fields: %s", recorder.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertExactKeys(t, body, "items")
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(body["items"], &items); err != nil {
		t.Fatalf("decode items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	for _, item := range items {
		assertExactKeys(t, item, "id", "ecosystem", "package_name", "version", "cache_result", "latency_ms", "bytes_sent", "status_code", "created_at")
	}
	var newestID, middleID uint
	if err := json.Unmarshal(items[0]["id"], &newestID); err != nil {
		t.Fatalf("decode newest id: %v", err)
	}
	if err := json.Unmarshal(items[1]["id"], &middleID); err != nil {
		t.Fatalf("decode middle id: %v", err)
	}
	if newestID != rows[3].ID || middleID != rows[2].ID {
		t.Fatalf("ids = [%d, %d], want newest download ids [%d, %d]", newestID, middleID, rows[3].ID, rows[2].ID)
	}
}

func TestDashboardRecentDownloadsLimitIsBounded(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{raw: "", want: defaultRecentDownloadsLimit},
		{raw: "invalid", want: defaultRecentDownloadsLimit},
		{raw: "0", want: 1},
		{raw: "5", want: 5},
		{raw: "999", want: maxRecentDownloadsLimit},
	}
	for _, test := range tests {
		if got := normalizeRecentDownloadsLimit(test.raw); got != test.want {
			t.Errorf("normalizeRecentDownloadsLimit(%q) = %d, want %d", test.raw, got, test.want)
		}
	}
}

func TestDashboardRecentDownloadsQueryUsesActionOrderIndex(t *testing.T) {
	database := newRecentDownloadsTestDB(t)
	var plan []struct {
		Detail string `gorm:"column:detail"`
	}
	if err := database.Raw(
		"EXPLAIN QUERY PLAN SELECT id, ecosystem, package_name, version, cache_result, latency_ms, bytes_sent, status_code, created_at FROM audit_logs WHERE action = ? ORDER BY id DESC LIMIT ?",
		"download",
		3,
	).Scan(&plan).Error; err != nil {
		t.Fatalf("explain recent downloads query: %v", err)
	}
	var details []string
	for _, row := range plan {
		details = append(details, row.Detail)
	}
	joined := strings.Join(details, "\n")
	if !strings.Contains(joined, "idx_audit_action_order") {
		t.Fatalf("recent downloads query does not use the action/order index:\n%s", joined)
	}
}

func TestDashboardRecentDownloadsPropagatesDatabaseErrors(t *testing.T) {
	database := newRecentDownloadsTestDB(t)
	if err := database.Callback().Query().Before("gorm:query").Register("test:fail_recent_download_query", func(tx *gorm.DB) {
		if reflect.TypeOf(tx.Statement.Dest) == reflect.TypeOf((*[]recentDownloadResponse)(nil)) {
			tx.AddError(errors.New("forced recent download failure"))
		}
	}); err != nil {
		t.Fatalf("register failure callback: %v", err)
	}

	recorder := performRecentDownloadsRequest(newRecentDownloadsTestRouter(database), "/dashboard/recent-downloads")
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if recorder.Code != http.StatusInternalServerError || body["code"] != "DB_ERROR" || !strings.Contains(body["message"].(string), "forced recent download failure") {
		t.Fatalf("status = %d, body = %#v", recorder.Code, body)
	}
}

func newRecentDownloadsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "recent-downloads.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !database.Migrator().HasIndex(&db.AuditLog{}, "idx_audit_action_order") {
		t.Fatal("missing idx_audit_action_order")
	}
	return database
}

func newRecentDownloadsTestRouter(database *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := NewDashboardHandler(database, nil, nil, false, 0)
	router := gin.New()
	router.GET("/dashboard/recent-downloads", handler.GetRecentDownloads)
	return router
}

func performRecentDownloadsRequest(router http.Handler, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}
