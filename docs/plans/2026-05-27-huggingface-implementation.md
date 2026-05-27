# HuggingFace adapter — Implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the 13th ecosystem adapter — HuggingFace Hub models + datasets — proxied through Depsilo at `/huggingface/*` with server-side 302 following, path-based caching, and a pass-through auth model.

**Architecture:** Approach 1 from the spec — Depsilo follows HuggingFace's 302 redirects to `cdn-lfs.huggingface.co` server-side and streams the result inline; clients never see the signed CDN URL. The adapter mirrors the Docker registry's shape (handler/resolver/keyer + a tiny headers helper). Auth is forwarded verbatim to upstream and bypasses the cache.

**Tech Stack:** Go 1.21, Gin, GORM (no new tables needed), existing `cache.Manager` + `upstream.Pool` (locked, not modified), React 18 + TypeScript + Vite (frontend), `simple-icons` (or emoji fallback).

**Spec source of truth:** [docs/specs/2026-05-27-huggingface-adapter.md](../specs/2026-05-27-huggingface-adapter.md) (commit 7fdd852).

---

## File structure

### New files (`depsilo/` repo)

| Path | Responsibility |
|---|---|
| `internal/adapter/huggingface/handler.go` | Gin route registration + method/path dispatch to sub-handlers |
| `internal/adapter/huggingface/resolver.go` | Server-side 302 following: upstream call → detect redirect → inner GET to signed CDN URL → stream back |
| `internal/adapter/huggingface/keyer.go` | Cache key derivation + TTL classification (commit-SHA vs branch/tag) |
| `internal/adapter/huggingface/headers.go` | Auth pass-through, bypass detection, HuggingFace-specific header preservation |
| `internal/adapter/huggingface/keyer_test.go` | Unit tests for keyer (path parse, SHA detection, TTL choice) |
| `internal/adapter/huggingface/resolver_test.go` | Unit tests for resolver (httptest mock upstream + CDN, redirect following, auth pass-through) |
| `tests/integration/huggingface_test.go` | Integration tests against in-process Depsilo + mock upstream |
| `testground/docker-huggingface/Dockerfile` | E2E: `huggingface-cli download prajjwal1/bert-tiny` via Depsilo |

### Modified files (`depsilo/` repo)

| Path | Change |
|---|---|
| `internal/config/config.go` | Add `HuggingFace AdapterConfig` field to `Config` struct |
| `internal/server/server.go` | Add `{"huggingface", "/huggingface", cfg.HuggingFace.Upstreams}` to `ecosystemDef` table + register adapter factory |
| `tests/mock/upstream_server.go` | New `RegisterHuggingFace()` method + call from `RegisterAll()` |
| `config.example.toml` | New `[[huggingface.upstreams]]` section |
| `Makefile` | New `test-docker-huggingface` target + add to `test-docker-all` list |
| `web/src/lib/ecosystemData.ts` | New LANGUAGES entry; extend `buildAgentPrompt` |
| `web/src/components/EcosystemIcon.tsx` | New `huggingface` case in `EcosystemType` union + `iconMap` |
| `web/src/i18n/zh.ts` + `en.ts` | New `quickstart.languages.huggingface.*` keys (parity-locked) |
| `internal/api/public/discover.go` | Append HuggingFace ecosystem entry + extend agent-prompt template |
| `README.md` + `docs/README_zh.md` | Mention HuggingFace under supported ecosystems + AI workload note |
| `CHANGELOG.md` | v0.5.0 entry under `### Added` |

---

# Phase 0 — Pre-flight

### Task 0.1: Confirm clean baseline

**Files:** none

- [ ] **Step 1: Confirm clean tree on master**

```bash
git status -s
git rev-parse --abbrev-ref HEAD
```

