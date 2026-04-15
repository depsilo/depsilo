# Docker Registry Proxy — Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden the Docker Registry proxy with tests, operational logging, singleflight token dedup, and Prometheus metrics.

**Architecture:** No structural changes. Add unit + integration tests following existing patterns (`tests/unit/`, `tests/integration/`). Fix cache key encoding for filesystem safety. Add logging/metrics to existing handler code paths.

**Tech Stack:** Go 1.21, testing stdlib, singleflight, zap, prometheus client_golang

---

### Task 1: Fix Cache Key Encoding (`:` → `__`)

Docker digest keys contain `:` (e.g. `sha256:abc123`) which is an illegal filesystem character on Windows and flagged by existing `TestCacheKey_ValidFilesystemPath`. Encode `:` as `__` in cache keys only.

**Files:**
- Modify: `internal/adapter/docker/keyer.go`
- Modify: `internal/adapter/docker/handler.go:110` (blob digest header still uses original format)

- [ ] **Step 1: Update keyer.go to encode colons**

Replace the entire file:

```go
package docker

import (
	"fmt"
	"strings"
)

// encodeDigest replaces ":" with "__" for filesystem-safe cache keys.
func encodeDigest(s string) string {
	return strings.ReplaceAll(s, ":", "__")
}

func ManifestCacheKey(registryName, imageName, reference string) string {
	return fmt.Sprintf("docker/%s/manifests/%s/%s", registryName, imageName, encodeDigest(reference))
}

func BlobCacheKey(registryName, digest string) string {
	return fmt.Sprintf("docker/%s/blobs/%s", registryName, encodeDigest(digest))
}

func TagListCacheKey(registryName, imageName string) string {
	return fmt.Sprintf("docker/%s/tags/%s/list", registryName, imageName)
}
```

- [ ] **Step 2: Verify handler.go still uses original digest for Docker-Content-Digest header**

In `handler.go:123`, the line `c.Header("Docker-Content-Digest", digest)` uses the raw `digest` variable (from `strings.TrimPrefix(endpoint, "blobs/")`), NOT the cache key. This is correct — no change needed. Verify by reading the line.

- [ ] **Step 3: Run build to verify compilation**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./...`
Expected: success, no errors

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/docker/keyer.go
git commit -m "fix(docker): encode colons in cache keys for filesystem safety"
```

---

### Task 2: Unit Tests — Cache Keys

**Files:**
- Modify: `tests/unit/cache_key_test.go`

- [ ] **Step 1: Add Docker cache key tests**

Add the following at the end of `tests/unit/cache_key_test.go`, before the `TestCacheKey_NoCollision` function:

```go
// ---------- Docker ----------

func TestCacheKey_Docker_Manifest(t *testing.T) {
	key := dockerAdapter.ManifestCacheKey("dockerhub", "library/nginx", "latest")
	if key != "docker/dockerhub/manifests/library/nginx/latest" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Docker_ManifestDigest(t *testing.T) {
	key := dockerAdapter.ManifestCacheKey("dockerhub", "library/nginx", "sha256:abc123")
	if key != "docker/dockerhub/manifests/library/nginx/sha256__abc123" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Docker_Blob(t *testing.T) {
	key := dockerAdapter.BlobCacheKey("dockerhub", "sha256:abc123")
	if key != "docker/dockerhub/blobs/sha256__abc123" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Docker_TagList(t *testing.T) {
	key := dockerAdapter.TagListCacheKey("dockerhub", "library/nginx")
	if key != "docker/dockerhub/tags/library/nginx/list" {
		t.Errorf("got %s", key)
	}
}
```

- [ ] **Step 2: Add Docker import to imports block**

Add to the import block:

```go
dockerAdapter "depsilo/internal/adapter/docker"
```

- [ ] **Step 3: Add Docker to TestCacheKey_NoCollision**

Add this entry to the `keys` map in `TestCacheKey_NoCollision`:

```go
"docker":   dockerAdapter.ManifestCacheKey("dockerhub", "library/nginx", "latest"),
```

- [ ] **Step 4: Add Docker to TestCacheKey_ValidFilesystemPath**

Add these entries to the `keys` slice in `TestCacheKey_ValidFilesystemPath`:

