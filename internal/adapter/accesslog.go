package adapter

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/accesslog"
	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/db"
)

// AuditLogger is the audit half of the access hook snapshot.
type AuditLogger interface {
	Log(entry db.AuditLog)
}

// accessHookSnapshot is immutable after publication. RequestScope stores one
// by value; the atomic pointer below exists only for unscoped compatibility
// callers.
type accessHookSnapshot struct {
	recorder accesslog.Recorder
	audit    AuditLogger
}

var accessHooks atomic.Pointer[accessHookSnapshot]

type suppressAccessLoggingContextKey struct{}

// SuppressAccessLogging marks requests initiated by an internal maintenance
// task. The marker is private to this package, so an external HTTP header
// cannot suppress End User access or audit records.
func SuppressAccessLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx := context.WithValue(request.Context(), suppressAccessLoggingContextKey{}, true)
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

// InstallAccessHooks atomically installs compatibility hooks for unscoped
// callers and returns an idempotent release function. Releasing an older owner
// cannot clear a snapshot installed by a newer owner.
func InstallAccessHooks(recorder accesslog.Recorder, audit AuditLogger) func() {
	owned := &accessHookSnapshot{recorder: recorder, audit: audit}
	accessHooks.Store(owned)
	return func() {
		accessHooks.CompareAndSwap(owned, nil)
	}
}

// SetAuditLogger sets the audit logger used by LogAccess.
// Deprecated: production servers should bind both hooks with NewRequestScope.
// This compatibility setter remains concurrency-safe.
func SetAuditLogger(l AuditLogger) {
	updateAccessHooks(func(next *accessHookSnapshot) { next.audit = l })
}

// SetRecorder wires the rollup recorder. Pass nil to fall back to the
// raw-only legacy path (useful for tests and emergency disable).
// Deprecated: production servers should use NewRequestScope.
func SetRecorder(r accesslog.Recorder) {
	updateAccessHooks(func(next *accessHookSnapshot) { next.recorder = r })
}

func updateAccessHooks(update func(*accessHookSnapshot)) {
	for {
		current := accessHooks.Load()
		next := &accessHookSnapshot{}
		if current != nil {
			*next = *current
		}
		update(next)
		if next.recorder == nil && next.audit == nil {
			next = nil
		}
		if accessHooks.CompareAndSwap(current, next) {
			return
		}
	}
}

// LogAccess submits an access log entry to the request's recorder snapshot.
// When ctx has no RequestScope, the process-wide compatibility hooks are used.
func LogAccess(ctx context.Context, database *gorm.DB, adapterType, method, cacheKey string, hit bool, upstreamName string, latency time.Duration, statusCode int, clientIP string, bytesSent int64) {
	if ctx != nil {
		if suppressed, _ := ctx.Value(suppressAccessLoggingContextKey{}).(bool); suppressed {
			return
		}
	}
	pkgName := packagekey.ExtractName(adapterType, cacheKey)
	now := time.Now().UTC()
	hooks := accessHooks.Load()
	if scope, ok := requestScopeFromContext(ctx); ok {
		// A scoped nil recorder/audit pair is intentional and authoritative.
		hooks = &scope.access
	}

	if hooks != nil && hooks.recorder != nil {
		hooks.recorder.Record(accesslog.Event{
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
		// Fallback: recorder not initialized yet (e.g. isolated adapter tests).
		// Keep this synchronous: spawning an unowned database goroutine here can
		// outlive the test/server resource that supplied database.
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
		if err := database.Create(&entry).Error; err != nil {
			zap.L().Warn("failed to write access log", zap.Error(err))
		}
	}

	if hooks != nil && hooks.audit != nil {
		action := "download"
		if db.ClassifyCacheKind(adapterType, cacheKey) == db.CacheKindMetadata {
			action = "metadata"
		}
		cacheResult := "miss"
		if hit {
			cacheResult = "hit"
		}
		if statusCode >= 500 {
			cacheResult = "error"
		}
		hooks.audit.Log(db.AuditLog{
			Ecosystem:   adapterType,
			PackageName: pkgName,
			Version:     packagekey.ExtractVersion(adapterType, cacheKey),
			Action:      action,
			CacheResult: cacheResult,
			ClientIP:    clientIP,
			LatencyMs:   latency.Milliseconds(),
			BytesSent:   bytesSent,
			StatusCode:  statusCode,
			CreatedAt:   now,
		})
	}
}
