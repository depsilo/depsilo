package admin

import (
	"context"
	"encoding/json"
	"errors"
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

func TestLatencyHistoryPreservesAllRangePointsAscending(t *testing.T) {
	database := newLatencyTestDB(t)
	record := db.UpstreamRecord{ID: 11, AdapterType: "pypi", Name: "one", URL: "https://one.example"}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}

	start := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	logs := make([]db.UpstreamLatencyLog, 0, 46)
	for index := 0; index < 46; index++ {
		logs = append(logs, db.UpstreamLatencyLog{
			UpstreamID: 11,
			Name:       "one",
			LatencyMs:  int64(100 + index),
			Healthy:    true,
			CreatedAt:  start.Add(time.Duration(index) * time.Minute),
		})
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performLatencyRequest(database, "/upstreams/11/latency?range=24h")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Points []struct {
			Time      time.Time `json:"time"`
			LatencyMS int64     `json:"latency_ms"`
		} `json:"points"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Points) != 46 {
		t.Fatalf("points=%d want=46", len(body.Points))
	}
	for index := 1; index < len(body.Points); index++ {
		if body.Points[index].Time.Before(body.Points[index-1].Time) {
			t.Fatalf("points are not ascending: %#v", body.Points)
		}
	}
	if first, last := body.Points[0].LatencyMS, body.Points[len(body.Points)-1].LatencyMS; first != 100 || last != 145 {
		t.Fatalf("latencies=%d..%d want full range 100..145", first, last)
	}
}

func TestLatencySeriesReturnsLatestPointsAscendingAndIsolatedByUpstreamID(t *testing.T) {
	database := newLatencyTestDB(t)
	for _, record := range []db.UpstreamRecord{
		{ID: 11, AdapterType: "pypi", Name: "shared", URL: "https://one.example"},
		{ID: 22, AdapterType: "npm", Name: "shared", URL: "https://two.example"},
	} {
		if err := database.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}

	start := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	logs := make([]db.UpstreamLatencyLog, 0, 49)
	for index := 0; index < 46; index++ {
		logs = append(logs, db.UpstreamLatencyLog{
			UpstreamID: 11,
			Name:       "shared",
			LatencyMs:  int64(100 + index),
			Healthy:    index%2 == 0,
			CreatedAt:  start.Add(time.Duration(index) * time.Minute),
		})
	}
	// Insert the second Upstream out of chronological order. Its values overlap
	// neither the first series nor its IDs, making ordering and isolation
	// independently observable from the HTTP response.
	for _, point := range []struct {
		minute  int
		latency int64
		healthy bool
	}{
		{minute: 12, latency: 222, healthy: false},
		{minute: 2, latency: 221, healthy: true},
		{minute: 32, latency: 223, healthy: true},
	} {
		logs = append(logs, db.UpstreamLatencyLog{
			UpstreamID: 22,
			Name:       "shared",
			LatencyMs:  point.latency,
			Healthy:    point.healthy,
			CreatedAt:  start.Add(time.Duration(point.minute) * time.Minute),
		})
	}
	logs = append(logs, db.UpstreamLatencyLog{
		UpstreamID: 22,
		Name:       "shared",
		LatencyMs:  220,
		Healthy:    true,
		CreatedAt:  start.Add(-48 * time.Hour),
	})
	if err := database.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performLatencySeriesRequest(database, "/upstreams/latency?range=24h")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Series []struct {
			UpstreamID uint `json:"upstream_id"`
			Points     []struct {
				Time      time.Time `json:"time"`
				LatencyMS int64     `json:"latency_ms"`
				Healthy   bool      `json:"healthy"`
			} `json:"points"`
		} `json:"series"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Series) != 2 {
		t.Fatalf("series=%#v", body.Series)
	}

	seriesByID := make(map[uint]int, len(body.Series))
	pointsByID := make(map[uint][]int64, len(body.Series))
	healthByID := make(map[uint][]bool, len(body.Series))
	for _, series := range body.Series {
		if _, duplicate := seriesByID[series.UpstreamID]; duplicate {
			t.Fatalf("duplicate series for upstream_id=%d", series.UpstreamID)
		}
		seriesByID[series.UpstreamID] = len(series.Points)
		for index, point := range series.Points {
			if index > 0 && point.Time.Before(series.Points[index-1].Time) {
				t.Fatalf("upstream_id=%d points are not ascending: %#v", series.UpstreamID, series.Points)
			}
			pointsByID[series.UpstreamID] = append(pointsByID[series.UpstreamID], point.LatencyMS)
			healthByID[series.UpstreamID] = append(healthByID[series.UpstreamID], point.Healthy)
		}
	}
	if seriesByID[11] != 44 {
		t.Fatalf("upstream_id=11 points=%d want=44", seriesByID[11])
	}
	if got := pointsByID[11]; len(got) != 44 || got[0] != 102 || got[len(got)-1] != 145 {
		t.Fatalf("upstream_id=11 latencies=%v want latest range 102..145", got)
	}
	if got := pointsByID[22]; len(got) != 3 || got[0] != 221 || got[1] != 222 || got[2] != 223 {
		t.Fatalf("upstream_id=22 latencies=%v want [221 222 223]", got)
	}
	if got := healthByID[22]; len(got) != 3 || !got[0] || got[1] || !got[2] {
		t.Fatalf("upstream_id=22 health=%v want [true false true]", got)
	}
}