```go
dockerAdapter.ManifestCacheKey("dockerhub", "library/nginx", "latest"),
dockerAdapter.ManifestCacheKey("dockerhub", "library/nginx", "sha256:abc123"),
dockerAdapter.BlobCacheKey("dockerhub", "sha256:abc123"),
dockerAdapter.TagListCacheKey("dockerhub", "library/nginx"),
```

- [ ] **Step 5: Run tests**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go test ./tests/unit/ -run TestCacheKey -v`
Expected: all tests PASS, including ValidFilesystemPath with the encoded Docker keys

- [ ] **Step 6: Commit**

```bash
git add tests/unit/cache_key_test.go
git commit -m "test(docker): add cache key unit tests"
```

---

### Task 3: Unit Tests — Resolver

**Files:**
- Create: `tests/unit/docker_resolver_test.go`

- [ ] **Step 1: Create resolver test file**

Create `tests/unit/docker_resolver_test.go`:

```go
package unit

import (
	"testing"

	"depsilo/internal/adapter/docker"
	"depsilo/internal/config"
)

func newTestResolver() *docker.Resolver {
	cfg := config.DockerConfig{
		DefaultRegistry: "dockerhub",
		Registries: []config.RegistryConfig{
			{Name: "dockerhub", URL: "https://registry-1.docker.io"},
			{Name: "ghcr", URL: "https://ghcr.io"},
		},
	}
	return docker.NewResolver(cfg)
}

func TestResolver_DefaultRegistry(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("library/nginx/manifests/latest")
	if reg == nil || reg.Name != "dockerhub" {
		t.Fatalf("expected dockerhub, got %v", reg)
	}
	if imageName != "library/nginx" {
		t.Errorf("imageName = %q, want library/nginx", imageName)
	}
	if endpoint != "manifests/latest" {
		t.Errorf("endpoint = %q, want manifests/latest", endpoint)
	}
}

func TestResolver_DomainRouting(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("ghcr.io/owner/repo/manifests/v1.0")
	if reg == nil || reg.Name != "ghcr" {
		t.Fatalf("expected ghcr, got %v", reg)
	}
	if imageName != "owner/repo" {
		t.Errorf("imageName = %q, want owner/repo", imageName)
	}
	if endpoint != "manifests/v1.0" {
		t.Errorf("endpoint = %q, want manifests/v1.0", endpoint)
	}
}

func TestResolver_BlobEndpoint(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("myimage/blobs/sha256:abc")
	if reg == nil || reg.Name != "dockerhub" {
		t.Fatalf("expected dockerhub default, got %v", reg)
	}
	if imageName != "myimage" {
		t.Errorf("imageName = %q, want myimage", imageName)
	}
	if endpoint != "blobs/sha256:abc" {
		t.Errorf("endpoint = %q, want blobs/sha256:abc", endpoint)
	}
}

func TestResolver_MultiLevelImageName(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("team/service/api/tags/list")
	if reg == nil || reg.Name != "dockerhub" {
		t.Fatalf("expected dockerhub default, got %v", reg)
	}
	if imageName != "team/service/api" {
		t.Errorf("imageName = %q, want team/service/api", imageName)
	}
	if endpoint != "tags/list" {
		t.Errorf("endpoint = %q, want tags/list", endpoint)
	}
}

func TestResolver_DockerHubAlias(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("docker.io/library/alpine/manifests/3.18")
	if reg == nil || reg.Name != "dockerhub" {
		t.Fatalf("expected dockerhub via alias, got %v", reg)
	}
	if imageName != "library/alpine" {
		t.Errorf("imageName = %q, want library/alpine", imageName)
	}
	if endpoint != "manifests/3.18" {
		t.Errorf("endpoint = %q, want manifests/3.18", endpoint)
	}
}

func TestResolver_UnregisteredDomain(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("unknown.registry.io/img/manifests/v1")
	if reg == nil || reg.Name != "dockerhub" {
		t.Fatalf("expected dockerhub fallback, got %v", reg)
	}
	if imageName != "unknown.registry.io/img" {
		t.Errorf("imageName = %q, want unknown.registry.io/img", imageName)
	}
	if endpoint != "manifests/v1" {
		t.Errorf("endpoint = %q, want manifests/v1", endpoint)
	}
}

