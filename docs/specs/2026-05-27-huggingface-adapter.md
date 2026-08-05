# HuggingFace adapter — Design

**Date:** 2026-05-27
**Status:** Approved design, ready for implementation plan
**Historical source:** 2026-05-20 feature backlog (available in Git history), Tier S1
**Companion existing spec:** [docs/specs/2026-04-13-docker-registry-proxy.md](2026-04-13-docker-registry-proxy.md) — Docker Registry adapter, comparable in shape (server-side redirect following, signed-URL handling, large blob streaming).

---

## 1. Overview

Depsilo gains a 13th ecosystem adapter: **HuggingFace** (models + datasets). Public HuggingFace Hub traffic — model metadata, file listings, and the large weight files served via Git LFS — flows through `/huggingface/*` on Depsilo, caches locally, and is served at LAN speed to subsequent clients on the same network.

The HuggingFace Python tooling (`huggingface_hub`, `transformers`, `datasets`) all honor the `HF_ENDPOINT` environment variable. Setting `HF_ENDPOINT=http://depsilo-host:23333/huggingface` is the single client-side change required.

**Why this matters now:** access from mainland China to `huggingface.co` is slow and intermittent, individual model files are 1-50+ GB, and there is no lightweight self-hosted proxy in the same niche. This is Tier S1 (highest moat) in the product backlog.

## 2. Protocol

HuggingFace Hub exposes a multi-layer surface; v1 proxies the subset needed for `huggingface-cli download` and `from_pretrained()`/`load_dataset()` flows.

### 2.1 Endpoints covered (v1)

| Method | HF URL | Purpose | Cache class |
|---|---|---|---|
| GET | `/api/models/<repo_id>` | Model metadata | Short TTL |
| GET | `/api/models/<repo_id>/revision/<rev>` | Versioned metadata | Long TTL if `<rev>` is a 40-char commit SHA, short otherwise |
| GET | `/api/models/<repo_id>/tree/<rev>` | File tree listing | Same TTL rule |
| GET | `/api/datasets/<repo_id>` | Dataset metadata | Short TTL |
| GET | `/api/datasets/<repo_id>/revision/<rev>` | Versioned dataset metadata | Same TTL rule |
| GET | `/api/datasets/<repo_id>/tree/<rev>` | Dataset file tree | Same TTL rule |
| GET, HEAD | `/<repo_id>/resolve/<ref>/<*path>` | Model/dataset file download (the LFS-redirect path) | Long if `<ref>` is commit SHA, short otherwise |
| GET | `/<repo_id>/raw/<ref>/<*path>` | Small file inline content | Same TTL rule |

Datasets share the protocol with models — only the URL prefix differs (`/api/datasets/` vs `/api/models/`). Both branches share a single set of handlers.

### 2.2 Endpoints explicitly NOT supported (v1)

| Endpoint | Reason |
|---|---|
| Inference API (`/api/inference/*`) | Different product surface (real-time inference, per-token billing). Out of Depsilo's "package cache" scope. |
| Spaces (`/spaces/*`) | Application deployment, not file download. Static asset proxying would be a separate adapter category. |
| LFS batch endpoint (`POST /api/<type>/<repo>.git/info/lfs/objects/batch`) | Standalone `git lfs` clients use this; `huggingface_hub` does not. Defer to v2 if direct git-clone use case appears. |
| Repo-level write (`POST /api/repos/create`, `PUT` uploads) | Depsilo is a read-through cache, not a mirror service. |
| Server-side HF token | Depsilo does not hold HuggingFace credentials. See §4. |

## 3. Routing

URL prefix: **`/huggingface/`**. Chosen for consistency with project convention (`/rubygems/`, `/composer/` use full names rather than abbreviations).

### 3.1 Routes registered

