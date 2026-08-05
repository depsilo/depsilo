package apt

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

// Handler implements the APT adapter.
// APT responses are fully passthrough — no content modification — to preserve GPG signatures.
type Handler struct {
	proxy *adapter.TransparentProxy
	cfg   config.CacheConfig
}

func New(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB) *Handler {
	return &Handler{
		proxy: adapter.NewTransparentProxy("apt", cacheMgr, selector, database),
		cfg:   cfg,
	}
}

func (h *Handler) Type() string { return "apt" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/:repo/dists/*filepath", h.handleRequest)
	rg.GET("/:repo/pool/*filepath", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	repo := c.Param("repo")
	filepath := c.Param("filepath")

	// Build the full upstream path: /{repo}/dists/{filepath} or /{repo}/pool/{filepath}
	// Gin strips the matched prefix, so we reconstruct from the full URL path
	fullPath := c.Request.URL.Path
	// Remove the /apt prefix to get the upstream path
	upstreamPath := fullPath[len("/apt"):]

	cacheKey := CacheKey(repo, upstreamPath)

	ttl := h.cfg.TTLBlob
	if IsMetadata(filepath) {
		ttl = h.cfg.TTLIndex
	}

	h.proxy.Serve(c, adapter.TransparentPlan{
		Path:               upstreamPath,
		CacheKey:           cacheKey,
		TTL:                ttl,
		DefaultContentType: detectContentType(filepath),
	})
}

func detectContentType(path string) string {
	switch {
	case len(path) > 4 && path[len(path)-4:] == ".deb":
		return "application/vnd.debian.binary-package"
	case len(path) > 3 && path[len(path)-3:] == ".gz":
		return "application/gzip"
	case len(path) > 3 && path[len(path)-3:] == ".xz":
		return "application/x-xz"
	default:
		return "application/octet-stream"
	}
}

// Ensure compile-time interface compliance.
var _ interface{ Type() string } = (*Handler)(nil)
