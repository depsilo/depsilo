package accesslog

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// RetentionConfig values are number-of-days. Zero means
// "never sweep that table" so an operator can opt-in incrementally.
type RetentionConfig struct {
	RawDays        int
	FiveMinuteDays int
	RollupDays     int
}

// StartRetention runs forever until ctx cancellation. Sweeps once on
// boot and then once per hour. Hourly is plenty — these tables grow at
// thousands of rows/day at most, the sweep just needs to keep up.
func StartRetention(ctx context.Context, gdb *gorm.DB, cfg RetentionConfig) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	RunRetention(ctx, gdb, cfg)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			RunRetention(ctx, gdb, cfg)
		}
	}
}

// RunRetention executes one sweep. Exposed so server.go can call it
// synchronously at boot before the background loop starts, and so
// tests can drive it deterministically.
func RunRetention(ctx context.Context, gdb *gorm.DB, cfg RetentionConfig) {
	if cfg.RawDays > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -cfg.RawDays)
		res := gdb.WithContext(ctx).
			Where("created_at < ?", cutoff).
			Delete(&db.AccessLog{})
		if res.Error != nil {
			zap.L().Warn("retention: access_logs delete failed", zap.Error(res.Error))
		} else if res.RowsAffected > 0 {
			zap.L().Info("retention: pruned access_logs",
				zap.Int64("rows", res.RowsAffected),
				zap.Time("before", cutoff))
		}
	}
	if cfg.FiveMinuteDays > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -cfg.FiveMinuteDays)
		res := gdb.WithContext(ctx).
			Where("bucket_start < ?", cutoff.Unix()).
			Delete(&db.AccessLogFiveMinutely{})
		if res.Error != nil {
			zap.L().Warn("retention: access_log_five_minutely delete failed", zap.Error(res.Error))
		} else if res.RowsAffected > 0 {
			zap.L().Info("retention: pruned access_log_five_minutely",
				zap.Int64("rows", res.RowsAffected),
				zap.Time("before", cutoff))
		}
	}
	if cfg.RollupDays > 0 {
		cutoffDate := time.Now().UTC().AddDate(0, 0, -cfg.RollupDays).Format("2006-01-02")
		if err := ArchiveRollupsBefore(ctx, gdb, cutoffDate); err != nil {
			zap.L().Warn("retention: rollup archive failed; expiring days kept for the next sweep",
				zap.String("before", cutoffDate),
				zap.Error(err))
		}
	}
}

