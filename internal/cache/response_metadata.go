package cache

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	maxResponseMetadataValueBytes      = 8 << 10
	maxResponseMetadataValuesPerHeader = 16
	maxEncodedResponseMetadataBytes    = 32 << 10
)

// responseMetadataHeaderAllowlist is intentionally narrow. Cache metadata can
// outlive the upstream response and is replayed to unrelated downstream
// requests, so credentials, redirects, cookies, and hop-by-hop headers must
// never cross this boundary.
var responseMetadataHeaderAllowlist = map[string]struct{}{
	"Accept-Ranges":         {},
	"Content-Disposition":   {},
	"Content-Encoding":      {},
	"Content-Language":      {},
	"Content-Range":         {},
	"Digest":                {},
	"Docker-Content-Digest": {},
	"Etag":                  {},
	"Last-Modified":         {},
	"Link":                  {},
	"X-Checksum-Md5":        {},
	"X-Checksum-Sha1":       {},
	"X-Checksum-Sha256":     {},
	"X-Checksum-Sha512":     {},
	"X-Linked-Etag":         {},
	"X-Linked-Size":         {},
	"X-Repo-Commit":         {},
}

// Keep representation validators and Hugging Face pagination links ahead of
// optional presentation metadata when a malformed or manually-edited upstream
// response reaches the aggregate persistence budget.
var responseMetadataHeaderOrder = []string{
	"Etag",
	"Last-Modified",
	"Content-Encoding",
	"Content-Language",
	"Content-Range",
	"Link",
	"X-Repo-Commit",
	"X-Linked-Etag",
	"X-Linked-Size",
	"Docker-Content-Digest",
	"Digest",
	"X-Checksum-Sha256",
	"X-Checksum-Sha512",
	"X-Checksum-Sha1",
	"X-Checksum-Md5",
	"Accept-Ranges",
	"Content-Disposition",
}

type responseMetadataReadCloser struct {
	io.ReadCloser
	headers http.Header
}

// DecorateBodyIdleTimeout keeps the watchdog adjacent to the transport body.
// Protocol wrappers outside it can then observe the timeout cause before they
// finalize health or integrity state.
func (b *responseMetadataReadCloser) DecorateBodyIdleTimeout(
	timeout time.Duration,
	cancel context.CancelCauseFunc,
) io.ReadCloser {
	b.ReadCloser = WithBodyIdleTimeout(b.ReadCloser, timeout, cancel)
	return b
}

// WithResponseMetadata annotates a fetched body with replay-safe
// representation headers without changing FetchFunc's public signature.
// Headers are allowlisted and cloned immediately; later caller mutation cannot
// change what Manager persists.
func WithResponseMetadata(body io.ReadCloser, headers http.Header) io.ReadCloser {
	if body == nil {
		return nil
	}
	merged := responseMetadataFrom(body)
	for key, values := range sanitizeResponseMetadata(headers) {
		merged[key] = append([]string(nil), values...)
	}
	return &responseMetadataReadCloser{
		ReadCloser: body,
		headers:    sanitizeResponseMetadata(merged),
	}
}

func responseMetadataFrom(body io.ReadCloser) http.Header {
	if wrapped, ok := body.(*responseMetadataReadCloser); ok {
		return cloneResponseMetadata(wrapped.headers)
	}
	return make(http.Header)
}

func sanitizeResponseMetadata(headers http.Header) http.Header {
	hopByHop := make(map[string]struct{})
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if http.CanonicalHeaderKey(key) == "Connection" {
			for _, value := range headers[key] {
				for _, name := range strings.Split(value, ",") {
					if canonical := http.CanonicalHeaderKey(strings.TrimSpace(name)); canonical != "" {
						hopByHop[canonical] = struct{}{}
					}
				}
			}
		}
	}

	candidates := make(http.Header)
	for _, key := range keys {
		canonicalKey := http.CanonicalHeaderKey(key)
		if _, allowed := responseMetadataHeaderAllowlist[canonicalKey]; !allowed {
			continue
		}
		if _, nominated := hopByHop[canonicalKey]; nominated {
			continue
		}
		for _, value := range headers[key] {
			if len(candidates[canonicalKey]) >= maxResponseMetadataValuesPerHeader {
				break
			}
			// net/http rejects response splitting too, but reject it at the
			// persistence boundary so a manually-edited legacy row cannot replay
			// an unsafe value through a future adapter.
			if len(value) > maxResponseMetadataValueBytes || strings.ContainsAny(value, "\r\n") {
				continue
			}
			if canonicalKey == "Link" && !safeLocalLinkValue(value) {
				continue
			}
			candidates[canonicalKey] = append(candidates[canonicalKey], value)
		}
	}

	safe := make(http.Header)
	encodedSize := len("{}")
	// Admit one value from each header before considering its next value. This
	// prevents repeated optional values from crowding out independent
	// validators while preserving every admitted value verbatim.
	for valueIndex := 0; valueIndex < maxResponseMetadataValuesPerHeader; valueIndex++ {
		for _, key := range responseMetadataHeaderOrder {
			values := candidates[key]
			if valueIndex >= len(values) {
				continue
			}
			value := values[valueIndex]
			nextSize := responseMetadataEncodedSizeAfterAppend(safe, key, value, encodedSize)
			if nextSize > maxEncodedResponseMetadataBytes {
				continue
			}
			safe[key] = append(safe[key], value)
			encodedSize = nextSize
		}
	}
	return safe
}

