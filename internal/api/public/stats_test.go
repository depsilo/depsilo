package public

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

func TestAllUpstreamLatencySeriesKeepsSameNamedRecordsIsolatedByID(t *testing.T) {
	database := newStatsTestDB(t)
	for _, record := range []db.UpstreamRecord{
		{ID: 11, AdapterType: "pypi", Name: "shared", URL: "https://one.example"},
		{ID: 22, AdapterType: "npm", Name: "shared", URL: "https://two.example"},
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
		{UpstreamID: 0, Name: "shared", LatencyMs: 50, Healthy: false, CreatedAt: checkedAt},
		{UpstreamID: 0, Name: "shared", LatencyMs: 50, Healthy: false, CreatedAt: checkedAt},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}

	series, err := (&StatsHandler{db: database}).allUpstreamLatencySeries(since)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 3 {
		t.Fatalf("series keys=%v", mapKeys(series))
	}
	for key, want := range map[string]struct {
		requests int64
		latency  int64
		healthy  bool
	}{
		"11":            {requests: 1, latency: 10, healthy: false},
		"22":            {requests: 3, latency: 30, healthy: true},
		"legacy:shared": {requests: 2, latency: 50, healthy: false},
	} {
		points := series[key]
		if len(points) != latencyBuckets {
			t.Fatalf("series[%q] points=%d want=%d; keys=%v", key, len(points), latencyBuckets, mapKeys(series))
		}
		if got := points[0]["requests"]; got != want.requests {
			t.Fatalf("series[%q] requests=%#v want=%d", key, got, want.requests)
		}
		if got := points[0]["latency_ms"]; got != want.latency {
			t.Fatalf("series[%q] latency_ms=%#v want=%d", key, got, want.latency)
		}
		if got := points[0]["healthy"]; got != want.healthy {
			t.Fatalf("series[%q] healthy=%#v want=%t", key, got, want.healthy)
		}
	}
	startBucket := (since.Unix() / int64(latencyIntervalMin*60)) * int64(latencyIntervalMin*60)
	assertSeriesTimes(t, series["11"], startBucket)
}

func TestAllUpstreamLatencySeriesUsesStableIDsAndExplicitLegacyKeys(t *testing.T) {
	database := newStatsTestDB(t)
	record := db.UpstreamRecord{ID: 33, AdapterType: "pypi", Name: "current", URL: "https://current.example"}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, time.July, 11, 2, 5, 0, 0, time.UTC)
	checkedAt := since.Add(time.Minute)
	logs := []db.UpstreamLatencyLog{
		{UpstreamID: 33, Name: "before-rename", LatencyMs: 11, Healthy: true, CreatedAt: checkedAt},
		{UpstreamID: 44, Name: "deleted", LatencyMs: 22, Healthy: true, CreatedAt: checkedAt},
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
	for key, latency := range map[string]int64{"33": 11, "44": 22, "legacy:extra-one": 33, "legacy:extra-two": 44} {
		points, ok := series[key]
		if !ok || len(points) != latencyBuckets || points[0]["latency_ms"] != latency {
			t.Fatalf("series[%q]=%#v; keys=%v", key, points, mapKeys(series))
		}
	}
	for _, unstableName := range []string{"current", "before-rename", "deleted", "extra-one", "extra-two"} {
		if _, exists := series[unstableName]; exists {
			t.Fatalf("name-keyed series %q retained: %v", unstableName, mapKeys(series))
		}
	}
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
				{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Healthy: true, AvgLatencyMs: 149},
				{ID: 2, AdapterType: "pypi", Name: "two", URL: "https://two.example", Healthy: true, AvgLatencyMs: 149},
			},
			want: "healthy",
		},
		{
			name: "available but slow",
			records: []db.UpstreamRecord{
				{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Healthy: true, AvgLatencyMs: 150},
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
				{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Healthy: true, AvgLatencyMs: 200},
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
			service, ok := snapshot["service"].(gin.H)
			if !ok {
				t.Fatalf("service=%#v", snapshot["service"])
			}
			if got := service["status"]; got != tt.want {
				t.Fatalf("status=%#v want=%q", got, tt.want)
			}
		})
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

func assertSeriesTimes(t *testing.T, points []gin.H, startBucket int64) {
	t.Helper()
	for i, point := range points {
		want := time.Unix(startBucket+int64(i*latencyIntervalMin*60), 0).UTC().Format(time.RFC3339)
		if point["time"] != want {
			t.Fatalf("point[%d].time=%#v want=%q", i, point["time"], want)
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
