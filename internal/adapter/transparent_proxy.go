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
}

// TransparentProxy hides the common select/cache/fetch/stream/log pipeline
// shared by signed or otherwise unmodified repository trees.
type TransparentProxy struct {
	adapterType string
	cache       *cache.Manager
	selector    upstream.Selector
	database    *gorm.DB
}

type transparentUpstreamError struct {
	name  string
	cause error
}

func (failure *transparentUpstreamError) Error() string { return failure.cause.Error() }
func (failure *transparentUpstreamError) Unwrap() error { return failure.cause }

func NewTransparentProxy(adapterType string, manager *cache.Manager, selector upstream.Selector, database *gorm.DB) *TransparentProxy {
	return &TransparentProxy{
		adapterType: strings.TrimSpace(adapterType),
		cache:       manager,
		selector:    selector,
		database:    database,
	}
}

// Serve executes one transparent proxy request. The caller must validate the
// path and apply protocol policy before crossing this seam.
func (proxy *TransparentProxy) Serve(c *gin.Context, plan TransparentPlan) {
	start := time.Now()
	result, err := proxy.cache.Get(c.Request.Context(), plan.CacheKey, proxy.adapterType, plan.TTL, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		selected, err := proxy.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}
		zap.L().Info("fetching from upstream",
			zap.String("adapter_type", proxy.adapterType),
			zap.String("path", plan.Path),
			zap.String("upstream", selected.Name),
		)
		fetched, err := selected.Fetch(ctx, "/"+strings.TrimPrefix(plan.Path, "/"))
		if err != nil {
			return nil, "", 0, "", &transparentUpstreamError{name: selected.Name, cause: err}
		}
		body := cache.WithResponseValidators(fetched.Body, fetched.ETag, fetched.LastModified)
		return body, fetched.ContentType, fetched.Size, selected.Name, nil
	})
	if err != nil {
		upstreamName := ""
		var upstreamFailure *transparentUpstreamError
		if errors.As(err, &upstreamFailure) {
			upstreamName = upstreamFailure.name
		}
		zap.L().Error("transparent upstream fetch failed",
			zap.String("adapter_type", proxy.adapterType),
			zap.String("path", plan.Path),
			zap.Error(err),
		)
		LogAccess(c.Request.Context(), proxy.database, proxy.adapterType, c.Request.Method, plan.CacheKey, false, upstreamName, time.Since(start), http.StatusBadGateway, c.ClientIP(), 0)
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
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

	LogAccess(c.Request.Context(), proxy.database, proxy.adapterType, c.Request.Method, plan.CacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), written)
}
