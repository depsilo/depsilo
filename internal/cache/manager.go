package cache

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/db"
)

// countingReader wraps an io.Reader and counts bytes read through it.
// Used to determine actual body size when upstream doesn't report Content-Length.
type countingReader struct {
	r io.Reader
	n int64
}

func NewCountingReader(r io.Reader) *countingReader {
	return &countingReader{r: r}
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n += int64(n)
	return n, err
}

func (cr *countingReader) BytesRead() int64 {
	return cr.n
}

// SecurityScanner is the optional contract for scanning new packages on miss.
type SecurityScanner interface {
	ScanPackage(ctx context.Context, ecosystem, packageName string) error
}

// Manager handles cache lookup, singleflight dedup, and storage writes.
// Strategy: stale-while-revalidate + offline fallback.
//   - Cache entries are NEVER deleted by TTL expiration.
//   - If cache exists and is fresh (< TTL): serve immediately (HIT).
//   - If cache exists but is stale (>= TTL): serve immediately (HIT),
//     then trigger a background refresh from upstream.
//   - If cache miss: fetch from upstream, store, serve (MISS).
//   - If upstream fails on miss but stale cache exists: serve stale (HIT).
type Manager struct {
	storage         Storage
	db              *gorm.DB
	group           singleflight.Group
	eventBus        *EventBus
	securityScanner SecurityScanner
}

