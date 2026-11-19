package helm

import (
	"io"
	"net/http"
	pathpkg "path"
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
	proxy := adapter.NewTransparentProxy("helm", cacheMgr, selector, database)
	// The served index names its charts relative to Depsilo, and a pool may
	// join different repositories, as the default one does. An index filled
	// by one member while another was failing stays cached after the other
	// answers again, and that member does not hold the charts it lists. A
	// chart one member answers 404 for is therefore asked of the others,
	// which reaches the member whose index named it, as the absolute URL did.
	proxy.RetryMissingArtifacts = true
	return &Handler{
		cacheMgr:     cacheMgr,
		selector:     selector,
		proxy:        proxy,
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

	h.proxy.Serve(c, h.plan(path))
}

// plan is the single description of how a repository path is fetched and
// cached. Serving a request and resolving a chart identity both go through
// it, so an index entry holds the same rewritten representation whichever of
// them filled it.
func (h *Handler) plan(path string) adapter.TransparentPlan {
	// .tgz chart files get long TTL, everything else (index.yaml, etc.) short
	ttl := h.cfg.TTLIndex
	if strings.HasSuffix(path, ".tgz") {
		ttl = h.cfg.TTLBlob
	}
	plan := adapter.TransparentPlan{Path: path, CacheKey: CacheKey(path), TTL: ttl}
	if pathpkg.Base(path) == "index.yaml" {
		directory := pathpkg.Dir(path)
		plan.Rewrite = func(body io.ReadCloser, served *upstream.Upstream) io.ReadCloser {
			repoURL := served.URL
			if directory != "." {
				repoURL = strings.TrimRight(repoURL, "/") + "/" + directory
			}
			return rewriteHelmIndex(body, repoURL)
		}
	}
	return plan
}
