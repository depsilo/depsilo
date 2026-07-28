package cache

import (
	"context"
	"errors"
	"net/http"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

var ErrCacheMiss = errors.New("cache entry is unavailable")

// HeadResult is the replay-safe representation metadata for a durable cached
// object. It deliberately exposes no storage reader, allowing protocol
// adapters to answer metadata HEAD requests without opening a multi-gigabyte
// body.
type HeadResult struct {
	ContentType string
	Size        int64
	Headers     http.Header
}

// Head returns metadata only when both the DB row and storage object exist.
// Missing or partially-persisted entries collapse to ErrCacheMiss.
func (m *Manager) Head(ctx context.Context, key, adapterType string) (*HeadResult, error) {
	if m == nil {
		return nil, ErrCacheMiss
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, ErrCacheMiss
	}

	unlock, err := m.lockSnapshot(ctx, key)
	if err != nil {
		return nil, err
	}
	defer unlock()

	if !m.invalidations.readable(key) {
		return nil, ErrCacheMiss
	}

	var entry db.CacheEntry
	if err := m.db.WithContext(ctx).Where("key = ?", key).First(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCacheMiss
		}
		return nil, err
	}
	exists, err := m.storage.Exists(ctx, entry.StoragePath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCacheMiss
	}

	_ = adapterType // retained in the seam for future adapter-specific policy
	return &HeadResult{
		ContentType: entry.ContentType,
		Size:        entry.Size,
		Headers:     decodeStoredResponseMetadata(entry.ResponseHeaders, entry.ETag, entry.LastModified),
	}, nil
}
