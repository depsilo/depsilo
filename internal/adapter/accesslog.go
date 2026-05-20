package adapter

import (
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/adapter/packagekey"
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
	pkgName := packagekey.ExtractName(adapterType, cacheKey)
	entry := db.AccessLog{
		AdapterType: adapterType,
		Method:      method,
		CacheKey:    cacheKey,
		PackageName: pkgName,
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
			PackageName: pkgName,
			Version:     packagekey.ExtractVersion(adapterType, cacheKey),
			Action:      action,
			CacheResult: cacheResult,
			ClientIP:    clientIP,
			LatencyMs:   latency.Milliseconds(),
			BytesSent:   bytesSent,
			StatusCode:  statusCode,
		})
	}
}
