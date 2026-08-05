package server

import (
	"context"
	"time"

	"depsilo/internal/api"
	"depsilo/internal/cache"
	"go.uber.org/zap"
)

func runCacheMetrics(ctx context.Context, manager *cache.Manager, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	update := func() {
		stats, err := manager.Stats(ctx)
		if err != nil {
			zap.L().Warn("failed to update package-cache metrics", zap.Error(err))
			return
		}
		api.M.CacheSizeBytes.Set(float64(stats.SizeBytes))
		api.M.CacheFilesTotal.Set(float64(stats.Entries))
	}
	update()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			update()
		}
	}
}
