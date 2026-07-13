package accesslog

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// FiveMinuteBackfillMarker is the transactional completion contract used to
// decide when five-minute history is authoritative for dashboard queries.
const FiveMinuteBackfillMarker = "access_log_five_minutely_v1"

// InvalidateFiveMinuteBackfill revokes fine-history readiness so the next
// BackfillFiveMinutely call rebuilds the trailing window from raw logs.
func InvalidateFiveMinuteBackfill(ctx context.Context, gdb *gorm.DB) error {
	return gdb.WithContext(ctx).
		Where("key = ?", FiveMinuteBackfillMarker).
		Delete(&db.ControlPlaneState{}).Error
}

// BackfillIfEmpty fills the three rollup tables from the existing
// access_logs table whenever access_log_hourly looks empty. It's safe
// to call repeatedly — the emptiness check short-circuits subsequent
// runs, and the underlying INSERT...SELECT statements use ON CONFLICT
// for idempotency.
//
// SQL is SQLite-specific (strftime). When/if Postgres support lands
// this needs a dialect split — flagged in spec §10 #6 and ADR-0002.
func BackfillIfEmpty(ctx context.Context, gdb *gorm.DB) error {
	var hourlyCount int64
	if err := gdb.WithContext(ctx).Model(&db.AccessLogHourly{}).Count(&hourlyCount).Error; err != nil {
		return err
	}
	if hourlyCount > 0 {
		zap.L().Info("rollup tables already populated, skipping backfill")
		return nil
	}
	zap.L().Info("backfilling rollup tables from access_logs")
	if err := backfillHourly(ctx, gdb); err != nil {
		return err
	}
	if err := backfillDaily(ctx, gdb); err != nil {
		return err
	}
	if err := backfillPackageDaily(ctx, gdb); err != nil {
		return err
	}
	zap.L().Info("rollup backfill complete")
	return nil
}

// BackfillFiveMinutely rebuilds the trailing seven days of five-minute
// history once. The marker and rebuilt rows commit atomically so a failed
// first attempt can be retried safely on the next startup.
func BackfillFiveMinutely(ctx context.Context, gdb *gorm.DB, now time.Time) error {
	var markerCount int64
	if err := gdb.WithContext(ctx).
		Model(&db.ControlPlaneState{}).
		Where("key = ?", FiveMinuteBackfillMarker).
		Count(&markerCount).Error; err != nil {
		return err
	}
	if markerCount > 0 {
		return nil
	}

	cutoff := now.UTC().Add(-7 * 24 * time.Hour)
	cutoffBucket := (cutoff.Unix() / 300) * 300
	return gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("bucket_start >= ?", cutoffBucket).
			Delete(&db.AccessLogFiveMinutely{}).Error; err != nil {
			return err
		}

		const sql = `
INSERT INTO access_log_five_minutely
  (bucket_start, adapter_type, hit, upstream, request_count, total_bytes, sum_latency_ms, error_count, updated_at)
SELECT
  (CAST(strftime('%s', created_at) AS INTEGER) / 300) * 300,
  adapter_type, hit, COALESCE(upstream, ''), COUNT(*),
  COALESCE(SUM(bytes_sent), 0), COALESCE(SUM(latency_ms), 0),
  COALESCE(SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END), 0), datetime('now')
FROM access_logs
WHERE created_at >= ?
GROUP BY 1, adapter_type, hit, COALESCE(upstream, '')`
		if err := tx.Exec(sql, cutoff).Error; err != nil {
			return err
		}

		return tx.Save(&db.ControlPlaneState{
			Key:   FiveMinuteBackfillMarker,
			Value: "true",
		}).Error
	})
}

func backfillHourly(ctx context.Context, gdb *gorm.DB) error {
	sql := `
INSERT INTO access_log_hourly
    (date, hour, adapter_type, hit, upstream,
     request_count, total_bytes, sum_latency_ms, error_count, updated_at)
SELECT
    strftime('%Y-%m-%d', created_at) AS date,
    CAST(strftime('%H', created_at) AS INTEGER) AS hour,
    adapter_type,
    hit,
    COALESCE(upstream, '') AS upstream,
    COUNT(*),
    COALESCE(SUM(bytes_sent), 0),
    COALESCE(SUM(latency_ms), 0),
    SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END),
    datetime('now')
FROM access_logs
GROUP BY date, hour, adapter_type, hit, upstream
ON CONFLICT(date, hour, adapter_type, hit, upstream) DO UPDATE SET
    request_count  = excluded.request_count,
    total_bytes    = excluded.total_bytes,
    sum_latency_ms = excluded.sum_latency_ms,
    error_count    = excluded.error_count,
    updated_at     = excluded.updated_at
`
	return gdb.WithContext(ctx).Exec(sql).Error
}

func backfillDaily(ctx context.Context, gdb *gorm.DB) error {
	sql := `
INSERT INTO access_log_daily
    (date, adapter_type, hit, upstream,
     request_count, total_bytes, sum_latency_ms, error_count, updated_at)
SELECT
    strftime('%Y-%m-%d', created_at) AS date,
    adapter_type,
    hit,
    COALESCE(upstream, '') AS upstream,
    COUNT(*),
    COALESCE(SUM(bytes_sent), 0),
    COALESCE(SUM(latency_ms), 0),
    SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END),
    datetime('now')
FROM access_logs
GROUP BY date, adapter_type, hit, upstream
ON CONFLICT(date, adapter_type, hit, upstream) DO UPDATE SET
    request_count  = excluded.request_count,
    total_bytes    = excluded.total_bytes,
    sum_latency_ms = excluded.sum_latency_ms,
    error_count    = excluded.error_count,
    updated_at     = excluded.updated_at
`
	return gdb.WithContext(ctx).Exec(sql).Error
}

func backfillPackageDaily(ctx context.Context, gdb *gorm.DB) error {
	sql := `
INSERT INTO access_log_package_daily
    (date, adapter_type, package_name, hit,
     request_count, total_bytes, updated_at)
SELECT
    strftime('%Y-%m-%d', created_at) AS date,
    adapter_type,
    package_name,
    hit,
    COUNT(*),
    COALESCE(SUM(bytes_sent), 0),
    datetime('now')
FROM access_logs
WHERE package_name != ''
GROUP BY date, adapter_type, package_name, hit
ON CONFLICT(date, adapter_type, package_name, hit) DO UPDATE SET
    request_count = excluded.request_count,
    total_bytes   = excluded.total_bytes,
    updated_at    = excluded.updated_at
`
	return gdb.WithContext(ctx).Exec(sql).Error
}
