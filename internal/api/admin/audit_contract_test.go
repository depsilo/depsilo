package admin

import (
	"encoding/csv"
	"encoding/json"
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

func TestAuditPackageQueryAndDeprecatedSearchAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "audit.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	const maskedUpstreamURL = "https://%2A%2A%2A:%2A%2A%2A@packages.example.test/simple"
	now := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	items := []db.AuditLog{
		{Ecosystem: "pypi", PackageName: "requests", Version: "2.32.0", Action: "download", CacheResult: "hit", ClientIP: "10.0.0.1", UpstreamURL: "https://alice:secret@packages.example.test/simple", LatencyMs: 8, BytesSent: 1200, StatusCode: 200, CreatedAt: now},
		{Ecosystem: "pypi", PackageName: "flask", Version: "3.1.0", Action: "download", CacheResult: "miss", ClientIP: "10.0.0.2", LatencyMs: 30, BytesSent: 900, StatusCode: 200, CreatedAt: now.Add(-time.Minute)},
	}
	if err := database.Create(&items).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := NewAuditHandler(database)
	r := gin.New()
	r.GET("/audit-logs", h.List)
	r.GET("/audit-logs/export", h.Export)

	expectedItemKeys := map[string]struct{}{
		"id": {}, "ecosystem": {}, "package_name": {}, "version": {},
		"action": {}, "cache_result": {}, "client_ip": {}, "user_agent": {},
		"upstream_url": {}, "latency_ms": {}, "bytes_sent": {}, "status_code": {},
		"created_at": {},
	}
	for _, path := range []string{
		"/audit-logs?package=requests",
		"/audit-logs?search=requests",
		"/audit-logs?package=requests&search=flask",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, body = %s", path, rec.Code, rec.Body.String())
		}
		var body struct {
			Items []map[string]any `json:"items"`
			Total int64            `json:"total"`
			Page  int              `json:"page"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if body.Total != 1 || len(body.Items) != 1 || body.Items[0]["package_name"] != "requests" {
			t.Fatalf("GET %s body = %#v", path, body)
		}
		if len(body.Items[0]) != len(expectedItemKeys) {
			t.Fatalf("GET %s item keys = %#v", path, body.Items[0])
		}
		for key := range expectedItemKeys {
			if _, exists := body.Items[0][key]; !exists {
				t.Fatalf("GET %s missing item key %q: %#v", path, key, body.Items[0])
			}
		}
		if _, exists := body.Items[0]["result"]; exists {
			t.Fatalf("GET %s returned noncanonical result alias", path)
		}
		if body.Items[0]["cache_result"] != "hit" {
			t.Fatalf("GET %s cache_result = %v", path, body.Items[0]["cache_result"])
		}
		if body.Items[0]["upstream_url"] != maskedUpstreamURL || strings.Contains(rec.Body.String(), "alice") || strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("GET %s leaked upstream credentials: %s", path, rec.Body.String())
		}
	}

	emptyPackageRec := httptest.NewRecorder()
	r.ServeHTTP(emptyPackageRec, httptest.NewRequest(http.MethodGet, "/audit-logs?package=&search=requests", nil))
	var emptyPackageBody struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	if err := json.Unmarshal(emptyPackageRec.Body.Bytes(), &emptyPackageBody); err != nil {
		t.Fatalf("decode explicit empty package: %v", err)
	}
	if emptyPackageRec.Code != http.StatusOK || emptyPackageBody.Total != 2 || len(emptyPackageBody.Items) != 2 {
		t.Fatalf("explicit empty package status = %d, body = %#v", emptyPackageRec.Code, emptyPackageBody)
	}

	exportRec := httptest.NewRecorder()
	r.ServeHTTP(exportRec, httptest.NewRequest(http.MethodGet, "/audit-logs/export?search=requests", nil))
	records, err := csv.NewReader(strings.NewReader(exportRec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if exportRec.Code != http.StatusOK || len(records) != 2 || records[1][2] != "requests" {
		t.Fatalf("export status = %d, records = %#v", exportRec.Code, records)
	}
	expectedHeader := []string{"Time", "Ecosystem", "Package", "Version", "Action", "Result", "Client IP", "Latency(ms)", "Bytes", "Upstream URL"}
	if !reflect.DeepEqual(records[0], expectedHeader) || records[1][9] != maskedUpstreamURL {
		t.Fatalf("export DTO columns = %#v", records)
	}
	if strings.Contains(exportRec.Body.String(), "alice") || strings.Contains(exportRec.Body.String(), "secret") {
		t.Fatalf("export leaked upstream credentials: %s", exportRec.Body.String())
	}
}

func TestEncodeAuditCSVNeutralizesFormulaPrefixes(t *testing.T) {
	items := []auditLogResponse{{
		Ecosystem: "=ecosystem", PackageName: "+package", Version: "-version",
		Action: "@action", CacheResult: "\tresult", ClientIP: "\rclient",
		UpstreamURL: "\nupstream", LatencyMs: 8, BytesSent: 1200,
		CreatedAt: time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC),
	}}

	data, err := encodeAuditCSV(items)
	if err != nil {
		t.Fatalf("encode csv: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	expected := []string{
		"2026-07-10T09:00:00Z", "'=ecosystem", "'+package", "'-version",
		"'@action", "'\tresult", "'\rclient", "8", "1200", "'\nupstream",
	}
	if len(records) != 2 || !reflect.DeepEqual(records[1], expected) {
		t.Fatalf("neutralized CSV row = %#v", records)
	}
}

func TestAuditMalformedUpstreamURLFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const rawURL = "https://alice:secret@example.test/%zz"
	const maskedURL = "***"

	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "malformed-url.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	entry := db.AuditLog{
		Ecosystem: "pypi", PackageName: "malformed", Version: "1.0.0",
		Action: "download", CacheResult: "miss", UpstreamURL: rawURL,
		CreatedAt: time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC),
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := NewAuditHandler(database)
	r := gin.New()
	r.GET("/audit-logs", h.List)
	r.GET("/audit-logs/export", h.Export)

	t.Run("helper", func(t *testing.T) {
		got := maskAuditURLUserInfo(rawURL)
		if got != maskedURL || strings.Contains(got, "alice") || strings.Contains(got, "secret") {
			t.Fatalf("maskAuditURLUserInfo = %q", got)
		}
	})

	t.Run("json", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/audit-logs?package=malformed", nil))
		var body struct {
			Items []struct {
				UpstreamURL string `json:"upstream_url"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if rec.Code != http.StatusOK || len(body.Items) != 1 || body.Items[0].UpstreamURL != maskedURL || strings.Contains(rec.Body.String(), "alice") || strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("malformed URL JSON status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("csv", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/audit-logs/export?package=malformed", nil))
		records, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
		if err != nil {
			t.Fatalf("read csv: %v", err)
		}
		if rec.Code != http.StatusOK || len(records) != 2 || records[1][9] != maskedURL || strings.Contains(rec.Body.String(), "alice") || strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("malformed URL CSV status = %d, records = %#v", rec.Code, records)
		}
	})
}

func TestAuditHandlersPropagateQueryErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "closed.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	h := NewAuditHandler(database)
	r := gin.New()
	r.GET("/audit-logs", h.List)
	r.GET("/audit-logs/export", h.Export)
	for _, tc := range []struct {
		path string
		code string
	}{
		{path: "/audit-logs", code: "DB_ERROR"},
		{path: "/audit-logs/export", code: "EXPORT_ERROR"},
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v", tc.path, err)
		}
		if rec.Code != http.StatusInternalServerError || body["code"] != tc.code {
			t.Fatalf("GET %s status = %d, body = %#v", tc.path, rec.Code, body)
		}
	}
}
