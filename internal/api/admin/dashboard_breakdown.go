package admin

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/api/contract"
	"depsilo/internal/db"
)

// Breakdown list bounds. Ecosystems are a closed set, so they are not capped.
const (
	breakdownUpstreamLimit = 20
	breakdownPackageLimit  = 10
)

// Package ranking granularity: the package rollup is day-grained, so a coarse
// range ranks whole UTC days and can include requests from before the range
// start on its first day. The response names the granularity
// (contract.DashboardBreakdownGrain) so a client never presents that ranking
// as an exact window.
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
		packages []contract.DashboardBreakdownPackage
		err      error
	)
	allTime := spec.key == allTimeKey
	if allTime {
		start, ok, startErr := h.allTimeStart(ctx)
		if startErr != nil {
			dashboardDatabaseError(c, startErr)
			return
		}
		from = now
		if ok && start.Before(now) {
			from = start
		}
	}
	switch {
	case allTime && useRollup:
		rows, err = h.breakdownRowsHourly(ctx, time.Unix(0, 0).UTC(), now)
		if err == nil {
			var archived []breakdownRow
			archived, err = h.breakdownRowsArchive(ctx)
			rows = append(rows, archived...)
		}
	case useRollup:
		rows, err = h.breakdownRowsHourly(ctx, from, now)
	case allTime:
		rows, err = h.breakdownRowsRaw(ctx, time.Unix(0, 0).UTC(), now)
	default:
		rows, err = h.breakdownRowsRaw(ctx, from, now)
	}
	if err != nil {
		dashboardDatabaseError(c, err)
		return
	}
	grain := contract.DashboardBreakdownGrainExact
	switch {
	case allTime && useRollup:
		grain = contract.DashboardBreakdownGrainUTCDay
		packages, err = h.breakdownPackagesAllTime(ctx)
	case useRollup:
		grain = contract.DashboardBreakdownGrainUTCDay
		packages, err = h.breakdownPackagesDaily(ctx, from, now)
	case allTime:
		packages, err = h.breakdownPackagesRaw(ctx, time.Unix(0, 0).UTC(), now)
	default:
		packages, err = h.breakdownPackagesRaw(ctx, from, now)
	}
	if err != nil {
		dashboardDatabaseError(c, err)
		return
	}

	byEcosystem, byUpstream := foldBreakdownRows(rows)
	c.JSON(http.StatusOK, contract.DashboardBreakdownResponse{
		Range: contract.DashboardRangeSpan{
			Key:   contract.DashboardRange(spec.key),
			Start: from.Format(time.RFC3339),
			End:   now.Format(time.RFC3339),
		},
		ByEcosystem:      byEcosystem,
		ByUpstream:       byUpstream,
		TopPackages:      packages,
		TopPackagesGrain: grain,
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

func (h *DashboardHandler) breakdownPackagesRaw(ctx context.Context, from, to time.Time) ([]contract.DashboardBreakdownPackage, error) {
	packages := make([]contract.DashboardBreakdownPackage, 0, breakdownPackageLimit)
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

func (h *DashboardHandler) breakdownPackagesDaily(ctx context.Context, from, to time.Time) ([]contract.DashboardBreakdownPackage, error) {
	packages := make([]contract.DashboardBreakdownPackage, 0, breakdownPackageLimit)
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
func foldBreakdownRows(rows []breakdownRow) ([]contract.DashboardBreakdownEcosystem, []contract.DashboardBreakdownUpstream) {
	type ecosystemSums struct {
		contract.DashboardBreakdownEcosystem
		sumHitLatency  int64
		sumMissLatency int64
	}
	type upstreamKey struct{ ecosystem, upstream string }
	type upstreamSums struct {
		contract.DashboardBreakdownUpstream
		sumLatency int64
	}

	ecosystems := make(map[string]*ecosystemSums)
	upstreams := make(map[upstreamKey]*upstreamSums)
	for _, row := range rows {
		eco := ecosystems[row.AdapterType]
		if eco == nil {
			eco = &ecosystemSums{DashboardBreakdownEcosystem: contract.DashboardBreakdownEcosystem{Ecosystem: row.AdapterType}}
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
			up = &upstreamSums{DashboardBreakdownUpstream: contract.DashboardBreakdownUpstream{Ecosystem: row.AdapterType, Upstream: row.Upstream}}
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

	byEcosystem := make([]contract.DashboardBreakdownEcosystem, 0, len(ecosystems))
	for _, eco := range ecosystems {
		if eco.HitRequests > 0 {
			eco.AvgHitLatencyMs = float64(eco.sumHitLatency) / float64(eco.HitRequests)
		}
		if eco.MissRequests > 0 {
			eco.AvgMissLatencyMs = float64(eco.sumMissLatency) / float64(eco.MissRequests)
		}
		byEcosystem = append(byEcosystem, eco.DashboardBreakdownEcosystem)
	}
	sort.Slice(byEcosystem, func(i, j int) bool {
		left := byEcosystem[i].HitBytes + byEcosystem[i].MissBytes
		right := byEcosystem[j].HitBytes + byEcosystem[j].MissBytes
		if left != right {
			return left > right
		}
		return byEcosystem[i].Ecosystem < byEcosystem[j].Ecosystem
	})

	byUpstream := make([]contract.DashboardBreakdownUpstream, 0, len(upstreams))
	for _, up := range upstreams {
		if up.MissRequests > 0 {
			up.AvgMissLatencyMs = float64(up.sumLatency) / float64(up.MissRequests)
		}
		if up.UpstreamRequests == 0 && up.MissRequests == 0 {
			continue
		}
		byUpstream = append(byUpstream, up.DashboardBreakdownUpstream)
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
