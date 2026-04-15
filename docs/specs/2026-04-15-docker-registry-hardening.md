# Docker Registry Proxy — Quality Hardening & Feature Completion

## Overview

Improve the existing Docker Registry proxy adapter with test coverage, operational logging, configuration validation, auth deduplication, and Prometheus metrics integration. No architectural changes — only additions and fixes to the existing implementation.

## 1. Unit Tests

**File**: `tests/unit/docker_test.go`

### 1.1 Cache Key Tests

Test all three key functions from `internal/adapter/docker/keyer.go`:

```text
ManifestCacheKey("dockerhub", "library/nginx", "latest")
  → "docker/dockerhub/manifests/library/nginx/latest"

ManifestCacheKey("dockerhub", "library/nginx", "sha256:abc123")
  → "docker/dockerhub/manifests/library/nginx/sha256__abc123"

BlobCacheKey("dockerhub", "sha256:abc123")
  → "docker/dockerhub/blobs/sha256__abc123"

TagListCacheKey("dockerhub", "library/nginx")
  → "docker/dockerhub/tags/library/nginx/list"
```

Add Docker keys to the existing `TestCacheKey_NoCollision` test.

**Filesystem safety**: Docker digest keys contain `:` (e.g. `sha256:abc123`), which the existing `TestCacheKey_ValidFilesystemPath` test flags as illegal. Since Depsilo targets Linux (where `:` in filenames is valid) and S3 (where `:` in keys is valid), the fix is:
1. Update the keyer to encode `:` as `__` in cache keys (e.g. `sha256__abc123`) for filesystem safety.
2. Add Docker keys to `TestCacheKey_ValidFilesystemPath` to verify they pass.
3. The `Docker-Content-Digest` response header still uses the original `sha256:abc123` format — only the cache key is encoded.

### 1.2 Resolver Tests

**File**: `tests/unit/docker_resolver_test.go`

Test `Resolver.Resolve(path)` with a resolver configured with:
- Registry "dockerhub" at `https://registry-1.docker.io`
- Registry "ghcr" at `https://ghcr.io`
- Default: "dockerhub"

| Input path | Expected registry | Expected imageName | Expected endpoint |
|---|---|---|---|
| `library/nginx/manifests/latest` | dockerhub | `library/nginx` | `manifests/latest` |
| `ghcr.io/owner/repo/manifests/v1.0` | ghcr | `owner/repo` | `manifests/v1.0` |
| `myimage/blobs/sha256:abc` | dockerhub (default) | `myimage` | `blobs/sha256:abc` |

| `team/service/api/tags/list` | dockerhub (default) | `team/service/api` | `tags/list` |
| `docker.io/library/alpine/manifests/3.18` | dockerhub (alias) | `library/alpine` | `manifests/3.18` |
| `unknown.registry.io/img/manifests/v1` | dockerhub (fallback) | `unknown.registry.io/img` | `manifests/v1` |
| `short` | nil (too few parts) | `""` | `""` |
| `a/b` | nil (no endpoint keyword) | `""` | `""` |

### 1.3 Auth Parser Tests

Test `parseAuthParam` with:

```text
Input:  `Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`
  parseAuthParam(input, "realm")   → "https://auth.docker.io/token"
  parseAuthParam(input, "service") → "registry.docker.io"
  parseAuthParam(input, "scope")   → "repository:library/nginx:pull"
  parseAuthParam(input, "missing") → ""
```

### 1.4 Package Name Extraction

Add Docker cases to `tests/unit/extract_package_name_test.go`:

| cacheKey | Expected |
|---|---|
| `docker/dockerhub/manifests/library/nginx/latest` | `library/nginx` |
| `docker/ghcr/manifests/owner/repo/sha256__abc` | `owner/repo` |
| `docker/dockerhub/manifests/team/service/api/v1.0` | `team/service/api` |
| `docker/dockerhub/blobs/sha256__abc` | `""` |
| `docker/dockerhub/tags/library/nginx/list` | (currently not handled — see section 7) |

## 2. Integration Tests

