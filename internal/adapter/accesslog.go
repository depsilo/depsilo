package adapter

import (
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/accesslog"
	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/db"
)

// auditLogger is the optional Pro audit logger, set via SetAuditLogger.
var auditLogger interface {
	Log(entry db.AuditLog)
}

// recorder is the optional rollup recorder, set via SetRecorder. When
// nil, LogAccess writes raw rows synchronously through a goroutine
// (legacy behavior). When set, the recorder owns both the raw write
// and the hourly/package-daily rollup aggregation.
var recorder accesslog.Recorder

// SetAuditLogger sets the audit logger used by LogAccess.
func SetAuditLogger(l interface{ Log(entry db.AuditLog) }) {
	auditLogger = l
}

// SetRecorder wires the rollup recorder. Pass nil to fall back to the
// raw-only legacy path (useful for tests and emergency disable).
func SetRecorder(r accesslog.Recorder) {
	recorder = r
}

// LogAccess asynchronously writes an access log entry.
func LogAccess(database *gorm.DB, adapterType, method, cacheKey string, hit bool, upstreamName string, latency time.Duration, statusCode int, clientIP string, bytesSent int64) {
	pkgName := packagekey.ExtractName(adapterType, cacheKey)
	now := time.Now().UTC()

	if recorder != nil {
		recorder.Record(accesslog.Event{
			AdapterType: adapterType,
			Method:      method,
			CacheKey:    cacheKey,
			PackageName: pkgName,
			Upstream:    upstreamName,
			ClientIP:    clientIP,
			Hit:         hit,
			LatencyMs:   latency.Milliseconds(),
			StatusCode:  statusCode,
			BytesSent:   bytesSent,
			At:          now,
		})
	} else {
		// Fallback: recorder not initialized yet (e.g. server bootstrap or
		// tests). Write raw row synchronously through a goroutine, matching
		// the pre-rollup behavior.
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
			CreatedAt:   now,
		}
		go func() {
			if err := database.Create(&entry).Error; err != nil {
				zap.L().Warn("failed to write access log", zap.Error(err))
			}
		}()
	}

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