func TestResolver_TooShortPath(t *testing.T) {
	r := newTestResolver()
	reg, imageName, endpoint := r.Resolve("short")
	if reg != nil {
		t.Errorf("expected nil registry for short path, got %v", reg)
	}
	if imageName != "" || endpoint != "" {
		t.Errorf("expected empty imageName and endpoint, got %q %q", imageName, endpoint)
	}
}

func TestResolver_NoEndpointKeyword(t *testing.T) {
	r := newTestResolver()
	reg, _, _ := r.Resolve("a/b")
	if reg != nil {
		t.Errorf("expected nil registry for path without endpoint keyword, got %v", reg)
	}
}
```

- [ ] **Step 2: Export Resolver fields for testing**

The `Resolver` struct and `NewResolver` are already exported, but `Resolver.Resolve` returns `*Registry` which has exported fields (`Name`, `URL`, etc.). Check that `docker.NewResolver` is callable from the test package — it takes `config.DockerConfig` which is exported. This should work with no changes.

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./tests/unit/`
Expected: compilation succeeds

- [ ] **Step 3: Run resolver tests**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go test ./tests/unit/ -run TestResolver -v`
Expected: all 8 tests PASS

- [ ] **Step 4: Commit**

```bash
git add tests/unit/docker_resolver_test.go
git commit -m "test(docker): add resolver unit tests"
```

---

### Task 4: Unit Tests — Auth Parser & Package Name Extraction

**Files:**
- Modify: `tests/unit/docker_resolver_test.go` (add auth parser tests)
- Modify: `tests/unit/extract_package_name_test.go` (add Docker cases)
- Modify: `internal/cache/manager.go` (add Docker case to `ExtractPackageName`)

- [ ] **Step 1: Add auth parser tests to docker_resolver_test.go**

Append to `tests/unit/docker_resolver_test.go`:

```go
func TestParseAuthParam(t *testing.T) {
	header := `Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`

	tests := []struct {
		name     string
		expected string
	}{
		{"realm", "https://auth.docker.io/token"},
		{"service", "registry.docker.io"},
		{"scope", "repository:library/nginx:pull"},
		{"missing", ""},
	}
	for _, tt := range tests {
		got := docker.ParseAuthParam(header, tt.name)
		if got != tt.expected {
			t.Errorf("ParseAuthParam(%q) = %q, want %q", tt.name, got, tt.expected)
		}
	}
}
```

- [ ] **Step 2: Export parseAuthParam for testing**

In `internal/adapter/docker/auth.go`, rename `parseAuthParam` to `ParseAuthParam` (line 140):

Change:
```go
func parseAuthParam(header, name string) string {
```
To:
```go
func ParseAuthParam(header, name string) string {
```

Update the two call sites in the same file (line 88-89):
```go
realm = ParseAuthParam(challenge, "realm")
service = ParseAuthParam(challenge, "service")
```

- [ ] **Step 3: Add Docker case to cache.ExtractPackageName**

In `internal/cache/manager.go`, add the Docker case before the final `}` of the switch statement (after the `helm` case, before line 168 `}`):

```go
	case "docker":
		if strings.Contains(key, "/manifests/") {
			parts := strings.SplitN(key, "/manifests/", 2)
			if len(parts) == 2 {
				image := parts[1]
				if idx := strings.LastIndex(image, "/"); idx > 0 {
					return image[:idx]
				}
				return image
			}
		}
		if strings.Contains(key, "/tags/") {
			parts := strings.SplitN(key, "/tags/", 2)
			if len(parts) == 2 {
				return strings.TrimSuffix(parts[1], "/list")
			}
		}
		if strings.Contains(key, "/blobs/") {
			return ""
		}