Expected: empty status (Makefile is unmodified — earlier sessions' DEPSILO_DEV_PRO change is already on master via run-pro target), branch is `master`.

- [ ] **Step 2: Confirm test baseline**

```bash
make lint
make test-unit
make test-integration
```

Expected: all green. If any fail, fix or note the unrelated failure before starting HuggingFace work.

- [ ] **Step 3: Capture starting commit**

```bash
git rev-parse HEAD > /tmp/depsilo-hf-start.txt
cat /tmp/depsilo-hf-start.txt
```

---

# Phase 1 — Range request spike + config struct

### Task 1.1: Determine whether `huggingface_hub` uses Range requests by default

**Files:** none (spike script lives in /tmp)

The spec §11 reserves this as a small spike. The outcome decides whether v1 needs to strip the `Range` header on cache miss.

- [ ] **Step 1: Write a spike script that captures requests**

Save to `/tmp/hf-spike.py`:

```python
#!/usr/bin/env python3
"""
Spike: does huggingface_hub send Range requests by default?
Runs huggingface-cli through a logging proxy and prints request headers.
"""
import http.server
import socketserver
import threading
import os
import subprocess
import tempfile

REQUESTS = []

class LogHandler(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        REQUESTS.append(('GET', self.path, dict(self.headers)))
        # Forward to upstream
        import urllib.request
        upstream = 'https://huggingface.co' + self.path
        try:
            with urllib.request.urlopen(upstream) as r:
                self.send_response(r.status)
                for k, v in r.headers.items():
                    self.send_header(k, v)
                self.end_headers()
                self.wfile.write(r.read())
        except Exception as e:
            self.send_response(500)
            self.end_headers()
            self.wfile.write(str(e).encode())

    def do_HEAD(self):
        REQUESTS.append(('HEAD', self.path, dict(self.headers)))
        self.send_response(200)
        self.end_headers()

def serve():
    with socketserver.ThreadingTCPServer(('127.0.0.1', 0), LogHandler) as srv:
        port = srv.server_address[1]
        print(f'Spike server: http://127.0.0.1:{port}', flush=True)
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        return port, srv

port, srv = serve()
os.environ['HF_ENDPOINT'] = f'http://127.0.0.1:{port}'
tmp = tempfile.mkdtemp()
subprocess.run(['huggingface-cli', 'download', 'prajjwal1/bert-tiny',
                '--local-dir', tmp], check=False)
srv.shutdown()

print("\n=== Captured requests ===")
for method, path, headers in REQUESTS:
    range_hdr = headers.get('Range') or headers.get('range') or '(none)'
    print(f"{method} {path[:80]} -- Range: {range_hdr}")
```

- [ ] **Step 2: Run the spike**

```bash
pip install --user huggingface_hub
python3 /tmp/hf-spike.py 2>&1 | tail -40
```

- [ ] **Step 3: Record the outcome**

Read the output. Look at the `Range:` column for each captured request. Two outcomes possible:

- **Outcome A (no Range used):** Every request shows `Range: (none)`. The spec §7.6 default (Range pass-through, no range caching in v1) works as-is. **Proceed.**
- **Outcome B (Range used for full downloads):** Some GET request shows `Range: bytes=0-` or similar even on first download. The resolver in §3 must strip `Range` headers on cache-miss-to-upstream calls and force a full body fetch. **Note this in the plan and adjust resolver impl in Phase 3.**

Save the outcome in plain text to `internal/adapter/huggingface/SPIKE.md` (will be committed in Task 1.2 alongside config). One line is fine:

```
# Range-request spike (2026-05-27)
Outcome: A — huggingface-cli does NOT send Range headers on first download.
v1 resolver does plain GET; no Range stripping needed.
```

(or "Outcome: B — Range is sent on every GET; resolver strips Range on cache-miss upstream calls.")

- [ ] **Step 4: No commit yet**

The SPIKE.md gets committed together with the config in Task 1.2.

### Task 1.2: Add `HuggingFaceConfig` (actually just reuse `AdapterConfig`) + commit spike

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/adapter/huggingface/SPIKE.md`

The 12 other adapters all use the shared `AdapterConfig` struct (just `Upstreams`). HuggingFace doesn't need anything else.

- [ ] **Step 1: Add HuggingFace field to Config**

In `internal/config/config.go`, find the `Config` struct (around line 5). After the `Helm AdapterConfig` line, before `Docker DockerConfig`, add:

```go
	HuggingFace AdapterConfig  `mapstructure:"huggingface"`
```

The full field block should look like:
```go
	CRAN     AdapterConfig  `mapstructure:"cran"`
	Helm     AdapterConfig  `mapstructure:"helm"`
	HuggingFace AdapterConfig  `mapstructure:"huggingface"`
	Docker   DockerConfig   `mapstructure:"docker"`
```

- [ ] **Step 2: Create the SPIKE.md with whichever outcome you recorded**

```bash
mkdir -p internal/adapter/huggingface
cat > internal/adapter/huggingface/SPIKE.md <<'EOF'
# Range-request spike (2026-05-27)

Outcome: A — huggingface-cli / huggingface_hub does NOT send `Range` headers
on first downloads. The resolver in this package therefore performs plain GET
to upstream on cache miss and serves the full body. Range header pass-through
on subsequent requests is fine; we just don't try to cache partial responses
in v1 (see spec §7.6).

If a future version of huggingface_hub starts using Range by default,
revisit: either (a) strip Range on upstream calls and force full download,
or (b) add range-aware cache semantics.
EOF
```

If your spike outcome was B, write that variant text instead.

- [ ] **Step 3: Verify build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go internal/adapter/huggingface/SPIKE.md
git commit -m "feat(config,hf): add HuggingFace adapter config + range-request spike outcome"
```

---

# Phase 2 — keyer TDD

### Task 2.1: Write failing keyer tests

**Files:**
- Create: `internal/adapter/huggingface/keyer_test.go`

- [ ] **Step 1: Write the test file**

```go
package huggingface_test

import (
	"testing"
	"time"

	"depsilo/internal/adapter/huggingface"
)

func TestIsCommitSHA(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", true},  // 40 lowercase hex
		{"A1B2C3D4E5F60718293A4B5C6D7E8F9012345678", false}, // uppercase — not standard SHA
		{"main", false},
		{"v1.0", false},
		{"refs/heads/feature", false},
		{"", false},
		{"a1b2c3", false},                                    // too short
		{"a1b2c3d4e5f60718293a4b5c6d7e8f90123456789", false}, // 41 chars
	}
	for _, tc := range cases {
		got := huggingface.IsCommitSHA(tc.in)
		if got != tc.want {
			t.Errorf("IsCommitSHA(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestTTLFor(t *testing.T) {
	sha := "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	if got := huggingface.TTLForRef(sha); got != 72*time.Hour {
		t.Errorf("TTLForRef(SHA) = %v, want 72h", got)
	}
	if got := huggingface.TTLForRef("main"); got != 5*time.Minute {
		t.Errorf("TTLForRef(branch) = %v, want 5m", got)
	}
	if got := huggingface.TTLForRef(""); got != 5*time.Minute {
		t.Errorf("TTLForRef(empty) = %v, want 5m", got)
	}
}

func TestParseRequestPath(t *testing.T) {
	cases := []struct {
		path     string
		wantKind huggingface.PathKind
		wantRepo string
		wantRef  string
		wantSub  string
	}{
		{
			path:     "/google/flan-t5-base/resolve/main/config.json",
			wantKind: huggingface.PathResolve,
			wantRepo: "google/flan-t5-base",
			wantRef:  "main",
			wantSub:  "config.json",
		},
		{
			path:     "/bert-base-uncased/resolve/main/pytorch_model.bin",
			wantKind: huggingface.PathResolve,
			wantRepo: "bert-base-uncased",
			wantRef:  "main",
			wantSub:  "pytorch_model.bin",
		},
		{
			path:     "/google/flan-t5/raw/v1.0/README.md",
			wantKind: huggingface.PathRaw,
			wantRepo: "google/flan-t5",
			wantRef:  "v1.0",
			wantSub:  "README.md",
		},
		{
			path:     "/api/models/bert-base-uncased",
			wantKind: huggingface.PathAPIModelInfo,
			wantRepo: "bert-base-uncased",
		},
		{
			path:     "/api/models/google/flan-t5-base/tree/main",
			wantKind: huggingface.PathAPIModelTree,
			wantRepo: "google/flan-t5-base",
			wantRef:  "main",
		},
		{
			path:     "/api/datasets/squad",
			wantKind: huggingface.PathAPIDatasetInfo,
			wantRepo: "squad",
		},
		{
			path:     "/api/datasets/wikitext/revision/abc1234567890123456789012345678901234567",
			wantKind: huggingface.PathAPIDatasetRevision,
			wantRepo: "wikitext",
			wantRef:  "abc1234567890123456789012345678901234567",
		},
		{
			path:     "/unknown/path",
			wantKind: huggingface.PathUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := huggingface.ParseRequestPath(tc.path)
			if got.Kind != tc.wantKind {
				t.Errorf("Kind = %v, want %v", got.Kind, tc.wantKind)
			}
			if got.Repo != tc.wantRepo {
				t.Errorf("Repo = %q, want %q", got.Repo, tc.wantRepo)
			}
			if got.Ref != tc.wantRef {
				t.Errorf("Ref = %q, want %q", got.Ref, tc.wantRef)
			}
			if got.Subpath != tc.wantSub {
				t.Errorf("Subpath = %q, want %q", got.Subpath, tc.wantSub)
			}
		})
	}
}

func TestCacheKey(t *testing.T) {
	parsed := huggingface.ParseRequestPath("/google/flan-t5-base/resolve/main/config.json")
	got := huggingface.CacheKey(parsed)
	want := "huggingface/google/flan-t5-base/resolve/main/config.json"
	if got != want {
		t.Errorf("CacheKey = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run tests; expect build failure**

```bash
go test ./internal/adapter/huggingface/ -v
```

Expected: compile errors — `huggingface.IsCommitSHA`, `huggingface.TTLForRef`, `huggingface.ParseRequestPath`, `huggingface.PathKind`, etc. all undefined. Red TDD state. Do not commit yet.

### Task 2.2: Implement keyer

**Files:**
- Create: `internal/adapter/huggingface/keyer.go`

- [ ] **Step 1: Write keyer.go**

```go
package huggingface

import (
	"regexp"
	"strings"
	"time"
)

// commitSHAPattern matches a canonical Git commit SHA: exactly 40
// lowercase hex characters. HuggingFace clients use these directly as
// refs for immutable downloads.
var commitSHAPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

// IsCommitSHA reports whether `ref` is a 40-character lowercase hex string
// that we can treat as immutable (long-TTL cache eligible).
func IsCommitSHA(ref string) bool {
	return commitSHAPattern.MatchString(ref)
}

// TTLForRef returns the cache TTL appropriate for a given ref. Commit SHAs
// are immutable (72h); branch/tag/everything else is mutable (5m) and
// participates in stale-while-revalidate via cache.Manager.
func TTLForRef(ref string) time.Duration {
	if IsCommitSHA(ref) {
		return 72 * time.Hour
	}
	return 5 * time.Minute
}

// PathKind tags the kind of HuggingFace URL a request maps to. The
// resolver/handler dispatches on this.
type PathKind int

const (
	PathUnknown PathKind = iota
	PathResolve              // /<repo>/resolve/<ref>/<subpath> — file download (LFS-aware)
	PathRaw                  // /<repo>/raw/<ref>/<subpath> — small file inline content
	PathAPIModelInfo         // /api/models/<repo>
	PathAPIModelRevision     // /api/models/<repo>/revision/<rev>
	PathAPIModelTree         // /api/models/<repo>/tree/<rev>
	PathAPIDatasetInfo       // /api/datasets/<repo>
	PathAPIDatasetRevision   // /api/datasets/<repo>/revision/<rev>
	PathAPIDatasetTree       // /api/datasets/<repo>/tree/<rev>
)

// Parsed holds the structured pieces of a HuggingFace request path.
type Parsed struct {
	Kind    PathKind
	Repo    string // "org/name" or "single-token"
	Ref     string // commit SHA, branch, or tag
	Subpath string // path within the repo for resolve/raw kinds
}

// ParseRequestPath splits a request path under /huggingface/ into its
// components. The leading slash is optional. Returns PathUnknown when no
// recognized pattern matches.
//
// Supported patterns (see spec §3.1):
//   /<repo>/resolve/<ref>/<subpath...>
//   /<repo>/raw/<ref>/<subpath...>
//   /api/models/<repo>
//   /api/models/<repo>/revision/<rev>
//   /api/models/<repo>/tree/<rev>
//   /api/datasets/<repo>[/revision|/tree]/<rev>
//
// Where <repo> is either "owner/name" (two segments) or a single token.
func ParseRequestPath(path string) Parsed {
	p := strings.TrimPrefix(path, "/")
	segs := strings.Split(p, "/")

	// /api/{models,datasets}/...
	if len(segs) >= 3 && segs[0] == "api" {
		return parseAPI(segs[1:])
	}

	// /<repo>/{resolve,raw}/<ref>/<subpath...>
	// Repo can be 1 or 2 segments. Find where "resolve" or "raw" appears.
	for i := 1; i <= 2 && i < len(segs); i++ {
		if i+2 < len(segs) && (segs[i] == "resolve" || segs[i] == "raw") {
			kind := PathResolve
			if segs[i] == "raw" {
				kind = PathRaw
			}
			return Parsed{
				Kind:    kind,
				Repo:    strings.Join(segs[:i], "/"),
				Ref:     segs[i+1],
				Subpath: strings.Join(segs[i+2:], "/"),
			}
		}
	}
	return Parsed{Kind: PathUnknown}
}

func parseAPI(segs []string) Parsed {
	// segs[0] = "models" or "datasets"
	// segs[1] = first repo segment (and possibly only)
	// segs[2] = (optional) second repo segment OR "revision" OR "tree"
	// ...
	if len(segs) < 2 {
		return Parsed{Kind: PathUnknown}
	}
	kindBase := segs[0] // "models" or "datasets"
	if kindBase != "models" && kindBase != "datasets" {
		return Parsed{Kind: PathUnknown}
	}

	// Find "revision" or "tree" if present
	splitAt := -1
	splitKind := ""
	for i := 2; i < len(segs); i++ {
		if segs[i] == "revision" || segs[i] == "tree" {
			splitAt = i
			splitKind = segs[i]
			break
		}
	}

	var repo string
	var ref string
	if splitAt == -1 {
		repo = strings.Join(segs[1:], "/")
	} else {
		repo = strings.Join(segs[1:splitAt], "/")
		if splitAt+1 < len(segs) {
			ref = segs[splitAt+1]
		}
	}

	out := Parsed{Repo: repo, Ref: ref}
	switch {
	case kindBase == "models" && splitKind == "":
		out.Kind = PathAPIModelInfo
	case kindBase == "models" && splitKind == "revision":
		out.Kind = PathAPIModelRevision
	case kindBase == "models" && splitKind == "tree":
		out.Kind = PathAPIModelTree
	case kindBase == "datasets" && splitKind == "":
		out.Kind = PathAPIDatasetInfo
	case kindBase == "datasets" && splitKind == "revision":
		out.Kind = PathAPIDatasetRevision
	case kindBase == "datasets" && splitKind == "tree":
		out.Kind = PathAPIDatasetTree
	default:
		out.Kind = PathUnknown
	}
	return out
}

// CacheKey derives the cache.Manager key from a parsed request. The shape
// mirrors the request path with the "huggingface/" prefix.
func CacheKey(p Parsed) string {
	switch p.Kind {
	case PathResolve:
		return "huggingface/" + p.Repo + "/resolve/" + p.Ref + "/" + p.Subpath
	case PathRaw:
		return "huggingface/" + p.Repo + "/raw/" + p.Ref + "/" + p.Subpath
	case PathAPIModelInfo:
		return "huggingface/api/models/" + p.Repo
	case PathAPIModelRevision:
		return "huggingface/api/models/" + p.Repo + "/revision/" + p.Ref
	case PathAPIModelTree:
		return "huggingface/api/models/" + p.Repo + "/tree/" + p.Ref
	case PathAPIDatasetInfo:
		return "huggingface/api/datasets/" + p.Repo
	case PathAPIDatasetRevision:
		return "huggingface/api/datasets/" + p.Repo + "/revision/" + p.Ref
	case PathAPIDatasetTree:
		return "huggingface/api/datasets/" + p.Repo + "/tree/" + p.Ref
	default:
		return ""
	}
}
```

- [ ] **Step 2: Run tests; expect green**

```bash
go test ./internal/adapter/huggingface/ -v
```

Expected: all 4 test functions pass.

- [ ] **Step 3: Commit**

```bash
git add internal/adapter/huggingface/keyer.go internal/adapter/huggingface/keyer_test.go
git commit -m "feat(hf): path parser + cache key + TTL classifier with unit tests"
```

---

# Phase 3 — Headers helper + resolver TDD

### Task 3.1: Headers helper (auth bypass + HF header preservation)

**Files:**
- Create: `internal/adapter/huggingface/headers.go`

- [ ] **Step 1: Write headers.go**

```go
package huggingface

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// hfPassthroughHeaders is the set of upstream response headers that
// huggingface_hub and related clients rely on for verification, size
// reporting, and resume support. Anything outside this list is treated
// as opaque metadata and dropped at the proxy boundary.
var hfPassthroughHeaders = []string{
	"X-Linked-Etag",      // SHA256 of the LFS blob — client compares against received bytes
	"X-Linked-Size",      // expected byte count
	"X-Repo-Commit",      // resolved commit SHA the ref pointed to
	"ETag",               // standard HTTP ETag
	"Content-Length",     // standard
	"Content-Type",       // standard
	"Accept-Ranges",      // tells clients resume is supported
	"Last-Modified",      // standard
	"Cache-Control",      // upstream's caching hint (we honor it for metadata)
}

// AuthBypass reports whether the incoming request carries an Authorization
// header, in which case we (a) forward it to upstream verbatim and
// (b) skip the cache entirely on both read and write.
func AuthBypass(c *gin.Context) bool {
	return c.GetHeader("Authorization") != ""
}

// CopyRequestHeaders forwards headers from the gin context to an outbound
// upstream request: Authorization (if present), User-Agent, Range, Accept,
// If-None-Match, If-Modified-Since. Anything else is dropped so the proxy
// doesn't leak host-specific headers (Host, Cookie, X-Forwarded-*) upstream.
func CopyRequestHeaders(c *gin.Context, upReq *http.Request) {
	for _, h := range []string{
		"Authorization",
		"User-Agent",
		"Range",
		"Accept",
		"Accept-Encoding",
		"If-None-Match",
		"If-Modified-Since",
	} {
		if v := c.GetHeader(h); v != "" {
			upReq.Header.Set(h, v)
		}
	}
}

// CopyResponseHeaders mirrors the allow-listed HuggingFace headers from an
// upstream response onto the gin response writer. Idempotent; safe to call
// multiple times (later calls overwrite earlier values).
func CopyResponseHeaders(c *gin.Context, resp *http.Response) {
	for _, h := range hfPassthroughHeaders {
		if v := resp.Header.Get(h); v != "" {
			c.Header(h, v)
		}
	}
}
```

- [ ] **Step 2: Compile-check**

```bash
go build ./internal/adapter/huggingface/
```

Expected: no errors. (No tests for headers in isolation — it's a thin helper covered by resolver tests in Task 3.2.)

- [ ] **Step 3: Commit**

```bash
git add internal/adapter/huggingface/headers.go
git commit -m "feat(hf): headers helper — auth bypass + request/response header pass-through"
```

### Task 3.2: Write failing resolver tests

**Files:**
- Create: `internal/adapter/huggingface/resolver_test.go`

- [ ] **Step 1: Write the test file**

```go
package huggingface_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter/huggingface"
)

