package huggingface

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxCacheQueryValueBytes = 8 << 10

// hfPassthroughHeaders is the set of upstream response headers that
// huggingface_hub and related clients rely on for verification, size
// reporting, and resume support. Anything outside this list is treated
// as opaque metadata and dropped at the proxy boundary.
var hfPassthroughHeaders = []string{
	"X-Linked-Etag",  // SHA256 of the LFS blob — client compares against received bytes
	"X-Linked-Size",  // expected byte count
	"X-Repo-Commit",  // resolved commit SHA the ref pointed to
	"ETag",           // standard HTTP ETag
	"Content-Length", // standard
	"Content-Type",   // standard
	"Accept-Ranges",  // tells clients resume is supported
	"Content-Range",  // required for correct 206 resume responses
	"Content-Encoding",
	"Last-Modified", // standard
	"Cache-Control", // upstream's caching hint (we honor it for metadata)
	"Age",           // internal freshness input; stripped before downstream replay
	"Date",          // internal freshness input; stripped before downstream replay
	"Vary",
	"Link", // normalized to a Depsilo-local target before this is retained
}

var hfRedirectMetadataHeaders = []string{
	"X-Linked-Etag",
	"X-Linked-Size",
	"X-Repo-Commit",
	"Accept-Ranges",
}

var hfErrorPassthroughHeaders = []string{
	"X-Error-Code",
	"X-Error-Message",
	"X-Request-Id",
	"Retry-After",
	"WWW-Authenticate",
	"X-HF-Warning",
}

// hfXetConnectionHeaders carry a short-lived CAS credential. They are safe to
// expose only on the explicitly parsed xet-read-token endpoint, which the
// handler always serves directly with Cache-Control: no-store.
var hfXetConnectionHeaders = []string{
	"X-Xet-Cas-Url",
	"X-Xet-Access-Token",
	"X-Xet-Token-Expiration",
}

// upstreamAuthorization returns only credentials that may be sent to the
// Hugging Face origin. Depsilo project tokens are routing credentials for this
// service, never third-party secrets; stripping the whole family also keeps an
// invalid or expired project token from leaving the process.
func upstreamAuthorization(request *http.Request) string {
	if request == nil {
		return ""
	}
	auth := strings.TrimSpace(request.Header.Get("Authorization"))
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) == 2 &&
		strings.EqualFold(parts[0], "Bearer") &&
		strings.HasPrefix(strings.TrimSpace(parts[1]), "depsilo_proj_") {
		return ""
	}
	return auth
}

// cacheEligible permits only a complete, public GET representation. Partial,
// authenticated, HEAD, and conditional requests retain their native HTTP
// semantics by bypassing both cache reads and writes.
func cacheEligible(request *http.Request) bool {
	if request == nil || request.Method != http.MethodGet || upstreamAuthorization(request) != "" {
		return false
	}
	if hasCredentialQuery(request.URL.Query()) ||
		!cacheSafeAccept(request.Header.Values("Accept")) ||
		!acceptsIdentityEncoding(request.Header.Values("Accept-Encoding")) {
		return false
	}
	for _, name := range []string{
		"Range",
		"If-Range",
		"If-Match",
		"If-None-Match",
		"If-Modified-Since",
		"If-Unmodified-Since",
	} {
		if request.Header.Get(name) != "" {
			return false
		}
	}
	return true
}

// originRequestHeaders returns the small allow-list forwarded to the selected
// Hub origin. Cacheable representations force identity encoding: otherwise a
// gzip body could be stored and later replayed without matching negotiation.
func originRequestHeaders(request *http.Request, cacheable bool) http.Header {
	out := make(http.Header)
	if request == nil {
		return out
	}
	for _, name := range []string{
		"User-Agent",
		"Accept",
		"Accept-Encoding",
		"If-None-Match",
		"If-Modified-Since",
		"If-Match",
		"If-Unmodified-Since",
	} {
		copyHeaderValues(out, request.Header, name)
	}
	if request.Method == http.MethodGet {
		if len(request.Header.Values("Range")) > 0 {
			copyHeaderValues(out, request.Header, "Range")
			copyHeaderValues(out, request.Header, "If-Range")
			// The multipart validator operates on the media-type bytes. A
			// top-level content coding would need decoding before MIME parsing,
			// so collapse every partial request onto identity as well.
			out.Set("Accept-Encoding", "identity")
		}
	}
	if auth := upstreamAuthorization(request); auth != "" {
		out.Set("Authorization", auth)
	}
	if cacheable {
		// Collapse every cacheable request onto one upstream representation.
		// Downstream User-Agent and absent-vs-*/* differences must not create a
		// hidden Vary dimension that the cache key cannot represent.
		out.Del("User-Agent")
		out.Set("Accept", "*/*")
		out.Set("Accept-Encoding", "identity")
	}
	return out
}

