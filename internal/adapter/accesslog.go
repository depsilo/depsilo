package adapter

import (
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// auditLogger is the optional Pro audit logger, set via SetAuditLogger.
var auditLogger interface {
	Log(entry db.AuditLog)
}

// SetAuditLogger sets the audit logger used by LogAccess.
func SetAuditLogger(l interface{ Log(entry db.AuditLog) }) {
	auditLogger = l
}

// LogAccess asynchronously writes an access log entry.
func LogAccess(database *gorm.DB, adapterType, method, cacheKey string, hit bool, upstreamName string, latency time.Duration, statusCode int, clientIP string, bytesSent int64) {
	entry := db.AccessLog{
		AdapterType: adapterType,
		Method:      method,
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

	if auditLogger != nil {
		action := "download"
		if strings.HasSuffix(cacheKey, ".json") || strings.HasSuffix(cacheKey, ".html") ||
			strings.HasSuffix(cacheKey, ".xml") || strings.Contains(cacheKey, "metadata") ||
			strings.Contains(cacheKey, "index") || strings.Contains(cacheKey, "PACKAGES") ||
			strings.Contains(cacheKey, "repodata") {
			action = "metadata"
		}
		cacheResult := "miss"
		if hit {
			cacheResult = "hit"
		}
		if statusCode >= 500 {
			cacheResult = "error"
		}
		auditLogger.Log(db.AuditLog{
			Ecosystem:   adapterType,
			PackageName: extractPackageName(adapterType, cacheKey),
			Action:      action,
			CacheResult: cacheResult,
			ClientIP:    clientIP,
			LatencyMs:   latency.Milliseconds(),
			BytesSent:   bytesSent,
			StatusCode:  statusCode,
		})
	}
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
	case "maven":
		// key: maven/org/apache/commons/commons-lang3/3.14.0/commons-lang3-3.14.0.jar
		parts := strings.Split(strings.TrimPrefix(key, "maven/"), "/")
		if len(parts) >= 3 {
			return parts[len(parts)-3]
		}
	case "rubygems":
		// key: rubygems/gems/rails-7.0.0.gem or rubygems/info/rails
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
		// key: composer/p2/monolog/monolog.json or composer/dist/monolog/monolog/abc123.zip
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
	case "docker":
		// key: docker/{registry}/manifests/{image}/{ref} or docker/{registry}/blobs/{digest}
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
		if strings.Contains(key, "/blobs/") {
			return ""
		}
	}
	return ""
}
