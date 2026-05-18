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

// ExtractPackageName extracts a human-readable package name from a cache key.
func ExtractPackageName(adapterType, key string) string {
	switch adapterType {
	case "pypi":
		if strings.HasPrefix(key, "pypi/simple/") {
			parts := strings.SplitN(strings.TrimPrefix(key, "pypi/simple/"), "/", 2)
			if len(parts) > 0 {
				return parts[0]
			}
		}
		if strings.HasPrefix(key, "pypi/files/") {
			path := strings.TrimPrefix(key, "pypi/files/")
			parts := strings.Split(path, "/")
			fname := parts[len(parts)-1]
			if idx := strings.Index(fname, "-"); idx > 0 {
				return fname[:idx]
			}
			return fname
		}
	case "apt":
		if strings.HasSuffix(key, ".deb") {
			parts := strings.Split(key, "/")
			fname := parts[len(parts)-1]
			if idx := strings.Index(fname, "_"); idx > 0 {
				return fname[:idx]
			}
			return fname
		}
		parts := strings.SplitN(strings.TrimPrefix(key, "apt/"), "/", 2)
		if len(parts) > 0 {
			return parts[0]
		}
	case "npm":
		trimmed := strings.TrimPrefix(key, "npm/")
		if strings.HasPrefix(trimmed, "@") {
			parts := strings.SplitN(trimmed, "/", 3)
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1]
			}
		} else {
			parts := strings.SplitN(trimmed, "/", 2)
			if len(parts) >= 1 {
				return parts[0]
			}
		}
	case "go":
		trimmed := strings.TrimPrefix(key, "go/")
		if idx := strings.Index(trimmed, "/@v/"); idx > 0 {
			return trimmed[:idx]
		}
		if idx := strings.Index(trimmed, "/@latest"); idx > 0 {
			return trimmed[:idx]
		}
	case "cargo":
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
		parts := strings.Split(strings.TrimPrefix(key, "maven/"), "/")
		if len(parts) >= 3 {
			return parts[len(parts)-3]
		}
	case "rubygems":
		trimmed := strings.TrimPrefix(key, "rubygems/")
		if strings.HasPrefix(trimmed, "gems/") {
			fname := strings.TrimPrefix(trimmed, "gems/")
			if idx := strings.LastIndex(fname, "-"); idx > 0 {
				return fname[:idx]
			}
			return strings.TrimSuffix(fname, ".gem")
		}
		if strings.HasPrefix(trimmed, "info/") {
			return strings.TrimPrefix(trimmed, "info/")
		}
	case "composer":
		trimmed := strings.TrimPrefix(key, "composer/")
		if strings.HasPrefix(trimmed, "p2/") {
			name := strings.TrimPrefix(trimmed, "p2/")
			name = strings.TrimSuffix(name, ".json")
			name = strings.TrimSuffix(name, "~dev")
			return name
		}
		if strings.HasPrefix(trimmed, "dist/") {
			parts := strings.SplitN(strings.TrimPrefix(trimmed, "dist/"), "/", 3)
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1]
			}
		}
	case "conda":
		trimmed := strings.TrimPrefix(key, "conda/")
		parts := strings.Split(trimmed, "/")
		fname := parts[len(parts)-1]
		if strings.HasSuffix(fname, ".tar.bz2") {
			fname = strings.TrimSuffix(fname, ".tar.bz2")
		} else if strings.HasSuffix(fname, ".conda") {
			fname = strings.TrimSuffix(fname, ".conda")
		} else {
			return fname
		}
		if idx := strings.Index(fname, "-"); idx > 0 {
			return fname[:idx]
		}
		return fname
	case "cran":
		trimmed := strings.TrimPrefix(key, "cran/")
		parts := strings.Split(trimmed, "/")
		fname := parts[len(parts)-1]
		if idx := strings.Index(fname, "_"); idx > 0 {
			return fname[:idx]
		}
		return strings.TrimSuffix(fname, ".tar.gz")
	case "nuget":
		trimmed := strings.TrimPrefix(key, "nuget/")
		if strings.HasPrefix(trimmed, "v3/package/") {
			parts := strings.SplitN(strings.TrimPrefix(trimmed, "v3/package/"), "/", 2)
			if len(parts) >= 1 {
				return parts[0]
			}
		}
		if strings.HasPrefix(trimmed, "v3/registration/") {
			parts := strings.SplitN(strings.TrimPrefix(trimmed, "v3/registration/"), "/", 2)
			if len(parts) >= 1 {
				return parts[0]
			}
		}
	case "helm":
		trimmed := strings.TrimPrefix(key, "helm/")
		if strings.HasSuffix(trimmed, ".tgz") {
			fname := trimmed
			if idx := strings.LastIndex(fname, "/"); idx >= 0 {
				fname = fname[idx+1:]
			}
			fname = strings.TrimSuffix(fname, ".tgz")
			for i := len(fname) - 1; i >= 0; i-- {
				if fname[i] == '-' && i+1 < len(fname) && fname[i+1] >= '0' && fname[i+1] <= '9' {
					return fname[:i]
				}
			}
			return fname
		}
	case "docker":
		if strings.Contains(key, "/manifests/") {
			parts := strings.SplitN(key, "/manifests/", 2)
			if len(parts) == 2 {
				image := parts[1]
				if idx := strings.LastIndex(image, "/"); idx > 0 {
					return image[:idx]
				}
				return image
			}
		}
		if strings.Contains(key, "/tags/") {
			parts := strings.SplitN(key, "/tags/", 2)
			if len(parts) == 2 {
				return strings.TrimSuffix(parts[1], "/list")
			}
		}
		if strings.Contains(key, "/blobs/") {
			return ""
		}
	}
	return ""
}

