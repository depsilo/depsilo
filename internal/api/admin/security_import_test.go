package admin

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/security"
)

type securityImportTestResponse struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	Imported     int    `json:"imported"`
	Received     int    `json:"received"`
	Packages     int    `json:"packages"`
	Duplicates   int    `json:"duplicates"`
	Skipped      int    `json:"skipped"`
	RulesCreated int    `json:"rules_created"`
}

func newSecurityImportTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "security-import.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(
		&db.Vulnerability{},
		&db.VulnerabilityCheck{},
		&db.SecurityPolicy{},
		&db.PackageRule{},
	); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return database
}

func newSecurityImportTestRouter(t *testing.T, database *gorm.DB, catalog *security.AdvisoryCatalog) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler := NewSecurityHandler(database, nil, catalog, nil)
	router := gin.New()
	router.POST("/security/import", handler.ImportData)
	return router
}

func performSecurityImport(
	t *testing.T,
	router http.Handler,
	payload []byte,
) (*httptest.ResponseRecorder, securityImportTestResponse) {
	t.Helper()
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	part, err := writer.CreateFormFile("file", "advisories.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/security/import", &requestBody)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	var response securityImportTestResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return recorder, response
}

func newSecurityImportTestCatalog(t *testing.T, database *gorm.DB) *security.AdvisoryCatalog {
	t.Helper()
	catalog, err := security.NewAdvisoryCatalog(database, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

var validSecurityImportPayload = []byte(`[
  {
    "id": "OSV-IMPORT-FIXTURE",
    "summary": "fixture advisory",
    "severity": [{"type": "CVSS_V3", "score": "8.1"}],
    "affected": [{
      "package": {"ecosystem": "PyPI", "name": "fixture-package"},
      "ranges": [{"type": "ECOSYSTEM", "events": [{"introduced": "0"}, {"fixed": "2.0.0"}]}]
    }],
    "published": "2026-07-01T00:00:00Z",
    "modified": "2026-07-02T00:00:00Z"
  }
]`)

func TestSecurityImportReturnsCatalogReceipt(t *testing.T) {
	database := newSecurityImportTestDB(t)
	router := newSecurityImportTestRouter(t, database, newSecurityImportTestCatalog(t, database))

	recorder, response := performSecurityImport(t, router, validSecurityImportPayload)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if response.Imported != 1 || response.Received != 1 || response.Packages != 1 ||
		response.Duplicates != 0 || response.Skipped != 0 || response.RulesCreated != 0 {
		t.Fatalf("receipt = %+v", response)
	}
}

func TestSecurityImportMapsCatalogValidationErrors(t *testing.T) {
	database := newSecurityImportTestDB(t)
	router := newSecurityImportTestRouter(t, database, newSecurityImportTestCatalog(t, database))

	t.Run("invalid", func(t *testing.T) {
		recorder, response := performSecurityImport(t, router, []byte(`{"id":`))
		if recorder.Code != http.StatusBadRequest || response.Code != "INVALID_IMPORT" || response.Message != "invalid advisory import" {
			t.Fatalf("status = %d, response = %+v", recorder.Code, response)
		}
	})

	t.Run("too large", func(t *testing.T) {
		payload := bytes.Repeat([]byte(" "), (32<<20)+1)
		recorder, response := performSecurityImport(t, router, payload)
		if recorder.Code != http.StatusRequestEntityTooLarge || response.Code != "IMPORT_TOO_LARGE" {
			t.Fatalf("status = %d, response = %+v", recorder.Code, response)
		}
	})
}

func TestSecurityImportDoesNotLeakPersistenceErrors(t *testing.T) {
	database := newSecurityImportTestDB(t)
	catalog := newSecurityImportTestCatalog(t, database)
	router := newSecurityImportTestRouter(t, database, catalog)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	recorder, response := performSecurityImport(t, router, validSecurityImportPayload)
	if recorder.Code != http.StatusInternalServerError || response.Code != "IMPORT_FAILED" || response.Message != "advisory import failed" {
		t.Fatalf("status = %d, response = %+v", recorder.Code, response)
	}
	if strings.Contains(strings.ToLower(recorder.Body.String()), "database is closed") ||
		strings.Contains(strings.ToLower(recorder.Body.String()), "sql") {
		t.Fatalf("response leaked persistence error: %s", recorder.Body.String())
	}
}

func TestSecurityImportReportsUnavailableCatalog(t *testing.T) {
	database := newSecurityImportTestDB(t)
	router := newSecurityImportTestRouter(t, database, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/security/import", nil))

	var response securityImportTestResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusServiceUnavailable || response.Code != "IMPORT_UNAVAILABLE" || response.Message != "advisory import is unavailable" {
		t.Fatalf("status = %d, response = %+v", recorder.Code, response)
	}
}
