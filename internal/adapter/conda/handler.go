package conda

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
	selector upstream.Selector
	proxy    *adapter.TransparentProxy
	cfg      config.CacheConfig
	// provenanceRequired enables Last-Modified-based provenance. The operator
	// must acknowledge the approximate source before the policy accepts a
	// positive conda threshold.
	provenanceRequired bool
	provenanceMemo     *condaProvenanceMemo
}

func New(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB) *Handler {
	return &Handler{
		selector:       selector,
		proxy:          adapter.NewTransparentProxy("conda", cacheMgr, selector, database),
		cfg:            cfg,
		provenanceMemo: &condaProvenanceMemo{},
	}
}

func (h *Handler) Type() string { return "conda" }

// SetProvenanceRequired enables Last-Modified-based provenance for conda
// artifacts.
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

	// Quarantine gate. Only fires on .conda / .tar.bz2 artifacts;
	// repodata.json and channeldata.json pass through.
	if pkg, version := packagekey.ParseCondaPath(path); pkg != "" && version != "" {
		if h.provenanceRequired {
			provenance := adapter.QuarantineProvenance{}
			if lastModified, upstreamSource, ok := h.publishedProvenance(c.Request.Context(), path); ok {
				provenance = adapter.QuarantineProvenance{
					SourceID:  condaArtifactSourceID(upstreamSource, path),
					PublishAt: lastModified,
				}
			}
			if blocked := adapter.QuarantineGateWithProvenance(c, "conda", pkg, version, provenance); blocked {
				return
			}
		} else if blocked := adapter.QuarantineGate(c, "conda", pkg, version); blocked {
			return
		}
	}

	cacheKey := CacheKey(path)

	// Determine TTL: metadata is short, package files are long
	ttl := h.cfg.TTLIndex
	if strings.HasSuffix(path, ".tar.bz2") || strings.HasSuffix(path, ".conda") {
		ttl = h.cfg.TTLBlob
	}

	h.proxy.Serve(c, adapter.TransparentPlan{Path: path, CacheKey: cacheKey, TTL: ttl})
}
