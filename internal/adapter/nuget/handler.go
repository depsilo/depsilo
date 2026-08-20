package nuget

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
	"depsilo/internal/adapter/packagekey"
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

func (h *Handler) Type() string { return "nuget" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")
	if path == "" {
		c.Status(http.StatusNotFound)
		return
	}

	// Quarantine gate. Only the flat-container .nupkg downloads
	// resolve to (id, version); service-index and registration JSON
	// pass through ungated.
	if id, version := packagekey.ParseNugetPath(path); id != "" && version != "" {
		if blocked := adapter.QuarantineGate(c, "nuget", id, version); blocked {
			return
		}
	}

	switch {
	case path == "v3/index.json":
		h.handleServiceIndex(c)
	default:
		h.handlePassthrough(c, path)
	}
}

// handleServiceIndex fetches the NuGet V3 service index and rewrites @id fields.
func (h *Handler) handleServiceIndex(c *gin.Context) {
	start := time.Now()
	baseURL := getBaseURL(c)
	cacheKey := CacheKey("v3/index.json")

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "nuget", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}
		zap.L().Info("fetching nuget service index from upstream", zap.String("upstream", ups.Name))

		fetchResult, err := ups.Fetch(ctx, "/v3/index.json")
		if err != nil {
			return nil, "", 0, "", err
		}
		body, err := io.ReadAll(fetchResult.Body)
		fetchResult.Body.Close()
		if err != nil {
			return nil, "", 0, "", err
		}

		// Store with placeholder for runtime base URL replacement
		rewritten, err := RewriteServiceIndex(body, "__BASE_URL__")
		if err != nil {
			return nil, "", 0, "", err
		}

		return io.NopCloser(strings.NewReader(string(rewritten))), "application/json", int64(len(rewritten)), ups.Name, nil
	})

	if err != nil {
		zap.L().Error("failed to fetch nuget service index", zap.Error(err))
		adapter.LogAccess(c.Request.Context(), h.db, "nuget", c.Request.Method, cacheKey, false, "", time.Since(start), http.StatusBadGateway, c.ClientIP(), 0)
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

	adapter.LogAccess(c.Request.Context(), h.db, "nuget", c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), int64(len(content)))
}

// handlePassthrough proxies all other NuGet requests (registration, package download, search).
func (h *Handler) handlePassthrough(c *gin.Context, path string) {
	start := time.Now()
	cacheKey := CacheKey(path)

	// .nupkg files get long TTL, everything else short
	ttl := h.cfg.TTLIndex
	if strings.HasSuffix(path, ".nupkg") {
		ttl = h.cfg.TTLBlob
	}

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "nuget", ttl, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}
		zap.L().Info("fetching from nuget upstream", zap.String("path", path), zap.String("upstream", ups.Name))
		fetchResult, err := ups.Fetch(ctx, "/"+path)
		if err != nil {
			return nil, "", 0, "", err
		}
		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, ups.Name, nil
	})

	if err != nil {
		zap.L().Error("failed to fetch nuget resource", zap.String("path", path), zap.Error(err))
		adapter.LogAccess(c.Request.Context(), h.db, "nuget", c.Request.Method, cacheKey, false, "", time.Since(start), http.StatusBadGateway, c.ClientIP(), 0)
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

	adapter.LogAccess(c.Request.Context(), h.db, "nuget", c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), written)
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
