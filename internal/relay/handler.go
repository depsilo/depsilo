// Package relay implements the parent side of Depsilo cascade peering.
//
// A child instance sends the exact upstream exchange it would otherwise make
// itself. This endpoint authenticates the peer, enforces a bounded traversal
// chain, fetches the raw origin response through a guarded egress client,
// optionally caches it, and streams it back. Protocol rewriting, provenance,
// and package policy stay entirely on the child, so every adapter keeps its
// normal behavior no matter how many cache levels the bytes traverse.
package relay

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/cascade"
	"depsilo/internal/db"
)

const (
	// cacheAdapterType keeps relay entries out of every protocol namespace
	// while making them visible to the shared LRU and cache admin surfaces.
	cacheAdapterType = "relay"
	cacheKeyPrefix   = "relay-v1/"
	// minRelayTTL keeps a peer from turning the parent into a no-op cache.
	minRelayTTL = 30 * time.Second
	// defaultMaxTTL matches the configuration default when the field is left
	// unset by a caller that constructs Handler directly (tests).
	defaultMaxTTL = 7 * 24 * time.Hour
)

// Handler serves authenticated relay requests from child instances.
type Handler struct {
	DB       *gorm.DB
	Cache    *cache.Manager
	Identity string
	Token    string
	MaxHops  int
	MaxTTL   time.Duration
	// DefaultClient reaches public origin targets. It must not follow
	// redirects: the child follows each hop through the relay so protocol
	// adapters observe the same redirect chain they would see directly.
	DefaultClient *http.Client
	// ResolveEgress optionally returns this instance's own client for the
	// child-declared upstream source. The matched client can itself egress
	// through another cascade peer, which is how chains of three or more cache
	// levels stay connected when an intermediate node has no direct WAN path.
	ResolveEgress func(source, target string) (*http.Client, bool)
}

// plan is one validated relay exchange.
type plan struct {
	method    string
	target    *url.URL
	targetRaw string
	source    string
	sourceURL *url.URL
	kind      string
	ttl       time.Duration
	chain     cascade.Chain
	next      cascade.Chain
	headers   http.Header
	cacheKey  string
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set(cascade.HeaderInstance, h.Identity)
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeRelayError(writer, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "relay supports GET and HEAD")
		return
	}
	if !h.authorized(request) {
		writeRelayError(writer, http.StatusUnauthorized, "CASCADE_UNAUTHORIZED", "cascade token is missing or invalid")
		return
	}
	exchange, err := h.planRequest(request)
	if err != nil {
		writeRelayError(writer, http.StatusBadRequest, "INVALID_RELAY_REQUEST", err.Error())
		return
	}
	if exchange.chain.Contains(h.Identity) {
		writeRelayError(writer, http.StatusLoopDetected, "CASCADE_LOOP", "cascade request returned to an instance it already visited")
		return
	}
	if len(exchange.chain) >= h.maxHops() {
		writeRelayError(writer, http.StatusLoopDetected, "CASCADE_HOP_LIMIT", "cascade request exceeded the configured hop limit")
		return
	}
	exchange.next = exchange.chain.With(h.Identity)

	cacheable := exchange.method == http.MethodGet &&
		exchange.ttl > 0 &&
		exchange.headers.Get("Range") == "" &&
		h.Cache != nil
	if !cacheable {
		h.serveUncached(writer, request, exchange)
		return
	}
	h.serveCached(writer, request, exchange)
}

func (h *Handler) authorized(request *http.Request) bool {
	if h.Token == "" {
		return false
	}
	provided := request.Header.Get(cascade.HeaderToken)
	if len(provided) != len(h.Token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(h.Token)) == 1
}

func (h *Handler) maxHops() int {
	if h.MaxHops > 0 {
		return h.MaxHops
	}
	return cascade.DefaultMaxHops
}

func (h *Handler) maxTTL() time.Duration {
	if h.MaxTTL > 0 {
		return h.MaxTTL
	}
	return defaultMaxTTL
}

