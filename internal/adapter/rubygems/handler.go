package rubygems

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
	// identityRequired enables compact-index identity resolution for .gem
	// downloads. The composition root sets it when a positive rubygems
	// threshold is active or when the known-malicious dataset covers
	// RubyGems; publish-time provenance is attached whenever the index
	// provides it.
	identityRequired bool
	provenanceMemo   *rubygemsProvenanceMemo
}

func New(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB) *Handler {
	return &Handler{
		cacheMgr:       cacheMgr,
		selector:       selector,
		proxy:          adapter.NewTransparentProxy("rubygems", cacheMgr, selector, database),
		cfg:            cfg,
		provenanceMemo: &rubygemsProvenanceMemo{},
	}
}

func (h *Handler) Type() string { return "rubygems" }

// SetProvenanceRequired enables compact-index identity resolution (and
// publish-time provenance when the index provides it) for .gem downloads.
func (h *Handler) SetProvenanceRequired(required bool) {
	h.identityRequired = required
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

	cacheKey := CacheKey(path)

	// A .gem download only carries the combined name-version[-platform]
	// filename. When the known-malicious dataset or the release-age gate
	// covers RubyGems, the compact index resolves the exact identity (and its
	// created_at/checksum); an unresolvable artifact fails closed instead of
	// being served without identity or provenance.
	if h.identityRequired && strings.HasPrefix(path, "gems/") && strings.HasSuffix(path, ".gem") {
		fullName := strings.TrimSuffix(strings.TrimPrefix(path, "gems/"), ".gem")
		resolution, err := h.publishedProvenance(c.Request.Context(), fullName)
		if err != nil {
			zap.L().Warn("rubygems artifact provenance unavailable; refusing to serve",
				zap.String("artifact", fullName),
				zap.Error(err),
			)
		}
		if resolution.gem == "" || resolution.version == "" {
			c.JSON(http.StatusUnavailableForLegalReasons, gin.H{
				"code":    "QUARANTINED",
				"message": "rubygems artifact identity is unavailable; refusing to serve while the known-malicious dataset or the minimum-release-age gate covers RubyGems",
			})
			return
		}
		provenance := adapter.QuarantineProvenance{}
		if resolution.ok {
			provenance = adapter.QuarantineProvenance{
				SourceID:  rubygemsArtifactSourceID(resolution.gem, resolution.version, resolution.cksum),
				PublishAt: resolution.published,
			}
		}
		if blocked := adapter.QuarantineGateWithProvenance(c, "rubygems", resolution.gem, resolution.version, provenance); blocked {
			return
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
