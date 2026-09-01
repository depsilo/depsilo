package huggingface

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"depsilo/internal/cache"
	"depsilo/internal/upstream"
)

// resolvedResponse is the protocol-level result of a Hugging Face request.
// The caller owns Body and must close it. Header is already restricted to the
// representation metadata safe to expose to clients and persist in cache.
type resolvedResponse struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
	Size       int64
	provenance responseProvenance
}

type responseProvenance uint8

const (
	responseFromHubOrigin responseProvenance = iota
	responseFromSignedArtifact
)

var errCanonicalCommitMismatch = errors.New("Hugging Face canonical response commit mismatch")

// resolver implements Hugging Face's origin -> signed artifact redirect
// protocol. It contains no HTTP clients or request state: all exchanges run
// through the selected Upstream, preserving its proxy and health semantics.
type resolver struct{}

func newResolver() *resolver { return &resolver{} }

const maxOriginRedirects = 10

// resolve performs one Hub origin request and, for a GET redirect, follows the
// signed artifact URL server-side. target is a relative request URI containing
// the original path and optional query. Authorization is sent only to the Hub;
// a signed artifact request receives representation negotiation headers only.
func (r *resolver) resolve(
	ctx context.Context,
	selected *upstream.Upstream,
	inbound *http.Request,
	target string,
	cacheable bool,
) (*resolvedResponse, error) {
	if selected == nil {
		return nil, errors.New("nil Hugging Face upstream")
	}
	if inbound == nil {
		return nil, errors.New("nil Hugging Face request")
	}

	health := newExchangeHealthReporter(ctx, selected)

	currentTarget := target
	currentOriginURL := ""
	relocatedOrigin := false
	followedSameOriginRedirect := false
	redirectMetadata := make(http.Header)
	for hop := 0; ; hop++ {
		requestHeaders := originRequestHeaders(inbound, cacheable)
		if relocatedOrigin {
			// Authorization belongs only to the configured Hub origin. Once a
			// canonical relocation crosses origins, every later hop stays
			// credential-free even if the chain redirects back.
			requestHeaders.Del("Authorization")
		}
		options := upstream.RequestOptions{
			Method:         inbound.Method,
			Header:         requestHeaders,
			SuppressHealth: true,
		}
		var originResponse *http.Response
		var err error
		switch {
		case currentOriginURL == "":
			originResponse, err = selected.Request(ctx, currentTarget, options)
		case relocatedOrigin:
			// RequestURL repeats external-target and guarded-dial validation for
			// each manually inspected hop. Redirect following stays disabled.
			originResponse, err = selected.RequestURL(ctx, currentOriginURL, options)
		default:
			originResponse, err = selected.RequestOriginURL(ctx, currentOriginURL, options)
		}
		if err != nil {
			health.report(false)
			return nil, err
		}

		if !isArtifactRedirect(originResponse.StatusCode) {
			headers := filterResponseHeadersForTarget(
				target,
				redirectMetadata,
				originResponse.Header,
			)
			if originResponse.StatusCode >= http.StatusBadRequest {
				for _, name := range hfErrorPassthroughHeaders {
					copyHeaderValues(headers, originResponse.Header, name)
				}
			}
			normalizeResponseLinks(
				headers,
				selected.URL,
				originResponse.Request.URL,
				followedSameOriginRedirect,
			)
			if err := validateCanonicalCommit(target, originResponse.StatusCode, headers); err != nil {
				_ = originResponse.Body.Close()
				health.reportCriticalFailure()
				return nil, err
			}
			size, sizeErr := expectedResponseBodySize(
				originResponse,
				headers,
				target,
				inbound.Header.Values("Range"),
			)
			if sizeErr != nil {
				_ = originResponse.Body.Close()
				health.reportCriticalFailure()
				return nil, sizeErr
			}
			if bodyErr := wrapMultipartPartialResponseBody(
				originResponse,
				headers,
				inbound.Header.Values("Range"),
			); bodyErr != nil {
				_ = originResponse.Body.Close()
				health.reportCriticalFailure()
				return nil, bodyErr
			}
			wrapExpectedResponseBody(originResponse, inbound.Method, size)
			health.observeOriginResponse(originResponse, inbound.Method)
			return resolvedHTTPResponse(
				originResponse,
				headers,
				size,
				responseFromHubOrigin,
			), nil
		}

		redirectMetadata = filterResponseHeaders(
			redirectMetadata,
			filterRedirectMetadata(originResponse.Header),
		)
		baseURL := originResponse.Request.URL
		redirectURL, resolveErr := resolveRedirectLocation(baseURL, originResponse.Header.Get("Location"))
		drainAndCloseRedirect(originResponse.Body)
		if resolveErr != nil {
			health.report(false)
			return nil, resolveErr
		}

		if sameOrigin(baseURL, redirectURL) {
			if hop+1 >= maxOriginRedirects {
				health.report(false)
				return nil, errors.New("Hugging Face origin exceeded redirect limit")
			}
			// Canonical Hub redirects are still origin requests. Keep sanitized
			// Hugging Face Authorization and account for every hop in origin
			// health; only a cross-origin signed artifact drops credentials.
			currentOriginURL = redirectURL.String()
			followedSameOriginRedirect = true
			continue
		}
		if isCrossOriginCanonicalRelocation(baseURL, redirectURL, redirectMetadata) {
			if hop+1 >= maxOriginRedirects {
				health.report(false)
				return nil, errors.New("Hugging Face origin exceeded redirect limit")
			}
			currentOriginURL = redirectURL.String()
			relocatedOrigin = true
			followedSameOriginRedirect = true
			continue
		}

		if inbound.Method == http.MethodHead {
			// A metadata-bearing cross-origin LFS redirect is the final logical
			// HEAD response; never fetch the signed artifact merely for headers.
			health.observeFinalHeaders()
			size := int64(-1)
			if linkedSize, ok := parseResponseSize(redirectMetadata.Get("X-Linked-Size")); ok {
				// A Hub LFS redirect's Content-Length describes its small
				// redirect document, not the artifact. huggingface_hub uses
				// HEAD for size validation, so expose the linked size.
				size = linkedSize
				redirectMetadata.Set("Content-Length", strconv.FormatInt(linkedSize, 10))
			}
			if err := validateCanonicalCommit(target, http.StatusOK, redirectMetadata); err != nil {
				health.reportCriticalFailure()
				return nil, err
			}
			health.report(true)
			return &resolvedResponse{
				StatusCode: http.StatusOK,
				Header:     redirectMetadata,
				Body:       http.NoBody,
				Size:       size,
				provenance: responseFromHubOrigin,
			}, nil
		}

		artifactResponse, requestErr := selected.RequestURL(ctx, redirectURL.String(), upstream.RequestOptions{
			Method:          http.MethodGet,
			Header:          cdnRequestHeaders(inbound, cacheable),
			FollowRedirects: true,
		})
		if requestErr != nil {
			// upstream.RequestURL already redacts path/query/userinfo. Do not
			// wrap redirectURL here: its query commonly carries a signature.
			health.report(false)
			return nil, requestErr
		}
		headers := filterArtifactResponseHeaders(redirectMetadata, artifactResponse.Header)
		if artifactResponse.StatusCode >= http.StatusBadRequest {
			for _, name := range hfErrorPassthroughHeaders {
				copyHeaderValues(headers, artifactResponse.Header, name)
			}
		}
		normalizeResponseLinks(headers, selected.URL, artifactResponse.Request.URL, false)
		if err := validateCanonicalCommit(target, artifactResponse.StatusCode, headers); err != nil {
			_ = artifactResponse.Body.Close()
			health.reportCriticalFailure()
			return nil, err
		}
		size, sizeErr := expectedResponseBodySize(
			artifactResponse,
			headers,
			target,
			inbound.Header.Values("Range"),
		)
		if sizeErr != nil {
			_ = artifactResponse.Body.Close()
			health.reportCriticalFailure()
			return nil, sizeErr
		}
		if bodyErr := wrapMultipartPartialResponseBody(
			artifactResponse,
			headers,
			inbound.Header.Values("Range"),
		); bodyErr != nil {
			_ = artifactResponse.Body.Close()
			health.reportCriticalFailure()
			return nil, bodyErr
		}
		wrapExpectedResponseBody(artifactResponse, http.MethodGet, size)
		health.observeArtifactResponse(artifactResponse)
		return resolvedHTTPResponse(
			artifactResponse,
			headers,
			size,
			responseFromSignedArtifact,
		), nil
	}
}

