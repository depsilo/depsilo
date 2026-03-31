package goproxy

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

func (h *Handler) Type() string { return "go" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	// Use a single catch-all route since module paths contain slashes
	rg.GET("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")

	// Determine request type from path suffix
	switch {
	case strings.HasSuffix(path, "/@v/list"):
		module := strings.TrimSuffix(path, "/@v/list")
		h.proxyWithCache(c, module, ListCacheKey(module), "/"+path, h.cfg.TTLIndex)

	case strings.HasSuffix(path, "/@latest"):
		module := strings.TrimSuffix(path, "/@latest")
		h.proxyWithCache(c, module, LatestCacheKey(module), "/"+path, h.cfg.TTLIndex)

	case strings.HasSuffix(path, ".info"):
		h.proxyVersionFile(c, path, "info")

	case strings.HasSuffix(path, ".mod"):
		h.proxyVersionFile(c, path, "mod")

	case strings.HasSuffix(path, ".zip"):
		h.proxyVersionFile(c, path, "zip")

	default:
		c.Status(http.StatusNotFound)
	}
}

func (h *Handler) proxyVersionFile(c *gin.Context, path, ext string) {
	// path: github.com/gin-gonic/gin/@v/v1.9.1.info
	idx := strings.LastIndex(path, "/@v/")
	if idx < 0 {
		c.Status(http.StatusNotFound)
		return
	}
	module := path[:idx]
	versionFile := path[idx+len("/@v/"):] // "v1.9.1.info"
	version := strings.TrimSuffix(versionFile, "."+ext)

	var cacheKey string
	switch ext {
	case "info":
		cacheKey = InfoCacheKey(module, version)
	case "mod":
		cacheKey = ModCacheKey(module, version)
	case "zip":
		cacheKey = ZipCacheKey(module, version)
	}

	// Version-specific files are immutable -> long TTL
	h.proxyWithCache(c, module, cacheKey, "/"+path, h.cfg.TTLBlob)
}

func (h *Handler) proxyWithCache(c *gin.Context, module, cacheKey, upstreamPath string, ttl time.Duration) {
	start := time.Now()

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "go", ttl, func(ctx context.Context) (io.ReadCloser, string, int64, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, err
		}

		zap.L().Info("fetching from go proxy upstream",
			zap.String("path", upstreamPath),
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.Fetch(ctx, upstreamPath)
		if err != nil {
			return nil, "", 0, err
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, nil
	})

	if err != nil {
		zap.L().Error("failed to fetch go module", zap.String("path", upstreamPath), zap.Error(err))
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

	adapter.LogAccess(h.db, "go", cacheKey, result.Hit, "", time.Since(start), http.StatusOK, c.ClientIP(), written)
}
