package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/accesslog"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

type DashboardHandler struct {
	db         *gorm.DB
	pools      map[string]*upstream.Pool
	ecosystems []string
	useRollup  bool
	maxSizeGB  int
	now        func() time.Time
}

func NewDashboardHandler(database *gorm.DB, pools map[string]*upstream.Pool, ecosystems []string, useRollup bool, maxSizeGB int) *DashboardHandler {
	return &DashboardHandler{
		db:         database,
		pools:      pools,
		ecosystems: ecosystems,
		useRollup:  useRollup,
		maxSizeGB:  maxSizeGB,
		now:        time.Now,
	}
}

func (h *DashboardHandler) GetDashboard(c *gin.Context) {
	now := time.Now().UTC()

	// Rolling windows replace the calendar-day "today" concept. The headline
	// cards report the last 24 hours; the change indicators below them
	// compare against the preceding 24 hours (i.e. now-48h..now-24h).
	last24h := h.aggWindow(now.Add(-24*time.Hour), now)
	prev24h := h.aggWindow(now.Add(-48*time.Hour), now.Add(-24*time.Hour))

	totalRequests := last24h.Total
	hitCount := last24h.Hits
	bytesSent := last24h.Bytes
	var avgLatency float64
	if last24h.Total > 0 {
		avgLatency = float64(last24h.SumLatency) / float64(last24h.Total)
	}
	var hitRate float64
	if totalRequests > 0 {
		hitRate = float64(hitCount) / float64(totalRequests)
	}
	var prevAvgLatency, prevHitRate float64
	if prev24h.Total > 0 {
		prevAvgLatency = float64(prev24h.SumLatency) / float64(prev24h.Total)
		prevHitRate = float64(prev24h.Hits) / float64(prev24h.Total)
	}

	// Daily stats for the last 7 days
	type dailyStat struct {
		Date        string `json:"date"`
		AdapterType string `json:"adapter_type"`
		Count       int64  `json:"count"`
	}
	var dailyStats []dailyStat
	if h.useRollup {
		// Read from access_log_hourly (not access_log_daily) so today's
		// partial-hour data is included — daily lags until the nightly
		// compactor runs and would render today as 0.
		sevenDaysAgoDate := now.AddDate(0, 0, -7).Format("2006-01-02")
		h.db.Table("access_log_hourly").
			Select("date, adapter_type, COALESCE(SUM(request_count),0) AS count").
			Where("date >= ?", sevenDaysAgoDate).
			Group("date, adapter_type").Order("date").
			Scan(&dailyStats)
	} else {
		sevenDaysAgo := now.AddDate(0, 0, -7)
		h.db.Model(&db.AccessLog{}).
			Select("DATE(created_at) as date, adapter_type, COUNT(*) as count").
			Where("datetime(created_at) >= datetime(?)", sevenDaysAgo).
			Group("date, adapter_type").Order("date").
			Scan(&dailyStats)
	}

	upstreams := make([]gin.H, 0)
	for _, name := range h.ecosystems {
		pool := h.pools[name]
		if pool == nil {
			continue
		}
		for _, u := range pool.Snapshot() {
			health := u.HealthSnapshot()
			upstreams = append(upstreams, gin.H{
				"id":             u.ID,
				"name":           u.Name,
				"adapter":        name,
				"healthy":        health.Healthy,
				"avg_latency_ms": health.AvgLatency.Milliseconds(),
				"success_rate":   health.SuccessRate,
			})
		}
	}

	// Top packages
	type topPkg struct {
		PackageName string `json:"name"`
		HitCount    int64  `json:"hit_count"`
	}
	type topPkgRow struct {
		AdapterType string
		PackageName string
		HitCount    int64
	}
	var topRows []topPkgRow
	if h.useRollup {
		h.db.Table("access_log_package_daily").
			Select("adapter_type, package_name, COALESCE(SUM(request_count),0) AS hit_count").
			Where("package_name != ''").
			Group("adapter_type, package_name").
			Order("hit_count DESC, adapter_type ASC, package_name ASC").
			Limit(10).
			Scan(&topRows)
	} else {
		h.db.Model(&db.AccessLog{}).
			Select("adapter_type, package_name, COUNT(*) AS hit_count").
			Where("package_name != ''").
			Group("adapter_type, package_name").
			Order("hit_count DESC, adapter_type ASC, package_name ASC").
			Limit(10).
			Scan(&topRows)
	}
	topPackages := make(map[string][]topPkg)
	for _, row := range topRows {
		topPackages[row.AdapterType] = append(topPackages[row.AdapterType], topPkg{
			PackageName: row.PackageName,
			HitCount:    row.HitCount,
		})
	}

	response := gin.H{
		"last_24h": gin.H{
			"total_requests": totalRequests,
			"hit_count":      hitCount,
			"hit_rate":       hitRate,
			"bytes_served":   bytesSent,
			"avg_latency_ms": avgLatency,
		},
		"prev_24h": gin.H{
			"total_requests": prev24h.Total,
			"hit_rate":       prevHitRate,
			"bytes_served":   prev24h.Bytes,
			"avg_latency_ms": prevAvgLatency,
		},
		"daily_stats":  dailyStats,
		"upstreams":    upstreams,
		"top_packages": topPackages,
	}
	if h.maxSizeGB > 0 {
		var totalSize int64
		result := h.db.Model(&db.CacheEntry{}).
			Select("COALESCE(SUM(size), 0)").
			Scan(&totalSize)
		if result.Error != nil {
			zap.L().Warn("load dashboard cache usage", zap.Error(result.Error))
		} else {
			const bytesPerGiB = 1024 * 1024 * 1024
			response["cache_usage_percent"] = float64(totalSize) / (float64(h.maxSizeGB) * bytesPerGiB) * 100
		}
	}

	c.JSON(http.StatusOK, response)
}