func isCrossOriginCanonicalRelocation(
	current, target *url.URL,
	redirectMetadata http.Header,
) bool {
	if current == nil || target == nil || sameOrigin(current, target) ||
		target.User != nil || target.Fragment != "" ||
		current.EscapedPath() != target.EscapedPath() ||
		current.RawQuery != target.RawQuery {
		return false
	}
	// A signed artifact redirect carries Hub-asserted repository identity or
	// size metadata. Only metadata-free, byte-for-byte target relocations are
	// eligible to become another origin hop.
	for _, name := range []string{"X-Repo-Commit", "X-Linked-Etag", "X-Linked-Size"} {
		if redirectMetadata.Get(name) != "" {
			return false
		}
	}
	return true
}

// validateCanonicalCommit protects every explicit-commit route, including
// direct cache-bypass requests such as Range, authenticated, and unusual-query
// downloads. Mirrors may omit X-Repo-Commit, but if they provide it on a
// successful response it must identify the requested immutable snapshot.
func validateCanonicalCommit(target string, status int, headers http.Header) error {
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil
	}
	path, _, _ := strings.Cut(target, "?")
	expected := ParseRequestPath(path).Ref
	if !IsCommitSHA(expected) {
		return nil
	}
	if commit := strings.TrimSpace(headers.Get("X-Repo-Commit")); commit != "" && commit != expected {
		return errCanonicalCommitMismatch
	}
	return nil
}