func responseMetadataEncodedSizeAfterAppend(headers http.Header, key, value string, current int) int {
	encodedValue, _ := json.Marshal(value)
	if len(headers[key]) > 0 {
		return current + 1 + len(encodedValue)
	}
	encodedKey, _ := json.Marshal(key)
	separator := 0
	if len(headers) > 0 {
		separator = 1
	}
	return current + separator + len(encodedKey) + 1 + 1 + len(encodedValue) + 1
}

// safeLocalLinkValue admits only links that stay inside the Hugging Face
// adapter. The adapter normalizes upstream absolute URLs before persistence;
// rejecting everything else keeps cached metadata from becoming an open
// redirect or bypassing project routing.
func safeLocalLinkValue(value string) bool {
	if len(value) > maxResponseMetadataValueBytes {
		return false
	}
	found := false
	for cursor := 0; cursor < len(value); {
		openOffset := strings.IndexByte(value[cursor:], '<')
		if openOffset < 0 {
			return found
		}
		open := cursor + openOffset
		closeOffset := strings.IndexByte(value[open+1:], '>')
		if closeOffset < 0 {
			return false
		}
		closeIndex := open + 1 + closeOffset
		target, err := url.Parse(value[open+1 : closeIndex])
		if err != nil || target.IsAbs() || target.Host != "" ||
			!strings.HasPrefix(target.Path, "/huggingface") ||
			len(target.Path) > len("/huggingface") && target.Path[len("/huggingface")] != '/' {
			return false
		}
		found = true
		cursor = closeIndex + 1
	}
	return found
}

func cloneResponseMetadata(headers http.Header) http.Header {
	return sanitizeResponseMetadata(headers)
}

func encodeResponseMetadata(headers http.Header) string {
	safe := sanitizeResponseMetadata(headers)
	if len(safe) == 0 {
		return ""
	}
	encoded, err := json.Marshal(safe)
	if err != nil {
		// http.Header contains only strings, so Marshal cannot fail in practice.
		// Keeping this helper total avoids turning optional representation
		// metadata into a cache persistence failure.
		return ""
	}
	if len(encoded) > maxEncodedResponseMetadataBytes {
		return ""
	}
	return string(encoded)
}

func decodeResponseMetadata(encoded string) http.Header {
	if encoded == "" {
		return make(http.Header)
	}
	// Legacy rows predate the bounded encoder. Reject an oversized cell before
	// JSON parsing so manually-edited or corrupt metadata cannot create
	// unbounded decode work.
	if len(encoded) > maxEncodedResponseMetadataBytes {
		return make(http.Header)
	}
	var headers http.Header
	if err := json.Unmarshal([]byte(encoded), &headers); err != nil {
		return make(http.Header)
	}
	return sanitizeResponseMetadata(headers)
}

func decodeStoredResponseMetadata(encoded, etag, lastModified string) http.Header {
	headers := decodeResponseMetadata(encoded)
	// Cache rows created before response_headers was introduced still carry
	// validators in their dedicated columns. Synthesize them on read so the new
	// GetResult contract is backwards-compatible without a data rewrite.
	if headers.Get("ETag") == "" && etag != "" {
		headers.Set("ETag", etag)
	}
	if headers.Get("Last-Modified") == "" && lastModified != "" {
		headers.Set("Last-Modified", lastModified)
	}
	return sanitizeResponseMetadata(headers)
}
