package admin

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
	"depsilo/internal/middleware"
	"depsilo/internal/upstream"
)

func TestCredentialURLMasking(t *testing.T) {
	if got := maskWebhookURL("https://hooks.example.test/services/T000/B000/secret?token=hidden"); got != "https://hooks.example.test/***" {
		t.Fatalf("maskWebhookURL = %q", got)
	}
	if got := maskWebhookURL("not a URL"); got != "***" {
		t.Fatalf("invalid maskWebhookURL = %q", got)
	}
	if got := maskCredentialURL("http://alice:password@proxy.example.test:8080/path?token=hidden#secret"); got != "http://proxy.example.test:8080/***" {
		t.Fatalf("maskCredentialURL = %q", got)
	}
	if got := maskCredentialURL("https://packages.example.test/signed/path?token=hidden"); got != "https://packages.example.test/***" {
		t.Fatalf("query credential maskCredentialURL = %q", got)
	}
	if got := maskCredentialURL("http://alice:%zz@proxy.example.test/path"); got != "***" {
		t.Fatalf("malformed maskCredentialURL = %q", got)
	}
}

func TestWebhookListMasksURLForReadonlyPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := openCredentialTestDB(t, "webhooks.db", &db.WebhookConfig{})
	config := db.WebhookConfig{Name: "ops", Platform: "slack", URL: "https://hooks.example.test/services/secret", Enabled: true, Events: "*"}
	if err := database.Create(&config).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := NewWebhookHandler(database, nil)
	request := func(canWrite bool) map[string]any {
		r := principalTestRouter(canWrite)
		r.GET("/webhooks", h.List)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/webhooks", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var items []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return items[0]
	}
	if got := request(false)["url"]; got != "https://hooks.example.test/***" {
		t.Fatalf("readonly url = %v", got)
	}
	if got := request(true)["url"]; got != config.URL {
		t.Fatalf("admin url = %v", got)
	}
}

func TestUpstreamListMasksCredentialsForReadonlyPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := openCredentialTestDB(t, "upstreams.db", &db.UpstreamRecord{})
	record := db.UpstreamRecord{Name: "private", AdapterType: "pypi", URL: "https://alice:secret@packages.example.test/private/path", Proxy: "http://proxy-user:proxy-pass@proxy.example.test:8080/private/path", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true}
	if err := database.Create(&record).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	registry, err := upstream.NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	h := NewUpstreamHandler(registry)
	request := func(canWrite bool) adminUpstreamResponse {
		r := principalTestRouter(canWrite)
		r.GET("/upstreams", h.List)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/upstreams", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var response upstreamListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || len(response.Items) != 1 {
			t.Fatalf("decode: err=%v body=%s", err, rec.Body.String())
		}
		return response.Items[0]
	}
	readonly := request(false)
	if readonly.URL != "https://packages.example.test/***" || readonly.Proxy != "http://proxy.example.test:8080/***" {
		t.Fatalf("readonly upstream = %#v", readonly)
	}
	writer := request(true)
	if writer.URL != record.URL || writer.Proxy != record.Proxy {
		t.Fatalf("writer upstream = %#v", writer)
	}
	var persisted db.UpstreamRecord
	if err := database.First(&persisted, record.ID).Error; err != nil || persisted.URL != record.URL || persisted.Proxy != record.Proxy {
		t.Fatalf("persisted upstream changed: err=%v upstream=%#v", err, persisted)
	}
}

func TestAuditListAndExportMaskUpstreamCredentialsByPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := openCredentialTestDB(t, "audit-credentials.db", &db.AuditLog{})
	const rawURL = "https://alice:secret@packages.example.test/signed/path?token=hidden#secret"
	entry := db.AuditLog{Ecosystem: "pypi", PackageName: "requests", Version: "2.32.0", Action: "download", CacheResult: "miss", ClientIP: "10.0.0.1", UpstreamURL: rawURL, StatusCode: 200, CreatedAt: time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	request := func(canWrite bool, path string) *httptest.ResponseRecorder {
		r := principalTestRouter(canWrite)
		h := NewAuditHandler(database)
		r.GET("/audit-logs", h.List)
		r.GET("/audit-logs/export", h.Export)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s canWrite=%v status=%d body=%s", path, canWrite, rec.Code, rec.Body.String())
		}
		return rec
	}

	listURL := func(rec *httptest.ResponseRecorder) string {
		var body struct {
			Items []struct {
				UpstreamURL string `json:"upstream_url"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Items) != 1 {
			t.Fatalf("decode audit list: err=%v body=%s", err, rec.Body.String())
		}
		return body.Items[0].UpstreamURL
	}
	if got := listURL(request(true, "/audit-logs")); got != rawURL {
		t.Fatalf("writer list upstream_url = %q", got)
	}
	readonlyList := request(false, "/audit-logs")
	if got := listURL(readonlyList); got != "https://packages.example.test/***" {
		t.Fatalf("readonly list upstream_url = %q", got)
	}
	if strings.Contains(readonlyList.Body.String(), "alice") || strings.Contains(readonlyList.Body.String(), "secret") {
		t.Fatalf("readonly list leaked credentials: %s", readonlyList.Body.String())
	}

	exportURL := func(rec *httptest.ResponseRecorder) string {
		records, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
		if err != nil || len(records) != 2 || len(records[1]) != 10 || records[0][9] != "Upstream URL" {
			t.Fatalf("decode audit export: err=%v records=%#v", err, records)
		}
		return records[1][9]
	}
	if got := exportURL(request(true, "/audit-logs/export")); got != rawURL {
		t.Fatalf("writer export upstream_url = %q", got)
	}
	readonlyExport := request(false, "/audit-logs/export")
	if got := exportURL(readonlyExport); got != "https://packages.example.test/***" {
		t.Fatalf("readonly export upstream_url = %q", got)
	}
	if strings.Contains(readonlyExport.Body.String(), "alice") || strings.Contains(readonlyExport.Body.String(), "secret") {
		t.Fatalf("readonly export leaked credentials: %s", readonlyExport.Body.String())
	}
}

func openCredentialTestDB(t *testing.T, name string, models ...any) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), name)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func principalTestRouter(canWrite bool) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		role := "readonly"
		if canWrite {
			role = "admin"
		}
		c.Set(middleware.ContextKeyPrincipal, middleware.Principal{ID: 1, Username: "operator", Role: role, Enabled: true, AuthMethod: middleware.AuthMethodJWT, CanWrite: canWrite})
		c.Next()
	})
	return r
}
