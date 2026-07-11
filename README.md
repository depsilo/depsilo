<div align="center">

<img src="docs/brand/logo-stacked-dark.svg" alt="Depsilo" width="200">

**Supply-chain enforcement layer for your package installs.**

14 ecosystems behind one proxy. Quarantine fresh versions, block known-malicious packages,<br>
serve installs at LAN speed. Single binary, ~50 MB memory, deploys in 10 minutes.

[![Go 1.21+](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Docker Pulls](https://img.shields.io/docker/pulls/depsilo/depsilo)](https://hub.docker.com/r/depsilo/depsilo)
[![Release](https://img.shields.io/github/v/release/depsilo/depsilo)](https://github.com/depsilo/depsilo/releases)

[Website](https://depsilo.com) &bull; [English](README.md) &bull; [中文](docs/README_zh.md)

</div>

---

## What Depsilo does

Every `pip install` / `npm install` / `cargo build` your team runs goes through
Depsilo. On the way through, three things happen:

1. **Cache** — first request fetches from upstream; every request after that
   serves from local disk at LAN speed. Offline-tolerant.
2. **Quarantine** — versions younger than a configurable per-ecosystem window
   (default: 7 days for npm, 3 days for pip / cargo / maven / ...) get
   blocked at the proxy. Mitigates self-propagating worms (Shai-Hulud-class)
   that get yanked within hours but not before damage is done.
3. **Audit** — every block / bypass / approve writes an event you can review
   in the admin UI, query via API, or fire to a webhook (Slack / 钉钉 /
   企微 / Feishu).

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

All thresholds are per-ecosystem configurable. Set any to `0` to disable.
Add per-package overrides via the allow list (glob / pin / range syntax).

## Quick start

### One-liner (Linux / macOS)

```bash
curl -fsSL https://depsilo.com/install.sh | bash
```

### Docker

```bash
docker run -d --name depsilo -p 23333:23333 -v depsilo-data:/app/data depsilo/depsilo:latest
```

Open `http://localhost:23333` for the portal — it ships copy-paste config
for all 14 ecosystems. Default admin login: `admin` / `admin` at `/admin`.

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
make build
./bin/depsilo serve
```

Requires Go 1.21+ and Node.js 20+.

## Supply-chain enforcement

The reason Depsilo exists. Three primitives, all open source.

### Minimum release age (quarantine)

A version published less than the configured threshold ago gets a `451`
with an actionable error body:

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
mode = "block"            # block | serve_last_eligible
fail_closed = true        # missing upstream timestamp → block

[supply_chain.min_release_age]
default  = "0"
pypi     = "3d"
npm      = "7d"
cargo    = "3d"
# ...

# Optional per-package bypass list — three syntaxes
# allow = [
#   "npm:@your-org/internal-*",      # glob
#   "pip:requests==2.32.3",          # exact pin
#   "npm:react>=18.0.0",             # version range
# ]
```

### Audit trail

Every quarantine decision (block / serve_eligible / bypass / approve /
revoke) writes a `QuarantineEvent` to the DB. The admin UI shows the
live event stream with ecosystem / action / package filters and a
30-second auto-refresh.

### Webhook on block

Configure a Slack / DingTalk / WeCom / Feishu / generic webhook in
**Settings → Webhooks**. Enable `quarantine_blocked` and the channel
fires the moment a quarantine event lands.

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
| `depsilo_warmup(ecosystem, packages[])` | Pre-fetch packages (requires admin token) |

### 3. Copy-paste prompt (any agent)

For non-MCP agents, paste the prompt the portal renders or fetch the
live host-substituted version:

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

- **Singleflight** — 100 concurrent requests for the same package = 1
  upstream fetch. The other 99 wait on the first.
- **Stale-while-revalidate** — expired cache is served immediately while
  refreshed in the background. No `pip install` ever blocks waiting for
  a metadata refresh.
- **Offline fallback** — if all configured upstreams are down, stale
  cache keeps your builds running.
- **Streaming** — large packages (torch ~2 GB) are piped through
  `io.Copy`, never buffered in memory.
- **Multi-upstream priority or latency selection** — configure mirrors
  per ecosystem; Depsilo picks the fastest healthy one, fails over on
  outages, and runs a circuit breaker on repeatedly-failing endpoints.
- **Per-upstream HTTP proxy** — different ecosystems can route through
  different egress proxies if your network demands it.

## Observability

- Web portal at `/` with copy-paste configuration for every ecosystem
  and live cache event stream
- Admin dashboard at `/admin` with trend charts, storage visualization,
  per-upstream latency monitoring, supply-chain quarantine event log
- Prometheus `/metrics`
- Structured access logs with filtering + CSV export
- Audit trail for every admin action

## Security

- JWT authentication for the admin API
- API tokens stored as hash-only
- SQLite WAL mode for safe concurrent access
- Configurable RBAC roles
- For SSO / OIDC, deploy behind a reverse proxy like
  [oauth2-proxy](https://oauth2-proxy.github.io/oauth2-proxy/),
  [Authelia](https://www.authelia.com/), or
  [Pomerium](https://www.pomerium.com/) — these are mature and pair
  cleanly with Depsilo's auth model

## Roadmap

Shipped:
- [x] 14 ecosystem proxies
- [x] Web portal + admin dashboard
- [x] Minimum release age quarantine (Shai-Hulud mitigation)
- [x] Quarantine audit log + admin approve / revoke
- [x] Webhook fire on quarantine events
- [x] Prometheus metrics
- [x] Audit logs
- [x] Package allow / deny rules
- [x] CLI: `serve` / `status` / `doctor` / `warmup` / `flush` / `init-agent`
- [x] Native MCP server for AI agents
- [x] OSV vulnerability scanning + Settings → Security dashboard

Next up:
- [ ] Known-malicious blocklist (OSV malicious + GitHub Advisory malware
      feed, hard block with 24h auto-expiring override)
- [ ] CRA-mode SBOM workflow (CycloneDX + SPDX, signed, per-project)
- [ ] Freeze / golden snapshot mode (reproducible build set, immune to
      upstream poisoning)
- [ ] Tamper detection (per-version hash, alert on upstream republish)
- [ ] Signed releases (cosign keyless via CI)
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
