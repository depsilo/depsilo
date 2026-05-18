package public

import (
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
	"depsilo/internal/version"
)

type StatsHandler struct {
	db           *gorm.DB
	storage      cache.Storage
	pools        map[string]*upstream.Pool
	ecosystems   []string
	startTime    time.Time
	extraIndexes []config.ExtraIndexConfig
}

func NewStatsHandler(database *gorm.DB, storage cache.Storage, pools map[string]*upstream.Pool, ecosystems []string, extraIndexes []config.ExtraIndexConfig) *StatsHandler {
	return &StatsHandler{
		db:           database,
		storage:      storage,
		pools:        pools,
		ecosystems:   ecosystems,
		startTime:    time.Now(),
		extraIndexes: extraIndexes,
	}
}

// kpiSeries returns 12 five-minute buckets covering the last hour,
// aggregated from the access_logs table.
func (h *StatsHandler) kpiSeries(since time.Time) []gin.H {
	const buckets = 12
	const intervalMin = 5

	type bucketRow struct {
		Bucket      int64
		Requests    int64
		Hits        int64
		BytesSaved  int64
		MissLatency float64
		MissCount   int64
	}

	var rows []bucketRow
	h.db.Model(&db.AccessLog{}).
		Select(`(CAST(strftime('%s', created_at) AS INTEGER) / ?) * ? AS bucket,
			COUNT(*) AS requests,
			SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END) AS hits,
			COALESCE(SUM(CASE WHEN hit = 1 THEN bytes_sent ELSE 0 END), 0) AS bytes_saved,
			COALESCE(SUM(CASE WHEN hit = 0 THEN latency_ms ELSE 0 END), 0) AS miss_latency,
			SUM(CASE WHEN hit = 0 THEN 1 ELSE 0 END) AS miss_count`,
			intervalMin*60, intervalMin*60).
		Where("created_at >= ?", since).
		Group("bucket").
		Order("bucket ASC").
		Scan(&rows)

	// Build a lookup map
	lookup := make(map[int64]*bucketRow, len(rows))
	for i := range rows {
		lookup[rows[i].Bucket] = &rows[i]
	}

	// Compute the aligned start bucket
	startBucket := (since.Unix() / int64(intervalMin*60)) * int64(intervalMin*60)

	points := make([]gin.H, 0, buckets)
	for i := 0; i < buckets; i++ {
		ts := startBucket + int64(i*intervalMin*60)
		p := gin.H{
			"time_unix":      ts,
			"requests":       int64(0),
			"hits":           int64(0),
			"hit_rate":       float64(0),
			"bytes_saved":    int64(0),
			"avg_latency_ms": float64(0),
		}
		if r, ok := lookup[ts]; ok {
			var hitRate float64
			if r.Requests > 0 {
				hitRate = math.Round(float64(r.Hits)/float64(r.Requests)*1000) / 1000
			}
			var avgLat float64
			if r.MissCount > 0 {
				avgLat = math.Round(float64(r.MissLatency)/float64(r.MissCount)*10) / 10
			}
			p["requests"] = r.Requests
			p["hits"] = r.Hits
			p["hit_rate"] = hitRate
			p["bytes_saved"] = r.BytesSaved
			p["avg_latency_ms"] = avgLat
		}
		points = append(points, p)
	}
	return points
}

const latencyBuckets = 90
const latencyIntervalMin = 16

// allUpstreamLatencySeries runs a single query for ALL upstreams and returns
// a map of name → 90-point series. This replaces N per-upstream queries.
func (h *StatsHandler) allUpstreamLatencySeries(since time.Time) map[string][]gin.H {
	type bucketRow struct {
		Name     string
		Bucket   int64
		AvgLat   float64
		AvgHP    float64
		Requests int64
	}

	var rows []bucketRow
	h.db.Model(&db.UpstreamLatencyLog{}).
		Select(fmt.Sprintf(`name,
			(CAST(strftime('%%s', created_at) AS INTEGER) / %d) * %d AS bucket,
			AVG(latency_ms) AS avg_lat,
			AVG(CASE WHEN healthy = 1 THEN 1.0 ELSE 0.0 END) AS avg_hp,
			COUNT(*) AS requests`,
			latencyIntervalMin*60, latencyIntervalMin*60)).
		Where("created_at >= ?", since).
		Group("name, bucket").
		Order("name, bucket ASC").
		Scan(&rows)

	// Index by name+bucket
	type key struct{ name string; bucket int64 }
	lookup := make(map[key]*bucketRow, len(rows))
	names := make(map[string]bool)
	for i := range rows {
		lookup[key{rows[i].Name, rows[i].Bucket}] = &rows[i]
		names[rows[i].Name] = true
	}

	startBucket := (since.Unix() / int64(latencyIntervalMin*60)) * int64(latencyIntervalMin*60)

	result := make(map[string][]gin.H, len(names))
	for name := range names {
		points := make([]gin.H, 0, latencyBuckets)
		for i := 0; i < latencyBuckets; i++ {
			ts := startBucket + int64(i*latencyIntervalMin*60)
			t := time.Unix(ts, 0).UTC()
			p := gin.H{
				"time":       t.Format(time.RFC3339),
				"latency_ms": int64(0),
				"healthy":    true,
				"requests":   int64(0),
			}
			if r, ok := lookup[key{name, ts}]; ok {
				p["latency_ms"] = int64(math.Round(r.AvgLat))
				p["healthy"] = r.AvgHP > 0.5
				p["requests"] = r.Requests
			}
			points = append(points, p)
		}
		result[name] = points
	}
	return result
}

// GetLatencySeries returns all upstream latency series in one response (public, no auth).
func (h *StatsHandler) GetLatencySeries(c *gin.Context) {
	all := h.allUpstreamLatencySeries(time.Now().Add(-24 * time.Hour))
	c.JSON(http.StatusOK, all)
}

func (h *StatsHandler) GetStats(c *gin.Context) {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	since := now.Add(-1 * time.Hour)

	// KPI series (12 x 5-min buckets)
	seriesPoints := h.kpiSeries(since)

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
	for _, name := range h.ecosystems {
		pool := h.pools[name]
		if pool == nil {
			continue
		}
		for _, u := range pool.Upstreams() {
			upstreams = append(upstreams, gin.H{
				"name":           u.Name,
				"adapter":        name,
				"url":            u.URL,
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

	// Extra indexes
	extraIdxs := make([]gin.H, 0, len(h.extraIndexes))
	for _, idx := range h.extraIndexes {
		extraIdxs = append(extraIdxs, gin.H{
			"name": idx.Name,
			"path": idx.Path,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"service": gin.H{
			"version":        version.Version,
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
		"series": gin.H{
			"interval_minutes": 5,
			"points":           seriesPoints,
		},
		"upstreams": upstreams,
		"top_packages": gin.H{
			"pypi": pypiTop,
			"apt":  aptTop,
		},
		"extra_indexes": extraIdxs,
	})
}