func TestResolver_Direct200(t *testing.T) {
	// Upstream returns 200 directly (non-LFS small file like config.json).
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Repo-Commit", "abc1234")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"hidden":false}`))
	}))
	defer upstream.Close()

	gin.SetMode(gin.TestMode)
	g := gin.New()
	res := huggingface.NewResolver()
	g.GET("/test/resolve/main/config.json", func(c *gin.Context) {
		res.Handle(c, upstream.URL, c.Request.URL.Path)
	})

	req := httptest.NewRequest(http.MethodGet, "/test/resolve/main/config.json", nil)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("X-Repo-Commit"); got != "abc1234" {
		t.Errorf("X-Repo-Commit = %q, want abc1234", got)
	}
	if !strings.Contains(w.Body.String(), `"hidden":false`) {
		t.Errorf("body did not contain expected JSON: %s", w.Body.String())
	}
}

func TestResolver_Follow302ToCDN(t *testing.T) {
	// CDN server: serves the actual blob bytes when fetched with the
	// signed URL. Receives no Authorization header.
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("CDN must not receive Authorization header; got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "11")
		w.WriteHeader(200)
		w.Write([]byte("FAKE_WEIGHT"))
	}))
	defer cdn.Close()

	// Upstream "huggingface.co": 302 to the CDN with HF headers
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Linked-Etag", "deadbeef")
		w.Header().Set("X-Linked-Size", "11")
		w.Header().Set("X-Repo-Commit", "abc1234")
		w.Header().Set("Location", cdn.URL+"/blob?sig=zzz")
		w.WriteHeader(302)
	}))
	defer upstream.Close()

	gin.SetMode(gin.TestMode)
	g := gin.New()
	res := huggingface.NewResolver()
	g.GET("/repo/resolve/main/weights.bin", func(c *gin.Context) {
		res.Handle(c, upstream.URL, c.Request.URL.Path)
	})

	req := httptest.NewRequest(http.MethodGet, "/repo/resolve/main/weights.bin", nil)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (resolver should follow 302 internally)", w.Code)
	}
	if w.Body.String() != "FAKE_WEIGHT" {
		t.Errorf("body = %q, want FAKE_WEIGHT", w.Body.String())
	}
	if got := w.Header().Get("X-Linked-Etag"); got != "deadbeef" {
		t.Errorf("X-Linked-Etag = %q, want deadbeef (must be passed through)", got)
	}
	if got := w.Header().Get("X-Repo-Commit"); got != "abc1234" {
		t.Errorf("X-Repo-Commit = %q, want abc1234", got)
	}
}