```

- [ ] **Step 4: Add Docker tests to extract_package_name_test.go**

Add before `TestExtractPackageName_Unknown` in `tests/unit/extract_package_name_test.go`:

```go
func TestExtractPackageName_Docker(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"docker/dockerhub/manifests/library/nginx/latest", "library/nginx"},
		{"docker/ghcr/manifests/owner/repo/sha256__abc", "owner/repo"},
		{"docker/dockerhub/manifests/team/service/api/v1.0", "team/service/api"},
		{"docker/dockerhub/blobs/sha256__abc", ""},
		{"docker/dockerhub/tags/library/nginx/list", "library/nginx"},
	}
	for _, tt := range tests {
		got := cache.ExtractPackageName("docker", tt.key)
		if got != tt.expected {
			t.Errorf("ExtractPackageName(docker, %q) = %q, want %q", tt.key, got, tt.expected)
		}
	}
}
```

- [ ] **Step 5: Run all unit tests**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go test ./tests/unit/ -v`
Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/adapter/docker/auth.go internal/cache/manager.go tests/unit/docker_resolver_test.go tests/unit/extract_package_name_test.go
git commit -m "test(docker): add auth parser and package name extraction tests"
```

---

### Task 5: Package Name Extraction in accesslog.go

The `extractPackageName` in `internal/adapter/accesslog.go` also needs the `/tags/` case added. It already has the `/manifests/` and `/blobs/` cases.

**Files:**
- Modify: `internal/adapter/accesslog.go:186-201`

- [ ] **Step 1: Add tags case to accesslog extractPackageName**

In `internal/adapter/accesslog.go`, replace the Docker case (lines 186-201):

```go
	case "docker":
		// key: docker/{registry}/manifests/{image}/{ref} or docker/{registry}/blobs/{digest}
		if strings.Contains(key, "/manifests/") {
			parts := strings.SplitN(key, "/manifests/", 2)
			if len(parts) == 2 {
				image := parts[1]
				if idx := strings.LastIndex(image, "/"); idx > 0 {
					return image[:idx]
				}
				return image
			}
		}
		if strings.Contains(key, "/tags/") {
			parts := strings.SplitN(key, "/tags/", 2)
			if len(parts) == 2 {
				return strings.TrimSuffix(parts[1], "/list")
			}
		}
		if strings.Contains(key, "/blobs/") {
			return ""
		}
```

- [ ] **Step 2: Run build**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/adapter/accesslog.go
git commit -m "fix(docker): add tags/list package name extraction in accesslog"
```

---

### Task 6: Configuration Validation & Unregistered Domain Logging

**Files:**
- Modify: `internal/adapter/docker/resolver.go`

- [ ] **Step 1: Add zap import**

Add `"go.uber.org/zap"` to the import block in `resolver.go`. The current imports are:

```go
import (
	"crypto/tls"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/config"
)
```

- [ ] **Step 2: Add startup validation warnings in NewResolver**

After the Docker Hub aliases block (line 98 `return r`), insert the following validation before the `return r` statement:

```go
	// Log warnings for default registry resolution
	if cfg.DefaultRegistry != "" && r.defaultReg == "" {
		zap.L().Warn("docker: configured default_registry not found, no default set",
			zap.String("default_registry", cfg.DefaultRegistry),
		)
	} else if cfg.DefaultRegistry != "" && r.defaultReg != cfg.DefaultRegistry {
		zap.L().Info("docker: default_registry resolved",
			zap.String("configured", cfg.DefaultRegistry),
			zap.String("resolved_to", r.defaultReg),
		)
	}

	return r
```

Remove the existing `return r` on what was line 100.

- [ ] **Step 3: Add unregistered domain logging in Resolve**

In `Resolve()`, after the `if ok {` block that returns (line 130), add an `else` logging branch. Replace lines 124-131:

```go
	if strings.ContainsAny(firstPart, ".:") {
		regName, ok := r.domainMap[firstPart]
		if ok {
			reg = r.registries[regName]
			imageName = strings.Join(parts[1:endpointIdx], "/")
			return reg, imageName, endpoint
		}
		zap.L().Info("docker: unregistered domain in path, using default registry",
			zap.String("domain", firstPart),
			zap.String("default", r.defaultReg),
		)
	}
```

