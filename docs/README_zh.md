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
  <em>依赖安装链路上的供应链策略执行层</em>
</p>

---

<!-- 截图：Web UI 仪表盘 -->
<!-- TODO: 首次部署后添加截图 -->

## ✨ 特性

- 🚀 **14 个生态代理缓存** — pip, apt, npm, Go, cargo, Maven, RubyGems, Composer, NuGet, Conda, CRAN, Helm, Alpine, Hugging Face
- 🛡️ **最小发布年龄隔离** — 按生态配置新版本冷却窗口，阻断刚发布的高风险版本
- 🧾 **审计与 Webhook** — block / bypass / approve 都可追踪，并可推送到 Slack / 钉钉 / 企微 / 飞书
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

### AI 工作负载说明

Hugging Face 模型动辄几十 GB（单个权重文件可达 50 GB+）。
如果你主要用 Depsilo 缓存模型，建议把 `config.toml` 里的
`[cache] max_size_gb` 从默认 20 GB 提到 200 GB 起步。

## 🤖 与 AI Agent 一起使用

把下面这段提示词粘进 Hermes、OpenClaw、Claude Code、Cursor 等任意 AI 编码 agent。Agent 会自动检测项目用了哪些包管理器，把对应的 install 流量都切到你本地的 Depsilo 缓存，无需安装任何 plugin / skill。

> 如果 Depsilo 部署在其他地址，把 `http://localhost:23333` 替换成实际 URL。Portal 首页有 "AI Agent" tab，会自动用当前部署地址渲染这段提示词，一键复制。

```text
This workspace has a local dependency cache called Depsilo at http://localhost:23333.
It caches packages from 14 ecosystems and serves them at LAN speed.

Before running any package install in this workspace:

1. Verify it's reachable: curl -sf http://localhost:23333/health

2. Detect which package managers this project uses (requirements.txt,
   package.json, go.mod, Cargo.toml, pom.xml, Gemfile, composer.json,
   *.csproj, environment.yml, DESCRIPTION, Chart.yaml, etc.).

3. Configure ONLY the detected tools. Pick from:

   pip:      pip config set global.index-url http://localhost:23333/pypi/simple/
   npm:      npm config set registry http://localhost:23333/npm/
   go:       go env -w GOPROXY=http://localhost:23333/go,direct
   cargo:    visit http://localhost:23333/ and copy the Cargo block to ~/.cargo/config.toml
   maven:    visit http://localhost:23333/ and copy the Maven mirror block to ~/.m2/settings.xml
   gem:      bundle config mirror.https://rubygems.org http://localhost:23333/rubygems/
   composer: composer config -g repo.packagist composer http://localhost:23333/composer/
   nuget:    dotnet nuget add source http://localhost:23333/nuget/v3/index.json -n depsilo
   conda:    add channel http://localhost:23333/conda/ to ~/.condarc
   helm:     helm repo add depsilo http://localhost:23333/helm/
   R/CRAN:   options(repos = c(CRAN = "http://localhost:23333/cran/")) in ~/.Rprofile
   huggingface: export HF_ENDPOINT=http://localhost:23333/huggingface

4. Run install commands normally — they auto-route through Depsilo.

If Depsilo is down, tools fall back to public registries — installs still
work, just not cached. Don't waste effort on retry logic for Depsilo itself.
```

Depsilo 不可达时，所有包管理器都会自动回退到公网 registry —— 安装命令照样成功，只是不缓存而已。

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

升级后的首次启动会把普通生态的上游源导入数据库，并记录已激活生态。完成首次导入后，Admin 与数据库成为权威来源；通过 Admin 删除或修改的上游源不会在重启时被配置文件覆盖。若要启用此前未激活的受支持生态，需要先在配置文件中添加该生态的上游源并重启。Docker Registry 与额外索引仍由配置文件管理，不属于 Admin 上游源 CRUD。

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

Settings 持久化、上游源实时变更、Principal 权限、响应语义和运维验证见 [Admin 控制面说明](admin-control-plane.md)。

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
- [ ] 审计日志（开源，UI 完善中）
- [ ] 包级 Allow/Deny 规则（开源，已实现）
- [ ] 最小发布年龄 / 恶意包阻断（开源，规划中）

## 🔓 开源承诺

Depsilo 是 MIT 许可证、自托管、单二进制部署。文档面向运维落地，重点
说明如何部署、接入和执行供应链策略。

当前开源能力包括 14 种生态代理、缓存与流量分析、上游源管理与健康检查、
审计日志、包级 Allow/Deny 规则引擎、安全情报 dashboard（OSV / CVE 集中
视图 + 决策工作流）、SBOM 导出（CycloneDX + SPDX）、Webhook 告警、
Prometheus 指标，以及供应链策略功能（最小发布年龄、恶意包阻断、
freeze/snapshot、tamper detection 等按路线图落地）。

## 🤝 参与贡献

欢迎贡献！详见 [CONTRIBUTING.md](../CONTRIBUTING.md)。

## 📄 许可证

[MIT License](../LICENSE)
