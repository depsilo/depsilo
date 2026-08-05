package admin

import (
	"encoding/csv"
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

func TestAccessLogListAndExportShareFilters(t *testing.T) {
	database := newAccessLogTestDB(t)
	now := time.Date(2026, 7, 10, 8, 30, 0, 0, time.UTC)
	rows := []db.AccessLog{
		{AdapterType: "pypi", Method: "GET", PackageName: "requests", CacheKey: "pypi/requests.whl", Hit: true, Upstream: "primary", LatencyMs: 12, StatusCode: 200, ClientIP: "10.0.0.1", BytesSent: 1200, CreatedAt: now},
		{AdapterType: "pypi", Method: "GET", PackageName: "requests", CacheKey: "pypi/requests.json", Hit: false, Upstream: "primary", LatencyMs: 40, StatusCode: 200, ClientIP: "10.0.0.2", BytesSent: 300, CreatedAt: now.Add(-time.Minute)},
		{AdapterType: "npm", Method: "GET", PackageName: "requests", CacheKey: "npm/requests", Hit: true, Upstream: "npmjs", LatencyMs: 15, StatusCode: 200, ClientIP: "10.0.0.4", BytesSent: 700, CreatedAt: now.Add(-90 * time.Second)},
		{AdapterType: "npm", Method: "GET", PackageName: "react", CacheKey: "npm/react", Hit: true, Upstream: "npmjs", LatencyMs: 10, StatusCode: 200, ClientIP: "10.0.0.3", BytesSent: 800, CreatedAt: now.Add(-2 * time.Minute)},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	r := newAccessLogTestRouter(database)
	filter := "search=requests&adapter_type=pypi&hit=true"

	listRec := performAccessLogRequest(r, "/logs?"+filter)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var listBody map[string]json.RawMessage
	if err := json.Unmarshal(listRec.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	assertExactKeys(t, listBody, "items", "total", "page", "page_size")
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(listBody["items"], &items); err != nil {
		t.Fatalf("decode items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	assertExactKeys(t, items[0],
		"id", "adapter_type", "method", "cache_key", "package_name", "hit",
		"upstream", "latency_ms", "status_code", "client_ip", "bytes_sent", "created_at",
	)
	var total int64
	var page, pageSize int
	var packageName string
	var hit bool
	if err := json.Unmarshal(listBody["total"], &total); err != nil {
		t.Fatalf("decode total: %v", err)
	}
	if err := json.Unmarshal(listBody["page"], &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if err := json.Unmarshal(listBody["page_size"], &pageSize); err != nil {
		t.Fatalf("decode page size: %v", err)
	}
	if err := json.Unmarshal(items[0]["package_name"], &packageName); err != nil {
		t.Fatalf("decode package name: %v", err)
	}
	if err := json.Unmarshal(items[0]["hit"], &hit); err != nil {
		t.Fatalf("decode hit: %v", err)
	}
	if total != 1 || page != 1 || pageSize != 50 || packageName != "requests" || !hit {
		t.Fatalf("list contract: total=%d page=%d page_size=%d package=%q hit=%t", total, page, pageSize, packageName, hit)
	}

	exportRec := performAccessLogRequest(r, "/logs/export?"+filter)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", exportRec.Code, exportRec.Body.String())
	}
	if !strings.HasPrefix(exportRec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("content type = %q", exportRec.Header().Get("Content-Type"))
	}
	if disposition := exportRec.Header().Get("Content-Disposition"); !strings.HasPrefix(disposition, "attachment; filename=\"depsilo-access-logs-") || !strings.HasSuffix(disposition, ".csv\"") {
		t.Fatalf("content disposition = %q", disposition)
	}
	records := readAccessLogCSV(t, exportRec.Body.String())
	expectedHeader := []string{"Time", "Method", "Ecosystem", "Package", "Hit", "Status", "Latency(ms)", "Bytes", "Upstream", "Client IP", "Cache Key"}
	if len(records) != 2 || !reflect.DeepEqual(records[0], expectedHeader) {
		t.Fatalf("csv records = %#v", records)
	}
	if records[1][3] != "requests" || records[1][4] != "true" {
		t.Fatalf("csv data row = %#v", records[1])
	}
	if strings.Contains(exportRec.Body.String(), "react") || strings.Contains(exportRec.Body.String(), "10.0.0.2") || strings.Contains(exportRec.Body.String(), "10.0.0.4") {
		t.Fatalf("export ignored list filters: %s", exportRec.Body.String())
	}

	legacyFilter := "search=requests&type=pypi&hit=true"
	legacyListRec := performAccessLogRequest(r, "/logs?"+legacyFilter)
	if legacyListRec.Code != http.StatusOK || legacyListRec.Body.String() != listRec.Body.String() {
		t.Fatalf("legacy list differs: canonical=%s legacy_status=%d legacy=%s", listRec.Body.String(), legacyListRec.Code, legacyListRec.Body.String())
	}
	legacyExportRec := performAccessLogRequest(r, "/logs/export?"+legacyFilter)
	if legacyExportRec.Code != http.StatusOK || legacyExportRec.Body.String() != exportRec.Body.String() {
		t.Fatalf("legacy export differs: canonical=%s legacy_status=%d legacy=%s", exportRec.Body.String(), legacyExportRec.Code, legacyExportRec.Body.String())
	}
}

func TestAccessLogFilterHandlesMalformedValues(t *testing.T) {
	r := newAccessLogTestRouter(newAccessLogTestDB(t))
	for _, path := range []string{"/logs?hit=sometimes", "/logs/export?hit=sometimes"} {
		rec := performAccessLogRequest(r, path)
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if rec.Code != http.StatusBadRequest || body["code"] != "BAD_REQUEST" || body["message"] != "hit must be true or false" {
			t.Fatalf("GET %s status = %d, body = %#v", path, rec.Code, body)
		}
	}

	rec := performAccessLogRequest(r, "/logs?page=invalid&page_size=999")
	var body struct {
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode normalized pagination: %v", err)
	}
	if rec.Code != http.StatusOK || body.Page != 1 || body.PageSize != 50 {
		t.Fatalf("normalized pagination status = %d, body = %#v", rec.Code, body)
	}
}

func TestEncodeAccessLogsCSVNeutralizesAllTextCells(t *testing.T) {
	item := accessLogResponse{
		Method: "=method", AdapterType: "+ecosystem", PackageName: "-package",
		Upstream: "@upstream", ClientIP: "\tclient", CacheKey: "\rcache",
		Hit: true, StatusCode: 200, LatencyMs: 12, BytesSent: 34,
		CreatedAt: time.Date(2026, 7, 10, 8, 30, 0, 0, time.UTC),
	}
	data, err := encodeAccessLogsCSV([]accessLogResponse{item})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	records := readAccessLogCSV(t, string(data))
	expected := []string{
		"2026-07-10T08:30:00Z", "'=method", "'+ecosystem", "'-package", "true",
		"200", "12", "34", "'@upstream", "'\tclient", "'\rcache",
	}
	if len(records) != 2 || !reflect.DeepEqual(records[1], expected) {
		t.Fatalf("neutralized row = %#v", records)
	}
}

func TestAccessLogExportCapsRowsAtTenThousand(t *testing.T) {
	if testing.Short() {
		t.Skip("10,001-row export boundary contract")
	}
	const (
		contractExportRows = 10000
		seedRows           = 10001
	)
	if maxAccessLogExportRows != contractExportRows {
		t.Fatalf("maxAccessLogExportRows = %d, want contractual %d", maxAccessLogExportRows, contractExportRows)
	}
	database := newAccessLogTestDB(t)
	rows := make([]db.AccessLog, seedRows)
	for i := range rows {
		rows[i] = db.AccessLog{Method: "GET", PackageName: "package", CreatedAt: time.Unix(int64(i), 0).UTC()}
	}
	if err := database.CreateInBatches(&rows, 500).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := performAccessLogRequest(newAccessLogTestRouter(database), "/logs/export")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	records := readAccessLogCSV(t, rec.Body.String())
	if len(records) != contractExportRows+1 {
		t.Fatalf("CSV rows = %d, want header + %d", len(records), contractExportRows)
	}
	newest := time.Unix(10000, 0).UTC().Format(time.RFC3339)
	oldestIncluded := time.Unix(1, 0).UTC().Format(time.RFC3339)
	oldestExcluded := time.Unix(0, 0).UTC().Format(time.RFC3339)
	if records[1][0] != newest || records[len(records)-1][0] != oldestIncluded {
		t.Fatalf("export order boundaries = first %q, last %q; want %q, %q", records[1][0], records[len(records)-1][0], newest, oldestIncluded)
	}
	for _, record := range records[1:] {
		if record[0] == oldestExcluded {
			t.Fatalf("export included oldest capped row %q", oldestExcluded)
		}
	}
}

func TestAccessLogHandlersPropagateDatabaseErrors(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		failTarget any
		code       string
	}{
		{name: "count", path: "/logs", failTarget: (*int64)(nil), code: "DB_ERROR"},
		{name: "find", path: "/logs", failTarget: (*[]db.AccessLog)(nil), code: "DB_ERROR"},
		{name: "export", path: "/logs/export", failTarget: (*[]db.AccessLog)(nil), code: "EXPORT_ERROR"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			database := newAccessLogTestDB(t)
			if err := database.Callback().Query().Before("gorm:query").Register("test:fail_query", func(tx *gorm.DB) {
				if reflect.TypeOf(tx.Statement.Dest) == reflect.TypeOf(tc.failTarget) {
					tx.AddError(errors.New("forced query failure"))
				}
			}); err != nil {
				t.Fatalf("register callback: %v", err)
			}
			rec := performAccessLogRequest(newAccessLogTestRouter(database), tc.path)
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if rec.Code != http.StatusInternalServerError || body["code"] != tc.code || !strings.Contains(body["message"].(string), "forced query failure") {
				t.Fatalf("status = %d, body = %#v", rec.Code, body)
			}
		})
	}
}

func newAccessLogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logs.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.AccessLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func newAccessLogTestRouter(database *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewAccessLogHandler(database)
	r := gin.New()
	r.GET("/logs", h.List)
	r.GET("/logs/export", h.Export)
	return r
}

func performAccessLogRequest(r http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func readAccessLogCSV(t *testing.T, data string) [][]string {
	t.Helper()
	records, err := csv.NewReader(strings.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	return records
}

func assertExactKeys(t *testing.T, object map[string]json.RawMessage, keys ...string) {
	t.Helper()
	want := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		want[key] = struct{}{}
	}
	if len(object) != len(want) {
		t.Fatalf("keys = %#v, want %#v", object, want)
	}
	for key := range want {
		if _, ok := object[key]; !ok {
			t.Fatalf("missing key %q in %#v", key, object)
		}
	}
}