// ExtractPackageVersion derives a version string from the cache key.
func ExtractPackageVersion(adapterType, key string) string {
	switch adapterType {
	case "pypi":
		// key: pypi/files/.../requests-2.31.0-py3-none-any.whl or requests-2.31.0.tar.gz
		if !strings.HasPrefix(key, "pypi/files/") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		// Remove extension
		for _, ext := range []string{".whl", ".tar.gz", ".zip", ".egg"} {
			fname = strings.TrimSuffix(fname, ext)
		}
		// Format: name-version or name-version-pyver-abi-platform
		dashParts := strings.SplitN(fname, "-", 3)
		if len(dashParts) >= 2 {
			return dashParts[1]
		}
	case "npm":
		// key: npm/lodash/-/lodash-4.17.21.tgz
		if !strings.HasSuffix(key, ".tgz") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		fname = strings.TrimSuffix(fname, ".tgz")
		// name-version.tgz — find last hyphen before version
		if idx := strings.LastIndex(fname, "-"); idx > 0 {
			return fname[idx+1:]
		}
	case "go":
		// key: go/github.com/gin-gonic/gin/@v/v1.9.1.zip
		if idx := strings.Index(key, "/@v/"); idx > 0 {
			ver := key[idx+4:]
			for _, ext := range []string{".zip", ".mod", ".info"} {
				ver = strings.TrimSuffix(ver, ext)
			}
			return ver
		}
	case "cargo":
		// key: cargo/crates/serde/1.0.0.crate
		if !strings.HasSuffix(key, ".crate") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		return strings.TrimSuffix(fname, ".crate")
	case "apt":
		// key: apt/.../curl_7.68.0-1ubuntu2_amd64.deb
		if !strings.HasSuffix(key, ".deb") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		fname = strings.TrimSuffix(fname, ".deb")
		// name_version_arch.deb
		underParts := strings.SplitN(fname, "_", 3)
		if len(underParts) >= 2 {
			return underParts[1]
		}
	case "maven":
		// key: maven/.../commons-lang3/3.14.0/commons-lang3-3.14.0.jar
		parts := strings.Split(strings.TrimPrefix(key, "maven/"), "/")
		if len(parts) >= 3 {
			return parts[len(parts)-2]
		}
	case "rubygems":
		// key: rubygems/gems/rails-7.0.0.gem
		if !strings.HasSuffix(key, ".gem") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		fname = strings.TrimSuffix(fname, ".gem")
		if idx := strings.LastIndex(fname, "-"); idx > 0 {
			return fname[idx+1:]
		}
	case "nuget":
		// key: nuget/v3/package/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg
		parts := strings.Split(strings.TrimPrefix(key, "nuget/"), "/")
		if len(parts) >= 3 && strings.HasPrefix(parts[0], "v3/package") {
			// v3/package/{name}/{version}/{file}.nupkg
			return parts[2]
		}
		// Try: v3/package/name/version/...
		for i, p := range parts {
			if p == "package" && i+2 < len(parts) {
				return parts[i+2]
			}
		}
	case "conda":
		// key: conda/.../numpy-1.24.0-py39h.tar.bz2
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		for _, ext := range []string{".tar.bz2", ".conda"} {
			fname = strings.TrimSuffix(fname, ext)
		}
		// name-version-build
		dashParts := strings.SplitN(fname, "-", 3)
		if len(dashParts) >= 2 {
			return dashParts[1]
		}
	case "cran":
		// key: cran/.../ggplot2_3.4.0.tar.gz
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		fname = strings.TrimSuffix(fname, ".tar.gz")
		fname = strings.TrimSuffix(fname, ".zip")
		fname = strings.TrimSuffix(fname, ".tgz")
		if idx := strings.Index(fname, "_"); idx > 0 {
			return fname[idx+1:]
		}
	case "helm":
		// key: helm/nginx-15.0.0.tgz
		if !strings.HasSuffix(key, ".tgz") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		fname = strings.TrimSuffix(fname, ".tgz")
		// Find last hyphen before version number
		for i := len(fname) - 1; i >= 0; i-- {
			if fname[i] == '-' && i+1 < len(fname) && fname[i+1] >= '0' && fname[i+1] <= '9' {
				return fname[i+1:]
			}
		}
	case "docker":
		// key: docker/{registry}/manifests/{image}/{ref}
		if strings.Contains(key, "/manifests/") {
			parts := strings.SplitN(key, "/manifests/", 2)
			if len(parts) == 2 {
				if idx := strings.LastIndex(parts[1], "/"); idx > 0 {
					return parts[1][idx+1:]
				}
			}
		}
	}
	return ""
}

