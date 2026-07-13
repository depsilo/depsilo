package admin

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

type DashboardHandler struct {
	db         *gorm.DB
	storage    cache.Storage
	pools      map[string]*upstream.Pool
	ecosystems []string
	useRollup  bool
	now        func() time.Time
}

func NewDashboardHandler(database *gorm.DB, storage cache.Storage, pools map[string]*upstream.Pool, ecosystems []string, useRollup bool) *DashboardHandler {
	return &DashboardHandler{
		db:         database,
		storage:    storage,
		pools:      pools,
		ecosystems: ecosystems,
		useRollup:  useRollup,
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
	var pypiTop, aptTop []topPkg
	if h.useRollup {
		h.db.Table("access_log_package_daily").
			Select("package_name, COALESCE(SUM(request_count),0) AS hit_count").
			Where("adapter_type = ? AND package_name != ''", "pypi").
			Group("package_name").Order("hit_count DESC").Limit(10).Scan(&pypiTop)
		h.db.Table("access_log_package_daily").
			Select("package_name, COALESCE(SUM(request_count),0) AS hit_count").
			Where("adapter_type = ? AND package_name != ''", "apt").
			Group("package_name").Order("hit_count DESC").Limit(10).Scan(&aptTop)
	} else {
		h.db.Model(&db.AccessLog{}).
			Select("package_name, COUNT(*) as hit_count").
			Where("adapter_type = ? AND package_name != ''", "pypi").
			Group("package_name").Order("hit_count DESC").Limit(10).Scan(&pypiTop)
		h.db.Model(&db.AccessLog{}).
			Select("package_name, COUNT(*) as hit_count").
			Where("adapter_type = ? AND package_name != ''", "apt").
			Group("package_name").Order("hit_count DESC").Limit(10).Scan(&aptTop)
	}

	c.JSON(http.StatusOK, gin.H{
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
		"daily_stats": dailyStats,
		"upstreams":   upstreams,
		"top_packages": gin.H{
			"pypi": pypiTop,
			"apt":  aptTop,
		},
	})
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

	var points []trendPoint
	if !h.useRollup {
		points = h.trendsRaw(spec)
	} else {
		switch rangeParam {
		case "1h":
			points = h.trendsRaw(spec)
		case "24h":
			points = h.trendsFiveMinutely(spec)
		case "30d":
			points = h.trendsHourlyGrouped(spec)
		default: // "7d"
			points = h.trendsSevenDays(spec)
		}
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

func (h *DashboardHandler) trendsRaw(spec trendSpec) []trendPoint {
	return h.trendsRawWindow(makeTrendWindow(h.trendNow(), spec))
}

func (h *DashboardHandler) trendsRawWindow(window trendWindow) []trendPoint {
	intervalSec := int64(window.spec.interval / time.Second)
	var rows []trendHourBucket
	h.db.Model(&db.AccessLog{}).
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
	return buildTrendPoints(window, rows)
}

func (h *DashboardHandler) trendsFiveMinutely(spec trendSpec) []trendPoint {
	window := makeTrendWindow(h.trendNow(), spec)
	if !h.hasFiveMinuteHistory(window) {
		return h.trendsRawWindow(window)
	}
	return h.trendsFiveMinutelyWindow(window)
}

func (h *DashboardHandler) trendsFiveMinutelyWindow(window trendWindow) []trendPoint {
	intervalSec := int64(window.spec.interval / time.Second)
	var rows []trendHourBucket
	h.db.Table("access_log_five_minutely").
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
	return buildTrendPoints(window, rows)
}

func (h *DashboardHandler) hasFiveMinuteHistory(window trendWindow) bool {
	var exists int
	err := h.db.Table("access_log_five_minutely").
		Select("1").
		Where("bucket_start >= ? AND bucket_start <= ?", window.start.Unix(), window.now.Unix()).
		Limit(1).
		Scan(&exists).Error
	return err == nil && exists == 1
}

func (h *DashboardHandler) trendsSevenDays(spec trendSpec) []trendPoint {
	now := h.trendNow()
	window := makeTrendWindow(now, spec)
	if h.hasFiveMinuteHistory(window) {
		return h.trendsFiveMinutelyWindow(window)
	}
	return h.trendsHourlyGroupedWindow(makeTrendWindow(now, sevenDayHourlyFallbackSpec))
}

func (h *DashboardHandler) trendsHourlyGrouped(spec trendSpec) []trendPoint {
	return h.trendsHourlyGroupedWindow(makeTrendWindow(h.trendNow(), spec))
}

func (h *DashboardHandler) trendsHourlyGroupedWindow(window trendWindow) []trendPoint {
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
	h.db.Table("access_log_hourly").
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
	return buildTrendPoints(window, aggregates)
}
