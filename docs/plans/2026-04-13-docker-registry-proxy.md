# Docker Registry Proxy — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Docker Registry V2 pull-through cache with multi-registry support (Docker Hub, ghcr.io, quay.io, private registries) via `/v2/` endpoint.

**Architecture:** New `docker` adapter with custom auth module (bearer token fetching/caching), registry resolver (domain→config lookup), and passthrough caching. Different from other adapters: uses per-registry credentials instead of priority-based upstream selection, and mounts at `/v2/` instead of `/docker/`.

**Tech Stack:** Go/Gin/GORM (backend), React/TypeScript (frontend), Docker Registry HTTP API V2

---

### Task 1: Config — Docker Registry Types

**Files:**
- Modify: `internal/config/config.go`
- Modify: `config.example.toml`

- [ ] **Step 1: Add Docker config types to `internal/config/config.go`**

Add after the `Helm` field in Config struct (line 22):

```go
	Docker   DockerConfig   `mapstructure:"docker"`
```

Add new types after `UpstreamConfig` (after line 73):

```go
type DockerConfig struct {
	DefaultRegistry string           `mapstructure:"default_registry"`
	Registries      []RegistryConfig `mapstructure:"registries"`
}

type RegistryConfig struct {
	Name     string `mapstructure:"name"`
	URL      string `mapstructure:"url"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Proxy    string `mapstructure:"proxy"`
}
```

- [ ] **Step 2: Add Docker section to `config.example.toml`**

Append at the end of the file:

```toml

[docker]
default_registry = "docker.io"

[[docker.registries]]
name     = "dockerhub"
url      = "https://registry-1.docker.io"
# username = ""
# password = ""
# proxy    = "http://127.0.0.1:7890"

# [[docker.registries]]
# name     = "ghcr"
# url      = "https://ghcr.io"
# username = "github-user"
# password = "ghp_xxx"
```

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/server`

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go config.example.toml
git commit -m "feat(docker): add Docker Registry config types"
```

---

### Task 2: Backend — Auth Token Manager

**Files:**
- Create: `internal/adapter/docker/auth.go`

- [ ] **Step 1: Create `internal/adapter/docker/auth.go`**

```go
package docker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type tokenEntry struct {
	Token     string
	ExpiresAt time.Time
}

type AuthManager struct {
	mu     sync.RWMutex
	tokens map[string]*tokenEntry // key: "registryName:scope"
}

func NewAuthManager() *AuthManager {
	return &AuthManager{tokens: make(map[string]*tokenEntry)}
}

// GetToken returns a valid bearer token for the given registry and scope.
// It caches tokens and refreshes them when expired.
func (a *AuthManager) GetToken(client *http.Client, registryURL, registryName, username, password, scope string) (string, error) {
	cacheKey := registryName + ":" + scope

	a.mu.RLock()
	if entry, ok := a.tokens[cacheKey]; ok && time.Now().Before(entry.ExpiresAt) {
		a.mu.RUnlock()
		return entry.Token, nil
	}
	a.mu.RUnlock()

	// Discover token endpoint from /v2/ challenge
	realm, service, err := a.discoverAuth(client, registryURL)
	if err != nil {
		return "", fmt.Errorf("auth discovery failed: %w", err)
	}
	if realm == "" {
		// No auth required (e.g., insecure registry)
		return "", nil
	}

	token, expiresIn, err := a.fetchToken(client, realm, service, scope, username, password)
	if err != nil {
		return "", fmt.Errorf("token fetch failed: %w", err)
	}

	a.mu.Lock()
	a.tokens[cacheKey] = &tokenEntry{
		Token:     token,
		ExpiresAt: time.Now().Add(time.Duration(expiresIn-30) * time.Second), // refresh 30s early
	}
	a.mu.Unlock()

	return token, nil
}

