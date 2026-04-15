# npm Adapter Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add npm registry proxy cache support to Depsilo, following the same Adapter pattern as PyPI and APT.

**Architecture:** New `internal/adapter/npm/` package with handler (routes + proxy logic), rewriter (JSON tarball URL rewriting), and keyer (cache key generation). Metadata is JSON-based — parse, rewrite `dist.tarball` fields, re-serialize. Tarballs are immutable and cached long-term. Scoped packages (`@scope/package`) require special route handling.

**Tech Stack:** Go/Gin (same as existing adapters), `encoding/json` for metadata rewriting, no new dependencies.

---

## Task 1: Add npm config section

**Files:**
- Modify: `internal/config/config.go`
- Modify: `config.example.toml`

**Step 1: Add NPM field to Config struct**

In `internal/config/config.go`, add `NPM` field to the Config struct after `APT`:

```go
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Storage  StorageConfig  `mapstructure:"storage"`
	Cache    CacheConfig    `mapstructure:"cache"`
	Auth     AuthConfig     `mapstructure:"auth"`
	PyPI     AdapterConfig  `mapstructure:"pypi"`
	APT      AdapterConfig  `mapstructure:"apt"`
	NPM      AdapterConfig  `mapstructure:"npm"`
}
```

**Step 2: Add npm upstreams to config.example.toml**

Append to `config.example.toml`:

```toml
[[npm.upstreams]]
name     = "npmmirror"
url      = "https://registry.npmmirror.com"
priority = 1

[[npm.upstreams]]
name     = "official"
url      = "https://registry.npmjs.org"
priority = 2
proxy    = "http://127.0.0.1:7890"
```

Also add to any existing `config.toml` if present.

**Step 3: Verify compilation**

Run: `go build ./...`

**Step 4: Commit**

```
feat: add npm upstream config section
```

---

## Task 2: Create npm cache key generator

**Files:**
- Create: `internal/adapter/npm/keyer.go`

**Step 1: Create keyer.go**

```go
package npm

import "strings"

// MetadataCacheKey returns the cache key for a package's metadata.
func MetadataCacheKey(packageName string) string {
	return "npm/" + strings.ToLower(packageName) + "/metadata.json"
}

// ScopedMetadataCacheKey returns the cache key for a scoped package's metadata.
func ScopedMetadataCacheKey(scope, packageName string) string {
	return "npm/@" + strings.ToLower(scope) + "/" + strings.ToLower(packageName) + "/metadata.json"
}

// TarballCacheKey returns the cache key for a package tarball.
func TarballCacheKey(packageName, filename string) string {
	return "npm/" + strings.ToLower(packageName) + "/-/" + filename
}

// ScopedTarballCacheKey returns the cache key for a scoped package tarball.
func ScopedTarballCacheKey(scope, packageName, filename string) string {
	return "npm/@" + strings.ToLower(scope) + "/" + strings.ToLower(packageName) + "/-/" + filename
}
```

**Step 2: Verify and commit**

```
feat: add npm cache key generator
```

---

## Task 3: Create npm metadata URL rewriter

**Files:**
- Create: `internal/adapter/npm/rewriter.go`

**Step 1: Create rewriter.go**

The rewriter parses the npm metadata JSON, walks all `versions[*].dist.tarball` fields, and replaces upstream URLs with the proxy URL.

```go
package npm

import (
	"encoding/json"
	"strings"
)

// RewriteTarballURLs rewrites all dist.tarball URLs in an npm packument JSON
// to point through the proxy. baseURL is the proxy's base URL (e.g. "http://localhost:8080").
//
// Input tarball URL:  https://registry.npmjs.org/express/-/express-4.18.2.tgz
// Output tarball URL: http://localhost:8080/npm/express/-/express-4.18.2.tgz
//
// For scoped packages:
// Input:  https://registry.npmjs.org/@babel/core/-/core-7.23.0.tgz
// Output: http://localhost:8080/npm/@babel/core/-/core-7.23.0.tgz
func RewriteTarballURLs(data []byte, baseURL string) ([]byte, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	// Get package name for constructing proxy URLs
	name, _ := doc["name"].(string)
	if name == "" {
		return data, nil // no name, return as-is
	}

	versions, ok := doc["versions"].(map[string]interface{})
	if !ok {
		return data, nil // no versions, return as-is
	}

	for _, versionData := range versions {
		vMap, ok := versionData.(map[string]interface{})
		if !ok {
			continue
		}
		dist, ok := vMap["dist"].(map[string]interface{})
		if !ok {
			continue
		}
		tarball, ok := dist["tarball"].(string)
		if !ok {
			continue
		}

		// Extract the filename from the tarball URL
		// e.g. "https://registry.npmjs.org/express/-/express-4.18.2.tgz"
		// → filename = "express-4.18.2.tgz"
		idx := strings.LastIndex(tarball, "/-/")
		if idx < 0 {
			continue
		}
		suffix := tarball[idx:] // "/-/express-4.18.2.tgz"

		// Construct new URL: baseURL + /npm/ + name + suffix
		dist["tarball"] = baseURL + "/npm/" + name + suffix
	}

	return json.Marshal(doc)
}
```

