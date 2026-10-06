package pypi

import (
	"bytes"
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
	// cacheNamespace and artifactAudience may vary inside a channel-aware
	// index family while adapterID stays stable for policy, metrics, and DB
	// classification. Empty values preserve the legacy adapterID identity.
	cacheNamespace   string
	artifactAudience string
	// upstreamSimplePath is the upstream root that contains project index
	// pages. PyPI uses /simple; PyTorch's CUDA indexes expose them at /.
	upstreamSimplePath string
	artifactSigningKey []byte
	artifactSelector   upstream.Selector
	// provenanceRequired requests the PEP 691 JSON simple index and signs the
	// registry upload-time into artifact references. It is only set for the
	// built-in PyPI adapter when the minimum-release-age gate is enabled.
	provenanceRequired bool
}

// Options configures one PyPI-compatible route while keeping the public route
// shape (/simple/...) independent from the upstream's project-index layout.
type Options struct {
	PathPrefix         string
	AdapterID          string
	UpstreamSimplePath string
	// ArtifactSigningKey authenticates artifact URLs declared by an index page.
	// Keep it stable across restarts for as long as cached index pages may live.
	ArtifactSigningKey []byte
	// ArtifactSelector chooses the egress client for signed artifact downloads.
	// Nil falls back to the metadata selector for backward compatibility.
	ArtifactSelector upstream.Selector
	// ProvenanceRequired negotiates the PEP 691 JSON simple index and carries
	// upload-time in signed artifact references. Requires ArtifactSigningKey.
	ProvenanceRequired bool
}

// New creates a new PyPI handler.
func New(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB) *Handler {
	handler, _ := newWithOptions(cacheMgr, selector, cfg, database, Options{
		PathPrefix:         "/pypi",
		AdapterID:          "pypi",
		UpstreamSimplePath: "/simple",
	})
	return handler
}

// NewWithOptions creates a PyPI-compatible handler with an explicit upstream
// project-index root and signed artifact references.
func NewWithOptions(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB, options Options) (*Handler, error) {
	return newWithOptions(cacheMgr, selector, cfg, database, options)
}

func newWithOptions(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB, options Options) (*Handler, error) {
	if options.PathPrefix == "" {
		options.PathPrefix = "/pypi"
	}
	if options.AdapterID == "" {
		options.AdapterID = "pypi"
	}
	if strings.HasPrefix(options.AdapterID, "extra:") && len(options.ArtifactSigningKey) < 32 {
		return nil, errors.New("extra PyPI index requires an artifact signing key of at least 32 bytes")
	}
	if options.ProvenanceRequired && len(options.ArtifactSigningKey) < 32 {
		return nil, errors.New("PyPI provenance mode requires an artifact signing key of at least 32 bytes")
	}
	simplePath, err := normalizeUpstreamSimplePath(options.UpstreamSimplePath)
	if err != nil {
		return nil, err
	}
	return &Handler{
		cacheMgr:           cacheMgr,
		selector:           selector,
		cfg:                cfg,
		db:                 database,
		pathPrefix:         strings.TrimRight(options.PathPrefix, "/"),
		adapterID:          options.AdapterID,
		cacheNamespace:     options.AdapterID,
		artifactAudience:   options.AdapterID,
		upstreamSimplePath: simplePath,
		artifactSigningKey: append([]byte(nil), options.ArtifactSigningKey...),
		artifactSelector:   options.ArtifactSelector,
		provenanceRequired: options.ProvenanceRequired,
	}, nil
}

