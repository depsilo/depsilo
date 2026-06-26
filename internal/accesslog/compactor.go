package accesslog

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// StartCompactor runs forever (until ctx cancellation), rolling each
// finished UTC day's access_log_hourly rows into a single
// access_log_daily row at ~UTC 00:05 every day. Running 5 minutes past
// midnight gives the last flush of the previous day room to land.
//
// At process start it also runs once for "yesterday" so a node that
// missed a midnight tick (down for maintenance / clock skew) catches up
// without an operator nudge.
func StartCompactor(ctx context.Context, gdb *gorm.DB) {
	runCompactOnce(ctx, gdb)
	for {
		next := nextCompactRunUTC(time.Now().UTC())
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
			runCompactOnce(ctx, gdb)
		}
	}
}

// nextCompactRunUTC returns the next UTC 00:05 timestamp strictly after now.
func nextCompactRunUTC(now time.Time) time.Time {
	tomorrow := now.Add(24 * time.Hour)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 5, 0, 0, time.UTC)
}

func runCompactOnce(ctx context.Context, gdb *gorm.DB) {
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	if err := compactDate(ctx, gdb, yesterday); err != nil {
		zap.L().Warn("compactor: failed", zap.String("date", yesterday), zap.Error(err))
		return
	}
	zap.L().Info("compactor: done", zap.String("date", yesterday))
}

// compactDate is intentionally idempotent — the DO UPDATE clause replaces
// the daily row rather than adding to it. Re-running the compactor for an
// already-compacted day is a no-op (modulo updated_at).
func compactDate(ctx context.Context, gdb *gorm.DB, date string) error {
	sql := `
INSERT INTO access_log_daily
    (date, adapter_type, hit, upstream,
     request_count, total_bytes, sum_latency_ms, error_count, updated_at)
SELECT date, adapter_type, hit, COALESCE(upstream, '') AS upstream,
       SUM(request_count), SUM(total_bytes),
       SUM(sum_latency_ms), SUM(error_count), ?
FROM access_log_hourly
WHERE date = ?
GROUP BY date, adapter_type, hit, upstream
ON CONFLICT(date, adapter_type, hit, upstream) DO UPDATE SET
    request_count  = excluded.request_count,
    total_bytes    = excluded.total_bytes,
    sum_latency_ms = excluded.sum_latency_ms,
    error_count    = excluded.error_count,
    updated_at     = excluded.updated_at
`
	return gdb.WithContext(ctx).Exec(sql, time.Now().UTC(), date).Error
}
