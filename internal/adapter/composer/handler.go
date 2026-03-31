package composer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

type Handler struct {
	cacheMgr *cache.Manager
	selector upstream.Selector
	cfg      config.CacheConfig
	db       *gorm.DB
}

func New(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB) *Handler {
	return &Handler{cacheMgr: cacheMgr, selector: selector, cfg: cfg, db: database}
}

func (h *Handler) Type() string { return "composer" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")

	switch {
	case path == "packages.json":
		h.handlePackagesJSON(c)
	case strings.HasPrefix(path, "p2/"):
		h.proxyPassthrough(c, path, h.cfg.TTLIndex) // metadata, short TTL
	case strings.HasPrefix(path, "dist/"):
		h.proxyPassthrough(c, path, h.cfg.TTLBlob) // dist files, long TTL
	default:
		h.proxyPassthrough(c, path, h.cfg.TTLIndex)
	}
}

// handlePackagesJSON fetches packages.json from upstream and rewrites metadata-url.
func (h *Handler) handlePackagesJSON(c *gin.Context) {
	start := time.Now()
	baseURL := getBaseURL(c)
	cacheKey := PackagesCacheKey()

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "composer", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, err
		}

		zap.L().Info("fetching composer packages.json from upstream",
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.Fetch(ctx, "/packages.json")
		if err != nil {
			return nil, "", 0, err
		}

		body, err := io.ReadAll(fetchResult.Body)
		fetchResult.Body.Close()
		if err != nil {
			return nil, "", 0, err
		}

		// Rewrite metadata-url with placeholder (runtime base applied later)
		rewritten, err := RewritePackagesJSON(body, "__BASE_URL__")
		if err != nil {
			zap.L().Warn("composer packages.json rewrite failed, using original", zap.Error(err))
			rewritten = body
		}

		return io.NopCloser(strings.NewReader(string(rewritten))), "application/json", int64(len(rewritten)), nil
	})

	if err != nil {
		zap.L().Error("failed to fetch composer packages.json", zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
		return
	}
	defer result.Reader.Close()

	body, err := io.ReadAll(result.Reader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "read cache"})
		return
	}

	// Apply runtime base URL
	content := strings.ReplaceAll(string(body), "__BASE_URL__", baseURL)

	c.Header("Content-Type", "application/json")
	c.String(http.StatusOK, content)

	adapter.LogAccess(h.db, "composer", cacheKey, result.Hit, "", time.Since(start), http.StatusOK, c.ClientIP(), int64(len(content)))
}

// proxyPassthrough proxies a request to the upstream with caching, no content modification.
func (h *Handler) proxyPassthrough(c *gin.Context, path string, ttl time.Duration) {
	start := time.Now()
	cacheKey := "composer/" + path

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "composer", ttl, func(ctx context.Context) (io.ReadCloser, string, int64, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, err
		}

		zap.L().Info("fetching from composer upstream",
			zap.String("path", path),
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.Fetch(ctx, "/"+path)
		if err != nil {
			return nil, "", 0, err
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, nil
	})

	if err != nil {
		zap.L().Error("failed to fetch composer resource", zap.String("path", path), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
		return
	}
	defer result.Reader.Close()

	ct := result.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	c.Header("Content-Type", ct)
	if result.Size > 0 {
		c.Header("Content-Length", fmt.Sprintf("%d", result.Size))
	}
	c.Status(http.StatusOK)
	written, _ := io.Copy(c.Writer, result.Reader)

	adapter.LogAccess(h.db, "composer", cacheKey, result.Hit, "", time.Since(start), http.StatusOK, c.ClientIP(), written)
}

func getBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + c.Request.Host
}