// aggSnapshot is the cross-cutting count/SUM tuple every rolling-window
// query inside GetDashboard wants. Defined as a struct (not gin.H) so the
// SQL Scan can land in fixed fields without reflection.
type aggSnapshot struct {
	Total      int64
	Hits       int64
	Bytes      int64
	SumLatency int64
}

// aggWindow runs one SUM over access_logs for [from, to). Raw rather than
// rollup because rolling windows aren't hour-aligned — access_log_hourly's
// granularity would force snap-to-hour which silently dropped the partial
// most-recent hour. 24h of raw rows scans cheaply (~thousands of rows on a
// busy server) and the same query also covers the prev-24h comparison.
func (h *DashboardHandler) aggWindow(from, to time.Time) aggSnapshot {
	var out aggSnapshot
	h.db.Model(&db.AccessLog{}).
		Select(`COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END), 0) AS hits,
			COALESCE(SUM(bytes_sent), 0) AS bytes,
			COALESCE(SUM(latency_ms), 0) AS sum_latency`).
		Where("created_at >= ? AND created_at < ?", from, to).
		Scan(&out)
	return out
}

type trendSpec struct {
	buckets  int
	interval time.Duration
}

var trendSpecs = map[string]trendSpec{
	"1h":  {buckets: 360, interval: 10 * time.Second},
	"24h": {buckets: 288, interval: 5 * time.Minute},
	"7d":  {buckets: 336, interval: 30 * time.Minute},
	"30d": {buckets: 360, interval: 2 * time.Hour},
}

var sevenDayHourlyFallbackSpec = trendSpec{buckets: 168, interval: time.Hour}

