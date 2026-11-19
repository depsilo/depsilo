package adapter

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/accesslog"
	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/db"
	"depsilo/internal/requestid"
	"depsilo/internal/traffic"
)

// AuditLogger is the audit half of the access hook snapshot. It receives
// policy blocks only: ordinary package request outcomes are request history,
// which the access log owns, and are not chained into the audit log.
type AuditLogger interface {
	Log(entry db.AuditLog)
}

// accessHookSnapshot is the immutable recorder/audit pair a RequestScope
// carries by value, so one request always reports to one server owner.
type accessHookSnapshot struct {
	recorder accesslog.Recorder
	audit    AuditLogger
}

type suppressAccessLoggingContextKey struct{}

// SuppressAccessLogging marks requests initiated by an internal maintenance
// task. The marker is private to this package, so an external HTTP header
// cannot suppress End User access or policy-block records.
func SuppressAccessLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx := context.WithValue(request.Context(), suppressAccessLoggingContextKey{}, true)
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

// LogAccess submits an access log entry to the request's recorder snapshot.
// See accessHooksFor for a ctx without a RequestScope.
func LogAccess(ctx context.Context, database *gorm.DB, adapterType, method, cacheKey string, hit bool, upstreamName string, latency time.Duration, statusCode int, clientIP string, bytesSent int64) {
	logAccess(ctx, database, adapterType, method, cacheKey, hit, upstreamName, latency, statusCode, clientIP, bytesSent, "unknown", "response_completion_not_recorded")
}

// LogRefusedAccess records a request an adapter refused before it established
// which artifact the request names. The access entry keeps the requested key,
// so Admin logs show the response the client received. Readers derive no
// version for a refused response (accesslog.RecordedVersion): the file name in
// a refused path is the client's claim, not an identity Depsilo verified.
func LogRefusedAccess(ctx context.Context, database *gorm.DB, adapterType, method, cacheKey string, latency time.Duration, statusCode int, clientIP string) {
	logAccess(ctx, database, adapterType, method, cacheKey, false, "", latency, statusCode, clientIP, 0, "unknown", "response_completion_not_recorded")
}

// LogAccessDelivery records the actual stream completion separately from the
// HTTP status (which may already be 200 when a client disconnects).
func LogAccessDelivery(ctx context.Context, database *gorm.DB, adapterType, method, cacheKey string, hit bool, upstreamName string, latency time.Duration, statusCode int, clientIP string, bytesSent int64, deliveryErr error) {
	result, reason := "completed", "response_body_completed"
	if deliveryErr != nil {
		result, reason = "failed", "response_body_incomplete"
	}
	logAccess(ctx, database, adapterType, method, cacheKey, hit, upstreamName, latency, statusCode, clientIP, bytesSent, result, reason)
}