func (h *Handler) planRequest(request *http.Request) (*plan, error) {
	if !cascade.ValidInstanceID(h.Identity) {
		return nil, errors.New("relay instance identity is not configured")
	}
	rawTarget := strings.TrimSpace(request.Header.Get(cascade.HeaderTarget))
	if rawTarget == "" || len(rawTarget) > cascade.MaxTargetURLLength {
		return nil, errors.New("relay target is missing or too long")
	}
	target, err := url.Parse(rawTarget)
	if err != nil || target.Host == "" || target.User != nil || target.Fragment != "" ||
		(target.Scheme != "http" && target.Scheme != "https") {
		return nil, errors.New("relay target is not a valid absolute HTTP URL")
	}
	chain, err := cascade.ParseChain(request.Header.Get(cascade.HeaderChain))
	if err != nil || len(chain) == 0 {
		return nil, errors.New("relay chain is missing or invalid")
	}

	kind := request.Header.Get(cascade.HeaderKind)
	if kind != cascade.KindArtifact {
		kind = cascade.KindIndex
	}
	ttl, _, err := cascade.ParseTTLHeader(request.Header.Get(cascade.HeaderTTL))
	if err != nil {
		return nil, errors.New("relay TTL is invalid")
	}
	if ttl > h.maxTTL() {
		ttl = h.maxTTL()
	}
	if ttl > 0 && ttl < minRelayTTL {
		ttl = minRelayTTL
	}

	source := strings.TrimSpace(request.Header.Get(cascade.HeaderSource))
	var sourceURL *url.URL
	if source != "" {
		if parsed, parseErr := url.Parse(source); parseErr == nil && parsed.Host != "" {
			sourceURL = parsed
		}
	}
	exchange := &plan{
		method:    request.Method,
		target:    target,
		targetRaw: target.String(),
		source:    source,
		sourceURL: sourceURL,
		kind:      kind,
		ttl:       ttl,
		chain:     chain,
		headers:   request.Header.Clone(),
	}
	exchange.cacheKey = relayCacheKey(exchange)
	return exchange, nil
}

func relayCacheKey(exchange *plan) string {
	digest := sha256.New()
	_, _ = io.WriteString(digest, exchange.method)
	digest.Write([]byte{0})
	_, _ = io.WriteString(digest, exchange.targetRaw)
	digest.Write([]byte{0})
	_, _ = io.WriteString(digest, exchange.headers.Get("Accept"))
	return cacheKeyPrefix + exchange.kind + "/" + hex.EncodeToString(digest.Sum(nil))
}

func (h *Handler) clientFor(exchange *plan) *http.Client {
	if h.ResolveEgress != nil && exchange.source != "" {
		if client, ok := h.ResolveEgress(exchange.source, exchange.targetRaw); ok && client != nil {
			return client
		}
	}
	return h.DefaultClient
}

func (h *Handler) serveCached(writer http.ResponseWriter, request *http.Request, exchange *plan) {
	ctx := cascade.WithChain(request.Context(), exchange.next)
	result, err := h.Cache.Get(ctx, exchange.cacheKey, cacheAdapterType, exchange.ttl, func(fetchCtx context.Context) (io.ReadCloser, string, int64, string, error) {
		return h.fetch(fetchCtx, request, exchange)
	})
	if err != nil {
		if errors.Is(err, errRelayRedirect) {
			// A redirect must never enter the cache: its Location is usually
			// short-lived and is not a replay-safe representation header. Fetch
			// it once more without the cache and let the child follow the hop.
			h.serveUncached(writer, request, exchange)
			return
		}
		if errors.Is(err, cache.ErrNotModified) {
			// The cache manager had no live entry to refresh even though stored
			// validators existed. Retry once without validators and stream the
			// full representation.
			h.serveUncached(writer, request, exchange)
			return
		}
		var statusErr *originStatusError
		if errors.As(err, &statusErr) {
			writeRelayError(writer, statusErr.status, "UPSTREAM_ERROR", "origin returned an error status")
			return
		}
		zap.L().Warn("relay upstream fetch failed",
			zap.String("target_host", exchange.target.Hostname()),
			zap.Error(err),
		)
		writeRelayError(writer, http.StatusBadGateway, "RELAY_UPSTREAM_UNAVAILABLE", "relay could not reach the origin")
		return
	}
	defer result.Reader.Close()

	for name, values := range result.Headers {
		writer.Header()[name] = append([]string(nil), values...)
	}
	contentType := result.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	writer.Header().Set("Content-Type", contentType)
	if result.Size > 0 {
		writer.Header().Set("Content-Length", fmt.Sprintf("%d", result.Size))
	}
	if result.Hit {
		writer.Header().Set(cascade.HeaderCache, cascade.CacheHit)
	} else {
		writer.Header().Set(cascade.HeaderCache, cascade.CacheMiss)
	}
	writer.WriteHeader(http.StatusOK)
	if _, copyErr := io.Copy(writer, result.Reader); copyErr != nil {
		zap.L().Warn("relay response copy failed", zap.Error(copyErr))
	}
}

func (h *Handler) serveUncached(writer http.ResponseWriter, request *http.Request, exchange *plan) {
	ctx := cascade.WithChain(request.Context(), exchange.next)
	originRequest, err := h.buildOriginRequest(ctx, request, exchange, exchange.method)
	if err != nil {
		writeRelayError(writer, http.StatusBadRequest, "INVALID_RELAY_REQUEST", "relay request could not be built")
		return
	}
	response, err := h.clientFor(exchange).Do(originRequest)
	if err != nil {
		zap.L().Warn("relay upstream fetch failed",
			zap.String("target_host", exchange.target.Hostname()),
			zap.Error(err),
		)
		writeRelayError(writer, http.StatusBadGateway, "RELAY_UPSTREAM_UNAVAILABLE", "relay could not reach the origin")
		return
	}
	defer response.Body.Close()

	copyRelayResponseHeaders(writer.Header(), response)
	writer.Header().Set(cascade.HeaderCache, cascade.CacheBypass)
	writer.WriteHeader(response.StatusCode)
	if exchange.method == http.MethodHead {
		return
	}
	if _, copyErr := io.Copy(writer, response.Body); copyErr != nil {
		zap.L().Warn("relay response copy failed", zap.Error(copyErr))
	}
}