// discoverAuth sends GET /v2/ and parses the WWW-Authenticate header.
func (a *AuthManager) discoverAuth(client *http.Client, registryURL string) (realm, service string, err error) {
	resp, err := client.Get(registryURL + "/v2/")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusOK {
		return "", "", nil // no auth needed
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return "", "", fmt.Errorf("unexpected status %d from /v2/", resp.StatusCode)
	}

	challenge := resp.Header.Get("WWW-Authenticate")
	if challenge == "" {
		return "", "", fmt.Errorf("no WWW-Authenticate header")
	}

	realm = parseAuthParam(challenge, "realm")
	service = parseAuthParam(challenge, "service")
	return realm, service, nil
}

// fetchToken requests a bearer token from the auth endpoint.
func (a *AuthManager) fetchToken(client *http.Client, realm, service, scope, username, password string) (string, int, error) {
	url := fmt.Sprintf("%s?service=%s&scope=%s", realm, service, scope)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", 0, err
	}
	if username != "" {
		req.SetBasicAuth(username, password)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", 0, err
	}

	token := tokenResp.Token
	if token == "" {
		token = tokenResp.AccessToken
	}
	expiresIn := tokenResp.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 300 // default 5 min
	}

	zap.L().Debug("docker auth token acquired", zap.String("service", service), zap.String("scope", scope), zap.Int("expires_in", expiresIn))
	return token, expiresIn, nil
}

// parseAuthParam extracts a named parameter from a WWW-Authenticate header.
// Example: `Bearer realm="https://auth.docker.io/token",service="registry.docker.io"`
func parseAuthParam(header, name string) string {
	search := name + "=\""
	idx := strings.Index(header, search)
	if idx < 0 {
		return ""
	}
	start := idx + len(search)
	end := strings.Index(header[start:], "\"")
	if end < 0 {
		return ""
	}
	return header[start : start+end]
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./cmd/server`

- [ ] **Step 3: Commit**

```bash
git add internal/adapter/docker/auth.go
git commit -m "feat(docker): add bearer token auth manager"
```

---

### Task 3: Backend — Registry Resolver

**Files:**
- Create: `internal/adapter/docker/resolver.go`

- [ ] **Step 1: Create `internal/adapter/docker/resolver.go`**

```go
package docker

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"strings"
	"time"

	"depsilo/internal/config"
)

// Registry holds runtime state for a configured registry.
type Registry struct {
	Name     string
	URL      string // e.g. "https://registry-1.docker.io"
	Username string
	Password string
	Client   *http.Client
}

// Resolver maps registry domains to Registry configs.
type Resolver struct {
	registries map[string]*Registry // name → Registry
	domainMap  map[string]string    // domain → registry name (e.g. "docker.io" → "dockerhub")
	defaultReg string               // default registry name
}

func NewResolver(cfg config.DockerConfig) *Resolver {
	r := &Resolver{
		registries: make(map[string]*Registry),
		domainMap:  make(map[string]string),
		defaultReg: "",
	}

	for _, rc := range cfg.Registries {
		client := &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
				MaxIdleConnsPerHost: 10,
			},
		}

		// Set up proxy if configured
		if rc.Proxy != "" {
			proxyURL, err := url.Parse(rc.Proxy)
			if err == nil {
				client.Transport = &http.Transport{
					Proxy:               http.ProxyURL(proxyURL),
					TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
					MaxIdleConnsPerHost: 10,
				}
			}
		}

		reg := &Registry{
			Name:     rc.Name,
			URL:      strings.TrimRight(rc.URL, "/"),
			Username: rc.Username,
			Password: rc.Password,
			Client:   client,
		}
		r.registries[rc.Name] = reg

		// Build domain map from URL
		if u, err := url.Parse(rc.URL); err == nil {
			domain := u.Host
			r.domainMap[domain] = rc.Name

			// Also map without port for standard ports
			if host := u.Hostname(); host != domain {
				r.domainMap[host] = rc.Name
			}
		}
	}

	// Resolve default registry
	if cfg.DefaultRegistry != "" {
		// Try exact name match first
		for _, reg := range r.registries {
			if reg.Name == cfg.DefaultRegistry {
				r.defaultReg = reg.Name
				break
			}
		}
		// Try domain match
		if r.defaultReg == "" {
			if name, ok := r.domainMap[cfg.DefaultRegistry]; ok {
				r.defaultReg = name
			}
		}
		// Fallback: use first registry
		if r.defaultReg == "" && len(cfg.Registries) > 0 {
			r.defaultReg = cfg.Registries[0].Name
		}
	} else if len(cfg.Registries) > 0 {
		r.defaultReg = cfg.Registries[0].Name
	}

	// Add common domain aliases for Docker Hub
	if name, ok := r.domainMap["registry-1.docker.io"]; ok {
		r.domainMap["docker.io"] = name
		r.domainMap["index.docker.io"] = name
	}

	return r
}

