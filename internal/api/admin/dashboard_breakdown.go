package admin

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/db"
)

// Breakdown list bounds. Ecosystems are a closed set, so they are not capped.
const (
	breakdownUpstreamLimit = 20
	breakdownPackageLimit  = 10
)

// Package ranking granularity. The package rollup is day-grained, so a coarse
// range ranks whole UTC days and can include requests from before the range
// start on its first day. The response names the granularity so a client
// never presents that ranking as an exact window.
const (
	breakdownGrainExact  = "exact"
	breakdownGrainUTCDay = "utc_day"
)

type breakdownEcosystem struct {
	Ecosystem        string  `json:"ecosystem"`
	HitRequests      int64   `json:"hit_requests"`
	MissRequests     int64   `json:"miss_requests"`
	HitBytes         int64   `json:"hit_bytes"`
	MissBytes        int64   `json:"miss_bytes"`
	AvgHitLatencyMs  float64 `json:"avg_hit_latency_ms"`
	AvgMissLatencyMs float64 `json:"avg_miss_latency_ms"`
}

// breakdownUpstream is one Upstream's part in the period. UpstreamRequests
// and UpstreamBytes are the measured Depsilo→upstream exchanges, the same
// quantity as the period block's origin traffic; coalesced or aborted client
// downloads make them differ from what clients received. The miss fields
// describe the client requests answered through this Upstream. Upstream names
// are unique only within an ecosystem, so the ecosystem is part of the key.
type breakdownUpstream struct {
	Ecosystem        string  `json:"ecosystem"`
	Upstream         string  `json:"upstream"`
	UpstreamRequests int64   `json:"upstream_requests"`
	UpstreamBytes    int64   `json:"upstream_bytes"`
	MissRequests     int64   `json:"miss_requests"`
	MissBytes        int64   `json:"miss_bytes"`
	AvgMissLatencyMs float64 `json:"avg_miss_latency_ms"`
}

type breakdownPackage struct {
	Ecosystem   string `json:"ecosystem"`
	PackageName string `json:"package_name"`
	Requests    int64  `json:"requests"`
	TotalBytes  int64  `json:"total_bytes"`
	HitBytes    int64  `json:"hit_bytes"`
}

// breakdownRow is one (ecosystem, hit, upstream) cell of the selected period.
// Both sources group on the same dimensions, so one fold serves either.
type breakdownRow struct {
	AdapterType      string
	Hit              bool
	Upstream         string
	Requests         int64
	Bytes            int64
	SumLatency       int64
	UpstreamRequests int64
	UpstreamBytes    int64
}

// GetBreakdown reports where the selected Overview period's traffic went: per
// ecosystem, per Upstream (measured origin exchanges and the misses served
// through it), and the largest packages. It uses
// the same range keys, boundaries, and request set as the period block of
// GetDashboard, so the ecosystem rows add up to that period's totals.
func (h *DashboardHandler) GetBreakdown(c *gin.Context) {
	now := time.Now().UTC()
	ctx := c.Request.Context()
	spec := normalizeWindowRange(c.Query("range"))
	from := now.Add(-spec.duration)
	useRollup := spec.rollup && h.useRollup

	var (
		rows     []breakdownRow
		packages []breakdownPackage
		err      error
	)
	if useRollup {
		rows, err = h.breakdownRowsHourly(ctx, from, now)
	} else {
		rows, err = h.breakdownRowsRaw(ctx, from, now)
	}
	if err != nil {
		dashboardDatabaseError(c, err)
		return
	}
	grain := breakdownGrainExact
	if useRollup {
		grain = breakdownGrainUTCDay
		packages, err = h.breakdownPackagesDaily(ctx, from, now)
	} else {
		packages, err = h.breakdownPackagesRaw(ctx, from, now)
	}
	if err != nil {
		dashboardDatabaseError(c, err)
		return
	}

	byEcosystem, byUpstream := foldBreakdownRows(rows)
	c.JSON(http.StatusOK, gin.H{
		"range": gin.H{
			"key":   spec.key,
			"start": from.Format(time.RFC3339),
			"end":   now.Format(time.RFC3339),
		},
		"by_ecosystem":       byEcosystem,
		"by_upstream":        byUpstream,
		"top_packages":       packages,
		"top_packages_grain": grain,
	})
}