// IsPackageFile returns true if the cache key represents an actual package file download
// (as opposed to metadata/index requests). Only package files should be recorded for SBOM.
func IsPackageFile(adapterType, key string) bool {
	switch adapterType {
	case "pypi":
		return strings.HasPrefix(key, "pypi/files/")
	case "npm":
		return strings.HasSuffix(key, ".tgz")
	case "go":
		return strings.HasSuffix(key, ".zip")
	case "cargo":
		return strings.HasSuffix(key, ".crate")
	case "apt":
		return strings.HasSuffix(key, ".deb")
	case "maven":
		return strings.HasSuffix(key, ".jar") || strings.HasSuffix(key, ".aar") || strings.HasSuffix(key, ".pom")
	case "rubygems":
		return strings.HasSuffix(key, ".gem")
	case "composer":
		return strings.HasSuffix(key, ".zip")
	case "nuget":
		return strings.HasSuffix(key, ".nupkg")
	case "conda":
		return strings.HasSuffix(key, ".tar.bz2") || strings.HasSuffix(key, ".conda")
	case "cran":
		return strings.HasSuffix(key, ".tar.gz") || strings.HasSuffix(key, ".zip") || strings.HasSuffix(key, ".tgz")
	case "helm":
		return strings.HasSuffix(key, ".tgz")
	case "docker":
		return strings.Contains(key, "/blobs/") || strings.Contains(key, "/manifests/")
	}
	return false
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
				PackageName:  ExtractPackageName(adapterType, key),
				ExpiresAt:    now.Add(ttl),
				LastAccessed: now,
			}
			if createErr := m.db.Create(&entry).Error; createErr != nil {
				// Already exists — update instead
				m.db.Where("key = ?", key).Updates(map[string]interface{}{
					"size":          size,
					"content_type":  contentType,
					"package_name":  ExtractPackageName(adapterType, key),
					"expires_at":    now.Add(ttl),
					"last_accessed": now,
				})
			}
		}

		m.publishEvent(key, adapterType, false, size)

		// Trigger async security scan for new packages
		if m.securityScanner != nil {
			pkgName := ExtractPackageName(adapterType, key)
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
		PackageName: ExtractPackageName(adapterType, key),
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
