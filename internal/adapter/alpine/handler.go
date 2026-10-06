package alpine

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

// Handler proxies Alpine Linux (apk) repositories. Alpine repos are a plain
// signed file tree (APKINDEX.tar.gz + *.apk), so this is a pure passthrough
// adapter — the response body is never modified, preserving the index signature.
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
		proxy:       adapter.NewTransparentProxy("alpine", cacheMgr, selector, database),
		cfg:         cfg,
		approximate: adapter.NewApproximateProvenance(),
	}
}

func (h *Handler) Type() string { return "alpine" }

// SetProvenanceRequired enables approximate Last-Modified provenance for APK
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

	// Quarantine gate. Only fires on .apk artifact paths;
	// APKINDEX.tar.gz and DESCRIPTION text files pass through.
	if pkg, version := packagekey.ParseAlpinePath(path); pkg != "" && version != "" {
		if h.provenanceRequired {
			provenance, _ := h.approximate.Resolve(c.Request.Context(), h.selector, "alpine", path)
			if blocked := adapter.QuarantineGateWithProvenance(c, "alpine", pkg, version, provenance); blocked {
				return
			}
		} else if blocked := adapter.QuarantineGate(c, "alpine", pkg, version); blocked {
			return
		}
	}

	cacheKey := CacheKey(path)

	// APKINDEX / text files are mutable metadata (short TTL); *.apk archives are
	// immutable (long TTL).
	ttl := h.cfg.TTLBlob
	if IsMetadata(path) {
		ttl = h.cfg.TTLIndex
	}

	h.proxy.Serve(c, adapter.TransparentPlan{Path: path, CacheKey: cacheKey, TTL: ttl})
}
