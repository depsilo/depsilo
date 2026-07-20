package pypi

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
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

// Handler implements the PyPI adapter.
type Handler struct {
	cacheMgr   *cache.Manager
	selector   upstream.Selector
	cfg        config.CacheConfig
	db         *gorm.DB
	pathPrefix string
	adapterID  string
}

// New creates a new PyPI handler.
func New(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB) *Handler {
	return &Handler{
		cacheMgr:   cacheMgr,
		selector:   selector,
		cfg:        cfg,
		db:         database,
		pathPrefix: "/pypi",
		adapterID:  "pypi",
	}
}

func NewWithPrefix(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB, pathPrefix, adapterID string) *Handler {
	return &Handler{
		cacheMgr:   cacheMgr,
		selector:   selector,
		cfg:        cfg,
		db:         database,
		pathPrefix: pathPrefix,
		adapterID:  adapterID,
	}
}

func (h *Handler) Type() string { return h.adapterID }

// Register mounts PyPI routes.
func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/simple/", h.handleSimpleIndex)
	rg.GET("/simple/:package/", h.handlePackageIndex)
	rg.GET("/simple/:package", h.handlePackageRedirect)
	rg.GET("/files/*filepath", h.handleFileDownload)
}

// handleSimpleIndex proxies the top-level simple index.
func (h *Handler) handleSimpleIndex(c *gin.Context) {
	ups, err := h.selector.Select(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": "ALL_UPSTREAMS_UNHEALTHY", "message": err.Error()})
		return
	}

	result, err := ups.Fetch(c.Request.Context(), "/simple/")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
		return
	}
	defer result.Body.Close()

	c.Header("Content-Type", result.ContentType)
	c.Status(result.StatusCode)
	_, copyErr := io.Copy(c.Writer, result.Body)
	if copyErr != nil {
		zap.L().Warn("copy to client failed", zap.String("key", h.pathPrefix+"/simple/"), zap.Error(copyErr))
	}
}

// handlePackageRedirect redirects /simple/:package to /simple/:package/
func (h *Handler) handlePackageRedirect(c *gin.Context) {
	pkg := c.Param("package")
	c.Redirect(http.StatusMovedPermanently, h.pathPrefix+"/simple/"+pkg+"/")
}

// handlePackageIndex proxies and caches a package's simple index, rewriting URLs.
func (h *Handler) handlePackageIndex(c *gin.Context) {
	pkg := c.Param("package")
	cacheKey := IndexCacheKey(h.adapterID, pkg)
	start := time.Now()

	baseURL := getBaseURL(c)

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, h.adapterID, h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		// This closure only runs after TTL expiry, on a miss, or for an
		// operator-triggered refresh. Fresh hits therefore do not even query
		// validators locally, let alone contact upstream.
		var cachedValidators db.CacheEntry
		_ = h.db.WithContext(ctx).Select("etag", "last_modified").
			Where("key = ?", cacheKey).Find(&cachedValidators).Error

		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}

		zap.L().Info("fetching package index from upstream",
			zap.String("package", pkg),
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.FetchWithHeaders(ctx, "/simple/"+pkg+"/", map[string]string{
			"If-None-Match":     cachedValidators.ETag,
			"If-Modified-Since": cachedValidators.LastModified,
		})
		if err != nil {
			return nil, "", 0, ups.Name, err
		}
		if fetchResult.StatusCode == http.StatusNotModified {
			fetchResult.Body.Close()
			return nil, "", 0, ups.Name, cache.ErrNotModified
		}

		// Read the HTML to rewrite URLs
		body, err := io.ReadAll(fetchResult.Body)
		fetchResult.Body.Close()
		if err != nil {
			return nil, "", 0, ups.Name, err
		}

		html := string(body)
		// Rewrite all download URLs to point through our proxy (stored with empty base)
		html = RewriteURLs(html, "", h.pathPrefix)

		bodyReader := io.NopCloser(strings.NewReader(html))
		return cache.WithResponseValidators(bodyReader, fetchResult.ETag, fetchResult.LastModified), fetchResult.ContentType, int64(len(html)), ups.Name, nil
	})

	if err != nil {
		zap.L().Error("failed to get package index", zap.String("package", pkg), zap.Error(err))
		status := http.StatusBadGateway
		code := "UPSTREAM_UNAVAILABLE"
		if strings.Contains(err.Error(), "returned 404") {
			status = http.StatusNotFound
			code = "NOT_FOUND"
		}
		c.JSON(status, gin.H{"code": code, "message": err.Error()})
		return
	}
	defer result.Reader.Close()

	// Read cached HTML and apply runtime URL rewriting with actual base URL
	body, err := io.ReadAll(result.Reader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "read cache"})
		return
	}

	// The cached version has relative /pypi/files/... paths.
	// Replace them with the full base URL for the client.
	html := string(body)
	html = strings.ReplaceAll(html, `href="`+h.pathPrefix+`/files/`, `href="`+baseURL+h.pathPrefix+`/files/`)

	ct := result.ContentType
	if ct == "" {
		ct = "text/html"
	}
	c.Header("Content-Type", ct)
	c.String(http.StatusOK, html)

	adapter.LogAccess(c.Request.Context(), h.db, h.adapterID, c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), int64(len(html)))
}

// handleFileDownload proxies and caches package file downloads.
func (h *Handler) handleFileDownload(c *gin.Context) {
	filepath := c.Param("filepath")
	// Quarantine gate. PyPI file paths look like
	// "/packages/source/r/requests/requests-2.32.3.tar.gz" or the
	// equivalent wheel; the last component carries (pkg, version)
	// in PEP 427 / 625 form. Only fire the check for the extra-
	// indexes / main PyPI route — we DO NOT block on the extra-index
	// adapterID layer here since each extra-index is its own
	// ecosystem name and may not have a quarantine threshold; the
	// checker short-circuits on threshold-0 ecosystems anyway, so
	// passing adapterID is safe.
	if base := lastPathSegment(filepath); base != "" {
		if pkg, version := packagekey.ParsePypiFilename(base); pkg != "" && version != "" {
			if blocked := adapter.QuarantineGate(c, h.adapterID, pkg, version); blocked {
				return
			}
		}
	}
	cacheKey := FileCacheKey(h.adapterID, filepath)
	start := time.Now()

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, h.adapterID, h.cfg.TTLBlob, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}

		zap.L().Info("fetching file from upstream",
			zap.String("filepath", filepath),
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.Fetch(ctx, filepath)
		if err != nil {
			return nil, "", 0, "", err
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, ups.Name, nil
	})

	if err != nil {
		zap.L().Error("failed to get file", zap.String("filepath", filepath), zap.Error(err))
		status := http.StatusBadGateway
		code := "UPSTREAM_UNAVAILABLE"
		if strings.Contains(err.Error(), "returned 404") {
			status = http.StatusNotFound
			code = "NOT_FOUND"
		}
		c.JSON(status, gin.H{"code": code, "message": err.Error()})
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

	adapter.LogAccess(c.Request.Context(), h.db, h.adapterID, c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), written)
}

// getBaseURL extracts the base URL from the request (scheme + host).
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

// lastPathSegment returns the substring after the final '/'. Used by
// the quarantine gate to find the artifact filename inside the
// /files/*filepath catch-all.
func lastPathSegment(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
