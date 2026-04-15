# Docker Registry Proxy

## Overview

Add Docker Registry V2 pull-through cache to Depsilo. Supports multiple registries (Docker Hub, ghcr.io, quay.io, private Harbor, etc.) via a single `/v2/` endpoint. Depsilo handles upstream authentication transparently — clients don't need to login to upstream registries.

## Protocol

Docker Registry HTTP API V2 ([spec](https://docs.docker.com/registry/spec/api/)). Only read (pull) operations are supported.

### Endpoints

| Method | Path | Purpose | Cache |
|--------|------|---------|-------|
| `GET` | `/v2/` | Version check, return `{}` | No cache |
| `HEAD/GET` | `/v2/{name}/manifests/{reference}` | Image manifest | Tag ref: short TTL (ttl_index); Digest ref: long TTL (ttl_blob, immutable) |
| `GET` | `/v2/{name}/blobs/{digest}` | Layer blob | Long TTL (ttl_blob, content-addressable, immutable) |
| `GET` | `/v2/{name}/tags/list` | Tag listing | Short TTL (ttl_index) |

### Not Supported

- `POST/PUT/PATCH/DELETE` — no push, no delete
- `GET /v2/_catalog` — not proxied
- Docker Content Trust / Notary — not handled
- Docker Hub search API — not proxied

## Routing

Mounted at `/v2/`. Default registry + domain-prefix routing for others.

### Resolution Logic

Given request path `/v2/{first}/{rest...}`:

1. If `first` contains a `.` (dot) or `:` (colon), treat it as a registry domain. Look up configured registry by matching domain. Strip it from the image name and route to that registry.
2. Otherwise, route to the default registry. The full path is the image name.

### Examples

```
GET /v2/library/nginx/manifests/latest
  → default registry (docker.io) → library/nginx:latest

GET /v2/ghcr.io/user/repo/manifests/v1.0
  → ghcr.io → user/repo:v1.0

GET /v2/quay.io/org/app/blobs/sha256:abc123
  → quay.io → org/app blob sha256:abc123

GET /v2/myregistry.company.com:5000/team/service/manifests/latest
  → myregistry.company.com:5000 → team/service:latest
```

### Docker Hub Special Case

Docker Hub's actual API endpoint is `registry-1.docker.io`, but clients reference it as `docker.io`. Short image names like `nginx` are expanded to `library/nginx` by the Docker daemon before reaching the registry. Depsilo does NOT need to handle this expansion — the Docker daemon does it.

## Authentication

Depsilo authenticates to upstream registries on behalf of clients. Clients do not need `docker login`.

### Token Flow

1. On first request to a registry, Depsilo sends `GET /v2/` to the upstream.
2. If upstream returns `401` with `WWW-Authenticate: Bearer realm="...",service="...",scope="..."`, Depsilo extracts the token endpoint.
3. Depsilo requests a bearer token from that endpoint, optionally with configured credentials (HTTP Basic Auth).
4. Token is cached in memory with its expiry time. Refreshed automatically when expired.

### Per-Registry Credentials

```toml
[[docker.registries]]
name     = "dockerhub"
url      = "https://registry-1.docker.io"
username = ""       # empty = anonymous (public images only)
password = ""

[[docker.registries]]
name     = "ghcr"
url      = "https://ghcr.io"
username = "github-user"
password = "ghp_xxxxxxxxxxxx"   # GitHub PAT with read:packages
```

### Token Caching

- Tokens cached in a `sync.Map` keyed by `{registry}:{scope}`.
- Each entry stores token string + expiry time.
- On request, if cached token is expired or absent, fetch a new one.
- No persistence — tokens are re-fetched on restart.

## Configuration

### New Config Structure

```go
type DockerConfig struct {
    DefaultRegistry string           `mapstructure:"default_registry"`
    Registries      []RegistryConfig `mapstructure:"registries"`
}

type RegistryConfig struct {
    Name     string `mapstructure:"name"`
    URL      string `mapstructure:"url"`      // e.g. https://registry-1.docker.io
    Username string `mapstructure:"username"`
    Password string `mapstructure:"password"`
    Proxy    string `mapstructure:"proxy"`    // optional HTTP proxy
}
```

This differs from other adapters' `AdapterConfig` because Docker needs per-registry credentials rather than priority-based upstream selection.

### config.example.toml

```toml
[docker]
default_registry = "docker.io"

[[docker.registries]]
name     = "dockerhub"
url      = "https://registry-1.docker.io"
# username = ""
# password = ""

# [[docker.registries]]
# name     = "ghcr"
# url      = "https://ghcr.io"
# username = "github-user"
# password = "ghp_xxx"
```

## Cache Key Convention

```
docker/{registry-name}/manifests/{image-name}/{reference}
docker/{registry-name}/blobs/{digest}
docker/{registry-name}/tags/{image-name}/list
```

Examples:
```
docker/dockerhub/manifests/library/nginx/latest
docker/dockerhub/manifests/library/nginx/sha256:abc123
docker/dockerhub/blobs/sha256:def456
docker/ghcr/manifests/user/repo/v1.0
docker/ghcr/blobs/sha256:789xyz
```

Per-registry isolation — no cross-registry blob dedup.

## Backend Implementation

### Files

```
internal/adapter/docker/
├── handler.go     # Handler struct, Register(), request routing
├── keyer.go       # CacheKey functions
├── auth.go        # Token fetching and caching
└── resolver.go    # Registry resolution (domain → config lookup)
```

### Handler

```go
type Handler struct {
    cacheMgr   *cache.Manager
    cfg        config.CacheConfig
    db         *gorm.DB
    registries map[string]*Registry   // name → Registry
    domainMap  map[string]string      // domain → registry name
    defaultReg string                 // default registry name
}

type Registry struct {
    Name     string
    URL      string
    Username string
    Password string
    Client   *http.Client            // with optional proxy
    tokens   sync.Map                // scope → tokenEntry
}
```

### Register()

```go
func (h *Handler) Register(rg *gin.RouterGroup) {
    rg.GET("/", h.handleVersionCheck)
    rg.HEAD("/*path", h.handleRequest)
    rg.GET("/*path", h.handleRequest)
}
```

### Request Flow

```
Client: GET /v2/ghcr.io/user/repo/manifests/v1.0

1. Parse path → registry=ghcr, imageName=user/repo, endpoint=manifests/v1.0
2. Determine cache key → docker/ghcr/manifests/user/repo/v1.0
3. Determine TTL → reference is tag "v1.0" → ttl_index
4. cacheMgr.Get(key, "docker", ttl, fetchFn)
   └─ fetchFn:
      a. Get bearer token for scope "repository:user/repo:pull"
      b. GET https://ghcr.io/v2/user/repo/manifests/v1.0
         with Authorization: Bearer <token>
         with Accept: application/vnd.docker.distribution.manifest.v2+json, ...
      c. Return body + content-type + size
5. Stream response to client
6. Log access
```

### Manifest Accept Headers

When fetching manifests from upstream, Depsilo must send proper Accept headers:

```
Accept: application/vnd.docker.distribution.manifest.v2+json,
        application/vnd.docker.distribution.manifest.list.v2+json,
        application/vnd.oci.image.manifest.v1+json,
        application/vnd.oci.image.index.v1+json
```

### HEAD Requests

Manifests support HEAD requests (used by `docker pull` to check digest before downloading). Depsilo handles HEAD by:
- If cache hit: return headers (Content-Type, Docker-Content-Digest, Content-Length) without body
- If cache miss: forward HEAD to upstream, return headers, do NOT cache (caching happens on subsequent GET)

### Response Headers

Depsilo must preserve these headers from upstream:
- `Docker-Content-Digest` — the digest of the manifest
- `Content-Type` — the media type of the manifest/blob
- `Content-Length` — size

## Frontend

### QuickStart Tab

Add "Docker" tab to the QuickStart page with configuration instructions:

**Mirror mode (daemon.json):**
```json
{
  "registry-mirrors": ["http://HOST:PORT"]
}
```

**Direct pull:**
```bash
docker pull HOST:PORT/nginx:latest
docker pull HOST:PORT/ghcr.io/user/repo:tag
```

### EcosystemIcon

Add `docker` to EcosystemIcon component using `siDocker` from simple-icons.

### Other Pages

- AccessLogs, AuditLogs, CacheManage, BandwidthReport — automatically work via `adapter_type = "docker"`
- Upstreams page — add 'docker' to ECOSYSTEMS filter list for display, but Docker registries are managed via config (not the upstream CRUD API)

### i18n Keys

```
quickstart.dockerLabel: "Docker" / "Docker"
quickstart.dockerDesc: "Container images" / "容器镜像"
quickstart.dockerMirror: "Mirror Mode" / "镜像模式"
quickstart.dockerMirrorDesc: "Add to /etc/docker/daemon.json:" / "添加到 /etc/docker/daemon.json："
quickstart.dockerDirect: "Direct Pull" / "直接拉取"
quickstart.dockerDirectDesc: "Pull images through the proxy:" / "通过代理拉取镜像："
quickstart.dockerRestart: "Restart Docker to apply:" / "重启 Docker 生效："
```

## Scope Boundaries

- Pull-through cache only, no push
- No Docker Hub search
- No cross-registry blob dedup
- No image signing / notary
- No automatic Docker Hub `library/` prefix expansion (Docker daemon handles this)
- No rate limit bypass (Docker Hub rate limits apply to Depsilo's IP)
- Registries are configured via TOML, not via admin UI CRUD
