[![Go](https://img.shields.io/badge/Go-1.26.5+-00ADD8?logo=go&logoColor=white)](https://go.dev)
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

- 🚀 **14 个常规生态代理缓存 + Docker OCI** — pip, apt, npm, Go, cargo, Maven, RubyGems, Composer, NuGet, Conda, CRAN, Helm, Alpine, Hugging Face
- 🛡️ **最小发布年龄隔离** — 默认关闭；可按生态启用新版本冷却窗口
- ⛔ **已知恶意包阻断** — 每 6 小时同步 8 个生态的 OSV MAL 精确版本/全版本记录，命中返回 451
- 🔐 **篡改检测** — 首次抓取记录 SHA-256；后台刷新不一致时保留缓存，LRU 淘汰后重取会缓存/返回新字节并告警
- 🧾 **审计与 Webhook** — block / bypass / approve / revoke / override 均可追踪；阻断和篡改信号可推送到 Slack / 钉钉 / 企微 / 飞书
- ⚡ **并发请求合并** — 100 个相同 miss 请求只触发 1 次回源
- 🌐 **多上游源**，支持为每个源单独配置 HTTP 代理
- 🔄 **自动健康检查**，按优先级选择健康上游；健康状态影响后续请求
- 💾 **本地文件系统或 S3 兼容**存储
- 📊 **Web 管理界面** + Prometheus `/metrics`
- 🪶 **约 50 MB 内存**，单二进制文件，默认 SQLite
- 🐳 **一行命令 Docker 部署**

版本说明：`v0.8.0` 已发布最小发布年龄隔离；恶意包阻断与篡改检测已合入
`master`，将在下一版本正式发布。

## 🚀 快速开始

### Docker（推荐）

```bash
# Docker Hub
docker run -d \
  --name depsilo \
  -p 23333:23333 \
  -v depsilo-state:/root/.depsilo \
  -e DEPSILO_DATABASE_DSN=/root/.depsilo/data/depsilo.db \
  -e DEPSILO_STORAGE_PATH=/root/.depsilo/data/cache \
  --restart unless-stopped \
  depsilo/depsilo:latest

```

当前 GHCR package 需要登录后拉取；匿名部署请使用上面的 Docker Hub 镜像。
首次打开 Portal 时，先用 `docker logs depsilo` 取得启动日志中的一次性
bootstrap token，再在 Setup Wizard 中设置管理员用户名和强密码。上述 volume 持久化向导生成的配置，
两个绝对路径环境变量保证向导重写 `config.toml` 后，SQLite 数据库和本地缓存
仍落在同一个 volume 中。

当前向导不会生成 Docker registry block 或 Hugging Face upstream。测试这两个
安装入口前，先按 `config.example.toml` 手工补齐对应配置并重启 Depsilo。

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
      - DEPSILO_AUTH_JWT_SECRET=${DEPSILO_AUTH_JWT_SECRET:?set a random secret}
      - DEPSILO_ADMIN_USERNAME=${DEPSILO_ADMIN_USERNAME:-admin}
      - DEPSILO_ADMIN_PASSWORD=${DEPSILO_ADMIN_PASSWORD:?set a strong password}
    restart: unless-stopped
```

```bash
# 下载示例配置并启动
curl -O https://raw.githubusercontent.com/depsilo/depsilo/master/config.example.toml
mv config.example.toml config.toml
export DEPSILO_AUTH_JWT_SECRET="$(openssl rand -hex 32)"
export DEPSILO_ADMIN_PASSWORD='请设置一个强初始密码'
docker-compose up -d
```

### 从源码构建

```bash
git clone https://github.com/depsilo/depsilo.git
cd depsilo
make setup
make build
cp config.example.toml config.toml
export DEPSILO_AUTH_JWT_SECRET="$(openssl rand -hex 32)"
export DEPSILO_ADMIN_PASSWORD='请设置一个强初始密码'
./bin/depsilo serve
```

源码构建需要 Go 1.26.5+ 与 Node.js 22.22.0+。

服务默认启动在 `http://localhost:23333`。
不再提供预设管理员密码；互动安装由向导创建，无头部署则从
`DEPSILO_ADMIN_*` 环境变量创建。

本地开发也可以直接运行 `make run`，它会完成编译并启动服务。未设置
`DEPSILO_AUTH_JWT_SECRET` 时，首次运行会生成权限为 0600 的
`.dev-jwt-secret`，之后重启继续复用。项目根目录没有 `config.toml` 时不会
再强制传入缺失路径，而是继续使用 CLI 的用户目录配置或内置默认配置。
生产部署仍须显式提供自己的 JWT 密钥。需要强制使用自定义配置路径时，请运行
`DEPSILO_CONFIG=/etc/depsilo.toml make run`。后台开发服务会让实际监听端口与健康
检查端口保持一致；需要改端口时使用 `PORT=18080 make dev`。

Depsilo 也可以为本机或团队构建机提供独立的 ccache / sccache 编译缓存。sccache
入口只是满足官方客户端所需的窄 WebDAV 兼容层，不是 `sccache-dist` 调度器，也
不是 S3 API。存储隔离、构建凭据、客户端配置和真实客户端回归命令见
[编译缓存指南](compile-cache.md)。

### AI 工作负载说明

Hugging Face 模型动辄几十 GB。当前适配器缓存普通 HTTP 下载路径（单文件约
50 GB 以内）；超过客户端该限制的 Xet 原生文件暂不支持。
如果你主要用 Depsilo 缓存模型，建议把 `config.toml` 里的
`[cache] max_size_gb` 从默认 20 GB 提到 200 GB 起步。

## 🤖 与 AI Agent 一起使用

有三种接入方式，按自动化程度选择：

1. 在项目根目录运行 `depsilo init-agent`，自动更新 `CLAUDE.md`、`AGENTS.md`
   或 `.cursorrules` 中由 Depsilo 管理的说明区块。
2. MCP 客户端连接 `http://localhost:23333/mcp`，调用状态、诊断、配置、搜索和
   最近请求工具。预热工具当前只返回 Admin API 请求模板，不会直接执行。
3. 非 MCP Agent 获取提示词。Portal 展示的是修改项目构建/CI 配置的版本：

```bash
curl -sf http://localhost:23333/api/v1/integration-prompt
```

若要配置当前开发机，则使用 bootstrap 版本：

```bash
curl -sf http://localhost:23333/api/v1/agent-prompt
```

两个端点都会按当前访问地址生成内容。公网恢复路径取决于具体包管理器：很多客户端
只接受单一源，需要保留手工回滚说明，不能假设 Depsilo 宕机时会自动切换。

## ⚙️ 配置

将 `config.example.toml` 复制为 `config.toml` 并按需编辑：

```toml
[server]
host = "0.0.0.0"
port = 23333

[database]
driver = "sqlite"           # 当前版本仅支持 sqlite
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

[supply_chain]
min_release_age_enabled = false  # 默认关闭；改为 true 后启用推荐冷却阈值

[auth]
enabled    = true
# 非 loopback 监听使用此占位值时将拒绝启动；推荐设置 DEPSILO_AUTH_JWT_SECRET。
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

配置单上游 HTTP 代理后，重定向产物域名由该代理负责解析。请在代理出口侧默认
阻断回环、链路本地和私网目标；只有私有制品库确实位于这些网段时才放行。
Depsilo 直连模式会在进程内执行同等网络边界检查。

`min_release_age_enabled` 只控制“最新版本冷却”门禁。默认开启的 OSV 已知恶意包
封锁和仅告警的篡改检测不受该开关影响。

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

**PyTorch 构建索引：** Depsilo 默认提供
`/pypi-torch/{channel}/simple/`，无需为每个新 CUDA 版本等待 Depsilo
更新。将 PyTorch 官方安装地址中 `/whl/` 后的通道填入 URL，即可分别缓存
CPU、CUDA 和 ROCm wheel。各通道拥有独立缓存，不会与清华 PyPI 的候选
版本自动混合；索引声明的下载地址会由 Depsilo 签名后代理：

```bash
PYTORCH_CHANNEL=cpu
pip install torch torchvision torchaudio \
  --index-url http://你的IP:23333/pypi-torch/${PYTORCH_CHANNEL}/simple/ \
  --trusted-host 你的IP
```

内置上游使用被动探测：没有客户端请求时不会访问 PyTorch。如需关闭：

```toml
[extra_index_presets]
disabled = ["pytorch"]
```

**Docker 构建**（无需修改 Dockerfile）：

```bash
DOCKER_BUILDKIT=1 docker build --network host \
  --build-arg PIP_INDEX_URL=http://127.0.0.1:23333/pypi/simple/ \
  --build-arg PIP_TRUSTED_HOST=127.0.0.1 \
  -t myapp .
```

`trusted-host` / `PIP_TRUSTED_HOST` 仅用于上面的 plain HTTP 示例；HTTPS 部署
必须省略它们并使用正常的 CA 证书校验。

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

已随 `v0.8.0` 发布：
- [x] 14 个常规生态代理 + Docker OCI
- [x] Web 管理界面（门户 + 管理后台）
- [x] Prometheus 监控指标
- [x] Docker 部署
- [x] 审计日志与包级 Allow/Deny 规则
- [x] 最小发布年龄隔离与审批流
- [x] CycloneDX + SPDX 源码和容器镜像 SBOM 发布附件

已合入 `master`，尚未发布：
- [x] OSV 已知恶意包阻断与 24 小时审计 override
- [x] 不可变制品篡改检测与 critical Webhook
- [x] Cosign keyless 签名、镜像 SBOM attestation 与发布冒烟测试

下一步：
- [ ] Freeze / Golden Snapshot
- [ ] CRA 完整 SBOM
- [ ] Helm Chart

## 🔓 开源承诺

Depsilo 是 MIT 许可证、自托管、单二进制部署。文档面向运维落地，重点
说明如何部署、接入和执行供应链策略。

当前开源能力包括 14 种生态代理、缓存与流量分析、上游源管理与健康检查、
审计日志、包级 Allow/Deny 规则引擎、安全情报 dashboard（OSV / CVE 集中
视图 + 决策工作流）、公开的 Depsilo 自身 CycloneDX/SPDX release SBOM、Webhook
告警、Prometheus 指标，以及已落地的供应链策略功能（最小发布年龄、恶意包阻断、
tamper detection）。运行时的每项目 SBOM 导出当前属于 Pro；Freeze / Snapshot、
CRA 完整 SBOM 和签名发布仍在路线图中。

## 🤝 参与贡献

欢迎贡献！详见 [CONTRIBUTING.md](../CONTRIBUTING.md)。

## 📄 许可证

[MIT License](../LICENSE)