```
GET, HEAD /huggingface/:repo_owner/:repo_name/resolve/:ref/*path
GET, HEAD /huggingface/:repo_owner/:repo_name/raw/:ref/*path
GET       /huggingface/api/models/:repo_owner/:repo_name
GET       /huggingface/api/models/:repo_owner/:repo_name/revision/:rev
GET       /huggingface/api/models/:repo_owner/:repo_name/tree/:rev
GET       /huggingface/api/datasets/:repo_owner/:repo_name
GET       /huggingface/api/datasets/:repo_owner/:repo_name/revision/:rev
GET       /huggingface/api/datasets/:repo_owner/:repo_name/tree/:rev
```

### 3.2 Single-token repo IDs

Some HuggingFace repos use single-token IDs (e.g. `bert-base-uncased` rather than `org/model`). The handler accepts both forms:

- `/huggingface/bert-base-uncased/resolve/main/config.json` → upstream `https://huggingface.co/bert-base-uncased/resolve/main/config.json`
- `/huggingface/google/flan-t5-base/resolve/main/config.json` → upstream `https://huggingface.co/google/flan-t5-base/resolve/main/config.json`

Internally the handler joins all path segments before `/resolve/` or `/raw/` and passes them through as the upstream repo path. No special-casing in route patterns required.

### 3.3 Client configuration

A single environment variable redirects every HuggingFace client to Depsilo:

```bash
export HF_ENDPOINT=http://localhost:23333/huggingface
```

After that, `huggingface-cli download`, `huggingface_hub.hf_hub_download(...)`, `transformers.AutoModel.from_pretrained(...)`, and `datasets.load_dataset(...)` all route through Depsilo with no further configuration.

## 4. Authentication

**Model: pass-through with cache opt-out.**

1. Client `Authorization: Bearer <hf_token>` header → forwarded verbatim to upstream
2. Requests that carry `Authorization` → response is **not** written to cache and does **not** participate in stale-while-revalidate
3. Requests without `Authorization` → normal cache flow

### 4.1 Why pass-through

- Zero credential management on Depsilo's side. No token rotation, no per-user provisioning.
- Users with their own HuggingFace tokens can still access gated models — they just don't benefit from caching for those models.
- Matches the standard HTTP forward-proxy pattern.

### 4.2 Why not cache authenticated responses

If User A's token unlocks a gated model and Depsilo cached the response, User B (anonymous) would receive User A's content on the next request — a real cross-user leak. The simplest correct rule: any request bearing `Authorization` skips the cache write path entirely, and a cache lookup for the same path returns "miss" so the request goes to upstream.

### 4.3 Implementation

In `headers.go`:

```go
func authBypass(c *gin.Context) bool {
    return c.GetHeader("Authorization") != ""
}
```

`handler.go` checks `authBypass(c)` before invoking `cache.Manager.Get(...)` / `Put(...)`. When bypass is active, the resolver streams the upstream response directly without touching the cache.

## 5. Configuration

### 5.1 New config block

`internal/config/config.go` gains:

```go
type HuggingFaceConfig struct {
    Upstreams []UpstreamConfig `mapstructure:"upstreams"`
}

type Config struct {
    // ... existing 12 ecosystem configs ...
    HuggingFace HuggingFaceConfig `mapstructure:"huggingface"`
}
```

### 5.2 `config.example.toml` additions

```toml
[[huggingface.upstreams]]
name     = "hf-mirror"
url      = "https://hf-mirror.com"
priority = 1

[[huggingface.upstreams]]
name     = "official"
url      = "https://huggingface.co"
priority = 2
# proxy = "http://127.0.0.1:7890"   # optional per-upstream HTTP proxy
```

Mirrors the schema used by the 12 existing adapters. The `LatencySelector` will probe both and pick the faster path automatically.

### 5.3 Storage / quota

No per-ecosystem quota. HuggingFace blobs share the global LRU pool with all other ecosystems. Operators deploying primarily for HuggingFace workloads should increase `cache.max_size_gb` from the default 20 GB — the README's "Use with AI workloads" subsection (added in this work) calls this out.

### 5.4 README annotation

Add a small note under the §11 Quick Start block: "If you primarily use Depsilo for HuggingFace model caching, raise `[cache] max_size_gb` — a single large model can be 30-50 GB."

## 6. Cache key + TTL convention

