package public

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/api/credentialurl"
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
	useRollup    bool
}

func NewStatsHandler(database *gorm.DB, storage cache.Storage, pools map[string]*upstream.Pool, ecosystems []string, extraIndexes []config.ExtraIndexConfig, useRollup bool) *StatsHandler {
	return &StatsHandler{
		db:           database,
		storage:      storage,
		pools:        pools,
		ecosystems:   ecosystems,
		startTime:    time.Now(),
		extraIndexes: extraIndexes,
		useRollup:    useRollup,
	}
}

// StartTime exposes the process start instant captured at handler
// construction. Used by NewNowHandler so /api/v1/now's uptime agrees
// with /api/v1/stats.
func (h *StatsHandler) StartTime() time.Time { return h.startTime }

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
		Where("datetime(created_at) >= datetime(?)", since.UTC()).
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

// Keep aligned with the shared Portal rule in web/src/lib/upstreamStatus.ts
// and DESIGN.md: an available upstream becomes degraded at 150 ms.
const publicUpstreamDegradedLatency = 150 * time.Millisecond

// allUpstreamLatencySeries runs a single query for ALL upstreams and returns
// a map of stable upstream identity → 90-point series. Persisted upstreams use
// their database ID; pre-ID legacy rows remain available under legacy:<name>.
// This replaces N per-upstream queries without merging same-named records.
func (h *StatsHandler) allUpstreamLatencySeries(since time.Time) (map[string][]gin.H, error) {
	type bucketRow struct {
		UpstreamID uint
		Name       string
		Bucket     int64
		AvgLat     float64
		AvgHP      float64
		Requests   int64
	}

	var rows []bucketRow
	query := h.db.Table("upstream_latency_logs AS l").
		Select(fmt.Sprintf(`l.upstream_id,
			COALESCE(NULLIF(u.name, ''), l.name) AS name,
			(CAST(strftime('%%s', l.created_at) AS INTEGER) / %d) * %d AS bucket,
			AVG(l.latency_ms) AS avg_lat,
			AVG(CASE WHEN l.healthy = 1 THEN 1.0 ELSE 0.0 END) AS avg_hp,
			COUNT(*) AS requests`,
			latencyIntervalMin*60, latencyIntervalMin*60)).
		Joins("LEFT JOIN upstream_records AS u ON u.id = l.upstream_id").
		Where("datetime(l.created_at) >= datetime(?)", since.UTC()).
		Group("l.upstream_id, COALESCE(NULLIF(u.name, ''), l.name), bucket").
		Order("name ASC, l.upstream_id ASC, bucket ASC").
		Scan(&rows)
	if query.Error != nil {
		return nil, query.Error
	}

	type bucketKey struct {
		series string
		bucket int64
	}
	type bucketAggregate struct {
		latencyTotal float64
		healthyTotal float64
		requests     int64
	}
	lookup := make(map[bucketKey]*bucketAggregate, len(rows))
	seriesSet := make(map[string]struct{})
	for i := range rows {
		row := &rows[i]
		seriesKey := upstreamLatencySeriesKey(row.UpstreamID, row.Name)
		key := bucketKey{series: seriesKey, bucket: row.Bucket}
		aggregate := lookup[key]
		if aggregate == nil {
			aggregate = &bucketAggregate{}
			lookup[key] = aggregate
		}
		aggregate.latencyTotal += row.AvgLat * float64(row.Requests)
		aggregate.healthyTotal += row.AvgHP * float64(row.Requests)
		aggregate.requests += row.Requests
		seriesSet[seriesKey] = struct{}{}
	}
	seriesKeys := make([]string, 0, len(seriesSet))
	for seriesKey := range seriesSet {
		seriesKeys = append(seriesKeys, seriesKey)
	}
	sort.Strings(seriesKeys)

	startBucket := (since.Unix() / int64(latencyIntervalMin*60)) * int64(latencyIntervalMin*60)

	result := make(map[string][]gin.H, len(seriesKeys))
	for _, seriesKey := range seriesKeys {
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
			if aggregate, ok := lookup[bucketKey{series: seriesKey, bucket: ts}]; ok && aggregate.requests > 0 {
				p["latency_ms"] = int64(math.Round(aggregate.latencyTotal / float64(aggregate.requests)))
				p["healthy"] = aggregate.healthyTotal/float64(aggregate.requests) > 0.5
				p["requests"] = aggregate.requests
			}
			points = append(points, p)
		}
		result[seriesKey] = points
	}
	return result, nil
}

