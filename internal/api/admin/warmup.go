package admin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

type WarmupHandler struct {
	cacheMgr *cache.Manager
	pools    map[string]*upstream.Pool
	cfg      *config.Config
}

func NewWarmupHandler(cacheMgr *cache.Manager, pools map[string]*upstream.Pool, cfg *config.Config) *WarmupHandler {
	return &WarmupHandler{cacheMgr: cacheMgr, pools: pools, cfg: cfg}
}

// Warmup accepts a list of packages and pre-fetches their index into cache.
// POST /api/v1/admin/cache/warmup
// Body: { "ecosystem": "pypi", "packages": ["numpy", "requests", "torch"] }
func (h *WarmupHandler) Warmup(c *gin.Context) {
	var body struct {
		Ecosystem string   `json:"ecosystem" binding:"required"`
		Packages  []string `json:"packages" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	pool, ok := h.pools[body.Ecosystem]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": fmt.Sprintf("unknown ecosystem: %s", body.Ecosystem)})
		return
	}

	// Respond immediately, warmup runs in background
	total := len(body.Packages)
	zap.L().Info("cache warmup started",
		zap.String("ecosystem", body.Ecosystem),
		zap.Int("packages", total),
	)

	go h.doWarmup(body.Ecosystem, body.Packages, pool)

	c.JSON(http.StatusOK, gin.H{
		"message":  "warmup started",
		"packages": total,
	})
}

func (h *WarmupHandler) doWarmup(ecosystem string, packages []string, pool *upstream.Pool) {
	selector := upstream.NewPrioritySelector(pool)
	ttl := h.cfg.Cache.TTLIndex

	for _, pkg := range packages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" || strings.HasPrefix(pkg, "#") || strings.HasPrefix(pkg, "-") {
			continue
		}
		// Strip version specifiers: "numpy==1.24.0" → "numpy", "requests>=2.0" → "requests"
		for _, sep := range []string{"==", ">=", "<=", "!=", "~=", ">", "<", "["} {
			if idx := strings.Index(pkg, sep); idx > 0 {
				pkg = pkg[:idx]
			}
		}
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}

		cacheKey := fmt.Sprintf("%s/simple/%s/index.html", ecosystem, strings.ToLower(pkg))
		if ecosystem != "pypi" {
			// For non-pypi ecosystems, just use a generic key
			cacheKey = fmt.Sprintf("%s/%s", ecosystem, strings.ToLower(pkg))
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		_, err := h.cacheMgr.Get(ctx, cacheKey, ecosystem, ttl, func(fetchCtx context.Context) (io.ReadCloser, string, int64, string, error) {
			ups, err := selector.Select(fetchCtx)
			if err != nil {
				return nil, "", 0, "", err
			}
			var path string
			switch ecosystem {
			case "pypi":
				path = "/simple/" + pkg + "/"
			case "npm":
				path = "/" + pkg
			default:
				path = "/" + pkg
			}
			result, err := ups.Fetch(fetchCtx, path)
			if err != nil {
				return nil, "", 0, ups.Name, err
			}
			return result.Body, result.ContentType, result.Size, ups.Name, nil
		})
		cancel()

		if err != nil {
			zap.L().Warn("warmup fetch failed",
				zap.String("package", pkg),
				zap.String("ecosystem", ecosystem),
				zap.Error(err),
			)
		} else {
			zap.L().Debug("warmup cached",
				zap.String("package", pkg),
				zap.String("ecosystem", ecosystem),
			)
		}
	}

	zap.L().Info("cache warmup completed",
		zap.String("ecosystem", ecosystem),
		zap.Int("packages", len(packages)),
	)
}
