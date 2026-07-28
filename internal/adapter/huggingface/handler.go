package huggingface

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

const (
	maxBufferedErrorBody                  = 1 << 20
	defaultHuggingFaceArtifactIdleTimeout = 3 * time.Minute
)

// Handler owns request classification, cache policy, and access accounting.
// Resolver owns the Hugging Face redirect protocol; upstream.Upstream owns
// transport, proxy, and passive health behavior.
type Handler struct {
	cacheMgr        *cache.Manager
	selector        upstream.Selector
	cfg             config.CacheConfig
	db              *gorm.DB
	resolver        *resolver
	revocations     *repositoryRevocationGate
	revocationStore repositoryRevocationStorage

	artifactIdleTimeout time.Duration
}

func New(
	cacheMgr *cache.Manager,
	selector upstream.Selector,
	cfg config.CacheConfig,
	database *gorm.DB,
) *Handler {
	handler := &Handler{
		cacheMgr:    cacheMgr,
		selector:    selector,
		cfg:         cfg,
		db:          database,
		resolver:    newResolver(),
		revocations: newRepositoryRevocationGate(),

		artifactIdleTimeout: defaultHuggingFaceArtifactIdleTimeout,
	}
	if database != nil {
		handler.revocationStore = newRepositoryRevocationStore(database)
		handler.loadRepositoryRevocations()
	}
	return handler
}

