package docker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/api"
	"depsilo/internal/cache"
	"depsilo/internal/config"
)

const manifestAccept = "application/vnd.docker.distribution.manifest.v2+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.oci.image.index.v1+json, " +
	"*/*"

type Handler struct {
	cacheMgr *cache.Manager
	cacheCfg config.CacheConfig
	db       *gorm.DB
	resolver *Resolver
	auth     *AuthManager
}

func New(cacheMgr *cache.Manager, cacheCfg config.CacheConfig, database *gorm.DB, dockerCfg config.DockerConfig) *Handler {
	return &Handler{
		cacheMgr: cacheMgr,
		cacheCfg: cacheCfg,
		db:       database,
		resolver: NewResolver(dockerCfg),
		auth:     NewAuthManager(),
	}
}

func (h *Handler) Type() string { return "docker" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.HEAD("/*path", h.handleRequest)
	rg.GET("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")

	// GET /v2/ — version check
	if path == "" || path == "/" {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	start := time.Now()

	reg, imageName, endpoint := h.resolver.Resolve(path)
	if reg == nil || imageName == "" || endpoint == "" {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "invalid path"})
		adapter.LogAccess(h.db, "docker", c.Request.Method, "docker/unknown/"+path, false, "", time.Since(start), http.StatusNotFound, c.ClientIP(), 0)
		return
	}

	if strings.HasPrefix(endpoint, "manifests/") {
		h.handleManifest(c, reg, imageName, endpoint, start)
	} else if strings.HasPrefix(endpoint, "blobs/") {
		h.handleBlob(c, reg, imageName, endpoint, start)
	} else if strings.HasPrefix(endpoint, "tags/") {
		h.handleTagList(c, reg, imageName, start)
	} else {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "unsupported endpoint"})
		adapter.LogAccess(h.db, "docker", c.Request.Method, "docker/unknown/"+path, false, "", time.Since(start), http.StatusNotFound, c.ClientIP(), 0)
	}
}

func (h *Handler) handleManifest(c *gin.Context, reg *Registry, imageName, endpoint string, start time.Time) {
	reference := strings.TrimPrefix(endpoint, "manifests/")

	ttl := h.cacheCfg.TTLIndex
	if strings.HasPrefix(reference, "sha256:") {
		ttl = h.cacheCfg.TTLBlob
	}

	cacheKey := ManifestCacheKey(reg.Name, imageName, reference)
	scope := fmt.Sprintf("repository:%s:pull", imageName)

	if c.Request.Method == "HEAD" {
		h.handleHead(c, reg, imageName, endpoint, scope, cacheKey, start)
		return
	}

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "docker", ttl, func(ctx context.Context) (io.ReadCloser, string, int64, error) {
		return h.fetchFromUpstream(ctx, reg, imageName, "manifests/"+reference, scope, true)
	})
	if err != nil {
		zap.L().Error("docker manifest fetch failed", zap.String("image", imageName), zap.String("ref", reference), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
		adapter.LogAccess(h.db, "docker", c.Request.Method, cacheKey, false, reg.Name, time.Since(start), http.StatusBadGateway, c.ClientIP(), 0)
		return
	}
	defer result.Reader.Close()

	h.streamResponse(c, result, cacheKey, start)
}

func (h *Handler) handleBlob(c *gin.Context, reg *Registry, imageName, endpoint string, start time.Time) {
	digest := strings.TrimPrefix(endpoint, "blobs/")
	cacheKey := BlobCacheKey(reg.Name, digest)
	scope := fmt.Sprintf("repository:%s:pull", imageName)

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "docker", h.cacheCfg.TTLBlob, func(ctx context.Context) (io.ReadCloser, string, int64, error) {
		return h.fetchFromUpstream(ctx, reg, imageName, "blobs/"+digest, scope, false)
	})
	if err != nil {
		zap.L().Error("docker blob fetch failed", zap.String("digest", digest), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
		adapter.LogAccess(h.db, "docker", c.Request.Method, cacheKey, false, reg.Name, time.Since(start), http.StatusBadGateway, c.ClientIP(), 0)
		return
	}
	defer result.Reader.Close()

	c.Header("Docker-Content-Digest", digest)
	h.streamResponse(c, result, cacheKey, start)
}

