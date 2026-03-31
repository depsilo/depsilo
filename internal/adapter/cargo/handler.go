package cargo

import (
	"context"
	"encoding/json"
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

func (h *Handler) Type() string { return "cargo" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/config.json", h.handleConfig)
	rg.GET("/api/v1/crates/:crate/:version/download", h.handleDownload)
	// Index routes: catch-all for prefix/crate paths
	rg.GET("/*path", h.handleIndex)
}

// handleConfig proxies and rewrites config.json to point dl at our proxy.
func (h *Handler) handleConfig(c *gin.Context) {
	start := time.Now()
	baseURL := getBaseURL(c)

	result, err := h.cacheMgr.Get(c.Request.Context(), ConfigCacheKey(), "cargo", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, err
		}

		zap.L().Info("fetching cargo config.json from upstream",
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.Fetch(ctx, "/config.json")
		if err != nil {
			return nil, "", 0, err
		}
		body, err := io.ReadAll(fetchResult.Body)
		fetchResult.Body.Close()
		if err != nil {
			return nil, "", 0, err
		}

		// Parse and rewrite dl field to use a placeholder (runtime base applied later)
		var cfg map[string]interface{}
		if err := json.Unmarshal(body, &cfg); err != nil {
			return nil, "", 0, err
		}
		cfg["dl"] = "__BASE_URL__/crates/api/v1/crates"
		rewritten, err := json.Marshal(cfg)
		if err != nil {
			return nil, "", 0, err
		}

		return io.NopCloser(strings.NewReader(string(rewritten))), "application/json", int64(len(rewritten)), nil
	})

	if err != nil {
		zap.L().Error("failed to fetch cargo config.json", zap.Error(err))
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

	adapter.LogAccess(h.db, "cargo", ConfigCacheKey(), result.Hit, "", time.Since(start), http.StatusOK, c.ClientIP(), int64(len(content)))
}

// handleIndex proxies crate index metadata (NDJSON).
func (h *Handler) handleIndex(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")

	// Skip paths that look like api routes (handled by handleDownload)
	if strings.HasPrefix(path, "api/") {
		c.Status(http.StatusNotFound)
		return
	}
	// Skip config.json (handled by handleConfig)
	if path == "config.json" {
		h.handleConfig(c)
		return
	}

	start := time.Now()

	// Extract crate name (last path segment) for logging
	parts := strings.Split(path, "/")
	crateName := parts[len(parts)-1]
	prefix := strings.Join(parts[:len(parts)-1], "/")
	cacheKey := IndexCacheKey(prefix, crateName)

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "cargo", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, err
		}

		zap.L().Info("fetching crate index from upstream",
			zap.String("crate", crateName),
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.Fetch(ctx, "/"+path)
		if err != nil {
			return nil, "", 0, err
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, nil
	})

	if err != nil {
		zap.L().Error("failed to fetch crate index", zap.String("crate", crateName), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
		return
	}
	defer result.Reader.Close()

	ct := result.ContentType
	if ct == "" {
		ct = "text/plain"
	}
	c.Header("Content-Type", ct)
	if result.Size > 0 {
		c.Header("Content-Length", fmt.Sprintf("%d", result.Size))
	}
	c.Status(http.StatusOK)
	written, _ := io.Copy(c.Writer, result.Reader)

	adapter.LogAccess(h.db, "cargo", cacheKey, result.Hit, "", time.Since(start), http.StatusOK, c.ClientIP(), written)
}

// handleDownload proxies crate file downloads (.crate files).
func (h *Handler) handleDownload(c *gin.Context) {
	crateName := c.Param("crate")
	version := c.Param("version")
	cacheKey := CrateCacheKey(crateName, version)
	start := time.Now()

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "cargo", h.cfg.TTLBlob, func(ctx context.Context) (io.ReadCloser, string, int64, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, err
		}

		zap.L().Info("fetching crate from upstream",
			zap.String("crate", crateName),
			zap.String("version", version),
			zap.String("upstream", ups.Name),
		)

		downloadPath := "/api/v1/crates/" + crateName + "/" + version + "/download"
		fetchResult, err := ups.Fetch(ctx, downloadPath)
		if err != nil {
			return nil, "", 0, err
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, nil
	})

	if err != nil {
		zap.L().Error("failed to fetch crate", zap.String("crate", crateName), zap.String("version", version), zap.Error(err))
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

	adapter.LogAccess(h.db, "cargo", cacheKey, result.Hit, "", time.Since(start), http.StatusOK, c.ClientIP(), written)
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