- [ ] **Step 4: Run build**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./...`
Expected: success

- [ ] **Step 5: Run unit tests to verify resolver still works**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go test ./tests/unit/ -run TestResolver -v`
Expected: all PASS (the unregistered domain test should still pass — logging doesn't change behavior)

- [ ] **Step 6: Commit**

```bash
git add internal/adapter/docker/resolver.go
git commit -m "feat(docker): add config validation warnings and unregistered domain logging"
```

---

### Task 7: Invalid Path & Error Access Logging

**Files:**
- Modify: `internal/adapter/docker/handler.go`

- [ ] **Step 1: Add access logging for invalid paths (404)**

In `handleRequest`, replace lines 63-66:

```go
	if reg == nil || imageName == "" || endpoint == "" {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "invalid path"})
		adapter.LogAccess(h.db, "docker", c.Request.Method, "docker/unknown/"+path, false, "", time.Since(start), http.StatusNotFound, c.ClientIP(), 0)
		return
	}
```

- [ ] **Step 2: Add access logging for unsupported endpoints**

Replace line 75:

```go
	} else {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "unsupported endpoint"})
		adapter.LogAccess(h.db, "docker", c.Request.Method, "docker/unknown/"+path, false, "", time.Since(start), http.StatusNotFound, c.ClientIP(), 0)
	}
```

- [ ] **Step 3: Add access logging for upstream errors in handleManifest**

After line 100 (`c.JSON(http.StatusBadGateway, ...)`), add:

```go
		adapter.LogAccess(h.db, "docker", c.Request.Method, cacheKey, false, reg.Name, time.Since(start), http.StatusBadGateway, c.ClientIP(), 0)
```

- [ ] **Step 4: Add access logging for upstream errors in handleBlob**

After line 118 (`c.JSON(http.StatusBadGateway, ...)`), add:

```go
		adapter.LogAccess(h.db, "docker", c.Request.Method, cacheKey, false, reg.Name, time.Since(start), http.StatusBadGateway, c.ClientIP(), 0)
```

- [ ] **Step 5: Add access logging for upstream errors in handleTagList**

After line 136 (`c.JSON(http.StatusBadGateway, ...)`), add:

```go
		adapter.LogAccess(h.db, "docker", c.Request.Method, cacheKey, false, reg.Name, time.Since(start), http.StatusBadGateway, c.ClientIP(), 0)
```

- [ ] **Step 6: Run build**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./...`
Expected: success

- [ ] **Step 7: Commit**

```bash
git add internal/adapter/docker/handler.go
git commit -m "feat(docker): add access logging for 404 and upstream error paths"
```

---

### Task 8: Singleflight Token Deduplication

**Files:**
- Modify: `internal/adapter/docker/auth.go`

- [ ] **Step 1: Add singleflight import and field**

Update the import block to add singleflight:

```go
import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)
```

Update `AuthManager` struct:

```go
type AuthManager struct {
	mu     sync.RWMutex
	tokens map[string]*tokenEntry
	sf     singleflight.Group
}
```

- [ ] **Step 2: Replace GetToken method**

Replace the entire `GetToken` method (lines 31-64) with:

```go
func (a *AuthManager) GetToken(client *http.Client, registryURL, registryName, username, password, scope string) (string, error) {
	cacheKey := registryName + ":" + scope

	// Fast path: check cache
	a.mu.RLock()
	if entry, ok := a.tokens[cacheKey]; ok && time.Now().Before(entry.ExpiresAt) {
		a.mu.RUnlock()
		return entry.Token, nil
	}
	a.mu.RUnlock()

	// Deduplicated fetch via singleflight
	val, err, _ := a.sf.Do(cacheKey, func() (interface{}, error) {
		// Double-check after winning the singleflight
		a.mu.RLock()
		if entry, ok := a.tokens[cacheKey]; ok && time.Now().Before(entry.ExpiresAt) {
			a.mu.RUnlock()
			return entry.Token, nil
		}
		a.mu.RUnlock()

		realm, service, err := a.discoverAuth(client, registryURL)
		if err != nil {
			return "", fmt.Errorf("auth discovery failed: %w", err)
		}
		if realm == "" {
			return "", nil
		}

		token, expiresIn, err := a.fetchToken(client, realm, service, scope, username, password)
		if err != nil {
			return "", fmt.Errorf("token fetch failed: %w", err)
		}

		a.mu.Lock()
		a.tokens[cacheKey] = &tokenEntry{
			Token:     token,
			ExpiresAt: time.Now().Add(time.Duration(expiresIn-30) * time.Second),
		}
		a.mu.Unlock()

		return token, nil
	})
	if err != nil {
		return "", err
	}
	return val.(string), nil
}
```

- [ ] **Step 3: Run build**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./...`
Expected: success. If `golang.org/x/sync` is not in go.mod, run `go mod tidy` first (but it should already be there since the project uses singleflight in cache/manager.go).

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/docker/auth.go
git commit -m "fix(docker): deduplicate concurrent token fetches with singleflight"
```

---

### Task 9: Prometheus Metrics Integration

**Files:**
- Modify: `internal/adapter/docker/handler.go`

- [ ] **Step 1: Add api import**

Add `"depsilo/internal/api"` to the import block.

- [ ] **Step 2: Add metrics to streamResponse**

In `streamResponse`, add metrics calls after the `io.Copy` and before `adapter.LogAccess`. Replace the method:

```go
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
```

- [ ] **Step 3: Add metrics to handleHead**

In `handleHead`, add before the existing `adapter.LogAccess` call (line 186):

```go
	api.M.RequestsTotal.WithLabelValues("docker", "false").Inc()
	api.M.RequestDuration.WithLabelValues("docker").Observe(time.Since(start).Seconds())