// Resolve parses a request path and returns the target registry, image name, and endpoint.
// Path format: /{maybe-domain}/{image-parts...}/{endpoint}/{reference}
// Returns: registry, imageName, endpoint (e.g. "manifests/latest" or "blobs/sha256:abc")
func (r *Resolver) Resolve(path string) (reg *Registry, imageName string, endpoint string) {
	// path is like "library/nginx/manifests/latest"
	// or "ghcr.io/user/repo/manifests/v1.0"
	// or "myregistry.com:5000/team/app/blobs/sha256:abc"

	parts := strings.SplitN(path, "/", -1)
	if len(parts) < 3 {
		return nil, "", ""
	}

	// Find endpoint separator (manifests, blobs, tags)
	endpointIdx := -1
	for i, p := range parts {
		if p == "manifests" || p == "blobs" || p == "tags" {
			endpointIdx = i
			break
		}
	}
	if endpointIdx < 1 {
		return nil, "", ""
	}

	firstPart := parts[0]
	endpoint = strings.Join(parts[endpointIdx:], "/")

	// Check if first part looks like a domain (contains . or :)
	if strings.ContainsAny(firstPart, ".:") {
		// Domain-prefixed path
		regName, ok := r.domainMap[firstPart]
		if ok {
			reg = r.registries[regName]
			imageName = strings.Join(parts[1:endpointIdx], "/")
			return reg, imageName, endpoint
		}
		// Unknown domain — treat as default registry, include domain as part of image name
	}

	// Default registry
	if r.defaultReg != "" {
		reg = r.registries[r.defaultReg]
	}
	imageName = strings.Join(parts[0:endpointIdx], "/")
	return reg, imageName, endpoint
}

// Default returns the default registry, if any.
func (r *Resolver) Default() *Registry {
	if r.defaultReg == "" {
		return nil
	}
	return r.registries[r.defaultReg]
}

// HasRegistries returns true if at least one registry is configured.
func (r *Resolver) HasRegistries() bool {
	return len(r.registries) > 0
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./cmd/server`

- [ ] **Step 3: Commit**

```bash
git add internal/adapter/docker/resolver.go
git commit -m "feat(docker): add registry resolver with domain routing"
```

---

### Task 4: Backend — Cache Key & Handler

**Files:**
- Create: `internal/adapter/docker/keyer.go`
- Create: `internal/adapter/docker/handler.go`

- [ ] **Step 1: Create `internal/adapter/docker/keyer.go`**

```go
package docker

import "fmt"

func ManifestCacheKey(registryName, imageName, reference string) string {
	return fmt.Sprintf("docker/%s/manifests/%s/%s", registryName, imageName, reference)
}

func BlobCacheKey(registryName, digest string) string {
	return fmt.Sprintf("docker/%s/blobs/%s", registryName, digest)
}

func TagListCacheKey(registryName, imageName string) string {
	return fmt.Sprintf("docker/%s/tags/%s/list", registryName, imageName)
}
```

- [ ] **Step 2: Create `internal/adapter/docker/handler.go`**

```go
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
	"depsilo/internal/cache"
	"depsilo/internal/config"
)

