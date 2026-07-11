package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

func TestDashboardUsesSnapshotUpstreamIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "dashboard.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.AccessLog{}, &db.UpstreamRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, record := range []db.UpstreamRecord{
		{ID: 901, AdapterType: "pypi", Name: "shared", URL: "https://db-pypi.example", Priority: 1},
		{ID: 902, AdapterType: "npm", Name: "shared", URL: "https://db-npm.example", Priority: 1},
	} {
		if err := database.Create(&record).Error; err != nil {
			t.Fatalf("create conflicting db upstream: %v", err)
		}
	}

	pypiPool, err := upstream.NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 11, AdapterType: "pypi", Name: "shared", URL: "https://pool-pypi.example", Priority: 1, Healthy: true,
	}})
	if err != nil {
		t.Fatalf("create pypi pool: %v", err)
	}
	npmPool, err := upstream.NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 22, AdapterType: "npm", Name: "shared", URL: "https://pool-npm.example", Priority: 1, Healthy: true,
	}})
	if err != nil {
		t.Fatalf("create npm pool: %v", err)
	}

	handler := NewDashboardHandler(database, nil, map[string]*upstream.Pool{
		"pypi": pypiPool,
		"npm":  npmPool,
	}, []string{"pypi", "npm"}, false)
	router := gin.New()
	router.GET("/dashboard", handler.GetDashboard)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Upstreams []struct {
			ID      uint   `json:"id"`
			Adapter string `json:"adapter"`
		} `json:"upstreams"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	got := make(map[string]uint, len(response.Upstreams))
	for _, item := range response.Upstreams {
		got[item.Adapter] = item.ID
	}
	if got["pypi"] != 11 || got["npm"] != 22 {
		t.Fatalf("upstream IDs = %#v, want map[pypi:11 npm:22]", got)
	}
}