// SetArtifactSigningKey enables signed artifact references after construction.
// The composition root uses it for the built-in PyPI adapter only when the
// minimum-release-age gate is enabled, so the default proxy behavior keeps the
// legacy URL shape.
func (h *Handler) SetArtifactSigningKey(key []byte, provenanceRequired bool) error {
	if h == nil {
		return errors.New("nil PyPI handler")
	}
	if len(key) < 32 {
		return errors.New("PyPI artifact signing key must be at least 32 bytes")
	}
	h.artifactSigningKey = append([]byte(nil), key...)
	h.provenanceRequired = provenanceRequired
	return nil
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

	result, err := ups.Fetch(c.Request.Context(), h.upstreamProjectPath(""))
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
	cacheKey := IndexCacheKey(h.cacheIdentity(), pkg)
	if len(h.artifactSigningKey) > 0 {
		cacheKey = signedIndexCacheKey(h.cacheIdentity(), pkg, h.artifactSigningKey)
	}
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

		headers := map[string]string{
			"If-None-Match":     cachedValidators.ETag,
			"If-Modified-Since": cachedValidators.LastModified,
		}
		if h.provenanceRequired {
			headers["Accept"] = "application/vnd.pypi.simple.v1+json, application/vnd.pypi.simple.v1+html;q=0.5, text/html;q=0.2"
		}
		fetchResult, err := ups.FetchWithHeaders(ctx, h.upstreamProjectPath(pkg), headers)
		if err != nil {
			return nil, "", 0, ups.Name, err
		}
		if fetchResult.StatusCode == http.StatusNotModified {
			fetchResult.Body.Close()
			return nil, "", 0, ups.Name, cache.ErrNotModified
		}

		// Read the index document and rewrite artifact URLs before caching.
		body, err := io.ReadAll(fetchResult.Body)
		fetchResult.Body.Close()
		if err != nil {
			return nil, "", 0, ups.Name, err
		}

		rewritten, contentType, err := h.rewriteIndex(body, fetchResult.ContentType, fetchResult.URL, ups.ProvenanceSourceID())
		if err != nil {
			return nil, "", 0, ups.Name, err
		}

		bodyReader := io.NopCloser(bytes.NewReader(rewritten))
		return cache.WithResponseValidators(bodyReader, fetchResult.ETag, fetchResult.LastModified), contentType, int64(len(rewritten)), ups.Name, nil
	})

	if err != nil {
		zap.L().Error("failed to get package index", zap.String("package", pkg), zap.Error(err))
		status := http.StatusBadGateway
		code := "UPSTREAM_UNAVAILABLE"
		if strings.Contains(err.Error(), "returned 404") {
			status = http.StatusNotFound
			code = "NOT_FOUND"
		}
		adapter.LogAccess(c.Request.Context(), h.db, h.adapterID, c.Request.Method, cacheKey, false, "", time.Since(start), status, c.ClientIP(), 0)
		c.JSON(status, gin.H{"code": code, "message": err.Error()})
		return
	}
	defer result.Reader.Close()

	// Read the cached representation and apply the client-facing URL shape.
	body, err := io.ReadAll(result.Reader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "read cache"})
		return
	}

	ct := result.ContentType
	if ct == "" {
		ct = "text/html"
	}
	body, ct, err = h.withClientRepresentation(body, ct, baseURL, c.GetHeader("Accept"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "render index"})
		return
	}
	c.Header("Content-Type", ct)
	c.String(http.StatusOK, string(body))

	adapter.LogAccess(c.Request.Context(), h.db, h.adapterID, c.Request.Method, cacheKey, result.Hit, result.Upstream, time.Since(start), http.StatusOK, c.ClientIP(), int64(len(body)))
}

// rewriteIndex converts the upstream simple index into cache-stable local
// artifact references. PEP 691 JSON is used when provenance is required; an
// upstream that ignores the negotiation returns HTML and the gate fails
// closed later because those references carry no upload-time.
func (h *Handler) rewriteIndex(body []byte, contentType, pageURL, sourceID string) ([]byte, string, error) {
	if h.provenanceRequired && strings.Contains(strings.ToLower(contentType), "json") {
		rewritten, err := rewriteSignedJSONIndex(body, h.pathPrefix, pageURL, h.tokenAudience(), sourceID, h.artifactSigningKey)
		if err != nil {
			return nil, "", err
		}
		return rewritten, "application/vnd.pypi.simple.v1+json", nil
	}
	html, err := rewriteSignedArtifactURLs(
		string(body),
		"",
		h.pathPrefix,
		pageURL,
		h.tokenAudience(),
		sourceID,
		h.artifactSigningKey,
	)
	if err != nil {
		return nil, "", err
	}
	return []byte(html), contentType, nil
}

