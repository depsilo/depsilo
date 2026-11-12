package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/accesslog"
	"depsilo/internal/db"
)

type allTimeOverview struct {
	Range struct {
		Key   string `json:"key"`
		Start string `json:"start"`
	} `json:"range"`
	Window struct {
		TotalRequests int64 `json:"total_requests"`
		HitRequests   int64 `json:"hit_requests"`
		HitBytes      int64 `json:"hit_bytes"`
		UpstreamBytes int64 `json:"upstream_bytes"`
		Errors        int64 `json:"errors"`
	} `json:"window"`
	Prev           *json.RawMessage `json:"prev"`
	OriginCoverage struct {
		WindowComplete bool `json:"window_complete"`
	} `json:"origin_coverage"`
	Archived *bool `json:"-"`
}

func getAllTimeOverview(t *testing.T, handler *DashboardHandler) allTimeOverview {
	t.Helper()
	router := gin.New()
	router.GET("/dashboard", handler.GetDashboard)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard?range=all", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body allTimeOverview
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

// seedAllTimeHistory writes rollups across a year and a half, then lets
// retention archive everything older than 365 days, as a long-running
// instance would have.
func seedAllTimeHistory(t *testing.T, database *gorm.DB, now time.Time) (string, string) {
	t.Helper()
	ancient := now.AddDate(0, 0, -500)
	old := now.AddDate(0, 0, -400)
	recent := now.AddDate(0, 0, -3)
	hourly := []db.AccessLogHourly{
		{Date: ancient.Format("2006-01-02"), Hour: 7, AdapterType: "pypi", Hit: true, RequestCount: 10, TotalBytes: 1000, SumLatencyMs: 50},
		{Date: old.Format("2006-01-02"), Hour: 2, AdapterType: "pypi", Hit: false, Upstream: "tuna", RequestCount: 4, TotalBytes: 400, UpstreamRequests: 4, UpstreamBytes: 420, SumLatencyMs: 800, ErrorCount: 1},
		{Date: recent.Format("2006-01-02"), Hour: 5, AdapterType: "npm", Hit: true, RequestCount: 6, TotalBytes: 600, SumLatencyMs: 12},
		{Date: recent.Format("2006-01-02"), Hour: 6, AdapterType: "npm", Hit: false, Upstream: "npmmirror", RequestCount: 2, TotalBytes: 200, UpstreamRequests: 2, UpstreamBytes: 190, SumLatencyMs: 400},
	}
	if err := database.Create(&hourly).Error; err != nil {
		t.Fatal(err)
	}
	packages := []db.AccessLogPackageDaily{
		{Date: ancient.Format("2006-01-02"), AdapterType: "pypi", PackageName: "numpy", Hit: true, RequestCount: 10, TotalBytes: 1000},
		{Date: old.Format("2006-01-02"), AdapterType: "pypi", PackageName: "torch", Hit: false, RequestCount: 4, TotalBytes: 400},
		{Date: recent.Format("2006-01-02"), AdapterType: "npm", PackageName: "react", Hit: true, RequestCount: 6, TotalBytes: 600},
		{Date: recent.Format("2006-01-02"), AdapterType: "npm", PackageName: "react", Hit: false, RequestCount: 2, TotalBytes: 200},
	}
	if err := database.Create(&packages).Error; err != nil {
		t.Fatal(err)
	}
	if err := accesslog.ArchiveRollupsBefore(t.Context(), database, now.AddDate(0, 0, -365).Format("2006-01-02")); err != nil {
		t.Fatalf("archive: %v", err)
	}
	return ancient.Format("2006-01-02"), recent.Format("2006-01-02")
}

func TestAllTimeOverviewAddsTheArchiveToRetainedRollups(t *testing.T) {
	handler, database := newOverviewTestHandler(t, true)
	now := time.Now().UTC()
	ancient, _ := seedAllTimeHistory(t, database, now)

	body := getAllTimeOverview(t, handler)
	if body.Range.Key != "all" {
		t.Fatalf("range key = %q, want all", body.Range.Key)
	}
	if !strings.HasPrefix(body.Range.Start, ancient) {
		t.Fatalf("range start = %q, want coverage from the archived %s", body.Range.Start, ancient)
	}
	if body.Window.TotalRequests != 22 || body.Window.HitRequests != 16 || body.Window.HitBytes != 1600 || body.Window.UpstreamBytes != 610 || body.Window.Errors != 1 {
		t.Fatalf("window = %+v, want archive plus retained rollups", body.Window)
	}
	if body.Prev != nil {
		t.Fatalf("prev = %s, want no previous period for all time", *body.Prev)
	}
	if body.OriginCoverage.WindowComplete {
		t.Fatal("origin coverage reported complete for a window that predates metering")
	}
}

func TestAllTimeOriginCoverageIsCompleteWhenMeteringBeganWithinTheFirstHour(t *testing.T) {
	handler, database := newOverviewTestHandler(t, true)
	now := time.Now().UTC()
	firstHour := now.Add(-5 * time.Hour).Truncate(time.Hour)
	// Metering began 23 minutes into the hour the first request was rolled up in.
	if err := database.Save(&db.ControlPlaneState{Key: db.OriginTrafficCoverageKey, Value: firstHour.Add(23 * time.Minute).Format(time.RFC3339)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.AccessLogHourly{Date: firstHour.Format("2006-01-02"), Hour: firstHour.Hour(), AdapterType: "npm", Hit: true, RequestCount: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if body := getAllTimeOverview(t, handler); !body.OriginCoverage.WindowComplete {
		t.Fatal("all-time origin coverage reported partial for an instance metered from its first hour")
	}
}

func TestAllTimeRangeSaysWhetherArchivedDaysAreIncluded(t *testing.T) {
	handler, database := newOverviewTestHandler(t, true)
	router := gin.New()
	router.GET("/dashboard", handler.GetDashboard)
	archivedFlag := func() any {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard?range=all", nil))
		var body struct {
			Range map[string]any `json:"range"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Range["archived"]
	}
	if got := archivedFlag(); got != false {
		t.Fatalf("archived = %v before any day expired, want false", got)
	}
	seedAllTimeHistory(t, database, time.Now().UTC())
	if got := archivedFlag(); got != true {
		t.Fatalf("archived = %v after expired days were folded in, want true", got)
	}
}

func TestAllTimeBreakdownAndTrendsCoverTheSameHistory(t *testing.T) {
	handler, database := newOverviewTestHandler(t, true)
	now := time.Now().UTC()
	_, recent := seedAllTimeHistory(t, database, now)

	body := getBreakdown(t, handler, "?range=all")
	if body.Range.Key != "all" || body.TopPackagesGrain != breakdownGrainUTCDay {
		t.Fatalf("range = %+v, grain = %q", body.Range, body.TopPackagesGrain)
	}
	var requests int64
	for _, eco := range body.ByEcosystem {
		requests += eco.HitRequests + eco.MissRequests
	}
	if requests != 22 {
		t.Fatalf("by_ecosystem = %+v, want 22 requests including the archive", body.ByEcosystem)
	}
	wantUpstreams := []breakdownUpstream{
		{Ecosystem: "pypi", Upstream: "tuna", UpstreamRequests: 4, UpstreamBytes: 420, MissRequests: 4, MissBytes: 400, AvgMissLatencyMs: 200},
		{Ecosystem: "npm", Upstream: "npmmirror", UpstreamRequests: 2, UpstreamBytes: 190, MissRequests: 2, MissBytes: 200, AvgMissLatencyMs: 200},
	}
	if fmt.Sprint(body.ByUpstream) != fmt.Sprint(wantUpstreams) {
		t.Fatalf("by_upstream = %+v, want %+v", body.ByUpstream, wantUpstreams)
	}
	wantPackages := []breakdownPackage{
		{Ecosystem: "pypi", PackageName: "numpy", Requests: 10, TotalBytes: 1000, HitBytes: 1000},
		{Ecosystem: "npm", PackageName: "react", Requests: 8, TotalBytes: 800, HitBytes: 600},
		{Ecosystem: "pypi", PackageName: "torch", Requests: 4, TotalBytes: 400},
	}
	if fmt.Sprint(body.TopPackages) != fmt.Sprint(wantPackages) {
		t.Fatalf("top_packages = %+v, want %+v", body.TopPackages, wantPackages)
	}

	// Trends are dated rows only: one UTC day per bucket from the oldest
	// retained rollup day, ending today.
	router := gin.New()
	router.GET("/dashboard/trends", handler.GetTrends)
	points := getTrendPoints(t, router, "all")
	if len(points) != 4 {
		t.Fatalf("all-time trend buckets = %d, want 4 days", len(points))
	}
	if got := time.Unix(points[0].Bucket, 0).UTC().Format("2006-01-02"); got != recent {
		t.Fatalf("first trend day = %s, want %s", got, recent)
	}
	var trendRequests int64
	for _, point := range points {
		trendRequests += point.Requests
		if point.Bucket%86400 != 0 {
			t.Fatalf("bucket %d is not a UTC day", point.Bucket)
		}
	}
	if trendRequests != 8 {
		t.Fatalf("trend requests = %d, want the 8 retained-day requests", trendRequests)
	}
}

func TestAllTimeWithoutRollupsCoversTheRetainedRawLog(t *testing.T) {
	handler, database := newOverviewTestHandler(t, false)
	now := time.Now().UTC()
	first := now.Add(-50 * time.Hour).Truncate(time.Second)
	rows := []db.AccessLog{
		{AdapterType: "pypi", PackageName: "numpy", Hit: true, CacheResult: "hit", BytesSent: 10, CreatedAt: first},
		{AdapterType: "pypi", PackageName: "numpy", Hit: false, CacheResult: "miss", Upstream: "tuna", BytesSent: 30, UpstreamRequests: 1, UpstreamBytes: 31, CreatedAt: now.Add(-time.Hour)},
		{AdapterType: "npm", PackageName: "blocked", CacheResult: "unknown", StatusCode: 451, CreatedAt: now.Add(-60 * time.Hour)},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	body := getAllTimeOverview(t, handler)
	if body.Window.TotalRequests != 2 || body.Window.UpstreamBytes != 31 {
		t.Fatalf("window = %+v, want both retained hit/miss rows", body.Window)
	}
	start, err := time.Parse(time.RFC3339, body.Range.Start)
	if err != nil || !start.Equal(first) {
		t.Fatalf("range start = %q, want %s (blocked rows do not extend coverage)", body.Range.Start, first.Format(time.RFC3339))
	}
	breakdown := getBreakdown(t, handler, "?range=all")
	if breakdown.TopPackagesGrain != breakdownGrainExact || len(breakdown.TopPackages) != 1 || breakdown.TopPackages[0].Requests != 2 {
		t.Fatalf("raw all-time breakdown = %+v", breakdown)
	}
	router := gin.New()
	router.GET("/dashboard/trends", handler.GetTrends)
	if points := getTrendPoints(t, router, "all"); len(points) < 3 || len(points) > 4 {
		t.Fatalf("raw all-time trend buckets = %d, want the days since the first row", len(points))
	}
}

func TestAllTimeEmptyInstanceStartsNow(t *testing.T) {
	handler, _ := newOverviewTestHandler(t, true)
	body := getAllTimeOverview(t, handler)
	start, err := time.Parse(time.RFC3339, body.Range.Start)
	if err != nil || time.Since(start) > time.Minute || body.Window.TotalRequests != 0 {
		t.Fatalf("empty all-time = %+v, want a zero window starting now", body)
	}
	router := gin.New()
	router.GET("/dashboard/trends", handler.GetTrends)
	if points := getTrendPoints(t, router, "all"); len(points) != 1 || points[0].Requests != 0 {
		t.Fatalf("empty all-time trends = %+v, want one empty day", points)
	}
}

func TestAllTimeTrendSpecSwitchesToWeeksForLongHistory(t *testing.T) {
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	if spec := allTimeTrendSpec(now.AddDate(0, 0, -10), true, now); spec.interval != 24*time.Hour || spec.buckets != 11 {
		t.Fatalf("10-day spec = %+v", spec)
	}
	spec := allTimeTrendSpec(now.AddDate(-3, 0, 0), true, now)
	if spec.interval != 7*24*time.Hour || spec.buckets < 156 || spec.buckets > 158 {
		t.Fatalf("3-year spec = %+v, want weekly buckets", spec)
	}
}

func TestAllTimeWeeklyTrendBucketsCarryTheirRequests(t *testing.T) {
	handler, database := newOverviewTestHandler(t, true)
	now := time.Now().UTC()
	// More than two years of retained rollups switch the series to weeks.
	for _, back := range []int{800, 400, 2} {
		day := now.AddDate(0, 0, -back)
		if err := database.Create(&db.AccessLogHourly{Date: day.Format("2006-01-02"), Hour: 12, AdapterType: "npm", Hit: true, RequestCount: 5}).Error; err != nil {
			t.Fatal(err)
		}
	}
	router := gin.New()
	router.GET("/dashboard/trends", handler.GetTrends)
	points := getTrendPoints(t, router, "all")
	var total int64
	for _, point := range points {
		total += point.Requests
	}
	if len(points) < 110 || len(points) > 120 || total != 15 {
		t.Fatalf("weekly series = %d buckets with %d requests, want ~115 weeks carrying all 15", len(points), total)
	}
}

func TestAllTimeReturnsDatabaseErrors(t *testing.T) {
	for _, table := range []string{"access_log_archive", "access_log_hourly"} {
		t.Run(table, func(t *testing.T) {
			handler, database := newOverviewTestHandler(t, true)
			registerDashboardReadCallback(t, database, func(tx *gorm.DB) {
				if tx.Statement.Table == table {
					tx.AddError(errors.New("private database details must not reach the response"))
				}
			})
			assertDashboardDatabaseError(t, performBreakdownRequest(handler, "?range=all"))
		})
	}
}