func (h *Handler) Type() string { return "huggingface" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/*path", h.handleRequest)
	rg.HEAD("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	start := time.Now()
	logKey := "huggingface/unknown"
	cacheHit := false
	upstreamName := ""
	defer func() {
		bytesSent := int64(c.Writer.Size())
		if bytesSent < 0 {
			bytesSent = 0
		}
		adapter.LogAccess(
			c.Request.Context(),
			h.db,
			"huggingface",
			c.Request.Method,
			logKey,
			cacheHit,
			upstreamName,
			time.Since(start),
			c.Writer.Status(),
			c.ClientIP(),
			bytesSent,
		)
	}()

	path, ok := escapedHuggingFacePath(c.Request)
	if !ok {
		c.String(http.StatusNotFound, "unrecognized Hugging Face path")
		return
	}
	parsed := ParseRequestPath(path)
	if parsed.Kind == PathUnknown {
		c.String(http.StatusNotFound, "unrecognized Hugging Face path")
		return
	}

	var queryErr error
	logKey, queryErr = CacheKeyForRawQuery(parsed, c.Request.URL.RawQuery)
	cacheKey := logKey

	// Keep the resolver's bounded Range parser and the outbound request in the
	// same trust domain. If an unsupported request reached an upstream first,
	// its otherwise valid 206 response could be misattributed as an upstream
	// integrity failure and permanently latch that source unhealthy.
	rangeValues := c.Request.Header.Values("Range")
	if c.Request.Method == http.MethodGet && len(rangeValues) > 0 {
		if _, valid := parseRequestedByteRanges(rangeValues); !valid {
			c.Header("Accept-Ranges", "bytes")
			c.String(
				http.StatusRequestedRangeNotSatisfiable,
				"invalid or too many byte ranges",
			)
			return
		}
		if !acceptsIdentityEncoding(c.Request.Header.Values("Accept-Encoding")) {
			c.Header("Vary", "Accept-Encoding")
			c.String(
				http.StatusNotAcceptable,
				"Range requests require identity content encoding",
			)
			return
		}
	}

	// Only artifact requests participate in minimum-age quarantine. API model
	// and tree metadata must remain discoverable, and the gate stays before the
	// cache lookup so a newly blocked revision cannot be served from old bytes.
	if parsed.Kind == PathResolve || parsed.Kind == PathRaw {
		if adapter.QuarantineGate(c, "huggingface", parsed.Repo, parsed.Ref) {
			return
		}
	}

	target := path
	if c.Request.URL.RawQuery != "" {
		target += "?" + c.Request.URL.RawQuery
	}
	repository, hasRepository := repositoryForParsed(parsed)
	publicRequest := publicRepositoryRequest(c.Request)
	if publicRequest && hasRepository && h.repositoryRevoked(repository) {
		upstreamName = h.serveDirectSelected(c, nil, target)
		return
	}

	var preferredUpstream *upstream.Upstream
	if (parsed.Kind == PathResolve || parsed.Kind == PathRaw) &&
		!IsCommitSHA(parsed.Ref) &&
		c.Request.Method == http.MethodHead &&
		(upstreamAuthorization(c.Request) != "" || hasCredentialQuery(c.Request.URL.Query())) {
		upstreamName = h.servePrivateMutableHEAD(c, parsed, target)
		return
	}
	if (parsed.Kind == PathResolve || parsed.Kind == PathRaw) &&
		!IsCommitSHA(parsed.Ref) &&
		upstreamAuthorization(c.Request) == "" &&
		!hasCredentialQuery(c.Request.URL.Query()) {
		pin, pinErr := h.resolveMutableRef(
			c.Request.Context(),
			c.Request,
			parsed,
			target,
		)
		if pinErr != nil {
			upstreamName = h.writeCacheError(c, pinErr)
			return
		}
		if pin.selected != nil && !pin.selected.IsHealthy() {
			if pin.headResponse != nil {
				_ = pin.headResponse.Body.Close()
				pin.headResponse = nil
			}
			pin.selected = h.selectAfterFailure(c.Request.Context(), pin.selected)
		}
		if pin.selected != nil {
			upstreamName = pin.selected.Name
		}
		if !pin.pinned {
			if c.Request.Method == http.MethodHead && pin.headResponse != nil {
				h.writeResolved(c, pin.headResponse)
				return
			}
			if pin.headResponse != nil {
				_ = pin.headResponse.Body.Close()
			}
			upstreamName = h.serveDirectSelected(c, pin.selected, target)
			return
		}

		parsed = pin.parsed
		repository, hasRepository = repositoryForParsed(parsed)
		target = pin.target
		preferredUpstream = pin.selected
		cacheKey, queryErr = CacheKeyForRawQuery(parsed, c.Request.URL.RawQuery)
		if c.Request.Method == http.MethodHead {
			if pin.ephemeral {
				if pin.headResponse != nil {
					_ = pin.headResponse.Body.Close()
				}
				writeLocalCanonicalRedirect(c, target)
				return
			}
			leaveCache := func() {}
			if publicRequest && hasRepository && h.revocations != nil {
				var admitted bool
				leaveCache, admitted = h.revocations.enterCache(repository.packageName)
				if !admitted {
					if pin.headResponse != nil {
						_ = pin.headResponse.Body.Close()
					}
					upstreamName = h.serveDirectSelected(c, pin.selected, target)
					return
				}
			}
			var direct bool
			cacheHit, upstreamName, direct = h.servePinnedHEAD(c, cacheKey, pin)
			leaveCache()
			if direct {
				upstreamName = h.serveDirectSelected(c, pin.selected, target)
			}
			return
		}
		if pin.headResponse != nil {
			_ = pin.headResponse.Body.Close()
		}
	}

	cacheTTL := h.ttl(parsed)
	cacheable := cacheEligible(c.Request) &&
		cacheSafeQuery(parsed, c.Request.URL.Query()) &&
		queryErr == nil &&
		h.cacheMgr != nil
	if !cacheable {
		upstreamName = h.serveDirectSelected(c, preferredUpstream, target)
		return
	}
	canonicalQuery := canonicalCacheQuery(parsed, c.Request.URL.Query())
	cacheKey = CacheKeyWithQuery(parsed, canonicalQuery)
	logKey = cacheKey
	targetPath, _, _ := strings.Cut(target, "?")
	target = targetPath
	if rawQuery := canonicalQuery.Encode(); rawQuery != "" {
		target += "?" + rawQuery
	}
	requestSnapshot := snapshotRequest(c.Request)
	requestSnapshot.URL.RawQuery = canonicalQuery.Encode()

	leaveCache := func() {}
	if publicRequest && hasRepository && h.revocations != nil {
		var admitted bool
		leaveCache, admitted = h.revocations.enterCache(repository.packageName)
		if !admitted {
			upstreamName = h.serveDirectSelected(c, preferredUpstream, target)
			return
		}
	}
	cacheContext := c.Request.Context()
	if parsed.Kind == PathResolve || parsed.Kind == PathRaw {
		// Model and dataset artifacts can be tens of gigabytes. Keep transport
		// connect/header limits, but do not impose Manager's generic 10-minute
		// total-body ceiling. Small API metadata retains the bounded default.
		cacheContext = cache.WithFetchTimeout(cacheContext, 0)
		cacheContext = cache.WithFetchIdleTimeout(cacheContext, h.artifactIdleTimeout)
	}
	result, err := h.cacheMgr.Get(
		cacheContext,
		cacheKey,
		"huggingface",
		cacheTTL,
		func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
			// Close the race with a request that passed the handler gate just
			// before another request discovered repository revocation.
			if publicRequest && hasRepository && h.repositoryRevoked(repository) {
				return nil, "", 0, "", &responseCacheBypass{
					selected: preferredUpstream,
				}
			}
			selected := preferredUpstream
			if selected != nil && !selected.IsHealthy() {
				selected = h.selectAfterFailure(ctx, selected)
			}
			var selectErr error
			if selected == nil {
				selected, selectErr = h.selector.Select(ctx)
			}
			if selectErr != nil || selected == nil {
				if selectErr == nil {
					selectErr = errors.New("selector returned nil upstream")
				}
				return nil, "", 0, "", &proxyFailure{
					status: http.StatusServiceUnavailable,
					cause:  selectErr,
				}
			}

			resolved, resolveErr := h.resolver.resolve(ctx, selected, requestSnapshot, target, true)
			if resolveErr != nil {
				return nil, "", 0, selected.Name, &proxyFailure{
					status:   http.StatusBadGateway,
					upstream: selected.Name,
					cause:    resolveErr,
				}
			}
			if resolved.StatusCode != http.StatusOK {
				repositoryRevoked := publicRequest &&
					hasRepository &&
					repositoryRevocationStatus(
						resolved.StatusCode,
						resolved.Header,
						resolved.provenance,
					)
				if repositoryRevoked {
					// The fetch function is also reused by immutable background
					// refreshes. Close and persist the gate as soon as response
					// headers arrive; a large or slow error body cannot keep stale
					// repository cache readable. Cleanup waits asynchronously for
					// admitted readers and coalesced followers to drain.
					h.revokeRepositoryAsync(ctx, parsed)
				}
				statusErr := newUpstreamStatusError(
					resolved,
					selected.Name,
					h.artifactIdleTimeout,
				)
				return nil, "", 0, selected.Name, statusErr
			}
			if !cacheResponseReusable(resolved.Header, cacheTTL) {
				_ = resolved.Body.Close()
				return nil, "", 0, selected.Name, &responseCacheBypass{
					selected: selected,
				}
			}

			// The outbound cacheable request has fixed representation dimensions,
			// so an upstream Vary no longer describes downstream behavior.
			resolved.Header.Del("Vary")
			body := cache.WithResponseMetadata(resolved.Body, resolved.Header)
			return body,
				resolved.Header.Get("Content-Type"),
				resolved.Size,
				selected.Name,
				nil
		},
	)
	if err != nil {
		leaveCache()
		var bypass *responseCacheBypass
		if errors.As(err, &bypass) {
			upstreamName = h.serveDirectSelected(c, bypass.selected, target)
			return
		}
		upstreamName = h.writeCacheError(c, err)
		return
	}
	if result == nil || result.Reader == nil {
		leaveCache()
		c.String(http.StatusBadGateway, "Hugging Face cache returned no response")
		return
	}

	defer leaveCache()
	cacheHit = result.Hit
	if result.Upstream != "" {
		upstreamName = result.Upstream
	}
	h.writeCachedResult(c, result)
}

func (h *Handler) servePrivateMutableHEAD(c *gin.Context, parsed Parsed, target string) string {
	selected, err := h.selector.Select(c.Request.Context())
	if err != nil || selected == nil {
		c.String(http.StatusServiceUnavailable, "no healthy Hugging Face upstream")
		return ""
	}
	resolved, err := h.resolver.resolve(
		c.Request.Context(),
		selected,
		c.Request,
		target,
		false,
	)
	if err != nil {
		zap.L().Warn("Hugging Face authenticated HEAD failed",
			zap.String("upstream", selected.Name),
			zap.Error(err),
		)
		c.String(http.StatusBadGateway, "Hugging Face upstream request failed")
		return selected.Name
	}
	commit := resolved.Header.Get("X-Repo-Commit")
	canonical, ok := withCommit(parsed, commit)
	if resolved.StatusCode != http.StatusOK || !ok {
		h.writeResolved(c, resolved)
		return selected.Name
	}
	_ = resolved.Body.Close()

	canonicalTarget, ok := requestTarget(canonical, c.Request.URL.RawQuery)
	if !ok {
		c.String(http.StatusBadGateway, "Hugging Face returned an invalid commit")
		return selected.Name
	}
	writeLocalCanonicalRedirect(c, canonicalTarget)
	return selected.Name
}

func writeLocalCanonicalRedirect(c *gin.Context, canonicalTarget string) {
	localPrefix := "/huggingface"
	if slug := c.Param("slug"); slug != "" {
		localPrefix = "/p/" + url.PathEscape(slug) + "/huggingface"
	}
	c.Header("Location", localPrefix+canonicalTarget)
	c.Status(http.StatusTemporaryRedirect)
}

func (h *Handler) serveDirect(c *gin.Context, target string) string {
	selected, err := h.selector.Select(c.Request.Context())
	if err != nil || selected == nil {
		c.String(http.StatusServiceUnavailable, "no healthy Hugging Face upstream")
		return ""
	}
	return h.serveDirectSelected(c, selected, target)
}

func (h *Handler) serveDirectSelected(c *gin.Context, selected *upstream.Upstream, target string) string {
	if selected != nil && !selected.IsHealthy() {
		selected = h.selectAfterFailure(c.Request.Context(), selected)
	}
	if selected == nil {
		var err error
		selected, err = h.selector.Select(c.Request.Context())
		if err != nil || selected == nil {
			c.String(http.StatusServiceUnavailable, "no healthy Hugging Face upstream")
			return ""
		}
	}
	resolveContext := c.Request.Context()
	var cancelResolve context.CancelCauseFunc
	if h.directArtifactIdleTimeoutApplies(c.Request) {
		resolveContext, cancelResolve = context.WithCancelCause(resolveContext)
		defer cancelResolve(context.Canceled)
	}
	revocationTicket := h.directRepositoryRevocationTicket(c.Request, target)
	resolved, err := h.resolver.resolve(
		resolveContext,
		selected,
		c.Request,
		target,
		false,
	)
	if err != nil {
		zap.L().Warn("Hugging Face upstream request failed",
			zap.String("upstream", selected.Name),
			zap.Error(err),
		)
		c.String(http.StatusBadGateway, "Hugging Face upstream request failed")
		return selected.Name
	}
	h.observeDirectRepositoryResponse(
		c.Request,
		target,
		resolved,
		revocationTicket,
	)
	if cancelResolve != nil {
		resolved.Body = withDirectArtifactBodyIdleTimeout(
			resolved.Body,
			h.artifactIdleTimeout,
			cancelResolve,
		)
	}
	h.writeResolved(c, resolved)
	return selected.Name
}

func withDirectArtifactBodyIdleTimeout(
	body io.ReadCloser,
	timeout time.Duration,
	cancel context.CancelCauseFunc,
) io.ReadCloser {
	// Protocol body decorators cooperate with the cache helper to keep the
	// watchdog next to the transport while preserving their outer semantics.
	return cache.WithBodyIdleTimeout(body, timeout, cancel)
}

func (h *Handler) directArtifactIdleTimeoutApplies(request *http.Request) bool {
	if h.artifactIdleTimeout <= 0 || request == nil || request.Method != http.MethodGet {
		return false
	}
	path, ok := escapedHuggingFacePath(request)
	if !ok {
		return false
	}
	parsed := ParseRequestPath(path)
	return parsed.Kind == PathResolve || parsed.Kind == PathRaw
}

func (h *Handler) servePinnedHEAD(
	c *gin.Context,
	cacheKey string,
	pin refPinResult,
) (bool, string, bool) {
	upstreamName := ""
	if pin.selected != nil {
		upstreamName = pin.selected.Name
	}
	if pin.cachedHead != nil {
		if pin.headResponse != nil {
			_ = pin.headResponse.Body.Close()
		}
		h.writeCacheHead(c, pin.cachedHead)
		return true, upstreamName, false
	}
	if h.cacheMgr != nil {
		head, err := h.cacheMgr.Head(c.Request.Context(), cacheKey, "huggingface")
		if err == nil {
			if pin.headResponse != nil {
				_ = pin.headResponse.Body.Close()
			}
			h.writeCacheHead(c, head)
			return true, upstreamName, false
		}
		if !errors.Is(err, cache.ErrCacheMiss) {
			zap.L().Warn("Hugging Face cached HEAD lookup failed",
				zap.String("key", cacheKey),
				zap.Error(err),
			)
		}
	}
	if pin.headResponse != nil {
		h.writeResolved(c, pin.headResponse)
		return false, upstreamName, false
	}
	return false, upstreamName, true
}

func (h *Handler) writeCacheHead(c *gin.Context, result *cache.HeadResult) {
	copyHTTPHeaders(c.Writer.Header(), cachedClientResponseHeaders(result.Headers, c.Param("slug")))
	if result.ContentType != "" {
		c.Header("Content-Type", result.ContentType)
	}
	if result.Size >= 0 {
		c.Header("Content-Length", strconv.FormatInt(result.Size, 10))
	}
	c.Status(http.StatusOK)
}

func snapshotRequest(request *http.Request) *http.Request {
	if request == nil {
		return nil
	}
	snapshot := request.Clone(context.Background())
	snapshot.Body = http.NoBody
	snapshot.GetBody = nil
	return snapshot
}

func (h *Handler) writeCachedResult(c *gin.Context, result *cache.GetResult) {
	defer result.Reader.Close()
	copyHTTPHeaders(c.Writer.Header(), cachedClientResponseHeaders(result.Headers, c.Param("slug")))
	if result.ContentType != "" {
		c.Header("Content-Type", result.ContentType)
	} else if c.Writer.Header().Get("Content-Type") == "" {
		c.Header("Content-Type", "application/octet-stream")
	}
	if result.Size >= 0 {
		c.Header("Content-Length", strconv.FormatInt(result.Size, 10))
	}
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, result.Reader); err != nil {
		path, _ := escapedHuggingFacePath(c.Request)
		zap.L().Warn("Hugging Face cached response stream interrupted",
			zap.String("key", CacheKey(ParseRequestPath(path))),
			zap.Error(err),
		)
	}
}

// escapedHuggingFacePath returns the path relative to the adapter while
// retaining URL-component boundaries. Gin's wildcard parameter is decoded, so
// using c.Param here would turn refs%2Fpr%2F1 into three path segments and send
// a different revision to the Hub.
func escapedHuggingFacePath(request *http.Request) (string, bool) {
	if request == nil || request.URL == nil {
		return "", false
	}
	const prefix = "/huggingface"
	path := request.URL.EscapedPath()
	if path == prefix {
		return "/", true
	}
	if !strings.HasPrefix(path, prefix+"/") {
		return "", false
	}
	return strings.TrimPrefix(path, prefix), true
}

func (h *Handler) writeResolved(c *gin.Context, resolved *resolvedResponse) {
	if resolved == nil {
		c.String(http.StatusBadGateway, "Hugging Face upstream returned no response")
		return
	}
	defer resolved.Body.Close()
	copyHTTPHeaders(c.Writer.Header(), clientResponseHeaders(resolved.Header, c.Param("slug")))
	if resolved.Size >= 0 && c.Writer.Header().Get("Content-Length") == "" {
		c.Header("Content-Length", strconv.FormatInt(resolved.Size, 10))
	}
	c.Status(resolved.StatusCode)
	if c.Request.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(c.Writer, resolved.Body); err != nil {
		zap.L().Warn("Hugging Face response stream interrupted", zap.Error(err))
	}
}

func (h *Handler) writeCacheError(c *gin.Context, err error) string {
	var statusErr *upstreamStatusError
	if errors.As(err, &statusErr) {
		copyHTTPHeaders(c.Writer.Header(), statusErr.header)
		c.Header("Content-Length", strconv.Itoa(len(statusErr.body)))
		c.Status(statusErr.status)
		_, _ = io.Copy(c.Writer, bytes.NewReader(statusErr.body))
		return statusErr.upstream
	}

	var failure *proxyFailure
	if errors.As(err, &failure) {
		zap.L().Warn("Hugging Face proxy request failed",
			zap.String("upstream", failure.upstream),
			zap.Error(failure.cause),
		)
		message := "Hugging Face upstream request failed"
		if failure.status == http.StatusServiceUnavailable {
			message = "no healthy Hugging Face upstream"
		}
		c.String(failure.status, message)
		return failure.upstream
	}

	zap.L().Warn("Hugging Face cache request failed", zap.Error(err))
	c.String(http.StatusBadGateway, "Hugging Face upstream request failed")
	return ""
}

func (h *Handler) ttl(parsed Parsed) time.Duration {
	indexTTL := h.cfg.TTLIndex
	if indexTTL <= 0 {
		indexTTL = 5 * time.Minute
	}
	blobTTL := h.cfg.TTLBlob
	if blobTTL <= 0 {
		blobTTL = 72 * time.Hour
	}
	switch parsed.Kind {
	case PathResolve,
		PathRaw,
		PathAPIModelRevision,
		PathAPIModelTree,
		PathAPIDatasetRevision,
		PathAPIDatasetTree:
		if IsCommitSHA(parsed.Ref) {
			return blobTTL
		}
	}
	return indexTTL
}

type proxyFailure struct {
	status   int
	upstream string
	cause    error
}

func (e *proxyFailure) Error() string {
	return fmt.Sprintf("Hugging Face proxy failed with status %d", e.status)
}

func (e *proxyFailure) Unwrap() error { return e.cause }

type responseCacheBypass struct {
	selected *upstream.Upstream
}

func (e *responseCacheBypass) Error() string {
	return "Hugging Face response is not reusable by a shared cache"
}

func (e *responseCacheBypass) AllowStaleFallback() bool { return false }

type upstreamStatusError struct {
	status     int
	header     http.Header
	body       []byte
	upstream   string
	provenance responseProvenance
}

func newUpstreamStatusError(
	response *resolvedResponse,
	upstreamName string,
	bodyIdleTimeout time.Duration,
) *upstreamStatusError {
	response.Body = cache.WithBodyIdleTimeout(response.Body, bodyIdleTimeout, nil)
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBufferedErrorBody+1))
	if err != nil {
		body = []byte("failed to read Hugging Face upstream error response")
	}
	if len(body) > maxBufferedErrorBody {
		body = body[:maxBufferedErrorBody]
	}
	headers := filterErrorResponseHeaders(response.Header)
	headers.Del("Content-Length")
	return &upstreamStatusError{
		status:     response.StatusCode,
		header:     headers,
		body:       body,
		upstream:   upstreamName,
		provenance: response.provenance,
	}
}

func (e *upstreamStatusError) Error() string {
	return fmt.Sprintf("Hugging Face upstream returned status %d", e.status)
}

func (e *upstreamStatusError) AllowStaleFallback() bool {
	return e.status == http.StatusRequestTimeout ||
		e.status == http.StatusTooEarly ||
		e.status == http.StatusTooManyRequests ||
		e.status >= http.StatusInternalServerError
}