type exchangeHealthReporter struct {
	ctx      context.Context
	selected *upstream.Upstream
	started  time.Time
	once     sync.Once

	latencyMu     sync.Mutex
	headerLatency time.Duration
	headerSeen    bool
}

func newExchangeHealthReporter(
	ctx context.Context,
	selected *upstream.Upstream,
) *exchangeHealthReporter {
	return &exchangeHealthReporter{ctx: ctx, selected: selected, started: time.Now()}
}

func (r *exchangeHealthReporter) observeFinalHeaders() {
	if r == nil {
		return
	}
	r.latencyMu.Lock()
	if !r.headerSeen {
		r.headerLatency = time.Since(r.started)
		r.headerSeen = true
	}
	r.latencyMu.Unlock()
}

func (r *exchangeHealthReporter) report(success bool) {
	if r == nil || r.selected == nil {
		return
	}
	r.once.Do(func() {
		r.selected.Report(r.latency(), success)
	})
}

func (r *exchangeHealthReporter) reportCriticalFailure() {
	if r == nil || r.selected == nil {
		return
	}
	r.once.Do(func() {
		r.selected.ReportCriticalFailure(r.latency())
	})
}

func (r *exchangeHealthReporter) latency() time.Duration {
	r.latencyMu.Lock()
	defer r.latencyMu.Unlock()
	if r.headerSeen {
		return r.headerLatency
	}
	return time.Since(r.started)
}

func (r *exchangeHealthReporter) observeOriginResponse(response *http.Response, method string) {
	if response == nil {
		r.report(false)
		return
	}
	r.observeFinalHeaders()
	if responseCarriesSuccessfulBody(response, method) {
		response.Body = &healthReportingBody{ReadCloser: response.Body, reporter: r}
		return
	}
	// Client-side statuses such as a missing model prove that the origin is
	// responsive; server failures do not.
	r.report(response.StatusCode < http.StatusInternalServerError)
}

func (r *exchangeHealthReporter) observeArtifactResponse(response *http.Response) {
	if response == nil {
		r.report(false)
		return
	}
	r.observeFinalHeaders()
	if responseCarriesSuccessfulBody(response, http.MethodGet) {
		response.Body = &healthReportingBody{ReadCloser: response.Body, reporter: r}
		return
	}
	// Once the Hub has issued a signed artifact URL, a 401/403/404 means that
	// logical upstream is broken. 416 remains a valid client Range outcome.
	healthy := response.StatusCode >= http.StatusOK &&
		response.StatusCode < http.StatusBadRequest
	if response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		healthy = true
	}
	r.report(healthy)
}

