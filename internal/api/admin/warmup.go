package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	npmadapter "depsilo/internal/adapter/npm"
	pypiadapter "depsilo/internal/adapter/pypi"
	"depsilo/internal/asyncruntime"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

const (
	maxWarmupPackages     = 100
	maxWarmupPackageBytes = 256
	maxWarmupDuration     = 10 * time.Minute
)

type WarmupHandler struct {
	cacheMgr *cache.Manager
	pools    map[string]*upstream.Pool
	cfg      *config.Config
	tasks    asyncruntime.Submitter
	running  atomic.Bool
}

// NewWarmupHandler binds warmup work to the server's async runtime.
func NewWarmupHandler(tasks asyncruntime.Submitter, cacheMgr *cache.Manager, pools map[string]*upstream.Pool, cfg *config.Config) *WarmupHandler {
	return &WarmupHandler{
		cacheMgr: cacheMgr,
		pools:    pools,
		cfg:      cfg,
		tasks:    tasks,
	}
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
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "request body must contain an ecosystem and package list"})
		return
	}
	body.Ecosystem = strings.ToLower(strings.TrimSpace(body.Ecosystem))
	packages, err := normalizeWarmupPackages(body.Ecosystem, body.Packages)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	pool, ok := h.pools[body.Ecosystem]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "the selected ecosystem is not active"})
		return
	}
	if !h.running.CompareAndSwap(false, true) {
		c.JSON(http.StatusConflict, gin.H{"code": "WARMUP_RUNNING", "message": "a cache warmup is already in progress"})
		return
	}
	release := func() { h.running.Store(false) }

	total := len(packages)
	if h.tasks == nil {
		release()
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SERVER_SHUTTING_DOWN", "message": "cache warmup is unavailable"})
		return
	}
	if err := h.tasks.Submit(func(ctx context.Context) {
		defer release()
		h.doWarmup(ctx, body.Ecosystem, packages, pool)
	}); err != nil {
		release()
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SERVER_SHUTTING_DOWN", "message": "cache warmup is unavailable"})
		return
	}

	zap.L().Info("cache warmup started",
		zap.String("ecosystem", body.Ecosystem),
		zap.Int("packages", total),
	)

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "warmup started",
		"packages": total,
	})
}

