package cran

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
	return &Handler{proxy: adapter.NewTransparentProxy("cran", cacheMgr, selector, database), cfg: cfg}
}

func (h *Handler) Type() string { return "cran" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")
	if path == "" {
		c.Status(http.StatusNotFound)
		return
	}

	// Quarantine gate. Only strict source/archive/binary artifact routes carry
	// an authoritative package identity; indexes and installers pass through.
	if pkg, version := packagekey.ParseCranPath(path); pkg != "" && version != "" {
		if blocked := adapter.QuarantineGate(c, "cran", pkg, version); blocked {
			return
		}
	}

	cacheKey := CacheKey(path)

	// Determine TTL: metadata is short, package files are long
	ttl := h.cfg.TTLIndex
	if strings.HasSuffix(path, ".tar.gz") || strings.HasSuffix(path, ".zip") || strings.HasSuffix(path, ".tgz") {
		ttl = h.cfg.TTLBlob
	}

	h.proxy.Serve(c, adapter.TransparentPlan{Path: path, CacheKey: cacheKey, TTL: ttl})
}