func responseCarriesSuccessfulBody(response *http.Response, method string) bool {
	return method != http.MethodHead &&
		response.StatusCode >= http.StatusOK &&
		response.StatusCode < http.StatusMultipleChoices &&
		response.ContentLength != 0 &&
		response.Body != nil &&
		response.Body != http.NoBody
}

type healthReportingBody struct {
	io.ReadCloser
	reporter *exchangeHealthReporter
}

// DecorateBodyIdleTimeout places the watchdog inside health accounting. When
// Close unblocks a stalled transport Read, the timeout cause is therefore
// visible before healthReportingBody decides whether the exchange succeeded.
func (b *healthReportingBody) DecorateBodyIdleTimeout(
	timeout time.Duration,
	cancel context.CancelCauseFunc,
) io.ReadCloser {
	b.ReadCloser = cache.WithBodyIdleTimeout(b.ReadCloser, timeout, cancel)
	return b
}

func (b *healthReportingBody) Read(buffer []byte) (int, error) {
	n, err := b.ReadCloser.Read(buffer)
	cause := context.Cause(b.reporter.ctx)
	switch {
	case errors.Is(err, cache.ErrBodySizeMismatch):
		b.reporter.reportCriticalFailure()
	case cause != nil && !errors.Is(cause, context.Canceled):
		// A timeout can close the transport concurrently with a final Read that
		// still returns progress and no error. The context cause is the shared
		// linearization point, so do not require err != nil before recording the
		// failed exchange.
		b.reporter.report(false)
	case cause != nil && errors.Is(cause, context.Canceled):
		// An ordinary downstream cancellation says nothing about upstream
		// health, regardless of which close error the transport returns.
	case err == io.EOF:
		b.reporter.report(true)
	case err != nil && !errors.Is(err, context.Canceled):
		b.reporter.report(false)
	}
	return n, err
}

func (b *healthReportingBody) Close() error {
	cause := context.Cause(b.reporter.ctx)
	if cause != nil && !errors.Is(cause, context.Canceled) {
		// The cache idle watchdog cancels the exchange before closing its nested
		// body. Report here as well as in Read so a timeout during an active
		// upstream Read cannot disappear when closing the transport unblocks that
		// Read with progress, EOF, or no error.
		b.reporter.report(false)
	}
	return b.ReadCloser.Close()
}

func drainAndCloseRedirect(body io.ReadCloser) {
	if body == nil {
		return
	}
	// Do not drain an untrusted redirect body. Artifact transfers deliberately
	// have no total-body deadline, so even a one-byte read could otherwise block
	// redirect processing forever. Closing unread HTTP/1.1 bodies may forfeit
	// reuse of that origin connection, but bounds the exchange without a timeout
	// goroutine that could itself leak behind an uninterruptible Read.
	_ = body.Close()
}

func isArtifactRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func resolveRedirectLocation(base *url.URL, location string) (*url.URL, error) {
	if base == nil || location == "" {
		return nil, errors.New("Hugging Face redirect missing a valid Location")
	}
	reference, err := url.Parse(location)
	if err != nil {
		return nil, errors.New("Hugging Face redirect has an invalid Location")
	}
	resolved := base.ResolveReference(reference)
	if resolved.Scheme != "http" && resolved.Scheme != "https" || resolved.Host == "" {
		return nil, errors.New("Hugging Face redirect has an unsupported target")
	}
	return resolved, nil
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil ||
		!strings.EqualFold(left.Scheme, right.Scheme) ||
		!strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	return effectivePort(left) == effectivePort(right)
}

