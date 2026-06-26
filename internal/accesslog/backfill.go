package accesslog

import (
	"context"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

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
