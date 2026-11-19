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

// A sink or checkpoint that has never succeeded is sent as null; Go's zero
// time would read as a real, truthy timestamp in the Admin UI.
func TestAuditIntegrityEndpointSendsNeverAsNull(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "audit-never.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/audit/integrity", NewAuditHandler(database).Integrity)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/audit/integrity", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Integrity  map[string]json.RawMessage `json:"integrity"`
		Anchors    map[string]json.RawMessage `json:"anchors"`
		AnchorFeed map[string]json.RawMessage `json:"anchor_feed"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]json.RawMessage{
		"integrity.base_created_at":          body.Integrity["base_created_at"],
		"anchors.latest_checked_at":          body.Anchors["latest_checked_at"],
		"anchor_feed.file_last_success_at":   body.AnchorFeed["file_last_success_at"],
		"anchor_feed.remote_last_success_at": body.AnchorFeed["remote_last_success_at"],
	} {
		if string(value) != "null" {
			t.Errorf("%s = %s, want null", name, value)
		}
	}
}

func TestAuditIntegrityEndpointReportsTheRetentionBase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "audit-base.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().AddDate(0, 0, -200)
	rows := []db.AuditLog{
		{Ecosystem: "admin", PackageName: "old", Action: "user_create", CacheResult: "success", StatusCode: 200, CreatedAt: old},
		{Ecosystem: "admin", PackageName: "new", Action: "user_create", CacheResult: "success", StatusCode: 200, CreatedAt: time.Now().UTC()},
	}
	if err := audit.AppendAuditRows(context.Background(), database, rows); err != nil {
		t.Fatal(err)
	}
	var pruned db.AuditLog
	if err := database.Order("id").First(&pruned).Error; err != nil {
		t.Fatal(err)
	}
	if deleted, err := audit.NewRetention(database, 90).RunOnce(context.Background()); err != nil || deleted != 1 {
		t.Fatalf("retention deleted=%d err=%v", deleted, err)
	}

	router := gin.New()
	router.GET("/audit/integrity", NewAuditHandler(database).Integrity)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/audit/integrity", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Integrity struct {
			OK            bool       `json:"ok"`
			ChainedRows   int64      `json:"chained_rows"`
			BaseID        uint       `json:"base_id"`
			BaseHash      string     `json:"base_hash"`
			BaseCreatedAt *time.Time `json:"base_created_at"`
		} `json:"integrity"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body.Integrity
	if !got.OK || got.ChainedRows != 1 || got.BaseID != pruned.ID || got.BaseHash != pruned.Hash ||
		got.BaseCreatedAt == nil || !got.BaseCreatedAt.Equal(pruned.CreatedAt) {
		t.Fatalf("integrity = %+v, want base row %d", got, pruned.ID)
	}
}