**File**: `tests/integration/docker_test.go`

### 2.1 Mock Server Extension

**File**: `tests/mock/upstream_server.go`

Add `RegisterDocker()` method. The mock simulates a Docker registry that requires Bearer token auth:

```text
GET /v2/                              → 401 + WWW-Authenticate header pointing to mock token endpoint
GET /v2/library/testimg/manifests/latest → manifest JSON (requires Bearer token)
GET /v2/library/testimg/manifests/sha256:fakedigest → manifest JSON (requires Bearer token)
GET /v2/library/testimg/blobs/sha256:fakelayer → blob data (requires Bearer token)
GET /v2/library/testimg/tags/list     → {"name":"library/testimg","tags":["latest","v1.0"]}
GET /auth/token?service=...&scope=... → {"token":"mock-token","expires_in":300}
```

The mock token endpoint accepts any credentials and returns a fixed token. The registry endpoints validate that `Authorization: Bearer mock-token` is present.

Add `RegisterDocker()` to `RegisterAll()`.

### 2.2 Test Config Extension

**File**: `tests/integration/main_test.go`

Add Docker config section to `writeTestConfig`:

```toml
[docker]
default_registry = "mock"

[[docker.registries]]
name = "mock"
url = "<mockServerURL>"
```

### 2.3 Test Cases

```text
TestDocker_VersionCheck        — GET /v2/ returns 200 with {}
TestDocker_ManifestFetch       — GET /v2/library/testimg/manifests/latest returns manifest, upstream hit
TestDocker_ManifestCacheHit    — Second GET returns same manifest, no upstream hit
TestDocker_DigestManifest      — GET /v2/library/testimg/manifests/sha256:fakedigest returns manifest
TestDocker_BlobFetch           — GET /v2/library/testimg/blobs/sha256:fakelayer returns blob data
TestDocker_BlobCacheHit        — Second GET returns same blob, no upstream hit
TestDocker_TagList             — GET /v2/library/testimg/tags/list returns tag list JSON
```

## 3. Configuration Validation

**File**: `internal/adapter/docker/resolver.go`

### 3.1 Startup Warning for Invalid default_registry

In `NewResolver`, after the default registry resolution block (lines 74-92), if `cfg.DefaultRegistry` was specified but couldn't be resolved to any configured registry, log a warning:

```go
if cfg.DefaultRegistry != "" && r.defaultReg == "" {
    zap.L().Warn("docker: configured default_registry not found, no default set",
        zap.String("default_registry", cfg.DefaultRegistry),
    )
} else if cfg.DefaultRegistry != "" && r.defaultReg != cfg.DefaultRegistry {
    // Was resolved by domain alias or fell back to first registry
    zap.L().Info("docker: default_registry resolved",
        zap.String("configured", cfg.DefaultRegistry),
        zap.String("resolved_to", r.defaultReg),
    )
}
```

Note: The current code already falls back to `cfg.Registries[0].Name` when `default_registry` doesn't match — keep this behavior, just add the warning.

### 3.2 Unregistered Domain Logging

In `Resolver.Resolve()`, when `firstPart` contains `.` or `:` but is not found in `domainMap`, log an info message:

```go
if strings.ContainsAny(firstPart, ".:") {
    regName, ok := r.domainMap[firstPart]
    if ok {
        // ... existing code ...
    } else {
        zap.L().Info("docker: unregistered domain in path, using default registry",
            zap.String("domain", firstPart),
            zap.String("default", r.defaultReg),
        )
    }
}
```

## 4. Invalid Path Access Logging

**File**: `internal/adapter/docker/handler.go`

### 4.1 Log 404 for Invalid Paths

In `handleRequest`, when returning 404 for invalid paths (line 64) and unsupported endpoints (line 75), add access log:

```go
// Invalid path → 404
if reg == nil || imageName == "" || endpoint == "" {
    c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "invalid path"})
    adapter.LogAccess(h.db, "docker", c.Request.Method, "docker/unknown/"+path, false, "", time.Since(start), http.StatusNotFound, c.ClientIP(), 0)
    return
}
```