```

- [ ] **Step 4: Add upstream metrics to fetchFromUpstream**

In `fetchFromUpstream`, add metrics after the upstream response. Replace lines 213-224:

```go
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
```

Note: `fetchFromUpstream` receives `reg *Registry` — but the parameter name in the current code uses the method parameter. Verify the function signature has `reg *Registry`.

- [ ] **Step 5: Run build**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./...`
Expected: success

- [ ] **Step 6: Commit**

```bash
git add internal/adapter/docker/handler.go
git commit -m "feat(docker): integrate Prometheus metrics for requests and upstream calls"
```

---

### Task 10: Integration Tests — Mock Server & Config

**Files:**
- Modify: `tests/mock/upstream_server.go`
- Modify: `tests/integration/main_test.go`

- [ ] **Step 1: Add RegisterDocker to mock server**

Add the following method to `tests/mock/upstream_server.go` before `RegisterAll`:

```go
// RegisterDocker adds Docker Registry V2 endpoints with Bearer token auth.
func (m *MockUpstream) RegisterDocker() {
	const mockToken = "mock-docker-token-12345"

	// Token endpoint — accepts any credentials, returns fixed token
	m.mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"token":"%s","expires_in":300}`, mockToken)
	})

	// /v2/ — returns 401 with WWW-Authenticate to trigger token flow
	m.mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		// Check if authenticated
		auth := r.Header.Get("Authorization")
		if auth == "Bearer "+mockToken {
			// Authenticated — serve registry endpoints
			path := strings.TrimPrefix(r.URL.Path, "/v2/")
			if path == "" || path == "/" {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{}`)
				return
			}

			if strings.Contains(path, "/manifests/") {
				w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
				w.Header().Set("Docker-Content-Digest", "sha256:fakedigest")
				fmt.Fprint(w, `{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json","config":{"digest":"sha256:fakeconfig"}}`)
				return
			}
			if strings.Contains(path, "/blobs/") {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Write([]byte("FAKE_DOCKER_BLOB_DATA"))
				return
			}
			if strings.HasSuffix(path, "/tags/list") {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"name":"library/testimg","tags":["latest","v1.0"]}`)
				return
			}
			http.NotFound(w, r)
			return
		}

		// Not authenticated — send challenge
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/auth/token",service="mock-registry"`, m.URL()))
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"errors":[{"code":"UNAUTHORIZED"}]}`)
	})
}
```

Add `"strings"` to the import block if not already present.

- [ ] **Step 2: Add RegisterDocker to RegisterAll**

In `RegisterAll`, add `m.RegisterDocker()`:

```go
func (m *MockUpstream) RegisterAll() {
	m.RegisterPyPI()
	m.RegisterAPT()
	m.RegisterNpm()
	m.RegisterGoModules()
	m.RegisterCargo()
	m.RegisterMaven()
	m.RegisterRubyGems()
	m.RegisterComposer()
	m.RegisterNuGet()
	m.RegisterConda()
	m.RegisterCRAN()
	m.RegisterHelm()
	m.RegisterDocker()
}
```

- [ ] **Step 3: Add Docker config to writeTestConfig**