**Step 2: Verify and commit**

```
feat: add npm metadata tarball URL rewriter
```

---

## Task 4: Create npm handler

**Files:**
- Create: `internal/adapter/npm/handler.go`

**Step 1: Create handler.go**

This is the main adapter. It handles:
- `GET /:package` — metadata (unscoped)
- `GET /@:scope/:package` — metadata (scoped)
- `GET /:package/-/:filename` — tarball (unscoped)
- `GET /@:scope/:package/-/:filename` — tarball (scoped)

```go
package npm

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
	"depsilo/internal/upstream"
)

type Handler struct {
	cacheMgr *cache.Manager
	selector upstream.Selector
	cfg      config.CacheConfig
	db       *gorm.DB
}

func New(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB) *Handler {
	return &Handler{
		cacheMgr: cacheMgr,
		selector: selector,
		cfg:      cfg,
		db:       database,
	}
}

func (h *Handler) Type() string { return "npm" }

func (h *Handler) Register(rg *gin.RouterGroup) {
	// Tarball routes (must be before metadata to avoid conflicts)
	rg.GET("/:package/-/:filename", h.handleTarball)
	rg.GET("/@:scope/:package/-/:filename", h.handleScopedTarball)

	// Metadata routes
	rg.GET("/:package", h.handleMetadata)
	rg.GET("/@:scope/:package", h.handleScopedMetadata)
}

// --- Metadata handlers ---

func (h *Handler) handleMetadata(c *gin.Context) {
	pkg := c.Param("package")
	// Skip if this looks like a tarball route that Gin mis-routed
	if pkg == "-" {
		c.Status(http.StatusNotFound)
		return
	}
	h.proxyMetadata(c, pkg, MetadataCacheKey(pkg), "/"+pkg)
}

func (h *Handler) handleScopedMetadata(c *gin.Context) {
	scope := c.Param("scope")
	pkg := c.Param("package")
	fullName := "@" + scope + "/" + pkg
	h.proxyMetadata(c, fullName, ScopedMetadataCacheKey(scope, pkg), "/"+fullName)
}

func (h *Handler) proxyMetadata(c *gin.Context, fullName, cacheKey, upstreamPath string) {
	start := time.Now()
	baseURL := getBaseURL(c)

	// Forward Accept header for abbreviated metadata
	acceptHeader := c.GetHeader("Accept")

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "npm", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, err
		}

		zap.L().Info("fetching npm metadata from upstream",
			zap.String("package", fullName),
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.FetchWithHeaders(ctx, upstreamPath, map[string]string{
			"Accept": acceptHeader,
		})
		if err != nil {
			return nil, "", 0, err
		}

		body, err := io.ReadAll(fetchResult.Body)
		fetchResult.Body.Close()
		if err != nil {
			return nil, "", 0, err
		}

		// Rewrite tarball URLs — store with empty base, apply runtime base later
		rewritten, err := RewriteTarballURLs(body, "")
		if err != nil {
			zap.L().Warn("npm url rewrite failed, using original", zap.Error(err))
			rewritten = body
		}

		ct := fetchResult.ContentType
		if ct == "" {
			ct = "application/json"
		}

		return io.NopCloser(strings.NewReader(string(rewritten))), ct, int64(len(rewritten)), nil
	})

	if err != nil {
		zap.L().Error("failed to get npm metadata", zap.String("package", fullName), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
		return
	}
	defer result.Reader.Close()

	// Read cached JSON and apply runtime base URL
	body, err := io.ReadAll(result.Reader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "read cache"})
		return
	}

	// Replace empty-base URLs with actual base URL
	// Cached version has: "/npm/express/-/express-4.18.2.tgz"
	// Replace with: "http://host:port/npm/express/-/express-4.18.2.tgz"
	content := strings.ReplaceAll(string(body), `"/npm/`, `"`+baseURL+`/npm/`)

	ct := result.ContentType
	if ct == "" {
		ct = "application/json"
	}
	c.Header("Content-Type", ct)
	c.String(http.StatusOK, content)

	adapter.LogAccess(h.db, "npm", cacheKey, result.Hit, "", time.Since(start), http.StatusOK, c.ClientIP(), int64(len(content)))
}

// --- Tarball handlers ---

func (h *Handler) handleTarball(c *gin.Context) {
	pkg := c.Param("package")
	filename := c.Param("filename")
	h.proxyTarball(c, pkg, filename, TarballCacheKey(pkg, filename), "/"+pkg+"/-/"+filename)
}

func (h *Handler) handleScopedTarball(c *gin.Context) {
	scope := c.Param("scope")
	pkg := c.Param("package")
	filename := c.Param("filename")
	fullName := "@" + scope + "/" + pkg
	h.proxyTarball(c, fullName, filename, ScopedTarballCacheKey(scope, pkg, filename), "/"+fullName+"/-/"+filename)
}

func (h *Handler) proxyTarball(c *gin.Context, fullName, filename, cacheKey, upstreamPath string) {
	start := time.Now()

	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "npm", h.cfg.TTLBlob, func(ctx context.Context) (io.ReadCloser, string, int64, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, err
		}

		zap.L().Info("fetching npm tarball from upstream",
			zap.String("package", fullName),
			zap.String("filename", filename),
			zap.String("upstream", ups.Name),
		)

		fetchResult, err := ups.Fetch(ctx, upstreamPath)
		if err != nil {
			return nil, "", 0, err
		}

		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, nil
	})

	if err != nil {
		zap.L().Error("failed to get npm tarball", zap.String("package", fullName), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPSTREAM_UNAVAILABLE", "message": err.Error()})
		return
	}
	defer result.Reader.Close()

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

	adapter.LogAccess(h.db, "npm", cacheKey, result.Hit, "", time.Since(start), http.StatusOK, c.ClientIP(), written)
}

func getBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + c.Request.Host
}
```

