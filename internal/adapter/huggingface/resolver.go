package huggingface

import (
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Resolver performs upstream request → optional 302 follow → response
// streaming. It is the heart of the HuggingFace adapter — see spec §4
// (Server-side following) for the design rationale.
//
// The struct holds no per-request state; it's safe to share one instance
// across all requests. The HTTP clients are reused so connections pool
// naturally.
type Resolver struct {
	// nonFollowing makes outbound calls to upstream (huggingface.co or
	// hf-mirror.com). We MUST NOT follow redirects automatically — we
	// need to inspect the 302's Location and X-Linked-Etag headers.
	nonFollowing *http.Client

	// cdnClient makes outbound calls to the signed CDN URL after a 302.
	// Default redirect-following is fine here; CDN URLs do not normally
	// chain further redirects.
	cdnClient *http.Client
}

// NewResolver builds a Resolver with sensible defaults for HuggingFace's
// traffic profile. The 30-minute CDN timeout accommodates multi-gigabyte
// model files on slow links.
func NewResolver() *Resolver {
	return &Resolver{
		nonFollowing: &http.Client{
			Timeout: 5 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		cdnClient: &http.Client{
			Timeout: 30 * time.Minute,
		},
	}
}

// Handle runs the full upstream-fetch + optional-redirect-follow flow for
// a single request. `upstreamBase` is e.g. "https://huggingface.co" and
// `requestPath` is the request URL path (e.g. "/repo/resolve/main/x").
//
// Caching: this method does not interact with cache.Manager directly —
// callers (the handler) are responsible for wrapping Handle in a cache
// lookup/write. The reason for the split: the resolver focuses on
// HuggingFace's redirect protocol; the handler owns cache-policy decisions
// (auth bypass, ref-classified TTL).
func (r *Resolver) Handle(c *gin.Context, upstreamBase, requestPath string) {
	upURL := upstreamBase + requestPath

	upReq, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, upURL, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "build upstream request: %v", err)
		return
	}
	CopyRequestHeaders(c, upReq)

	upResp, err := r.nonFollowing.Do(upReq)
	if err != nil {
		zap.L().Warn("huggingface upstream call failed",
			zap.String("url", upURL), zap.Error(err))
		c.String(http.StatusBadGateway, "upstream error: %v", err)
		return
	}
	defer upResp.Body.Close()

	CopyResponseHeaders(c, upResp)

	switch upResp.StatusCode {
	case http.StatusOK:
		// Non-LFS small file: pass body straight through.
		c.Status(http.StatusOK)
		if c.Request.Method != http.MethodHead {
			if _, err := io.Copy(c.Writer, upResp.Body); err != nil {
				zap.L().Warn("huggingface stream to client interrupted",
					zap.String("url", upURL), zap.Error(err))
			}
		}

	case http.StatusFound, http.StatusMovedPermanently,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		// HEAD: just expose the headers we already copied, don't fetch the body.
		if c.Request.Method == http.MethodHead {
			c.Status(http.StatusOK)
			return
		}

		// GET: follow the redirect server-side to the (typically signed) CDN URL.
		// The signed URL carries its own auth in query params; we MUST NOT
		// forward client Authorization to the CDN.
		signed := upResp.Header.Get("Location")
		if signed == "" {
			c.String(http.StatusBadGateway, "upstream redirect missing Location")
			return
		}
		innerReq, err := http.NewRequestWithContext(c.Request.Context(),
			http.MethodGet, signed, nil)
		if err != nil {
			c.String(http.StatusInternalServerError, "build CDN request: %v", err)
			return
		}
		// Forward Range/User-Agent/Accept-Encoding only; deliberately drop Authorization.
		for _, h := range []string{"User-Agent", "Range", "Accept-Encoding"} {
			if v := c.GetHeader(h); v != "" {
				innerReq.Header.Set(h, v)
			}
		}

		innerResp, err := r.cdnClient.Do(innerReq)
		if err != nil {
			zap.L().Warn("huggingface CDN fetch failed",
				zap.String("url", signed), zap.Error(err))
			c.String(http.StatusBadGateway, "cdn error: %v", err)
			return
		}
		defer innerResp.Body.Close()

		// Headers from the inner response (Content-Length is what the
		// client needs; the upstream's X-Linked-* are already on c.Header).
		if cl := innerResp.Header.Get("Content-Length"); cl != "" {
			c.Header("Content-Length", cl)
		}
		if ct := innerResp.Header.Get("Content-Type"); ct != "" {
			c.Header("Content-Type", ct)
		}
		c.Status(innerResp.StatusCode)
		if _, err := io.Copy(c.Writer, innerResp.Body); err != nil {
			zap.L().Warn("huggingface CDN stream to client interrupted",
				zap.String("url", signed), zap.Error(err))
		}

	default:
		// 4xx / 5xx — pass status and body through. Not cached.
		c.Status(upResp.StatusCode)
		if c.Request.Method != http.MethodHead {
			if _, err := io.Copy(c.Writer, upResp.Body); err != nil {
				zap.L().Warn("huggingface error-body stream to client interrupted",
					zap.String("url", upURL), zap.Error(err))
			}
		}
	}
}
