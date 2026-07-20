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
		h.handleDist(c, path) // dist archives via the injected mirror template
	default:
		h.proxyPassthrough(c, path, h.cfg.TTLIndex)
	}
}

// handlePackagesJSON fetches packages.json from upstream and rewrites metadata-url.
func (h *Handler) handlePackagesJSON(c *gin.Context) {
	start := time.Now()
	baseURL := getBaseURL(c)
	cacheKey := PackagesCacheKey()

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "composer", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}

		zap.L().Info("fetching composer packages.json from upstream",
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.Fetch(ctx, "/packages.json")
		if err != nil {
			return nil, "", 0, "", err
		}

		body, err := io.ReadAll(fetchResult.Body)
		fetchResult.Body.Close()
		if err != nil {
			return nil, "", 0, "", err
		}

		// Rewrite metadata-url with placeholder (runtime base applied later)
		rewritten, err := RewritePackagesJSON(body, "__BASE_URL__")
		if err != nil {
			zap.L().Warn("composer packages.json rewrite failed, using original", zap.Error(err))
			rewritten = body
		}

		return io.NopCloser(strings.NewReader(string(rewritten))), "application/json", int64(len(rewritten)), ups.Name, nil
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

	adapter.LogAccess(c.Request.Context(), h.db, "composer", c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), int64(len(content)))
}

// handleDist serves a dist archive requested through the mirror
// template injected into packages.json. The mirror URL only carries
// (package, version_normalized, reference); the real download
// location lives in the p2 metadata, so the handler resolves the
// version manifest first — which also yields the pretty version the
// quarantine gate needs.
func (h *Handler) handleDist(c *gin.Context, path string) {
	vendor, pkg, versionNorm, reference, ext, ok := ParseDistPath(path)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "malformed composer dist path"})
		return
	}
	fullName := vendor + "/" + pkg

	entry, err := h.resolveDistEntry(c.Request.Context(), vendor, pkg, versionNorm, reference)
	if err != nil {
		zap.L().Error("failed to resolve composer dist metadata", zap.String("package", fullName), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
		return
	}
	if entry == nil || entry.Dist.URL == "" {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "version not present in upstream metadata"})
		return
	}
	// The extension must match the metadata's dist type: it feeds the
	// cache key, and accepting arbitrary client-supplied values would
	// let unauthenticated requests store the same artifact under
	// unbounded keys (cache/storage amplification).
	if want := entry.Dist.Type; (want != "" && ext != want) || (want == "" && ext != "zip") {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "dist type mismatch"})
		return
	}
	// The dist URL comes from upstream-controlled metadata and is
	// fetched as an absolute URL — refuse anything but plain HTTP(S)
	// so metadata can't point the proxy at other schemes.
	distURL := entry.Dist.URL
	if !strings.HasPrefix(distURL, "https://") && !strings.HasPrefix(distURL, "http://") {
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_ERROR", "message": "unsupported dist url scheme"})
		return
	}

	// Quarantine gate with the pretty version — the same string the
	// composer publish-time resolver matches against p2 metadata.
	// NOTE: composer falls back to the original dist URL on 451
	// (see the enforcement caveat in rewriter.go), so this blocks
	// best-effort and records the audit event; it is not airtight
	// against a client with direct registry egress.
	if blocked := adapter.QuarantineGate(c, "composer", fullName, entry.Version); blocked {
		return
	}

	start := time.Now()
	cacheKey := DistCacheKey(vendor, pkg, reference, ext)

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "composer", h.cfg.TTLBlob, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}

		zap.L().Info("fetching composer dist",
			zap.String("package", fullName),
			zap.String("url", distURL),
			zap.String("upstream", ups.Name),
		)

		// Dist archives live wherever the metadata points (GitHub
		// for packagist, the mirror's own storage for CN mirrors) —
		// fetched absolutely, but through the selected upstream's
		// client so its proxy setting applies.
		fetchResult, err := ups.FetchURL(ctx, distURL)
		if err != nil {
			return nil, "", 0, "", err
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, ups.Name, nil
	})

	if err != nil {
		zap.L().Error("failed to fetch composer dist", zap.String("package", fullName), zap.Error(err))
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

	adapter.LogAccess(c.Request.Context(), h.db, "composer", c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), written)
}

// resolveDistEntry loads the p2 metadata for a package (through the
// cache, so dist resolution shares the entry the p2 passthrough
// route populates) and finds the manifest for the requested version.
// Dev versions live in the separate ~dev metadata file, so a miss in
// the primary file falls back to the other one.
func (h *Handler) resolveDistEntry(ctx context.Context, vendor, pkg, versionNorm, reference string) (*distEntry, error) {
	fullName := vendor + "/" + pkg
	dev := strings.HasPrefix(versionNorm, "dev-") || strings.HasSuffix(versionNorm, "-dev")

	doc, err := h.fetchMetadataDoc(ctx, vendor, pkg, dev)
	if err != nil {
		return nil, err
	}
	if entry := findDistEntry(doc, fullName, versionNorm, reference); entry != nil {
		return entry, nil
	}

	// Fall back to the other metadata file. Its absence upstream
	// (404 → fetch error) is expected for most packages: the primary
	// lookup already worked, the version simply isn't there.
	doc, err = h.fetchMetadataDoc(ctx, vendor, pkg, !dev)
	if err != nil {
		return nil, nil
	}
	return findDistEntry(doc, fullName, versionNorm, reference), nil
}

// fetchMetadataDoc reads a p2 metadata file through the cache manager
// using the same cache key the p2 passthrough route uses, so both
// paths share one cached copy.
func (h *Handler) fetchMetadataDoc(ctx context.Context, vendor, pkg string, dev bool) ([]byte, error) {
	metaPath := "p2/" + vendor + "/" + pkg + ".json"
	cacheKey := MetadataCacheKey(vendor, pkg)
	if dev {
		metaPath = "p2/" + vendor + "/" + pkg + "~dev.json"
		cacheKey = DevMetadataCacheKey(vendor, pkg)
	}

	result, err := h.cacheMgr.Get(ctx, cacheKey, "composer", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}
		fetchResult, err := ups.Fetch(ctx, "/"+metaPath)
		if err != nil {
			return nil, "", 0, "", err
		}
		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, ups.Name, nil
	})
	if err != nil {
		return nil, err
	}
	defer result.Reader.Close()
	return io.ReadAll(result.Reader)
}

// proxyPassthrough proxies a request to the upstream with caching, no content modification.
func (h *Handler) proxyPassthrough(c *gin.Context, path string, ttl time.Duration) {
	start := time.Now()
	cacheKey := "composer/" + path

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "composer", ttl, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}

		zap.L().Info("fetching from composer upstream",
			zap.String("path", path),
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.Fetch(ctx, "/"+path)
		if err != nil {
			return nil, "", 0, "", err
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, ups.Name, nil
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
	written, copyErr := io.Copy(c.Writer, result.Reader)
	if copyErr != nil {
		zap.L().Warn("copy to client failed", zap.String("key", cacheKey), zap.Error(copyErr))
	}

	adapter.LogAccess(c.Request.Context(), h.db, "composer", c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), written)
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
