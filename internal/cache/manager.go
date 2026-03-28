package cache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"repocache/internal/db"
)

// Manager handles cache lookup, singleflight dedup, and storage writes.
type Manager struct {
	storage Storage
	db      *gorm.DB
	group   singleflight.Group
}

// NewManager creates a new cache manager.
func NewManager(storage Storage, database *gorm.DB) *Manager {
	return &Manager{
		storage: storage,
		db:      database,
	}
}

// FetchFunc is called on cache miss to fetch data from upstream.
// It returns the response body, content type, and size.
type FetchFunc func(ctx context.Context) (body io.ReadCloser, contentType string, size int64, err error)

// GetResult holds the result of a cache get operation.
type GetResult struct {
	Reader      io.ReadCloser
	ContentType string
	Size        int64
	Hit         bool
}

// Get tries the cache first; on miss, calls fetchFn via singleflight,
// stores the result, and returns it.
func (m *Manager) Get(ctx context.Context, key string, adapterType string, ttl time.Duration, fetchFn FetchFunc) (*GetResult, error) {
	// Try cache first
	exists, err := m.storage.Exists(ctx, key)
	if err != nil {
		zap.L().Warn("cache exists check failed", zap.String("key", key), zap.Error(err))
	}

	if exists {
		// Check if expired in DB
		var entry db.CacheEntry
		result := m.db.Where("key = ?", key).First(&entry)
		if result.Error == nil && time.Now().Before(entry.ExpiresAt) {
			reader, size, err := m.storage.Get(ctx, key)
			if err == nil {
				// Update hit count and last accessed
				m.db.Model(&entry).Updates(map[string]interface{}{
					"hit_count":     gorm.Expr("hit_count + 1"),
					"last_accessed": time.Now(),
				})
				zap.L().Debug("cache hit", zap.String("key", key))
				return &GetResult{
					Reader:      reader,
					ContentType: entry.ContentType,
					Size:        size,
					Hit:         true,
				}, nil
			}
			zap.L().Warn("cache get failed despite exists", zap.String("key", key), zap.Error(err))
		} else if result.Error == nil {
			// Expired, clean up
			zap.L().Debug("cache expired", zap.String("key", key))
			m.storage.Delete(ctx, key)
			m.db.Delete(&entry)
		}
	}

	// Cache miss — use singleflight to deduplicate concurrent fetches
	type sfResult struct {
		contentType string
		size        int64
	}

	val, err, _ := m.group.Do(key, func() (interface{}, error) {
		body, contentType, size, err := fetchFn(ctx)
		if err != nil {
			return nil, err
		}
		defer body.Close()

		// Read the body into a buffer so we can store it and return it.
		// For large files (Phase 6 optimization), this should be streamed via io.TeeReader.
		// For now, buffer to ensure correctness with singleflight.
		var buf bytes.Buffer
		written, err := io.Copy(&buf, body)
		if err != nil {
			return nil, fmt.Errorf("read upstream body: %w", err)
		}
		if size <= 0 {
			size = written
		}

		// Write to storage
		if err := m.storage.Put(ctx, key, bytes.NewReader(buf.Bytes()), size, contentType); err != nil {
			zap.L().Warn("cache put failed", zap.String("key", key), zap.Error(err))
			// Don't fail the request if storage write fails
		} else {
			// Record in DB
			now := time.Now()
			entry := db.CacheEntry{
				Key:          key,
				AdapterType:  adapterType,
				StoragePath:  key,
				Size:         size,
				HitCount:     0,
				ContentType:  contentType,
				ExpiresAt:    now.Add(ttl),
				LastAccessed: now,
			}
			if err := m.db.Create(&entry).Error; err != nil {
				// Might be duplicate key from race, try update
				m.db.Where("key = ?", key).Updates(map[string]interface{}{
					"size":          size,
					"content_type":  contentType,
					"expires_at":    now.Add(ttl),
					"last_accessed": now,
				})
			}
		}

		return &sfResult{contentType: contentType, size: size}, nil
	})

	if err != nil {
		return nil, err
	}

	sfRes := val.(*sfResult)

	// Read back from cache
	reader, size, err := m.storage.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read from cache after store: %w", err)
	}

	return &GetResult{
		Reader:      reader,
		ContentType: sfRes.contentType,
		Size:        size,
		Hit:         false,
	}, nil
}

// Storage returns the underlying storage for direct access.
func (m *Manager) Storage() Storage {
	return m.storage
}

// DB returns the underlying database for direct access.
func (m *Manager) DB() *gorm.DB {
	return m.db
}
