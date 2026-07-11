package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
)

func TestLatencyHistoryFiltersByUpstreamIDAcrossRename(t *testing.T) {
	database := newLatencyTestDB(t)
	record := db.UpstreamRecord{ID: 11, AdapterType: "pypi", Name: "renamed", URL: "https://one.example"}
	other := db.UpstreamRecord{ID: 22, AdapterType: "npm", Name: "same-old-name", URL: "https://two.example"}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	logs := []db.UpstreamLatencyLog{
		{UpstreamID: 11, Name: "same-old-name", LatencyMs: 11, Healthy: true, CreatedAt: now},
		{UpstreamID: 22, Name: "same-old-name", LatencyMs: 22, Healthy: true, CreatedAt: now},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}
	recorder := performLatencyRequest(database, "/upstreams/11/latency")
	var body struct {
		UpstreamName string `json:"upstream_name"`
		Points       []struct {
			LatencyMS int64 `json:"latency_ms"`
		} `json:"points"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || body.UpstreamName != "renamed" || len(body.Points) != 1 || body.Points[0].LatencyMS != 11 {
		t.Fatalf("status=%d body=%#v", recorder.Code, body)
	}
}

func TestLatencyHistoryRejectsInvalidAndMissingIDs(t *testing.T) {
	database := newLatencyTestDB(t)
	for _, tc := range []struct {
		path string
		code int
		body string
	}{
		{path: "/upstreams/nope/latency", code: http.StatusBadRequest, body: "INVALID_ID"},
		{path: "/upstreams/99/latency", code: http.StatusNotFound, body: "NOT_FOUND"},
	} {
		recorder := performLatencyRequest(database, tc.path)
		var body map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != tc.code || body["code"] != tc.body {
			t.Fatalf("path=%s status=%d body=%#v", tc.path, recorder.Code, body)
		}
	}
}

func TestLatencyHistoryReturnsDatabaseErrorForLookupFailure(t *testing.T) {
	database := newLatencyTestDB(t)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	assertLatencyDBError(t, performLatencyRequest(database, "/upstreams/1/latency"))
}

func TestLatencyHistoryReturnsDatabaseErrorForHistoryFailure(t *testing.T) {
	database := newLatencyTestDB(t)
	record := db.UpstreamRecord{ID: 11, AdapterType: "pypi", Name: "one", URL: "https://one.example"}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Callback().Query().Before("gorm:query").Register("test:fail_latency_history", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "UpstreamLatencyLog" {
			tx.AddError(errors.New("forced history failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	assertLatencyDBError(t, performLatencyRequest(database, "/upstreams/11/latency"))
}

func assertLatencyDBError(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusInternalServerError || body["code"] != "DB_ERROR" {
		t.Fatalf("status=%d body=%#v", recorder.Code, body)
	}
}

func performLatencyRequest(database *gorm.DB, path string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/upstreams/:id/latency", NewLatencyHandler(database).GetLatencyHistory)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func newLatencyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "latency.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.UpstreamRecord{}, &db.UpstreamLatencyLog{}); err != nil {
		t.Fatal(err)
	}
	return database
}