### 6.1 Key construction

| Request path | Cache key |
|---|---|
| `/huggingface/<repo>/resolve/<ref>/<path>` | `huggingface/<repo>/resolve/<ref>/<path>` |
| `/huggingface/<repo>/raw/<ref>/<path>` | `huggingface/<repo>/raw/<ref>/<path>` |
| `/huggingface/api/models/<repo>` | `huggingface/api/models/<repo>` |
| `/huggingface/api/models/<repo>/revision/<rev>` | `huggingface/api/models/<repo>/revision/<rev>` |
| `/huggingface/api/models/<repo>/tree/<rev>` | `huggingface/api/models/<repo>/tree/<rev>` |
| (same for datasets) | (same) |
| any request with `Authorization` header | **no key — bypasses cache entirely** |

### 6.2 TTL classification

Distinguishing immutable vs mutable refs:

```go
var commitSHAPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

func ttlFor(ref string) time.Duration {
    if commitSHAPattern.MatchString(ref) {
        return 72 * time.Hour // immutable commit
    }
    return 5 * time.Minute   // branch, tag, or "main"
}
```

| Ref pattern | TTL | Reasoning |
|---|---|---|
| 40-char hex commit SHA | 72h | Commits are content-addressed and immutable |
| Branch name (e.g. `main`), tag, any other ref | 5m | May move at any time; stale-while-revalidate serves quickly while refreshing in the background |
| API metadata routes (`/api/models/<repo>`) | 5m | The repo's "latest" view; cheap to revalidate |

The stale-while-revalidate behavior (already in `cache.Manager`) means a 5m TTL doesn't translate to a 5m user-visible latency penalty — expired entries serve immediately while the refresh happens in the background.

## 7. Backend implementation

### 7.1 File layout

```
internal/adapter/huggingface/
├── handler.go        # gin.RouterGroup registration; method dispatch
├── resolver.go       # Server-side 302-following for resolve/raw paths
├── keyer.go          # Cache key + TTL by URL pattern
├── headers.go        # Auth pass-through + bypass flag + header passthrough
└── handler_test.go   # Unit tests for keyer, headers, resolver
```

Total expected: ~400-500 LOC across the 5 files.

### 7.2 `Register()` shape

```go
type Adapter struct {
    pool    *upstream.Pool
    cacheMgr *cache.Manager
}

func New(pool *upstream.Pool, cacheMgr *cache.Manager) *Adapter {
    return &Adapter{pool: pool, cacheMgr: cacheMgr}
}

func (a *Adapter) Type() string { return "huggingface" }

func (a *Adapter) Register(rg *gin.RouterGroup) {
    g := rg.Group("/huggingface")

    // File downloads — the hot path
    g.GET("/*resolve_path", a.handleAny)    // catches resolve/raw + api/*
    g.HEAD("/*resolve_path", a.handleHead)
}
```

A single catch-all route + internal path dispatch keeps the handler simple. Path parsing in `handleAny` decides which sub-handler runs:

- `path == "api/models/.../tree/..."` → tree handler
- `path == ".../resolve/.../..."` → resolve handler (server-side following)
- `path == ".../raw/.../..."` → raw handler
- `path == "api/models/..."` or `api/datasets/...` → metadata handler

### 7.3 Server-side following (the resolver)

The core of the adapter. Pseudocode:

