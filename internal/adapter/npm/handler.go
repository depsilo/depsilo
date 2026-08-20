package npm

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

func (h *Handler) Type() string { return "npm" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	// Tarball routes (must be registered before metadata to avoid conflicts with /:package)
	rg.GET("/:package/-/:filename", h.handleTarball)
	rg.GET("/@:scope/:package/-/:filename", h.handleScopedTarball)
	// Metadata routes
	rg.GET("/:package", h.handleMetadata)
	rg.GET("/@:scope/:package", h.handleScopedMetadata)
}

func (h *Handler) handleMetadata(c *gin.Context) {
	pkg := c.Param("package")
	if pkg == "-" {
		c.Status(http.StatusNotFound)
		return
	}
	h.proxyMetadata(c, pkg, MetadataCacheKey(pkg), "/"+pkg)
}

func (h *Handler) handleScopedMetadata(c *gin.Context) {
	scope := c.Param("scope")
	pkg := c.Param("package")
	fullName := "@" + scope + "/" + pkg
	h.proxyMetadata(c, fullName, ScopedMetadataCacheKey(scope, pkg), "/"+fullName)
}

func (h *Handler) proxyMetadata(c *gin.Context, fullName, cacheKey, upstreamPath string) {
	start := time.Now()
	baseURL := getBaseURL(c)
	acceptHeader := c.GetHeader("Accept")

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "npm", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}

		zap.L().Info("fetching npm metadata from upstream",
			zap.String("package", fullName),
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.FetchWithHeaders(ctx, upstreamPath, map[string]string{
			"Accept": acceptHeader,
		})
		if err != nil {
			return nil, "", 0, "", err
		}

		body, err := io.ReadAll(fetchResult.Body)
		fetchResult.Body.Close()
		if err != nil {
			return nil, "", 0, "", err
		}

		// Rewrite tarball URLs with empty base (runtime base applied later)
		rewritten, err := RewriteTarballURLs(body, "")
		if err != nil {
			zap.L().Warn("npm url rewrite failed, using original", zap.Error(err))
			rewritten = body
		}

		ct := fetchResult.ContentType
		if ct == "" {
			ct = "application/json"
		}

		return io.NopCloser(strings.NewReader(string(rewritten))), ct, int64(len(rewritten)), ups.Name, nil
	})

	if err != nil {
		zap.L().Error("failed to get npm metadata", zap.String("package", fullName), zap.Error(err))
		adapter.LogAccess(c.Request.Context(), h.db, "npm", c.Request.Method, cacheKey, false, "", time.Since(start), http.StatusBadGateway, c.ClientIP(), 0)
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
		return
	}
	defer result.Reader.Close()

	body, err := io.ReadAll(result.Reader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "read cache"})
		return
	}

	// Apply the runtime base URL only to the rewritten tarball paths.
	content := ApplyBaseURL(body, baseURL)

	ct := result.ContentType
	if ct == "" {
		ct = "application/json"
	}
	c.Header("Content-Type", ct)
	c.String(http.StatusOK, string(content))

	adapter.LogAccess(c.Request.Context(), h.db, "npm", c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), int64(len(content)))
}

func (h *Handler) handleTarball(c *gin.Context) {
	pkg := c.Param("package")
	filename := c.Param("filename")
	h.proxyTarball(c, pkg, filename, TarballCacheKey(pkg, filename), "/"+pkg+"/-/"+filename)
}

func (h *Handler) handleScopedTarball(c *gin.Context) {
	scope := c.Param("scope")
	pkg := c.Param("package")
	filename := c.Param("filename")
	fullName := "@" + scope + "/" + pkg
	h.proxyTarball(c, fullName, filename, ScopedTarballCacheKey(scope, pkg, filename), "/"+fullName+"/-/"+filename)
}

func (h *Handler) proxyTarball(c *gin.Context, fullName, filename, cacheKey, upstreamPath string) {
	// Quarantine gate — parse version from filename and consult the
	// supply-chain policy before any upstream fetch. A blocked
	// version returns 451 directly; allowed paths fall through to
	// the cache + upstream flow unchanged.
	if version := packagekey.ParseNpmFilename(fullName, filename); version != "" {
		if blocked := adapter.QuarantineGate(c, "npm", fullName, version); blocked {
			return
		}
	}
	start := time.Now()

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "npm", h.cfg.TTLBlob, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}

		zap.L().Info("fetching npm tarball from upstream",
			zap.String("package", fullName),
			zap.String("filename", filename),
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.Fetch(ctx, upstreamPath)
		if err != nil {
			return nil, "", 0, "", err
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, ups.Name, nil
	})

	if err != nil {
		zap.L().Error("failed to get npm tarball", zap.String("package", fullName), zap.Error(err))
		adapter.LogAccess(c.Request.Context(), h.db, "npm", c.Request.Method, cacheKey, false, "", time.Since(start), http.StatusBadGateway, c.ClientIP(), 0)
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

	adapter.LogAccess(c.Request.Context(), h.db, "npm", c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), written)
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