// Manifest media types to request from upstream
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
	rg.GET("/", h.handleVersionCheck)
	rg.HEAD("/*path", h.handleRequest)
	rg.GET("/*path", h.handleRequest)
}

func (h *Handler) handleVersionCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")
	if path == "" {
		c.Status(http.StatusNotFound)
		return
	}

	start := time.Now()

	reg, imageName, endpoint := h.resolver.Resolve(path)
	if reg == nil || imageName == "" || endpoint == "" {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "invalid path"})
		return
	}

	// Determine what we're serving
	if strings.HasPrefix(endpoint, "manifests/") {
		h.handleManifest(c, reg, imageName, endpoint, start)
	} else if strings.HasPrefix(endpoint, "blobs/") {
		h.handleBlob(c, reg, imageName, endpoint, start)
	} else if strings.HasPrefix(endpoint, "tags/") {
		h.handleTagList(c, reg, imageName, start)
	} else {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "unsupported endpoint"})
	}
}

func (h *Handler) handleManifest(c *gin.Context, reg *Registry, imageName, endpoint string, start time.Time) {
	reference := strings.TrimPrefix(endpoint, "manifests/")

	// Digest references are immutable → long TTL
	// Tag references are mutable → short TTL
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
		return
	}
	defer result.Reader.Close()

	h.streamResponse(c, result, cacheKey, start)
}

func (h *Handler) handleHead(c *gin.Context, reg *Registry, imageName, endpoint, scope, cacheKey string, start time.Time) {
	// For HEAD: try cache first, if miss forward HEAD upstream (don't cache)
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
		return nil, "", 0, err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, "", 0, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, string(body))
	}

	return resp.Body, resp.Header.Get("Content-Type"), resp.ContentLength, nil
}