// ArchiveRollupsBefore folds every rollup day before cutoffDate (YYYY-MM-DD)
// into the undated archive and deletes those days, all in one transaction. A
// request is therefore counted in exactly one place, so all-time totals stay
// exact after the rollup horizon passes. If any step fails nothing is deleted.
//
// The hourly rollup is the archive's source, not the daily one: the nightly
// compactor fills a day only when the process is up at 00:05 UTC, while every
// recorded request has an hourly row.
func ArchiveRollupsBefore(ctx context.Context, gdb *gorm.DB, cutoffDate string) error {
	return gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var first struct {
			Date *string
			Hour *int
		}
		if err := tx.Raw(`SELECT date, MIN(hour) AS hour FROM access_log_hourly
			WHERE date = (SELECT MIN(date) FROM access_log_hourly WHERE date < ?) GROUP BY date`, cutoffDate).Scan(&first).Error; err != nil {
			return err
		}
		if first.Date != nil && *first.Date != "" && first.Hour != nil {
			now := time.Now().UTC()
			if err := tx.Exec(`
INSERT INTO access_log_archive
    (adapter_type, hit, upstream, request_count, total_bytes, upstream_requests,
     upstream_bytes, sum_latency_ms, error_count, updated_at)
SELECT adapter_type, hit, COALESCE(upstream, ''),
       SUM(request_count), SUM(total_bytes), SUM(upstream_requests),
       SUM(upstream_bytes), SUM(sum_latency_ms), SUM(error_count), ?
FROM access_log_hourly
WHERE date < ?
GROUP BY adapter_type, hit, COALESCE(upstream, '')
ON CONFLICT(adapter_type, hit, upstream) DO UPDATE SET
    request_count     = access_log_archive.request_count + excluded.request_count,
    total_bytes       = access_log_archive.total_bytes + excluded.total_bytes,
    upstream_requests = access_log_archive.upstream_requests + excluded.upstream_requests,
    upstream_bytes    = access_log_archive.upstream_bytes + excluded.upstream_bytes,
    sum_latency_ms    = access_log_archive.sum_latency_ms + excluded.sum_latency_ms,
    error_count       = access_log_archive.error_count + excluded.error_count,
    updated_at        = excluded.updated_at`, now, cutoffDate).Error; err != nil {
				return err
			}
			day, err := time.Parse("2006-01-02", *first.Date)
			if err != nil {
				return err
			}
			if err := recordArchiveFirstHour(tx, day.Add(time.Duration(*first.Hour)*time.Hour)); err != nil {
				return err
			}
		}
		if err := tx.Exec(`
INSERT INTO access_log_package_archive
    (adapter_type, package_name, hit, request_count, total_bytes, updated_at)
SELECT adapter_type, package_name, hit, SUM(request_count), SUM(total_bytes), ?
FROM access_log_package_daily
WHERE date < ?
GROUP BY adapter_type, package_name, hit
ON CONFLICT(adapter_type, package_name, hit) DO UPDATE SET
    request_count = access_log_package_archive.request_count + excluded.request_count,
    total_bytes   = access_log_package_archive.total_bytes + excluded.total_bytes,
    updated_at    = excluded.updated_at`, time.Now().UTC(), cutoffDate).Error; err != nil {
			return err
		}
		for _, table := range []string{"access_log_hourly", "access_log_daily", "access_log_package_daily"} {
			if err := tx.Exec("DELETE FROM "+table+" WHERE date < ?", cutoffDate).Error; err != nil {
				return err
			}
		}
		return recordArchiveThroughDate(tx, cutoffDate)
	})
}

// ArchivedThroughDate returns the date before which every day is archived,
// or "" when nothing has been archived.
func ArchivedThroughDate(ctx context.Context, gdb *gorm.DB) (string, error) {
	var state []db.ControlPlaneState
	if err := gdb.WithContext(ctx).Where("key = ?", db.AccessLogArchiveThroughDateKey).Limit(1).Find(&state).Error; err != nil {
		return "", err
	}
	if len(state) == 0 {
		return "", nil
	}
	return state[0].Value, nil
}

// recordArchiveThroughDate only moves the boundary forward, so lowering the
// rollup horizon later never re-opens an archived day.
func recordArchiveThroughDate(tx *gorm.DB, cutoffDate string) error {
	var existing []db.ControlPlaneState
	if err := tx.Where("key = ?", db.AccessLogArchiveThroughDateKey).Limit(1).Find(&existing).Error; err != nil {
		return err
	}
	if len(existing) == 1 && existing[0].Value >= cutoffDate {
		return nil
	}
	return tx.Save(&db.ControlPlaneState{Key: db.AccessLogArchiveThroughDateKey, Value: cutoffDate}).Error
}

// recordArchiveFirstHour keeps the earliest hour ever folded into the archive.
func recordArchiveFirstHour(tx *gorm.DB, hour time.Time) error {
	var existing []db.ControlPlaneState
	if err := tx.Where("key = ?", db.AccessLogArchiveFirstDateKey).Limit(1).Find(&existing).Error; err != nil {
		return err
	}
	if len(existing) == 1 {
		if previous, ok := ParseArchiveFirstHour(existing[0].Value); ok && !hour.Before(previous) {
			return nil
		}
	}
	return tx.Save(&db.ControlPlaneState{Key: db.AccessLogArchiveFirstDateKey, Value: hour.UTC().Format(time.RFC3339)}).Error
}

// ParseArchiveFirstHour reads the archive's first-hour marker.
func ParseArchiveFirstHour(value string) (time.Time, bool) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), true
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.UTC(), true
	}
	return time.Time{}, false
}
