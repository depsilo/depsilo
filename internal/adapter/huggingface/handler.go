package huggingface

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

// Handler is the HuggingFace adapter entry point. Construction mirrors
// the simple-passthrough adapter pattern (see internal/adapter/cran for
// reference) but with a Resolver injected for the server-side redirect
// following flow.
type Handler struct {
	cacheMgr *cache.Manager
	selector upstream.Selector
	cfg      config.CacheConfig
	db       *gorm.DB
	resolver *Resolver
}

func New(cacheMgr *cache.Manager, selector upstream.Selector,
	cfg config.CacheConfig, database *gorm.DB) *Handler {
	return &Handler{
		cacheMgr: cacheMgr,
		selector: selector,
		cfg:      cfg,
		db:       database,
		resolver: NewResolver(),
	}
}

// Type implements adapter.Adapter.
func (h *Handler) Type() string { return "huggingface" }

// Register implements adapter.Adapter. We use a single catch-all route for
// both GET and HEAD; handler-internal dispatch on the parsed path decides
// how to handle each kind of request.
func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/*path", h.handleRequest)
	rg.HEAD("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := c.Param("path")
	parsed := ParseRequestPath(path)

	if parsed.Kind == PathUnknown {
		c.String(404, "unrecognized HuggingFace path")
		return
	}

	// Quarantine gate. The HF Parsed.Ref is the revision (branch /
	// tag / commit); on `/resolve/<ref>/<file>` paths we have both
	// the repo (owner/name) and the ref, which the resolver maps to
	// the model's lastModified. Skip on metadata-only requests where
	// the resolver couldn't determine a ref.
	if parsed.Repo != "" && parsed.Ref != "" {
		if blocked := adapter.QuarantineGate(c, "huggingface", parsed.Repo, parsed.Ref); blocked {
			return
		}
	}

	up, err := h.selector.Select(c.Request.Context())
	if err != nil || up == nil {
		c.String(503, "no healthy huggingface upstream")
		return
	}
	upBase := up.URL

	// Note v1 caching scope: the resolver itself currently does NOT
	// integrate with h.cacheMgr — passthrough mode. A follow-up PR will
	// wire cache.Manager.Get/Put using these keyer outputs.
	// See plan front matter "Implementation note: deferred work".
	_ = AuthBypass(c)
	_ = CacheKey(parsed)
	_ = TTLForRef(parsed.Ref)

	h.resolver.Handle(c, upBase, path)

	zap.L().Debug("huggingface request handled",
		zap.String("kind", kindString(parsed.Kind)),
		zap.String("repo", parsed.Repo),
		zap.String("ref", parsed.Ref),
		zap.Int("status", c.Writer.Status()),
	)
}

func kindString(k PathKind) string {
	switch k {
	case PathResolve:
		return "resolve"
	case PathRaw:
		return "raw"
	case PathAPIModelInfo:
		return "api/models"
	case PathAPIModelRevision:
		return "api/models/revision"
	case PathAPIModelTree:
		return "api/models/tree"
	case PathAPIDatasetInfo:
		return "api/datasets"
	case PathAPIDatasetRevision:
		return "api/datasets/revision"
	case PathAPIDatasetTree:
		return "api/datasets/tree"
	default:
		return "unknown"
	}
}
