package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"depsilo/internal/cache"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TransparentPlan contains the protocol-specific decisions required to proxy
// one unmodified file. Path parsing, quarantine policy and TTL classification
// remain owned by the protocol Adapter.
type TransparentPlan struct {
	Path               string
	CacheKey           string
	TTL                time.Duration
	DefaultContentType string
	// Optional marks content a pool member may not offer at all, such as a
	// relayed Go checksum database. A member that answers 404 or 410 is
	// passed over for the next one whatever kind the cache gives the key,
	// and a 404 or 410 from the last member asked reaches the client as that
	// status instead of 502.
	Optional bool
	// PresenceIsAnswer marks optional content whose presence is itself what
	// the client asks about, as for the checksum database's "supported";
	// leave it unset for content that stays valid once fetched. A 404 or 410
	// from the pool then withdraws the cached copy instead of letting it
	// answer past its TTL. A pool that gives no such answer is covered by the
	// cached copy while there is one and is reported as 404 otherwise: a
	// client that reads 404 as "not offered here" must not be handed a
	// gateway error where no member could be asked.
	PresenceIsAnswer bool
	// Rewrite, when set, replaces a fetched body before the cache stores it,
	// given the pool member that served it, so hits and misses return the
	// same rewritten representation. It must stream rather than buffer the
	// body, and the returned reader owns closing body. The stored size is
	// then whatever the rewritten stream yields.
	Rewrite func(body io.ReadCloser, served *upstream.Upstream) io.ReadCloser
}

// TransparentProxy hides the common select/cache/fetch/stream/log pipeline
// shared by signed or otherwise unmodified repository trees.
type TransparentProxy struct {
	adapterType string
	cache       *cache.Manager
	selector    upstream.Selector
	database    *gorm.DB
	// RetryMissingArtifacts lets another pool member serve an immutable
	// artifact that the selected member answers with 404. Enable it only
	// for ecosystems whose pools mirror one repository and whose clients
	// verify artifacts against hashes in signed or source-bound metadata,
	// or, as for Helm, whose served metadata names artifacts relative to
	// Depsilo, so that an artifact one member lacks was listed by another
	// member's metadata. Metadata itself never moves on a 404: mixing
	// snapshots of different mirrors would break those consistency checks.
	RetryMissingArtifacts bool
}

type transparentUpstreamError struct {
	name  string
	cause error
	// revoked is set when the pool's answer withdraws the cached copy.
	revoked bool
	// absent is set for optional content when every member that answered
	// said it does not exist (404 or 410): an expected answer, not a fault.
	absent bool
}

func (failure *transparentUpstreamError) Error() string { return failure.cause.Error() }
func (failure *transparentUpstreamError) Unwrap() error { return failure.cause }

// AllowStaleFallback keeps a withdrawn copy from answering in place of the
// pool: the cache manager drops the entry when a fetch refuses the fallback.
// Every other failure may still be served from a stale copy.
func (failure *transparentUpstreamError) AllowStaleFallback() bool { return !failure.revoked }

// upstreamStatus returns the HTTP status an upstream answered with, or 0 when
// err is not an upstream error response.
func upstreamStatus(err error) int {
	var response *upstream.ResponseError
	if errors.As(err, &response) {
		return response.Code
	}
	return 0
}

// isAbsenceStatus reports whether an upstream status says the content does
// not exist there.
func isAbsenceStatus(status int) bool {
	return status == http.StatusNotFound || status == http.StatusGone
}

func NewTransparentProxy(adapterType string, manager *cache.Manager, selector upstream.Selector, database *gorm.DB) *TransparentProxy {
	return &TransparentProxy{
		adapterType: strings.TrimSpace(adapterType),
		cache:       manager,
		selector:    selector,
		database:    database,
	}
}

