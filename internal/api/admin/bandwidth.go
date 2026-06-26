package admin

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

type BandwidthHandler struct {
	db        *gorm.DB
	useRollup bool
}

func NewBandwidthHandler(database *gorm.DB, useRollup bool) *BandwidthHandler {
	return &BandwidthHandler{db: database, useRollup: useRollup}
}

func (h *BandwidthHandler) GetReport(c *gin.Context) {
	rangeParam := c.DefaultQuery("range", "7d")
	startParam := c.Query("start")
	endParam := c.Query("end")

	var start, end time.Time
	now := time.Now()

	switch rangeParam {
	case "custom":
		var err error
		start, err = time.Parse("2006-01-02", startParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_DATE", "message": "invalid start date"})
			return
		}
		end, err = time.Parse("2006-01-02", endParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_DATE", "message": "invalid end date"})
			return
		}
		end = end.Add(24*time.Hour - time.Nanosecond) // end of day
	case "30d":
		start = now.AddDate(0, 0, -30).Truncate(24 * time.Hour)
		end = now
	case "90d":
		start = now.AddDate(0, 0, -90).Truncate(24 * time.Hour)
		end = now
	default: // 7d
		start = now.AddDate(0, 0, -7).Truncate(24 * time.Hour)
		end = now
	}

	// Normalise to UTC. The rollup tables key on UTC date strings, and the
	// raw fallback compares via datetime() so the boundary case is the same.
	start = start.UTC()
	end = end.UTC()
	startDateStr := start.Format("2006-01-02")
	endDateStr := end.Format("2006-01-02")

	// Summary: total/hit/miss bytes and counts
	type summaryRow struct {
		Hit        bool
		TotalBytes int64
		Count      int64
		AvgLatency float64
	}
	var summaryRows []summaryRow
	if h.useRollup {
		// access_log_daily — avg latency derived from sum/count.
		h.db.Table("access_log_daily").
			Select("hit, COALESCE(SUM(total_bytes), 0) AS total_bytes, COALESCE(SUM(request_count), 0) AS count, COALESCE(SUM(sum_latency_ms) * 1.0 / NULLIF(SUM(request_count), 0), 0) AS avg_latency").
			Where("date >= ? AND date <= ?", startDateStr, endDateStr).
			Group("hit").Scan(&summaryRows)
	} else {
		h.db.Model(&db.AccessLog{}).
			Select("hit, COALESCE(SUM(bytes_sent), 0) as total_bytes, COUNT(*) as count, COALESCE(AVG(latency_ms), 0) as avg_latency").
			Where("datetime(created_at) >= datetime(?) AND datetime(created_at) <= datetime(?)", start, end).
			Group("hit").Scan(&summaryRows)
	}

	var totalBytes, hitBytes, missBytes, totalRequests, hitRequests, missRequests int64
	var avgHitLatency, avgMissLatency float64
	for _, r := range summaryRows {
		totalBytes += r.TotalBytes
		totalRequests += r.Count
		if r.Hit {
			hitBytes = r.TotalBytes
			hitRequests = r.Count
			avgHitLatency = r.AvgLatency
		} else {
			missBytes = r.TotalBytes
			missRequests = r.Count
			avgMissLatency = r.AvgLatency
		}
	}

	var savingsRate float64
	if totalBytes > 0 {
		savingsRate = float64(hitBytes) / float64(totalBytes)
	}

	// Time saved: hit_count * (avg_miss_latency - avg_hit_latency) per ecosystem
	type ecoLatency struct {
		AdapterType  string
		Hit          bool
		AvgLatencyMs float64
		Count        int64
	}
	var ecoLatencies []ecoLatency
	if h.useRollup {
		h.db.Table("access_log_daily").
			Select("adapter_type, hit, COALESCE(SUM(sum_latency_ms) * 1.0 / NULLIF(SUM(request_count), 0), 0) AS avg_latency_ms, COALESCE(SUM(request_count), 0) AS count").
			Where("date >= ? AND date <= ?", startDateStr, endDateStr).
			Group("adapter_type, hit").Scan(&ecoLatencies)
	} else {
		h.db.Model(&db.AccessLog{}).
			Select("adapter_type, hit, AVG(latency_ms) as avg_latency_ms, COUNT(*) as count").
			Where("datetime(created_at) >= datetime(?) AND datetime(created_at) <= datetime(?)", start, end).
			Group("adapter_type, hit").Scan(&ecoLatencies)
	}

	// Build per-ecosystem latency maps
	type ecoStats struct {
		hitCount       int64
		avgHitLatency  float64
		avgMissLatency float64
	}
	ecoMap := make(map[string]*ecoStats)
	for _, el := range ecoLatencies {
		if ecoMap[el.AdapterType] == nil {
			ecoMap[el.AdapterType] = &ecoStats{}
		}
		if el.Hit {
			ecoMap[el.AdapterType].hitCount = el.Count
			ecoMap[el.AdapterType].avgHitLatency = el.AvgLatencyMs
		} else {
			ecoMap[el.AdapterType].avgMissLatency = el.AvgLatencyMs
		}
	}

	var timeSavedMs int64
	for _, es := range ecoMap {
		missLat := es.avgMissLatency
		if missLat == 0 && avgMissLatency > 0 {
			missLat = avgMissLatency // fallback to global avg
		}
		diff := missLat - es.avgHitLatency
		if diff > 0 {
			timeSavedMs += int64(diff * float64(es.hitCount))
		}
	}

	// Daily breakdown
	type dailyRow struct {
		Date  string
		Hit   bool
		Bytes int64
		Count int64
	}
	var dailyRows []dailyRow
	if h.useRollup {
		h.db.Table("access_log_daily").
			Select("date, hit, COALESCE(SUM(total_bytes), 0) AS bytes, COALESCE(SUM(request_count), 0) AS count").
			Where("date >= ? AND date <= ?", startDateStr, endDateStr).
			Group("date, hit").Order("date").Scan(&dailyRows)
	} else {
		h.db.Model(&db.AccessLog{}).
			Select("DATE(created_at) as date, hit, COALESCE(SUM(bytes_sent), 0) as bytes, COUNT(*) as count").
			Where("datetime(created_at) >= datetime(?) AND datetime(created_at) <= datetime(?)", start, end).
			Group("date, hit").Order("date").Scan(&dailyRows)
	}

	type dailyPoint struct {
		Date      string `json:"date"`
		HitBytes  int64  `json:"hit_bytes"`
		MissBytes int64  `json:"miss_bytes"`
		HitCount  int64  `json:"hit_count"`
		MissCount int64  `json:"miss_count"`
	}
	dailyMap := make(map[string]*dailyPoint)
	var dailyOrder []string
	for _, r := range dailyRows {
		if dailyMap[r.Date] == nil {
			dailyMap[r.Date] = &dailyPoint{Date: r.Date}
			dailyOrder = append(dailyOrder, r.Date)
		}
		dp := dailyMap[r.Date]
		if r.Hit {
			dp.HitBytes = r.Bytes
			dp.HitCount = r.Count
		} else {
			dp.MissBytes = r.Bytes
			dp.MissCount = r.Count
		}
	}
	daily := make([]dailyPoint, 0, len(dailyOrder))
	for _, d := range dailyOrder {
		daily = append(daily, *dailyMap[d])
	}

	// By ecosystem
	type ecoRow struct {
		AdapterType string
		Hit         bool
		Bytes       int64
		Count       int64
		AvgLatency  float64
	}
	var ecoRows []ecoRow
	if h.useRollup {
		h.db.Table("access_log_daily").
			Select("adapter_type, hit, COALESCE(SUM(total_bytes), 0) AS bytes, COALESCE(SUM(request_count), 0) AS count, COALESCE(SUM(sum_latency_ms) * 1.0 / NULLIF(SUM(request_count), 0), 0) AS avg_latency").
			Where("date >= ? AND date <= ?", startDateStr, endDateStr).
			Group("adapter_type, hit").Scan(&ecoRows)
	} else {
		h.db.Model(&db.AccessLog{}).
			Select("adapter_type, hit, COALESCE(SUM(bytes_sent), 0) as bytes, COUNT(*) as count, COALESCE(AVG(latency_ms), 0) as avg_latency").
			Where("datetime(created_at) >= datetime(?) AND datetime(created_at) <= datetime(?)", start, end).
			Group("adapter_type, hit").Scan(&ecoRows)
	}

	type ecoPoint struct {
		Ecosystem        string  `json:"ecosystem"`
		HitBytes         int64   `json:"hit_bytes"`
		MissBytes        int64   `json:"miss_bytes"`
		HitCount         int64   `json:"hit_count"`
		MissCount        int64   `json:"miss_count"`
		AvgHitLatencyMs  float64 `json:"avg_hit_latency_ms"`
		AvgMissLatencyMs float64 `json:"avg_miss_latency_ms"`
	}
	ecoPointMap := make(map[string]*ecoPoint)
	for _, r := range ecoRows {
		if ecoPointMap[r.AdapterType] == nil {
			ecoPointMap[r.AdapterType] = &ecoPoint{Ecosystem: r.AdapterType}
		}
		ep := ecoPointMap[r.AdapterType]
		if r.Hit {
			ep.HitBytes = r.Bytes
			ep.HitCount = r.Count
			ep.AvgHitLatencyMs = r.AvgLatency
		} else {
			ep.MissBytes = r.Bytes
			ep.MissCount = r.Count
			ep.AvgMissLatencyMs = r.AvgLatency
		}
	}
	byEcosystem := make([]ecoPoint, 0, len(ecoPointMap))
	for _, ep := range ecoPointMap {
		byEcosystem = append(byEcosystem, *ep)
	}

	// Top packages by total bytes
	type pkgRow struct {
		PackageName string `json:"package_name"`
		Ecosystem   string `json:"ecosystem"`
		TotalBytes  int64  `json:"total_bytes"`
		HitBytes    int64  `json:"hit_bytes"`
		Count       int64  `json:"request_count"`
	}
	var topPackages []pkgRow
	if h.useRollup {
		h.db.Table("access_log_package_daily").
			Select("package_name, adapter_type AS ecosystem, COALESCE(SUM(total_bytes), 0) AS total_bytes, COALESCE(SUM(CASE WHEN hit = 1 THEN total_bytes ELSE 0 END), 0) AS hit_bytes, COALESCE(SUM(request_count), 0) AS count").
			Where("date >= ? AND date <= ? AND package_name != ''", startDateStr, endDateStr).
			Group("package_name, adapter_type").
			Order("total_bytes DESC").Limit(10).Scan(&topPackages)
	} else {
		h.db.Model(&db.AccessLog{}).
			Select("package_name, adapter_type as ecosystem, SUM(bytes_sent) as total_bytes, SUM(CASE WHEN hit = 1 THEN bytes_sent ELSE 0 END) as hit_bytes, COUNT(*) as count").
			Where("datetime(created_at) >= datetime(?) AND datetime(created_at) <= datetime(?) AND package_name != ''", start, end).
			Group("package_name, adapter_type").
			Order("total_bytes DESC").Limit(10).Scan(&topPackages)
	}

	// By upstream (miss only)
	type upstreamRow struct {
		Upstream     string  `json:"upstream"`
		MissBytes    int64   `json:"miss_bytes"`
		RequestCount int64   `json:"request_count"`
		AvgLatencyMs float64 `json:"avg_latency_ms"`
	}
	var byUpstream []upstreamRow
	if h.useRollup {
		h.db.Table("access_log_daily").
			Select("upstream, COALESCE(SUM(total_bytes), 0) AS miss_bytes, COALESCE(SUM(request_count), 0) AS request_count, COALESCE(SUM(sum_latency_ms) * 1.0 / NULLIF(SUM(request_count), 0), 0) AS avg_latency_ms").
			Where("date >= ? AND date <= ? AND hit = ? AND upstream != ''", startDateStr, endDateStr, false).
			Group("upstream").Order("miss_bytes DESC").Scan(&byUpstream)
	} else {
		h.db.Model(&db.AccessLog{}).
			Select("upstream, COALESCE(SUM(bytes_sent), 0) as miss_bytes, COUNT(*) as request_count, COALESCE(AVG(latency_ms), 0) as avg_latency_ms").
			Where("datetime(created_at) >= datetime(?) AND datetime(created_at) <= datetime(?) AND hit = ? AND upstream != ''", start, end, false).
			Group("upstream").Order("miss_bytes DESC").Scan(&byUpstream)
	}

	c.JSON(http.StatusOK, gin.H{
		"range": gin.H{
			"start": start.Format("2006-01-02"),
			"end":   end.Format("2006-01-02"),
		},
		"summary": gin.H{
			"total_bytes":      totalBytes,
			"hit_bytes":        hitBytes,
			"miss_bytes":       missBytes,
			"savings_rate":     savingsRate,
			"total_requests":   totalRequests,
			"hit_requests":     hitRequests,
			"miss_requests":    missRequests,
			"time_saved_ms":    timeSavedMs,
			"avg_hit_latency":  avgHitLatency,
			"avg_miss_latency": avgMissLatency,
		},
		"daily":        daily,
		"by_ecosystem": byEcosystem,
		"top_packages": topPackages,
		"by_upstream":  byUpstream,
	})
}
