package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/quarantine"
)

func newQuarantineAdminTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&db.ApprovedVersion{}, &db.QuarantineEvent{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	handler := NewQuarantineHandler(database, quarantine.NewStore(database), true)
	router := gin.New()
	router.POST("/approve", handler.Approve)
	return router, database
}

func TestQuarantineApproveRejectsWhenAgeGateUnavailable(t *testing.T) {
	router, database := newQuarantineAdminTestRouter(t)
	handler := NewQuarantineHandler(database, quarantine.NewStore(database), false)
	router = gin.New()
	router.POST("/approve", handler.Approve)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/approve", strings.NewReader(`{
		"ecosystem":"npm","package":"left-pad","version":"1.0.0","reason":"security review complete"
	}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "MINIMUM_RELEASE_AGE_UNAVAILABLE") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var count int64
	if err := database.Model(&db.ApprovedVersion{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unavailable approval persisted %d rows", count)
	}
}

func TestQuarantineApproveReturnsCanonicalCoordinate(t *testing.T) {
	router, database := newQuarantineAdminTestRouter(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/approve", strings.NewReader(`{
		"ecosystem":"PyPI","package":"My_Pkg","version":"v1.0-1","reason":"security review complete"
	}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Ecosystem string `json:"ecosystem"`
		Package   string `json:"package"`
		Version   string `json:"version"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Ecosystem != "pypi" || response.Package != "my-pkg" || response.Version != "1.0.post1" {
		t.Fatalf("response coordinate = %+v", response)
	}
	var row db.ApprovedVersion
	if err := database.Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Ecosystem != response.Ecosystem || row.Package != response.Package || row.Version != response.Version {
		t.Fatalf("stored coordinate = %+v, response = %+v", row, response)
	}
}

func TestQuarantineApproveRejectsInvalidDialectCoordinate(t *testing.T) {
	router, database := newQuarantineAdminTestRouter(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/approve", strings.NewReader(`{
		"ecosystem":"npm","package":"left-pad","version":"not-semver","reason":"security review complete"
	}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "INVALID_COORDINATE") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var count int64
	if err := database.Model(&db.ApprovedVersion{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid approval persisted %d rows", count)
	}
}
