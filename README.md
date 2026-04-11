<div align="center">

<img src="docs/brand/logo-stacked-dark.svg" alt="Depsilo" width="200">

**One cache for all your dependencies.**

Deploy in minutes. LAN-speed installs for 12 package managers.<br>
Single binary, ~50 MB memory, zero complexity.

[![Go 1.21+](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Docker Pulls](https://img.shields.io/docker/pulls/depsilo/depsilo)](https://hub.docker.com/r/depsilo/depsilo)
[![Release](https://img.shields.io/github/v/release/depsilo/depsilo)](https://github.com/depsilo/depsilo/releases)

[Website](https://depsilo.com) &bull; [English](README.md) &bull; [中文](docs/README_zh.md)

</div>

---

## Why Depsilo?

Your team runs `pip install`, `npm install`, `go get`, `cargo build` hundreds of times a day. Every install hits the public internet, burning bandwidth and slowing CI. If an upstream goes down, your builds break.

**Depsilo sits between your team and the public registries.** First request fetches from upstream; every request after that is served from local cache at LAN speed.

| | Nexus / Artifactory | Depsilo |
|---|---|---|
| Deploy time | 30+ min, Java runtime, config wizards | `docker run` — done |
| Memory | 2+ GB | ~50 MB |
| Binary | WAR/JAR + JVM | Single static binary |
| Config | XML/YAML, web wizard, LDAP, roles... | One TOML file |
| Ecosystems | Many (with per-ecosystem setup) | 12, unified config |

> Depsilo is **not** a full artifact repository. It's a caching proxy — purpose-built to be fast, light, and invisible.

## Supported Ecosystems

| Manager | Ecosystem | Proxy Type |
|---------|-----------|------------|
| **pip** / uv / Poetry | Python | URL rewrite |
| **apt** | Debian / Ubuntu | Passthrough |
| **npm** / yarn / pnpm | Node.js | URL rewrite |
| **go get** | Go Modules | Passthrough |
| **cargo** | Rust | config.json rewrite |
| **maven** / gradle | Java / Kotlin | Passthrough |
| **gem** / bundler | Ruby | Passthrough |
| **composer** | PHP | metadata-url rewrite |
| **dotnet** | .NET (NuGet) | service index rewrite |
| **conda** | Data science | Passthrough |
| **Rscript** | R (CRAN) | Passthrough |
| **helm** | Kubernetes | Passthrough |

## Quick Start

```bash
docker run -d --name depsilo -p 23333:23333 -v depsilo-data:/app/data depsilo/depsilo:latest
```

Open `http://localhost:23333` — the portal shows copy-paste config for all 12 ecosystems.

Default admin login: `admin` / `admin` at `/admin`.

<details>
<summary><b>docker-compose</b></summary>

```yaml
services:
  depsilo:
    image: depsilo/depsilo:latest
    ports:
      - "23333:23333"
    volumes:
      - ./data:/app/data
      - ./config.toml:/app/config.toml
    restart: unless-stopped
```

```bash
curl -O https://raw.githubusercontent.com/depsilo/depsilo/master/config.example.toml
mv config.example.toml config.toml
docker compose up -d
```

</details>

<details>
<summary><b>Build from source</b></summary>

```bash
git clone https://github.com/depsilo/depsilo.git
cd depsilo
make build
cp config.example.toml config.toml
./bin/depsilo
```

Requires Go 1.21+ and Node.js 20+.

</details>

## Usage Examples

```bash
# Python
pip install requests -i http://YOUR_HOST:23333/pypi/simple/ --trusted-host YOUR_HOST

# Node.js
npm config set registry http://YOUR_HOST:23333/npm/

# Go
export GOPROXY=http://YOUR_HOST:23333/go,direct

# Rust
# ~/.cargo/config.toml
# [source.crates-io]
# replace-with = "depsilo"
# [source.depsilo]
# registry = "sparse+http://YOUR_HOST:23333/crates/"
```

See the built-in **Quick Start** page for all 12 ecosystems, including Maven, Composer, NuGet, Conda, CRAN, and Helm.

## Key Features

**Caching Engine**
- **Singleflight** — 100 concurrent requests for the same package = 1 upstream fetch
- **Stale-while-revalidate** — expired cache is served immediately while refreshing in the background
- **Offline fallback** — if all upstreams are down, stale cache keeps your builds running
- **Streaming** — large packages (torch ~2 GB) are never buffered in memory

**Upstream Management**
- Multiple upstreams per ecosystem with priority or latency-based selection
- Per-upstream HTTP proxy support
- Automatic health checks with failover
- Circuit breaker for unhealthy upstreams

**Storage**
- Local filesystem (default) or S3-compatible (MinIO, AWS S3)
- LRU eviction when cache exceeds configured threshold
- Per-ecosystem cache size tracking

**Observability**
- Web portal with quick-start guides and real-time cache event stream
- Admin dashboard with trend charts, storage visualization, upstream latency monitoring
- Prometheus `/metrics` endpoint
- Access logs with filtering and export
- Audit trail for admin operations

**Security**
- JWT authentication for admin API
- API token management (hash-only storage)
- Package allow/deny rules
- SQLite WAL mode for safe concurrent access

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
lru_threshold = 90           # trigger LRU cleanup at 90% capacity

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

See [`config.example.toml`](config.example.toml) for the full reference.

## Roadmap

- [x] 12 ecosystem proxies
- [x] Web UI (portal + admin dashboard)
- [x] Real-time cache event stream (SSE)
- [x] Storage visualization & package search
- [x] Prometheus metrics
- [x] Audit logs
- [x] Package allow/deny rules
- [ ] Docker Registry proxy
- [ ] Cluster mode (multi-node shared cache)
- [ ] LDAP / SSO integration
- [ ] Bandwidth savings reports

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

[MIT License](LICENSE)

---

<div align="center">

[depsilo.com](https://depsilo.com)

</div>
