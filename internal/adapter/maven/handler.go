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
	selector           upstream.Selector
	proxy              *adapter.TransparentProxy
	cfg                config.CacheConfig
	provenanceRequired bool
	approximate        *adapter.ApproximateProvenance
}

func New(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB) *Handler {
	return &Handler{
		selector:    selector,
		proxy:       adapter.NewTransparentProxy("maven", cacheMgr, selector, database),
		cfg:         cfg,
		approximate: adapter.NewApproximateProvenance(),
	}
}

func (h *Handler) Type() string { return "maven" }

// SetProvenanceRequired enables approximate Last-Modified provenance for
// Maven artifacts.
func (h *Handler) SetProvenanceRequired(required bool) {
	h.provenanceRequired = required
}

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")
	if path == "" {
		c.Status(http.StatusNotFound)
		return
	}

	// Quarantine gate. Maven packaging extensions are open-ended, so the
	// repository layout identifies artifacts; metadata paths pass through.
	if coord, version := packagekey.ParseMavenPath(path); coord != "" && version != "" {
		if h.provenanceRequired {
			provenance, _ := h.approximate.Resolve(c.Request.Context(), h.selector, "maven", path)
			if blocked := adapter.QuarantineGateWithProvenance(c, "maven", coord, version, provenance); blocked {
				return
			}
		} else if blocked := adapter.QuarantineGate(c, "maven", coord, version); blocked {
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
