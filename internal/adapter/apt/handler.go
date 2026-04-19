package apt

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

// Handler implements the APT adapter.
// APT responses are fully passthrough — no content modification — to preserve GPG signatures.
type Handler struct {
	cacheMgr *cache.Manager
	selector upstream.Selector
	cfg      config.CacheConfig
	db       *gorm.DB
}

func New(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB) *Handler {
	return &Handler{
		cacheMgr: cacheMgr,
		selector: selector,
		cfg:      cfg,
		db:       database,
	}
}

func (h *Handler) Type() string { return "apt" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/:repo/dists/*filepath", h.handleRequest)
	rg.GET("/:repo/pool/*filepath", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	repo := c.Param("repo")
	filepath := c.Param("filepath")
	start := time.Now()

	// Build the full upstream path: /{repo}/dists/{filepath} or /{repo}/pool/{filepath}
	// Gin strips the matched prefix, so we reconstruct from the full URL path
	fullPath := c.Request.URL.Path
	// Remove the /apt prefix to get the upstream path
	upstreamPath := fullPath[len("/apt"):]

	cacheKey := CacheKey(repo, upstreamPath)

	ttl := h.cfg.TTLBlob
	if IsMetadata(filepath) {
		ttl = h.cfg.TTLIndex
	}

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "apt", ttl, func(ctx context.Context) (io.ReadCloser, string, int64, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, err
		}

		zap.L().Info("fetching from apt upstream",
			zap.String("upstream", ups.Name),
			zap.String("path", upstreamPath),
		)

		fetchResult, err := ups.Fetch(ctx, upstreamPath)
		if err != nil {
			return nil, "", 0, err
		}

		if fetchResult.StatusCode == http.StatusNotFound {
			fetchResult.Body.Close()
			return nil, "", 0, fmt.Errorf("not found: %s", upstreamPath)
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, nil
	})

	if err != nil {
		zap.L().Error("apt fetch failed",
			zap.String("path", upstreamPath),
			zap.Error(err),
		)
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
		return
	}
	defer result.Reader.Close()

	ct := result.ContentType
	if ct == "" {
		ct = detectContentType(filepath)
	}
	c.Header("Content-Type", ct)
	if result.Size > 0 {
		c.Header("Content-Length", fmt.Sprintf("%d", result.Size))
	}
	c.Status(http.StatusOK)
	written, copyErr := io.Copy(c.Writer, result.Reader)
	if copyErr != nil {
		zap.L().Warn("copy to client failed", zap.String("key", cacheKey), zap.Error(copyErr))
	}

	adapter.LogAccess(h.db, "apt", c.Request.Method, cacheKey, result.Hit, "", time.Since(start), http.StatusOK, c.ClientIP(), written)
}

func detectContentType(path string) string {
	switch {
	case len(path) > 4 && path[len(path)-4:] == ".deb":
		return "application/vnd.debian.binary-package"
	case len(path) > 3 && path[len(path)-3:] == ".gz":
		return "application/gzip"
	case len(path) > 3 && path[len(path)-3:] == ".xz":
		return "application/x-xz"
	default:
		return "application/octet-stream"
	}
}

// Ensure compile-time interface compliance.
var _ interface{ Type() string } = (*Handler)(nil)
