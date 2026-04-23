# Extra PyPI Indexes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Depsilo proxy multiple PyPI-compatible indexes at different path prefixes (e.g., `/pypi-torch-cu130/`) so CUDA-specific wheels get cached alongside standard PyPI packages.

**Architecture:** Make the PyPI handler's path prefix configurable via a new `pathPrefix` + `adapterID` field. Add `ExtraIndexConfig` to the config struct. Register extra PyPI adapter instances in server.go from the config.

**Tech Stack:** Go (config, handler, server), no new dependencies.

---

## File Structure

| Action | File | Responsibility |
| ------ | ---- | -------------- |
| Modify | `internal/config/config.go` | Add `ExtraIndexConfig` struct + field on `Config` |
| Modify | `internal/adapter/pypi/handler.go` | Add `pathPrefix`/`adapterID` fields, parameterize hardcoded `/pypi/` |
| Modify | `internal/adapter/pypi/rewriter.go` | `RewriteURLs` takes `pathPrefix` parameter |
| Modify | `internal/adapter/pypi/keyer.go` | Key functions take `prefix` parameter |
| Modify | `cmd/server/server.go` | Register extra indexes from config |
| Modify | `tests/unit/url_rewrite_test.go` | Update existing tests for new signature |
| Modify | `config.example.toml` | Add commented example |

---

### Task 1: Add ExtraIndexConfig to config

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add ExtraIndexConfig struct and field**

In `internal/config/config.go`, add after the `AdapterConfig` struct (line 70):

```go
type ExtraIndexConfig struct {
	Name      string           `mapstructure:"name"`
	Path      string           `mapstructure:"path"`
	Upstreams []UpstreamConfig `mapstructure:"upstreams"`
}
```

Add to the `Config` struct, after the `Security` field (line 27):

```go
	ExtraIndexes []ExtraIndexConfig `mapstructure:"extra_indexes"`
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add ExtraIndexConfig for additional PyPI-compatible indexes"
```

---

### Task 2: Make PyPI keyer prefix-aware

**Files:**
- Modify: `internal/adapter/pypi/keyer.go`

- [ ] **Step 1: Add prefix parameter to key functions**

Replace the entire `internal/adapter/pypi/keyer.go` with:

```go
package pypi

import "strings"

// IndexCacheKey returns the cache key for a package's simple index page.
func IndexCacheKey(prefix, packageName string) string {
	return prefix + "/simple/" + strings.ToLower(packageName) + "/index.html"
}

// FileCacheKey returns the cache key for a package file.
func FileCacheKey(prefix, filepath string) string {
	return prefix + "/files/" + strings.TrimPrefix(filepath, "/")
}
```

- [ ] **Step 2: Verify build fails**

Run: `go build ./...`
Expected: Build fails — callers pass wrong number of args. This confirms we need to update them.

- [ ] **Step 3: Commit**

```bash
git add internal/adapter/pypi/keyer.go
git commit -m "feat(pypi): make cache key prefix configurable"
```

---

### Task 3: Make PyPI rewriter prefix-aware

**Files:**
- Modify: `internal/adapter/pypi/rewriter.go`

- [ ] **Step 1: Add pathPrefix parameter to RewriteURLs**

Replace the entire `internal/adapter/pypi/rewriter.go` with:

```go
package pypi

import (
	"regexp"
	"strings"
)

// hrefRe matches all href attributes in HTML anchor tags.
var hrefRe = regexp.MustCompile(`href="([^"]+)"`)

