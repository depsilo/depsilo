package accesslog

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// RetentionConfig — both values are number-of-days. Zero means
// "never sweep that table" so an operator can opt-in incrementally.
type RetentionConfig struct {
	RawDays    int
	RollupDays int
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
	if cfg.RollupDays > 0 {
		cutoffDate := time.Now().UTC().AddDate(0, 0, -cfg.RollupDays).Format("2006-01-02")
		for _, table := range []string{
			"access_log_hourly",
			"access_log_daily",
			"access_log_package_daily",
		} {
			if err := gdb.WithContext(ctx).
				Exec("DELETE FROM "+table+" WHERE date < ?", cutoffDate).Error; err != nil {
				zap.L().Warn("retention: rollup delete failed",
					zap.String("table", table),
					zap.Error(err))
			}
		}
	}
}
