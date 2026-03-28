package cache

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"repocache/internal/db"
)

// StartLRUCleanup runs a background goroutine that periodically checks cache usage
// and evicts least-recently-accessed entries when usage exceeds the threshold.
func StartLRUCleanup(ctx context.Context, storage Storage, database *gorm.DB, maxSizeGB int, thresholdPct int, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	maxBytes := int64(maxSizeGB) * 1024 * 1024 * 1024
	threshold := int64(float64(maxBytes) * float64(thresholdPct) / 100.0)

	zap.L().Info("LRU cleanup started",
		zap.Int("max_size_gb", maxSizeGB),
		zap.Int("threshold_pct", thresholdPct),
		zap.Duration("interval", interval),
	)

	for {
		select {
		case <-ctx.Done():
			zap.L().Info("LRU cleanup stopped")
			return
		case <-ticker.C:
			runLRU(ctx, storage, database, maxBytes, threshold)
		}
	}
}

func runLRU(ctx context.Context, storage Storage, database *gorm.DB, maxBytes, threshold int64) {
	// Check current usage
	totalSize, err := storage.TotalSize(ctx)
	if err != nil {
		zap.L().Warn("LRU: failed to get total size", zap.Error(err))
		return
	}

	if totalSize < threshold {
		return
	}

	zap.L().Info("LRU: usage above threshold, starting eviction",
		zap.Int64("total_bytes", totalSize),
		zap.Int64("threshold_bytes", threshold),
	)

	// Also clean expired entries first
	var expired []db.CacheEntry
	database.Where("expires_at < ?", time.Now()).Find(&expired)
	for _, entry := range expired {
		if err := storage.Delete(ctx, entry.StoragePath); err != nil {
			zap.L().Warn("LRU: failed to delete expired file", zap.String("key", entry.Key), zap.Error(err))
		}
		database.Delete(&entry)
	}
	if len(expired) > 0 {
		zap.L().Info("LRU: cleaned expired entries", zap.Int("count", len(expired)))
	}

	// Re-check after expiry cleanup
	totalSize, err = storage.TotalSize(ctx)
	if err != nil || totalSize < threshold {
		return
	}

	// Evict LRU entries until below 80% of max
	target := int64(float64(maxBytes) * 0.8)
	var entries []db.CacheEntry
	database.Order("last_accessed ASC").Find(&entries)

	evicted := 0
	for _, entry := range entries {
		if totalSize <= target {
			break
		}

		if err := storage.Delete(ctx, entry.StoragePath); err != nil {
			zap.L().Warn("LRU: failed to delete file", zap.String("key", entry.Key), zap.Error(err))
			continue
		}
		database.Delete(&entry)
		totalSize -= entry.Size
		evicted++
	}

	if evicted > 0 {
		zap.L().Info("LRU: eviction completed",
			zap.Int("evicted", evicted),
			zap.Int64("new_total_bytes", totalSize),
		)
	}
}