In `tests/integration/main_test.go`, add Docker config to the config template string, before the closing backtick. Add after the helm config:

```go
[docker]
default_registry = "mock"

[[docker.registries]]
name = "mock"
url = "%s"
```

And add one more `upstreamURL` to the `fmt.Sprintf` arguments at the end.

- [ ] **Step 4: Verify build**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./tests/...`
Expected: success

- [ ] **Step 5: Commit**

```bash
git add tests/mock/upstream_server.go tests/integration/main_test.go
git commit -m "test(docker): add mock Docker registry and integration test config"
```

---

### Task 11: Integration Tests — Test Cases

**Files:**
- Create: `tests/integration/docker_test.go`

- [ ] **Step 1: Create Docker integration test file**

Create `tests/integration/docker_test.go`:

```go
//go:build integration

package integration

import (
	"encoding/json"
	"testing"
)

func TestDocker_VersionCheck(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/v2/")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Errorf("response is not valid JSON: %v", err)
	}
}

func TestDocker_ManifestFetch(t *testing.T) {
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/v2/library/testimg/manifests/latest")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body == "" {
		t.Error("empty manifest response")
	}
	// Should contain manifest JSON
	if !containsStr(body, "schemaVersion") {
		t.Error("manifest response missing schemaVersion")
	}
	// Upstream should have been hit
	if mockServer.RequestCount() <= before {
		t.Error("expected upstream hit on first request")
	}
}

func TestDocker_ManifestCacheHit(t *testing.T) {
	// Ensure cached from previous test
	httpGet(t, depsiloURL+"/v2/library/testimg/manifests/latest")

	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/v2/library/testimg/manifests/latest")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !containsStr(body, "schemaVersion") {
		t.Error("manifest response missing schemaVersion on cache hit")
	}
	if mockServer.RequestCount() != before {
		t.Error("expected no upstream request on cache hit")
	}
}

func TestDocker_DigestManifest(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/v2/library/testimg/manifests/sha256:fakedigest")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !containsStr(body, "schemaVersion") {
		t.Error("digest manifest response missing schemaVersion")
	}
}

func TestDocker_BlobFetch(t *testing.T) {
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/v2/library/testimg/blobs/sha256:fakelayer")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_DOCKER_BLOB_DATA" {
		t.Errorf("unexpected blob body: %s", body)
	}
	if mockServer.RequestCount() <= before {
		t.Error("expected upstream hit on first blob request")
	}
}

func TestDocker_BlobCacheHit(t *testing.T) {
	// Ensure cached
	httpGet(t, depsiloURL+"/v2/library/testimg/blobs/sha256:fakelayer")

	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/v2/library/testimg/blobs/sha256:fakelayer")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_DOCKER_BLOB_DATA" {
		t.Errorf("unexpected blob body on cache hit: %s", body)
	}
	if mockServer.RequestCount() != before {
		t.Error("expected no upstream request on blob cache hit")
	}
}

func TestDocker_TagList(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/v2/library/testimg/tags/list")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	var result struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("failed to parse tag list: %v", err)
	}
	if result.Name != "library/testimg" {
		t.Errorf("name = %q, want library/testimg", result.Name)
	}
	if len(result.Tags) != 2 {
		t.Errorf("tags count = %d, want 2", len(result.Tags))
	}
}

func containsStr(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Verify test helpers exist**

The `httpGet`, `assertStatus`, `readBody` functions should be defined in the integration test package already. Check `tests/integration/main_test.go` or a helper file for these. If they exist, no action needed.

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./tests/integration/`
Expected: success (build only, don't run integration tests as they need a running server)

- [ ] **Step 3: Commit**

```bash
git add tests/integration/docker_test.go
git commit -m "test(docker): add integration tests for Docker Registry proxy"
```

---

### Task 12: Final Verification

- [ ] **Step 1: Run all unit tests**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go test ./tests/unit/ -v`
Expected: all tests PASS

- [ ] **Step 2: Run full build**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./...`
Expected: success

- [ ] **Step 3: Run integration tests (if environment supports it)**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go test ./tests/integration/ -tags integration -v -timeout 120s`
Expected: all tests PASS including Docker tests

- [ ] **Step 4: Verify no regressions**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go vet ./...`
Expected: no issues