**Step 2: Check if `FetchWithHeaders` exists on Upstream**

The handler uses `ups.FetchWithHeaders()` to forward the `Accept` header. This method likely doesn't exist yet. If not, add it to `internal/upstream/pool.go` — a variant of `Fetch` that accepts extra headers:

```go
func (u *Upstream) FetchWithHeaders(ctx context.Context, path string, headers map[string]string) (*FetchResult, error) {
	// Same as Fetch, but sets additional headers on the request
}
```

Or simpler: just modify the existing `Fetch` to always work (since npm public registry returns JSON regardless, and the Accept header for abbreviated metadata is an optimization). For the initial implementation, we can skip the Accept header forwarding and just use the full metadata. Add a TODO for optimization.

**Step 3: Verify and commit**

```
feat: add npm proxy handler with metadata rewriting and tarball caching
```

---

## Task 5: Add FetchWithHeaders to upstream pool

**Files:**
- Modify: `internal/upstream/pool.go`

**Step 1: Add FetchWithHeaders method**

Add after the existing `Fetch` method:

```go
// FetchWithHeaders fetches a resource from this upstream with additional request headers.
func (u *Upstream) FetchWithHeaders(ctx context.Context, path string, headers map[string]string) (*FetchResult, error) {
	url := strings.TrimRight(u.URL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	start := time.Now()
	resp, err := u.client.Do(req)
	latency := time.Since(start)

	if err != nil {
		u.Report(u.Name, latency, false)
		return nil, err
	}

	u.Report(u.Name, latency, resp.StatusCode < 500)

	return &FetchResult{
		Body:        resp.Body,
		ContentType: resp.Header.Get("Content-Type"),
		Size:        resp.ContentLength,
		StatusCode:  resp.StatusCode,
	}, nil
}
```

Need to add `"strings"` import if not already present.

**Step 2: Verify and commit**

```
feat: add FetchWithHeaders to upstream pool
```

---

## Task 6: Wire npm adapter into main.go and router

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `internal/api/router.go`

**Step 1: Add npm pool and handler to main.go**

After the APT pool initialization (~line 98-101), add:

```go
	// Initialize npm upstream pool
	npmPool, err := upstream.NewPool(cfg.NPM.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create npm upstream pool", zap.Error(err))
	}
```

Add npm to syncUpstreams (~line 91):

```go
	syncUpstreams(database, "npm", cfg.NPM.Upstreams)
```

