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
	todayDate := now.Format("2006-01-02")
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	var totalRequests, hitCount int64
	var bytesSent int64
	var avgLatency float64

	if h.useRollup {
		// access_log_hourly's PK is (date, hour, adapter_type, hit, upstream).
		// "today" = all rows with date == today_utc; aggregating gives the
		// same numbers as the raw COUNT/SUM but without scanning 100k rows.
		var totals struct {
			Total      int64
			Hits       int64
			Bytes      int64
			SumLatency int64
		}
		h.db.Table("access_log_hourly").
			Select("COALESCE(SUM(request_count),0) AS total, COALESCE(SUM(CASE WHEN hit=1 THEN request_count ELSE 0 END),0) AS hits, COALESCE(SUM(total_bytes),0) AS bytes, COALESCE(SUM(sum_latency_ms),0) AS sum_latency").
			Where("date = ?", todayDate).
			Scan(&totals)
		totalRequests = totals.Total
		hitCount = totals.Hits
		bytesSent = totals.Bytes
		if totals.Total > 0 {
			avgLatency = float64(totals.SumLatency) / float64(totals.Total)
		}
	} else {
		h.db.Model(&db.AccessLog{}).Where("datetime(created_at) >= datetime(?)", todayStart).Count(&totalRequests)
		h.db.Model(&db.AccessLog{}).Where("datetime(created_at) >= datetime(?) AND hit = ?", todayStart, true).Count(&hitCount)
		h.db.Model(&db.AccessLog{}).Where("datetime(created_at) >= datetime(?)", todayStart).
			Select("COALESCE(SUM(bytes_sent), 0)").Scan(&bytesSent)
		h.db.Model(&db.AccessLog{}).Where("datetime(created_at) >= datetime(?)", todayStart).
			Select("COALESCE(AVG(latency_ms), 0)").Scan(&avgLatency)
	}

	var hitRate float64
	if totalRequests > 0 {
		hitRate = float64(hitCount) / float64(totalRequests)
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
		"today": gin.H{
			"total_requests": totalRequests,
			"hit_count":      hitCount,
			"hit_rate":       hitRate,
			"bytes_served":   bytesSent,
			"avg_latency_ms": avgLatency,
		},
		"daily_stats": dailyStats,
		"upstreams":   upstreams,
		"top_packages": gin.H{
			"pypi": pypiTop,
			"apt":  aptTop,
		},
	})
}

// GetTrends powers the dashboard's hit/miss chart. Four ranges, each backed
// by the source that best fits its granularity:
//
//	1h   — 12 × 5-minute buckets, raw access_logs (sub-hour, can't use rollup)
//	24h  — 24 × 1-hour buckets, access_log_hourly (live, includes "now" hour)
//	7d   — 7  × 1-day buckets,  access_log_hourly GROUP BY date
//	30d  — 30 × 1-day buckets,  same
//
// The 7d/30d paths intentionally read access_log_hourly rather than
// access_log_daily so today shows up in the chart — access_log_daily is
// populated by the nightly compactor and would be empty for today's date.
// Hourly has up to 24 rows per day × 13 adapters × 2 hits, but the GROUP BY
// collapses to one row per date so the chart payload size is identical.
func (h *DashboardHandler) GetTrends(c *gin.Context) {
	rangeParam := c.DefaultQuery("range", "1h")

	var points []trendPoint

	switch rangeParam {
	case "1h":
		points = h.trends1h()
	case "24h":
		points = h.trends24h()
	case "30d":
		points = h.trendsByDay(30)
	default: // "7d"
		points = h.trendsByDay(7)
	}

	c.JSON(http.StatusOK, gin.H{"points": points})
}

type trendPoint struct {
	Bucket      int64   `json:"bucket"`
	Date        string  `json:"date"`
	Requests    int64   `json:"requests"`
	Hits        int64   `json:"hits"`
	Misses      int64   `json:"misses"`
	HitRate     float64 `json:"hit_rate"`
	BytesServed int64   `json:"bytes_served"`
}

// trends1h returns 12 five-minute buckets ending at the current minute,
// scanned from raw access_logs. The rollup tables key at hour granularity
// so they can't power this view; raw access_logs at ~5 minutes of data per
// query is fast even on busy servers.
func (h *DashboardHandler) trends1h() []trendPoint {
	const buckets = 12
	const intervalSec = 300 // 5 min

	type bucketRow struct {
		Bucket      int64
		Requests    int64
		Hits        int64
		Misses      int64
		BytesServed int64
	}
	now := time.Now().UTC()
	since := now.Add(-buckets * 5 * time.Minute)

	var rows []bucketRow
	h.db.Model(&db.AccessLog{}).
		Select(`(CAST(strftime('%s', created_at) AS INTEGER) / ?) * ? AS bucket,
			COUNT(*) AS requests,
			COALESCE(SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END), 0) AS hits,
			COALESCE(SUM(CASE WHEN hit = 0 THEN 1 ELSE 0 END), 0) AS misses,
			COALESCE(SUM(bytes_sent), 0) AS bytes_served`,
			intervalSec, intervalSec).
		Where("created_at >= ?", since).
		Group("bucket").
		Order("bucket ASC").
		Scan(&rows)

	lookup := make(map[int64]bucketRow, len(rows))
	for _, r := range rows {
		lookup[r.Bucket] = r
	}
	startBucket := (since.Unix() / int64(intervalSec)) * int64(intervalSec)
	out := make([]trendPoint, 0, buckets)
	for i := 0; i < buckets; i++ {
		ts := startBucket + int64(i*intervalSec)
		p := trendPoint{Bucket: ts, Date: time.Unix(ts, 0).UTC().Format("2006-01-02 15:04")}
		if r, ok := lookup[ts]; ok {
			p.Requests = r.Requests
			p.Hits = r.Hits
			p.Misses = r.Misses
			p.BytesServed = r.BytesServed
			if p.Requests > 0 {
				p.HitRate = float64(p.Hits) / float64(p.Requests)
			}
		}
		out = append(out, p)
	}
	return out
}