func effectivePort(target *url.URL) string {
	if port := target.Port(); port != "" {
		return port
	}
	if strings.EqualFold(target.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(target.Scheme, "http") {
		return "80"
	}
	return ""
}

func resolvedHTTPResponse(
	response *http.Response,
	headers http.Header,
	size int64,
	provenance responseProvenance,
) *resolvedResponse {
	body := response.Body
	if body == nil {
		body = http.NoBody
	}
	return &resolvedResponse{
		StatusCode: response.StatusCode,
		Header:     headers,
		Body:       body,
		Size:       size,
		provenance: provenance,
	}
}

// expectedResponseBodySize selects the byte count of the actual response body.
// For complete artifact responses, the Hub's X-Linked-Size is authoritative;
// Content-Length from a cross-origin signed URL is only corroborating
// transport metadata. A 206 response instead uses its partial range length.
func expectedResponseBodySize(
	response *http.Response,
	headers http.Header,
	target string,
	requestRangeValues []string,
) (int64, error) {
	if response == nil {
		return -1, nil
	}
	if response.StatusCode >= http.StatusOK &&
		response.StatusCode < http.StatusMultipleChoices &&
		response.Request != nil &&
		strings.EqualFold(
			strings.TrimSpace(response.Request.Header.Get("Accept-Encoding")),
			"identity",
		) {
		if contentEncodingValues := headers.Values("Content-Encoding"); len(contentEncodingValues) > 0 {
			return -1, fmt.Errorf(
				"%w: identity request returned %d Content-Encoding values",
				cache.ErrBodySizeMismatch,
				len(contentEncodingValues),
			)
		}
	}

	linkedSizeValues := headers.Values("X-Linked-Size")
	if len(linkedSizeValues) > 1 {
		return -1, fmt.Errorf(
			"%w: response has %d X-Linked-Size values",
			cache.ErrBodySizeMismatch,
			len(linkedSizeValues),
		)
	}
	linkedSizeValue := ""
	if len(linkedSizeValues) == 1 {
		linkedSizeValue = linkedSizeValues[0]
	}
	linkedSize, hasLinkedSize := parseResponseSize(linkedSizeValue)
	if len(linkedSizeValues) == 1 && !hasLinkedSize {
		return -1, fmt.Errorf(
			"%w: invalid X-Linked-Size %q",
			cache.ErrBodySizeMismatch,
			linkedSizeValue,
		)
	}
	if response.StatusCode == http.StatusPartialContent {
		return expectedPartialBodySize(
			response,
			headers,
			linkedSize,
			hasLinkedSize,
			requestRangeValues,
		)
	}

	parsed := ParseRequestPath(strings.SplitN(target, "?", 2)[0])
	isArtifact := parsed.Kind == PathResolve || parsed.Kind == PathRaw
	if response.StatusCode == http.StatusOK &&
		isArtifact &&
		hasLinkedSize {
		if response.ContentLength >= 0 && response.ContentLength != linkedSize {
			return -1, fmt.Errorf(
				"%w: Hub X-Linked-Size is %d but response Content-Length is %d",
				cache.ErrBodySizeMismatch,
				linkedSize,
				response.ContentLength,
			)
		}
		return linkedSize, nil
	}
	if response.ContentLength >= 0 {
		return response.ContentLength, nil
	}
	if size, ok := parseResponseSize(headers.Get("Content-Length")); ok {
		return size, nil
	}
	if response.Request != nil && response.Request.Method == http.MethodHead &&
		isArtifact && hasLinkedSize {
		return linkedSize, nil
	}
	return -1, nil
}

func expectedPartialBodySize(
	response *http.Response,
	headers http.Header,
	linkedSize int64,
	hasLinkedSize bool,
	requestRangeValues []string,
) (int64, error) {
	requestedRanges, requestOK := parseRequestedByteRanges(requestRangeValues)
	if !requestOK {
		return -1, fmt.Errorf(
			"%w: partial response did not correspond to a valid Range request",
			cache.ErrBodySizeMismatch,
		)
	}

	contentRangeValues := headers.Values("Content-Range")
	if len(contentRangeValues) > 1 {
		return -1, fmt.Errorf(
			"%w: partial response has %d Content-Range values",
			cache.ErrBodySizeMismatch,
			len(contentRangeValues),
		)
	}
	if len(contentRangeValues) == 0 {
		if len(requestedRanges) > 1 {
			return expectedMultipartPartialBodySize(response, headers)
		}
		return -1, fmt.Errorf(
			"%w: invalid partial Content-Range %q",
			cache.ErrBodySizeMismatch,
			"",
		)
	}
	contentRange := strings.TrimSpace(contentRangeValues[0])
	if contentRange == "" {
		return -1, fmt.Errorf(
			"%w: invalid partial Content-Range %q",
			cache.ErrBodySizeMismatch,
			contentRange,
		)
	}
	content, rangeOK := parseContentRange(contentRange)
	if !rangeOK {
		return -1, fmt.Errorf(
			"%w: invalid partial Content-Range %q",
			cache.ErrBodySizeMismatch,
			contentRange,
		)
	}
	contentTypeValues := headers.Values("Content-Type")
	if len(contentTypeValues) > 1 {
		return -1, fmt.Errorf(
			"%w: single-part partial response has %d Content-Type values",
			cache.ErrBodySizeMismatch,
			len(contentTypeValues),
		)
	}
	contentType := ""
	if len(contentTypeValues) == 1 {
		contentType = contentTypeValues[0]
	}
	if contentType != "" {
		mediaType, _, mediaTypeErr := mime.ParseMediaType(contentType)
		if mediaTypeErr != nil {
			return -1, fmt.Errorf(
				"%w: single-part partial response used invalid Content-Type %q",
				cache.ErrBodySizeMismatch,
				contentType,
			)
		}
		if strings.EqualFold(mediaType, "multipart/byteranges") {
			return -1, fmt.Errorf(
				"%w: single-part partial response used multipart Content-Type %q",
				cache.ErrBodySizeMismatch,
				contentType,
			)
		}
	}
	if !requestedRangesMatchContentRange(
		requestedRanges,
		content,
		linkedSize,
		hasLinkedSize,
	) {
		return -1, fmt.Errorf(
			"%w: partial Content-Range %q does not satisfy requested Range %q",
			cache.ErrBodySizeMismatch,
			contentRange,
			requestRangeValues[0],
		)
	}
	if rangeOK && response.ContentLength >= 0 &&
		response.ContentLength != content.size() {
		return -1, fmt.Errorf(
			"%w: partial Content-Range is %d bytes but Content-Length is %d",
			cache.ErrBodySizeMismatch,
			content.size(),
			response.ContentLength,
		)
	}
	if rangeOK && content.hasTotal && hasLinkedSize && content.total != linkedSize {
		return -1, fmt.Errorf(
			"%w: partial Content-Range total is %d but Hub X-Linked-Size is %d",
			cache.ErrBodySizeMismatch,
			content.total,
			linkedSize,
		)
	}
	if response.ContentLength >= 0 {
		return response.ContentLength, nil
	}
	if rangeOK {
		return content.size(), nil
	}
	return -1, nil
}

func expectedMultipartPartialBodySize(
	response *http.Response,
	headers http.Header,
) (int64, error) {
	if contentRangeValues := headers.Values("Content-Range"); len(contentRangeValues) != 0 {
		return -1, fmt.Errorf(
			"%w: multipart partial response has %d top-level Content-Range values",
			cache.ErrBodySizeMismatch,
			len(contentRangeValues),
		)
	}
	contentTypeValues := headers.Values("Content-Type")
	if len(contentTypeValues) != 1 ||
		!multipartByteRangesContentType(contentTypeValues[0]) {
		return -1, fmt.Errorf(
			"%w: multiple Range request returned %d invalid multipart Content-Type values",
			cache.ErrBodySizeMismatch,
			len(contentTypeValues),
		)
	}
	if (response.Request == nil || response.Request.Method != http.MethodHead) &&
		(response.Body == nil || response.Body == http.NoBody || response.ContentLength == 0) {
		return -1, fmt.Errorf(
			"%w: multipart partial response has no body",
			cache.ErrBodySizeMismatch,
		)
	}
	if response.ContentLength >= 0 {
		return response.ContentLength, nil
	}
	if size, ok := parseResponseSize(headers.Get("Content-Length")); ok {
		return size, nil
	}
	return -1, nil
}

func multipartByteRangesContentType(value string) bool {
	_, ok := multipartByteRangesBoundary(value)
	return ok
}

func multipartByteRangesBoundary(value string) (string, bool) {
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil || !strings.EqualFold(mediaType, "multipart/byteranges") {
		return "", false
	}
	boundary := parameters["boundary"]
	if !validMultipartBoundary(boundary) {
		return "", false
	}
	return boundary, true
}

// RFC 2046 limits multipart boundaries to 70 conservative ASCII characters.
// Enforce the same grammar as mime/multipart.Writer.SetBoundary before an
// untrusted value reaches parser allocations and boundary scanning.
func validMultipartBoundary(boundary string) bool {
	if len(boundary) < 1 || len(boundary) > 70 {
		return false
	}
	end := len(boundary) - 1
	for index, character := range boundary {
		if character >= 'A' && character <= 'Z' ||
			character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' {
			continue
		}
		switch character {
		case '\'', '(', ')', '+', '_', ',', '-', '.', '/', ':', '=', '?':
			continue
		case ' ':
			if index != end {
				continue
			}
		}
		return false
	}
	return true
}

