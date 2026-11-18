package cache

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

type hitCountUpdate struct {
	entryID      uint
	lastAccessed time.Time
}

func (m *Manager) recordHit(entryID uint, lastAccessed time.Time) bool {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.lifecycleClosed {
		return false
	}
	select {
	case m.hitUpdates <- hitCountUpdate{entryID: entryID, lastAccessed: lastAccessed}:
		return true
	default:
		// Hit statistics are advisory. Keep the request path non-blocking when
		// SQLite is slow; a later hit will still advance last_accessed.
		return false
	}
}

type mergedHitCount struct {
	count        int64
	lastAccessed time.Time
}

// mergeHitCount keeps the advisory hit-count backlog bounded while preserving
// updates for IDs already admitted. A sustained database write failure must
// not turn otherwise healthy cache-hit traffic into unbounded process memory.
func mergeHitCount(pending map[uint]mergedHitCount, update hitCountUpdate) bool {
	merged, exists := pending[update.entryID]
	if !exists && len(pending) >= hitUpdatePendingLimit {
		return false
	}
	merged.count++
	if update.lastAccessed.After(merged.lastAccessed) {
		merged.lastAccessed = update.lastAccessed
	}
	pending[update.entryID] = merged
	return true
}

func (m *Manager) flushHitCounts(ctx context.Context, pending map[uint]mergedHitCount) error {
	if len(pending) == 0 {
		return nil
	}
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for entryID, update := range pending {
			if err := tx.Model(&db.CacheEntry{}).
				Where("id = ?", entryID).
				Updates(map[string]interface{}{
					"hit_count":     gorm.Expr("hit_count + ?", update.count),
					"last_accessed": update.lastAccessed,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	clear(pending)
	return nil
}

func (m *Manager) runHitCountWorker(ctx context.Context) {
	ticker := time.NewTicker(hitUpdateFlushInterval)
	defer ticker.Stop()
	pending := make(map[uint]mergedHitCount)
	retryDelay := hitUpdateRetryInitial
	var retryAt time.Time
	var dropped uint64
	merge := func(update hitCountUpdate) {
		if mergeHitCount(pending, update) {
			return
		}
		if dropped == 0 {
			zap.L().Debug("cache hit-count backlog full; dropping new advisory IDs",
				zap.Int("pending_ids", len(pending)),
				zap.Int("pending_limit", hitUpdatePendingLimit),
			)
		}
		dropped++
	}
	flush := func(flushCtx context.Context) {
		if err := m.flushHitCounts(flushCtx, pending); err != nil {
			zap.L().Debug("cache hit-count batch update failed", zap.Error(err))
			retryAt = time.Now().Add(retryDelay)
			if retryDelay >= hitUpdateRetryMax/2 {
				retryDelay = hitUpdateRetryMax
			} else {
				retryDelay *= 2
			}
			return
		}
		retryAt = time.Time{}
		retryDelay = hitUpdateRetryInitial
		if dropped > 0 {
			zap.L().Debug("cache hit-count backlog recovered after dropping advisory IDs",
				zap.Uint64("dropped_updates", dropped),
			)
			dropped = 0
		}
	}

	for {
		select {
		case update := <-m.hitUpdates:
			merge(update)
			// Once a flush fails, only the timed retry path below may touch the
			// database. Incoming hits continue to coalesce without turning every
			// event into another failed transaction.
			if retryAt.IsZero() && len(pending) >= hitUpdateFlushBatchSize {
				flush(ctx)
			}
		case <-ticker.C:
			if retryAt.IsZero() || !time.Now().Before(retryAt) {
				flush(ctx)
			}
		case <-ctx.Done():
			// recordHit and Close serialize on lifecycleMu. Once cancellation is
			// observed, no producer can add another accepted event, so draining
			// until empty has a stable boundary.
			for {
				select {
				case update := <-m.hitUpdates:
					merge(update)
				default:
					flushCtx, cancel := context.WithTimeout(
						context.WithoutCancel(ctx),
						hitUpdateCloseFlushTimeout,
					)
					if err := m.flushHitCounts(flushCtx, pending); err != nil {
						zap.L().Debug("cache hit-count final batch update failed", zap.Error(err))
					}
					cancel()
					if dropped > 0 {
						zap.L().Debug("cache hit-count shutdown dropped advisory updates",
							zap.Uint64("dropped_updates", dropped),
						)
					}
					return
				}
			}
		}
	}
}
