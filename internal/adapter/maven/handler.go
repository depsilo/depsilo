package maven

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

func (h *Handler) Type() string { return "maven" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")
	if path == "" {
		c.Status(http.StatusNotFound)
		return
	}

	start := time.Now()
	cacheKey := CacheKey(path)

	// Determine TTL: metadata and snapshots are short, artifacts are long
	ttl := h.cfg.TTLBlob
	if strings.HasSuffix(path, "maven-metadata.xml") || strings.Contains(path, "-SNAPSHOT") {
		ttl = h.cfg.TTLIndex
	}

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "maven", ttl, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}
		zap.L().Info("fetching from maven upstream", zap.String("path", path), zap.String("upstream", ups.Name))
		fetchResult, err := ups.Fetch(ctx, "/"+path)
		if err != nil {
			return nil, "", 0, "", err
		}
		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, ups.Name, nil
	})

	if err != nil {
		zap.L().Error("failed to fetch maven artifact", zap.String("path", path), zap.Error(err))
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
	written, copyErr := io.Copy(c.Writer, result.Reader)
	if copyErr != nil {
		zap.L().Warn("copy to client failed", zap.String("key", cacheKey), zap.Error(copyErr))
	}

	adapter.LogAccess(h.db, "maven", c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), written)
}
