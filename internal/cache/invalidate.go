package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/db"
)

func (m *Manager) lockMutation(ctx context.Context, key string) (func(), error) {
	if m.mutations == nil {
		return nil, errors.New("cache mutation gate is not initialized")
	}
	return m.mutations.lock(ctx, key)
}

func (m *Manager) lockSnapshot(ctx context.Context, key string) (func(), error) {
	if m.mutations == nil {
		return nil, errors.New("cache mutation gate is not initialized")
	}
	return m.mutations.lockSnapshot(ctx, key)
}

// Invalidate makes a cache key unreadable before removing its metadata and
// object. If neither backend can be cleaned, the in-memory tombstone remains
// fail-closed until a successful replacement write or process restart.
func (m *Manager) Invalidate(ctx context.Context, key string) error {
	if m == nil {
		return ErrManagerClosed
	}
	if key == "" {
		return errors.New("cache invalidation key is empty")
	}
	return m.invalidateCachedEntry(ctx, key)
}

// removeUncommittedObject compensates for the lack of a transaction spanning
// SQLite and Local/S3 storage. The caller holds the key mutation gate, so this
// delete cannot remove a newer fill. A detached, bounded context gives S3 a
// chance to remove bytes even when the failed DB operation exhausted or
// cancelled the original operation context.
func (m *Manager) removeUncommittedObject(ctx context.Context, key string, commitErr error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	generation := m.invalidations.beginInvalidation(key)
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	deleteResult := m.db.WithContext(cleanupCtx).
		Where("key = ?", key).
		Delete(&db.CacheEntry{})
	dbErr := deleteResult.Error
	storageErr := m.storage.Delete(cleanupCtx, key)
	cleanupSafe := dbErr == nil || storageErr == nil
	m.invalidations.finishInvalidation(key, generation, cleanupSafe)

	if dbErr != nil {
		dbErr = fmt.Errorf("remove uncommitted cache metadata: %w", dbErr)
	}
	if storageErr != nil {
		storageErr = fmt.Errorf("remove uncommitted cache object: %w", storageErr)
		zap.L().Error("failed to remove uncommitted cache object",
			zap.String("key", key),
			zap.Error(storageErr),
		)
	}
	return errors.Join(commitErr, dbErr, storageErr)
}

// invalidateCachedEntry makes an authoritative upstream result sticky across
// later transient failures. Metadata is removed even when object cleanup
// fails, and object cleanup is still attempted when the DB is unavailable; in
// either case the old representation cannot be served through a valid row.
func (m *Manager) invalidateCachedEntry(ctx context.Context, key string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	generation := m.invalidations.beginInvalidation(key)
	cleanupSafe := false
	defer func() {
		m.invalidations.finishInvalidation(key, generation, cleanupSafe)
	}()

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.invalidationTimeout)
	defer cancel()
	unlock, err := m.lockMutation(cleanupCtx, key)
	if err != nil {
		return err
	}
	defer unlock()
	if !m.invalidations.invalidationPending(key, generation) {
		return nil
	}

	deleteResult := m.db.WithContext(cleanupCtx).
		Where("key = ?", key).
		Delete(&db.CacheEntry{})
	dbErr := deleteResult.Error
	storageErr := m.storage.Delete(cleanupCtx, key)
	cleanupSafe = dbErr == nil || storageErr == nil
	if dbErr != nil {
		dbErr = fmt.Errorf("delete invalid cache metadata: %w", dbErr)
	}
	if storageErr != nil {
		storageErr = fmt.Errorf("delete invalid cache object: %w", storageErr)
	}
	return errors.Join(dbErr, storageErr)
}
