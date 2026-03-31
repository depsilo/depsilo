package adapter

import (
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// LogAccess asynchronously writes an access log entry.
func LogAccess(database *gorm.DB, adapterType, cacheKey string, hit bool, upstreamName string, latency time.Duration, statusCode int, clientIP string, bytesSent int64) {
	entry := db.AccessLog{
		AdapterType: adapterType,
		CacheKey:    cacheKey,
		PackageName: extractPackageName(adapterType, cacheKey),
		Hit:         hit,
		Upstream:    upstreamName,
		LatencyMs:   latency.Milliseconds(),
		StatusCode:  statusCode,
		ClientIP:    clientIP,
		BytesSent:   bytesSent,
	}

	go func() {
		if err := database.Create(&entry).Error; err != nil {
			zap.L().Warn("failed to write access log", zap.Error(err))
		}
	}()
}

// extractPackageName derives a human-readable package name from the cache key.
func extractPackageName(adapterType, key string) string {
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
		// Find /@v/ or /@latest to extract the module path
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
	}
	return ""
}
