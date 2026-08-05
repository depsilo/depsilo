package rubygems

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/adapter/packagekey"
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

	// Quarantine gate. Only artifact downloads have a parseable
	// (gem, version); metadata paths (versions, info/*, specs.4.8.gz)
	// don't, and the helper short-circuits when we pass empties.
	if strings.HasPrefix(path, "gems/") && strings.HasSuffix(path, ".gem") {
		filename := strings.TrimPrefix(path, "gems/")
		if gem, version := packagekey.ParseRubygemsFilename(filename); gem != "" && version != "" {
			if blocked := adapter.QuarantineGate(c, "rubygems", gem, version); blocked {
				return
			}
		}
	}

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
