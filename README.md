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

<!-- Screenshot: Web UI Dashboard -->
<!-- TODO: Add screenshot after first deployment -->

## ✨ Features

- 🚀 **pip + apt proxy cache** — npm / cargo / Go coming soon
- ⚡ **Singleflight** — 100 concurrent requests = 1 upstream fetch
- 🌐 **Multi-upstream** with per-source HTTP proxy
- 🔄 **Automatic health checks** and latency-based failover
- 💾 **Local filesystem or S3-compatible** storage
- 📊 **Web UI dashboard** + Prometheus `/metrics`
- 🪶 **~50 MB memory**, single binary, SQLite default
- 🐳 **One-line Docker** deployment

## 🚀 Quick Start

### Docker (recommended)

```bash
# Docker Hub
docker run -d \
  --name depsilo \
  -p 23333:23333 \
  -v depsilo-data:/app/data \
  --restart unless-stopped \
  depsilo/depsilo:latest

# or GitHub Container Registry
docker run -d \
  --name depsilo \
  -p 23333:23333 \
  -v depsilo-data:/app/data \
  --restart unless-stopped \
  ghcr.io/depsilo/depsilo:latest
```

### docker-compose

```yaml
# docker-compose.yml
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
# Copy the example config and start
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

The server starts on `http://localhost:23333` by default.
Default admin credentials: `admin` / `admin` — change after first login.

## ⚙️ Configuration

Copy `config.example.toml` to `config.toml` and edit as needed:

```toml
[server]
host = "0.0.0.0"
port = 23333

[database]
driver = "sqlite"           # sqlite | postgres
dsn    = "./data/depsilo.db"

[storage]
type = "local"              # local | s3
path = "./data/cache"

# S3 config (when type = "s3")
# bucket   = "depsilo"
# endpoint = "http://minio:9000"
# region   = "us-east-1"
# access_key = ""
# secret_key = ""

[cache]
max_size_gb  = 20
ttl_index    = "5m"         # PyPI simple / APT Release metadata
ttl_blob     = "72h"        # wheel / .deb files
lru_threshold = 90          # trigger LRU eviction above this %

[auth]
enabled    = true
jwt_secret = "change-me-in-production"
token_ttl  = "168h"         # 7 days

[[pypi.upstreams]]
name     = "tuna"
url      = "https://pypi.tuna.tsinghua.edu.cn"
priority = 1

[[pypi.upstreams]]
name     = "official"
url      = "https://pypi.org"
priority = 2
# proxy  = "http://your-proxy:7890"   # optional per-upstream proxy

[[apt.upstreams]]
name     = "tuna"
url      = "https://mirrors.tuna.tsinghua.edu.cn"
priority = 1

[[apt.upstreams]]
name     = "aliyun"
url      = "https://mirrors.aliyun.com"
priority = 2
```

## 📦 Usage

### pip

**One-off install:**

```bash
pip install <package> -i http://YOUR_IP:23333/pypi/simple/ --trusted-host YOUR_IP
```

**Permanent config** (`~/.config/pip/pip.conf`):

```ini
[global]
index-url = http://YOUR_IP:23333/pypi/simple/
trusted-host = YOUR_IP
```

**Docker build** (no Dockerfile changes needed):

```bash
DOCKER_BUILDKIT=1 docker build --network host \
  --build-arg PIP_INDEX_URL=http://127.0.0.1:23333/pypi/simple/ \
  --build-arg PIP_TRUSTED_HOST=127.0.0.1 \
  -t myapp .
```

### apt

**Add source file** (`/etc/apt/sources.list.d/depsilo.list`):

```
deb http://YOUR_IP:23333/apt/ubuntu noble main restricted universe multiverse
deb http://YOUR_IP:23333/apt/ubuntu noble-updates main restricted universe multiverse
```

**Replace existing sources in-place:**

```bash
sudo sed -i "s|https\?://[^/]*/ubuntu|http://YOUR_IP:23333/apt/ubuntu|g" /etc/apt/sources.list
sudo apt update
```

## 🖥 Web UI

Open `http://YOUR_IP:23333` to access the portal:

- **Quick Start** — copy-paste config commands for pip, apt, Docker
- **Package Browse** — search and explore cached packages
- **Live Stream** — watch cache hits/misses in real time
- **Service Status** — upstream health and cache stats

Admin dashboard at `http://YOUR_IP:23333/admin` (login required).

## 📊 Metrics

Prometheus metrics are exposed at `/metrics`:

```
depsilo_requests_total
depsilo_request_duration_seconds
depsilo_upstream_requests_total
depsilo_cache_size_bytes
depsilo_cache_files_total
```

## 🗺 Roadmap

- [x] pip proxy cache
- [x] apt proxy cache
- [x] Web UI (portal + admin dashboard)
- [x] Prometheus metrics
- [x] Docker deployment
- [ ] npm support
- [ ] cargo support
- [ ] Go modules support
- [ ] Pro: Audit logs
- [ ] Pro: Package allow/deny rules

## 💼 Pro Version

| | Community | Pro |
|---|---|---|
| pip + apt cache | ✅ | ✅ |
| Web UI + metrics | ✅ | ✅ |
| npm / cargo / Go | ✅ (when available) | ✅ |
| Audit logs | — | ✅ |
| Package allow/deny rules | — | ✅ |
| PostgreSQL + S3 | — | ✅ |
| Priority support | — | ✅ |
| Price | Free | $9/mo |

👉 [depsilo.com/#pricing](https://depsilo.com/#pricing)

## 🤝 Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## 📄 License

[MIT License](LICENSE)
