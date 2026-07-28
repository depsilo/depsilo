package admin

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/accesslog"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

var fixedTrendsNow = time.Date(2026, time.July, 12, 12, 34, 56, 0, time.UTC)

func newTrendsTestHandler(t *testing.T) (*DashboardHandler, *gin.Engine) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "trends.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open trends db: %v", err)
	}
	if err := database.AutoMigrate(
		&db.AccessLog{},
		&db.AccessLogFiveMinutely{},
		&db.AccessLogHourly{},
		&db.ControlPlaneState{},
	); err != nil {
		t.Fatalf("migrate trends db: %v", err)
	}

	handler := NewDashboardHandler(database, nil, nil, true, 0)
	handler.now = func() time.Time { return fixedTrendsNow }

	// Normal fixtures model a completed transactional backfill. Fallback
	// tests explicitly remove this marker while retaining fine rows.
	if err := database.Create(&db.ControlPlaneState{
		Key:   accesslog.FiveMinuteBackfillMarker,
		Value: "true",
	}).Error; err != nil {
		t.Fatalf("create five-minute backfill marker: %v", err)
	}

	router := gin.New()
	router.GET("/dashboard/trends", handler.GetTrends)
	return handler, router
}

func getTrendPoints(t *testing.T, router http.Handler, rangeParam string) []trendPoint {
	t.Helper()

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard/trends?range="+rangeParam, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var body struct {
		Points []trendPoint `json:"points"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode trends response: %v", err)
	}
	return body.Points
}

func performTrendRequest(router http.Handler, rangeParam string, ctx context.Context) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/dashboard/trends?range="+rangeParam, nil)
	if ctx != nil {
		request = request.WithContext(ctx)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func assertTrendDatabaseError(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode database error: %v", err)
	}
	if len(body) != 2 || body["code"] != "DB_ERROR" || body["message"] != "failed to load dashboard trends" {
		t.Fatalf("database error body = %#v", body)
	}
}

func trendQueryTable(tx *gorm.DB) string {
	if tx.Statement.Table != "" {
		return tx.Statement.Table
	}
	switch tx.Statement.Model.(type) {
	case *db.AccessLog:
		return "access_logs"
	case *db.AccessLogFiveMinutely:
		return "access_log_five_minutely"
	case *db.AccessLogHourly:
		return "access_log_hourly"
	case *db.ControlPlaneState:
		return "control_plane_states"
	default:
		return ""
	}
}

func registerTrendRowCallback(t *testing.T, database *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()
	const callbackName = "test:dashboard_trends_row"
	if err := database.Callback().Row().Before("gorm:row").Register(callbackName, callback); err != nil {
		t.Fatalf("register row callback: %v", err)
	}
	t.Cleanup(func() { _ = database.Callback().Row().Remove(callbackName) })
}

func assertTrendResolution(t *testing.T, points []trendPoint, wantPoints int, wantStep int64) {
	t.Helper()
	if len(points) != wantPoints {
		t.Fatalf("points = %d, want %d", len(points), wantPoints)
	}
	for i := 1; i < len(points); i++ {
		if step := points[i].Bucket - points[i-1].Bucket; step != wantStep {
			t.Fatalf("step at %d = %d, want %d", i, step, wantStep)
		}
	}

	wantEnd := fixedTrendsNow.Truncate(time.Duration(wantStep) * time.Second).Unix()
	wantStart := wantEnd - int64(wantPoints-1)*wantStep
	if points[0].Bucket != wantStart || points[len(points)-1].Bucket != wantEnd {
		t.Fatalf("bounds = [%d, %d], want [%d, %d]", points[0].Bucket, points[len(points)-1].Bucket, wantStart, wantEnd)
	}
}

func assertAggregatePoint(t *testing.T, point trendPoint) {
	t.Helper()
	if point.Requests != 5 || point.Hits != 2 || point.Misses != 3 {
		t.Errorf("request dimensions = requests:%d hits:%d misses:%d, want 5/2/3", point.Requests, point.Hits, point.Misses)
	}
	if point.BytesHit != 300 || point.BytesMiss != 900 || point.BytesServed != 1200 {
		t.Errorf("byte dimensions = hit:%d miss:%d served:%d, want 300/900/1200", point.BytesHit, point.BytesMiss, point.BytesServed)
	}
	if point.SumLatencyMs != 400 || math.Abs(point.AvgLatencyMs-80) > 1e-9 {
		t.Errorf("latency = sum:%d avg:%f, want 400/80", point.SumLatencyMs, point.AvgLatencyMs)
	}
	if math.Abs(point.HitRate-0.4) > 1e-9 {
		t.Errorf("hit rate = %f, want 0.4", point.HitRate)
	}
	if point.Errors != 2 {
		t.Errorf("errors = %d, want 2", point.Errors)
	}
}

func trendPointAt(t *testing.T, points []trendPoint, bucket time.Time) trendPoint {
	t.Helper()
	want := bucket.Unix()
	for _, point := range points {
		if point.Bucket == want {
			return point
		}
	}
	t.Fatalf("bucket %d not found", want)
	return trendPoint{}
}

func insertRawAggregate(t *testing.T, database *gorm.DB, bucket time.Time) {
	t.Helper()
	rows := []db.AccessLog{
		{AdapterType: "pypi", Hit: true, BytesSent: 100, LatencyMs: 10, StatusCode: 200, CreatedAt: bucket.Add(time.Second)},
		{AdapterType: "npm", Hit: true, BytesSent: 200, LatencyMs: 30, StatusCode: 200, CreatedAt: bucket.Add(2 * time.Second)},
		{AdapterType: "pypi", Hit: false, BytesSent: 200, LatencyMs: 60, StatusCode: 500, CreatedAt: bucket.Add(3 * time.Second)},
		{AdapterType: "npm", Hit: false, BytesSent: 300, LatencyMs: 100, StatusCode: 503, CreatedAt: bucket.Add(4 * time.Second)},
		{AdapterType: "apt", Hit: false, BytesSent: 400, LatencyMs: 200, StatusCode: 404, CreatedAt: bucket.Add(5 * time.Second)},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("create raw aggregate: %v", err)
	}
}

func insertFiveMinuteAggregate(t *testing.T, database *gorm.DB, bucket time.Time) {
	t.Helper()
	rows := []db.AccessLogFiveMinutely{
		{BucketStart: bucket.Unix(), AdapterType: "pypi", Hit: true, Upstream: "cache", RequestCount: 2, TotalBytes: 300, SumLatencyMs: 40},
		{BucketStart: bucket.Unix(), AdapterType: "pypi", Hit: false, Upstream: "origin", RequestCount: 3, TotalBytes: 900, SumLatencyMs: 360, ErrorCount: 2},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("create five-minute aggregate: %v", err)
	}
}

func insertHourlyAggregate(t *testing.T, database *gorm.DB, bucket time.Time) {
	t.Helper()
	bucket = bucket.UTC()
	rows := []db.AccessLogHourly{
		{Date: bucket.Format("2006-01-02"), Hour: bucket.Hour(), AdapterType: "pypi", Hit: true, Upstream: "cache", RequestCount: 2, TotalBytes: 300, SumLatencyMs: 40},
		{Date: bucket.Format("2006-01-02"), Hour: bucket.Hour(), AdapterType: "pypi", Hit: false, Upstream: "origin", RequestCount: 3, TotalBytes: 900, SumLatencyMs: 360, ErrorCount: 2},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("create hourly aggregate: %v", err)
	}
}

func TestDashboardTrends_ReturnsExpectedResolutionForEveryRange(t *testing.T) {
	tests := []struct {
		rangeParam string
		wantPoints int
		wantStep   int64
	}{
		{"1h", 360, 10},
		{"24h", 288, 300},
		{"7d", 336, 1800},
		{"30d", 360, 7200},
	}
	for _, tt := range tests {
		t.Run(tt.rangeParam, func(t *testing.T) {
			_, router := newTrendsTestHandler(t)
			points := getTrendPoints(t, router, tt.rangeParam)
			assertTrendResolution(t, points, tt.wantPoints, tt.wantStep)
		})
	}
}

func TestDashboardTrends_AggregatesCurrentPartialBucket(t *testing.T) {
	tests := []struct {
		name       string
		rangeParam string
		insert     func(*testing.T, *gorm.DB)
	}{
		{
			name:       "one hour raw ten-second bucket",
			rangeParam: "1h",
			insert: func(t *testing.T, database *gorm.DB) {
				insertRawAggregate(t, database, fixedTrendsNow.Truncate(10*time.Second))
			},
		},
		{
			name:       "twenty-four hour five-minute bucket",
			rangeParam: "24h",
			insert: func(t *testing.T, database *gorm.DB) {
				insertFiveMinuteAggregate(t, database, fixedTrendsNow.Truncate(5*time.Minute))
			},
		},
		{
			name:       "seven day thirty-minute bucket",
			rangeParam: "7d",
			insert: func(t *testing.T, database *gorm.DB) {
				insertFiveMinuteAggregate(t, database, fixedTrendsNow.Truncate(5*time.Minute))
			},
		},
		{
			name:       "thirty day two-hour bucket",
			rangeParam: "30d",
			insert: func(t *testing.T, database *gorm.DB) {
				insertHourlyAggregate(t, database, fixedTrendsNow.Truncate(time.Hour))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, router := newTrendsTestHandler(t)
			tt.insert(t, handler.db)
			points := getTrendPoints(t, router, tt.rangeParam)
			assertAggregatePoint(t, points[len(points)-1])
		})
	}
}

func TestDashboardTrends_FallsBackWhenFineHistoryIsUnavailable(t *testing.T) {
	t.Run("24h keeps raw history authoritative when one fine row exists without marker", func(t *testing.T) {
		handler, router := newTrendsTestHandler(t)
		if err := handler.db.Exec("DELETE FROM control_plane_states WHERE key = ?", accesslog.FiveMinuteBackfillMarker).Error; err != nil {
			t.Fatalf("clear five-minute backfill marker: %v", err)
		}
		rawBucket := fixedTrendsNow.Truncate(5 * time.Minute).Add(-2 * time.Hour)
		fineBucket := fixedTrendsNow.Truncate(5 * time.Minute)
		insertRawAggregate(t, handler.db, rawBucket)
		if err := handler.db.Create(&db.AccessLogFiveMinutely{
			BucketStart:  fineBucket.Unix(),
			AdapterType:  "partial-fine-history",
			Upstream:     "test",
			RequestCount: 99,
		}).Error; err != nil {
			t.Fatalf("create partial fine history: %v", err)
		}

		points := getTrendPoints(t, router, "24h")
		assertTrendResolution(t, points, 288, 300)
		assertAggregatePoint(t, trendPointAt(t, points, rawBucket))
		if got := trendPointAt(t, points, fineBucket).Requests; got != 0 {
			t.Fatalf("fine-only bucket requests = %d, want 0", got)
		}
	})

	t.Run("7d keeps hourly history authoritative when one fine row exists without marker", func(t *testing.T) {
		handler, router := newTrendsTestHandler(t)
		if err := handler.db.Exec("DELETE FROM control_plane_states WHERE key = ?", accesslog.FiveMinuteBackfillMarker).Error; err != nil {
			t.Fatalf("clear five-minute backfill marker: %v", err)
		}
		hourlyBucket := fixedTrendsNow.Truncate(time.Hour).Add(-48 * time.Hour)
		fineBucket := fixedTrendsNow.Truncate(5 * time.Minute)
		insertHourlyAggregate(t, handler.db, hourlyBucket)
		if err := handler.db.Create(&db.AccessLogFiveMinutely{
			BucketStart:  fineBucket.Unix(),
			AdapterType:  "partial-fine-history",
			Upstream:     "test",
			RequestCount: 99,
		}).Error; err != nil {
			t.Fatalf("create partial fine history: %v", err)
		}

		points := getTrendPoints(t, router, "7d")
		assertTrendResolution(t, points, 168, 3600)
		assertAggregatePoint(t, trendPointAt(t, points, hourlyBucket))
		if got := trendPointAt(t, points, fixedTrendsNow.Truncate(time.Hour)).Requests; got != 0 {
			t.Fatalf("fine-only bucket requests = %d, want 0", got)
		}
	})
}

func TestDashboardTrends_RebuiltMarkerIncludesRawOnlyInterval(t *testing.T) {
	handler, router := newTrendsTestHandler(t)
	initialBucket := fixedTrendsNow.Truncate(30 * time.Minute).Add(-4 * time.Hour)
	rawOnlyBucket := fixedTrendsNow.Truncate(30 * time.Minute).Add(-2 * time.Hour)
	insertRawAggregate(t, handler.db, initialBucket)
	insertFiveMinuteAggregate(t, handler.db, initialBucket)

	if err := accesslog.InvalidateFiveMinuteBackfill(context.Background(), handler.db); err != nil {
		t.Fatalf("invalidate five-minute backfill: %v", err)
	}
	insertRawAggregate(t, handler.db, rawOnlyBucket)
	if err := accesslog.BackfillFiveMinutely(context.Background(), handler.db, fixedTrendsNow); err != nil {
		t.Fatalf("rebuild five-minute history: %v", err)
	}

	for _, rangeParam := range []string{"24h", "7d"} {
		t.Run(rangeParam, func(t *testing.T) {
			points := getTrendPoints(t, router, rangeParam)
			assertAggregatePoint(t, trendPointAt(t, points, rawOnlyBucket))
		})
	}
}

func TestDashboardTrends_RollupDisabledUsesRawForEveryRange(t *testing.T) {
	handler, router := newTrendsTestHandler(t)
	handler.useRollup = false
	insertRawAggregate(t, handler.db, fixedTrendsNow.Truncate(10*time.Second))
	if err := handler.db.Create(&db.AccessLogFiveMinutely{
		BucketStart:  fixedTrendsNow.Truncate(5 * time.Minute).Unix(),
		AdapterType:  "fine-noise",
		Upstream:     "test",
		RequestCount: 99,
	}).Error; err != nil {
		t.Fatalf("create five-minute noise: %v", err)
	}
	if err := handler.db.Create(&db.AccessLogHourly{
		Date:         fixedTrendsNow.Format("2006-01-02"),
		Hour:         fixedTrendsNow.Hour(),
		AdapterType:  "hourly-noise",
		Upstream:     "test",
		RequestCount: 101,
	}).Error; err != nil {
		t.Fatalf("create hourly noise: %v", err)
	}

	tests := []struct {
		rangeParam string
		wantPoints int
		wantStep   int64
	}{
		{"1h", 360, 10},
		{"24h", 288, 300},
		{"7d", 336, 1800},
		{"30d", 360, 7200},
	}
	for _, tt := range tests {
		t.Run(tt.rangeParam, func(t *testing.T) {
			points := getTrendPoints(t, router, tt.rangeParam)
			assertTrendResolution(t, points, tt.wantPoints, tt.wantStep)
			assertAggregatePoint(t, points[len(points)-1])
		})
	}
}

func TestDashboardTrends_InvalidRangeFallsBackToSevenDays(t *testing.T) {
	_, router := newTrendsTestHandler(t)
	want := getTrendPoints(t, router, "7d")
	got := getTrendPoints(t, router, "not-a-range")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid range response differs from 7d response")
	}
}

func TestDashboardTrends_HourlyGroupingUsesUTCAndCombinesPairs(t *testing.T) {
	t.Setenv("TZ", "Asia/Hong_Kong")
	handler, router := newTrendsTestHandler(t)
	firstHour := time.Date(2026, time.July, 12, 10, 0, 0, 0, time.UTC)
	insertFiveMinuteAggregate(t, handler.db, fixedTrendsNow.Truncate(5*time.Minute))
	insertHourlyAggregate(t, handler.db, firstHour)
	insertHourlyAggregate(t, handler.db, firstHour.Add(time.Hour))

	points := getTrendPoints(t, router, "30d")
	wantBucket := firstHour.Unix()
	for _, point := range points {
		if point.Bucket == wantBucket {
			if point.Requests != 10 || point.Hits != 4 || point.Misses != 6 || point.SumLatencyMs != 800 {
				t.Fatalf("paired UTC bucket = %+v, want doubled aggregate", point)
			}
			if point.Date != "2026-07-12 10:00" {
				t.Fatalf("UTC date label = %q, want 2026-07-12 10:00", point.Date)
			}
			return
		}
	}
	t.Fatalf("UTC bucket %d not found", wantBucket)
}

func TestDashboardTrends_RawQueryIsBoundedAndZeroFillsGaps(t *testing.T) {
	handler, router := newTrendsTestHandler(t)
	start := fixedTrendsNow.Truncate(10 * time.Second).Add(-359 * 10 * time.Second)
	rows := []db.AccessLog{
		{Hit: true, StatusCode: 200, CreatedAt: start.Add(-time.Second)},
		{Hit: true, StatusCode: 200, CreatedAt: start},
		{Hit: true, StatusCode: 200, CreatedAt: fixedTrendsNow.Add(-time.Second)},
		{Hit: true, StatusCode: 200, CreatedAt: fixedTrendsNow.Add(time.Second)},
	}
	if err := handler.db.Create(&rows).Error; err != nil {
		t.Fatalf("create bounded raw rows: %v", err)
	}

	points := getTrendPoints(t, router, "1h")
	assertTrendResolution(t, points, 360, 10)
	if points[0].Requests != 1 || points[len(points)-1].Requests != 1 {
		t.Fatalf("edge requests = first:%d last:%d, want 1/1", points[0].Requests, points[len(points)-1].Requests)
	}
	for i := 1; i < len(points)-1; i++ {
		if points[i].Requests != 0 {
			t.Fatalf("gap bucket %d requests = %d, want 0", i, points[i].Requests)
		}
	}
}

func TestDashboardTrends_ReturnsDatabaseErrorForSourceFailures(t *testing.T) {
	tests := []struct {
		name       string
		rangeParam string
		failTable  string
		occurrence int
		setup      func(*testing.T, *DashboardHandler)
	}{
		{
			name:       "raw aggregate",
			rangeParam: "1h",
			failTable:  "access_logs",
			occurrence: 1,
		},
		{
			name:       "fine aggregate",
			rangeParam: "24h",
			failTable:  "access_log_five_minutely",
			occurrence: 1,
			setup: func(t *testing.T, handler *DashboardHandler) {
				insertFiveMinuteAggregate(t, handler.db, fixedTrendsNow.Truncate(5*time.Minute))
			},
		},
		{
			name:       "hourly aggregate",
			rangeParam: "30d",
			failTable:  "access_log_hourly",
			occurrence: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, router := newTrendsTestHandler(t)
			if tt.setup != nil {
				tt.setup(t, handler)
			}
			seen := 0
			registerTrendRowCallback(t, handler.db, func(tx *gorm.DB) {
				if trendQueryTable(tx) != tt.failTable {
					return
				}
				seen++
				if seen == tt.occurrence {
					tx.AddError(errors.New("forced dashboard trends query failure"))
				}
			})

			recorder := performTrendRequest(router, tt.rangeParam, nil)
			assertTrendDatabaseError(t, recorder)
			if seen != tt.occurrence {
				t.Fatalf("matching queries = %d, want %d", seen, tt.occurrence)
			}
		})
	}
}

func TestDashboardTrends_MarkerQueryFailureReturnsDatabaseErrorWithoutFallback(t *testing.T) {
	handler, router := newTrendsTestHandler(t)
	markerQueries := 0
	rawQueries := 0
	registerTrendRowCallback(t, handler.db, func(tx *gorm.DB) {
		switch trendQueryTable(tx) {
		case "control_plane_states":
			markerQueries++
			tx.AddError(errors.New("forced five-minute marker query failure"))
		case "access_logs":
			rawQueries++
		}
	})

	recorder := performTrendRequest(router, "24h", nil)
	assertTrendDatabaseError(t, recorder)
	if markerQueries != 1 {
		t.Fatalf("marker queries = %d, want 1", markerQueries)
	}
	if rawQueries != 0 {
		t.Fatalf("raw fallback queries = %d, want 0 after marker query failure", rawQueries)
	}
}

func TestDashboardTrends_PropagatesRequestContextToEverySourceQuery(t *testing.T) {
	type contextKey struct{}
	key := contextKey{}
	const value = "dashboard-trends-request"

	tests := []struct {
		name        string
		rangeParam  string
		wantQueries int
		setup       func(*testing.T, *DashboardHandler)
	}{
		{name: "raw", rangeParam: "1h", wantQueries: 1},
		{
			name:        "marker readiness and fine aggregate",
			rangeParam:  "24h",
			wantQueries: 2,
			setup: func(t *testing.T, handler *DashboardHandler) {
				insertFiveMinuteAggregate(t, handler.db, fixedTrendsNow.Truncate(5*time.Minute))
			},
		},
		{
			name:        "marker readiness and hourly fallback",
			rangeParam:  "7d",
			wantQueries: 2,
			setup: func(t *testing.T, handler *DashboardHandler) {
				if err := handler.db.Exec("DELETE FROM control_plane_states WHERE key = ?", accesslog.FiveMinuteBackfillMarker).Error; err != nil {
					t.Fatalf("clear five-minute backfill marker: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, router := newTrendsTestHandler(t)
			if tt.setup != nil {
				tt.setup(t, handler)
			}
			queries := 0
			missingContext := false
			registerTrendRowCallback(t, handler.db, func(tx *gorm.DB) {
				table := trendQueryTable(tx)
				if table != "access_logs" && table != "access_log_five_minutely" &&
					table != "access_log_hourly" && table != "control_plane_states" {
					return
				}
				queries++
				if tx.Statement.Context.Value(key) != value {
					missingContext = true
				}
			})

			ctx := context.WithValue(context.Background(), key, value)
			recorder := performTrendRequest(router, tt.rangeParam, ctx)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if queries != tt.wantQueries {
				t.Fatalf("queries = %d, want %d", queries, tt.wantQueries)
			}
			if missingContext {
				t.Fatal("one or more trend queries did not receive the request context")
			}
		})
	}
}

func TestDashboardTrends_ReadsClockOncePerRequest(t *testing.T) {
	tests := []struct {
		name       string
		rangeParam string
		wantStep   time.Duration
		setup      func(*testing.T, *DashboardHandler)
	}{
		{
			name:       "primary fine source",
			rangeParam: "24h",
			wantStep:   5 * time.Minute,
			setup: func(t *testing.T, handler *DashboardHandler) {
				insertFiveMinuteAggregate(t, handler.db, fixedTrendsNow.Truncate(5*time.Minute))
			},
		},
		{
			name:       "hourly fallback",
			rangeParam: "7d",
			wantStep:   time.Hour,
			setup: func(t *testing.T, handler *DashboardHandler) {
				if err := handler.db.Exec("DELETE FROM control_plane_states WHERE key = ?", accesslog.FiveMinuteBackfillMarker).Error; err != nil {
					t.Fatalf("clear five-minute backfill marker: %v", err)
				}
			},
		},
		{
			name:       "rollup disabled",
			rangeParam: "30d",
			wantStep:   2 * time.Hour,
			setup: func(_ *testing.T, handler *DashboardHandler) {
				handler.useRollup = false
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, router := newTrendsTestHandler(t)
			tt.setup(t, handler)
			calls := 0
			handler.now = func() time.Time {
				calls++
				if calls == 1 {
					return fixedTrendsNow
				}
				return fixedTrendsNow.Add(24 * time.Hour)
			}

			points := getTrendPoints(t, router, tt.rangeParam)
			if calls != 1 {
				t.Fatalf("clock calls = %d, want 1", calls)
			}
			wantLast := fixedTrendsNow.Truncate(tt.wantStep).Unix()
			if got := points[len(points)-1].Bucket; got != wantLast {
				t.Fatalf("last bucket = %d, want %d", got, wantLast)
			}
		})
	}
}

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

	handler := NewDashboardHandler(database, map[string]*upstream.Pool{
		"pypi": pypiPool,
		"npm":  npmPool,
	}, []string{"pypi", "npm"}, false, 0)
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
