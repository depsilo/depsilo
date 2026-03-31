package cache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// ExtractPackageName extracts a human-readable package name from a cache key.
func ExtractPackageName(adapterType, key string) string {
	switch adapterType {
	case "pypi":
		// key: pypi/simple/{package}/index.html or pypi/files/.../{filename}
		if strings.HasPrefix(key, "pypi/simple/") {
			parts := strings.SplitN(strings.TrimPrefix(key, "pypi/simple/"), "/", 2)
			if len(parts) > 0 {
				return parts[0]
			}
		}
		if strings.HasPrefix(key, "pypi/files/") {
			// Extract filename, e.g. "requests-2.31.0-py3-none-any.whl" → "requests"
			path := strings.TrimPrefix(key, "pypi/files/")
			parts := strings.Split(path, "/")
			fname := parts[len(parts)-1]
			// Split on '-' and take first part
			if idx := strings.Index(fname, "-"); idx > 0 {
				return fname[:idx]
			}
			return fname
		}
	case "apt":
		// key: apt/{repo}/{path}, extract .deb name or path component
		if strings.HasSuffix(key, ".deb") {
			parts := strings.Split(key, "/")
			fname := parts[len(parts)-1]
			// "curl_7.68.0-1ubuntu2_amd64.deb" → "curl"
			if idx := strings.Index(fname, "_"); idx > 0 {
				return fname[:idx]
			}
			return fname
		}
		// For metadata files, use the repo name
		parts := strings.SplitN(strings.TrimPrefix(key, "apt/"), "/", 2)
		if len(parts) > 0 {
			return parts[0]
		}
	case "npm":
		// key: npm/<package>/metadata.json or npm/<package>/-/<filename>
		// or npm/@<scope>/<package>/metadata.json or npm/@<scope>/<package>/-/<filename>
		trimmed := strings.TrimPrefix(key, "npm/")
		if strings.HasPrefix(trimmed, "@") {
			// Scoped: @scope/package/...
			parts := strings.SplitN(trimmed, "/", 3)
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1] // @scope/package
			}
		} else {
			// Unscoped: package/...
			parts := strings.SplitN(trimmed, "/", 2)
			if len(parts) >= 1 {
				return parts[0]
			}
		}
	case "go":
		// key: go/<module>/@v/... or go/<module>/@latest
		trimmed := strings.TrimPrefix(key, "go/")
		if idx := strings.Index(trimmed, "/@v/"); idx > 0 {
			return trimmed[:idx]
		}
		if idx := strings.Index(trimmed, "/@latest"); idx > 0 {
			return trimmed[:idx]
		}
	case "cargo":
		// key: cargo/index/{prefix}/{crate} or cargo/crates/{crate}/{version}.crate
		trimmed := strings.TrimPrefix(key, "cargo/")
		if strings.HasPrefix(trimmed, "crates/") {
			parts := strings.SplitN(strings.TrimPrefix(trimmed, "crates/"), "/", 2)
			if len(parts) >= 1 {
				return parts[0]
			}
		}
		if strings.HasPrefix(trimmed, "index/") {
			parts := strings.Split(trimmed, "/")
			if len(parts) >= 1 {
				return parts[len(parts)-1]
			}
		}
	case "maven":
		// key: maven/org/apache/commons/commons-lang3/3.14.0/commons-lang3-3.14.0.jar
		// Extract artifactId (second-to-last path segment for versioned files)
		parts := strings.Split(strings.TrimPrefix(key, "maven/"), "/")
		if len(parts) >= 3 {
			return parts[len(parts)-3] // artifactId
		}
	case "rubygems":
		// key: rubygems/gems/rails-7.0.0.gem or rubygems/info/rails
		trimmed := strings.TrimPrefix(key, "rubygems/")
		if strings.HasPrefix(trimmed, "gems/") {
			fname := strings.TrimPrefix(trimmed, "gems/")
			// "rails-7.0.0.gem" -> "rails"
			if idx := strings.LastIndex(fname, "-"); idx > 0 {
				return fname[:idx]
			}
			return strings.TrimSuffix(fname, ".gem")
		}
		if strings.HasPrefix(trimmed, "info/") {
			return strings.TrimPrefix(trimmed, "info/")
		}
	case "composer":
		// key: composer/p2/monolog/monolog.json or composer/dist/monolog/monolog/abc123.zip
		trimmed := strings.TrimPrefix(key, "composer/")
		if strings.HasPrefix(trimmed, "p2/") {
			name := strings.TrimPrefix(trimmed, "p2/")
			name = strings.TrimSuffix(name, ".json")
			name = strings.TrimSuffix(name, "~dev")
			return name // "monolog/monolog"
		}
		if strings.HasPrefix(trimmed, "dist/") {
			parts := strings.SplitN(strings.TrimPrefix(trimmed, "dist/"), "/", 3)
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1]
			}
		}
	}
	return ""
}

// Manager handles cache lookup, singleflight dedup, and storage writes.
type Manager struct {
	storage  Storage
	db       *gorm.DB
	group    singleflight.Group
	eventBus *EventBus
}

// NewManager creates a new cache manager.
func NewManager(storage Storage, database *gorm.DB, eventBus *EventBus) *Manager {
	return &Manager{
		storage:  storage,
		db:       database,
		eventBus: eventBus,
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
				if m.eventBus != nil {
					parts := strings.Split(key, "/")
					m.eventBus.Publish(CacheEvent{
						Time:        time.Now(),
						PackageName: ExtractPackageName(adapterType, key),
						FileName:    parts[len(parts)-1],
						AdapterType: adapterType,
						Hit:         true,
					})
				}
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
				PackageName:  ExtractPackageName(adapterType, key),
				ExpiresAt:    now.Add(ttl),
				LastAccessed: now,
			}
			if err := m.db.Create(&entry).Error; err != nil {
				// Might be duplicate key from race, try update
				m.db.Where("key = ?", key).Updates(map[string]interface{}{
					"size":          size,
					"content_type":  contentType,
					"package_name":  ExtractPackageName(adapterType, key),
					"expires_at":    now.Add(ttl),
					"last_accessed": now,
				})
			}
		}

		if m.eventBus != nil {
			parts := strings.Split(key, "/")
			m.eventBus.Publish(CacheEvent{
				Time:        time.Now(),
				PackageName: ExtractPackageName(adapterType, key),
				FileName:    parts[len(parts)-1],
				AdapterType: adapterType,
				Hit:         false,
				Size:        size,
			})
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
