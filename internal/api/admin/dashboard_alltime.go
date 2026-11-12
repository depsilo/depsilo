package admin

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/accesslog"
	"depsilo/internal/db"
)

// hasArchive reports whether any expired rollup day has been folded into the
// archive, i.e. whether the all-time totals include days the trend omits.
func (h *DashboardHandler) hasArchive(ctx context.Context) (bool, error) {
	if !h.useRollup {
		return false, nil
	}
	var count int64
	if err := h.db.WithContext(ctx).Table("access_log_archive").Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// allTimeKey is the Overview range that covers everything the instance still
// knows: the access log archive (rollup days retention has expired) plus the
// retained rollups. Without rollups it is the retained raw log, which the raw
// retention horizon bounds; the response's range start says where coverage
// begins either way, so the UI never claims more history than exists.
const allTimeKey = "all"

// Trend buckets for the all-time range: one per UTC day, or one per seven days
// once a day-grained series would exceed this many points.
const allTimeMaxDailyBuckets = 731

// allTimeStart is the earliest instant any retained or archived statistic
// covers. ok is false when nothing has been recorded.
func (h *DashboardHandler) allTimeStart(ctx context.Context) (time.Time, bool, error) {
	database := h.db.WithContext(ctx)
	var candidates []time.Time
	if h.useRollup {
		var archived db.ControlPlaneState
		err := database.Where("key = ?", db.AccessLogArchiveFirstDateKey).First(&archived).Error
		switch {
		case err == nil:
			if hour, ok := accesslog.ParseArchiveFirstHour(archived.Value); ok {
				candidates = append(candidates, hour)
			}
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return time.Time{}, false, err
		}
		var first struct {
			Date *string
			Hour *int
		}
		if err := database.Raw(`SELECT date, MIN(hour) AS hour FROM access_log_hourly
			WHERE date = (SELECT MIN(date) FROM access_log_hourly) GROUP BY date`).Scan(&first).Error; err != nil {
			return time.Time{}, false, err
		}
		if first.Date != nil && first.Hour != nil {
			if day, parseErr := time.Parse("2006-01-02", *first.Date); parseErr == nil {
				candidates = append(candidates, day.UTC().Add(time.Duration(*first.Hour)*time.Hour))
			}
		}
	} else {
		// Read the row rather than MIN(created_at): SQLite returns an aggregate
		// over a time column as text, which does not scan into time.Time.
		var first []db.AccessLog
		if err := database.Select("created_at").
			Where("cache_result IN (?, ?) OR cache_result = ''", "hit", "miss").
			Order("created_at ASC").Limit(1).Find(&first).Error; err != nil {
			return time.Time{}, false, err
		}
		if len(first) == 1 && !first[0].CreatedAt.IsZero() {
			candidates = append(candidates, first[0].CreatedAt.UTC())
		}
	}
	if len(candidates) == 0 {
		return time.Time{}, false, nil
	}
	earliest := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.Before(earliest) {
			earliest = candidate
		}
	}
	return earliest, true, nil
}

// aggAllTime is the all-time counterpart of aggWindow: archive plus every
// retained rollup hour, or the whole retained raw log without rollups.
func (h *DashboardHandler) aggAllTime(ctx context.Context, now time.Time) (aggSnapshot, error) {
	if !h.useRollup {
		return h.aggWindow(ctx, time.Unix(0, 0).UTC(), now)
	}
	const selectSums = `COALESCE(SUM(request_count), 0) AS total,
			COALESCE(SUM(CASE WHEN hit = 1 THEN request_count ELSE 0 END), 0) AS hits,
			COALESCE(SUM(total_bytes), 0) AS bytes,
			COALESCE(SUM(upstream_requests), 0) AS upstream_requests,
			COALESCE(SUM(upstream_bytes), 0) AS upstream_bytes,
			COALESCE(SUM(sum_latency_ms), 0) AS sum_latency,
			COALESCE(SUM(CASE WHEN hit = 1 THEN sum_latency_ms ELSE 0 END), 0) AS sum_hit_latency,
			COALESCE(SUM(CASE WHEN hit = 0 THEN sum_latency_ms ELSE 0 END), 0) AS sum_miss_latency,
			COALESCE(SUM(CASE WHEN hit = 1 THEN total_bytes ELSE 0 END), 0) AS hit_bytes,
			COALESCE(SUM(CASE WHEN hit = 0 THEN total_bytes ELSE 0 END), 0) AS miss_bytes,
			COALESCE(SUM(error_count), 0) AS errors`
	var retained, archived aggSnapshot
	if err := h.db.WithContext(ctx).Table("access_log_hourly").Select(selectSums).Scan(&retained).Error; err != nil {
		return aggSnapshot{}, err
	}
	if err := h.db.WithContext(ctx).Table("access_log_archive").Select(selectSums).Scan(&archived).Error; err != nil {
		return aggSnapshot{}, err
	}
	return retained.plus(archived), nil
}