// cdnRequestHeaders carries representation negotiation to a signed artifact
// URL while deliberately excluding Authorization and every other credential.
func cdnRequestHeaders(request *http.Request, cacheable bool) http.Header {
	out := make(http.Header)
	if request == nil {
		return out
	}
	for _, name := range []string{
		"User-Agent",
		"Accept",
		"Accept-Encoding",
		"If-Match",
		"If-None-Match",
		"If-Modified-Since",
		"If-Unmodified-Since",
	} {
		copyHeaderValues(out, request.Header, name)
	}
	if request.Method == http.MethodGet {
		if len(request.Header.Values("Range")) > 0 {
			copyHeaderValues(out, request.Header, "Range")
			copyHeaderValues(out, request.Header, "If-Range")
			out.Set("Accept-Encoding", "identity")
		}
	}
	if cacheable {
		out.Del("User-Agent")
		out.Set("Accept", "*/*")
		out.Set("Accept-Encoding", "identity")
	}
	return out
}

func cacheSafeQuery(parsed Parsed, query url.Values) bool {
	if len(query) == 0 {
		return true
	}
	switch parsed.Kind {
	case PathResolve, PathRaw:
		values, ok := query["download"]
		return ok && len(query) == 1 && len(values) == 1 && values[0] == "true"
	case PathAPIModelInfo, PathAPIModelRevision:
		return cacheSafeInfoQuery(query, true)
	case PathAPIDatasetInfo, PathAPIDatasetRevision:
		return cacheSafeInfoQuery(query, false)
	case PathAPIModelTree, PathAPIDatasetTree:
		return cacheSafeTreeQuery(query)
	default:
		return false
	}
}

func cacheSafeInfoQuery(query url.Values, model bool) bool {
	allowedNames := map[string]struct{}{
		"blobs": {},
	}
	if model {
		allowedNames["securityStatus"] = struct{}{}
	}
	for name, values := range query {
		if _, ok := allowedNames[name]; !ok || len(values) == 0 {
			return false
		}
		if len(values) != 1 || !strings.EqualFold(values[0], "true") {
			return false
		}
	}
	return true
}