```go
func (a *Adapter) handleResolve(c *gin.Context, cachedKey string, ttl time.Duration) {
    bypass := authBypass(c)

    // Cache lookup unless auth bypass
    if !bypass {
        if hit := a.cacheMgr.Get(c.Request.Context(), cachedKey); hit != nil {
            streamCached(c, hit)
            return
        }
    }

    // Build upstream request
    up := a.pool.Select(c.Request.Context())
    upReq, _ := http.NewRequestWithContext(c.Request.Context(), c.Request.Method,
        up.URL+c.Request.URL.Path, nil)
    if auth := c.GetHeader("Authorization"); auth != "" {
        upReq.Header.Set("Authorization", auth)
    }
    upReq.Header.Set("User-Agent", c.GetHeader("User-Agent"))

    // We need to handle the 302 manually, so use a non-following client
    nonFollowing := &http.Client{
        CheckRedirect: func(*http.Request, []*http.Request) error {
            return http.ErrUseLastResponse
        },
        Timeout: 5 * time.Minute,
    }

    upResp, err := nonFollowing.Do(upReq)
    if err != nil {
        writeUpstreamError(c, err)
        return
    }
    defer upResp.Body.Close()

    // Pass through metadata headers that huggingface_hub relies on
    passHFHeaders(c, upResp)

    switch upResp.StatusCode {
    case http.StatusOK:
        // Non-LFS small file — body is the content
        streamAndCache(c, upResp.Body, cachedKey, ttl, bypass)
    case http.StatusFound, http.StatusMovedPermanently, http.StatusTemporaryRedirect:
        // LFS path — Location points to signed CDN URL
        signed := upResp.Header.Get("Location")
        innerReq, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, signed, nil)
        // Signed URL carries auth in the query string — do not forward Authorization
        innerClient := &http.Client{Timeout: 30 * time.Minute}
        innerResp, err := innerClient.Do(innerReq)
        if err != nil {
            writeUpstreamError(c, err)
            return
        }
        defer innerResp.Body.Close()

        c.Status(innerResp.StatusCode)
        copyContentHeaders(c, innerResp)
        streamAndCache(c, innerResp.Body, cachedKey, ttl, bypass)
    default:
        // 404, 401, 5xx — pass through status + body without caching
        c.Status(upResp.StatusCode)
        io.Copy(c.Writer, upResp.Body)
    }
}
```

### 7.4 Headers to pass through

`huggingface_hub` relies on these in HEAD/GET responses for integrity verification:

| Header | Purpose |
|---|---|
| `X-Linked-Etag` | SHA256 of the actual blob; client compares against downloaded bytes |
| `X-Repo-Commit` | Commit SHA the resolve was pinned to |
| `X-Linked-Size` | Expected byte count |
| `ETag` | Standard HTTP ETag (often present alongside X-Linked-Etag) |
| `Content-Length` | Same |
| `Content-Type` | Same |
| `Accept-Ranges` | Honored for resume support |

`passHFHeaders(c, upResp)` and `copyContentHeaders(c, innerResp)` together transfer this set. `Authorization` is **not** forwarded to the CDN inner GET — the signature is in the URL.

### 7.5 HEAD requests

`huggingface_hub` issues `HEAD` first to fetch the etag without downloading the body. The same `handleResolve` runs but with `c.Method == "HEAD"`. The 302 from upstream is also a HEAD (or some implementations 302 on HEAD too). For HEAD, we just emit headers + status without body, never invoke `streamAndCache`. Cache HEAD-only entries are not stored; only the GET-side flow populates the cache.

### 7.6 Range requests

`huggingface_hub` and `huggingface-cli` use range requests to resume partial downloads. Pass-through `Range` request header to upstream; pass-through `Content-Range` and `206` status back. **Range responses are not cached in v1** — only the full-body 200 response goes to the cache. Defer range-caching to v2.

## 8. Frontend integration

### 8.1 EcosystemIcon

`web/src/components/EcosystemIcon.tsx` — add a `huggingface` case. Recommended: HuggingFace 🤗 emoji-shaped SVG glyph (HF's own brand mark is recognizable; a clean monochrome version fits Depsilo's icon set style). Alternatively use the literal 🤗 emoji rendered at glyph size — simplest, works in every browser.

### 8.2 LANGUAGES entry

`web/src/lib/ecosystemData.ts` — append:

```typescript
{
  id: 'huggingface',
  name: 'Hugging Face',
  glyph: 'HF',
  iconAdapter: 'huggingface',
  managers: [
    {
      id: 'huggingface-cli',
      name: 'huggingface-cli',
      hint: 'Official CLI from HuggingFace Hub',
      // setup block: export HF_ENDPOINT=<base>/huggingface
    },
    {
      id: 'transformers',
      name: 'transformers',
      hint: 'Python: AutoModel.from_pretrained()',
      // same env var — same setup
    },
  ],
},
```