func TestLatencySeriesUsesCompositeIndex(t *testing.T) {
	database := newLatencyTestDB(t)
	type queryPlanRow struct {
		Detail string `gorm:"column:detail"`
	}
	var plan []queryPlanRow
	if err := database.Raw(
		"EXPLAIN QUERY PLAN "+latencySeriesQuery,
		time.Now().Add(-24*time.Hour),
		maxLatencyHistoryPoints,
	).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	details := make([]string, 0, len(plan))
	for _, row := range plan {
		details = append(details, row.Detail)
	}
	if joined := strings.Join(details, "\n"); !strings.Contains(joined, "idx_upstream_latency_upstream_created") {
		t.Fatalf("batch query does not use the composite latency index:\n%s", joined)
	}
}

func TestLatencySeriesExecutesSingleDatabaseQuery(t *testing.T) {
	database := newLatencyTestDB(t)
	counter := &countingLatencyLogger{Interface: logger.Default.LogMode(logger.Silent)}
	countedDatabase := database.Session(&gorm.Session{Logger: counter})
	recorder := performLatencySeriesRequest(countedDatabase, "/upstreams/latency")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if counter.Queries != 1 {
		t.Fatalf("database queries=%d want=1", counter.Queries)
	}
}

func TestLatencySeriesReturnsEmptyArrayAndDatabaseErrors(t *testing.T) {
	database := newLatencyTestDB(t)
	recorder := performLatencySeriesRequest(database, "/upstreams/latency")
	var body struct {
		Series []upstreamLatencySeries `json:"series"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || body.Series == nil || len(body.Series) != 0 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	assertLatencyDBError(t, performLatencySeriesRequest(database, "/upstreams/latency"))
}

func TestUpstreamLatencyLogMigrationCreatesCompositeIndex(t *testing.T) {
	database, err := gorm.Open(
		sqlite.Open(filepath.Join(t.TempDir(), "legacy-latency.db")),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&legacyUpstreamLatencyLog{}); err != nil {
		t.Fatal(err)
	}
	legacy := legacyUpstreamLatencyLog{
		UpstreamID: 11,
		Name:       "legacy",
		LatencyMs:  42,
		Healthy:    true,
		CreatedAt:  time.Now().UTC(),
	}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.UpstreamLatencyLog{}); err != nil {
		t.Fatal(err)
	}
	const indexName = "idx_upstream_latency_upstream_created"
	if !database.Migrator().HasIndex(&db.UpstreamLatencyLog{}, indexName) {
		t.Fatalf("missing composite index %q", indexName)
	}
	var count int64
	if err := database.Model(&db.UpstreamLatencyLog{}).Where("upstream_id = ?", legacy.UpstreamID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("legacy rows=%d want=1", count)
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

func performLatencySeriesRequest(database *gorm.DB, path string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewLatencyHandler(database)
	router.GET("/upstreams/latency", handler.GetLatencySeries)
	router.GET("/upstreams/:id/latency", handler.GetLatencyHistory)
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

type countingLatencyLogger struct {
	logger.Interface
	Queries int
}

type legacyUpstreamLatencyLog struct {
	ID         uint   `gorm:"primarykey"`
	UpstreamID uint   `gorm:"index"`
	Name       string `gorm:"size:128;index"`
	LatencyMs  int64
	Healthy    bool
	CreatedAt  time.Time `gorm:"index"`
}

func (legacyUpstreamLatencyLog) TableName() string { return "upstream_latency_logs" }

func (l *countingLatencyLogger) Trace(
	ctx context.Context,
	begin time.Time,
	query func() (sql string, rowsAffected int64),
	err error,
) {
	l.Queries++
	l.Interface.Trace(ctx, begin, query, err)
}