func (h *Handler) fetch(
	ctx context.Context,
	inbound *http.Request,
	exchange *plan,
) (io.ReadCloser, string, int64, string, error) {
	originRequest, err := h.buildOriginRequest(ctx, inbound, exchange, http.MethodGet)
	if err != nil {
		return nil, "", 0, "", err
	}
	if h.DB != nil {
		var stored db.CacheEntry
		if lookupErr := h.DB.WithContext(ctx).
			Select("etag", "last_modified").
			Where("key = ?", exchange.cacheKey).
			First(&stored).Error; lookupErr == nil {
			if stored.ETag != "" {
				originRequest.Header.Set("If-None-Match", stored.ETag)
			}
			if stored.LastModified != "" {
				originRequest.Header.Set("If-Modified-Since", stored.LastModified)
			}
		}
	}
	response, err := h.clientFor(exchange).Do(originRequest)
	if err != nil {
		return nil, "", 0, "", err
	}
	switch {
	case response.StatusCode == http.StatusNotModified:
		_ = response.Body.Close()
		return nil, "", 0, "", cache.ErrNotModified
	case response.StatusCode >= 300 && response.StatusCode < 400:
		drainRelayBody(response.Body)
		return nil, "", 0, "", errRelayRedirect
	case response.StatusCode >= 400:
		drainRelayBody(response.Body)
		return nil, "", 0, "", &originStatusError{status: response.StatusCode}
	}
	body := cache.WithResponseMetadata(response.Body, response.Header)
	return body, response.Header.Get("Content-Type"), response.ContentLength, "relay", nil
}

func (h *Handler) buildOriginRequest(
	ctx context.Context,
	inbound *http.Request,
	exchange *plan,
	method string,
) (*http.Request, error) {
	originRequest, err := http.NewRequestWithContext(ctx, method, exchange.targetRaw, nil)
	if err != nil {
		return nil, err
	}
	userAgent := ""
	if inbound != nil {
		userAgent = strings.TrimSpace(inbound.Header.Get("User-Agent"))
	}
	if userAgent == "" {
		userAgent = "depsilo-relay/0.1"
	}
	originRequest.Header.Set("User-Agent", userAgent)
	if inbound != nil {
		for _, name := range []string{"Accept", "Accept-Language", "Range"} {
			if value := inbound.Header.Get(name); value != "" {
				originRequest.Header.Set(name, value)
			}
		}
		// Credentials only travel to the exact origin they were configured
		// for; a redirect or artifact CDN never receives them.
		if exchange.sourceURL != nil && sameOrigin(exchange.target, exchange.sourceURL) {
			if authorization := inbound.Header.Get("Authorization"); authorization != "" {
				originRequest.Header.Set("Authorization", authorization)
			}
		}
	}
	return originRequest, nil
}

var relayResponseHeaders = []string{
	"Accept-Ranges",
	"Content-Disposition",
	"Content-Encoding",
	"Content-Language",
	"Content-Range",
	"Digest",
	"Docker-Content-Digest",
	"ETag",
	"Last-Modified",
	"Link",
	"Location",
	"X-Checksum-Md5",
	"X-Checksum-Sha1",
	"X-Checksum-Sha256",
	"X-Checksum-Sha512",
	"X-Linked-Etag",
	"X-Linked-Size",
	"X-Repo-Commit",
}

func copyRelayResponseHeaders(destination http.Header, response *http.Response) {
	for _, name := range relayResponseHeaders {
		for _, value := range response.Header.Values(name) {
			destination.Add(name, value)
		}
	}
	if value := response.Header.Get("Content-Type"); value != "" {
		destination.Set("Content-Type", value)
	}
	if response.ContentLength > 0 {
		destination.Set("Content-Length", fmt.Sprintf("%d", response.ContentLength))
	}
}

var errRelayRedirect = errors.New("relay upstream returned a redirect")

type originStatusError struct {
	status int
}

func (failure *originStatusError) Error() string {
	return fmt.Sprintf("relay origin returned %d", failure.status)
}

// AllowStaleFallback keeps authoritative origin answers authoritative: a
// removed or now-private artifact must reach the child as an error instead of
// being rewritten from the parent's stale copy. Transient 4xx (rate limits,
// timeouts) and 5xx may still use stale bytes.
func (failure *originStatusError) AllowStaleFallback() bool {
	switch failure.status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone:
		return false
	default:
		return true
	}
}

func drainRelayBody(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, body, 64<<10)
	_ = body.Close()
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func writeRelayError(writer http.ResponseWriter, status int, code, message string) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_, _ = io.WriteString(writer, fmt.Sprintf(`{"code":%q,"message":%q}`, code, message))
}