type byteContentRange struct {
	first    int64
	last     int64
	total    int64
	hasTotal bool
}

func (r byteContentRange) size() int64 {
	return r.last - r.first + 1
}

func parseContentRange(value string) (byteContentRange, bool) {
	unit, rest, found := strings.Cut(strings.TrimSpace(value), " ")
	if !found || !strings.EqualFold(unit, "bytes") {
		return byteContentRange{}, false
	}
	byteRange, totalValue, found := strings.Cut(strings.TrimSpace(rest), "/")
	if !found || byteRange == "" || byteRange == "*" || totalValue == "" {
		return byteContentRange{}, false
	}
	firstValue, lastValue, found := strings.Cut(byteRange, "-")
	if !found {
		return byteContentRange{}, false
	}
	first, firstErr := strconv.ParseInt(firstValue, 10, 64)
	last, lastErr := strconv.ParseInt(lastValue, 10, 64)
	if firstErr != nil || lastErr != nil || first < 0 || last < first ||
		last-first == math.MaxInt64 {
		return byteContentRange{}, false
	}
	result := byteContentRange{first: first, last: last}
	if totalValue == "*" {
		return result, true
	}
	total, totalErr := strconv.ParseInt(totalValue, 10, 64)
	if totalErr != nil || total <= last {
		return byteContentRange{}, false
	}
	result.total = total
	result.hasTotal = true
	return result, true
}