// Open returns the plan's content through the cache, filling a miss exactly
// as Serve does. A protocol that reads the content itself uses it so the
// entry it fills is the one Serve would have stored.
func (proxy *TransparentProxy) Open(ctx context.Context, plan TransparentPlan) (*cache.GetResult, error) {
	return proxy.cache.Get(ctx, plan.CacheKey, proxy.adapterType, plan.TTL, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		selected, err := proxy.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}
		retryMissing := plan.Optional || (proxy.RetryMissingArtifacts && db.ClassifyCacheKind(proxy.adapterType, plan.CacheKey) == db.CacheKindArtifact)
		otherAnswer := false
		fetched, served, err := fetchFromPool(ctx, proxy.selector, selected, retryMissing, plan.Optional, func(member *upstream.Upstream) (*upstream.FetchResult, error) {
			zap.L().Info("fetching from upstream",
				zap.String("adapter_type", proxy.adapterType),
				zap.String("path", plan.Path),
				zap.String("upstream", member.Name),
			)
			result, err := member.Fetch(ctx, "/"+strings.TrimPrefix(plan.Path, "/"))
			if status := upstreamStatus(err); status != 0 && !isAbsenceStatus(status) {
				otherAnswer = true
			}
			return result, err
		})
		if err != nil {
			missing := isAbsenceStatus(upstreamStatus(err))
			revoked := plan.Optional && plan.PresenceIsAnswer && missing
			absent := plan.Optional && missing && !otherAnswer
			return nil, "", 0, "", &transparentUpstreamError{name: served.Name, cause: err, revoked: revoked, absent: absent}
		}
		body, size := fetched.Body, fetched.Size
		if plan.Rewrite != nil {
			body, size = plan.Rewrite(body, served), -1
		}
		body = cache.WithResponseValidators(body, fetched.ETag, fetched.LastModified)
		return body, fetched.ContentType, size, served.Name, nil
	})
}

// Serve executes one transparent proxy request. The caller must validate the
// path and apply protocol policy before crossing this seam.
func (proxy *TransparentProxy) Serve(c *gin.Context, plan TransparentPlan) {
	start := time.Now()
	result, err := proxy.Open(c.Request.Context(), plan)
	if err != nil {
		upstreamName := ""
		var upstreamFailure *transparentUpstreamError
		if errors.As(err, &upstreamFailure) {
			upstreamName = upstreamFailure.name
		}
		if upstreamFailure != nil && upstreamFailure.absent {
			// Optional content the pool does not offer, such as the checksum
			// database behind a proxy that does not mirror it.
			zap.L().Debug("transparent upstream does not offer optional content",
				zap.String("adapter_type", proxy.adapterType),
				zap.String("path", plan.Path),
				zap.Error(err),
			)
		} else {
			zap.L().Error("transparent upstream fetch failed",
				zap.String("adapter_type", proxy.adapterType),
				zap.String("path", plan.Path),
				zap.Error(err),
			)
		}
		status, code := UpstreamFailureStatus(err)
		if status == http.StatusBadGateway && plan.Optional && plan.PresenceIsAnswer {
			// No member said the content is offered and no copy is cached.
			status, code = http.StatusNotFound, "NOT_FOUND"
		}
		LogAccess(c.Request.Context(), proxy.database, proxy.adapterType, c.Request.Method, plan.CacheKey, false, upstreamName, time.Since(start), status, c.ClientIP(), 0)
		c.JSON(status, gin.H{"code": code, "message": err.Error()})
		return
	}
	defer result.Reader.Close()

	// Manager exposes only allowlisted representation metadata. Replaying it on
	// both misses and hits preserves validators without forwarding credentials,
	// cookies, redirects, or hop-by-hop headers from an upstream response.
	for key, values := range result.Headers {
		c.Writer.Header()[key] = append([]string(nil), values...)
	}
	contentType := result.ContentType
	if contentType == "" {
		contentType = plan.DefaultContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
	}
	c.Header("Content-Type", contentType)
	if result.Size > 0 {
		c.Header("Content-Length", fmt.Sprintf("%d", result.Size))
	}
	c.Status(http.StatusOK)
	written, copyErr := io.Copy(c.Writer, result.Reader)
	if copyErr != nil {
		zap.L().Warn("copy transparent response to client failed",
			zap.String("adapter_type", proxy.adapterType),
			zap.String("key", plan.CacheKey),
			zap.Error(copyErr),
		)
	}

	LogAccessDelivery(c.Request.Context(), proxy.database, proxy.adapterType, c.Request.Method, plan.CacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), written, copyErr)
}
