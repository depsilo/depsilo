package public

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/api/contract"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

func TestStatsPublicUpstreamURLDoesNotExposeCredentials(t *testing.T) {
	database := newStatsTestDB(t)
	if err := database.AutoMigrate(&db.AccessLog{}, &db.CacheEntry{}); err != nil {
		t.Fatal(err)
	}
	pool, err := upstream.NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "pypi", Name: "private",
		URL:      "https://alice:secret@packages.example.test:8443/private/path",
		Priority: 1, Healthy: true,
	}})
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/stats", NewStatsHandler(database, nil, map[string]*upstream.Pool{"pypi": pool}, []string{"pypi"}, nil, false).GetStats)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/stats", nil))

	var body struct {
		Upstreams []struct {
			URL string `json:"url"`
		} `json:"upstreams"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || len(body.Upstreams) != 1 {
		t.Fatalf("decode: err=%v body=%s", err, recorder.Body.String())
	}
	if got := body.Upstreams[0].URL; got != "https://packages.example.test:8443" {
		t.Fatalf("public upstream URL = %q", got)
	}
}

// Latency series are keyed by the stable upstream ID, so same-named records,
// renames and deleted records never merge. Rows without an ID keep an
// explicit legacy key per name; no series is keyed by a bare name.
func TestAllUpstreamLatencySeriesUsesStableIDsAndExplicitLegacyKeys(t *testing.T) {
	database := newStatsTestDB(t)
	for _, record := range []db.UpstreamRecord{
		{ID: 11, AdapterType: "pypi", Name: "shared", URL: "https://one.example"},
		{ID: 22, AdapterType: "npm", Name: "shared", URL: "https://two.example"},
		{ID: 33, AdapterType: "pypi", Name: "current", URL: "https://current.example"},
	} {
		if err := database.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
	since := time.Date(2026, time.July, 11, 0, 5, 0, 0, time.UTC)
	checkedAt := since.Add(time.Minute)
	logs := []db.UpstreamLatencyLog{
		{UpstreamID: 11, Name: "old-one", LatencyMs: 10, Healthy: false, CreatedAt: checkedAt},
		{UpstreamID: 22, Name: "old-two", LatencyMs: 30, Healthy: true, CreatedAt: checkedAt},
		{UpstreamID: 22, Name: "old-two", LatencyMs: 30, Healthy: true, CreatedAt: checkedAt},
		{UpstreamID: 22, Name: "old-two", LatencyMs: 30, Healthy: true, CreatedAt: checkedAt},
		{UpstreamID: 33, Name: "before-rename", LatencyMs: 11, Healthy: true, CreatedAt: checkedAt},
		{UpstreamID: 44, Name: "deleted", LatencyMs: 22, Healthy: true, CreatedAt: checkedAt},
		{UpstreamID: 0, Name: "shared", LatencyMs: 50, Healthy: false, CreatedAt: checkedAt},
		{UpstreamID: 0, Name: "shared", LatencyMs: 50, Healthy: false, CreatedAt: checkedAt},
		{UpstreamID: 0, Name: "extra-one", LatencyMs: 33, Healthy: true, CreatedAt: checkedAt},
		{UpstreamID: 0, Name: "extra-two", LatencyMs: 44, Healthy: true, CreatedAt: checkedAt},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}

	series, err := (&StatsHandler{db: database}).allUpstreamLatencySeries(since)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 7 {
		t.Fatalf("series keys=%v", mapKeys(series))
	}
	for key, want := range map[string]struct {
		requests int64
		latency  int64
		healthy  bool
	}{
		"11":               {requests: 1, latency: 10, healthy: false},
		"22":               {requests: 3, latency: 30, healthy: true},
		"33":               {requests: 1, latency: 11, healthy: true},
		"44":               {requests: 1, latency: 22, healthy: true},
		"legacy:shared":    {requests: 2, latency: 50, healthy: false},
		"legacy:extra-one": {requests: 1, latency: 33, healthy: true},
		"legacy:extra-two": {requests: 1, latency: 44, healthy: true},
	} {
		points := series[key]
		if len(points) != latencyBuckets {
			t.Fatalf("series[%q] points=%d want=%d; keys=%v", key, len(points), latencyBuckets, mapKeys(series))
		}
		if got := points[0].Requests; got != want.requests {
			t.Fatalf("series[%q] requests=%#v want=%d", key, got, want.requests)
		}
		if got := points[0].LatencyMs; got != want.latency {
			t.Fatalf("series[%q] latency_ms=%#v want=%d", key, got, want.latency)
		}
		if got := points[0].Healthy; got != want.healthy {
			t.Fatalf("series[%q] healthy=%#v want=%t", key, got, want.healthy)
		}
	}
	for _, unstableName := range []string{"shared", "current", "before-rename", "deleted", "extra-one", "extra-two"} {
		if _, exists := series[unstableName]; exists {
			t.Fatalf("name-keyed series %q retained: %v", unstableName, mapKeys(series))
		}
	}
	startBucket := (since.Unix() / int64(latencyIntervalMin*60)) * int64(latencyIntervalMin*60)
	assertSeriesTimes(t, series["11"], startBucket)
}

func TestStatsServiceStatusReflectsUpstreamHealth(t *testing.T) {
	tests := []struct {
		name    string
		records []db.UpstreamRecord
		want    string
	}{
		{name: "no upstreams", want: "healthy"},
		{
			name: "all healthy",
			records: []db.UpstreamRecord{
				{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Healthy: true, AvgLatencyMs: 999},
				{ID: 2, AdapterType: "pypi", Name: "two", URL: "https://two.example", Healthy: true, AvgLatencyMs: 999},
			},
			want: "healthy",
		},
		{
			name: "available but slow",
			records: []db.UpstreamRecord{
				{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Healthy: true, AvgLatencyMs: 1000},
			},
			want: "degraded",
		},
		{
			name: "partial outage",
			records: []db.UpstreamRecord{
				{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Healthy: true},
				{ID: 2, AdapterType: "pypi", Name: "two", URL: "https://two.example", Healthy: false},
			},
			want: "degraded",
		},
		{
			name: "slow source keeps failed pool available",
			records: []db.UpstreamRecord{
				{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Healthy: true, AvgLatencyMs: 1200},
				{ID: 2, AdapterType: "pypi", Name: "two", URL: "https://two.example", Healthy: false},
			},
			want: "degraded",
		},
		{
			name: "complete outage",
			records: []db.UpstreamRecord{
				{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Healthy: false},
				{ID: 2, AdapterType: "pypi", Name: "two", URL: "https://two.example", Healthy: false},
			},
			want: "failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := newStatsTestDB(t)
			if err := database.AutoMigrate(&db.AccessLog{}, &db.CacheEntry{}); err != nil {
				t.Fatal(err)
			}
			pools := map[string]*upstream.Pool{}
			ecosystems := []string(nil)
			if len(tt.records) > 0 {
				pool, err := upstream.NewPoolFromRecords(tt.records)
				if err != nil {
					t.Fatal(err)
				}
				pools["pypi"] = pool
				ecosystems = []string{"pypi"}
			}
			snapshot := NewStatsHandler(database, nil, pools, ecosystems, nil, false).snapshot(time.Now())
			if got := snapshot.Service.Status; string(got) != tt.want {
				t.Fatalf("status=%#v want=%q", got, tt.want)
			}
		})
	}
}

// Rollup rows are keyed by the request's UTC date. "Today" has to be that
// same UTC day wherever the host's clock is: local midnight converted to UTC
// names the previous date on a UTC+8 host from 08:00 local, and the next
// UTC day's rows are missed on a UTC-5 host in the evening.
func TestStatsTodayIsTheCurrentUTCDayOnNonUTCHosts(t *testing.T) {
	// One instant per case, 2026-11-16 UTC in both.
	cases := map[string]time.Time{
		"UTC+8 afternoon": time.Date(2026, time.November, 16, 16, 36, 0, 0, time.FixedZone("CST", 8*3600)),
		"UTC-5 evening":   time.Date(2026, time.November, 15, 20, 30, 0, 0, time.FixedZone("EST", -5*3600)),
	}
	for name, now := range cases {
		for _, useRollup := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s rollup=%t", name, useRollup), func(t *testing.T) {
				if got := now.UTC().Format("2006-01-02"); got != "2026-11-16" {
					t.Fatalf("test instant is on UTC date %s", got)
				}
				database := newStatsTestDB(t)
				if err := database.AutoMigrate(&db.AccessLog{}, &db.CacheEntry{}, &db.AccessLogHourly{}, &db.AccessLogDaily{}); err != nil {
					t.Fatal(err)
				}
				seedStatsDays(t, database, useRollup)

				snapshot := NewStatsHandler(database, nil, nil, nil, nil, useRollup).snapshot(now)
				// 2026-11-16 UTC: 3 hits of 100 bytes and 1 miss of 50 bytes.
				wantToday := contract.StatsToday{TotalRequests: 4, HitCount: 3, MissCount: 1, HitRate: 0.75, BytesServed: 350, BytesSaved: 300}
				if snapshot.Today != wantToday {
					t.Errorf("today = %+v, want %+v", snapshot.Today, wantToday)
				}
				// The week adds 2026-11-10 through 2026-11-15 (one hit of 10
				// bytes each day that has data) and leaves 2026-11-09 out.
				wantWeek := contract.StatsWeek{TotalRequests: 6, HitCount: 5, HitRate: 5.0 / 6.0, BytesSaved: 320}
				if snapshot.Week != wantWeek {
					t.Errorf("week = %+v, want %+v", snapshot.Week, wantWeek)
				}
			})
		}
	}
}

// seedStatsDays records the same traffic either as rollup rows or as raw
// access logs: four requests on 2026-11-16 UTC, one hit on 2026-11-15 and on
// 2026-11-10, and one on 2026-11-09, which is outside the seven-day window.
func seedStatsDays(t *testing.T, database *gorm.DB, useRollup bool) {
	t.Helper()
	if useRollup {
		hourly := []db.AccessLogHourly{
			{Date: "2026-11-16", Hour: 0, AdapterType: "pypi", Hit: true, RequestCount: 2, TotalBytes: 200},
			{Date: "2026-11-16", Hour: 1, AdapterType: "pypi", Hit: true, RequestCount: 1, TotalBytes: 100},
			{Date: "2026-11-16", Hour: 1, AdapterType: "pypi", Hit: false, RequestCount: 1, TotalBytes: 50},
			// The compactor leaves hourly rows in place; they must not be
			// counted again as today.
			{Date: "2026-11-15", Hour: 23, AdapterType: "pypi", Hit: true, RequestCount: 1, TotalBytes: 10},
		}
		daily := []db.AccessLogDaily{
			{Date: "2026-11-15", AdapterType: "pypi", Hit: true, RequestCount: 1, TotalBytes: 10},
			{Date: "2026-11-10", AdapterType: "pypi", Hit: true, RequestCount: 1, TotalBytes: 10},
			{Date: "2026-11-09", AdapterType: "pypi", Hit: true, RequestCount: 1, TotalBytes: 10},
		}
		if err := database.Create(&hourly).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Create(&daily).Error; err != nil {
			t.Fatal(err)
		}
		return
	}
	at := func(day, hour, minute int) time.Time {
		return time.Date(2026, time.November, day, hour, minute, 0, 0, time.UTC)
	}
	logs := []db.AccessLog{
		{AdapterType: "pypi", Hit: true, BytesSent: 100, CreatedAt: at(16, 0, 0)},
		{AdapterType: "pypi", Hit: true, BytesSent: 100, CreatedAt: at(16, 0, 20)},
		{AdapterType: "pypi", Hit: true, BytesSent: 100, CreatedAt: at(16, 1, 5)},
		{AdapterType: "pypi", Hit: false, BytesSent: 50, CreatedAt: at(16, 1, 10)},
		{AdapterType: "pypi", Hit: true, BytesSent: 10, CreatedAt: at(15, 23, 59)},
		{AdapterType: "pypi", Hit: true, BytesSent: 10, CreatedAt: at(10, 0, 0)},
		{AdapterType: "pypi", Hit: true, BytesSent: 10, CreatedAt: at(9, 23, 59)},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}
}

func TestLatencySeriesReturnsStableDatabaseError(t *testing.T) {
	database := newStatsTestDB(t)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := (&StatsHandler{db: database}).allUpstreamLatencySeries(time.Now().Add(-24 * time.Hour)); err == nil {
		t.Fatal("closed database query succeeded")
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/latency-series", (&StatsHandler{db: database}).GetLatencySeries)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/latency-series", nil))
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusInternalServerError || body["code"] != "DB_ERROR" || body["message"] != "failed to load latency series" {
		t.Fatalf("status=%d body=%#v", recorder.Code, body)
	}
}

func newStatsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "stats.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.UpstreamRecord{}, &db.UpstreamLatencyLog{}); err != nil {
		t.Fatal(err)
	}
	return database
}

func assertSeriesTimes(t *testing.T, points []contract.StatsLatencyPoint, startBucket int64) {
	t.Helper()
	for i, point := range points {
		want := time.Unix(startBucket+int64(i*latencyIntervalMin*60), 0).UTC().Format(time.RFC3339)
		if point.Time != want {
			t.Fatalf("point[%d].time=%#v want=%q", i, point.Time, want)
		}
	}
}

func mapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