// breakdownRowsRaw groups raw access logs for [from, to). The cache_result
// filter is the one aggWindow uses, so blocked and incomplete requests stay
// out of both the totals and their breakdown.
func (h *DashboardHandler) breakdownRowsRaw(ctx context.Context, from, to time.Time) ([]breakdownRow, error) {
	var rows []breakdownRow
	err := h.db.WithContext(ctx).Model(&db.AccessLog{}).
		Select(`adapter_type, hit, upstream,
			COUNT(*) AS requests,
			COALESCE(SUM(bytes_sent), 0) AS bytes,
			COALESCE(SUM(latency_ms), 0) AS sum_latency,
			COALESCE(SUM(upstream_requests), 0) AS upstream_requests,
			COALESCE(SUM(upstream_bytes), 0) AS upstream_bytes`).
		Where("created_at >= ? AND created_at < ? AND (cache_result IN (?, ?) OR cache_result = '')", from, to, "hit", "miss").
		Group("adapter_type, hit, upstream").
		Scan(&rows).Error
	return rows, err
}

// breakdownRowsHourly is the coarse-range equivalent. Its hour predicate is
// the one aggWindowHourly uses.
func (h *DashboardHandler) breakdownRowsHourly(ctx context.Context, from, to time.Time) ([]breakdownRow, error) {
	var rows []breakdownRow
	from, to = from.UTC(), to.UTC()
	err := h.db.WithContext(ctx).Table("access_log_hourly").
		Select(`adapter_type, hit, upstream,
			COALESCE(SUM(request_count), 0) AS requests,
			COALESCE(SUM(total_bytes), 0) AS bytes,
			COALESCE(SUM(sum_latency_ms), 0) AS sum_latency,
			COALESCE(SUM(upstream_requests), 0) AS upstream_requests,
			COALESCE(SUM(upstream_bytes), 0) AS upstream_bytes`).
		Where(`(date > ? OR (date = ? AND hour >= ?))
			AND (date < ? OR (date = ? AND hour <= ?))`,
			from.Format("2006-01-02"), from.Format("2006-01-02"), from.Hour(),
			to.Format("2006-01-02"), to.Format("2006-01-02"), to.Hour()).
		Group("adapter_type, hit, upstream").
		Scan(&rows).Error
	return rows, err
}

const breakdownPackageOrder = "total_bytes DESC, requests DESC, ecosystem ASC, package_name ASC"

func (h *DashboardHandler) breakdownPackagesRaw(ctx context.Context, from, to time.Time) ([]breakdownPackage, error) {
	packages := make([]breakdownPackage, 0, breakdownPackageLimit)
	err := h.db.WithContext(ctx).Model(&db.AccessLog{}).
		Select(`adapter_type AS ecosystem, package_name,
			COUNT(*) AS requests,
			COALESCE(SUM(bytes_sent), 0) AS total_bytes,
			COALESCE(SUM(CASE WHEN hit = 1 THEN bytes_sent ELSE 0 END), 0) AS hit_bytes`).
		Where("created_at >= ? AND created_at < ? AND (cache_result IN (?, ?) OR cache_result = '') AND package_name != ''", from, to, "hit", "miss").
		Group("adapter_type, package_name").
		Order(breakdownPackageOrder).
		Limit(breakdownPackageLimit).
		Scan(&packages).Error
	return packages, err
}

func (h *DashboardHandler) breakdownPackagesDaily(ctx context.Context, from, to time.Time) ([]breakdownPackage, error) {
	packages := make([]breakdownPackage, 0, breakdownPackageLimit)
	err := h.db.WithContext(ctx).Table("access_log_package_daily").
		Select(`adapter_type AS ecosystem, package_name,
			COALESCE(SUM(request_count), 0) AS requests,
			COALESCE(SUM(total_bytes), 0) AS total_bytes,
			COALESCE(SUM(CASE WHEN hit = 1 THEN total_bytes ELSE 0 END), 0) AS hit_bytes`).
		Where("date >= ? AND date <= ? AND package_name != ''",
			from.UTC().Format("2006-01-02"), to.UTC().Format("2006-01-02")).
		Group("adapter_type, package_name").
		Order(breakdownPackageOrder).
		Limit(breakdownPackageLimit).
		Scan(&packages).Error
	return packages, err
}

