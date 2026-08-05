package goproxy

import (
	"net/http"
	"strings"
	"time"

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
	return &Handler{proxy: adapter.NewTransparentProxy("go", cacheMgr, selector, database), cfg: cfg}
}

func (h *Handler) Type() string { return "go" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	// Use a single catch-all route since module paths contain slashes
	rg.GET("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")

	// Determine request type from path suffix
	switch {
	case strings.HasSuffix(path, "/@v/list"):
		module := strings.TrimSuffix(path, "/@v/list")
		h.proxyWithCache(c, ListCacheKey(module), path, h.cfg.TTLIndex)

	case strings.HasSuffix(path, "/@latest"):
		module := strings.TrimSuffix(path, "/@latest")
		h.proxyWithCache(c, LatestCacheKey(module), path, h.cfg.TTLIndex)

	case strings.HasSuffix(path, ".info"):
		h.proxyVersionFile(c, path, "info")

	case strings.HasSuffix(path, ".mod"):
		h.proxyVersionFile(c, path, "mod")

	case strings.HasSuffix(path, ".zip"):
		// Malware gate. Go deliberately has NO age quarantine
		// (threshold 0 — the checksum DB covers re-tag attacks), but
		// the known-malicious blocklist is checker step 0 and runs
		// before the threshold short-circuit, so the gate call still
		// belongs here (v0.8.0 review finding: Go data was synced but
		// never enforced).
		if module, version := packagekey.ParseGoZipPath(path); module != "" {
			if blocked := adapter.QuarantineGate(c, "go", module, version); blocked {
				return
			}
		}
		h.proxyVersionFile(c, path, "zip")

	default:
		c.Status(http.StatusNotFound)
	}
}

func (h *Handler) proxyVersionFile(c *gin.Context, path, ext string) {
	// path: github.com/gin-gonic/gin/@v/v1.9.1.info
	idx := strings.LastIndex(path, "/@v/")
	if idx < 0 {
		c.Status(http.StatusNotFound)
		return
	}
	module := path[:idx]
	versionFile := path[idx+len("/@v/"):] // "v1.9.1.info"
	version := strings.TrimSuffix(versionFile, "."+ext)

	var cacheKey string
	switch ext {
	case "info":
		cacheKey = InfoCacheKey(module, version)
	case "mod":
		cacheKey = ModCacheKey(module, version)
	case "zip":
		cacheKey = ZipCacheKey(module, version)
	}

	// Version-specific files are immutable -> long TTL
	h.proxyWithCache(c, cacheKey, path, h.cfg.TTLBlob)
}

func (h *Handler) proxyWithCache(c *gin.Context, cacheKey, upstreamPath string, ttl time.Duration) {
	h.proxy.Serve(c, adapter.TransparentPlan{Path: upstreamPath, CacheKey: cacheKey, TTL: ttl})
}
