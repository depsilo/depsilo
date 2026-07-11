<div align="center">

<img src="docs/brand/logo-stacked-dark.svg" alt="Depsilo" width="200">

**Supply-chain enforcement layer for your package installs.**

14 package ecosystems plus Docker OCI behind one proxy. Quarantine fresh versions,<br>
block known-malicious packages,
detect silent republishing, and serve installs at LAN speed. Single binary, ~50 MB memory.

[![Go 1.25.6+](https://img.shields.io/badge/Go-1.25.6+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Docker Pulls](https://img.shields.io/docker/pulls/depsilo/depsilo)](https://hub.docker.com/r/depsilo/depsilo)
[![Release](https://img.shields.io/github/v/release/depsilo/depsilo)](https://github.com/depsilo/depsilo/releases)

[Website](https://depsilo.com) &bull; [English](README.md) &bull; [中文](docs/README_zh.md)

</div>

---

## What Depsilo does

Every `pip install` / `npm install` / `cargo build` your team runs goes through
Depsilo. On the way through, four things happen:

1. **Cache** — first request fetches from upstream; every request after that
   serves from local disk at LAN speed. Offline-tolerant.
2. **Enforce** — versions younger than a configurable per-ecosystem window are
   quarantined. Imported exact-version and all-version OSV MAL records are
   hard-blocked for eight covered ecosystems.
3. **Verify** — immutable artifacts get a first-seen SHA-256. A background
   refresh mismatch preserves the cached copy and raises a critical alert; an
   LRU miss still alerts but cannot restore bytes that were already evicted.
4. **Audit** — block / bypass / approve / revoke / override / tamper decisions
   write events for the admin UI and API. Block and tamper signals can also
   fire a webhook (Slack / 钉钉 / 企微 / Feishu).

It's a proxy on the request path that **refuses to serve** based on
supply-chain policy. Operators get one config, one place to enforce
"the org doesn't install fresh dependencies for N days." Devs use their
package manager exactly the way they always have.

## Supported ecosystems

| Manager | Ecosystem | Quarantine threshold default |
|---------|-----------|--------|
| **pip** / uv / Poetry | Python | 3 days |
| **apt** | Debian / Ubuntu | 0 (curated upstream) |
| **npm** / yarn / pnpm | Node.js | 7 days |
| **go get** | Go modules | 0 (checksum DB protects) |
| **cargo** | Rust | 3 days |
| **maven** / gradle | Java / Kotlin | 3 days |
| **gem** / bundler | Ruby | 3 days |
| **composer** | PHP | 3 days |
| **dotnet** | .NET (NuGet) | 3 days |
| **conda** | Data science | 3 days |
| **Rscript** | R (CRAN) | 3 days |
| **helm** | Kubernetes | 3 days |
| **apk** | Alpine | 3 days |
| **huggingface-cli** / transformers / datasets | HF Hub | 3 days |

Thresholds are configurable for the 13 ecosystems with publish-time resolvers;
set one to `0` to disable its age check. APT is not connected to the quarantine
gate, and Go has no publish-time resolver, so both must remain `0`. Add
per-package overrides via the allow list (glob / pin / range syntax).

## Quick start

### One-liner (Linux / macOS)

```bash
curl -fsSL https://depsilo.com/install.sh | bash
```

### Docker

```bash
docker run -d --name depsilo -p 23333:23333 \
  -v depsilo-state:/root/.depsilo \
  -e DEPSILO_DATABASE_DSN=/root/.depsilo/data/depsilo.db \
  -e DEPSILO_STORAGE_PATH=/root/.depsilo/data/cache \
  depsilo/depsilo:latest
```

Open `http://localhost:23333` for the portal — it ships copy-paste config
for all 14 ecosystems. On first run, complete the setup wizard; then sign in at
`/admin` with `admin` / `admin` and change the password immediately. The named
volume persists the generated config; the two absolute path overrides keep the
SQLite database and local cache in that same volume after the wizard rewrites
`config.toml`.

The current wizard does not generate a Docker registry block or a Hugging Face
upstream. Add those sections from `config.example.toml` and restart Depsilo
before testing those two install surfaces.

### Manual download

```bash
# Grab the binary archive for your platform from GitHub Releases
tar xzf depsilo_*_linux_amd64.tar.gz
cp config.example.toml config.toml
./depsilo serve --port 23333
```

### Build from source

```bash
git clone https://github.com/depsilo/depsilo.git
cd depsilo
cd web && npm ci && cd ..
make build
./bin/depsilo serve
```

Requires Go 1.25.6 or newer and Node.js 20+.

## Supply-chain enforcement

The reason Depsilo exists. Three primitives, all open source.

Release status: `v0.8.0` includes minimum release age. The malicious
blocklist and tamper detection are implemented on `master` and will ship in
the next release.

### Minimum release age (quarantine)

A version published less than the configured threshold ago gets a `451`
with a structured error body:

```
HTTP/1.1 451 Unavailable For Legal Reasons
Content-Type: application/json

{
  "code": "QUARANTINED",
  "message": "version 99.0.0 of lodash was published 1d ago, which is younger than the configured 7d minimum release age for npm",
  "ecosystem": "npm",
  "package": "lodash",
  "version": "99.0.0"
}
```

The admin can approve any specific `(ecosystem, package, version)` from
the **Supply-Chain Quarantine** page in the admin UI — with a mandatory
audit reason. Approvals are permanent until explicitly revoked.

```toml
[supply_chain]
mode = "block"            # currently supported mode
fail_closed = true        # block on not-found/unsupported; upstream outages still allow

# Optional per-package bypass list — three syntaxes
# allow = [
#   "npm:@your-org/internal-*",      # glob
#   "pypi:requests==2.32.3",         # exact pin
#   "npm:react>=18.0.0",             # version range
# ]

[supply_chain.min_release_age]
# "default" is only a fallback for unknown/future ecosystem keys; the
# supported ecosystems retain their built-in values unless listed explicitly.
default  = "0"
pypi     = "3d"
npm      = "7d"
cargo    = "3d"
# ...
```

### Known-malicious blocklist

Depsilo syncs the OSV malicious-packages dataset every 6 hours by default for
npm, PyPI, Cargo, RubyGems, Composer, NuGet, Go, and Maven. It imports explicit
affected versions and all-version advisories; bounded ranges without an
explicit version list are skipped because range evaluation is not implemented.
A match is refused with HTTP 451 `MALICIOUS_BLOCKED` before the minimum-age and
allow-list checks. Existing cache bytes become unreachable through the gate but
remain on disk until LRU or operator cleanup. A false-positive override expires
after 24 hours and cannot be extended in place; continuing it requires a new
audited override.

```toml
[supply_chain.blocklist]
enabled = true
sync_interval = "6h"
# mirror_url = "https://osv-vulnerabilities.storage.googleapis.com"
# proxy = "http://127.0.0.1:7890"
```

### Tamper detection

For immutable package artifacts, Depsilo records a streaming SHA-256 on first
fetch. A natural background refresh that returns different bytes keeps serving
the first-seen cached copy, refuses to store the replacement, and emits a
critical `tamper_detected` event and webhook. Detection is passive and
alert-only: it adds no probe traffic and does not return a 451. If LRU has
already removed the first-seen bytes, a cache miss stores and serves the
re-fetched bytes before verification; Depsilo retains the old hash baseline
and alerts, but cannot restore the evicted copy.

```toml
[supply_chain.tamper_detection]
enabled = true
```

### Audit trail

Quarantine, malware, override, approval, revoke, and tamper decisions write a
`QuarantineEvent` to the DB. The admin UI shows the live event stream with
ecosystem / action / package filters and a 30-second auto-refresh.

### Webhooks

Configure a Slack / DingTalk / WeCom / Feishu / generic webhook in
**Settings → Webhooks**. Quarantine and malware blocks fire immediately;
tamper mismatches use critical severity.

## Use with AI coding agents

Three increasingly automated ways to give an AI coding agent control
of Depsilo. Pick whichever fits your stack:

### 1. Bootstrap a project (one command)

```bash
cd my-project/
depsilo init-agent
```

Writes `CLAUDE.md` / `AGENTS.md` / `.cursorrules` (auto-detects which
based on your project) with a marker-bracketed Depsilo section. Any AI
agent you open the project with reads its own instruction file at
startup and knows that **this project uses Depsilo at
`http://localhost:23333`**.

Idempotent — re-running updates only the content inside the markers.

### 2. Native MCP for Claude Code / Cursor / other MCP clients

Depsilo ships a built-in Model Context Protocol server at `POST /mcp`
(JSON-RPC 2.0 over Streamable HTTP). MCP clients get structured tool
calls instead of parsing free-form prompts. Point your client at
`http://localhost:23333/mcp`. Available tools:

| Tool | Effect |
| --- | --- |
| `depsilo_status` | Service health, 24h request totals, cache hit rate, configured ecosystems |
| `depsilo_doctor` | End-to-end diagnosis with actionable hints |
| `depsilo_configure(ecosystem)` | Returns shell / env / config / verify snippets |
| `depsilo_search(query, ecosystem?, limit?)` | LIKE query against cached packages |
| `depsilo_recent(limit?, only_miss?)` | Tail of cache events for debugging |
| `depsilo_warmup(ecosystem, packages[])` | Returns an admin warmup request template; MCP execution is not wired yet |

### 3. Copy-paste prompt (any agent)

For non-MCP agents, use the project-integration prompt shown in the Portal:

```bash
curl -sf http://localhost:23333/api/v1/integration-prompt
```

For a local developer-machine bootstrap prompt instead, use:

```bash
curl -sf http://localhost:23333/api/v1/agent-prompt
```

The agent detects which package managers your project uses,
reconfigures each, and verifies the cache is reachable.

## Configuration

```toml
[server]
port = 23333

[storage]
type = "local"              # local | s3
path = "./data/cache"

[cache]
max_size_gb   = 20
ttl_index     = "5m"        # metadata refresh interval
ttl_blob      = "72h"       # package file TTL
lru_threshold = 90          # trigger LRU cleanup at 90% capacity

[[pypi.upstreams]]
name     = "tuna"
url      = "https://pypi.tuna.tsinghua.edu.cn"
priority = 1

[[pypi.upstreams]]
name     = "official"
url      = "https://pypi.org"
priority = 2
proxy    = "http://127.0.0.1:7890"    # optional per-upstream proxy
```

On the first start after upgrading, Depsilo imports ordinary ecosystem upstreams into the database and records the active ecosystems. After that seed, Admin and the database are authoritative: deleting or editing an upstream is not overwritten by later restarts. Adding upstreams in config for a previously inactive supported ecosystem activates that ecosystem on the next restart. Docker registries and extra indexes remain config-owned and are not managed by Admin Upstream CRUD.

See [`config.example.toml`](config.example.toml) for the full reference,
including the `[supply_chain]` quarantine block.

### CLI flags + environment

```bash
depsilo serve --port 18080 --host 0.0.0.0 --log-level debug
DEPSILO_SERVER_PORT=18080 depsilo serve
DEPSILO_CONFIG=/etc/depsilo.toml depsilo serve
```

Precedence (highest wins): CLI flag → env variable → config file → built-in
default. Run `depsilo serve --help` for the full list.

> **AI workloads:** Hugging Face models are large — a single weights file
> can be 30-50 GB. If you primarily use Depsilo as a model cache, raise
> `[cache] max_size_gb` from the default 20 GB. A practical starting
> point is 200 GB for teams using multiple LLMs.

## Caching engine

What's happening under the hood when a `pip install` lands on Depsilo:

- **Request coalescing** — 100 concurrent requests for the same package = 1
  upstream fetch. The other 99 wait on the first.
- **Stale-while-revalidate** — expired cache is served immediately while
  refreshed in the background. No `pip install` ever blocks waiting for
  a metadata refresh.
- **Offline fallback for cached artifacts** — if all configured upstreams are
  down, existing stale entries can still be served; uncached requests fail.
- **Streaming** — large packages (torch ~2 GB) are piped through
  `io.Copy`, never buffered in memory.
- **Multi-upstream priority selection** — configure mirrors per ecosystem;
  Depsilo chooses the highest-priority healthy source. Health checks and
  request outcomes affect selection for subsequent requests.
- **Per-upstream HTTP proxy** — different ecosystems can route through
  different egress proxies if your network demands it.

## Observability

- Web portal at `/` with copy-paste configuration and a service-health monitor
- Admin dashboard at `/admin` with trend charts, storage visualization,
  per-upstream latency monitoring, supply-chain quarantine event log

See [Admin Control Plane](docs/admin-control-plane.md) for Settings persistence, live Upstream mutation, Principal permissions, response semantics, and operator verification.

- Prometheus `/metrics`
- Structured proxy-request logs with filtering
- Audited supply-chain decisions

## Security

- JWT authentication for the admin API
- API tokens stored as hash-only
- SQLite WAL mode for safe concurrent access
- Basic `admin` / `readonly` user roles; richer RBAC and SSO stay external
- For SSO / OIDC, deploy behind a reverse proxy like
  [oauth2-proxy](https://oauth2-proxy.github.io/oauth2-proxy/),
  [Authelia](https://www.authelia.com/), or
  [Pomerium](https://www.pomerium.com/) — these are mature and pair
  cleanly with Depsilo's auth model

## Roadmap

Released through v0.8.0:
- [x] 14 package-ecosystem proxies + Docker OCI
- [x] Web portal + admin dashboard
- [x] Minimum release age quarantine (Shai-Hulud mitigation)
- [x] Quarantine audit log + admin approve / revoke
- [x] Webhook fire on quarantine blocks
- [x] Prometheus metrics
- [x] Audit logs
- [x] Package allow / deny rules
- [x] CLI: `serve` / `status` / `doctor` / `warmup` / `flush` / `init-agent`
- [x] Native MCP server for AI agents
- [x] OSV vulnerability scanning + Settings → Security dashboard
- [x] CycloneDX + SPDX source and container-image SBOM release artifacts (unsigned)

Landed on `master` (unreleased):
- [x] Known-malicious blocklist (OSV MAL advisories + 24h audited override)
- [x] Tamper detection (first-seen SHA-256 + critical alert)

Next up:
- [ ] Signed releases (cosign keyless via CI)
- [ ] Freeze / golden snapshot mode (reproducible build set, immune to
      upstream poisoning)
- [ ] CRA-mode SBOM workflow (supplier, hashes, licenses, dependency graph)
- [ ] Helm chart

## Contributing

Contributions welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for
guidelines. The codebase is documented at three levels:
[`docs/DIRECTION.md`](docs/DIRECTION.md) (product direction),
[`docs/adr/`](docs/adr/) (architecture decisions), and the per-package
README comments in `internal/`.

## License

[MIT License](LICENSE)

---

<div align="center">

[depsilo.com](https://depsilo.com)

</div>