func (h *WarmupHandler) doWarmup(parent context.Context, ecosystem string, packages []string, pool *upstream.Pool) {
	normalized, err := normalizeWarmupPackages(ecosystem, packages)
	if err != nil {
		zap.L().Warn("cache warmup rejected", zap.String("ecosystem", ecosystem), zap.Error(err))
		return
	}
	packages = normalized
	warmupContext, cancelWarmup := context.WithTimeout(parent, maxWarmupDuration)
	defer cancelWarmup()
	selector := upstream.NewPassiveRecoverySelector(pool)
	ttl := h.cfg.Cache.TTLIndex

	for _, pkg := range packages {
		if warmupContext.Err() != nil {
			break
		}
		cacheKey, upstreamPath, err := warmupTarget(ecosystem, pkg)
		if err != nil {
			zap.L().Warn("warmup target invalid", zap.String("ecosystem", ecosystem), zap.Error(err))
			continue
		}

		ctx, cancel := context.WithTimeout(warmupContext, 2*time.Minute)
		err = h.cacheMgr.Prefetch(ctx, cacheKey, ecosystem, ttl, func(fetchCtx context.Context) (io.ReadCloser, string, int64, string, error) {
			ups, err := selector.Select(fetchCtx)
			if err != nil {
				return nil, "", 0, "", err
			}
			result, err := ups.Fetch(fetchCtx, upstreamPath)
			if err != nil {
				return nil, "", 0, ups.Name, err
			}
			body, readErr := io.ReadAll(result.Body)
			closeErr := result.Body.Close()
			if err := errors.Join(readErr, closeErr); err != nil {
				return nil, "", 0, ups.Name, err
			}
			rewritten, err := rewriteWarmupIndex(ecosystem, body)
			if err != nil {
				return nil, "", 0, ups.Name, err
			}
			contentType := result.ContentType
			if contentType == "" {
				if ecosystem == "npm" {
					contentType = "application/json"
				} else {
					contentType = "text/html"
				}
			}
			return io.NopCloser(strings.NewReader(string(rewritten))), contentType, int64(len(rewritten)), ups.Name, nil
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

func normalizeWarmupPackages(ecosystem string, raw []string) ([]string, error) {
	if ecosystem != "pypi" && ecosystem != "npm" {
		return nil, errors.New("cache warmup currently supports only PyPI and npm")
	}
	if len(raw) == 0 {
		return nil, errors.New("at least one package is required")
	}
	if len(raw) > maxWarmupPackages {
		return nil, fmt.Errorf("package list exceeds the %d item limit", maxWarmupPackages)
	}

	packages := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		name := strings.TrimSpace(item)
		if name == "" || strings.HasPrefix(name, "#") || strings.HasPrefix(name, "-") {
			continue
		}
		if ecosystem == "pypi" {
			name = stripPyPISpecifier(name)
		} else {
			name = stripNPMSpecifier(name)
		}
		name = strings.TrimSpace(name)
		if err := validateWarmupPackageName(ecosystem, name); err != nil {
			return nil, err
		}
		canonical := strings.ToLower(name)
		if _, duplicate := seen[canonical]; duplicate {
			continue
		}
		seen[canonical] = struct{}{}
		packages = append(packages, name)
	}
	if len(packages) == 0 {
		return nil, errors.New("at least one valid package is required")
	}
	return packages, nil
}

func stripPyPISpecifier(name string) string {
	for _, separator := range []string{"==", ">=", "<=", "!=", "~=", ">", "<", "["} {
		if index := strings.Index(name, separator); index > 0 {
			name = name[:index]
		}
	}
	return name
}

func stripNPMSpecifier(name string) string {
	if strings.HasPrefix(name, "@") {
		slash := strings.Index(name, "/")
		if slash > 1 {
			if version := strings.LastIndex(name, "@"); version > slash {
				return name[:version]
			}
		}
		return name
	}
	if version := strings.Index(name, "@"); version > 0 {
		return name[:version]
	}
	return name
}

func validateWarmupPackageName(ecosystem, name string) error {
	if name == "" || len(name) > maxWarmupPackageBytes || strings.ContainsFunc(name, unicode.IsSpace) || strings.ContainsFunc(name, unicode.IsControl) {
		return fmt.Errorf("package names must be non-empty and at most %d bytes without whitespace", maxWarmupPackageBytes)
	}
	if strings.ContainsAny(name, "\\?#") {
		return errors.New("package name contains unsupported path characters")
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			continue
		}
		if strings.ContainsRune("-._~", character) || ecosystem == "npm" && (character == '@' || character == '/') {
			continue
		}
		return errors.New("package name contains unsupported characters")
	}
	if ecosystem == "pypi" {
		if strings.Contains(name, "/") {
			return errors.New("PyPI package names must not contain a slash")
		}
		return nil
	}
	if strings.HasPrefix(name, "@") {
		parts := strings.Split(name, "/")
		if len(parts) != 2 || len(parts[0]) < 2 || parts[1] == "" {
			return errors.New("scoped npm packages must use @scope/name")
		}
		return nil
	}
	if strings.Contains(name, "/") {
		return errors.New("npm package names must not contain a slash unless scoped")
	}
	return nil
}

func warmupTarget(ecosystem, pkg string) (cacheKey, upstreamPath string, err error) {
	switch ecosystem {
	case "pypi":
		return pypiadapter.IndexCacheKey("pypi", pkg), "/simple/" + pkg + "/", nil
	case "npm":
		if strings.HasPrefix(pkg, "@") {
			parts := strings.SplitN(strings.TrimPrefix(pkg, "@"), "/", 2)
			if len(parts) != 2 {
				return "", "", errors.New("invalid scoped npm package")
			}
			return npmadapter.ScopedMetadataCacheKey(parts[0], parts[1]), "/" + pkg, nil
		}
		return npmadapter.MetadataCacheKey(pkg), "/" + pkg, nil
	default:
		return "", "", errors.New("unsupported warmup ecosystem")
	}
}

func rewriteWarmupIndex(ecosystem string, body []byte) ([]byte, error) {
	switch ecosystem {
	case "pypi":
		return []byte(pypiadapter.RewriteURLs(string(body), "", "/pypi")), nil
	case "npm":
		return npmadapter.RewriteTarballURLs(body, "")
	default:
		return nil, errors.New("unsupported warmup ecosystem")
	}
}