// logAccess writes the access entry. It is the one record of a package
// request outcome; the audit log keeps policy blocks and management events.
func logAccess(ctx context.Context, database *gorm.DB, adapterType, method, cacheKey string, hit bool, upstreamName string, latency time.Duration, statusCode int, clientIP string, bytesSent int64, deliveryResult, deliveryReason string) {
	if ctx != nil {
		if suppressed, _ := ctx.Value(suppressAccessLoggingContextKey{}).(bool); suppressed {
			return
		}
	}
	pkgName := packagekey.ExtractName(adapterType, cacheKey)
	if authenticated, ok := AuthenticatedArtifactCoordinateFromContext(ctx); ok &&
		authenticated.Ecosystem == adapterType {
		pkgName = authenticated.PackageName
	}
	now := time.Now().UTC()
	requestID := requestid.FromContext(ctx)
	cacheResult, cacheReason := accessCacheOutcome(hit, statusCode)
	originRequests, originBytes := traffic.AttributionFromContext(ctx).OriginTotals()
	hooks, observer := accessHooksFor(ctx)
	if observer != nil {
		observer.ObserveAccess(AccessObservation{
			AdapterType: adapterType,
			Method:      method,
			Hit:         hit,
			Upstream:    upstreamName,
			Latency:     latency,
			StatusCode:  statusCode,
			BytesSent:   bytesSent,
		})
	}

	if hooks != nil && hooks.recorder != nil {
		hooks.recorder.Record(accesslog.Event{
			RequestID:        requestID,
			AdapterType:      adapterType,
			Method:           method,
			CacheKey:         cacheKey,
			DependencyKind:   accesslog.DependencyRequestKind(adapterType, cacheKey),
			PackageName:      pkgName,
			Upstream:         upstreamName,
			ClientIP:         clientIP,
			Hit:              hit,
			LatencyMs:        latency.Milliseconds(),
			StatusCode:       statusCode,
			BytesSent:        bytesSent,
			UpstreamRequests: originRequests,
			UpstreamBytes:    originBytes,
			CacheResult:      cacheResult,
			CacheReason:      cacheReason,
			PolicyDecision:   "unknown",
			PolicyReason:     "not_recorded",
			DeliveryResult:   deliveryResult,
			DeliveryReason:   deliveryReason,
			At:               now,
		})
	} else {
		// No recorder in the request's scope (or no scope at all): write the raw
		// row directly. Keep this synchronous: spawning an unowned database goroutine here can
		// outlive the test/server resource that supplied database.
		entry := db.AccessLog{
			RequestID:        requestID,
			AdapterType:      adapterType,
			Method:           method,
			CacheKey:         cacheKey,
			PackageName:      pkgName,
			Hit:              hit,
			Upstream:         upstreamName,
			LatencyMs:        latency.Milliseconds(),
			StatusCode:       statusCode,
			ClientIP:         clientIP,
			BytesSent:        bytesSent,
			UpstreamRequests: originRequests,
			UpstreamBytes:    originBytes,
			CacheResult:      cacheResult,
			CacheReason:      cacheReason,
			PolicyDecision:   "unknown",
			PolicyReason:     "not_recorded",
			DeliveryResult:   deliveryResult,
			DeliveryReason:   deliveryReason,
			CreatedAt:        now,
		}
		if err := database.Create(&entry).Error; err != nil {
			zap.L().Warn("failed to write access log", zap.Error(err))
		}
	}
}

// LogPolicyBlock records a request that reached Depsilo but was refused before
// the cache/upstream path. A policy block is a security decision, so it is
// chained into the audit log; it writes no synthetic cache access, and the
// access log keeps every other request outcome.
func LogPolicyBlock(ctx context.Context, ecosystem, packageName, version string, statusCode int, clientIP string) {
	if ctx != nil {
		if suppressed, _ := ctx.Value(suppressAccessLoggingContextKey{}).(bool); suppressed {
			return
		}
	}
	hooks, _ := accessHooksFor(ctx)
	action := "metadata"
	if version != "" {
		action = "download"
	}
	logAuditOutcome(hooks, db.AuditLog{
		RequestID:   requestid.FromContext(ctx),
		Ecosystem:   ecosystem,
		PackageName: packageName,
		Version:     version,
		Action:      action,
		CacheResult: "blocked",
		ClientIP:    clientIP,
		StatusCode:  statusCode,
		CreatedAt:   time.Now().UTC(),
	})
}

// accessHooksFor returns the hooks bound by the request's RequestScope. The
// scope is the only source: a request without one (an isolated handler test,
// or an in-process call that bypasses the server's scope wrapper) gets no
// recorder, no audit logger, and no observer, so LogAccess falls back to the
// raw database row and policy blocks are not recorded. A scoped nil
// field means the same thing for that server owner.
func accessHooksFor(ctx context.Context) (*accessHookSnapshot, RequestObserver) {
	scope, ok := requestScopeFromContext(ctx)
	if !ok {
		return nil, nil
	}
	return &scope.access, scope.observer
}

func logAuditOutcome(hooks *accessHookSnapshot, entry db.AuditLog) {
	if hooks != nil && hooks.audit != nil {
		hooks.audit.Log(entry)
	}
}

func accessCacheOutcome(hit bool, statusCode int) (string, string) {
	if hit {
		return "hit", "cached_response_used"
	}
	if statusCode >= 400 {
		return "unknown", "not_recorded"
	}
	return "miss", "cached_response_not_used"
}
