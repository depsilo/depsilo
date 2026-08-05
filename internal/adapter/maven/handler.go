package maven

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
	return &Handler{proxy: adapter.NewTransparentProxy("maven", cacheMgr, selector, database), cfg: cfg}
}

func (h *Handler) Type() string { return "maven" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")
	if path == "" {
		c.Status(http.StatusNotFound)
		return
	}

	// Quarantine gate. Only fires on .jar / .pom artifact paths;
	// maven-metadata.xml and snapshot manifests pass through.
	if coord, version := packagekey.ParseMavenPath(path); coord != "" && version != "" {
		if blocked := adapter.QuarantineGate(c, "maven", coord, version); blocked {
			return
		}
	}

	cacheKey := CacheKey(path)

	// Determine TTL: metadata and snapshots are short, artifacts are long
	ttl := h.cfg.TTLBlob
	if strings.HasSuffix(path, "maven-metadata.xml") || strings.Contains(path, "-SNAPSHOT") {
		ttl = h.cfg.TTLIndex
	}

	h.proxy.Serve(c, adapter.TransparentPlan{
		Path:     path,
		CacheKey: cacheKey,
		TTL:      ttl,
	})
}
