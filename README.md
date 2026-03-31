<div align="center">

<img src="docs/brand/icon-dark.svg" alt="Depsilo" width="80" height="80">

### Depsilo

*One cache for all your dependencies*

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Docker Pulls](https://img.shields.io/docker/pulls/depsilo/depsilo)](https://hub.docker.com/r/depsilo/depsilo)
[![Release](https://img.shields.io/github/v/release/depsilo/depsilo)](https://github.com/depsilo/depsilo/releases)

[English](README.md) · [中文](docs/README_zh.md)

</div>

---

## What is Depsilo?

A lightweight dependency proxy cache gateway. Single binary, ~50 MB memory, deploy in minutes. Caches packages from upstream registries so your team gets LAN-speed installs.

## Supported Package Managers

| Manager | Language / Ecosystem | Route | URL Rewriting |
|---------|---------------------|-------|---------------|
| **PyPI** | Python (pip / uv / Poetry) | `/pypi/` | Yes (HTML) |
| **APT** | Debian / Ubuntu | `/apt/` | No |
| **npm** | Node.js (npm / yarn / pnpm) | `/npm/` | Yes (JSON) |
| **Go Modules** | Go | `/go/` | No |
| **Cargo** | Rust | `/crates/` | Yes (config.json) |
| **Maven** | Java / Kotlin / Gradle | `/maven/` | No |
| **RubyGems** | Ruby (bundler / gem) | `/rubygems/` | No |
| **Composer** | PHP (Packagist) | `/composer/` | Yes (metadata-url) |
| **NuGet** | .NET (dotnet) | `/nuget/` | Yes (service index) |
| **Conda** | Python data science | `/conda/` | No |
| **CRAN** | R | `/cran/` | No |
| **Helm** | Kubernetes charts | `/helm/` | No |

## Features

- **12 package managers** in one service
- **Singleflight** deduplication — 100 concurrent requests = 1 upstream fetch
- **Multi-upstream** with per-source HTTP proxy and priority/latency-based selection
- **Automatic health checks** with latency monitoring and failover
- **Local filesystem or S3-compatible** storage backend
- **Web UI** — portal with quick-start guides, package browser, real-time cache stream
- **Admin dashboard** — trend charts, storage treemap, upstream latency sparklines
- **Prometheus** `/metrics` endpoint
- **Single binary**, SQLite default, ~50 MB memory
- **One-line Docker** deployment

## Quick Start

### Docker (recommended)

```bash
docker run -d \
  --name depsilo \
  -p 23333:23333 \
  -v depsilo-data:/app/data \
  --restart unless-stopped \
  depsilo/depsilo:latest
```

### docker-compose

```yaml
version: '3.8'
services:
  depsilo:
    image: depsilo/depsilo:latest
    ports:
      - "23333:23333"
    volumes:
      - ./data:/app/data
      - ./config.toml:/app/config.toml
    environment:
      - DEPSILO_CONFIG=/app/config.toml
    restart: unless-stopped
```

```bash
curl -O https://raw.githubusercontent.com/depsilo/depsilo/master/config.example.toml
mv config.example.toml config.toml
docker-compose up -d
```

### Build from source

```bash
git clone https://github.com/depsilo/depsilo.git
cd depsilo
make build
cp config.example.toml config.toml
./bin/depsilo
```

The server starts on `http://localhost:23333`. Default admin: `admin` / `admin`.

## Usage

Open `http://YOUR_IP:23333` for the portal with copy-paste config commands for all 12 package managers.

### pip

```bash
pip install <package> -i http://YOUR_IP:23333/pypi/simple/ --trusted-host YOUR_IP
```

### npm

```bash
npm install <package> --registry http://YOUR_IP:23333/npm/
```

### Go

```bash
GOPROXY=http://YOUR_IP:23333/go,direct go get <package>
```

### Maven (~/.m2/settings.xml)

```xml
<settings>
  <mirrors>
    <mirror>
      <id>depsilo</id>
      <mirrorOf>central</mirrorOf>
      <url>http://YOUR_IP:23333/maven/</url>
    </mirror>
  </mirrors>
</settings>
```

### Cargo (~/.cargo/config.toml)

```toml
[source.crates-io]
replace-with = "depsilo"

[source.depsilo]
registry = "sparse+http://YOUR_IP:23333/crates/"
```

### More

See the **Quick Start** page in the web UI for apt, RubyGems, Composer, NuGet, Conda, CRAN, Helm, and Docker build configurations.

## Configuration

Copy `config.example.toml` to `config.toml`. Key sections:

```toml
[server]
host = "0.0.0.0"
port = 23333

[storage]
type = "local"       # local | s3
path = "./data/cache"

[cache]
max_size_gb   = 20
ttl_index     = "5m"    # metadata
ttl_blob      = "72h"   # package files
lru_threshold = 90

# Each adapter has its own [[<type>.upstreams]] section
[[pypi.upstreams]]
name     = "tuna"
url      = "https://pypi.tuna.tsinghua.edu.cn"
priority = 1

[[npm.upstreams]]
name     = "npmmirror"
url      = "https://registry.npmmirror.com"
priority = 1
```

See `config.example.toml` for the full reference with all 12 adapter configurations.

## Web UI

**Portal** (`/`) — no login required:
- Quick Start — copy-paste config for all package managers
- Package Browse — search and explore cached packages
- Live Stream — real-time cache hit/miss events (SSE)
- Service Status — upstream health and statistics

**Admin** (`/admin`) — login required:
- Dashboard — trend charts, hit rate, latency sparklines
- Cache Management — storage treemap, search, cleanup
- Upstreams — add/edit/delete upstream sources
- Access Logs — request history with filtering
- Users — user and API token management
- Settings — cache policy, storage, auth configuration

## Metrics

Prometheus metrics at `/metrics`:

```
depsilo_requests_total
depsilo_request_duration_seconds
depsilo_upstream_requests_total
depsilo_cache_size_bytes
depsilo_cache_files_total
```

## Roadmap

- [x] 12 package manager proxies (pip, apt, npm, Go, Cargo, Maven, RubyGems, Composer, NuGet, Conda, CRAN, Helm)
- [x] Web UI (portal + admin dashboard)
- [x] Real-time cache event stream (SSE)
- [x] Package search and browsing
- [x] Storage distribution treemap
- [x] Upstream latency monitoring
- [x] Prometheus metrics
- [x] Docker deployment
- [ ] Docker Registry proxy
- [ ] Audit logs
- [ ] Package allow/deny rules

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

[MIT License](LICENSE)
