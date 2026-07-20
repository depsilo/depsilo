package cache

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
)

// StartLRUCleanup periodically asks Retention to enforce its capacity policy.
// Retention owns candidate selection, mutation serialization and failure
// aggregation; this function owns scheduling and operational logging only.
func StartLRUCleanup(ctx context.Context, retention *Retention, interval time.Duration) {
	if ctx == nil {
		ctx = context.Background()
	}
	if retention == nil {
		zap.L().Error("LRU cleanup is unavailable", zap.Error(errors.New("nil cache retention")))
		return
	}
	if interval <= 0 {
		zap.L().Error("LRU cleanup is unavailable", zap.Duration("interval", interval))
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	zap.L().Info("LRU cleanup started",
		zap.Int64("max_bytes", retention.policy.MaxBytes),
		zap.Int("threshold_percent", retention.policy.ThresholdPercent),
		zap.Int("target_percent", retention.policy.TargetPercent),
		zap.Duration("interval", interval),
	)

	for {
		select {
		case <-ctx.Done():
			zap.L().Info("LRU cleanup stopped")
			return
		case <-ticker.C:
			report, err := retention.Reclaim(ctx, ReclaimModeCapacity)
			if err != nil && ctx.Err() == nil {
				zap.L().Warn("LRU cleanup incomplete",
					zap.Int("removed", report.Removed),
					zap.Int("failed", report.Failed),
					zap.Int64("reclaimed_bytes", report.ReclaimedBytes),
					zap.Error(err),
				)
			}
		}
	}
}