func TestResolver_AuthForwardedToUpstreamNotCDN(t *testing.T) {
	upstreamGotAuth := false
	cdnGotAuth := false

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			cdnGotAuth = true
		}
		w.Write([]byte("ok"))
	}))
	defer cdn.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer testtoken" {
			upstreamGotAuth = true
		}
		w.Header().Set("Location", cdn.URL+"/blob")
		w.WriteHeader(302)
	}))
	defer upstream.Close()

	gin.SetMode(gin.TestMode)
	g := gin.New()
	res := huggingface.NewResolver()
	g.GET("/repo/resolve/main/file", func(c *gin.Context) {
		res.Handle(c, upstream.URL, c.Request.URL.Path)
	})

	req := httptest.NewRequest(http.MethodGet, "/repo/resolve/main/file", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)

	if !upstreamGotAuth {
		t.Error("upstream must receive Authorization header")
	}
	if cdnGotAuth {
		t.Error("CDN must NOT receive Authorization header (signed URL carries its own auth)")
	}
}

func TestResolver_404PassThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":"Entry not found"}`))
	}))
	defer upstream.Close()

	gin.SetMode(gin.TestMode)
	g := gin.New()
	res := huggingface.NewResolver()
	g.GET("/missing/resolve/main/x", func(c *gin.Context) {
		res.Handle(c, upstream.URL, c.Request.URL.Path)
	})

	req := httptest.NewRequest(http.MethodGet, "/missing/resolve/main/x", nil)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Entry not found") {
		t.Errorf("body = %q, want pass-through of upstream body", w.Body.String())
	}
}

func TestResolver_HEAD_BodyNotFetched(t *testing.T) {
	cdnGotRequest := false
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnGotRequest = true
		w.Write([]byte("body"))
	}))
	defer cdn.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("upstream got %s, want HEAD", r.Method)
		}
		w.Header().Set("X-Linked-Etag", "deadbeef")
		w.Header().Set("X-Linked-Size", "11")
		w.Header().Set("Location", cdn.URL+"/blob")
		w.WriteHeader(302)
	}))
	defer upstream.Close()

	gin.SetMode(gin.TestMode)
	g := gin.New()
	res := huggingface.NewResolver()
	g.HEAD("/repo/resolve/main/weights.bin", func(c *gin.Context) {
		res.Handle(c, upstream.URL, c.Request.URL.Path)
	})

	req := httptest.NewRequest(http.MethodHead, "/repo/resolve/main/weights.bin", nil)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("HEAD response had body of %d bytes, want 0", w.Body.Len())
	}
	if got := w.Header().Get("X-Linked-Etag"); got != "deadbeef" {
		t.Errorf("X-Linked-Etag = %q, want deadbeef (header passes through on HEAD)", got)
	}
	if cdnGotRequest {
		t.Error("CDN must NOT be hit for HEAD requests — only the upstream HEAD response is needed")
	}
	_ = io.Discard
}
```

- [ ] **Step 2: Run tests; expect compile failures**

```bash
go test ./internal/adapter/huggingface/ -v -run TestResolver
```

Expected: build errors mentioning `huggingface.NewResolver`, `(*Resolver).Handle` undefined. Red TDD state.

### Task 3.3: Implement Resolver

**Files:**
- Create: `internal/adapter/huggingface/resolver.go`

- [ ] **Step 1: Write resolver.go**

```go
package huggingface

import (
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Resolver performs upstream request → optional 302 follow → response
// streaming. It is the heart of the HuggingFace adapter — see spec §4
// (Server-side following) for the design rationale.
//
// The struct holds no per-request state; it's safe to share one instance
// across all requests. The HTTP clients are reused so connections pool
// naturally.
type Resolver struct {
	// nonFollowing makes outbound calls to upstream (huggingface.co or
	// hf-mirror.com). We MUST NOT follow redirects automatically — we
	// need to inspect the 302's Location and X-Linked-Etag headers.
	nonFollowing *http.Client

	// cdnClient makes outbound calls to the signed CDN URL after a 302.
	// Default redirect-following is fine here; CDN URLs do not normally
	// chain further redirects.
	cdnClient *http.Client
}

// NewResolver builds a Resolver with sensible defaults for HuggingFace's
// traffic profile. The 30-minute CDN timeout accommodates multi-gigabyte
// model files on slow links.
func NewResolver() *Resolver {
	return &Resolver{
		nonFollowing: &http.Client{
			Timeout: 5 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		cdnClient: &http.Client{
			Timeout: 30 * time.Minute,
		},
	}
}

// Handle runs the full upstream-fetch + optional-redirect-follow flow for
// a single request. `upstreamBase` is e.g. "https://huggingface.co" and
// `requestPath` is the request URL path (e.g. "/repo/resolve/main/x").
//
// Caching: this method does not interact with cache.Manager directly —
// callers (the handler) are responsible for wrapping Handle in a cache
// lookup/write. The reason for the split: the resolver focuses on
// HuggingFace's redirect protocol; the handler owns cache-policy decisions
// (auth bypass, ref-classified TTL).
func (r *Resolver) Handle(c *gin.Context, upstreamBase, requestPath string) {
	upURL := upstreamBase + requestPath

	upReq, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, upURL, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "build upstream request: %v", err)
		return
	}
	CopyRequestHeaders(c, upReq)

	upResp, err := r.nonFollowing.Do(upReq)
	if err != nil {
		zap.L().Warn("huggingface upstream call failed",
			zap.String("url", upURL), zap.Error(err))
		c.String(http.StatusBadGateway, "upstream error: %v", err)
		return
	}
	defer upResp.Body.Close()

	CopyResponseHeaders(c, upResp)

	switch upResp.StatusCode {
	case http.StatusOK:
		// Non-LFS small file: pass body straight through.
		c.Status(http.StatusOK)
		if c.Request.Method != http.MethodHead {
			_, _ = io.Copy(c.Writer, upResp.Body)
		}

	case http.StatusFound, http.StatusMovedPermanently,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		// HEAD: just expose the headers we already copied, don't fetch the body.
		if c.Request.Method == http.MethodHead {
			c.Status(http.StatusOK)
			return
		}

		// GET: follow the redirect server-side to the (typically signed) CDN URL.
		// The signed URL carries its own auth in query params; we MUST NOT
		// forward client Authorization to the CDN.
		signed := upResp.Header.Get("Location")
		if signed == "" {
			c.String(http.StatusBadGateway, "upstream redirect missing Location")
			return
		}
		innerReq, err := http.NewRequestWithContext(c.Request.Context(),
			http.MethodGet, signed, nil)
		if err != nil {
			c.String(http.StatusInternalServerError, "build CDN request: %v", err)
			return
		}
		// Forward Range/User-Agent/Accept-Encoding only; deliberately drop Authorization.
		for _, h := range []string{"User-Agent", "Range", "Accept-Encoding"} {
			if v := c.GetHeader(h); v != "" {
				innerReq.Header.Set(h, v)
			}
		}

		innerResp, err := r.cdnClient.Do(innerReq)
		if err != nil {
			zap.L().Warn("huggingface CDN fetch failed",
				zap.String("url", signed), zap.Error(err))
			c.String(http.StatusBadGateway, "cdn error: %v", err)
			return
		}
		defer innerResp.Body.Close()

		// Headers from the inner response (Content-Length is what the
		// client needs; the upstream's X-Linked-* are already on c.Header).
		if cl := innerResp.Header.Get("Content-Length"); cl != "" {
			c.Header("Content-Length", cl)
		}
		if ct := innerResp.Header.Get("Content-Type"); ct != "" {
			c.Header("Content-Type", ct)
		}
		c.Status(innerResp.StatusCode)
		_, _ = io.Copy(c.Writer, innerResp.Body)

	default:
		// 4xx / 5xx — pass status and body through. Not cached.
		c.Status(upResp.StatusCode)
		if c.Request.Method != http.MethodHead {
			_, _ = io.Copy(c.Writer, upResp.Body)
		}
	}
}
```

- [ ] **Step 2: Run resolver tests**

```bash
go test ./internal/adapter/huggingface/ -v -run TestResolver
```

Expected: all 5 resolver tests pass.

- [ ] **Step 3: Run all package tests**

```bash
go test ./internal/adapter/huggingface/ -v
```

Expected: 4 keyer tests + 5 resolver tests = 9 PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/huggingface/resolver.go internal/adapter/huggingface/resolver_test.go
git commit -m "feat(hf): server-side 302 following resolver with httptest unit tests"
```

---

# Phase 4 — Handler + dispatch

### Task 4.1: Handler that ties resolver + keyer + cache together

**Files:**
- Create: `internal/adapter/huggingface/handler.go`

- [ ] **Step 1: Write handler.go**

