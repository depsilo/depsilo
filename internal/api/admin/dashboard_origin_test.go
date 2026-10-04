package admin

import (
	"encoding/json"
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

type dashboardOverviewBody struct {
	Window struct {
		TotalRequests    int64 `json:"total_requests"`
		HitBytes         int64 `json:"hit_bytes"`
		UpstreamRequests int64 `json:"upstream_requests"`
		UpstreamBytes    int64 `json:"upstream_bytes"`
	} `json:"window"`
	OriginCoverage struct {
		Measured       bool    `json:"measured"`
		Since          *string `json:"since"`
		WindowComplete bool    `json:"window_complete"`
	} `json:"origin_coverage"`
}

func newOverviewTestHandler(t *testing.T, useRollup bool) (*DashboardHandler, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "overview.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open overview db: %v", err)
	}
	if err := database.AutoMigrate(
		&db.AccessLog{},
		&db.AccessLogFiveMinutely{},
		&db.AccessLogHourly{},
		&db.AccessLogDaily{},
		&db.ControlPlaneState{},
	); err != nil {
		t.Fatalf("migrate overview db: %v", err)
	}
	return NewDashboardHandler(database, nil, nil, useRollup, 0), database
}

func getOverview(t *testing.T, handler *DashboardHandler, query string) dashboardOverviewBody {
	t.Helper()
	router := gin.New()
	router.GET("/dashboard", handler.GetDashboard)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard"+query, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body dashboardOverviewBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	return body
}

func TestDashboardWindowReportsMeasuredOriginTraffic(t *testing.T) {
	handler, database := newOverviewTestHandler(t, false)
	now := time.Now().UTC()

	rows := []db.AccessLog{
		{AdapterType: "pypi", Method: "GET", PackageName: "requests", Hit: false, CacheResult: "miss",
			BytesSent: 1000, UpstreamRequests: 2, UpstreamBytes: 900, LatencyMs: 60, StatusCode: 200,
			CreatedAt: now.Add(-10 * time.Minute)},
		{AdapterType: "pypi", Method: "GET", PackageName: "requests", Hit: true, CacheResult: "hit",
			BytesSent: 500, LatencyMs: 5, StatusCode: 200, CreatedAt: now.Add(-5 * time.Minute)},
		// A blocked/incomplete outcome must not inflate the period denominator.
		{AdapterType: "pypi", Method: "GET", PackageName: "evil", Hit: false, CacheResult: "unknown",
			StatusCode: 403, CreatedAt: now.Add(-4 * time.Minute)},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed access logs: %v", err)
	}
	if err := database.Create(&db.ControlPlaneState{
		Key:   db.OriginTrafficCoverageKey,
		Value: now.Add(-2 * time.Hour).Format(time.RFC3339),
	}).Error; err != nil {
		t.Fatalf("seed coverage marker: %v", err)
	}

	body := getOverview(t, handler, "?range=1h")
	if body.Window.TotalRequests != 2 {
		t.Fatalf("total requests = %d, want 2 (blocked row excluded)", body.Window.TotalRequests)
	}
	if body.Window.UpstreamRequests != 2 || body.Window.UpstreamBytes != 900 {
		t.Fatalf("origin totals = (%d, %d), want (2, 900)", body.Window.UpstreamRequests, body.Window.UpstreamBytes)
	}
	if body.Window.HitBytes != 500 {
		t.Fatalf("hit bytes = %d, want 500", body.Window.HitBytes)
	}
	if !body.OriginCoverage.Measured || !body.OriginCoverage.WindowComplete || body.OriginCoverage.Since == nil {
		t.Fatalf("origin coverage = %+v, want complete and measured", body.OriginCoverage)
	}
}

func TestDashboardFlagsPartialOriginCoverage(t *testing.T) {
	handler, database := newOverviewTestHandler(t, false)
	now := time.Now().UTC()
	if err := database.Create(&db.AccessLog{
		AdapterType: "npm", Method: "GET", PackageName: "left-pad", Hit: false, CacheResult: "miss",
		BytesSent: 100, UpstreamRequests: 1, UpstreamBytes: 90, StatusCode: 200, CreatedAt: now.Add(-30 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed access log: %v", err)
	}
	// Metering began 10 minutes ago, after this 1h window started.
	if err := database.Create(&db.ControlPlaneState{
		Key:   db.OriginTrafficCoverageKey,
		Value: now.Add(-10 * time.Minute).Format(time.RFC3339),
	}).Error; err != nil {
		t.Fatalf("seed coverage marker: %v", err)
	}

	body := getOverview(t, handler, "?range=1h")
	if body.OriginCoverage.WindowComplete {
		t.Fatal("window reported complete origin coverage despite starting before metering began")
	}
	if !body.OriginCoverage.Measured {
		t.Fatal("coverage marker present but measured=false")
	}
}

func TestDashboardCoarseRangeReadsHourlyRollup(t *testing.T) {
	handler, database := newOverviewTestHandler(t, true)
	now := time.Now().UTC()
	date := now.Format("2006-01-02")
	if err := database.Create(&db.AccessLogHourly{
		Date: date, Hour: now.Hour(), AdapterType: "npm", Hit: false,
		RequestCount: 3, TotalBytes: 300, UpstreamRequests: 3, UpstreamBytes: 270, SumLatencyMs: 30,
	}).Error; err != nil {
		t.Fatalf("seed hourly rollup: %v", err)
	}

	body := getOverview(t, handler, "?range=7d")
	if body.Window.TotalRequests != 3 || body.Window.UpstreamBytes != 270 {
		t.Fatalf("7d window = %+v, want the hourly rollup totals", body.Window)
	}
}
