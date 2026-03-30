[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](../LICENSE)
[![Docker Pulls](https://img.shields.io/docker/pulls/depsilo/depsilo)](https://hub.docker.com/r/depsilo/depsilo)
[![Release](https://img.shields.io/github/v/release/depsilo/depsilo)](https://github.com/depsilo/depsilo/releases)

**[English](../README.md) | [中文](README_zh.md)**

<p align="center">
  <img src="brand/logo-stacked-dark.svg" alt="Depsilo" height="120">
  <br>
  <strong>依仓 · Depsilo</strong>
  <br>
  <em>一个缓存，搞定所有依赖</em>
</p>

---

<!-- 截图：Web UI 仪表盘 -->
<!-- TODO: 首次部署后添加截图 -->

## ✨ 特性

- 🚀 **pip + apt 代理缓存** — npm / cargo / Go 即将支持
- ⚡ **Singleflight** — 100 个并发请求只触发 1 次回源
- 🌐 **多上游源**，支持为每个源单独配置 HTTP 代理
- 🔄 **自动健康检查**，基于延迟自动切换
- 💾 **本地文件系统或 S3 兼容**存储
- 📊 **Web 管理界面** + Prometheus `/metrics`
- 🪶 **约 50 MB 内存**，单二进制文件，默认 SQLite
- 🐳 **一行命令 Docker 部署**

## 🚀 快速开始

### Docker（推荐）

```bash
# Docker Hub
docker run -d \
  --name depsilo \
  -p 23333:23333 \
  -v depsilo-data:/app/data \
  --restart unless-stopped \
  depsilo/depsilo:latest

# 或者使用 GitHub Container Registry
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
# 下载示例配置并启动
curl -O https://raw.githubusercontent.com/depsilo/depsilo/master/config.example.toml
mv config.example.toml config.toml
docker-compose up -d
```

### 从源码构建

```bash
git clone https://github.com/depsilo/depsilo.git
cd depsilo
make build
cp config.example.toml config.toml
./bin/depsilo
```

服务默认启动在 `http://localhost:23333`。
默认管理员账号：`admin` / `admin` — 首次登录后请修改密码。

## ⚙️ 配置

将 `config.example.toml` 复制为 `config.toml` 并按需编辑：

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

# S3 配置（type = "s3" 时生效）
# bucket   = "depsilo"
# endpoint = "http://minio:9000"
# region   = "us-east-1"
# access_key = ""
# secret_key = ""

[cache]
max_size_gb  = 20
ttl_index    = "5m"         # PyPI simple / APT Release 等元数据
ttl_blob     = "72h"        # wheel / .deb 文件
lru_threshold = 90          # 使用率超过此百分比触发 LRU 清理

[auth]
enabled    = true
jwt_secret = "change-me-in-production"
token_ttl  = "168h"         # 7 天

[[pypi.upstreams]]
name     = "tuna"
url      = "https://pypi.tuna.tsinghua.edu.cn"
priority = 1

[[pypi.upstreams]]
name     = "official"
url      = "https://pypi.org"
priority = 2
# proxy  = "http://your-proxy:7890"   # 可选，单独给此上游配 HTTP 代理

[[apt.upstreams]]
name     = "tuna"
url      = "https://mirrors.tuna.tsinghua.edu.cn"
priority = 1

[[apt.upstreams]]
name     = "aliyun"
url      = "https://mirrors.aliyun.com"
priority = 2
```

## 📦 使用方法

### pip

**临时使用：**

```bash
pip install <package> -i http://你的IP:23333/pypi/simple/ --trusted-host 你的IP
```

**永久配置**（`~/.config/pip/pip.conf`）：

```ini
[global]
index-url = http://你的IP:23333/pypi/simple/
trusted-host = 你的IP
```

**Docker 构建**（无需修改 Dockerfile）：

```bash
DOCKER_BUILDKIT=1 docker build --network host \
  --build-arg PIP_INDEX_URL=http://127.0.0.1:23333/pypi/simple/ \
  --build-arg PIP_TRUSTED_HOST=127.0.0.1 \
  -t myapp .
```

### apt

**添加源配置文件**（`/etc/apt/sources.list.d/depsilo.list`）：

```
deb http://你的IP:23333/apt/ubuntu noble main restricted universe multiverse
deb http://你的IP:23333/apt/ubuntu noble-updates main restricted universe multiverse
```

**一键替换现有源：**

```bash
sudo sed -i "s|https\?://[^/]*/ubuntu|http://你的IP:23333/apt/ubuntu|g" /etc/apt/sources.list
sudo apt update
```

## 🖥 Web 界面

打开 `http://你的IP:23333` 访问用户门户：

- **快速开始** — 一键复制 pip、apt、Docker 配置命令
- **包浏览** — 搜索和浏览已缓存的包
- **实时动态** — 实时查看缓存命中/未命中
- **服务状态** — 上游源健康状态和缓存统计

管理后台地址：`http://你的IP:23333/admin`（需登录）。

## 📊 监控指标

Prometheus 指标暴露在 `/metrics` 端点：

```
depsilo_requests_total
depsilo_request_duration_seconds
depsilo_upstream_requests_total
depsilo_cache_size_bytes
depsilo_cache_files_total
```

## 🗺 路线图

- [x] pip 代理缓存
- [x] apt 代理缓存
- [x] Web 管理界面（门户 + 管理后台）
- [x] Prometheus 监控指标
- [x] Docker 部署
- [ ] npm 支持
- [ ] cargo 支持
- [ ] Go modules 支持
- [ ] Pro：审计日志
- [ ] Pro：包白名单/黑名单

## 💼 Pro 版本

| | 社区版 | Pro 版 |
|---|---|---|
| pip + apt 缓存 | ✅ | ✅ |
| Web 界面 + 监控 | ✅ | ✅ |
| npm / cargo / Go | ✅（开发中） | ✅ |
| 审计日志 | — | ✅ |
| 包白名单/黑名单 | — | ✅ |
| PostgreSQL + S3 | — | ✅ |
| 优先支持 | — | ✅ |
| 价格 | 免费 | $9/月 |

👉 [depsilo.com/#pricing](https://depsilo.com/#pricing)

## 🤝 参与贡献

欢迎贡献！详见 [CONTRIBUTING.md](../CONTRIBUTING.md)。

## 📄 许可证

[MIT License](../LICENSE)
