package huggingface

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// hfPassthroughHeaders is the set of upstream response headers that
// huggingface_hub and related clients rely on for verification, size
// reporting, and resume support. Anything outside this list is treated
// as opaque metadata and dropped at the proxy boundary.
var hfPassthroughHeaders = []string{
	"X-Linked-Etag",      // SHA256 of the LFS blob — client compares against received bytes
	"X-Linked-Size",      // expected byte count
	"X-Repo-Commit",      // resolved commit SHA the ref pointed to
	"ETag",               // standard HTTP ETag
	"Content-Length",     // standard
	"Content-Type",       // standard
	"Accept-Ranges",      // tells clients resume is supported
	"Last-Modified",      // standard
	"Cache-Control",      // upstream's caching hint (we honor it for metadata)
}

// AuthBypass reports whether the incoming request carries an Authorization
// header, in which case we (a) forward it to upstream verbatim and
// (b) skip the cache entirely on both read and write.
func AuthBypass(c *gin.Context) bool {
	return c.GetHeader("Authorization") != ""
}

// CopyRequestHeaders forwards headers from the gin context to an outbound
// upstream request: Authorization (if present), User-Agent, Range, Accept,
// If-None-Match, If-Modified-Since. Anything else is dropped so the proxy
// doesn't leak host-specific headers (Host, Cookie, X-Forwarded-*) upstream.
func CopyRequestHeaders(c *gin.Context, upReq *http.Request) {
	for _, h := range []string{
		"Authorization",
		"User-Agent",
		"Range",
		"Accept",
		"Accept-Encoding",
		"If-None-Match",
		"If-Modified-Since",
	} {
		if v := c.GetHeader(h); v != "" {
			upReq.Header.Set(h, v)
		}
	}
}

// CopyResponseHeaders mirrors the allow-listed HuggingFace headers from an
// upstream response onto the gin response writer. Idempotent; safe to call
// multiple times (later calls overwrite earlier values).
func CopyResponseHeaders(c *gin.Context, resp *http.Response) {
	for _, h := range hfPassthroughHeaders {
		if v := resp.Header.Get(h); v != "" {
			c.Header(h, v)
		}
	}
}