// withClientRepresentation injects the request origin and serves HTML to
// clients that did not advertise PEP 691 JSON support.
func (h *Handler) withClientRepresentation(body []byte, contentType, baseURL, accept string) ([]byte, string, error) {
	jsonRepresentation := strings.Contains(strings.ToLower(contentType), "json")
	clientAcceptsJSON := strings.Contains(strings.ToLower(accept), "application/vnd.pypi.simple.v1+json")
	if jsonRepresentation && !clientAcceptsJSON {
		rendered, err := renderSimpleHTMLFromJSON(body)
		if err != nil {
			return nil, "", err
		}
		return bytes.ReplaceAll(
			rendered,
			[]byte(`href="`+h.pathPrefix+`/files/`),
			[]byte(`href="`+baseURL+h.pathPrefix+`/files/`),
		), "text/html; charset=utf-8", nil
	}
	if jsonRepresentation {
		return bytes.ReplaceAll(
			body,
			[]byte(`"url":"`+h.pathPrefix+`/files/`),
			[]byte(`"url":"`+baseURL+h.pathPrefix+`/files/`),
		), contentType, nil
	}
	return bytes.ReplaceAll(
		body,
		[]byte(`href="`+h.pathPrefix+`/files/`),
		[]byte(`href="`+baseURL+h.pathPrefix+`/files/`),
	), contentType, nil
}

