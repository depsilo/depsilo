package accesslog

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/db"
)

// upsertHourly accumulates the in-memory hourly bucket map into
// access_log_hourly via INSERT ... ON CONFLICT DO UPDATE. The DO UPDATE
// clause adds the new value to the existing value so two consecutive
// flushes for the same bucket compose correctly. excluded.X is the
// SQLite/Postgres idiom for "the value the INSERT tried to write".
//
// glebarez/sqlite carries SQLite >= 3.24, which is when ON CONFLICT
// landed; no extra dialect handling is needed for the install targets
// this project supports today.
func upsertHourly(ctx context.Context, gdb *gorm.DB, m map[hourlyKey]*counters) error {
	if len(m) == 0 {
		return nil
	}
	rows := make([]db.AccessLogHourly, 0, len(m))
	now := time.Now().UTC()
	for k, c := range m {
		rows = append(rows, db.AccessLogHourly{
			Date:         k.Date,
			Hour:         k.Hour,
			AdapterType:  k.AdapterType,
			Hit:          k.Hit,
			Upstream:     k.Upstream,
			RequestCount: c.RequestCount,
			TotalBytes:   c.TotalBytes,
			SumLatencyMs: c.SumLatencyMs,
			ErrorCount:   c.ErrorCount,
			UpdatedAt:    now,
		})
	}
	return gdb.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "date"}, {Name: "hour"}, {Name: "adapter_type"},
			{Name: "hit"}, {Name: "upstream"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"request_count":  gorm.Expr("access_log_hourly.request_count + excluded.request_count"),
			"total_bytes":    gorm.Expr("access_log_hourly.total_bytes + excluded.total_bytes"),
			"sum_latency_ms": gorm.Expr("access_log_hourly.sum_latency_ms + excluded.sum_latency_ms"),
			"error_count":    gorm.Expr("access_log_hourly.error_count + excluded.error_count"),
			"updated_at":     now,
		}),
	}).CreateInBatches(rows, 200).Error
}

func upsertPackageDaily(ctx context.Context, gdb *gorm.DB, m map[pkgDailyKey]*counters) error {
	if len(m) == 0 {
		return nil
	}
	rows := make([]db.AccessLogPackageDaily, 0, len(m))
	now := time.Now().UTC()
	for k, c := range m {
		rows = append(rows, db.AccessLogPackageDaily{
			Date:         k.Date,
			AdapterType:  k.AdapterType,
			PackageName:  k.PackageName,
			Hit:          k.Hit,
			RequestCount: c.RequestCount,
			TotalBytes:   c.TotalBytes,
			UpdatedAt:    now,
		})
	}
	return gdb.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "date"}, {Name: "adapter_type"},
			{Name: "package_name"}, {Name: "hit"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"request_count": gorm.Expr("access_log_package_daily.request_count + excluded.request_count"),
			"total_bytes":   gorm.Expr("access_log_package_daily.total_bytes + excluded.total_bytes"),
			"updated_at":    now,
		}),
	}).CreateInBatches(rows, 200).Error
}