func upstreamLatencySeriesKey(upstreamID uint, name string) string {
	if upstreamID != 0 {
		return strconv.FormatUint(uint64(upstreamID), 10)
	}
	return "legacy:" + name
}

// GetLatencySeries returns all upstream latency series in one response (public, no auth).
func (h *StatsHandler) GetLatencySeries(c *gin.Context) {
	all, err := h.allUpstreamLatencySeries(time.Now().Add(-24 * time.Hour))
	if err != nil {
		zap.L().Error("load upstream latency series", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "failed to load latency series"})
		return
	}
	c.JSON(http.StatusOK, all)
}

func (h *StatsHandler) GetStats(c *gin.Context) {
	c.JSON(http.StatusOK, h.snapshot(time.Now()))
}

// snapshot is the single in-process source for the public stats contract.
// HTTP and MCP callers serialize this value directly; neither needs to fetch
// the public route over the network.
func (h *StatsHandler) snapshot(now time.Time) gin.H {
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	since := now.Add(-1 * time.Hour)

	// KPI series (12 x 5-min buckets)
	seriesPoints := h.kpiSeries(since)

	// Today's stats. Rollup path reads from access_log_hourly (one query
	// covers all four KPIs); raw path keeps the original four-COUNT/SUM
	// shape so the fallback diff stays small.
	var totalRequests, hitCount, missCount int64
	var bytesSent, bytesSaved int64

	todayStartUTC := todayStart.UTC()
	if h.useRollup {
		todayDate := todayStartUTC.Format("2006-01-02")
		var t struct {
			Total      int64
			Hits       int64
			Bytes      int64
			BytesSaved int64
		}
		h.db.Table("access_log_hourly").
			Select(`COALESCE(SUM(request_count),0) AS total,
				COALESCE(SUM(CASE WHEN hit=1 THEN request_count ELSE 0 END),0) AS hits,
				COALESCE(SUM(total_bytes),0) AS bytes,
				COALESCE(SUM(CASE WHEN hit=1 THEN total_bytes ELSE 0 END),0) AS bytes_saved`).
			Where("date = ?", todayDate).
			Scan(&t)
		totalRequests = t.Total
		hitCount = t.Hits
		bytesSent = t.Bytes
		bytesSaved = t.BytesSaved
	} else {
		h.db.Model(&db.AccessLog{}).Where("datetime(created_at) >= datetime(?)", todayStartUTC).Count(&totalRequests)
		h.db.Model(&db.AccessLog{}).Where("datetime(created_at) >= datetime(?) AND hit = ?", todayStartUTC, true).Count(&hitCount)
		h.db.Model(&db.AccessLog{}).Where("datetime(created_at) >= datetime(?)", todayStartUTC).
			Select("COALESCE(SUM(bytes_sent), 0)").Scan(&bytesSent)
		h.db.Model(&db.AccessLog{}).Where("datetime(created_at) >= datetime(?) AND hit = ?", todayStartUTC, true).
			Select("COALESCE(SUM(bytes_sent), 0)").Scan(&bytesSaved)
	}
	missCount = totalRequests - hitCount

	var hitRate float64
	if totalRequests > 0 {
		hitRate = float64(hitCount) / float64(totalRequests)
	}

	// 7-day rolling window (6 finished days + today). The daily rollup
	// only holds FINISHED days (the compactor runs at UTC 00:05), so
	// the week block sums access_log_daily for [today-6, yesterday] and
	// adds today's totals computed above. Portal surfaces use this
	// instead of the today block — a day-scoped hit rate resets at
	// midnight and swings wildly at low sample counts.
	var weekTotal, weekHits, weekBytesSaved int64
	if h.useRollup {
		todayDate := todayStartUTC.Format("2006-01-02")
		weekStartDate := todayStartUTC.AddDate(0, 0, -6).Format("2006-01-02")
		var w struct {
			Total      int64
			Hits       int64
			BytesSaved int64
		}
		h.db.Table("access_log_daily").
			Select(`COALESCE(SUM(request_count),0) AS total,
				COALESCE(SUM(CASE WHEN hit=1 THEN request_count ELSE 0 END),0) AS hits,
				COALESCE(SUM(CASE WHEN hit=1 THEN total_bytes ELSE 0 END),0) AS bytes_saved`).
			Where("date >= ? AND date < ?", weekStartDate, todayDate).
			Scan(&w)
		weekTotal = w.Total + totalRequests
		weekHits = w.Hits + hitCount
		weekBytesSaved = w.BytesSaved + bytesSaved
	} else {
		weekStartUTC := todayStartUTC.AddDate(0, 0, -6)
		h.db.Model(&db.AccessLog{}).Where("datetime(created_at) >= datetime(?)", weekStartUTC).Count(&weekTotal)
		h.db.Model(&db.AccessLog{}).Where("datetime(created_at) >= datetime(?) AND hit = ?", weekStartUTC, true).Count(&weekHits)
		h.db.Model(&db.AccessLog{}).Where("datetime(created_at) >= datetime(?) AND hit = ?", weekStartUTC, true).
			Select("COALESCE(SUM(bytes_sent), 0)").Scan(&weekBytesSaved)
	}
	var weekHitRate float64
	if weekTotal > 0 {
		weekHitRate = float64(weekHits) / float64(weekTotal)
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
	healthyUpstreams := 0
	hasDegradedServiceCondition := false
	for _, name := range h.ecosystems {
		pool := h.pools[name]
		if pool == nil {
			continue
		}
		for _, u := range pool.Snapshot() {
			health := u.HealthSnapshot()
			if health.Healthy {
				healthyUpstreams++
			}
			if !health.Healthy || health.AvgLatency >= publicUpstreamDegradedLatency {
				hasDegradedServiceCondition = true
			}
			upstreams = append(upstreams, gin.H{
				"id":             u.ID,
				"name":           u.Name,
				"adapter":        name,
				"url":            credentialurl.PublicOrigin(u.URL),
				"healthy":        health.Healthy,
				"avg_latency_ms": health.AvgLatency.Milliseconds(),
				"success_rate":   health.SuccessRate,
			})
		}
	}
	serviceStatus := "healthy"
	if len(upstreams) > 0 && hasDegradedServiceCondition {
		serviceStatus = "degraded"
		if healthyUpstreams == 0 {
			serviceStatus = "failed"
		}
	}

	// Extra indexes
	extraIdxs := make([]gin.H, 0, len(h.extraIndexes))
	for _, idx := range h.extraIndexes {
		extraIdxs = append(extraIdxs, gin.H{
			"name": idx.Name,
			"kind": idx.Kind,
			"path": idx.Path,
		})
	}

	return gin.H{
		"service": gin.H{
			"version":        version.Version,
			"uptime_seconds": int64(now.Sub(h.startTime).Seconds()),
			"status":         serviceStatus,
		},
		"today": gin.H{
			"total_requests": totalRequests,
			"hit_count":      hitCount,
			"miss_count":     missCount,
			"hit_rate":       hitRate,
			"bytes_served":   bytesSent,
			"bytes_saved":    bytesSaved,
		},
		"week": gin.H{
			"total_requests": weekTotal,
			"hit_count":      weekHits,
			"hit_rate":       weekHitRate,
			"bytes_saved":    weekBytesSaved,
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
		"upstreams":     upstreams,
		"extra_indexes": extraIdxs,
	}
}