The setup snippet for both managers is the same single line: `export HF_ENDPOINT=<base>/huggingface`. Setting that env var redirects every HuggingFace tool in the user's Python environment automatically.

### 8.3 i18n keys

New keys under `quickstart.languages.huggingface.*` and `quickstart.managers.huggingface-cli.*` / `transformers.*` — about 8 new keys × 2 locales (parity-locked via `make lint-i18n`).

### 8.4 `buildAgentPrompt` extension

The MEDIUM agent prompt (added in 2026-05-26) — append one more line in the per-ecosystem checklist:

```
huggingface: export HF_ENDPOINT=<URL>/huggingface
```

That's the single mechanical change to the prompt; the model will already auto-route `pip install huggingface_hub && huggingface-cli download ...` through Depsilo once that env var is set.

### 8.5 Other pages

`Dashboard.tsx`, `BandwidthReport.tsx`, etc. already render per-ecosystem rows by iterating `deps.Ecosystems`. Once the backend registers "huggingface" in that list, dashboards pick it up automatically. No frontend touchpoints beyond §8.1–§8.4.

## 9. Testing strategy

### 9.1 Unit tests (`internal/adapter/huggingface/handler_test.go`)

```
TestKey_CommitSHA_LongTTL              ref=40-hex → 72h
TestKey_Branch_ShortTTL                ref=main, tag → 5m
TestKey_APIMetadata                    /api/models/X → 5m
TestKey_APIWithRevisionSHA             /api/models/X/revision/<sha> → 72h
TestAuthBypass_HasAuthorization        Authorization header present → bypass=true
TestAuthBypass_NoAuthorization         no header → bypass=false
TestResolver_DirectContent_200         upstream 200 → body streamed verbatim
TestResolver_LFSRedirect_FollowsToCDN  upstream 302 → inner GET to Location → body streamed
TestResolver_LFSRedirect_NoAuthOnInner Authorization not forwarded to CDN inner GET
TestResolver_NonGetMethod_HEAD         HEAD → headers only, no cache write
TestResolver_404PassThrough            upstream 404 → 404 to client, not cached
TestPathParse_OrgSlashName             /org/name/resolve/main/file → repo=org/name
TestPathParse_SingleToken              /bert-base/resolve/main/file → repo=bert-base
```

### 9.2 Integration tests (`tests/integration/huggingface_test.go`)

Uses the existing `tests/mock` upstream harness (registered via `mock.MockUpstream.RegisterAll`). Add a `huggingface` mock handler that simulates:

- `/api/models/<repo>` returns JSON metadata
- `/<repo>/resolve/<rev>/<path>` returns 302 with Location to a separate mock CDN
- The mock CDN serves canned binary content with the right `Content-Length` and `X-Linked-Etag`

```
TestHF_ModelMetadata                   GET /huggingface/api/models/X → mocked JSON
TestHF_ModelTreeListing                GET /huggingface/api/models/X/tree/main → mocked listing
TestHF_DatasetMetadata                 GET /huggingface/api/datasets/X → mocked JSON
TestHF_ResolveLFS                      GET /huggingface/X/resolve/main/weight.bin → bytes streamed via 302→CDN
TestHF_ResolveSmallFile                GET /huggingface/X/resolve/main/config.json → direct 200, no redirect
TestHF_AuthForwarded                   request with Authorization → upstream receives Authorization
TestHF_AuthNotCached                   second auth'd request hits upstream again (cache bypassed)
TestHF_PublicCached                    second anonymous request hits cache (no upstream)
TestHF_HEAD_NoBody                     HEAD returns same headers as GET but body length 0
TestHF_CommitSHA_LongTTL               request with SHA ref doesn't refetch within 72h
```

### 9.3 Docker E2E (`testground/docker-huggingface/Dockerfile`)

Use a **tiny** real model to keep CI under 60s. `prajjwal1/bert-tiny` (~17MB) is ideal — same protocol surface as any large model, vastly smaller download. The Dockerfile:

