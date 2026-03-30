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
	db       *gorm.DB
	storage  cache.Storage
	pypiPool *upstream.Pool
	aptPool  *upstream.Pool
}

func NewDashboardHandler(database *gorm.DB, storage cache.Storage, pypiPool, aptPool *upstream.Pool) *DashboardHandler {
	return &DashboardHandler{db: database, storage: storage, pypiPool: pypiPool, aptPool: aptPool}
}

func (h *DashboardHandler) GetDashboard(c *gin.Context) {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	var totalRequests, hitCount int64
	var bytesSent int64
	h.db.Model(&db.AccessLog{}).Where("created_at >= ?", todayStart).Count(&totalRequests)
	h.db.Model(&db.AccessLog{}).Where("created_at >= ? AND hit = ?", todayStart, true).Count(&hitCount)
	h.db.Model(&db.AccessLog{}).Where("created_at >= ?", todayStart).
		Select("COALESCE(SUM(bytes_sent), 0)").Scan(&bytesSent)

	var hitRate float64
	if totalRequests > 0 {
		hitRate = float64(hitCount) / float64(totalRequests)
	}

	var avgLatency float64
	h.db.Model(&db.AccessLog{}).Where("created_at >= ?", todayStart).
		Select("COALESCE(AVG(latency_ms), 0)").Scan(&avgLatency)

	// Daily stats for the last 7 days
	type dailyStat struct {
		Date        string `json:"date"`
		AdapterType string `json:"adapter_type"`
		Count       int64  `json:"count"`
	}
	var dailyStats []dailyStat
	sevenDaysAgo := now.AddDate(0, 0, -7)
	h.db.Model(&db.AccessLog{}).
		Select("DATE(created_at) as date, adapter_type, COUNT(*) as count").
		Where("created_at >= ?", sevenDaysAgo).
		Group("date, adapter_type").Order("date").
		Scan(&dailyStats)

	// Upstream status
	upstreams := make([]gin.H, 0)
	for _, u := range h.pypiPool.Upstreams() {
		upstreams = append(upstreams, gin.H{
			"name":           u.Name,
			"adapter":        "pypi",
			"healthy":        u.Healthy,
			"avg_latency_ms": u.AvgLatency().Milliseconds(),
			"success_rate":   u.SuccessRate(),
		})
	}
	for _, u := range h.aptPool.Upstreams() {
		upstreams = append(upstreams, gin.H{
			"name":           u.Name,
			"adapter":        "apt",
			"healthy":        u.Healthy,
			"avg_latency_ms": u.AvgLatency().Milliseconds(),
			"success_rate":   u.SuccessRate(),
		})
	}

	// Top packages
	type topPkg struct {
		PackageName string `json:"name"`
		HitCount    int64  `json:"hit_count"`
	}
	var pypiTop, aptTop []topPkg
	h.db.Model(&db.AccessLog{}).
		Select("package_name, COUNT(*) as hit_count").
		Where("adapter_type = ? AND package_name != ''", "pypi").
		Group("package_name").Order("hit_count DESC").Limit(10).Scan(&pypiTop)
	h.db.Model(&db.AccessLog{}).
		Select("package_name, COUNT(*) as hit_count").
		Where("adapter_type = ? AND package_name != ''", "apt").
		Group("package_name").Order("hit_count DESC").Limit(10).Scan(&aptTop)

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

	startDate := time.Now().AddDate(0, 0, -days).Truncate(24 * time.Hour)

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

	h.db.Model(&db.AccessLog{}).
		Select(`
			DATE(created_at) as date,
			COUNT(*) as requests,
			SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END) as hits,
			SUM(CASE WHEN hit = 0 THEN 1 ELSE 0 END) as misses,
			COALESCE(SUM(bytes_sent), 0) as bytes_served
		`).
		Where("created_at >= ?", startDate).
		Group("DATE(created_at)").
		Order("date ASC").
		Scan(&rawPoints)

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
