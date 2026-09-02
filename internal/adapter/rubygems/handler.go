package rubygems

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

type Handler struct {
	proxy *adapter.TransparentProxy
	cfg   config.CacheConfig
}

func New(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB) *Handler {
	return &Handler{proxy: adapter.NewTransparentProxy("rubygems", cacheMgr, selector, database), cfg: cfg}
}

func (h *Handler) Type() string { return "rubygems" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")
	if path == "" {
		c.Status(http.StatusNotFound)
		return
	}

	cacheKey := CacheKey(path)

	// Platform gem filenames do not expose a reversible name/version/platform
	// boundary. Quarantine stays disabled until compact-index provenance or the
	// embedded gemspec can establish the identity without guessing.

	// Determine TTL by path type
	ttl := h.cfg.TTLIndex // default short for metadata
	if strings.HasPrefix(path, "gems/") && strings.HasSuffix(path, ".gem") {
		// .gem files are immutable (version-specific)
		ttl = h.cfg.TTLBlob
	} else if strings.HasPrefix(path, "quick/") && strings.HasSuffix(path, ".gemspec.rz") {
		// gemspec files are version-specific and immutable
		ttl = h.cfg.TTLBlob
	}
	// Everything else (versions, info/*, specs.4.8.gz, etc.) uses short TTL

	h.proxy.Serve(c, adapter.TransparentPlan{Path: path, CacheKey: cacheKey, TTL: ttl})
}