// RewriteURLs rewrites all package download URLs in a PyPI simple index HTML page
// to point through our proxy. pathPrefix is the route prefix (e.g., "/pypi" or "/pypi-torch-cu130").
func RewriteURLs(html string, baseURL string, pathPrefix string) string {
	baseURL = strings.TrimRight(baseURL, "/")

	return hrefRe.ReplaceAllStringFunc(html, func(match string) string {
		sub := hrefRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		url := sub[1]

		// Find /packages/ in the URL — this is the key path component
		idx := strings.Index(url, "/packages/")
		if idx < 0 {
			// Also handle relative paths like "../../packages/..."
			idx = strings.Index(url, "packages/")
			if idx < 0 {
				return match
			}
			filePath := "/" + url[idx:]
			return `href="` + baseURL + pathPrefix + `/files` + filePath + `"`
		}

		filePath := url[idx:]
		return `href="` + baseURL + pathPrefix + `/files` + filePath + `"`
	})
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/adapter/pypi/rewriter.go
git commit -m "feat(pypi): make URL rewrite path prefix configurable"
```

---

### Task 4: Make PyPI handler prefix-aware

**Files:**
- Modify: `internal/adapter/pypi/handler.go`

- [ ] **Step 1: Add fields and update constructor**

Replace the `Handler` struct, `New` function, and `Type` method (lines 22-40) with:

```go
// Handler implements the PyPI adapter.
type Handler struct {
	cacheMgr   *cache.Manager
	selector   upstream.Selector
	cfg        config.CacheConfig
	db         *gorm.DB
	pathPrefix string // route prefix: "/pypi" or "/pypi-torch-cu130"
	adapterID  string // for cache keys and logs: "pypi" or "extra:pytorch-cu130"
}

// New creates a new PyPI handler with default "/pypi" prefix.
func New(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB) *Handler {
	return &Handler{
		cacheMgr:   cacheMgr,
		selector:   selector,
		cfg:        cfg,
		db:         database,
		pathPrefix: "/pypi",
		adapterID:  "pypi",
	}
}

// NewWithPrefix creates a PyPI handler with a custom path prefix and adapter ID.
func NewWithPrefix(cacheMgr *cache.Manager, selector upstream.Selector, cfg config.CacheConfig, database *gorm.DB, pathPrefix, adapterID string) *Handler {
	return &Handler{
		cacheMgr:   cacheMgr,
		selector:   selector,
		cfg:        cfg,
		db:         database,
		pathPrefix: pathPrefix,
		adapterID:  adapterID,
	}
}

func (h *Handler) Type() string { return h.adapterID }
```

- [ ] **Step 2: Update handleSimpleIndex log line (line 69)**

Change:
```go
		zap.L().Warn("copy to client failed", zap.String("key", "/pypi/simple/"), zap.Error(copyErr))
```
To:
```go
		zap.L().Warn("copy to client failed", zap.String("key", h.pathPrefix+"/simple/"), zap.Error(copyErr))
```

- [ ] **Step 3: Update handlePackageRedirect (line 76)**

Change:
```go
	c.Redirect(http.StatusMovedPermanently, "/pypi/simple/"+pkg+"/")
```
To:
```go
	c.Redirect(http.StatusMovedPermanently, h.pathPrefix+"/simple/"+pkg+"/")
```

- [ ] **Step 4: Update handlePackageIndex — cache key and rewrite calls**

Change `cacheKey` (line 82):
```go
	cacheKey := IndexCacheKey(pkg)
```
To:
```go
	cacheKey := IndexCacheKey(h.adapterID, pkg)
```

Change `RewriteURLs` call (line 112):
```go
		html = RewriteURLs(html, "")
```
To:
```go
		html = RewriteURLs(html, "", h.pathPrefix)
```

Change the runtime URL replacement (line 140):
```go
	html = strings.ReplaceAll(html, `href="/pypi/files/`, `href="`+baseURL+`/pypi/files/`)
```
To:
```go
	html = strings.ReplaceAll(html, `href="`+h.pathPrefix+`/files/`, `href="`+baseURL+h.pathPrefix+`/files/`)
```

Change the `cacheMgr.Get` adapter type (line 87):
```go
	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "pypi", h.cfg.TTLIndex, func(...) {
```
To:
```go
	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, h.adapterID, h.cfg.TTLIndex, func(...) {
```

Change `LogAccess` adapter type (line 149):
```go
	adapter.LogAccess(h.db, "pypi", c.Request.Method, cacheKey, ...)
```
To:
```go
	adapter.LogAccess(h.db, h.adapterID, c.Request.Method, cacheKey, ...)
```

- [ ] **Step 5: Update handleFileDownload — cache key and adapter type**

Change `cacheKey` (line 155):
```go
	cacheKey := FileCacheKey(filepath)
```
To:
```go
	cacheKey := FileCacheKey(h.adapterID, filepath)
```

Change `cacheMgr.Get` adapter type (line 158):
```go
	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, "pypi", h.cfg.TTLBlob, func(...) {
```
To:
```go
	result, err := h.cacheMgr.Get(c.Request.Context(), cacheKey, h.adapterID, h.cfg.TTLBlob, func(...) {
```

Change `LogAccess` adapter type (line 204):
```go
	adapter.LogAccess(h.db, "pypi", c.Request.Method, cacheKey, ...)
```
To:
```go
	adapter.LogAccess(h.db, h.adapterID, c.Request.Method, cacheKey, ...)
```

- [ ] **Step 6: Verify build succeeds**

Run: `go build ./...`
Expected: Build succeeds.

- [ ] **Step 7: Commit**

```bash
git add internal/adapter/pypi/handler.go
git commit -m "feat(pypi): parameterize path prefix and adapter ID in handler"
```

---

### Task 5: Update existing tests

**Files:**
- Modify: `tests/unit/url_rewrite_test.go`
- Modify: `tests/unit/cache_key_test.go`

- [ ] **Step 1: Update URL rewrite tests**

In `tests/unit/url_rewrite_test.go`, every call to `pypi.RewriteURLs(html, baseURL)` needs a third argument. Update all calls:

- `pypi.RewriteURLs(html, "http://localhost:8080")` → `pypi.RewriteURLs(html, "http://localhost:8080", "/pypi")`
- `pypi.RewriteURLs(html, "")` → `pypi.RewriteURLs(html, "", "/pypi")`

Search and replace all occurrences.

- [ ] **Step 2: Update cache key tests**

In `tests/unit/cache_key_test.go`, update PyPI cache key calls:

- `pypi.IndexCacheKey("requests")` → `pypi.IndexCacheKey("pypi", "requests")`
- `pypi.FileCacheKey(...)` → `pypi.FileCacheKey("pypi", ...)`

Search and replace all occurrences.

- [ ] **Step 3: Run all tests**

Run: `go test ./tests/unit/... -v`
Expected: All tests pass.

- [ ] **Step 4: Commit**

```bash
git add tests/unit/url_rewrite_test.go tests/unit/cache_key_test.go
git commit -m "test: update PyPI tests for prefix-aware keyer and rewriter"
```

---

### Task 6: Register extra indexes in server.go

**Files:**
- Modify: `cmd/server/server.go`

- [ ] **Step 1: Add extra index registration after Docker handler**

In `cmd/server/server.go`, after the Docker handler block (after line 255, the closing `}` of the Docker `if` block), add:

```go
	// Register extra PyPI-compatible indexes
	for _, idx := range cfg.ExtraIndexes {
		idxPool, err := upstream.NewPool(idx.Upstreams)
		if err != nil {
			return nil, fmt.Errorf("create extra index %s pool: %w", idx.Name, err)
		}
		syncUpstreams(database, "extra:"+idx.Name, idx.Upstreams)
		upstream.RestoreFromDB(idxPool, database)
		go upstream.StartHealthCheck(ctx, idxPool, database, 30*time.Second)

		idxHandler := pypi.NewWithPrefix(cacheMgr, upstream.NewPrioritySelector(idxPool), cfg.Cache, database, "/"+idx.Path, "extra:"+idx.Name)
		idxHandler.Register(r.Group("/" + idx.Path))
		idxHandler.Register(projectGroup.Group("/" + idx.Path))

		zap.L().Info("extra index registered",
			zap.String("name", idx.Name),
			zap.String("path", "/"+idx.Path),
			zap.Int("upstreams", len(idx.Upstreams)),
		)
	}
```

Note: `projectGroup` is defined later in the code. Move this block to after the project group registration (after line 263). Read the file to find the exact insertion point.

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: Build succeeds.

- [ ] **Step 3: Run all tests**

Run: `go test ./... 2>&1 | tail -10`
Expected: All tests pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/server/server.go
git commit -m "feat(server): register extra PyPI indexes from config"
```

---

### Task 7: Add config example and verify end-to-end

**Files:**
- Modify: `config.example.toml`

- [ ] **Step 1: Add extra_indexes example to config.example.toml**

Append at the end of `config.example.toml`:

```toml

# ─── Extra PyPI-compatible indexes ───────────
# Register additional PyPI indexes at custom paths.
# Useful for CUDA-specific wheels (PyTorch, vLLM).
#
# [[extra_indexes]]
# name = "pytorch-cu130"
# path = "pypi-torch-cu130"
# [[extra_indexes.upstreams]]
# name = "pytorch"
# url = "https://download.pytorch.org/whl/cu130"
# priority = 1
#
# [[extra_indexes]]
# name = "vllm-cu130"
# path = "pypi-vllm-cu130"
# [[extra_indexes.upstreams]]
# name = "vllm-wheels"
# url = "https://wheels.vllm.ai/0.19.1/cu130"
# priority = 1
#
# Usage:
#   pip install torch --index-url http://HOST:PORT/pypi-torch-cu130/simple/
```

- [ ] **Step 2: Add real extra_indexes to config.toml for testing**

Add to your `config.toml`:

```toml
[[extra_indexes]]
name = "pytorch-cu130"
path = "pypi-torch-cu130"
[[extra_indexes.upstreams]]
name = "pytorch"
url = "https://download.pytorch.org/whl/cu130"
priority = 1
```

- [ ] **Step 3: Start the server and verify**

Run: `make dev`

Then test:
```bash
# Verify the extra index endpoint exists
curl -s http://localhost:23333/pypi-torch-cu130/simple/torch/ | head -5

# Should return HTML with href links pointing to /pypi-torch-cu130/files/...
```

- [ ] **Step 4: Commit**

```bash
git add config.example.toml
git commit -m "docs: add extra_indexes example to config.example.toml"
```