type requestedByteRange struct {
	first        int64
	last         int64
	suffixLength int64
	hasFirst     bool
	hasLast      bool
}

const maxRequestedByteRanges = 16

func parseSingleRequestedByteRange(values []string) (requestedByteRange, bool) {
	ranges, ok := parseRequestedByteRanges(values)
	if !ok || len(ranges) != 1 {
		return requestedByteRange{}, false
	}
	return ranges[0], true
}

func parseRequestedByteRanges(values []string) ([]requestedByteRange, bool) {
	if len(values) != 1 {
		return nil, false
	}
	unit, value, found := strings.Cut(strings.TrimSpace(values[0]), "=")
	if !found || !strings.EqualFold(strings.TrimSpace(unit), "bytes") {
		return nil, false
	}
	specifications := strings.Split(value, ",")
	if len(specifications) == 0 || len(specifications) > maxRequestedByteRanges {
		return nil, false
	}
	ranges := make([]requestedByteRange, 0, len(specifications))
	for _, specification := range specifications {
		parsed, ok := parseRequestedByteRange(specification)
		if !ok {
			return nil, false
		}
		ranges = append(ranges, parsed)
	}
	return ranges, true
}

func parseRequestedByteRange(value string) (requestedByteRange, bool) {
	firstValue, lastValue, found := strings.Cut(strings.TrimSpace(value), "-")
	if !found {
		return requestedByteRange{}, false
	}
	firstValue = strings.TrimSpace(firstValue)
	lastValue = strings.TrimSpace(lastValue)
	if firstValue == "" {
		suffixLength, err := strconv.ParseInt(lastValue, 10, 64)
		if err != nil || suffixLength <= 0 {
			return requestedByteRange{}, false
		}
		return requestedByteRange{suffixLength: suffixLength}, true
	}
	first, err := strconv.ParseInt(firstValue, 10, 64)
	if err != nil || first < 0 {
		return requestedByteRange{}, false
	}
	result := requestedByteRange{first: first, hasFirst: true}
	if lastValue == "" {
		return result, true
	}
	last, err := strconv.ParseInt(lastValue, 10, 64)
	if err != nil || last < first {
		return requestedByteRange{}, false
	}
	result.last = last
	result.hasLast = true
	return result, true
}