Add npm health check (~line 107):

```go
	go upstream.StartHealthCheck(ctx, npmPool, database, 30*time.Second)
```

Add npm to Deps struct (pass npmPool):

```go
	NPMPool:  npmPool,
```

Register npm handler after APT handler:

```go
	// Register npm adapter
	npmHandler := npm.New(cacheMgr, upstream.NewPrioritySelector(npmPool), cfg.Cache, database)
	npmGroup := r.Group("/npm")
	npmHandler.Register(npmGroup)
```

Import `"depsilo/internal/adapter/npm"`.

**Step 2: Add NPMPool to Deps struct in router.go**

```go
type Deps struct {
	DB       *gorm.DB
	Storage  cache.Storage
	Config   *config.Config
	PyPIPool *upstream.Pool
	APTPool  *upstream.Pool
	NPMPool  *upstream.Pool
	EventBus *cache.EventBus
}
```

Add npm upstreams to the stats handler if needed. For now the stats handler iterates pypiPool and aptPool — add npmPool iteration.

**Step 3: Update stats.go to include npm upstreams**

In `internal/api/public/stats.go`, the `GetStats` handler iterates pypiPool and aptPool for upstream status. Add npmPool. This requires passing npmPool to `NewStatsHandler`.

Update constructor:
```go
func NewStatsHandler(database *gorm.DB, storage cache.Storage, pypiPool, aptPool, npmPool *upstream.Pool) *StatsHandler
```

Add npm upstream iteration after apt loop.

Similarly update `internal/api/admin/dashboard.go` `NewDashboardHandler`.

**Step 4: Verify and commit**

```
feat: wire npm adapter into server startup and routing
```

---

## Task 7: Update ExtractPackageName for npm

**Files:**
- Modify: `internal/cache/manager.go`
- Modify: `internal/adapter/accesslog.go`

**Step 1: Add npm case to ExtractPackageName in manager.go**

In the `ExtractPackageName` function, add a `case "npm":` block:

```go
	case "npm":
		// key: npm/<package>/metadata.json or npm/<package>/-/<filename>
		// or npm/@<scope>/<package>/metadata.json or npm/@<scope>/<package>/-/<filename>
		trimmed := strings.TrimPrefix(key, "npm/")
		if strings.HasPrefix(trimmed, "@") {
			// Scoped: @scope/package/...
			parts := strings.SplitN(trimmed, "/", 3)
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1] // @scope/package
			}
		} else {
			// Unscoped: package/...
			parts := strings.SplitN(trimmed, "/", 2)
			if len(parts) >= 1 {
				return parts[0]
			}
		}
```

**Step 2: Add same logic to extractPackageName in accesslog.go**

Add `case "npm":` with the same logic.

**Step 3: Verify and commit**

```
feat: add npm package name extraction for cache and access logs
```

---

## Task 8: Add npm config to config.toml and update QuickStart

**Files:**
- Modify: `config.toml` (if exists)
- Modify: `web/src/portal/pages/QuickStart.tsx`
- Modify: `web/src/i18n/en.ts`
- Modify: `web/src/i18n/zh.ts`

**Step 1: Add npm tab to QuickStart page**

The QuickStart page currently has pip and apt tabs. Add a third tab for npm with steps:

1. **Temporary Usage**: `npm install express --registry http://<host>/npm/`
2. **Permanent Config**: `npm config set registry http://<host>/npm/`
3. **`.npmrc` file**: Show the file content
4. **yarn / pnpm users**: `yarn config set registry ...` and pnpm notes

**Step 2: Add i18n keys for npm**

Add `npmLabel`, `npmDesc`, and npm-specific step keys to both en.ts and zh.ts in the `quickstart` section.

**Step 3: Build frontend and backend**

```bash
cd web && npm run build
cd .. && go build -o bin/depsilo ./cmd/server
```

**Step 4: Commit**

```
feat: add npm tab to QuickStart page with config instructions
```

---

## Task 9: Final build and smoke test

**Step 1: Full build**

```bash
go build -o bin/depsilo ./cmd/server
```

**Step 2: Smoke test**

1. Start the server
2. `curl http://localhost:23333/npm/express | head -c 200` — should return JSON metadata
3. `npm install express --registry http://localhost:23333/npm/` — should succeed
4. Check `/admin` dashboard shows npm upstreams
5. Check `/packages` shows npm cached packages

**Step 3: Commit**

```
feat: npm adapter complete - proxy cache for npm registry
```