// GetTrends returns a fixed-size UTC-aligned series. The current unfinished
// bucket is always the final point, and absent buckets are represented by
// zero-valued points.
func (h *DashboardHandler) GetTrends(c *gin.Context) {
	rangeParam := c.DefaultQuery("range", "1h")
	spec, ok := trendSpecs[rangeParam]
	if !ok {
		rangeParam = "7d"
		spec = trendSpecs[rangeParam]
	}

	ctx := c.Request.Context()
	now := h.trendNow()
	var (
		points []trendPoint
		err    error
	)
	if !h.useRollup {
		points, err = h.trendsRaw(ctx, spec, now)
	} else {
		switch rangeParam {
		case "1h":
			points, err = h.trendsRaw(ctx, spec, now)
		case "24h":
			points, err = h.trendsFiveMinutely(ctx, spec, now)
		case "30d":
			points, err = h.trendsHourlyGrouped(ctx, spec, now)
		default: // "7d"
			points, err = h.trendsSevenDays(ctx, spec, now)
		}
	}
	if err != nil {
		zap.L().Error("load dashboard trends", zap.String("range", rangeParam), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "DB_ERROR",
			"message": "failed to load dashboard trends",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"points": points})
}

// trendPoint is one row in the trends response. Carries every dimension the
// frontend tabs need (requests / bandwidth / latency / errors) so a tab
// switch is a pure render — no refetch. Bucket is unix seconds at the
// bucket's UTC start; the frontend formats it in the browser's timezone.
type trendPoint struct {
	Bucket       int64   `json:"bucket"`
	Date         string  `json:"date"` // legacy display label (UTC); prefer formatting Bucket client-side
	Requests     int64   `json:"requests"`
	Hits         int64   `json:"hits"`
	Misses       int64   `json:"misses"`
	HitRate      float64 `json:"hit_rate"`
	BytesServed  int64   `json:"bytes_served"`
	BytesHit     int64   `json:"bytes_hit"`
	BytesMiss    int64   `json:"bytes_miss"`
	SumLatencyMs int64   `json:"sum_latency_ms"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	Errors       int64   `json:"errors"`
}

// trendHourBucket is the common aggregate row all three sources produce.
type trendHourBucket struct {
	Bucket       int64
	Requests     int64
	Hits         int64
	Misses       int64
	BytesHit     int64
	BytesMiss    int64
	SumLatencyMs int64
	Errors       int64
}

func (b *trendHourBucket) add(other trendHourBucket) {
	b.Requests += other.Requests
	b.Hits += other.Hits
	b.Misses += other.Misses
	b.BytesHit += other.BytesHit
	b.BytesMiss += other.BytesMiss
	b.SumLatencyMs += other.SumLatencyMs
	b.Errors += other.Errors
}

func (b trendHourBucket) toPoint(bucketStart time.Time) trendPoint {
	p := trendPoint{
		Bucket:       bucketStart.Unix(),
		Date:         bucketStart.Format("2006-01-02 15:04"),
		Requests:     b.Requests,
		Hits:         b.Hits,
		Misses:       b.Misses,
		BytesHit:     b.BytesHit,
		BytesMiss:    b.BytesMiss,
		BytesServed:  b.BytesHit + b.BytesMiss,
		SumLatencyMs: b.SumLatencyMs,
		Errors:       b.Errors,
	}
	if b.Requests > 0 {
		p.HitRate = float64(b.Hits) / float64(b.Requests)
		p.AvgLatencyMs = float64(b.SumLatencyMs) / float64(b.Requests)
	}
	return p
}

type trendWindow struct {
	now   time.Time
	start time.Time
	end   time.Time
	spec  trendSpec
}

func makeTrendWindow(now time.Time, spec trendSpec) trendWindow {
	now = now.UTC()
	end := now.Truncate(spec.interval)
	return trendWindow{
		now:   now,
		start: end.Add(-time.Duration(spec.buckets-1) * spec.interval),
		end:   end,
		spec:  spec,
	}
}

func (h *DashboardHandler) trendNow() time.Time {
	if h.now == nil {
		return time.Now().UTC()
	}
	return h.now().UTC()
}

func buildTrendPoints(window trendWindow, rows []trendHourBucket) []trendPoint {
	lookup := make(map[int64]trendHourBucket, len(rows))
	for _, row := range rows {
		lookup[row.Bucket] = row
	}

	out := make([]trendPoint, 0, window.spec.buckets)
	for i := 0; i < window.spec.buckets; i++ {
		bucket := window.start.Add(time.Duration(i) * window.spec.interval)
		out = append(out, lookup[bucket.Unix()].toPoint(bucket))
	}
	return out
}

func (h *DashboardHandler) trendsRaw(ctx context.Context, spec trendSpec, now time.Time) ([]trendPoint, error) {
	return h.trendsRawWindow(ctx, makeTrendWindow(now, spec))
}

func (h *DashboardHandler) trendsRawWindow(ctx context.Context, window trendWindow) ([]trendPoint, error) {
	intervalSec := int64(window.spec.interval / time.Second)
	var rows []trendHourBucket
	result := h.db.WithContext(ctx).Model(&db.AccessLog{}).
		Select(`(CAST(strftime('%s', created_at) AS INTEGER) / ?) * ? AS bucket,
			COUNT(*) AS requests,
			COALESCE(SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END), 0) AS hits,
			COALESCE(SUM(CASE WHEN hit = 0 THEN 1 ELSE 0 END), 0) AS misses,
			COALESCE(SUM(CASE WHEN hit = 1 THEN bytes_sent ELSE 0 END), 0) AS bytes_hit,
			COALESCE(SUM(CASE WHEN hit = 0 THEN bytes_sent ELSE 0 END), 0) AS bytes_miss,
			COALESCE(SUM(latency_ms), 0) AS sum_latency_ms,
			COALESCE(SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END), 0) AS errors`,
			intervalSec, intervalSec).
		Where("created_at >= ? AND created_at <= ?", window.start, window.now).
		Group("bucket").Order("bucket ASC").Scan(&rows)
	if result.Error != nil {
		return nil, result.Error
	}
	return buildTrendPoints(window, rows), nil
}

func (h *DashboardHandler) trendsFiveMinutely(ctx context.Context, spec trendSpec, now time.Time) ([]trendPoint, error) {
	window := makeTrendWindow(now, spec)
	hasHistory, err := h.hasFiveMinuteHistory(ctx)
	if err != nil {
		return nil, err
	}
	if !hasHistory {
		return h.trendsRawWindow(ctx, window)
	}
	return h.trendsFiveMinutelyWindow(ctx, window)
}

func (h *DashboardHandler) trendsFiveMinutelyWindow(ctx context.Context, window trendWindow) ([]trendPoint, error) {
	intervalSec := int64(window.spec.interval / time.Second)
	var rows []trendHourBucket
	result := h.db.WithContext(ctx).Table("access_log_five_minutely").
		Select(`(bucket_start / ?) * ? AS bucket,
			COALESCE(SUM(request_count), 0) AS requests,
			COALESCE(SUM(CASE WHEN hit = 1 THEN request_count ELSE 0 END), 0) AS hits,
			COALESCE(SUM(CASE WHEN hit = 0 THEN request_count ELSE 0 END), 0) AS misses,
			COALESCE(SUM(CASE WHEN hit = 1 THEN total_bytes ELSE 0 END), 0) AS bytes_hit,
			COALESCE(SUM(CASE WHEN hit = 0 THEN total_bytes ELSE 0 END), 0) AS bytes_miss,
			COALESCE(SUM(sum_latency_ms), 0) AS sum_latency_ms,
			COALESCE(SUM(error_count), 0) AS errors`,
			intervalSec, intervalSec).
		Where("bucket_start >= ? AND bucket_start <= ?", window.start.Unix(), window.now.Unix()).
		Group("bucket").Order("bucket ASC").Scan(&rows)
	if result.Error != nil {
		return nil, result.Error
	}
	return buildTrendPoints(window, rows), nil
}

func (h *DashboardHandler) hasFiveMinuteHistory(ctx context.Context) (bool, error) {
	var exists int
	result := h.db.WithContext(ctx).Table("control_plane_states").
		Select("1").
		Where("key = ?", accesslog.FiveMinuteBackfillMarker).
		Limit(1).
		Scan(&exists)
	if result.Error != nil {
		return false, result.Error
	}
	return exists == 1, nil
}

func (h *DashboardHandler) trendsSevenDays(ctx context.Context, spec trendSpec, now time.Time) ([]trendPoint, error) {
	window := makeTrendWindow(now, spec)
	hasHistory, err := h.hasFiveMinuteHistory(ctx)
	if err != nil {
		return nil, err
	}
	if hasHistory {
		return h.trendsFiveMinutelyWindow(ctx, window)
	}
	return h.trendsHourlyGroupedWindow(ctx, makeTrendWindow(now, sevenDayHourlyFallbackSpec))
}

func (h *DashboardHandler) trendsHourlyGrouped(ctx context.Context, spec trendSpec, now time.Time) ([]trendPoint, error) {
	return h.trendsHourlyGroupedWindow(ctx, makeTrendWindow(now, spec))
}

func (h *DashboardHandler) trendsHourlyGroupedWindow(ctx context.Context, window trendWindow) ([]trendPoint, error) {
	type hourlyRow struct {
		Date         string
		Hour         int
		Requests     int64
		Hits         int64
		Misses       int64
		BytesHit     int64
		BytesMiss    int64
		SumLatencyMs int64
		Errors       int64
	}

	var rows []hourlyRow
	result := h.db.WithContext(ctx).Table("access_log_hourly").
		Select(`date, hour,
			COALESCE(SUM(request_count), 0) AS requests,
			COALESCE(SUM(CASE WHEN hit = 1 THEN request_count ELSE 0 END), 0) AS hits,
			COALESCE(SUM(CASE WHEN hit = 0 THEN request_count ELSE 0 END), 0) AS misses,
			COALESCE(SUM(CASE WHEN hit = 1 THEN total_bytes ELSE 0 END), 0) AS bytes_hit,
			COALESCE(SUM(CASE WHEN hit = 0 THEN total_bytes ELSE 0 END), 0) AS bytes_miss,
			COALESCE(SUM(sum_latency_ms), 0) AS sum_latency_ms,
			COALESCE(SUM(error_count), 0) AS errors`).
		Where(`(date > ? OR (date = ? AND hour >= ?))
			AND (date < ? OR (date = ? AND hour <= ?))`,
			window.start.Format("2006-01-02"), window.start.Format("2006-01-02"), window.start.Hour(),
			window.now.Format("2006-01-02"), window.now.Format("2006-01-02"), window.now.Hour()).
		Group("date, hour").Order("date ASC, hour ASC").Scan(&rows)
	if result.Error != nil {
		return nil, result.Error
	}

	intervalSec := int64(window.spec.interval / time.Second)
	grouped := make(map[int64]trendHourBucket, len(rows))
	for _, row := range rows {
		date, err := time.Parse("2006-01-02", row.Date)
		if err != nil {
			continue
		}
		hourStart := time.Date(date.Year(), date.Month(), date.Day(), row.Hour, 0, 0, 0, time.UTC)
		bucket := (hourStart.Unix() / intervalSec) * intervalSec
		aggregate := grouped[bucket]
		aggregate.Bucket = bucket
		aggregate.add(trendHourBucket{
			Requests:     row.Requests,
			Hits:         row.Hits,
			Misses:       row.Misses,
			BytesHit:     row.BytesHit,
			BytesMiss:    row.BytesMiss,
			SumLatencyMs: row.SumLatencyMs,
			Errors:       row.Errors,
		})
		grouped[bucket] = aggregate
	}

	aggregates := make([]trendHourBucket, 0, len(grouped))
	for _, aggregate := range grouped {
		aggregates = append(aggregates, aggregate)
	}
	return buildTrendPoints(window, aggregates), nil
}