func (r requestedByteRange) matches(
	content byteContentRange,
	linkedSize int64,
	hasLinkedSize bool,
) bool {
	total, hasTotal := content.total, content.hasTotal
	if !hasTotal && hasLinkedSize {
		total, hasTotal = linkedSize, true
	}
	first, last, ok := r.concrete(total, hasTotal)
	if ok {
		// RFC 9110 permits a server to return only a subset of a requested
		// range. Require both response endpoints to remain inside that range.
		return content.first >= first && content.last <= last
	}
	// An open-ended range remains verifiable without a known representation
	// length: every returned byte must be at or after the requested start.
	return !hasTotal &&
		r.hasFirst &&
		!r.hasLast &&
		content.first >= r.first
}

func (r requestedByteRange) concrete(total int64, hasTotal bool) (int64, int64, bool) {
	if !r.hasFirst {
		if !hasTotal || total <= 0 {
			return 0, 0, false
		}
		first := total - r.suffixLength
		if first < 0 {
			first = 0
		}
		return first, total - 1, true
	}
	if hasTotal && r.first >= total {
		return 0, 0, false
	}
	if r.hasLast {
		last := r.last
		if hasTotal && last >= total {
			last = total - 1
		}
		return r.first, last, last >= r.first
	}
	if hasTotal {
		return r.first, total - 1, total > r.first
	}
	// An unknown-length open range has no finite upper bound, but it remains
	// concrete enough to validate a response and coalesce a nearby closed
	// member. A suffix range still cannot be located without a total.
	return r.first, math.MaxInt64, true
}

func requestedRangesMatchContentRange(
	requested []requestedByteRange,
	content byteContentRange,
	linkedSize int64,
	hasLinkedSize bool,
) bool {
	if len(requested) == 1 {
		return requested[0].matches(content, linkedSize, hasLinkedSize)
	}
	for _, candidate := range requested {
		if candidate.matches(content, linkedSize, hasLinkedSize) {
			return true
		}
	}
	total, hasTotal := content.total, content.hasTotal
	if !hasTotal && hasLinkedSize {
		total, hasTotal = linkedSize, true
	}
	type concreteRange struct {
		first int64
		last  int64
	}
	concrete := make([]concreteRange, 0, len(requested))
	for _, candidate := range requested {
		first, last, ok := candidate.concrete(total, hasTotal)
		if !ok {
			continue
		}
		concrete = append(concrete, concreteRange{first: first, last: last})
	}
	sort.Slice(concrete, func(left, right int) bool {
		if concrete[left].first == concrete[right].first {
			return concrete[left].last < concrete[right].last
		}
		return concrete[left].first < concrete[right].first
	})

	for index, interval := range concrete {
		if content.first < interval.first || content.first > interval.last {
			continue
		}
		coveredThrough := interval.last
		if content.last <= coveredThrough {
			return true
		}
		for _, next := range concrete[index+1:] {
			if next.last <= coveredThrough {
				continue
			}
			if next.first > coveredThrough {
				gap := next.first - coveredThrough - 1
				if gap > maxCoalescedRangeGap {
					return false
				}
			}
			if content.last < next.first {
				// The response ended in an unrequested gap instead of another
				// requested interval.
				return false
			}
			if next.last > coveredThrough {
				coveredThrough = next.last
			}
			if content.last <= coveredThrough {
				return true
			}
		}
		return false
	}
	return false
}

// A multipart boundary and the per-part headers normally cost around 80 bytes.
// Keep a deliberately conservative ceiling so common servers can coalesce
// nearby ranges without allowing a response to span an attacker-sized hole.
const maxCoalescedRangeGap int64 = 256

func wrapExpectedResponseBody(response *http.Response, method string, expected int64) {
	if responseCarriesSuccessfulBody(response, method) && expected >= 0 {
		response.Body = cache.WithExpectedBodySize(response.Body, expected)
	}
}

func parseResponseSize(value string) (int64, bool) {
	size, err := strconv.ParseInt(value, 10, 64)
	return size, err == nil && size >= 0
}
