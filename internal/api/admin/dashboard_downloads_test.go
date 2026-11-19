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

	"depsilo/internal/api/contract"
	"depsilo/internal/db"
)

func TestDashboardRecentDownloadsReturnsSmallPrivateSnapshot(t *testing.T) {
	database := newRecentDownloadsTestDB(t)
	now := time.Date(2026, 7, 21, 10, 30, 0, 0, time.UTC)
	rows := []db.AccessLog{
		{AdapterType: "pypi", CacheKey: "pypi/files/ab/oldest-1.0.0-py3-none-any.whl", PackageName: "oldest", Hit: true, ClientIP: "10.0.0.1", Upstream: "secret-upstream", LatencyMs: 8, BytesSent: 1200, StatusCode: 200, CreatedAt: now.Add(-3 * time.Minute)},
		{AdapterType: "npm", CacheKey: "npm-exact-v1/metadata-only/metadata.json", PackageName: "metadata-only", StatusCode: 200, CreatedAt: now.Add(-2 * time.Minute)},
		{AdapterType: "npm", CacheKey: "npm-exact-v1/middle/-/middle-2.0.0-rc.1.tgz", PackageName: "middle", LatencyMs: 31, BytesSent: 2400, StatusCode: 200, CreatedAt: now.Add(-time.Minute)},
		{AdapterType: "cargo", CacheKey: "cargo/crates/newest/3.0.0.crate", PackageName: "newest", LatencyMs: 55, StatusCode: 502, CreatedAt: now},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A request row an earlier release chained into the audit log is not a
	// second source: the feed reads the access log alone.
	if err := database.Create(&db.AuditLog{Ecosystem: "npm", PackageName: "legacy-audit-row", Version: "9.9.9", Action: "download", CacheResult: "hit", StatusCode: 200, CreatedAt: now.Add(time.Minute)}).Error; err != nil {
		t.Fatalf("seed legacy audit row: %v", err)
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
	if strings.Contains(recorder.Body.String(), "10.0.0.1") || strings.Contains(recorder.Body.String(), "secret-upstream") || strings.Contains(recorder.Body.String(), "legacy-audit-row") {
		t.Fatalf("response leaked private request fields: %s", recorder.Body.String())
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
	var decoded contract.RecentDownloadsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode typed response: %v", err)
	}
	newest, middle := decoded.Items[0], decoded.Items[1]
	// A failed response proved no artifact identity, so it names no version.
	if newest.PackageName != "newest" || newest.Version != "" || newest.CacheResult != "error" || newest.StatusCode != 502 {
		t.Fatalf("newest = %#v", newest)
	}
	if middle.Ecosystem != "npm" || middle.PackageName != "middle" || middle.Version != "2.0.0-rc.1" ||
		middle.CacheResult != "miss" || middle.BytesSent != 2400 || middle.LatencyMs != 31 {
		t.Fatalf("middle = %#v", middle)
	}
}

func TestDashboardRecentDownloadsScansPastMetadataRequests(t *testing.T) {
	database := newRecentDownloadsTestDB(t)
	now := time.Date(2026, 7, 21, 10, 30, 0, 0, time.UTC)
	if err := database.Create(&db.AccessLog{AdapterType: "npm", CacheKey: "npm-exact-v1/left-pad/-/left-pad-1.3.0.tgz", PackageName: "left-pad", Hit: true, BytesSent: 10, StatusCode: 200, CreatedAt: now}).Error; err != nil {
		t.Fatalf("seed download: %v", err)
	}
	metadata := make([]db.AccessLog, recentDownloadsScanBatch+5)
	for index := range metadata {
		metadata[index] = db.AccessLog{AdapterType: "npm", CacheKey: "npm-exact-v1/left-pad/metadata.json", PackageName: "left-pad", Hit: true, StatusCode: 200, CreatedAt: now}
	}
	if err := database.CreateInBatches(&metadata, 50).Error; err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	recorder := performRecentDownloadsRequest(newRecentDownloadsTestRouter(database), "/dashboard/recent-downloads?limit=3")
	var body contract.RecentDownloadsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(body.Items) != 1 || body.Items[0].PackageName != "left-pad" || body.Items[0].Version != "1.3.0" || body.Items[0].CacheResult != "hit" {
		t.Fatalf("items = %#v, want the one download behind a batch of metadata requests", body.Items)
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

func TestDashboardRecentDownloadsPropagatesDatabaseErrors(t *testing.T) {
	database := newRecentDownloadsTestDB(t)
	if err := database.Callback().Query().Before("gorm:query").Register("test:fail_recent_download_query", func(tx *gorm.DB) {
		if reflect.TypeOf(tx.Statement.Dest) == reflect.TypeOf((*[]db.AccessLog)(nil)) {
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
	if err := database.AutoMigrate(&db.AccessLog{}, &db.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
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