// SetSecurityScanner attaches an optional security scanner. Pass nil to detach.
func (m *Manager) SetSecurityScanner(s SecurityScanner) {
	m.securityScanner = s
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
type FetchFunc func(ctx context.Context) (body io.ReadCloser, contentType string, size int64, err error)

// GetResult holds the result of a cache get operation.
type GetResult struct {
	Reader      io.ReadCloser
	ContentType string
	Size        int64
	Hit         bool
}

// Get implements stale-while-revalidate caching:
//  1. Cache fresh → return immediately
//  2. Cache stale → return immediately + background refresh
//  3. Cache miss  → fetch from upstream → store → return
//  4. Upstream fail + stale cache exists → return stale cache
func (m *Manager) Get(ctx context.Context, key string, adapterType string, ttl time.Duration, fetchFn FetchFunc) (*GetResult, error) {
	// Check if we have a cached version (fresh or stale)
	exists, err := m.storage.Exists(ctx, key)
	if err != nil {
		zap.L().Warn("cache exists check failed", zap.String("key", key), zap.Error(err))
	}

	if exists {
		var entry db.CacheEntry
		dbErr := m.db.Where("key = ?", key).First(&entry).Error

		if dbErr == nil {
			isFresh := time.Now().Before(entry.ExpiresAt)

			// Serve from cache (both fresh and stale)
			reader, size, readErr := m.storage.Get(ctx, key)
			if readErr == nil {
				// Update hit count
				m.db.Model(&entry).Updates(map[string]interface{}{
					"hit_count":     gorm.Expr("hit_count + 1"),
					"last_accessed": time.Now(),
				})

				zap.L().Debug("cache hit",
					zap.String("key", key),
					zap.Bool("fresh", isFresh),
				)

				m.publishEvent(key, adapterType, true, 0)

				// If stale, trigger background refresh (non-blocking)
				if !isFresh {
					go m.backgroundRefresh(key, adapterType, ttl, fetchFn)
				}

				return &GetResult{
					Reader:      reader,
					ContentType: entry.ContentType,
					Size:        size,
					Hit:         true,
				}, nil
			}
			zap.L().Warn("cache read failed despite exists", zap.String("key", key), zap.Error(readErr))
		}
	}

	// Cache miss — fetch from upstream via singleflight
	result, err := m.fetchAndStore(ctx, key, adapterType, ttl, fetchFn)
	if err != nil {
		// Upstream failed — try serving stale cache as last resort
		if exists {
			zap.L().Warn("upstream failed, serving stale cache",
				zap.String("key", key),
				zap.Error(err),
			)
			return m.serveStale(ctx, key, adapterType)
		}
		return nil, err
	}

	return result, nil
}

// fetchAndStore fetches from upstream via singleflight, stores to cache, returns result.
func (m *Manager) fetchAndStore(ctx context.Context, key string, adapterType string, ttl time.Duration, fetchFn FetchFunc) (*GetResult, error) {
	type sfResult struct {
		contentType string
		size        int64
	}

	val, err, _ := m.group.Do(key, func() (interface{}, error) {
		fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer fetchCancel()

		body, contentType, size, err := fetchFn(fetchCtx)
		if err != nil {
			return nil, err
		}
		defer body.Close()

		cr := NewCountingReader(body)

		// Write to storage (streams directly from upstream)
		if putErr := m.storage.Put(fetchCtx, key, cr, size, contentType); putErr != nil {
			zap.L().Warn("cache put failed", zap.String("key", key), zap.Error(putErr))
		} else {
			if size <= 0 {
				size = cr.BytesRead()
			}
			// Upsert DB entry (never delete, just update expiry)
			now := time.Now()
			entry := db.CacheEntry{
				Key:          key,
				AdapterType:  adapterType,
				StoragePath:  key,
				Size:         size,
				HitCount:     0,
				ContentType:  contentType,
				PackageName:  packagekey.ExtractName(adapterType, key),
				ExpiresAt:    now.Add(ttl),
				LastAccessed: now,
			}
			if createErr := m.db.Create(&entry).Error; createErr != nil {
				// Already exists — update instead
				m.db.Where("key = ?", key).Updates(map[string]interface{}{
					"size":          size,
					"content_type":  contentType,
					"package_name":  packagekey.ExtractName(adapterType, key),
					"expires_at":    now.Add(ttl),
					"last_accessed": now,
				})
			}
		}

		m.publishEvent(key, adapterType, false, size)

		// Trigger async security scan for new packages
		if m.securityScanner != nil {
			pkgName := packagekey.ExtractName(adapterType, key)
			if pkgName != "" {
				go func() {
					if err := m.securityScanner.ScanPackage(context.Background(), adapterType, pkgName); err != nil {
						zap.L().Debug("security scan for new package failed", zap.Error(err))
					}
				}()
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

// backgroundRefresh refreshes a stale cache entry in the background.
// It uses singleflight so concurrent requests for the same key share one fetch.
func (m *Manager) backgroundRefresh(key string, adapterType string, ttl time.Duration, fetchFn FetchFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	_, err, _ := m.group.Do("bg:"+key, func() (interface{}, error) {
		body, contentType, size, err := fetchFn(ctx)
		if err != nil {
			return nil, err
		}
		defer body.Close()

		cr := NewCountingReader(body)

		if putErr := m.storage.Put(ctx, key, cr, size, contentType); putErr != nil {
			return nil, fmt.Errorf("bg refresh put: %w", putErr)
		}
		if size <= 0 {
			size = cr.BytesRead()
		}

		now := time.Now()
		m.db.Where("key = ?", key).Updates(map[string]interface{}{
			"size":          size,
			"content_type":  contentType,
			"expires_at":    now.Add(ttl),
			"last_accessed": now,
		})

		zap.L().Debug("background refresh done", zap.String("key", key))
		return nil, nil
	})

	if err != nil {
		zap.L().Warn("background refresh failed",
			zap.String("key", key),
			zap.Error(err),
		)
	}
}

// serveStale returns a stale cached version when upstream is unavailable.
func (m *Manager) serveStale(ctx context.Context, key string, adapterType string) (*GetResult, error) {
	var entry db.CacheEntry
	if err := m.db.Where("key = ?", key).First(&entry).Error; err != nil {
		return nil, fmt.Errorf("no stale cache for %s", key)
	}

	reader, size, err := m.storage.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("stale cache read failed: %w", err)
	}

	m.db.Model(&entry).Updates(map[string]interface{}{
		"hit_count":     gorm.Expr("hit_count + 1"),
		"last_accessed": time.Now(),
	})

	m.publishEvent(key, adapterType, true, 0)

	return &GetResult{
		Reader:      reader,
		ContentType: entry.ContentType,
		Size:        size,
		Hit:         true,
	}, nil
}

// publishEvent sends a cache event to the SSE bus.
func (m *Manager) publishEvent(key, adapterType string, hit bool, size int64) {
	if m.eventBus == nil {
		return
	}
	parts := strings.Split(key, "/")
	m.eventBus.Publish(CacheEvent{
		Time:        time.Now(),
		PackageName: packagekey.ExtractName(adapterType, key),
		FileName:    parts[len(parts)-1],
		AdapterType: adapterType,
		Hit:         hit,
		Size:        size,
	})
}

// Storage returns the underlying storage for direct access.
func (m *Manager) Storage() Storage {
	return m.storage
}

// DB returns the underlying database for direct access.
func (m *Manager) DB() *gorm.DB {
	return m.db
}