func (h *Handler) streamResponse(c *gin.Context, result *cache.Result, cacheKey string, start time.Time) {
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

	adapter.LogAccess(h.db, "docker", c.Request.Method, cacheKey, result.Hit, "", time.Since(start), http.StatusOK, c.ClientIP(), written)
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/server`

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/docker/
git commit -m "feat(docker): add handler, keyer, and request routing"
```

---

### Task 5: Backend — Wire into main.go

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `internal/adapter/accesslog.go`

- [ ] **Step 1: Add Docker import and registration to `cmd/server/main.go`**

Add import (after line 22, the helm import):

```go
	dockeradapter "depsilo/internal/adapter/docker"
```

Add adapter registration after line 292 (after Helm registration block):

```go
	// Register Docker Registry adapter
	if h.resolver := cfg.Docker; len(h.Docker.Registries) > 0 {
		dockerHandler := dockeradapter.New(cacheMgr, cfg.Cache, database, cfg.Docker)
		dockerGroup := r.Group("/v2")
		dockerHandler.Register(dockerGroup)
		zap.L().Info("docker registry proxy enabled",
			zap.Int("registries", len(cfg.Docker.Registries)),
			zap.String("default", cfg.Docker.DefaultRegistry),
		)
	}
```

Actually, let me write the exact code that fits the pattern. Add after line 292 (`helmHandler.Register(helmGroup)`):

```go

	// Register Docker Registry adapter (only if registries configured)
	if len(cfg.Docker.Registries) > 0 {
		dockerHandler := dockeradapter.New(cacheMgr, cfg.Cache, database, cfg.Docker)
		dockerGroup := r.Group("/v2")
		dockerHandler.Register(dockerGroup)
		zap.L().Info("docker registry proxy enabled",
			zap.Int("registries", len(cfg.Docker.Registries)),
			zap.String("default", cfg.Docker.DefaultRegistry),
		)
	}
```

- [ ] **Step 2: Add docker case to `extractPackageName` in `internal/adapter/accesslog.go`**

Add a new case in the switch statement (after the "helm" case):

```go
	case "docker":
		// key: docker/{registry}/manifests/{image}/{ref} or docker/{registry}/blobs/{digest}
		if strings.Contains(key, "/manifests/") {
			parts := strings.SplitN(key, "/manifests/", 2)
			if len(parts) == 2 {
				// parts[0] = "docker/registryName", parts[1] = "imageName/reference"
				image := parts[1]
				// Remove the reference (last segment)
				if idx := strings.LastIndex(image, "/"); idx > 0 {
					return image[:idx]
				}
				return image
			}
		}
		if strings.Contains(key, "/blobs/") {
			// Blobs don't have a meaningful package name
			return ""
		}
```

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/server`

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go internal/adapter/accesslog.go
git commit -m "feat(docker): wire adapter into main and add package name extraction"
```

---

### Task 6: Frontend — EcosystemIcon & ECOSYSTEMS Lists

**Files:**
- Modify: `web/src/components/EcosystemIcon.tsx`
- Modify: `web/src/admin/pages/Upstreams.tsx`
- Modify: `web/src/admin/pages/CacheManage.tsx`
- Modify: `web/src/admin/pages/AccessLogs.tsx`
- Modify: `web/src/admin/pages/AuditLogs.tsx`

- [ ] **Step 1: Add Docker icon to `web/src/components/EcosystemIcon.tsx`**

Add `siDocker` to the import (line 1):

```typescript
import {
  siPython,
  siUbuntu,
  siNpm,
  siGo,
  siRust,
  siApachemaven,
  siRuby,
  siPhp,
  siDotnet,
  siAnaconda,
  siR,
  siHelm,
  siDocker,
} from 'simple-icons'
```

Add `'docker'` to the `EcosystemType` union (after `'helm'` on line 28):

```typescript
  | 'docker'
```

Add to `iconMap` (after the helm line, line 52):

```typescript
  docker: siDocker,
```

- [ ] **Step 2: Add 'docker' to all ECOSYSTEMS arrays**

In each of these files, add `'docker'` to the end of the ECOSYSTEMS array:

**`web/src/admin/pages/Upstreams.tsx` line 12:**
```typescript
const ECOSYSTEMS = ['pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'helm', 'docker'] as const
```

**`web/src/admin/pages/CacheManage.tsx` line 27:**
```typescript
const ECOSYSTEMS = ['pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'helm', 'docker']
```

**`web/src/admin/pages/AccessLogs.tsx` line 17:**
```typescript
const ECOSYSTEMS = ['pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'helm', 'docker']
```

**`web/src/admin/pages/AuditLogs.tsx` line 10:**
```typescript
const ECOSYSTEMS = ['pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'helm', 'docker']
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/EcosystemIcon.tsx web/src/admin/pages/Upstreams.tsx web/src/admin/pages/CacheManage.tsx web/src/admin/pages/AccessLogs.tsx web/src/admin/pages/AuditLogs.tsx
git commit -m "feat(docker): add Docker icon and ecosystem filter support"
```

---

### Task 7: Frontend — QuickStart Docker Tab & i18n

**Files:**
- Modify: `web/src/portal/pages/QuickStart.tsx` (Tab type, tab list, tab content)
- Modify: `web/src/i18n/en.ts`
- Modify: `web/src/i18n/zh.ts`

- [ ] **Step 1: Add i18n keys to `web/src/i18n/en.ts`**

Add inside the `quickstart` section (after the `helmUseDesc` line):

```typescript
      dockerLabel: 'Docker',
      dockerDesc: 'Container images (Docker Hub, ghcr.io, etc.)',
      dockerMirror: 'Mirror Mode (daemon.json)',
      dockerMirrorDesc: 'Add to /etc/docker/daemon.json and restart Docker:',
      dockerDirect: 'Direct Pull',
      dockerDirectDesc: 'Pull images through the proxy:',
      dockerRestart: 'Restart Docker',
      dockerRestartDesc: 'Apply the mirror configuration:',
      dockerOther: 'Other Registries',
      dockerOtherDesc: 'Pull from non-default registries by prefixing the domain:',
```

- [ ] **Step 2: Add i18n keys to `web/src/i18n/zh.ts`**

Add matching keys in the `quickstart` section:

```typescript
      dockerLabel: 'Docker',
      dockerDesc: '容器镜像（Docker Hub, ghcr.io 等）',
      dockerMirror: '镜像模式 (daemon.json)',
      dockerMirrorDesc: '添加到 /etc/docker/daemon.json 并重启 Docker：',
      dockerDirect: '直接拉取',
      dockerDirectDesc: '通过代理拉取镜像：',
      dockerRestart: '重启 Docker',
      dockerRestartDesc: '应用镜像配置：',
      dockerOther: '其他仓库',
      dockerOtherDesc: '通过域名前缀拉取非默认仓库的镜像：',
```

- [ ] **Step 3: Add Docker tab to `web/src/portal/pages/QuickStart.tsx`**

Add `'docker'` to the Tab type (line 9):

```typescript
type Tab = 'pip' | 'apt' | 'npm' | 'go' | 'cargo' | 'maven' | 'rubygems' | 'composer' | 'nuget' | 'conda' | 'cran' | 'helm' | 'docker'
```

Add Docker to the Quick Reference table in `genRulesBlock` (line 34, after the Helm row):

```typescript
    `| Docker    | Mirror: add \`${u}\` to daemon.json registry-mirrors |`,
```

Find the tabs array in the component (search for `helmLabel`) and add a Docker tab entry after the Helm entry:

```typescript
    { id: 'docker' as Tab, label: t('quickstart.dockerLabel'), desc: t('quickstart.dockerDesc') },
```

Find the tab content rendering section (search for the helm content block) and add Docker content after it:

```tsx
              {activeTab === 'docker' && (
                <>
                  <h4 className="text-[13px] font-[500] mt-0 mb-2" style={{ color: 'var(--heading)' }}>{t('quickstart.dockerMirror')}</h4>
                  <p className="text-[13px] mb-2" style={{ color: 'var(--body)' }}>{t('quickstart.dockerMirrorDesc')}</p>
                  <CodeBlockV2 filename="/etc/docker/daemon.json" language="json" code={`{\n  "registry-mirrors": ["${u}"]\n}`} />
                  <h4 className="text-[13px] font-[500] mt-5 mb-2" style={{ color: 'var(--heading)' }}>{t('quickstart.dockerRestart')}</h4>
                  <p className="text-[13px] mb-2" style={{ color: 'var(--body)' }}>{t('quickstart.dockerRestartDesc')}</p>
                  <CodeBlockV2 language="bash" code="sudo systemctl restart docker" />
                  <h4 className="text-[13px] font-[500] mt-5 mb-2" style={{ color: 'var(--heading)' }}>{t('quickstart.dockerDirect')}</h4>
                  <p className="text-[13px] mb-2" style={{ color: 'var(--body)' }}>{t('quickstart.dockerDirectDesc')}</p>
                  <CodeBlockV2 language="bash" code={`docker pull ${h}:${location.port || '23333'}/nginx:latest`} />
                  <h4 className="text-[13px] font-[500] mt-5 mb-2" style={{ color: 'var(--heading)' }}>{t('quickstart.dockerOther')}</h4>
                  <p className="text-[13px] mb-2" style={{ color: 'var(--body)' }}>{t('quickstart.dockerOtherDesc')}</p>
                  <CodeBlockV2 language="bash" code={`docker pull ${h}:${location.port || '23333'}/ghcr.io/owner/repo:tag`} />
                </>
              )}
```

- [ ] **Step 4: Commit**

```bash
git add web/src/portal/pages/QuickStart.tsx web/src/i18n/en.ts web/src/i18n/zh.ts
git commit -m "feat(docker): add QuickStart Docker tab and i18n keys"
```

---

### Task 8: Build Verification

**Files:** none (verification only)

- [ ] **Step 1: Build backend**

Run: `go build ./cmd/server`
Expected: clean build

- [ ] **Step 2: Run tests**

Run: `go test ./...`
Expected: all tests pass

- [ ] **Step 3: Build frontend**

Run: `make frontend`
Expected: clean build
