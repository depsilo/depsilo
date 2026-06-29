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
	// Gin doesn't allow catch-all wildcard with sibling routes,
	// so use a single catch-all and dispatch internally.
	rg.GET("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")

	switch {
	case path == "config.json":
		h.handleConfig(c)
	case strings.HasPrefix(path, "api/v1/crates/"):
		// api/v1/crates/{crate}/{version}/download
		parts := strings.Split(strings.TrimPrefix(path, "api/v1/crates/"), "/")
		if len(parts) >= 3 && parts[2] == "download" {
			c.Params = append(c.Params, gin.Param{Key: "crate", Value: parts[0]})
			c.Params = append(c.Params, gin.Param{Key: "version", Value: parts[1]})
			h.handleDownload(c)
		} else {
			c.Status(http.StatusNotFound)
		}
	default:
		h.handleIndex(c)
	}
}

// handleConfig proxies and rewrites config.json to point dl at our proxy.
func (h *Handler) handleConfig(c *gin.Context) {
	start := time.Now()
	baseURL := getBaseURL(c)

	result, err := h.cacheMgr.Get(c.Request.Context(), ConfigCacheKey(), "cargo", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}

		zap.L().Info("fetching cargo config.json from upstream",
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.Fetch(ctx, "/config.json")
		if err != nil {
			return nil, "", 0, "", err
		}
		body, err := io.ReadAll(fetchResult.Body)
		fetchResult.Body.Close()
		if err != nil {
			return nil, "", 0, "", err
		}

		// Parse and rewrite dl field to use a placeholder (runtime base applied later)
		var cfg map[string]interface{}
		if err := json.Unmarshal(body, &cfg); err != nil {
			return nil, "", 0, "", err
		}
		cfg["dl"] = "__BASE_URL__/crates/api/v1/crates"
		rewritten, err := json.Marshal(cfg)
		if err != nil {
			return nil, "", 0, "", err
		}

		return io.NopCloser(strings.NewReader(string(rewritten))), "application/json", int64(len(rewritten)), ups.Name, nil
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

	adapter.LogAccess(h.db, "cargo", c.Request.Method, ConfigCacheKey(), result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), int64(len(content)))
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

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "cargo", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}

		zap.L().Info("fetching crate index from upstream",
			zap.String("crate", crateName),
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.Fetch(ctx, "/"+path)
		if err != nil {
			return nil, "", 0, "", err
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, ups.Name, nil
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
	written, copyErr := io.Copy(c.Writer, result.Reader)
	if copyErr != nil {
		zap.L().Warn("copy to client failed", zap.String("key", cacheKey), zap.Error(copyErr))
	}

	adapter.LogAccess(h.db, "cargo", c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), written)
}

// handleDownload proxies crate file downloads (.crate files).
func (h *Handler) handleDownload(c *gin.Context) {
	crateName := c.Param("crate")
	version := c.Param("version")
	// Quarantine gate. (crate, version) come pre-parsed from the
	// /api/v1/crates/<crate>/<version>/download path by
	// handleRequest above — no extra parsing needed here.
	if blocked := adapter.QuarantineGate(c, "cargo", crateName, version); blocked {
		return
	}
	cacheKey := CrateCacheKey(crateName, version)
	start := time.Now()

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "cargo", h.cfg.TTLBlob, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}

		zap.L().Info("fetching crate from upstream",
			zap.String("crate", crateName),
			zap.String("version", version),
			zap.String("upstream", ups.Name),
		)

		downloadPath := "/api/v1/crates/" + crateName + "/" + version + "/download"
		fetchResult, err := ups.Fetch(ctx, downloadPath)
		if err != nil {
			return nil, "", 0, "", err
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, ups.Name, nil
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
	written, copyErr := io.Copy(c.Writer, result.Reader)
	if copyErr != nil {
		zap.L().Warn("copy to client failed", zap.String("key", cacheKey), zap.Error(copyErr))
	}

	adapter.LogAccess(h.db, "cargo", c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), written)
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