```dockerfile
FROM python:3.11-alpine
RUN pip install --no-cache-dir huggingface_hub
ENV HF_ENDPOINT=http://172.17.0.1:23333/huggingface
RUN huggingface-cli download prajjwal1/bert-tiny --local-dir /tmp/m \
    && ls -la /tmp/m/pytorch_model.bin \
    && python -c "import json; meta = json.load(open('/tmp/m/config.json')); assert meta['model_type'] == 'bert'"
CMD ["echo", "depsilo huggingface E2E passed"]
```

Wired into `make test-docker-huggingface` following the existing per-ecosystem pattern. Added to `make test-docker-all`'s sequential run.

## 10. Scope boundaries

### 10.1 v1 in-scope

- Models + datasets metadata (`/api/models/...`, `/api/datasets/...`)
- File downloads via `/<repo>/resolve/<ref>/<path>` (GET + HEAD, LFS-aware)
- Raw small-file content via `/<repo>/raw/<ref>/<path>`
- Public repo full functionality with caching
- Authenticated requests pass through to upstream (gated models work for users with their own tokens; auth'd responses are not cached)
- Two upstreams default (`hf-mirror.com` priority 1 + `huggingface.co` priority 2) with LatencySelector
- Frontend: QuickStart "Hugging Face" tab + Agent prompt addition
- 13 unit tests + 10 integration tests + 1 Docker E2E

### 10.2 v1 explicitly out-of-scope

| Feature | Reason for deferral |
|---|---|
| HuggingFace Spaces | Application deployment, not file caching |
| Inference API | Per-token billing, different product surface |
| LFS batch endpoint (`POST .../lfs/objects/batch`) | Used by raw `git lfs`; not used by mainstream HuggingFace clients |
| Server-side HF token configuration | Operational complexity for one-tenant token sharing; revisit if gated-model UX becomes a priority |
| SHA256-based content deduplication | Operational data-driven decision; revisit if cross-repo blob duplication exceeds 20% of storage |
| Per-ecosystem quota | The global LRU is sufficient at MVP; revisit if a user reports HuggingFace blobs starving other ecosystems |
| Range request caching | Pass-through works; caching partial blobs is a meaningful caching-layer expansion |
| Repository write/upload endpoints | Depsilo is a read-through cache, not a mirror service |

### 10.3 Risks called out

- **HF protocol drift**: HuggingFace changes the resolve URL response format roughly every 6-12 months (different header names, different redirect target hostnames). The adapter's headers passthrough is the most likely break point; integration tests against the mock harness give us a contract to update against.
- **Large blobs**: A single model can be 50 GB. The cache.Manager streaming path is already verified for ~2GB torch wheels and will scale linearly. The risk is operational: a single new model can fill an undersized cache. The default `max_size_gb=20` was chosen for general use; HF-focused deployments should bump this, and the README will say so.
- **hf-mirror.com availability**: The Chinese mirror is community-operated, not a HuggingFace product. It can lag behind canonical hf.co content. The LatencySelector + per-upstream health checks naturally route around outages; users wanting only the official source can comment out the hf-mirror entry in their `config.toml`.

## 11. Implementation note: deviation reservation

This spec leaves one item explicitly open for the implementation plan to settle, since it depends on small details discovered during coding:

- **Exact `Range` request behavior**: spec §7.6 says pass-through (no caching of partial responses). If during implementation we discover that `huggingface_hub`'s default download path uses range requests for the WHOLE file (not just resume), v1 caching might miss in practice. The plan should include a small spike: confirm with the integration mock whether range or full-body GET is the default.

If range turns out to be default, two recovery paths:
1. Treat range requests as "request the full body, serve the requested range from cache" — common proxy pattern. Adds ~30 LOC.
2. Strip the `Range` header on the way to upstream when there's no `Content-Range` partial response yet — forces a full download for the first miss, then range requests hit the cache. Adds ~5 LOC.

Either is fine; the plan should pick one based on what the mock test demonstrates.