// trends24h returns 24 one-hour buckets ending at the current hour, scanned
// from access_log_hourly when rollup is enabled (with a raw fallback).
func (h *DashboardHandler) trends24h() []trendPoint {
	const buckets = 24
	now := time.Now().UTC()
	since := now.Truncate(time.Hour).Add(-(buckets - 1) * time.Hour)

	type bucketRow struct {
		Date        string
		Hour        int
		Requests    int64
		Hits        int64
		Misses      int64
		BytesServed int64
	}
	var rows []bucketRow

	if h.useRollup {
		startDate := since.Format("2006-01-02")
		h.db.Table("access_log_hourly").
			Select(`date, hour,
				COALESCE(SUM(request_count), 0) AS requests,
				COALESCE(SUM(CASE WHEN hit = 1 THEN request_count ELSE 0 END), 0) AS hits,
				COALESCE(SUM(CASE WHEN hit = 0 THEN request_count ELSE 0 END), 0) AS misses,
				COALESCE(SUM(total_bytes), 0) AS bytes_served`).
			Where("date >= ?", startDate).
			Group("date, hour").
			Order("date ASC, hour ASC").
			Scan(&rows)
	} else {
		h.db.Model(&db.AccessLog{}).
			Select(`strftime('%Y-%m-%d', created_at) AS date,
				CAST(strftime('%H', created_at) AS INTEGER) AS hour,
				COUNT(*) AS requests,
				COALESCE(SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END), 0) AS hits,
				COALESCE(SUM(CASE WHEN hit = 0 THEN 1 ELSE 0 END), 0) AS misses,
				COALESCE(SUM(bytes_sent), 0) AS bytes_served`).
			Where("created_at >= ?", since).
			Group("date, hour").
			Order("date ASC, hour ASC").
			Scan(&rows)
	}

	type key struct {
		date string
		hour int
	}
	lookup := make(map[key]bucketRow, len(rows))
	for _, r := range rows {
		lookup[key{r.Date, r.Hour}] = r
	}

	out := make([]trendPoint, 0, buckets)
	for i := 0; i < buckets; i++ {
		bucketStart := since.Add(time.Duration(i) * time.Hour)
		k := key{bucketStart.Format("2006-01-02"), bucketStart.Hour()}
		p := trendPoint{
			Bucket: bucketStart.Unix(),
			Date:   bucketStart.Format("2006-01-02 15:04"),
		}
		if r, ok := lookup[k]; ok {
			p.Requests = r.Requests
			p.Hits = r.Hits
			p.Misses = r.Misses
			p.BytesServed = r.BytesServed
			if p.Requests > 0 {
				p.HitRate = float64(p.Hits) / float64(p.Requests)
			}
		}
		out = append(out, p)
	}
	return out
}

// trendsByDay returns N daily buckets. Both rollup and raw paths group by
// date string so today's partial-hour data is included — access_log_hourly
// is live (updated by the recorder), unlike access_log_daily which lags one
// compactor cycle.
func (h *DashboardHandler) trendsByDay(days int) []trendPoint {
	now := time.Now().UTC()
	startDate := now.AddDate(0, 0, -(days - 1)).Truncate(24 * time.Hour)

	type bucketRow struct {
		Date        string
		Requests    int64
		Hits        int64
		Misses      int64
		BytesServed int64
	}
	var rows []bucketRow

	if h.useRollup {
		h.db.Table("access_log_hourly").
			Select(`date,
				COALESCE(SUM(request_count), 0) AS requests,
				COALESCE(SUM(CASE WHEN hit = 1 THEN request_count ELSE 0 END), 0) AS hits,
				COALESCE(SUM(CASE WHEN hit = 0 THEN request_count ELSE 0 END), 0) AS misses,
				COALESCE(SUM(total_bytes), 0) AS bytes_served`).
			Where("date >= ?", startDate.Format("2006-01-02")).
			Group("date").
			Order("date ASC").
			Scan(&rows)
	} else {
		h.db.Model(&db.AccessLog{}).
			Select(`DATE(created_at) AS date,
				COUNT(*) AS requests,
				COALESCE(SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END), 0) AS hits,
				COALESCE(SUM(CASE WHEN hit = 0 THEN 1 ELSE 0 END), 0) AS misses,
				COALESCE(SUM(bytes_sent), 0) AS bytes_served`).
			Where("datetime(created_at) >= datetime(?)", startDate).
			Group("DATE(created_at)").
			Order("date ASC").
			Scan(&rows)
	}

	lookup := make(map[string]bucketRow, len(rows))
	for _, r := range rows {
		lookup[r.Date] = r
	}
	out := make([]trendPoint, 0, days)
	for i := 0; i < days; i++ {
		d := startDate.AddDate(0, 0, i)
		ds := d.Format("2006-01-02")
		p := trendPoint{Bucket: d.Unix(), Date: ds}
		if r, ok := lookup[ds]; ok {
			p.Requests = r.Requests
			p.Hits = r.Hits
			p.Misses = r.Misses
			p.BytesServed = r.BytesServed
			if p.Requests > 0 {
				p.HitRate = float64(p.Hits) / float64(p.Requests)
			}
		}
		out = append(out, p)
	}
	return out
}
