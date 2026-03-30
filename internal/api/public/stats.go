package public

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

type StatsHandler struct {
	db        *gorm.DB
	storage   cache.Storage
	pypiPool  *upstream.Pool
	aptPool   *upstream.Pool
	startTime time.Time
}

func NewStatsHandler(database *gorm.DB, storage cache.Storage, pypiPool, aptPool *upstream.Pool) *StatsHandler {
	return &StatsHandler{
		db:        database,
		storage:   storage,
		pypiPool:  pypiPool,
		aptPool:   aptPool,
		startTime: time.Now(),
	}
}

func (h *StatsHandler) GetStats(c *gin.Context) {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// Today's stats from access logs
	var totalRequests, hitCount, missCount int64
	var bytesSent, bytesSaved int64

	h.db.Model(&db.AccessLog{}).Where("created_at >= ?", todayStart).Count(&totalRequests)
	h.db.Model(&db.AccessLog{}).Where("created_at >= ? AND hit = ?", todayStart, true).Count(&hitCount)
	missCount = totalRequests - hitCount

	h.db.Model(&db.AccessLog{}).Where("created_at >= ?", todayStart).
		Select("COALESCE(SUM(bytes_sent), 0)").Scan(&bytesSent)
	h.db.Model(&db.AccessLog{}).Where("created_at >= ? AND hit = ?", todayStart, true).
		Select("COALESCE(SUM(bytes_sent), 0)").Scan(&bytesSaved)

	var hitRate float64
	if totalRequests > 0 {
		hitRate = float64(hitCount) / float64(totalRequests)
	}

	// Cache stats
	var totalFiles int64
	var totalSizeBytes int64
	var pypiFiles, aptFiles int64

	h.db.Model(&db.CacheEntry{}).Count(&totalFiles)
	h.db.Model(&db.CacheEntry{}).Select("COALESCE(SUM(size), 0)").Scan(&totalSizeBytes)
	h.db.Model(&db.CacheEntry{}).Where("adapter_type = ?", "pypi").Count(&pypiFiles)
	h.db.Model(&db.CacheEntry{}).Where("adapter_type = ?", "apt").Count(&aptFiles)

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
		Group("package_name").Order("hit_count DESC").Limit(10).
		Scan(&pypiTop)
	h.db.Model(&db.AccessLog{}).
		Select("package_name, COUNT(*) as hit_count").
		Where("adapter_type = ? AND package_name != ''", "apt").
		Group("package_name").Order("hit_count DESC").Limit(10).
		Scan(&aptTop)

	c.JSON(http.StatusOK, gin.H{
		"service": gin.H{
			"version":        "0.1.0",
			"uptime_seconds": int64(time.Since(h.startTime).Seconds()),
			"status":         "healthy",
		},
		"today": gin.H{
			"total_requests": totalRequests,
			"hit_count":      hitCount,
			"miss_count":     missCount,
			"hit_rate":       hitRate,
			"bytes_served":   bytesSent,
			"bytes_saved":    bytesSaved,
		},
		"cache": gin.H{
			"total_files":      totalFiles,
			"total_size_bytes": totalSizeBytes,
			"pypi_files":       pypiFiles,
			"apt_files":        aptFiles,
		},
		"upstreams": upstreams,
		"top_packages": gin.H{
			"pypi": pypiTop,
			"apt":  aptTop,
		},
	})
}