```go
package huggingface

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

// Handler is the HuggingFace adapter entry point. Construction mirrors
// the simple-passthrough adapter pattern (see internal/adapter/cran for
// reference) but with a Resolver injected for the server-side redirect
// following flow.
type Handler struct {
	cacheMgr *cache.Manager
	selector upstream.Selector
	cfg      config.CacheConfig
	db       *gorm.DB
	resolver *Resolver
}

func New(cacheMgr *cache.Manager, selector upstream.Selector,
	cfg config.CacheConfig, database *gorm.DB) *Handler {
	return &Handler{
		cacheMgr: cacheMgr,
		selector: selector,
		cfg:      cfg,
		db:       database,
		resolver: NewResolver(),
	}
}

// Type implements adapter.Adapter.
func (h *Handler) Type() string { return "huggingface" }

// Register implements adapter.Adapter. We use a single catch-all route for
// both GET and HEAD; handler-internal dispatch on the parsed path decides
// how to handle each kind of request.
func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/*path", h.handleRequest)
	rg.HEAD("/*path", h.handleRequest)
}

func (h *Handler) handleRequest(c *gin.Context) {
	path := c.Param("path")
	parsed := ParseRequestPath(path)

	if parsed.Kind == PathUnknown {
		c.String(404, "unrecognized HuggingFace path")
		return
	}

	up := h.selector.Select(c.Request.Context())
	if up == nil {
		c.String(503, "no healthy huggingface upstream")
		return
	}
	upBase := up.URL

	// Note v1 caching scope: the resolver itself currently does NOT
	// integrate with h.cacheMgr — passthrough mode. Phase 6 wires this
	// when integration tests reveal what the cache hit/miss boundaries
	// look like in practice. See spec §11.
	//
	// AuthBypass(c) is also checked here for future cache integration.
	_ = AuthBypass(c)
	_ = parsed
	_ = CacheKey(parsed)
	_ = TTLForRef(parsed.Ref)
	_ = adapter.Adapter(h) // silence unused-import warning during scaffold

	h.resolver.Handle(c, upBase, path)

	zap.L().Debug("huggingface request handled",
		zap.String("kind", kindString(parsed.Kind)),
		zap.String("repo", parsed.Repo),
		zap.String("ref", parsed.Ref),
		zap.Int("status", c.Writer.Status()),
	)
}

func kindString(k PathKind) string {
	switch k {
	case PathResolve:
		return "resolve"
	case PathRaw:
		return "raw"
	case PathAPIModelInfo:
		return "api/models"
	case PathAPIModelRevision:
		return "api/models/revision"
	case PathAPIModelTree:
		return "api/models/tree"
	case PathAPIDatasetInfo:
		return "api/datasets"
	case PathAPIDatasetRevision:
		return "api/datasets/revision"
	case PathAPIDatasetTree:
		return "api/datasets/tree"
	default:
		return "unknown"
	}
}
```

Note: the handler in this v1 plan is intentionally pass-through (no `cache.Manager` integration yet). The resolver streams responses directly. Cache integration happens after integration tests demonstrate the protocol's hit/miss behavior under realistic conditions (Phase 6). This is the same "make it work first, optimize later" pattern the project applied to Docker Registry.

- [ ] **Step 2: Verify build**

```bash
go build ./...
```

Expected: no errors. The `_ = ...` lines silence unused-variable warnings for symbols that will be re-wired in Phase 6 (cache integration).

- [ ] **Step 3: Run tests**

```bash
go test ./internal/adapter/huggingface/ -v
```

Expected: 9 PASS, 0 FAIL.

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/huggingface/handler.go
git commit -m "feat(hf): adapter handler dispatching parsed paths to resolver"
```

---

# Phase 5 — Server wiring

### Task 5.1: Register HuggingFace in ecosystemDef table + adapter factory

**Files:**
- Modify: `internal/server/server.go`

- [ ] **Step 1: Add to ecosystemDef table**

Find the `ecosystems := []ecosystemDef{...}` block (around line 107). Add the HuggingFace entry after Helm:

```go
		{"cran", "/cran", cfg.CRAN.Upstreams},
		{"helm", "/helm", cfg.Helm.Upstreams},
		{"huggingface", "/huggingface", cfg.HuggingFace.Upstreams},  // NEW
	}
```

- [ ] **Step 2: Add factory function**

Find the `adapterFactory := map[string]func(...)` block (around line 240+). Add:

```go
		"huggingface": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return huggingface.New(cm, s, cc, db)
		},
```

(Place it after the `"helm":` entry, before `"docker":` if present.)

- [ ] **Step 3: Add the import**

At the top of `internal/server/server.go`, find the adapter imports block. Add:

```go
	"depsilo/internal/adapter/huggingface"
```

(Alphabetically: it goes between `helm` and `maven`.)

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 5: Verify server boots**

```bash
make stop 2>/dev/null || true
make build
DEPSILO_CONFIG=config.example.toml ./bin/depsilo serve > /tmp/hf-boot.log 2>&1 &
PID=$!
sleep 3
curl -sf http://localhost:23333/health
kill $PID 2>/dev/null
grep "huggingface" /tmp/hf-boot.log | head -5
```

Expected: `/health` returns 200, logs show "registered upstream" lines for the huggingface ecosystem.

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go
git commit -m "feat(server): register HuggingFace ecosystem in routing table + factory"
```

### Task 5.2: Default config + Makefile target stub

**Files:**
- Modify: `config.example.toml`
- Modify: `Makefile` (just the `test-docker-all` list for now; the new target body comes in Phase 7)

- [ ] **Step 1: Add `[[huggingface.upstreams]]` block to `config.example.toml`**

Find the existing `[[helm.upstreams]]` block. After it, add:

```toml
# ─── Hugging Face Hub ─────────────────────────────────────
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

- [ ] **Step 2: Add HuggingFace to the test-docker-all list**

In `Makefile`, find the `test-docker-all` declaration:

```makefile
test-docker-all: test-docker-pypi test-docker-apt test-docker-npm test-docker-go \
    test-docker-cargo test-docker-maven test-docker-rubygems test-docker-composer test-docker-nuget \
    test-docker-conda test-docker-cran test-docker-helm test-docker-docker \
    ...
```

Insert `test-docker-huggingface` near the end (before `test-docker-docker`):

```makefile
test-docker-all: test-docker-pypi test-docker-apt test-docker-npm test-docker-go \
    test-docker-cargo test-docker-maven test-docker-rubygems test-docker-composer test-docker-nuget \
    test-docker-conda test-docker-cran test-docker-helm test-docker-huggingface test-docker-docker \
    ...
```

(The actual target body is added in Phase 7; for now we just reserve its place in the sequence.)

- [ ] **Step 3: Verify the test-docker-all list parses**

```bash
make -n test-docker-all 2>&1 | head -3
```

Expected: Make complains it cannot find `test-docker-huggingface` recipe — that's expected since Phase 7 hasn't run. The error confirms the list parses; if there's a syntax error in Makefile itself, Make would complain differently.

- [ ] **Step 4: Commit**

```bash
git add config.example.toml Makefile
git commit -m "feat(config): default huggingface upstreams (hf-mirror + official) + Makefile slot"
```

---

# Phase 6 — Mock upstream + integration tests

### Task 6.1: Add `RegisterHuggingFace()` to the mock upstream

**Files:**
- Modify: `tests/mock/upstream_server.go`

- [ ] **Step 1: Add the method**

Find `RegisterAll()` at the end of `tests/mock/upstream_server.go`. Add a new registration method before it:

```go
// RegisterHuggingFace adds HuggingFace Hub endpoints — model metadata,
// tree listing, and the resolve flow with a 302 redirect to a tracked
// "CDN" path served by the same mock server.
func (m *MockUpstream) RegisterHuggingFace() {
	// Model metadata endpoint
	m.mux.HandleFunc("/api/models/bert-base-uncased", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"modelId":"bert-base-uncased","sha":"abc1234","siblings":[{"rfilename":"config.json"},{"rfilename":"pytorch_model.bin"}]}`)
	})

	// Tree listing
	m.mux.HandleFunc("/api/models/bert-base-uncased/tree/main", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `[{"path":"config.json","type":"file","size":645},{"path":"pytorch_model.bin","type":"file","size":11}]`)
	})

	// Dataset metadata (proves the datasets branch works too)
	m.mux.HandleFunc("/api/datasets/squad", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"id":"squad","sha":"def5678"}`)
	})

	// Direct 200 file (config.json — not LFS)
	m.mux.HandleFunc("/bert-base-uncased/resolve/main/config.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Repo-Commit", "abc1234")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"hidden":false}`)
	})

	// LFS file (pytorch_model.bin) — 302 redirects to mock CDN path
	m.mux.HandleFunc("/bert-base-uncased/resolve/main/pytorch_model.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Linked-Etag", "deadbeefcafe")
		w.Header().Set("X-Linked-Size", "11")
		w.Header().Set("X-Repo-Commit", "abc1234")
		w.Header().Set("Location", m.URL()+"/cdn-lfs/pytorch_model.bin?sig=mock-sig")
		w.WriteHeader(302)
	})

	// "CDN" path serving the actual bytes
	m.mux.HandleFunc("/cdn-lfs/pytorch_model.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "11")
		w.WriteHeader(200)
		w.Write([]byte("FAKE_WEIGHT"))
	})
}
```

- [ ] **Step 2: Hook into `RegisterAll()`**