func (s aggSnapshot) plus(o aggSnapshot) aggSnapshot {
	return aggSnapshot{
		Total:            s.Total + o.Total,
		Hits:             s.Hits + o.Hits,
		Bytes:            s.Bytes + o.Bytes,
		UpstreamRequests: s.UpstreamRequests + o.UpstreamRequests,
		UpstreamBytes:    s.UpstreamBytes + o.UpstreamBytes,
		SumLatency:       s.SumLatency + o.SumLatency,
		SumHitLatency:    s.SumHitLatency + o.SumHitLatency,
		SumMissLatency:   s.SumMissLatency + o.SumMissLatency,
		HitBytes:         s.HitBytes + o.HitBytes,
		MissBytes:        s.MissBytes + o.MissBytes,
		Errors:           s.Errors + o.Errors,
	}
}

// allTimeTrendSpec sizes a UTC-aligned series from the coverage start to now.
// Archived days have no dated rows, so the series begins with the oldest
// retained rollup day, not the archive.
func allTimeTrendSpec(start time.Time, ok bool, now time.Time) trendSpec {
	interval := 24 * time.Hour
	if !ok || start.After(now) {
		return trendSpec{buckets: 1, interval: interval}
	}
	// Count on the Unix-epoch grid that makeTrendWindow and the SQL use.
	count := func(step time.Duration) int {
		sec := int64(step / time.Second)
		return int((now.Unix()/sec)-(start.Unix()/sec)) + 1
	}
	days := count(interval)
	if days > allTimeMaxDailyBuckets {
		interval = 7 * 24 * time.Hour
		days = count(interval)
	}
	return trendSpec{buckets: days, interval: interval}
}

// retainedTrendStart is where dated trend rows begin: the oldest retained
// rollup hour, or the oldest raw row without rollups.
func (h *DashboardHandler) retainedTrendStart(ctx context.Context) (time.Time, bool, error) {
	if !h.useRollup {
		return h.allTimeStart(ctx)
	}
	var first *string
	if err := h.db.WithContext(ctx).Raw("SELECT MIN(date) FROM access_log_hourly").Scan(&first).Error; err != nil {
		return time.Time{}, false, err
	}
	if first == nil || *first == "" {
		return time.Time{}, false, nil
	}
	day, err := time.Parse("2006-01-02", *first)
	if err != nil {
		return time.Time{}, false, nil
	}
	return day.UTC(), true, nil
}

func (h *DashboardHandler) trendsAllTime(ctx context.Context, now time.Time) ([]trendPoint, error) {
	start, ok, err := h.retainedTrendStart(ctx)
	if err != nil {
		return nil, err
	}
	window := makeTrendWindow(now, allTimeTrendSpec(start, ok, now))
	if !h.useRollup {
		return h.trendsRawWindow(ctx, window)
	}
	return h.trendsHourlyGroupedWindow(ctx, window)
}

// breakdownRowsArchive returns the archive in the same cell shape as the
// dated breakdown queries.
func (h *DashboardHandler) breakdownRowsArchive(ctx context.Context) ([]breakdownRow, error) {
	var rows []breakdownRow
	err := h.db.WithContext(ctx).Table("access_log_archive").
		Select(`adapter_type, hit, upstream,
			request_count AS requests,
			total_bytes AS bytes,
			sum_latency_ms AS sum_latency,
			upstream_requests,
			upstream_bytes`).
		Scan(&rows).Error
	return rows, err
}

// breakdownPackagesAllTime ranks packages over the archive and every retained
// package day together.
func (h *DashboardHandler) breakdownPackagesAllTime(ctx context.Context) ([]breakdownPackage, error) {
	packages := make([]breakdownPackage, 0, breakdownPackageLimit)
	err := h.db.WithContext(ctx).Raw(`
SELECT adapter_type AS ecosystem, package_name,
       COALESCE(SUM(request_count), 0) AS requests,
       COALESCE(SUM(total_bytes), 0) AS total_bytes,
       COALESCE(SUM(CASE WHEN hit = 1 THEN total_bytes ELSE 0 END), 0) AS hit_bytes
FROM (
    SELECT adapter_type, package_name, hit, request_count, total_bytes FROM access_log_package_daily
    UNION ALL
    SELECT adapter_type, package_name, hit, request_count, total_bytes FROM access_log_package_archive
) AS all_packages
WHERE package_name != ''
GROUP BY adapter_type, package_name
ORDER BY `+breakdownPackageOrder+`
LIMIT ?`, breakdownPackageLimit).Scan(&packages).Error
	return packages, err
}
