package npm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	signer   *tarballSigner
}

var errInvalidPackumentProvenance = errors.New("invalid npm packument provenance")

func New(
	cacheMgr *cache.Manager,
	selector upstream.Selector,
	cfg config.CacheConfig,
	database *gorm.DB,
	tarballSigningKey []byte,
) *Handler {
	signer, err := newTarballSigner(tarballSigningKey)
	if err != nil {
		// The composition root must inject a domain-separated deployment key.
		// Continuing without one would turn an internal route into a bypass.
		panic(fmt.Sprintf("initialize npm tarball provenance: %v", err))
	}
	return newHandlerWithSigner(cacheMgr, selector, cfg, database, signer)
}

func newHandlerWithSigner(
	cacheMgr *cache.Manager,
	selector upstream.Selector,
	cfg config.CacheConfig,
	database *gorm.DB,
	signer *tarballSigner,
) *Handler {
	if signer == nil {
		panic("initialize npm tarball provenance: nil signer")
	}
	return &Handler{cacheMgr: cacheMgr, selector: selector, cfg: cfg, db: database, signer: signer}
}

func (h *Handler) Type() string { return "npm" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	// Authenticated tarball routes must precede the legacy direct routes. Only
	// these routes carry a packument-derived version into policy checks.
	rg.GET("/:package/-/"+SignedTarballRouteSegment+"/:token/:filename", h.handleSignedTarball)
	rg.GET("/@:scope/:package/-/"+SignedTarballRouteSegment+"/:token/:filename", h.handleScopedSignedTarball)
	// Legacy tarball routes (must be registered before metadata to avoid
	// conflicts with /:package) are explicit rejection endpoints. Keeping them
	// registered prevents a client from bypassing the signed provenance route.
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
	routePrefix, audience := npmRouteContext(c)
	acceptHeader := c.GetHeader("Accept")
	cacheKey = metadataCacheKeyForAccept(cacheKey, acceptHeader)

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

		// Validate and retain the exact metadata source + dist target before the
		// cache commits. Runtime responses sign these stable placeholders.
		rewritten, err := PreparePackument(
			body,
			fullName,
			fetchResult.URL,
			ups.ProvenanceSourceID(),
		)
		if err != nil {
			return nil, "", 0, ups.Name, fmt.Errorf("%w: %v", errInvalidPackumentProvenance, err)
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
		code := "UPSTREAM_UNAVAILABLE"
		message := err.Error()
		if errors.Is(err, errInvalidPackumentProvenance) {
			code = "UPSTREAM_INVALID_METADATA"
			message = "npm metadata could not be verified"
		}
		c.JSON(http.StatusBadGateway, gin.H{"code": code, "message": message})
		return
	}
	defer result.Reader.Close()

	body, err := io.ReadAll(result.Reader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "read cache"})
		return
	}

	// Sign from the cached packument's versions map on every response. The
	// cache remains process-independent and never stores the private-key token.
	content, err := signRuntimeTarballURLs(
		body,
		baseURL,
		routePrefix,
		audience,
		fullName,
		h.signer,
	)
	if err != nil {
		zap.L().Error("failed to establish npm tarball provenance",
			zap.String("package", fullName),
			zap.Error(err),
		)
		adapter.LogAccess(c.Request.Context(), h.db, "npm", c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusBadGateway, c.ClientIP(), 0)
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_INVALID_METADATA", "message": "npm metadata could not be verified"})
		return
	}

	ct := result.ContentType
	if ct == "" {
		ct = "application/json"
	}
	c.Header("Content-Type", ct)
	c.Header("Vary", "Accept")
	c.String(http.StatusOK, string(content))

	adapter.LogAccess(c.Request.Context(), h.db, "npm", c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), int64(len(content)))
}

func (h *Handler) handleTarball(c *gin.Context) {
	// A direct archive URL has no authenticated packument version. Serving it
	// would let an End User bypass hard malware decisions by skipping metadata.
	c.Status(http.StatusNotFound)
}

func (h *Handler) handleScopedTarball(c *gin.Context) {
	c.Status(http.StatusNotFound)
}

func (h *Handler) handleSignedTarball(c *gin.Context) {
	fullName := c.Param("package")
	h.proxyAuthenticatedTarball(c, fullName)
}

func (h *Handler) handleScopedSignedTarball(c *gin.Context) {
	scope := c.Param("scope")
	packageName := c.Param("package")
	fullName := "@" + scope + "/" + packageName
	h.proxyAuthenticatedTarball(c, fullName)
}

func (h *Handler) proxyAuthenticatedTarball(c *gin.Context, fullName string) {
	filename := c.Param("filename")
	_, audience := npmRouteContext(c)
	claims, valid := h.signer.verify(c.Param("token"), audience, fullName, filename)
	if !valid {
		// Do not reveal whether the key, claims, package, or filename differed.
		c.Status(http.StatusNotFound)
		return
	}
	resolver, ok := h.selector.(upstream.ProvenanceSourceResolver)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	source, err := resolver.ResolveProvenanceSource(claims.Source)
	if err != nil {
		// A token may not fail over to another mirror when its original source
		// disappears or changes configuration.
		c.Status(http.StatusNotFound)
		return
	}
	adapter.BindAuthenticatedArtifactCoordinate(c, "npm", claims.Package, claims.Version)
	if blocked := adapter.PackageRuleGate(c, "npm", claims.Package, claims.Version); blocked {
		return
	}
	h.proxyTarball(c, claims, source, authenticatedTarballCacheKey(claims))
}

func (h *Handler) proxyTarball(
	c *gin.Context,
	claims tarballClaims,
	source *upstream.Upstream,
	cacheKey string,
) {
	if blocked := adapter.QuarantineGate(c, "npm", claims.Package, claims.Version); blocked {
		return
	}
	start := time.Now()

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "npm", h.cfg.TTLBlob, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		zap.L().Info("fetching npm tarball from upstream",
			zap.String("package", claims.Package),
			zap.String("version", claims.Version),
			zap.String("filename", claims.Filename),
			zap.String("upstream", source.Name),
		)

		fetchResult, err := source.FetchProvenanceURL(ctx, claims.Source, claims.Target)
		if err != nil {
			return nil, "", 0, "", err
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, source.Name, nil
	})

	if err != nil {
		zap.L().Error("failed to get npm tarball", zap.String("package", claims.Package), zap.Error(err))
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

func npmRouteContext(c *gin.Context) (routePrefix, audience string) {
	if slug := c.Param("slug"); slug != "" {
		return "/p/" + url.PathEscape(slug) + "/npm", "project:" + slug
	}
	return "/npm", "global"
}