// foldBreakdownRows turns (ecosystem, hit, upstream) cells into the two
// lists. Averages are sums over counts, never averages of averages.
func foldBreakdownRows(rows []breakdownRow) ([]breakdownEcosystem, []breakdownUpstream) {
	type ecosystemSums struct {
		breakdownEcosystem
		sumHitLatency  int64
		sumMissLatency int64
	}
	type upstreamKey struct{ ecosystem, upstream string }
	type upstreamSums struct {
		breakdownUpstream
		sumLatency int64
	}

	ecosystems := make(map[string]*ecosystemSums)
	upstreams := make(map[upstreamKey]*upstreamSums)
	for _, row := range rows {
		eco := ecosystems[row.AdapterType]
		if eco == nil {
			eco = &ecosystemSums{breakdownEcosystem: breakdownEcosystem{Ecosystem: row.AdapterType}}
			ecosystems[row.AdapterType] = eco
		}
		if row.Hit {
			eco.HitRequests += row.Requests
			eco.HitBytes += row.Bytes
			eco.sumHitLatency += row.SumLatency
		} else {
			eco.MissRequests += row.Requests
			eco.MissBytes += row.Bytes
			eco.sumMissLatency += row.SumLatency
		}
		// A row without an Upstream name selected none (for example a
		// negative-cache reply); it counts for its ecosystem only.
		if row.Upstream == "" {
			continue
		}
		key := upstreamKey{row.AdapterType, row.Upstream}
		up := upstreams[key]
		if up == nil {
			up = &upstreamSums{breakdownUpstream: breakdownUpstream{Ecosystem: row.AdapterType, Upstream: row.Upstream}}
			upstreams[key] = up
		}
		// A hit can still reach its Upstream (a revalidation), so measured
		// exchanges are taken from every row; the miss figures from misses.
		up.UpstreamRequests += row.UpstreamRequests
		up.UpstreamBytes += row.UpstreamBytes
		if !row.Hit {
			up.MissRequests += row.Requests
			up.MissBytes += row.Bytes
			up.sumLatency += row.SumLatency
		}
	}

	byEcosystem := make([]breakdownEcosystem, 0, len(ecosystems))
	for _, eco := range ecosystems {
		if eco.HitRequests > 0 {
			eco.AvgHitLatencyMs = float64(eco.sumHitLatency) / float64(eco.HitRequests)
		}
		if eco.MissRequests > 0 {
			eco.AvgMissLatencyMs = float64(eco.sumMissLatency) / float64(eco.MissRequests)
		}
		byEcosystem = append(byEcosystem, eco.breakdownEcosystem)
	}
	sort.Slice(byEcosystem, func(i, j int) bool {
		left := byEcosystem[i].HitBytes + byEcosystem[i].MissBytes
		right := byEcosystem[j].HitBytes + byEcosystem[j].MissBytes
		if left != right {
			return left > right
		}
		return byEcosystem[i].Ecosystem < byEcosystem[j].Ecosystem
	})

	byUpstream := make([]breakdownUpstream, 0, len(upstreams))
	for _, up := range upstreams {
		if up.MissRequests > 0 {
			up.AvgMissLatencyMs = float64(up.sumLatency) / float64(up.MissRequests)
		}
		if up.UpstreamRequests == 0 && up.MissRequests == 0 {
			continue
		}
		byUpstream = append(byUpstream, up.breakdownUpstream)
	}
	sort.Slice(byUpstream, func(i, j int) bool {
		if byUpstream[i].UpstreamBytes != byUpstream[j].UpstreamBytes {
			return byUpstream[i].UpstreamBytes > byUpstream[j].UpstreamBytes
		}
		if byUpstream[i].MissBytes != byUpstream[j].MissBytes {
			return byUpstream[i].MissBytes > byUpstream[j].MissBytes
		}
		if byUpstream[i].Ecosystem != byUpstream[j].Ecosystem {
			return byUpstream[i].Ecosystem < byUpstream[j].Ecosystem
		}
		return byUpstream[i].Upstream < byUpstream[j].Upstream
	})
	if len(byUpstream) > breakdownUpstreamLimit {
		byUpstream = byUpstream[:breakdownUpstreamLimit]
	}
	return byEcosystem, byUpstream
}
