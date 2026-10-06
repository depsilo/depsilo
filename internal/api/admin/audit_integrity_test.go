package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/audit"
	"depsilo/internal/db"
)

func TestAuditIntegrityEndpointReportsChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "audit-integrity.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	rows := []db.AuditLog{
		{Ecosystem: "pypi", PackageName: "requests", Action: "download", CacheResult: "hit", StatusCode: 200, CreatedAt: time.Now().UTC()},
		{Ecosystem: "npm", PackageName: "left-pad", Action: "download", CacheResult: "miss", StatusCode: 200, CreatedAt: time.Now().UTC()},
	}
	if err := audit.AppendAuditRows(context.Background(), database, rows); err != nil {
		t.Fatal(err)
	}

	handler := NewAuditHandler(database)
	router := gin.New()
	router.GET("/audit/integrity", handler.Integrity)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/audit/integrity", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Integrity audit.ChainReport `json:"integrity"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Integrity.OK || body.Integrity.ChainedRows != 2 || body.Integrity.HeadHash == "" {
		t.Fatalf("integrity = %+v", body.Integrity)
	}

	// Tampering with a stored row breaks the chain and names the row.
	if err := database.Model(&db.AuditLog{}).Where("id = ?", 1).Update("package_name", "tampered").Error; err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/audit/integrity", nil))
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Integrity.OK || body.Integrity.BrokenAtID != 1 {
		t.Fatalf("tampered integrity = %+v", body.Integrity)
	}
}