Same pattern for the "unsupported endpoint" branch.

### 4.2 Log Upstream Errors

When upstream returns error (502), also log access with the error status:

```go
// In handleManifest, handleBlob, handleTagList — after the error JSON response
adapter.LogAccess(h.db, "docker", c.Request.Method, cacheKey, false, reg.Name, time.Since(start), http.StatusBadGateway, c.ClientIP(), 0)
```

## 5. Singleflight for Token Acquisition

**File**: `internal/adapter/docker/auth.go`

### Current Problem

Between `RUnlock` and `Lock`, two concurrent requests to the same scope can both see an expired token and both issue token requests. This is harmless but wasteful.

### Fix

Add `singleflight.Group` to `AuthManager`:

```go
import "golang.org/x/sync/singleflight"

type AuthManager struct {
    mu     sync.RWMutex
    tokens map[string]*tokenEntry
    sf     singleflight.Group
}
```

Wrap the token fetch in `GetToken`:

```go
func (a *AuthManager) GetToken(...) (string, error) {
    cacheKey := registryName + ":" + scope

    // Fast path: check cache
    a.mu.RLock()
    if entry, ok := a.tokens[cacheKey]; ok && time.Now().Before(entry.ExpiresAt) {
        a.mu.RUnlock()
        return entry.Token, nil
    }
    a.mu.RUnlock()

    // Deduplicated fetch
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

## 6. Prometheus Metrics

**File**: `internal/adapter/docker/handler.go`

### Current State

`internal/api/metrics.go` defines global metrics (`depsilo_requests_total`, `depsilo_request_duration_seconds`, `depsilo_upstream_requests_total`) with `adapter_type` and other labels, but **no adapter currently calls them**. The Docker adapter will be the first to integrate.

### Implementation

Add metrics calls to `streamResponse` and error paths:

```go
import "depsilo/internal/api"

func (h *Handler) streamResponse(c *gin.Context, result *cache.GetResult, cacheKey string, start time.Time) {
    // ... existing streaming code ...

    // Metrics
    hitStr := "false"
    if result.Hit {
        hitStr = "true"
    }
    api.M.RequestsTotal.WithLabelValues("docker", hitStr).Inc()
    api.M.RequestDuration.WithLabelValues("docker").Observe(time.Since(start).Seconds())

    adapter.LogAccess(...)
}
```

For upstream fetches (cache miss), in `fetchFromUpstream`:

```go
// After successful upstream response
api.M.UpstreamRequestsTotal.WithLabelValues(reg.Name, "true").Inc()

// After failed upstream response
api.M.UpstreamRequestsTotal.WithLabelValues(reg.Name, "false").Inc()
```

For HEAD requests in `handleHead`:

```go
api.M.RequestsTotal.WithLabelValues("docker", "false").Inc()
api.M.RequestDuration.WithLabelValues("docker").Observe(time.Since(start).Seconds())
```

### Note on Other Adapters

This spec only adds metrics to the Docker adapter. Other adapters currently don't report metrics — adding metrics to all adapters is out of scope for this task but could be a follow-up.

## 7. Package Name Extraction Fix

**File**: `internal/adapter/accesslog.go`

The `extractPackageName` function's Docker case doesn't handle tag list keys (`docker/{registry}/tags/{image}/list`). Add handling:

```go
case "docker":
    if strings.Contains(key, "/manifests/") {
        // existing code...
    }
    if strings.Contains(key, "/tags/") {
        parts := strings.SplitN(key, "/tags/", 2)
        if len(parts) == 2 {
            image := strings.TrimSuffix(parts[1], "/list")
            return image
        }
    }
    if strings.Contains(key, "/blobs/") {
        return ""
    }
```

## Scope Boundaries

- No architectural changes to the Docker adapter
- No changes to the Resolver's fallback behavior (unregistered domains still fall back to default)
- Prometheus metrics only added to Docker adapter, not other adapters
- No new Prometheus metric definitions — only use existing `api.M.*`
- No TLS/insecure registry config options
- No changes to frontend