In the same file, find `func (m *MockUpstream) RegisterAll()`. Add `m.RegisterHuggingFace()` to its body alongside the other adapters:

```go
	m.RegisterCRAN()
	m.RegisterHelm()
	m.RegisterHuggingFace()  // NEW
	m.RegisterDocker()
```

- [ ] **Step 3: Add a HuggingFace upstream to the test config**

In `tests/integration/main_test.go`, find `writeTestConfig()`. Inside the `cfg` string, append a `[[huggingface.upstreams]]` block following the existing pattern:

Add the directive interpolation at the end of the upstream block (just before `[docker]`):

```go
[[huggingface.upstreams]]
name = "mock"
url = "%s"
priority = 1
```

And add the corresponding `upstreamURL` argument to the `fmt.Sprintf` call at the end. The function builds a long format string and ends with a long list of `upstreamURL` args (12 of them currently — one per ecosystem). Append a 13th.

- [ ] **Step 4: Verify build + existing integration tests still pass**

```bash
go build ./...
make test-integration
```

Expected: PASS. All previously-passing integration tests should still pass — your change adds new mock endpoints but doesn't remove any.

- [ ] **Step 5: Commit**

```bash
git add tests/mock/upstream_server.go tests/integration/main_test.go
git commit -m "test(mock,integration): HuggingFace mock endpoints + test config upstream"
```

### Task 6.2: Integration tests for HuggingFace

**Files:**
- Create: `tests/integration/huggingface_test.go`

- [ ] **Step 1: Write the test file**

```go
//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
)

func TestHF_ModelMetadata(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/huggingface/api/models/bert-base-uncased")
	assertStatus(t, resp, 200)
	assertBodyContains(t, resp, `"modelId":"bert-base-uncased"`)
}

func TestHF_ModelTreeListing(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/huggingface/api/models/bert-base-uncased/tree/main")
	assertStatus(t, resp, 200)
	assertBodyContains(t, resp, `"path":"config.json"`)
	assertBodyContains(t, resp, `"path":"pytorch_model.bin"`)
}

func TestHF_DatasetMetadata(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/huggingface/api/datasets/squad")
	assertStatus(t, resp, 200)
	assertBodyContains(t, resp, `"id":"squad"`)
}

func TestHF_ResolveSmallFile(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/huggingface/bert-base-uncased/resolve/main/config.json")
	assertStatus(t, resp, 200)
	defer resp.Body.Close()
	body := readBody(t, resp)
	if !strings.Contains(body, `"hidden":false`) {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestHF_ResolveLFS_StreamedFromCDN(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/huggingface/bert-base-uncased/resolve/main/pytorch_model.bin")
	assertStatus(t, resp, 200)
	defer resp.Body.Close()
	body := readBody(t, resp)
	if body != "FAKE_WEIGHT" {
		t.Errorf("body = %q, want FAKE_WEIGHT (server-side redirect should have fetched the CDN bytes)", body)
	}
	if got := resp.Header.Get("X-Linked-Etag"); got != "deadbeefcafe" {
		t.Errorf("X-Linked-Etag = %q, want deadbeefcafe (header from upstream must pass through)", got)
	}
}

func TestHF_HEAD_HeadersOnly(t *testing.T) {
	req, err := http.NewRequest(http.MethodHead,
		depsiloURL+"/huggingface/bert-base-uncased/resolve/main/pytorch_model.bin", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.ContentLength > 0 {
		body := readBody(t, resp)
		if len(body) > 0 {
			t.Errorf("HEAD body length = %d, want 0", len(body))
		}
	}
	if got := resp.Header.Get("X-Linked-Etag"); got != "deadbeefcafe" {
		t.Errorf("X-Linked-Etag = %q, want deadbeefcafe", got)
	}
}

func TestHF_UnknownPath_404(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/huggingface/no/such/route/at/all")
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404 for unrecognized path", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run integration tests**

```bash
make test-integration
```

Expected: previous tests + 7 new HF tests all pass.

- [ ] **Step 3: Commit**

```bash
git add tests/integration/huggingface_test.go
git commit -m "test(integration): HuggingFace adapter — metadata, tree, resolve (200 + 302), HEAD, 404"
```

---

# Phase 7 — Docker E2E

### Task 7.1: testground/docker-huggingface E2E

**Files:**
- Create: `testground/docker-huggingface/Dockerfile`
- Modify: `Makefile` (add the actual `test-docker-huggingface` recipe)

- [ ] **Step 1: Write the Dockerfile**

`testground/docker-huggingface/Dockerfile`:

```dockerfile
FROM python:3.11-alpine

ARG HF_ENDPOINT
ENV HF_ENDPOINT=${HF_ENDPOINT}

RUN apk add --no-cache git
RUN pip install --no-cache-dir huggingface_hub

# Use a tiny published model so the E2E completes in seconds.
# prajjwal1/bert-tiny is ~17 MB — same protocol surface as a 50 GB model,
# vastly smaller download. If this model becomes unavailable, swap to
# any public model under ~50 MB.
RUN huggingface-cli download prajjwal1/bert-tiny --local-dir /tmp/m \
    && test -f /tmp/m/config.json \
    && test -f /tmp/m/pytorch_model.bin \
    && python -c "import json; cfg = json.load(open('/tmp/m/config.json')); \
                  assert cfg.get('model_type') == 'bert', cfg; print('config.json OK')"

CMD ["echo", "depsilo huggingface E2E passed"]
```

- [ ] **Step 2: Add the Makefile recipe**

In `Makefile`, find the `test-docker-helm:` recipe. Add the HuggingFace recipe just after it (mirroring its shape):

```makefile
test-docker-huggingface: dev    ## E2E: huggingface-cli download bert-tiny through proxy
	@echo "=== [huggingface] huggingface-cli download prajjwal1/bert-tiny ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg HF_ENDPOINT=$(DEPSILO_URL)/huggingface \
		-t depsilo-test-huggingface $(TEST_DIR)/docker-huggingface
