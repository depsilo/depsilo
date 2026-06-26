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
		sevenDaysAgoDate := now.AddDate(0, 0, -7).Format("2006-01-02")
		h.db.Table("access_log_daily").
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

func (h *DashboardHandler) GetTrends(c *gin.Context) {
	rangeParam := c.DefaultQuery("range", "7d")

	var days int
	switch rangeParam {
	case "today":
		days = 1
	case "7d":
		days = 7
	case "30d":
		days = 30
	default:
		days = 7
	}

	startDate := time.Now().UTC().AddDate(0, 0, -days).Truncate(24 * time.Hour)

	type TrendPoint struct {
		Date        string  `json:"date"`
		Requests    int64   `json:"requests"`
		Hits        int64   `json:"hits"`
		Misses      int64   `json:"misses"`
		HitRate     float64 `json:"hit_rate"`
		BytesServed int64   `json:"bytes_served"`
	}

	var rawPoints []struct {
		Date        string
		Requests    int64
		Hits        int64
		Misses      int64
		BytesServed int64
	}

	if h.useRollup {
		// access_log_daily stores date as a string, so the range filter is a
		// string comparison (works because dates are ISO YYYY-MM-DD).
		h.db.Table("access_log_daily").
			Select(`date,
				COALESCE(SUM(request_count), 0) AS requests,
				COALESCE(SUM(CASE WHEN hit = 1 THEN request_count ELSE 0 END), 0) AS hits,
				COALESCE(SUM(CASE WHEN hit = 0 THEN request_count ELSE 0 END), 0) AS misses,
				COALESCE(SUM(total_bytes), 0) AS bytes_served`).
			Where("date >= ?", startDate.Format("2006-01-02")).
			Group("date").
			Order("date ASC").
			Scan(&rawPoints)
	} else {
		h.db.Model(&db.AccessLog{}).
			Select(`
				DATE(created_at) as date,
				COUNT(*) as requests,
				SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END) as hits,
				SUM(CASE WHEN hit = 0 THEN 1 ELSE 0 END) as misses,
				COALESCE(SUM(bytes_sent), 0) as bytes_served
			`).
			Where("datetime(created_at) >= datetime(?)", startDate).
			Group("DATE(created_at)").
			Order("date ASC").
			Scan(&rawPoints)
	}

	points := make([]TrendPoint, 0, len(rawPoints))
	for _, r := range rawPoints {
		p := TrendPoint{
			Date:        r.Date,
			Requests:    r.Requests,
			Hits:        r.Hits,
			Misses:      r.Misses,
			BytesServed: r.BytesServed,
		}
		if p.Requests > 0 {
			p.HitRate = float64(p.Hits) / float64(p.Requests)
		}
		points = append(points, p)
	}

	c.JSON(http.StatusOK, gin.H{"points": points})
}