func cacheSafeTreeQuery(query url.Values) bool {
	for name, values := range query {
		if len(values) != 1 {
			return false
		}
		value := values[0]
		switch name {
		case "recursive", "expand":
			if !strings.EqualFold(value, "true") && !strings.EqualFold(value, "false") {
				return false
			}
		case "cursor":
			if value == "" || len(value) > maxCacheQueryValueBytes {
				return false
			}
		case "limit":
			limit, err := strconv.Atoi(value)
			if err != nil || limit < 1 || limit > 1000 || strconv.Itoa(limit) != value {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func cacheResponseReusable(headers http.Header, ttl time.Duration) bool {
	directives := make(map[string]string)
	for _, line := range headers.Values("Cache-Control") {
		for _, rawDirective := range strings.Split(line, ",") {
			name, value, _ := strings.Cut(strings.TrimSpace(rawDirective), "=")
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" {
				continue
			}
			directives[name] = strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	for _, name := range []string{
		"no-store",
		"private",
		"no-cache",
		"must-revalidate",
		"proxy-revalidate",
	} {
		if _, present := directives[name]; present {
			return false
		}
	}
	ageName := "s-maxage"
	age, present := directives[ageName]
	if !present {
		ageName = "max-age"
		age, present = directives[ageName]
	}
	if present {
		seconds, err := strconv.ParseInt(age, 10, 64)
		requiredSeconds := int64(ttl / time.Second)
		if ttl%time.Second != 0 {
			requiredSeconds++
		}
		currentAge, ageOK := responseCurrentAgeSeconds(headers, time.Now())
		if err != nil ||
			seconds < 0 ||
			!ageOK ||
			currentAge > seconds ||
			seconds-currentAge < requiredSeconds {
			return false
		}
	}
	for _, line := range headers.Values("Vary") {
		for _, name := range strings.Split(line, ",") {
			if strings.TrimSpace(name) == "*" {
				return false
			}
		}
	}
	return true
}

func responseCurrentAgeSeconds(
	headers http.Header,
	now time.Time,
) (int64, bool) {
	currentAge := int64(0)
	if values := headers.Values("Age"); len(values) != 0 {
		if len(values) != 1 {
			return 0, false
		}
		age, err := strconv.ParseInt(strings.TrimSpace(values[0]), 10, 64)
		if err != nil || age < 0 {
			return 0, false
		}
		currentAge = age
	}
	if value := strings.TrimSpace(headers.Get("Date")); value != "" {
		date, err := http.ParseTime(value)
		if err != nil {
			return 0, false
		}
		if apparentAge := int64(now.Sub(date) / time.Second); apparentAge > currentAge {
			currentAge = apparentAge
		}
	}
	return currentAge, true
}

func hasCredentialQuery(query url.Values) bool {
	for name := range query {
		lower := strings.ToLower(strings.TrimSpace(name))
		switch lower {
		case "authorization",
			"token",
			"hf_token",
			"access_token",
			"auth_token",
			"api_key",
			"apikey",
			"signature",
			"sig",
			"policy",
			"key-pair-id":
			return true
		}
		if strings.HasPrefix(lower, "x-amz-") ||
			strings.HasPrefix(lower, "x-goog-") {
			return true
		}
	}
	return false
}

func cacheSafeAccept(values []string) bool {
	if len(values) == 0 {
		return true
	}
	for _, joined := range values {
		for _, value := range strings.Split(joined, ",") {
			if trimmed := strings.TrimSpace(value); trimmed != "" && trimmed != "*/*" {
				return false
			}
		}
	}
	return true
}

func acceptsIdentityEncoding(values []string) bool {
	if len(values) == 0 {
		return true
	}
	identityQuality := -1.0
	wildcardQuality := -1.0
	for _, joined := range values {
		for _, item := range strings.Split(joined, ",") {
			parts := strings.Split(item, ";")
			name := strings.ToLower(strings.TrimSpace(parts[0]))
			quality := 1.0
			for _, parameter := range parts[1:] {
				keyValue := strings.SplitN(parameter, "=", 2)
				if len(keyValue) != 2 || !strings.EqualFold(strings.TrimSpace(keyValue[0]), "q") {
					continue
				}
				parsed, err := strconv.ParseFloat(strings.TrimSpace(keyValue[1]), 64)
				if err != nil {
					quality = 0
				} else {
					quality = parsed
				}
			}
			switch name {
			case "identity":
				identityQuality = quality
			case "*":
				wildcardQuality = quality
			}
		}
	}
	if identityQuality >= 0 {
		return identityQuality > 0
	}
	if wildcardQuality >= 0 {
		return wildcardQuality > 0
	}
	return true
}

func copyHeaderValues(dst, src http.Header, name string) {
	for _, value := range src.Values(name) {
		dst.Add(name, value)
	}
}

// filterResponseHeaders returns a fresh allow-listed header map. When several
// trusted responses are supplied, later representation headers replace
// earlier values. Cross-origin artifact responses use the stricter
// filterArtifactResponseHeaders merge below.
func filterResponseHeaders(sources ...http.Header) http.Header {
	out := make(http.Header)
	for _, source := range sources {
		for _, name := range hfPassthroughHeaders {
			if values := source.Values(name); len(values) > 0 {
				out.Del(name)
				for _, value := range values {
					out.Add(name, value)
				}
			}
		}
	}
	return out
}

func filterResponseHeadersForTarget(target string, sources ...http.Header) http.Header {
	out := filterResponseHeaders(sources...)
	requestURI, err := url.ParseRequestURI(target)
	if err != nil || requestURI.IsAbs() || requestURI.Host != "" ||
		!isXetReadToken(ParseRequestPath(requestURI.EscapedPath())) {
		return out
	}
	for _, source := range sources {
		for _, name := range hfXetConnectionHeaders {
			if values := source.Values(name); len(values) > 0 {
				out.Del(name)
				for _, value := range values {
					out.Add(name, value)
				}
			}
		}
	}
	return out
}

func filterRedirectMetadata(source http.Header) http.Header {
	out := make(http.Header)
	for _, name := range hfRedirectMetadataHeaders {
		copyHeaderValues(out, source, name)
	}
	return out
}

// filterArtifactResponseHeaders combines ordinary CDN representation headers
// with metadata asserted by the Hub redirect. A signed cross-origin endpoint
// is trusted only for the bytes it serves; it must never replace the Hub's
// repository commit, linked digest, or full-object size.
func filterArtifactResponseHeaders(hub, artifact http.Header) http.Header {
	out := filterResponseHeaders(hub, artifact)
	for _, name := range []string{
		"X-Linked-Etag",
		"X-Linked-Size",
		"X-Repo-Commit",
	} {
		out.Del(name)
		copyHeaderValues(out, hub, name)
	}
	return out
}

func filterErrorResponseHeaders(source http.Header) http.Header {
	out := filterResponseHeaders(source)
	for _, name := range hfErrorPassthroughHeaders {
		copyHeaderValues(out, source, name)
	}
	return out
}

// normalizeResponseLinks rewrites same-origin Hub pagination targets to the
// local adapter mount. Absolute upstream URLs must never be replayed: clients
// following them would bypass Depsilo and any project-scoped route.
func normalizeResponseLinks(
	headers http.Header,
	configuredOrigin string,
	responseURL *url.URL,
	allowValidatedResponsePath bool,
) {
	values := headers.Values("Link")
	headers.Del("Link")
	if len(values) == 0 || responseURL == nil {
		return
	}
	origin, err := url.Parse(configuredOrigin)
	if err != nil || origin.Host == "" {
		return
	}
	for _, value := range values {
		if normalized, ok := normalizeLinkValue(
			value,
			origin,
			responseURL,
			allowValidatedResponsePath,
		); ok {
			headers.Add("Link", normalized)
		}
	}
}

func normalizeLinkValue(
	value string,
	origin, responseURL *url.URL,
	allowValidatedResponsePath bool,
) (string, bool) {
	if len(value) > 8<<10 {
		return "", false
	}
	var out strings.Builder
	found := false
	for cursor := 0; cursor < len(value); {
		openOffset := strings.IndexByte(value[cursor:], '<')
		if openOffset < 0 {
			out.WriteString(value[cursor:])
			break
		}
		open := cursor + openOffset
		closeOffset := strings.IndexByte(value[open+1:], '>')
		if closeOffset < 0 {
			return "", false
		}
		closeIndex := open + 1 + closeOffset
		reference, err := url.Parse(value[open+1 : closeIndex])
		if err != nil {
			return "", false
		}
		target := responseURL.ResolveReference(reference)
		if !sameOrigin(origin, target) || hasCredentialQuery(target.Query()) {
			return "", false
		}
		localTarget, ok := localHuggingFaceTarget(
			origin,
			target,
			responseURL,
			allowValidatedResponsePath,
		)
		if !ok {
			return "", false
		}
		out.WriteString(value[cursor : open+1])
		out.WriteString(localTarget)
		out.WriteByte('>')
		cursor = closeIndex + 1
		found = true
	}
	return out.String(), found
}

func localHuggingFaceTarget(
	origin, target, responseURL *url.URL,
	allowValidatedResponsePath bool,
) (string, bool) {
	targetPath := target.EscapedPath()
	basePath := strings.TrimSuffix(origin.EscapedPath(), "/")
	if basePath != "" && basePath != "/" {
		switch {
		case targetPath == basePath:
			targetPath = "/"
		case strings.HasPrefix(targetPath, basePath+"/"):
			targetPath = strings.TrimPrefix(targetPath, basePath)
		default:
			// A configured base-path origin may canonically redirect the request
			// to a root-relative path on the same origin. The resolver validates
			// every hop before enabling this fallback. Limit it to the exact final
			// response path: unrelated same-origin links must not acquire a new
			// route through Depsilo.
			if !allowValidatedResponsePath ||
				!sameOrigin(origin, responseURL) ||
				target.Path != responseURL.Path {
				return "", false
			}
		}
	}
	if !strings.HasPrefix(targetPath, "/") || strings.HasPrefix(targetPath, "//") {
		return "", false
	}
	local := "/huggingface" + targetPath
	if target.RawQuery != "" {
		local += "?" + target.RawQuery
	}
	if fragment := target.EscapedFragment(); fragment != "" {
		local += "#" + fragment
	}
	return local, true
}

func clientResponseHeaders(source http.Header, projectSlug string) http.Header {
	out := source.Clone()
	if projectSlug == "" {
		return out
	}
	for index, value := range out.Values("Link") {
		out["Link"][index] = rewriteProjectLink(value, projectSlug)
	}
	return out
}

func cachedClientResponseHeaders(source http.Header, projectSlug string) http.Header {
	out := clientResponseHeaders(source, projectSlug)
	// A local cache fill starts a different downstream freshness timeline.
	// Stored metadata does not normally contain these fields, but strip them
	// defensively so a legacy or manually edited row cannot replay origin age.
	out.Del("Age")
	out.Del("Date")
	return out
}

func rewriteProjectLink(value, projectSlug string) string {
	const publicPrefix = "</huggingface"
	projectPrefix := "</p/" + url.PathEscape(projectSlug) + "/huggingface"
	return strings.ReplaceAll(value, publicPrefix, projectPrefix)
}

// copyHTTPHeaders replaces destination values with a safe header snapshot.
func copyHTTPHeaders(dst, src http.Header) {
	for name, values := range src {
		dst.Del(name)
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}
