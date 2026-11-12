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

	"depsilo/internal/db"
)

type breakdownBody struct {
	Range struct {
		Key   string `json:"key"`
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"range"`
	ByEcosystem      []breakdownEcosystem `json:"by_ecosystem"`
	ByUpstream       []breakdownUpstream  `json:"by_upstream"`
	TopPackages      []breakdownPackage   `json:"top_packages"`
	TopPackagesGrain string               `json:"top_packages_grain"`
}

func performBreakdownRequest(handler *DashboardHandler, query string) *httptest.ResponseRecorder {
	router := gin.New()
	router.GET("/dashboard/breakdown", handler.GetBreakdown)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard/breakdown"+query, nil))
	return recorder
}

func getBreakdown(t *testing.T, handler *DashboardHandler, query string) breakdownBody {
	t.Helper()
	recorder := performBreakdownRequest(handler, query)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body breakdownBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode breakdown: %v", err)
	}
	return body
}

func seedBreakdownAccessLogs(t *testing.T, database *gorm.DB, now time.Time) {
	t.Helper()
	rows := []db.AccessLog{
		// PyPI: two hits (one without a package name) and two misses through tuna.
		{AdapterType: "pypi", PackageName: "numpy", Hit: true, CacheResult: "hit", BytesSent: 100, LatencyMs: 4, CreatedAt: now.Add(-10 * time.Minute)},
		{AdapterType: "pypi", PackageName: "", Hit: true, CacheResult: "hit", BytesSent: 13, LatencyMs: 6, CreatedAt: now.Add(-11 * time.Minute)},
		// The scipy client aborted after 500 bytes of a 2,000-byte origin read.
		{AdapterType: "pypi", PackageName: "numpy", Hit: false, CacheResult: "miss", Upstream: "tuna", BytesSent: 300, LatencyMs: 200, UpstreamRequests: 1, UpstreamBytes: 300, CreatedAt: now.Add(-20 * time.Minute)},
		{AdapterType: "pypi", PackageName: "scipy", Hit: false, CacheResult: "miss", Upstream: "tuna", BytesSent: 500, LatencyMs: 400, UpstreamRequests: 1, UpstreamBytes: 2000, CreatedAt: now.Add(-30 * time.Minute)},
		// APT also has an upstream named tuna; it is a different Upstream.
		{AdapterType: "apt", PackageName: "curl", Hit: false, CacheResult: "miss", Upstream: "tuna", BytesSent: 50, LatencyMs: 100, UpstreamRequests: 1, UpstreamBytes: 50, CreatedAt: now.Add(-5 * time.Minute)},
		// A revalidated hit still exchanged metadata with its Upstream.
		{AdapterType: "apt", PackageName: "curl", Hit: true, CacheResult: "hit", Upstream: "tuna", BytesSent: 0, LatencyMs: 0, UpstreamRequests: 1, UpstreamBytes: 4, CreatedAt: now.Add(-4 * time.Minute)},
		// A miss answered without selecting an Upstream counts for its ecosystem only.
		{AdapterType: "apt", PackageName: "curl", Hit: false, CacheResult: "miss", Upstream: "", BytesSent: 7, LatencyMs: 20, CreatedAt: now.Add(-6 * time.Minute)},
		// Pre-diagnostics rows carry an empty cache_result and still count.
		{AdapterType: "npm", PackageName: "left-pad", Hit: true, CacheResult: "", BytesSent: 11, LatencyMs: 2, CreatedAt: now.Add(-7 * time.Minute)},
		// Blocked or incomplete requests never enter the period.
		{AdapterType: "npm", PackageName: "blocked", Hit: false, CacheResult: "unknown", Upstream: "official", BytesSent: 999, LatencyMs: 1, StatusCode: 451, CreatedAt: now.Add(-8 * time.Minute)},
		// Outside one hour, inside 24 hours.
		{AdapterType: "pypi", PackageName: "torch", Hit: false, CacheResult: "miss", Upstream: "official", BytesSent: 9000, LatencyMs: 900, UpstreamRequests: 1, UpstreamBytes: 9000, CreatedAt: now.Add(-2 * time.Hour)},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed access logs: %v", err)
	}
}

func TestBreakdownRawWindowSplitsEcosystemsUpstreamsAndPackages(t *testing.T) {
	handler, database := newOverviewTestHandler(t, false)
	seedBreakdownAccessLogs(t, database, time.Now().UTC())

	body := getBreakdown(t, handler, "?range=1h")
	if body.Range.Key != "1h" || body.TopPackagesGrain != breakdownGrainExact {
		t.Fatalf("range = %+v, grain = %q, want an exact one-hour period", body.Range, body.TopPackagesGrain)
	}

	wantEcosystems := []breakdownEcosystem{
		{Ecosystem: "pypi", HitRequests: 2, MissRequests: 2, HitBytes: 113, MissBytes: 800, AvgHitLatencyMs: 5, AvgMissLatencyMs: 300},
		{Ecosystem: "apt", HitRequests: 1, MissRequests: 2, MissBytes: 57, AvgMissLatencyMs: 60},
		{Ecosystem: "npm", HitRequests: 1, HitBytes: 11, AvgHitLatencyMs: 2},
	}
	if fmt.Sprint(body.ByEcosystem) != fmt.Sprint(wantEcosystems) {
		t.Fatalf("by_ecosystem = %+v, want %+v", body.ByEcosystem, wantEcosystems)
	}

	wantUpstreams := []breakdownUpstream{
		{Ecosystem: "pypi", Upstream: "tuna", UpstreamRequests: 2, UpstreamBytes: 2300, MissRequests: 2, MissBytes: 800, AvgMissLatencyMs: 300},
		{Ecosystem: "apt", Upstream: "tuna", UpstreamRequests: 2, UpstreamBytes: 54, MissRequests: 1, MissBytes: 50, AvgMissLatencyMs: 100},
	}
	if fmt.Sprint(body.ByUpstream) != fmt.Sprint(wantUpstreams) {
		t.Fatalf("by_upstream = %+v, want measured origin bytes, same-named upstreams kept apart: %+v", body.ByUpstream, wantUpstreams)
	}

	wantPackages := []breakdownPackage{
		{Ecosystem: "pypi", PackageName: "scipy", Requests: 1, TotalBytes: 500},
		{Ecosystem: "pypi", PackageName: "numpy", Requests: 2, TotalBytes: 400, HitBytes: 100},
		{Ecosystem: "apt", PackageName: "curl", Requests: 3, TotalBytes: 57},
		{Ecosystem: "npm", PackageName: "left-pad", Requests: 1, TotalBytes: 11, HitBytes: 11},
	}
	if fmt.Sprint(body.TopPackages) != fmt.Sprint(wantPackages) {
		t.Fatalf("top_packages = %+v, want %+v", body.TopPackages, wantPackages)
	}

	day := getBreakdown(t, handler, "?range=24h")
	if day.ByEcosystem[0].Ecosystem != "pypi" || day.ByEcosystem[0].MissBytes != 9800 {
		t.Fatalf("24h pypi = %+v, want the older miss included", day.ByEcosystem[0])
	}
	if day.ByUpstream[0] != (breakdownUpstream{Ecosystem: "pypi", Upstream: "official", UpstreamRequests: 1, UpstreamBytes: 9000, MissRequests: 1, MissBytes: 9000, AvgMissLatencyMs: 900}) {
		t.Fatalf("24h by_upstream[0] = %+v, want pypi/official first", day.ByUpstream[0])
	}
}

func TestBreakdownCoarseRangeReadsRollupsAndMatchesThePeriodTotals(t *testing.T) {
	handler, database := newOverviewTestHandler(t, true)
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	stale := now.Add(-9 * 24 * time.Hour)

	hourly := []db.AccessLogHourly{
		{Date: today, Hour: now.Hour(), AdapterType: "npm", Hit: true, RequestCount: 2, TotalBytes: 200, SumLatencyMs: 8},
		{Date: today, Hour: now.Hour(), AdapterType: "npm", Hit: false, Upstream: "npmmirror", RequestCount: 3, TotalBytes: 300, SumLatencyMs: 900, UpstreamRequests: 3, UpstreamBytes: 280},
		{Date: today, Hour: now.Hour(), AdapterType: "go", Hit: false, Upstream: "official", RequestCount: 1, TotalBytes: 1000, SumLatencyMs: 50, UpstreamRequests: 1, UpstreamBytes: 1000},
		// Nine days old: outside 7d, inside 30d.
		{Date: stale.Format("2006-01-02"), Hour: stale.Hour(), AdapterType: "npm", Hit: false, Upstream: "npmmirror", RequestCount: 10, TotalBytes: 5000, SumLatencyMs: 100, UpstreamRequests: 10, UpstreamBytes: 5000},
	}
	if err := database.Create(&hourly).Error; err != nil {
		t.Fatalf("seed hourly rollups: %v", err)
	}
	packages := []db.AccessLogPackageDaily{
		{Date: today, AdapterType: "npm", PackageName: "react", Hit: true, RequestCount: 2, TotalBytes: 200},
		{Date: today, AdapterType: "npm", PackageName: "react", Hit: false, RequestCount: 3, TotalBytes: 300},
		{Date: today, AdapterType: "go", PackageName: "github.com/gin-gonic/gin", Hit: false, RequestCount: 1, TotalBytes: 1000},
		{Date: today, AdapterType: "npm", PackageName: "", Hit: false, RequestCount: 50, TotalBytes: 50000},
		{Date: stale.Format("2006-01-02"), AdapterType: "npm", PackageName: "lodash", Hit: false, RequestCount: 10, TotalBytes: 5000},
	}
	if err := database.Create(&packages).Error; err != nil {
		t.Fatalf("seed package rollups: %v", err)
	}
	// A coarse range must not rescan the access log.
	if err := database.Create(&db.AccessLog{AdapterType: "npm", PackageName: "raw-only", CacheResult: "miss", Upstream: "npmmirror", BytesSent: 77777, CreatedAt: now.Add(-time.Minute)}).Error; err != nil {
		t.Fatalf("seed raw access log: %v", err)
	}

	body := getBreakdown(t, handler, "?range=7d")
	if body.Range.Key != "7d" || body.TopPackagesGrain != breakdownGrainUTCDay {
		t.Fatalf("range = %+v, grain = %q, want a day-grained package ranking", body.Range, body.TopPackagesGrain)
	}
	wantEcosystems := []breakdownEcosystem{
		{Ecosystem: "go", MissRequests: 1, MissBytes: 1000, AvgMissLatencyMs: 50},
		{Ecosystem: "npm", HitRequests: 2, MissRequests: 3, HitBytes: 200, MissBytes: 300, AvgHitLatencyMs: 4, AvgMissLatencyMs: 300},
	}
	if fmt.Sprint(body.ByEcosystem) != fmt.Sprint(wantEcosystems) {
		t.Fatalf("by_ecosystem = %+v, want %+v", body.ByEcosystem, wantEcosystems)
	}
	wantUpstreams := []breakdownUpstream{
		{Ecosystem: "go", Upstream: "official", UpstreamRequests: 1, UpstreamBytes: 1000, MissRequests: 1, MissBytes: 1000, AvgMissLatencyMs: 50},
		{Ecosystem: "npm", Upstream: "npmmirror", UpstreamRequests: 3, UpstreamBytes: 280, MissRequests: 3, MissBytes: 300, AvgMissLatencyMs: 300},
	}
	if fmt.Sprint(body.ByUpstream) != fmt.Sprint(wantUpstreams) {
		t.Fatalf("by_upstream = %+v, want %+v", body.ByUpstream, wantUpstreams)
	}
	wantPackages := []breakdownPackage{
		{Ecosystem: "go", PackageName: "github.com/gin-gonic/gin", Requests: 1, TotalBytes: 1000},
		{Ecosystem: "npm", PackageName: "react", Requests: 5, TotalBytes: 500, HitBytes: 200},
	}
	if fmt.Sprint(body.TopPackages) != fmt.Sprint(wantPackages) {
		t.Fatalf("top_packages = %+v, want %+v", body.TopPackages, wantPackages)
	}

	// The breakdown and the period block describe the same request set.
	period := getOverview(t, handler, "?range=7d").Window
	var requests, hitBytes, originBytes int64
	for _, eco := range body.ByEcosystem {
		requests += eco.HitRequests + eco.MissRequests
		hitBytes += eco.HitBytes
	}
	for _, up := range body.ByUpstream {
		originBytes += up.UpstreamBytes
	}
	if requests != period.TotalRequests || hitBytes != period.HitBytes || originBytes != period.UpstreamBytes {
		t.Fatalf("breakdown totals = %d requests / %d hit bytes / %d origin bytes, period = %+v", requests, hitBytes, originBytes, period)
	}

	month := getBreakdown(t, handler, "?range=30d")
	if month.ByUpstream[0] != (breakdownUpstream{Ecosystem: "npm", Upstream: "npmmirror", UpstreamRequests: 13, UpstreamBytes: 5280, MissRequests: 13, MissBytes: 5300, AvgMissLatencyMs: 1000.0 / 13}) {
		t.Fatalf("30d by_upstream[0] = %+v, want the older rollup included", month.ByUpstream[0])
	}
	if month.TopPackages[0].PackageName != "lodash" {
		t.Fatalf("30d top_packages = %+v, want lodash first", month.TopPackages)
	}
}

func TestBreakdownWithoutRollupsKeepsCoarseRangesExact(t *testing.T) {
	handler, database := newOverviewTestHandler(t, false)
	seedBreakdownAccessLogs(t, database, time.Now().UTC())

	body := getBreakdown(t, handler, "?range=7d")
	if body.Range.Key != "7d" || body.TopPackagesGrain != breakdownGrainExact {
		t.Fatalf("range = %+v, grain = %q, want exact raw rows", body.Range, body.TopPackagesGrain)
	}
	if len(body.ByEcosystem) != 3 || body.ByEcosystem[0].MissBytes != 9800 {
		t.Fatalf("by_ecosystem = %+v, want every raw row of the week", body.ByEcosystem)
	}
}

func TestBreakdownEmptyPeriodReturnsEmptyListsAndDefaultsTheRange(t *testing.T) {
	for _, useRollup := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollup=%t", useRollup), func(t *testing.T) {
			handler, _ := newOverviewTestHandler(t, useRollup)
			recorder := performBreakdownRequest(handler, "?range=unsupported")
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			raw := recorder.Body.String()
			for _, field := range []string{`"by_ecosystem":[]`, `"by_upstream":[]`, `"top_packages":[]`, `"key":"24h"`} {
				if !strings.Contains(raw, field) {
					t.Fatalf("body = %s, want %s", raw, field)
				}
			}
		})
	}
}

func TestBreakdownRanksAtMostTenPackagesWithAStableOrder(t *testing.T) {
	handler, database := newOverviewTestHandler(t, false)
	now := time.Now().UTC()
	rows := make([]db.AccessLog, 0, 12)
	for i := range 12 {
		rows = append(rows, db.AccessLog{
			AdapterType: "npm", PackageName: fmt.Sprintf("pkg-%02d", i), CacheResult: "miss",
			Upstream: "official", BytesSent: 100, CreatedAt: now.Add(-time.Minute),
		})
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed access logs: %v", err)
	}

	body := getBreakdown(t, handler, "?range=1h")
	if len(body.TopPackages) != breakdownPackageLimit {
		t.Fatalf("top_packages = %d rows, want %d", len(body.TopPackages), breakdownPackageLimit)
	}
	for i, pkg := range body.TopPackages {
		if want := fmt.Sprintf("pkg-%02d", i); pkg.PackageName != want {
			t.Fatalf("top_packages[%d] = %q, want %q for equal traffic", i, pkg.PackageName, want)
		}
	}
}

func TestFoldBreakdownRowsBoundsTheUpstreamList(t *testing.T) {
	rows := make([]breakdownRow, 0, breakdownUpstreamLimit+5)
	for i := range breakdownUpstreamLimit + 5 {
		rows = append(rows, breakdownRow{AdapterType: "npm", Upstream: fmt.Sprintf("mirror-%02d", i), Requests: 1, Bytes: int64(i), UpstreamRequests: 1, UpstreamBytes: int64(i)})
	}
	byEcosystem, byUpstream := foldBreakdownRows(rows)
	if len(byUpstream) != breakdownUpstreamLimit || byUpstream[0].Upstream != "mirror-24" {
		t.Fatalf("by_upstream = %d rows starting at %+v, want the %d largest", len(byUpstream), byUpstream[0], breakdownUpstreamLimit)
	}
	if len(byEcosystem) != 1 || byEcosystem[0].MissRequests != int64(breakdownUpstreamLimit+5) {
		t.Fatalf("by_ecosystem = %+v, want every row counted", byEcosystem)
	}
}

func TestBreakdownReturnsDatabaseErrorForEveryQuery(t *testing.T) {
	tests := []struct {
		name       string
		useRollup  bool
		table      string
		occurrence int
	}{
		{name: "raw period cells", table: "access_logs", occurrence: 1},
		{name: "raw packages", table: "access_logs", occurrence: 2},
		{name: "rollup period cells", useRollup: true, table: "access_log_hourly", occurrence: 1},
		{name: "rollup packages", useRollup: true, table: "access_log_package_daily", occurrence: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, database := newOverviewTestHandler(t, tt.useRollup)
			seen := 0
			registerDashboardReadCallback(t, database, func(tx *gorm.DB) {
				if tx.Statement.Table != tt.table {
					return
				}
				seen++
				if seen == tt.occurrence {
					tx.AddError(errors.New("private database details must not reach the response"))
				}
			})
			assertDashboardDatabaseError(t, performBreakdownRequest(handler, "?range=7d"))
			if seen != tt.occurrence {
				t.Fatalf("matching queries = %d, want %d", seen, tt.occurrence)
			}
		})
	}
}