// handleFileDownload proxies and caches package file downloads.
func (h *Handler) handleFileDownload(c *gin.Context) {
	filepath := c.Param("filepath")
	externalTarget, external, err := h.resolveExternalArtifact(filepath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "invalid external artifact reference"})
		return
	}
	artifactPath := filepath
	if external {
		artifactPath = externalTarget.filename
	}
	// PyPI file paths carry (package, version) in their PEP 427 / 625
	// filename. Keep adapterID as the policy identity: the blocklist layer
	// canonicalizes extra:* to PyPI, while extra-index release-age resolution
	// remains distinct from the public PyPI registry.
	if base := lastPathSegment(artifactPath); base != "" {
		if pkg, version := packagekey.ParsePypiFilename(base); pkg != "" && version != "" {
			provenance := adapter.QuarantineProvenance{}
			if external {
				provenance = adapter.QuarantineProvenance{
					SourceID:  externalTarget.source,
					PublishAt: externalTarget.uploadTime,
				}
			}
			if blocked := adapter.QuarantineGateWithProvenance(c, h.adapterID, pkg, version, provenance); blocked {
				return
			}
		}
	}
	cacheKey := FileCacheKey(h.cacheIdentity(), filepath)
	if external {
		cacheKey = ExternalFileCacheKey(h.cacheIdentity(), externalTarget.url)
	}
	start := time.Now()

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, h.adapterID, h.cfg.TTLBlob, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		downloadSelector := h.selector
		if external && h.artifactSelector != nil {
			downloadSelector = h.artifactSelector
		}
		ups, err := downloadSelector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}

		logFields := []zap.Field{
			zap.String("cache_key", cacheKey),
			zap.String("upstream", ups.Name),
		}
		if external {
			logFields = append(logFields,
				zap.String("artifact_filename", externalTarget.filename),
				zap.String("artifact_host", externalTarget.host),
			)
		} else {
			logFields = append(logFields, zap.String("filepath", filepath))
		}
		zap.L().Info("fetching file from upstream", logFields...)

		var fetchResult *upstream.FetchResult
		if external {
			fetchResult, err = ups.FetchURL(ctx, externalTarget.url)
		} else {
			fetchResult, err = ups.Fetch(ctx, filepath)
		}
		if err != nil {
			return nil, "", 0, "", err
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, ups.Name, nil
	})

	if err != nil {
		logFields := []zap.Field{zap.String("cache_key", cacheKey), zap.Error(err)}
		if external {
			logFields = append(logFields,
				zap.String("artifact_filename", externalTarget.filename),
				zap.String("artifact_host", externalTarget.host),
			)
		} else {
			logFields = append(logFields, zap.String("filepath", filepath))
		}
		zap.L().Error("failed to get file", logFields...)
		status := http.StatusBadGateway
		code := "UPSTREAM_UNAVAILABLE"
		if strings.Contains(err.Error(), "returned 404") {
			status = http.StatusNotFound
			code = "NOT_FOUND"
		}
		adapter.LogAccess(c.Request.Context(), h.db, h.adapterID, c.Request.Method, cacheKey, false, "", time.Since(start), status, c.ClientIP(), 0)
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

type externalArtifactTarget struct {
	url        string
	host       string
	filename   string
	source     string
	uploadTime time.Time
}

var errInvalidExternalArtifactReference = errors.New("invalid external artifact reference")

func (h *Handler) resolveExternalArtifact(filepath string) (externalArtifactTarget, bool, error) {
	const prefix = "/_external/"
	if !strings.HasPrefix(filepath, prefix) {
		return externalArtifactTarget{}, false, nil
	}
	reference := strings.TrimPrefix(filepath, prefix)
	token, requestedFilename, found := strings.Cut(reference, "/")
	if !found || token == "" || requestedFilename == "" ||
		strings.Contains(requestedFilename, "/") || len(requestedFilename) > maxArtifactFilenameLength+len(".metadata") {
		return externalArtifactTarget{}, true, errInvalidExternalArtifactReference
	}
	claims, err := decodeExternalArtifactToken(h.artifactSigningKey, h.tokenAudience(), token)
	if err != nil {
		return externalArtifactTarget{}, true, errInvalidExternalArtifactReference
	}
	target, err := parseFetchableArtifactURL(claims.Target)
	if err != nil || !obviousPythonArtifactURL(target) {
		return externalArtifactTarget{}, true, errInvalidExternalArtifactReference
	}
	expectedFilename, err := artifactFilename(target)
	if err != nil {
		return externalArtifactTarget{}, true, errInvalidExternalArtifactReference
	}
	artifactFilename := expectedFilename
	switch {
	case requestedFilename == expectedFilename:
	case !strings.HasSuffix(strings.ToLower(expectedFilename), ".metadata") && requestedFilename == expectedFilename+".metadata":
		target.Path += ".metadata"
		if target.RawPath != "" {
			target.RawPath += ".metadata"
		}
	default:
		return externalArtifactTarget{}, true, errInvalidExternalArtifactReference
	}
	var uploadTime time.Time
	if claims.UploadTime != "" {
		parsed, err := time.Parse(time.RFC3339, claims.UploadTime)
		if err != nil {
			return externalArtifactTarget{}, true, errInvalidExternalArtifactReference
		}
		uploadTime = parsed.UTC()
	}
	return externalArtifactTarget{
		url:        target.String(),
		host:       target.Hostname(),
		filename:   artifactFilename,
		source:     claims.Source,
		uploadTime: uploadTime,
	}, true, nil
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

func normalizeUpstreamSimplePath(value string) (string, error) {
	if value == "" {
		return "/simple", nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		!strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") {
		return "", fmt.Errorf("upstream simple path %q must be an absolute URL path", value)
	}
	normalized := strings.TrimRight(parsed.EscapedPath(), "/")
	if normalized == "" {
		normalized = "/"
	}
	return normalized, nil
}

func (h *Handler) upstreamProjectPath(packageName string) string {
	if packageName == "" {
		if h.upstreamSimplePath == "/" {
			return "/"
		}
		return h.upstreamSimplePath + "/"
	}
	if h.upstreamSimplePath == "/" {
		return "/" + packageName + "/"
	}
	return h.upstreamSimplePath + "/" + packageName + "/"
}

func (h *Handler) cacheIdentity() string {
	if h.cacheNamespace != "" {
		return h.cacheNamespace
	}
	return h.adapterID
}

func (h *Handler) tokenAudience() string {
	if h.artifactAudience != "" {
		return h.artifactAudience
	}
	return h.adapterID
}