```

- [ ] **Step 3: Run the E2E**

```bash
make stop 2>/dev/null || true
make test-docker-huggingface
```

Expected: Docker builds the image, the RUN step inside completes, and the build ends with "depsilo huggingface E2E passed". This validates the entire chain: `huggingface-cli` → `HF_ENDPOINT` → Depsilo `/huggingface/*` routes → mock or real HF (depending on whether the host has internet) → cache → local file.

If `bert-tiny` is not accessible from the test environment (no internet, region block), the build will fail at the `huggingface-cli download` step with an upstream error. That's a real signal — Depsilo can't proxy what it can't reach. Skip the E2E in offline CI (the Makefile's `make test-e2e-skip-huggingface` target can be added later if needed).

- [ ] **Step 4: Commit**

```bash
git add testground/docker-huggingface/Dockerfile Makefile
git commit -m "test(e2e): Docker E2E — huggingface-cli download bert-tiny through Depsilo"
```

---

# Phase 8 — Frontend: icon, LANGUAGES entry, i18n

### Task 8.1: Add `huggingface` to EcosystemIcon

**Files:**
- Modify: `web/src/components/EcosystemIcon.tsx`

- [ ] **Step 1: Inspect what simple-icons provides for HuggingFace**

```bash
node -e "const si = require('simple-icons'); console.log(Object.keys(si).filter(k => k.toLowerCase().includes('hug')));" 2>&1 | head -3
```

If output includes `siHuggingface`, use it. If not, fall back to a custom inline SVG (the HuggingFace 🤗 mark).

- [ ] **Step 2: Add to the `EcosystemType` union**

In `web/src/components/EcosystemIcon.tsx`, find the type union (around line 13):

```ts
type EcosystemType =
  | 'pip' | 'pypi'
  | ...
  | 'docker'
  | 'huggingface'   // NEW
```

- [ ] **Step 3: Add to `iconMap`**

Find the `iconMap` constant. If `siHuggingface` exists, import it and add:

```ts
import {
  ...
  siHuggingface,
} from 'simple-icons'

const iconMap: Record<string, typeof siPython> = {
  ...
  huggingface: siHuggingface,
}
```

If `siHuggingface` does NOT exist in the installed simple-icons version, define a minimal local icon descriptor matching the shape simple-icons uses:

```ts
// Inline icon descriptor — used when simple-icons doesn't ship a HuggingFace mark.
// SVG path traced from the HuggingFace 🤗 brand mark, simplified to a single path.
const siHuggingfaceFallback = {
  title: 'Hugging Face',
  slug: 'huggingface',
  hex: 'FFD21E',
  path: 'M11.087 0a11.087 11.087 0 1 0 0 22.174 11.087 11.087 0 0 0 0-22.174zm-3.694 8.652a1.232 1.232 0 1 1 0 2.464 1.232 1.232 0 0 1 0-2.464zm7.388 0a1.232 1.232 0 1 1 0 2.464 1.232 1.232 0 0 1 0-2.464zM5.696 14.087a5.391 5.391 0 0 0 10.782 0H5.696z',
} as const

const iconMap: Record<string, typeof siPython> = {
  ...
  huggingface: siHuggingfaceFallback,
}
```

(Adjust the `path` if a more accurate SVG is needed; the simplified mark is recognizable.)

- [ ] **Step 4: Type-check**

```bash
cd web && npx tsc --noEmit && cd -
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/EcosystemIcon.tsx
git commit -m "feat(web): EcosystemIcon supports 'huggingface' (simple-icons or fallback SVG)"
```

### Task 8.2: Add HuggingFace LANGUAGES entry + extend buildAgentPrompt

**Files:**
- Modify: `web/src/lib/ecosystemData.ts`

- [ ] **Step 1: Inspect an existing simple LANGUAGES entry for shape reference**

```bash
sed -n '160,200p' web/src/lib/ecosystemData.ts
```

Note the structure: `{id, name, glyph, iconAdapter, managers: [{id, name, hint, quick, methods, persistent, verify, paths, tutorial}]}`.

- [ ] **Step 2: Add the HuggingFace entry**

In `web/src/lib/ecosystemData.ts`, find the `LANGUAGES` array. Append (or insert before `kubernetes` if that's the last entry — order is up to you, alphabetical or by ecosystem family):

```ts
  {
    id: 'huggingface', name: 'Hugging Face', glyph: 'HF', iconAdapter: 'huggingface',
    managers: [
      {
        id: 'huggingface-cli', name: 'huggingface-cli', hint: 'Official CLI',
        quick:      { lang: 'sh', body: 'export HF_ENDPOINT={URL}/huggingface\nhuggingface-cli download bert-base-uncased --local-dir ./bert' },
        methods: [
          { label: 'quickstart.method.env',     lang: 'sh', body: 'export HF_ENDPOINT={URL}/huggingface' },
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'HF_ENDPOINT={URL}/huggingface huggingface-cli download bert-base-uncased --local-dir ./bert' },
        ],
        persistent: { file: '~/.bashrc', lang: 'sh', body: 'export HF_ENDPOINT={URL}/huggingface' },
        verify:     { lang: 'sh', body: 'huggingface-cli download prajjwal1/bert-tiny --local-dir /tmp/bert-tiny && ls /tmp/bert-tiny' },
        paths: [
          { os: 'POSIX shells', path: '~/.bashrc, ~/.zshrc, ~/.profile' },
          { os: 'fish',         path: 'set -Ux HF_ENDPOINT {URL}/huggingface' },
        ],
        tutorial: [
          'huggingface-cli, transformers, datasets — every official tool respects HF_ENDPOINT.',
          'Set it once in your shell rc and forget about it; every download routes through Depsilo automatically.',
          'Gated models: pass your usual HF_TOKEN — Depsilo forwards it to upstream verbatim but does NOT cache the response.',
        ],
      },
      {
        id: 'transformers', name: 'transformers', hint: 'Python: AutoModel.from_pretrained()',
        quick:      { lang: 'py', body: 'import os; os.environ["HF_ENDPOINT"] = "{URL}/huggingface"\nfrom transformers import AutoModel\nm = AutoModel.from_pretrained("bert-base-uncased")' },
        methods: [
          { label: 'quickstart.method.env',     lang: 'sh', body: 'export HF_ENDPOINT={URL}/huggingface' },
          { label: 'quickstart.method.inline',  lang: 'py', body: 'import os; os.environ["HF_ENDPOINT"] = "{URL}/huggingface"\nfrom transformers import AutoModel\nm = AutoModel.from_pretrained("bert-base-uncased")' },
        ],
        persistent: { file: '~/.bashrc', lang: 'sh', body: 'export HF_ENDPOINT={URL}/huggingface' },
        verify:     { lang: 'py', body: 'from transformers import AutoModel\nm = AutoModel.from_pretrained("prajjwal1/bert-tiny")\nprint(m.config.model_type)' },
        paths: [
          { os: 'POSIX shells', path: '~/.bashrc, ~/.zshrc, ~/.profile' },
          { os: 'In-script',    path: 'os.environ before importing transformers' },
        ],
        tutorial: [
          'Set HF_ENDPOINT before importing transformers (or any HF library). The env var must be set at import time, not later.',
          'Works for transformers, datasets, evaluate, peft, accelerate — anything that uses huggingface_hub under the hood.',
        ],
      },
    ],
  },
```

- [ ] **Step 3: Extend `buildAgentPrompt`**

In the same file, find `export function buildAgentPrompt(endpoint: string): string`. Inside the prompt template, in the "Configure ONLY the detected tools" section, add a new line:

```
   huggingface: export HF_ENDPOINT=${endpoint}/huggingface
```

(Position it logically — e.g. after `nuget:` and before `conda:`. The exact ordering in the rest of the prompt doesn't matter; AI agents read the whole block.)

- [ ] **Step 4: Update the project-detection hint**

In the same template, find the bullet about detecting package managers:

```
2. Detect which package managers this project uses (requirements.txt,
   package.json, go.mod, Cargo.toml, pom.xml, Gemfile, composer.json,
   *.csproj, environment.yml, DESCRIPTION, Chart.yaml, etc.).
```

Add `pyproject.toml with [tool.transformers]` or just generic `huggingface_hub usage` if that's hard to pin down. A practical heuristic: any project that does `from transformers import` or imports `huggingface_hub` benefits. So just extend the list:

```
2. Detect which package managers this project uses (requirements.txt,
   package.json, go.mod, Cargo.toml, pom.xml, Gemfile, composer.json,
   *.csproj, environment.yml, DESCRIPTION, Chart.yaml, or `import transformers` /
   `import huggingface_hub` in Python source, etc.).
```

- [ ] **Step 5: Type-check + build**

```bash
cd web && npx tsc --noEmit && npm run build && cd -
```

Expected: both pass.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/ecosystemData.ts
git commit -m "feat(web): LANGUAGES entry + buildAgentPrompt covers HuggingFace tools"
```

### Task 8.3: i18n keys for HuggingFace

**Files:**
- Modify: `web/src/i18n/zh.ts`
- Modify: `web/src/i18n/en.ts`

- [ ] **Step 1: Locate the languages/managers namespace pattern**

```bash
grep -n "huggingface\|languages:" web/src/i18n/zh.ts | head -5
grep -n "huggingface-cli" web/src/i18n/zh.ts | head -3
```

The pattern for ecosystem labels is typically in `quickstart.managers.<manager-id>` (hint text) and `quickstart.languages.<lang-id>` (anchor text in mini-headers). Check what keys other ecosystems use as guides.

- [ ] **Step 2: Inspect what keys other ecosystems use**

Most labels are statically resolved from the `LANGUAGES` array's `name` and `hint` fields directly — they're not in i18n. The strings in `quickstart.method.env`, `quickstart.method.cmdline`, `quickstart.method.inline` ARE i18n keys; check that they exist for `inline`:

```bash
grep "method.inline" web/src/i18n/{zh,en}.ts
```

If `quickstart.method.inline` doesn't exist, add it.

- [ ] **Step 3: Add missing keys to zh.ts**

If `quickstart.method.inline` is missing, add it inside the existing `quickstart.method` namespace in `web/src/i18n/zh.ts`:

```ts
      method: {
        ...
        inline: '内联',
        ...
      },
```

If the LANGUAGES entry uses any i18n keys for tutorial bullets or aux labels (it shouldn't, based on the existing structure — tutorial bullets are plain strings), add them under `quickstart.huggingface.*` here. Otherwise no i18n additions are needed for v1.

- [ ] **Step 4: Mirror to en.ts**

```ts
      method: {
        ...
        inline: 'Inline',
        ...
      },
```

- [ ] **Step 5: Run lint**

```bash
make lint
```

Expected: i18n audit reports both locales at the same key count, no missing keys, no placeholder mismatch.

- [ ] **Step 6: Commit (only if any i18n changes were needed)**

```bash
git add web/src/i18n/zh.ts web/src/i18n/en.ts
git commit -m "i18n: add quickstart.method.inline (used by HuggingFace transformers entry)"
```

If no i18n change was needed, skip this commit.

---

# Phase 9 — Discover endpoint + agent-prompt sync

### Task 9.1: Add HuggingFace to discover endpoint metadata

**Files:**
- Modify: `internal/api/public/discover.go`

- [ ] **Step 1: Add to `ecosystemPurposes` map**

In `internal/api/public/discover.go`, find the `ecosystemPurposes` map (around line 30). Add:

```go
	"huggingface": {"/huggingface/", "Hugging Face — models + datasets (huggingface-cli, transformers, datasets)"},
```

- [ ] **Step 2: Extend the agent prompt template**

In the same file, find `func (h *DiscoverHandler) AgentPrompt`. Inside the prompt body (the `fmt.Sprintf` block), in the "Configure ONLY the detected tools" section, add:

```
   huggingface: export HF_ENDPOINT=%s/huggingface
```

You'll need to add one more `base` argument to the `fmt.Sprintf` arg list at the bottom of the function — it currently has 12 `base`s (one per ecosystem). Add a 13th in the right position to align with the new `%s/huggingface` placeholder.

Also update the detection hint paragraph the same way you did in Task 8.2 (mention `transformers` / `huggingface_hub`).

- [ ] **Step 3: Verify build + smoke**

```bash
go build ./...
make stop 2>/dev/null || true
make dev
sleep 3
curl -sf http://localhost:23333/api/v1/discover | python3 -m json.tool | grep -A2 huggingface
curl -sf http://localhost:23333/api/v1/agent-prompt | grep huggingface
make stop
```

Expected: `/discover` lists huggingface in ecosystems; `/agent-prompt` includes the `huggingface: export HF_ENDPOINT=...` line.

- [ ] **Step 4: Commit**

```bash
git add internal/api/public/discover.go
git commit -m "feat(api/discover): HuggingFace ecosystem entry + agent-prompt config line"
```

---

# Phase 10 — README + CHANGELOG + final verification

### Task 10.1: Update READMEs

**Files:**
- Modify: `README.md`
- Modify: `docs/README_zh.md`

- [ ] **Step 1: Update README.md "Supported Ecosystems" table**

Find the `## Supported Ecosystems` table. Add a row for Hugging Face:

```markdown
| **huggingface-cli** / transformers / datasets | Hugging Face Hub (models + datasets) | Server-side LFS follow |
```

(Match the table column style of the existing rows.)

- [ ] **Step 2: Add a note about cache size for HF workloads**

Below the "Quick Start" section or in a new sub-section, add:

```markdown
### Note for AI workloads

Hugging Face models are large — a single weights file can be 30-50 GB.
If you primarily use Depsilo as a model cache, raise the
`[cache] max_size_gb` setting in `config.toml` from the default 20 GB.
A practical starting point is 200 GB for teams using multiple LLMs.
```

- [ ] **Step 3: Mirror to docs/README_zh.md**

Add the same row (in Chinese) and the note translated:

```markdown
### AI 工作负载说明

Hugging Face 模型动辄几十 GB（单个权重文件可达 50 GB+）。
如果你主要用 Depsilo 缓存模型，建议把 `config.toml` 里的
`[cache] max_size_gb` 从默认 20 GB 提到 200 GB 起步。
```

- [ ] **Step 4: Update the AI-agent prompt block in both READMEs**

Both READMEs contain a copy-paste prompt that mirrors the Portal's "AI Agent" tab. Find the `huggingface` line that should be added — it goes alongside the other per-ecosystem config commands:

```
   huggingface: export HF_ENDPOINT=http://localhost:23333/huggingface
```

- [ ] **Step 5: Commit**

```bash
git add README.md docs/README_zh.md
git commit -m "docs(readme): add Hugging Face ecosystem + AI workload note + agent prompt"
```

### Task 10.2: CHANGELOG + full verification

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Add v0.5.0 CHANGELOG entry**

At the top of `CHANGELOG.md`, after the format note line and before the existing `## [0.4.0]` entry:

```markdown
## [0.5.0] - 2026-05-27

### Added
- HuggingFace Hub adapter — proxies models + datasets via `/huggingface/*`. Supports the full huggingface_hub client surface (huggingface-cli, transformers, datasets) by honoring `HF_ENDPOINT`. Server-side 302 following streams LFS blobs from cdn-lfs.huggingface.co inline; clients never see the signed CDN URL.
- New `[[huggingface.upstreams]]` config block with `hf-mirror.com` + `huggingface.co` defaults (LatencySelector picks the faster).
- Pass-through Authorization handling: gated models work when users provide their own `HF_TOKEN`, but auth'd responses are NOT cached (cross-user safety).
- New unit tests: 4 keyer + 5 resolver. New integration tests: 7 covering metadata, tree listing, dataset metadata, resolve (200 + 302), HEAD-only, unknown path. New Docker E2E (`test-docker-huggingface`) using `prajjwal1/bert-tiny`.
- Portal QuickStart: "Hugging Face" entry with `huggingface-cli` and `transformers` setup snippets.
- `/api/v1/discover` and `/api/v1/agent-prompt` know about HuggingFace.
```

- [ ] **Step 2: Full verification sweep**

```bash
make stop 2>/dev/null || true
make lint
make test-unit
make test-integration
cd web && npx tsc --noEmit && npm run build && cd -
```

Expected: every gate green.

- [ ] **Step 3: Optional E2E run**

If your environment has internet access to huggingface.co or hf-mirror.com:

```bash
make test-docker-huggingface
```

Skip if offline; the integration tests already covered the protocol flow against the mock.

- [ ] **Step 4: Verify the running server end-to-end**

```bash
make dev
sleep 3
# Status sanity
curl -sf http://localhost:23333/health
# HuggingFace upstream registered
curl -sf http://localhost:23333/api/v1/discover | python3 -m json.tool | grep huggingface
# Agent prompt includes HF line
curl -sf http://localhost:23333/api/v1/agent-prompt | grep -c "huggingface"
# Portal renders the new tab (manual browser check)
echo "Open http://localhost:23333/ and verify a 'Hugging Face' entry exists in the LanguageRail"
make stop
```

- [ ] **Step 5: Commit + summary**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): 0.5.0 — Hugging Face Hub adapter (13th ecosystem)"

# Optional summary log of commits in this work
git log --oneline $(cat /tmp/depsilo-hf-start.txt)..HEAD
```

- [ ] **Step 6: Push**

```bash
git push origin master
```

(Or open a PR if the project policy requires it — at v0.4.0, the user merged 30-commit PRs but smaller changes have gone direct to master. Use judgment.)

---

# Self-review

After completing all phases, run this checklist:

1. **Spec coverage** — every section of the spec should map to a task:
   - Spec §1 Overview → mentioned in Plan header
   - Spec §2 Protocol → Phase 2 (keyer), Phase 6 (mock + integration)
   - Spec §3 Routing → Phase 4 (handler), Phase 5 (server.go ecosystemDef)
   - Spec §4 Authentication → Phase 3 (headers.go AuthBypass), Phase 6 (auth integration test). **Note:** the v1 handler in Phase 4 is intentionally pass-through and does not yet wire cache integration; auth bypass is implemented but unused. Cache integration is the natural next step after this PR — see "Implementation note" below.
   - Spec §5 Configuration → Phase 1 (HuggingFaceConfig), Phase 5 (config.example.toml)
   - Spec §6 Cache key + TTL → Phase 2 (keyer)
   - Spec §7 Backend implementation → Phase 2-4 (the adapter package)
   - Spec §8 Frontend integration → Phase 8 (EcosystemIcon, LANGUAGES, i18n), Phase 9 (discover.go agent-prompt)
   - Spec §9 Testing strategy → Phase 2-4 (unit), Phase 6 (integration), Phase 7 (Docker E2E)
   - Spec §10 Scope boundaries → enforced implicitly by what's NOT in the file structure (no `lfs/objects/batch` endpoint, no Inference API handler, etc.)
   - Spec §11 Range request spike → Phase 1.1

2. **Placeholder scan** — searched for `TBD / FIXME / fill in / similar to`: none in implementation steps. The "v1 handler is pass-through" note in Phase 4 is a documented deferral, not a TBD.

3. **Type consistency** — `huggingface.New(cacheMgr, selector, cacheCfg, db)` signature matches the factory call in Phase 5. `PathKind` enum values referenced in Phase 2 tests match the constants defined in `keyer.go`. `CopyRequestHeaders` / `CopyResponseHeaders` signatures match between `headers.go` (Phase 3.1) and resolver.go (Phase 3.3).

**Self-review pass.**

# Implementation note: deferred work

Two items are mentioned in code/tests but intentionally NOT implemented in v1 of this plan. They're tracked for the follow-up plan after this one ships:

- **Cache integration in the handler**: Phase 4's handler.go has `_ = AuthBypass(c)`, `_ = CacheKey(parsed)`, `_ = TTLForRef(parsed.Ref)` lines — silenced unused-variable warnings for keyer outputs the resolver doesn't yet consume. The follow-up adds a thin wrapper that does `cacheMgr.Get(key)` → on miss, calls resolver → on success, `cacheMgr.Put(key, body, ttl)`. The handler's structure is ready for this; the integration is intentionally separate so the v1 ships with a clean pass-through baseline that the integration tests already validate.
- **Range request caching**: see spec §7.6 and the spike outcome in `internal/adapter/huggingface/SPIKE.md`. If huggingface_hub uses Range for full downloads (spike Outcome B), the handler's cache integration needs to strip Range on upstream calls. If not (Outcome A), pass-through is the simplest correct path.

---

# Execution handoff

**Plan complete and saved to `docs/plans/2026-05-27-huggingface-implementation.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
