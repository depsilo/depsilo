package cache

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/db"
)

// PackageInvalidationResult reports whether every discovered representation
// was made unreadable. Backend cleanup errors can coexist with SafeToRestore:
// deleting either the metadata row or the object is enough to prevent the old
// representation from being served.
type PackageInvalidationResult struct {
	Entries       int
	SafeToRestore bool
}

type packageInvalidationEntry struct {
	Key         string
	StoragePath string
	generation  uint64
}

// InvalidatePackage makes all cache entries for one adapter package
// unreadable before deleting any of them. The batch-first tombstones prevent a
// sibling entry from being served while cleanup is walking a large package.
func (m *Manager) InvalidatePackage(
	ctx context.Context,
	adapterType string,
	packageName string,
) (PackageInvalidationResult, error) {
	result := PackageInvalidationResult{}
	if m == nil {
		return result, ErrManagerClosed
	}
	adapterType = strings.TrimSpace(adapterType)
	packageName = strings.TrimSpace(packageName)
	if adapterType == "" {
		return result, errors.New("cache package invalidation adapter type is empty")
	}
	if packageName == "" {
		return result, errors.New("cache package invalidation package name is empty")
	}
	if m.db == nil || m.storage == nil || m.invalidations == nil {
		return result, errors.New("cache package invalidation is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		m.invalidationTimeout,
	)
	defer cancel()

	// Keep miss and background-refresh registries stable through the
	// persisted-row snapshot and tombstone publication. A writer transitions
	// from inflight/refreshing to a committed row before leaving its registry;
	// without this handshake it could disappear after the SQL snapshot and
	// escape both sets.
	m.inflightMu.Lock()
	m.refreshMu.Lock()
	entriesByKey := make(map[string]packageInvalidationEntry)
	for key := range m.inflight {
		if packagekey.ExtractName(adapterType, key) == packageName {
			entriesByKey[key] = packageInvalidationEntry{
				Key:         key,
				StoragePath: key,
			}
		}
	}
	for key := range m.refreshing {
		if packagekey.ExtractName(adapterType, key) == packageName {
			entriesByKey[key] = packageInvalidationEntry{
				Key:         key,
				StoragePath: key,
			}
		}
	}

	var persisted []packageInvalidationEntry
	if err := m.db.WithContext(cleanupCtx).
		Model(&db.CacheEntry{}).
		Select("key", "storage_path").
		Where("adapter_type = ? AND package_name = ?", adapterType, packageName).
		Find(&persisted).Error; err != nil {
		m.refreshMu.Unlock()
		m.inflightMu.Unlock()
		return result, fmt.Errorf("list package cache entries: %w", err)
	}
	for _, entry := range persisted {
		if entry.Key == "" {
			continue
		}
		if entry.StoragePath == "" {
			entry.StoragePath = entry.Key
		}
		entriesByKey[entry.Key] = entry
	}

	keys := make([]string, 0, len(entriesByKey))
	for key := range entriesByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	entries := make([]packageInvalidationEntry, 0, len(keys))
	for _, key := range keys {
		entry := entriesByKey[key]
		entry.generation = m.invalidations.beginInvalidation(key)
		entries = append(entries, entry)
	}
	m.refreshMu.Unlock()
	m.inflightMu.Unlock()
	result.Entries = len(entries)
	result.SafeToRestore = true

	var cleanupErr error
	for _, entry := range entries {
		cleanupSafe := false
		unlock, lockErr := m.lockMutation(cleanupCtx, entry.Key)
		if lockErr != nil {
			m.invalidations.finishInvalidation(entry.Key, entry.generation, false)
			result.SafeToRestore = false
			cleanupErr = errors.Join(
				cleanupErr,
				fmt.Errorf("lock package cache entry %q: %w", entry.Key, lockErr),
			)
			continue
		}

		if m.invalidations.invalidationPending(entry.Key, entry.generation) {
			deleteResult := m.db.WithContext(cleanupCtx).
				Where("key = ?", entry.Key).
				Delete(&db.CacheEntry{})
			dbErr := deleteResult.Error
			storageErr := m.storage.Delete(cleanupCtx, entry.StoragePath)
			cleanupSafe = dbErr == nil || storageErr == nil
			if dbErr != nil {
				cleanupErr = errors.Join(
					cleanupErr,
					fmt.Errorf("delete package cache metadata %q: %w", entry.Key, dbErr),
				)
			}
			if storageErr != nil {
				cleanupErr = errors.Join(
					cleanupErr,
					fmt.Errorf("delete package cache object %q: %w", entry.Key, storageErr),
				)
			}
		} else {
			// A newer successful write superseded this generation. The caller's
			// repository gate remains closed, so report unsafe and require a
			// later cleanup before that gate can be restored.
			cleanupErr = errors.Join(
				cleanupErr,
				fmt.Errorf("package cache entry %q was superseded during invalidation", entry.Key),
			)
		}
		unlock()
		m.invalidations.finishInvalidation(entry.Key, entry.generation, cleanupSafe)
		if !cleanupSafe {
			result.SafeToRestore = false
		}
	}

	return result, cleanupErr
}
