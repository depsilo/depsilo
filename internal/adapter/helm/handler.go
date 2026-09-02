package helm

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
	return &Handler{proxy: adapter.NewTransparentProxy("helm", cacheMgr, selector, database), cfg: cfg}
}

func (h *Handler) Type() string { return "helm" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")
	if path == "" {
		c.Status(http.StatusNotFound)
		return
	}

	// Chart filenames cannot be split safely into name/version when either
	// component contains hyphens. Quarantine stays disabled until index.yaml
	// provenance is carried to this request instead of guessing here.

	cacheKey := CacheKey(path)

	// .tgz chart files get long TTL, everything else (index.yaml, etc.) short
	ttl := h.cfg.TTLIndex
	if strings.HasSuffix(path, ".tgz") {
		ttl = h.cfg.TTLBlob
	}

	h.proxy.Serve(c, adapter.TransparentPlan{Path: path, CacheKey: cacheKey, TTL: ttl})
}
