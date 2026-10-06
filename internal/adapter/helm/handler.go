package helm

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

type Handler struct {
	cacheMgr *cache.Manager
	selector upstream.Selector
	proxy    *adapter.TransparentProxy
	cfg      config.CacheConfig
	// provenanceRequired enables index-resolved chart identity plus
	// approximate Last-Modified evidence. The composition root sets it only
	// when a positive helm threshold is active.
	provenanceRequired bool
	approximate        *adapter.ApproximateProvenance
	identityMemo       *helmIdentityMemo
}

func New(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB) *Handler {
	return &Handler{
		cacheMgr:     cacheMgr,
		selector:     selector,
		proxy:        adapter.NewTransparentProxy("helm", cacheMgr, selector, database),
		cfg:          cfg,
		approximate:  adapter.NewApproximateProvenance(),
		identityMemo: &helmIdentityMemo{},
	}
}

func (h *Handler) Type() string { return "helm" }

// SetProvenanceRequired enables index-resolved chart identity and
// approximate Last-Modified provenance for chart archives.
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

	// Chart filenames cannot be split safely into name/version when either
	// component contains hyphens, so the gate resolves the identity from the
	// repository index and HEADs the exact artifact for the approximate
	// publish time. An artifact the index does not declare is refused.
	if h.provenanceRequired && strings.HasSuffix(path, ".tgz") {
		identity, err := h.resolveChartIdentity(c.Request.Context(), path)
		if err != nil {
			zap.L().Warn("helm chart provenance unavailable; refusing to serve",
				zap.String("path", path),
				zap.Error(err),
			)
		}
		if !identity.ok() {
			c.JSON(http.StatusUnavailableForLegalReasons, gin.H{
				"code":    "QUARANTINED",
				"message": "helm chart provenance is unavailable; refusing to serve while the minimum-release-age gate is enabled",
			})
			return
		}
		provenance, _ := h.approximate.Resolve(c.Request.Context(), h.selector, "helm", path)
		if blocked := adapter.QuarantineGateWithProvenance(c, "helm", identity.chart, identity.version, provenance); blocked {
			return
		}
	}

	cacheKey := CacheKey(path)

	// .tgz chart files get long TTL, everything else (index.yaml, etc.) short
	ttl := h.cfg.TTLIndex
	if strings.HasSuffix(path, ".tgz") {
		ttl = h.cfg.TTLBlob
	}

	h.proxy.Serve(c, adapter.TransparentPlan{Path: path, CacheKey: cacheKey, TTL: ttl})
}
