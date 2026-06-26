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
}

func NewDashboardHandler(database *gorm.DB, storage cache.Storage, pools map[string]*upstream.Pool, ecosystems []string, useRollup bool) *DashboardHandler {
	return &DashboardHandler{db: database, storage: storage, pools: pools, ecosystems: ecosystems, useRollup: useRollup}
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

	// Upstream status — batch-load IDs from DB
	var upstreamRecords []db.UpstreamRecord
	h.db.Find(&upstreamRecords)
	idByName := make(map[string]uint, len(upstreamRecords))
	for _, r := range upstreamRecords {
		idByName[r.Name] = r.ID
	}

	upstreams := make([]gin.H, 0)
	for _, name := range h.ecosystems {
		pool := h.pools[name]
		if pool == nil {
			continue
		}
		for _, u := range pool.Upstreams() {
			upstreams = append(upstreams, gin.H{
				"id":             idByName[u.Name],
				"name":           u.Name,
				"adapter":        name,
				"healthy":        u.Healthy,
				"avg_latency_ms": u.AvgLatency().Milliseconds(),
				"success_rate":   u.SuccessRate(),
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

// GetTrends powers the dashboard's hit/miss chart. Four ranges, each backed
// by the source that best fits its granularity:
//
//	1h   — 12 × 5-minute buckets, raw access_logs (sub-hour, can't use rollup)
//	24h  — 24 × 1-hour buckets, access_log_hourly (live, includes "now" hour)
//	7d   — 168 × 1-hour buckets, access_log_hourly (frontend re-buckets to days in browser TZ)
//	30d  — 720 × 1-hour buckets, same
//
// The 7d/30d paths return hourly granularity rather than collapsing GROUP BY
// date so the frontend can re-aggregate by the user's local-timezone day
// boundaries. The response carries every dimension a tab could want
// (hits / misses / bytes_hit / bytes_miss / sum_latency / errors) so tab
// switching on the frontend is instant — no refetch.
func (h *DashboardHandler) GetTrends(c *gin.Context) {
	rangeParam := c.DefaultQuery("range", "1h")

	var points []trendPoint

	switch rangeParam {
	case "1h":
		points = h.trends1h()
	case "24h":
		points = h.trendsHourly(24)
	case "30d":
		points = h.trendsHourly(24 * 30)
	default: // "7d"
		points = h.trendsHourly(24 * 7)
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

// trendsHourBucket is the union row both 1h-resolution (5-min) and
// hour-resolution paths Scan into. SQL projects every dimension the chart
// tabs can render so tab switching is a frontend-only operation.
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

// trends1h returns 12 five-minute buckets ending at the current minute,
// scanned from raw access_logs. The rollup tables key at hour granularity
// so they can't power this view; raw access_logs at ~5 minutes of data per
// query is fast even on busy servers.
func (h *DashboardHandler) trends1h() []trendPoint {
	const buckets = 12
	const intervalSec = 300
	now := time.Now().UTC()
	since := now.Add(-buckets * 5 * time.Minute)

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
		Where("created_at >= ?", since).
		Group("bucket").Order("bucket ASC").Scan(&rows)

	lookup := make(map[int64]trendHourBucket, len(rows))
	for _, r := range rows {
		lookup[r.Bucket] = r
	}
	startBucket := (since.Unix() / int64(intervalSec)) * int64(intervalSec)
	out := make([]trendPoint, 0, buckets)
	for i := 0; i < buckets; i++ {
		ts := startBucket + int64(i*intervalSec)
		bucket := time.Unix(ts, 0).UTC()
		row := lookup[ts] // zero-value is fine for empty bucket
		out = append(out, row.toPoint(bucket))
	}
	return out
}

// trendsHourly returns N consecutive one-hour buckets ending at the current
// hour. Same shape regardless of whether N is 24, 168 (7d), or 720 (30d) —
// the frontend re-aggregates 7d/30d into local-timezone days for display.
//
// Rollup-mode reads from access_log_hourly (live; updated by the recorder
// every flush); raw mode falls back to a strftime-bucketed access_logs
// scan, which only matters for operators who disabled rollup_enabled.
func (h *DashboardHandler) trendsHourly(buckets int) []trendPoint {
	now := time.Now().UTC()
	since := now.Truncate(time.Hour).Add(-time.Duration(buckets-1) * time.Hour)

	type rawRow struct {
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
	var rows []rawRow

	if h.useRollup {
		startDate := since.Format("2006-01-02")
		h.db.Table("access_log_hourly").
			Select(`date, hour,
				COALESCE(SUM(request_count), 0) AS requests,
				COALESCE(SUM(CASE WHEN hit = 1 THEN request_count ELSE 0 END), 0) AS hits,
				COALESCE(SUM(CASE WHEN hit = 0 THEN request_count ELSE 0 END), 0) AS misses,
				COALESCE(SUM(CASE WHEN hit = 1 THEN total_bytes ELSE 0 END), 0) AS bytes_hit,
				COALESCE(SUM(CASE WHEN hit = 0 THEN total_bytes ELSE 0 END), 0) AS bytes_miss,
				COALESCE(SUM(sum_latency_ms), 0) AS sum_latency_ms,
				COALESCE(SUM(error_count), 0) AS errors`).
			Where("date >= ?", startDate).
			Group("date, hour").Order("date ASC, hour ASC").Scan(&rows)
	} else {
		h.db.Model(&db.AccessLog{}).
			Select(`strftime('%Y-%m-%d', created_at) AS date,
				CAST(strftime('%H', created_at) AS INTEGER) AS hour,
				COUNT(*) AS requests,
				COALESCE(SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END), 0) AS hits,
				COALESCE(SUM(CASE WHEN hit = 0 THEN 1 ELSE 0 END), 0) AS misses,
				COALESCE(SUM(CASE WHEN hit = 1 THEN bytes_sent ELSE 0 END), 0) AS bytes_hit,
				COALESCE(SUM(CASE WHEN hit = 0 THEN bytes_sent ELSE 0 END), 0) AS bytes_miss,
				COALESCE(SUM(latency_ms), 0) AS sum_latency_ms,
				COALESCE(SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END), 0) AS errors`).
			Where("created_at >= ?", since).
			Group("date, hour").Order("date ASC, hour ASC").Scan(&rows)
	}

	type key struct {
		date string
		hour int
	}
	lookup := make(map[key]trendHourBucket, len(rows))
	for _, r := range rows {
		lookup[key{r.Date, r.Hour}] = trendHourBucket{
			Requests:     r.Requests,
			Hits:         r.Hits,
			Misses:       r.Misses,
			BytesHit:     r.BytesHit,
			BytesMiss:    r.BytesMiss,
			SumLatencyMs: r.SumLatencyMs,
			Errors:       r.Errors,
		}
	}

	out := make([]trendPoint, 0, buckets)
	for i := 0; i < buckets; i++ {
		bucket := since.Add(time.Duration(i) * time.Hour)
		row := lookup[key{bucket.Format("2006-01-02"), bucket.Hour()}]
		out = append(out, row.toPoint(bucket))
	}
	return out
}