func (h *Handler) handleTagList(c *gin.Context, reg *Registry, imageName string, start time.Time) {
	cacheKey := TagListCacheKey(reg.Name, imageName)
	scope := fmt.Sprintf("repository:%s:pull", imageName)

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "docker", h.cacheCfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, error) {
		return h.fetchFromUpstream(ctx, reg, imageName, "tags/list", scope, false)
	})
	if err != nil {
		zap.L().Error("docker tags list failed", zap.String("image", imageName), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
		adapter.LogAccess(h.db, "docker", c.Request.Method, cacheKey, false, reg.Name, time.Since(start), http.StatusBadGateway, c.ClientIP(), 0)
		return
	}
	defer result.Reader.Close()

	h.streamResponse(c, result, cacheKey, start)
}

func (h *Handler) handleHead(c *gin.Context, reg *Registry, imageName, endpoint, scope, cacheKey string, start time.Time) {
	token, err := h.auth.GetToken(reg.Client, reg.URL, reg.Name, reg.Username, reg.Password, scope)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": "AUTH_FAILED", "message": err.Error()})
		return
	}

	upstreamURL := fmt.Sprintf("%s/v2/%s/%s", reg.URL, imageName, endpoint)
	req, err := http.NewRequestWithContext(c.Request.Context(), "HEAD", upstreamURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "REQUEST_ERROR", "message": err.Error()})
		return
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", manifestAccept)

	resp, err := reg.Client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		c.Status(resp.StatusCode)
		return
	}

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	}
	if digest := resp.Header.Get("Docker-Content-Digest"); digest != "" {
		c.Header("Docker-Content-Digest", digest)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		c.Header("Content-Length", cl)
	}
	c.Status(http.StatusOK)

	api.M.RequestsTotal.WithLabelValues("docker", "false").Inc()
	api.M.RequestDuration.WithLabelValues("docker").Observe(time.Since(start).Seconds())
	adapter.LogAccess(h.db, "docker", "HEAD", cacheKey, false, reg.Name, time.Since(start), http.StatusOK, c.ClientIP(), 0)
}

func (h *Handler) fetchFromUpstream(ctx context.Context, reg *Registry, imageName, endpoint, scope string, isManifest bool) (io.ReadCloser, string, int64, error) {
	token, err := h.auth.GetToken(reg.Client, reg.URL, reg.Name, reg.Username, reg.Password, scope)
	if err != nil {
		return nil, "", 0, err
	}

	upstreamURL := fmt.Sprintf("%s/v2/%s/%s", reg.URL, imageName, endpoint)
	req, err := http.NewRequestWithContext(ctx, "GET", upstreamURL, nil)
	if err != nil {
		return nil, "", 0, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if isManifest {
		req.Header.Set("Accept", manifestAccept)
	}

	zap.L().Info("fetching from docker upstream",
		zap.String("registry", reg.Name),
		zap.String("image", imageName),
		zap.String("endpoint", endpoint),
	)

	resp, err := reg.Client.Do(req)
	if err != nil {
		api.M.UpstreamRequestsTotal.WithLabelValues(reg.Name, "false").Inc()
		return nil, "", 0, err
	}

	if resp.StatusCode != http.StatusOK {
		api.M.UpstreamRequestsTotal.WithLabelValues(reg.Name, "false").Inc()
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, "", 0, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, string(body))
	}

	api.M.UpstreamRequestsTotal.WithLabelValues(reg.Name, "true").Inc()
	return resp.Body, resp.Header.Get("Content-Type"), resp.ContentLength, nil
}

func (h *Handler) streamResponse(c *gin.Context, result *cache.GetResult, cacheKey string, start time.Time) {
	ct := result.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	c.Header("Content-Type", ct)
	if result.Size > 0 {
		c.Header("Content-Length", fmt.Sprintf("%d", result.Size))
	}
	c.Status(http.StatusOK)
	written, _ := io.Copy(c.Writer, result.Reader)

	// Prometheus metrics
	hitStr := "false"
	if result.Hit {
		hitStr = "true"
	}
	api.M.RequestsTotal.WithLabelValues("docker", hitStr).Inc()
	api.M.RequestDuration.WithLabelValues("docker").Observe(time.Since(start).Seconds())

	adapter.LogAccess(h.db, "docker", c.Request.Method, cacheKey, result.Hit, "", time.Since(start), http.StatusOK, c.ClientIP(), written)
}
