# CLAUDE.md — Depsilo 项目全量指南

> 本文件是 Claude Code 的唯一权威参考。实现时严格遵循此文档，不得自行发明架构或引入未列出的依赖。

---

## 一、项目概述

**Depsilo** 是一个**供应链执行层（Supply-Chain Enforcement Layer）**：站在包安装请求路径上的自托管代理，依据供应链策略实时**拒绝服务**（而非扫描后报告），同时提供代理缓存加速。用 Go 编写，单二进制部署。

> 2026-06-30 战略转向（ADR-0004）：缓存是"楔子"（让代理被装进去的理由），执行层是核心价值。战略权威文档是 `docs/DIRECTION.md` + `docs/adr/0003`、`docs/adr/0004`，本文件侧重实现规范。

### 核心价值
- **供应链强制执行**：最小发布年龄隔离、OSV MAL-* 已知恶意包 451 阻断、不可变制品篡改告警、包 allow/deny 规则与 Webhook 通知——"组织级强制"是与客户端个人配置的核心差异
- 支持 14 个常规生态路由 + 独立 Docker OCI `/v2/`（共 15 个 install surfaces），共享缓存引擎、存储后端和 Web UI
- 多上游源支持，自动健康检查；每个上游源可单独配置 HTTP 代理
- 统一 Web 入口：用户门户（无需登录）+ 管理后台（需登录）
- 新增生态通过 Adapter 实现接入，不影响核心逻辑（项目没有运行时插件系统）

### 已支持的生态

| #   | 协议       | 路径         | 语言/生态                   | 代理类型           |
| --- | ---------- | ------------ | --------------------------- | ------------------ |
| 1   | PyPI       | `/pypi/`     | Python (pip / uv / Poetry)  | URL 重写（HTML）   |
| 2   | APT        | `/apt/`      | Debian / Ubuntu             | Passthrough        |
| 3   | npm        | `/npm/`      | Node.js (npm / yarn / pnpm) | URL 重写（JSON）   |
| 4   | Go Modules | `/go/`       | Go                          | Passthrough        |
| 5   | Cargo      | `/crates/`   | Rust                        | config.json 重写   |
| 6   | Maven      | `/maven/`    | Java / Kotlin / Gradle      | Passthrough        |
| 7   | RubyGems   | `/rubygems/` | Ruby (bundler / gem)        | Passthrough        |
| 8   | Composer   | `/composer/` | PHP (Packagist)             | metadata-url + dist mirrors 重写 |
| 9   | NuGet      | `/nuget/`    | .NET (dotnet)               | service index 重写 |
| 10  | Conda      | `/conda/`    | Python 数据科学             | Passthrough        |
| 11  | CRAN       | `/cran/`     | R                           | Passthrough        |
| 12  | Helm       | `/helm/`     | Kubernetes Charts           | Passthrough        |
| 13  | Alpine     | `/alpine/`   | Alpine Linux (apk)          | Passthrough        |
| 14  | Docker     | `/v2/`       | 容器镜像（Registry V2）     | Passthrough（token 代理） |
| 15  | HuggingFace| `/huggingface/` | AI 模型/数据集           | Passthrough        |

### 定位
与通用制品库（Nexus / Artifactory / Artifact Keeper）**共存而非竞争**：可部署在既有 registry 前面做隔离墙（chainable proxy）。只做"唯有请求路径上的代理才能做"的事；生态数量刻意封顶（不追 45+），详见 ADR-0004。

---

## 二、技术栈（不得替换）

| 组件       | 选型                                  | 说明                          |
| ---------- | ------------------------------------- | ----------------------------- |
| 语言       | Go 1.25.12                            | 以 `go.mod` 为准，标准库优先 |
| HTTP 框架  | **Gin** (`github.com/gin-gonic/gin`)  | v1.9+                         |
| ORM        | **GORM** (`gorm.io/gorm`)             | 当前仅接入 SQLite driver     |
| DB 迁移    | GORM `AutoMigrate`                    | 启动时自动执行                |
| S3 客户端  | `github.com/aws/aws-sdk-go-v2`        | 兼容 MinIO                    |
| 配置       | `github.com/spf13/viper`              | 读取 TOML 配置文件            |
| 日志       | `go.uber.org/zap`                     | 结构化日志                    |
| 并发合并   | 自研 inflight + `singleflight`        | miss 流式合并 + 后台刷新去重 |
| 上游容错   | `net/http` + 健康检查                 | 按优先级选健康上游；状态影响后续请求 |
| Metrics    | `github.com/prometheus/client_golang` | 暴露 `/metrics`               |
| 前端       | React 19 + TypeScript + Vite          | 见第五节                      |
| 前端样式   | Tailwind CSS v4 + 自研 "Instrument" 设计系统 | 见第五节（已不用 shadcn/ui） |
| 前端打包   | Go `embed`                            | 编译进二进制                  |

---

## 三、项目目录结构

以下是简化结构示意，不是完整文件清单；实际结构以工作树为准。新增代码应优先
归入既有模块，不要无理由新增顶层目录。

> 下面的树是 2026-05 的核心骨架，此后新增了这些包（放置新代码时优先归入既有包）：
> `internal/`：`quarantine/`（供应链隔离，见 4.21）、`accesslog/`（rollup 聚合）、`audit/`、`rules/`、`security/`（OSV）、`notify/`（Webhook）、`license/` + `trial/` + `entitlement/`（Pro 门控）、`sbom/`、`cli/`（13 个命令）、`server/`（装配）、`tray/`、`prompts/`、`version/`；
> `internal/adapter/`：`alpine/`、`docker/`、`huggingface/`、`packagekey/`（包名/版本解析）；
> `cmd/`：`depsilo/`（CLI + server 主入口）、`depsilo-tray/`（托盘）；`testground/docker-<eco>/`（E2E）。

```
depsilo/
├── cmd/
│   └── server/
│       └── main_server.go           # 兼容服务器入口（主入口为 cmd/depsilo）
├── internal/
│   ├── config/
│   │   ├── config.go                # 配置结构体定义
│   │   └── loader.go                # viper 加载逻辑
│   ├── adapter/
│   │   ├── interface.go             # Adapter interface 定义
│   │   ├── accesslog.go             # 访问日志记录 + 包名提取
│   │   ├── pypi/                    # Python（URL 重写）
│   │   │   ├── handler.go
│   │   │   ├── rewriter.go
│   │   │   └── keyer.go
│   │   ├── apt/                     # Debian/Ubuntu（Passthrough）
│   │   │   ├── handler.go
│   │   │   └── keyer.go
│   │   ├── npm/                     # Node.js（URL 重写）
│   │   │   ├── handler.go
│   │   │   ├── rewriter.go
│   │   │   └── keyer.go
│   │   ├── goproxy/                 # Go Modules（Passthrough）
│   │   │   ├── handler.go
│   │   │   └── keyer.go
│   │   ├── cargo/                   # Rust（config.json 重写）
│   │   │   ├── handler.go
│   │   │   └── keyer.go
│   │   ├── maven/                   # Java/Kotlin（Passthrough）
│   │   │   ├── handler.go
│   │   │   └── keyer.go
│   │   ├── rubygems/                # Ruby（Passthrough）
│   │   │   ├── handler.go
│   │   │   └── keyer.go
│   │   ├── composer/                # PHP（metadata-url 重写）
│   │   │   ├── handler.go
│   │   │   ├── rewriter.go
│   │   │   └── keyer.go
│   │   ├── nuget/                   # .NET（service index 重写）
│   │   │   ├── handler.go
│   │   │   ├── rewriter.go
│   │   │   └── keyer.go
│   │   ├── conda/                   # Python 数据科学（Passthrough）
│   │   │   ├── handler.go
│   │   │   └── keyer.go
│   │   ├── cran/                    # R（Passthrough）
│   │   │   ├── handler.go
│   │   │   └── keyer.go
│   │   └── helm/                    # Kubernetes（Passthrough）
│   │       ├── handler.go
│   │       └── keyer.go
│   ├── cache/
│   │   ├── manager.go               # 核心：单飞 + 回源 + 写缓存 + 流式透传
│   │   ├── store.go                 # Storage interface
│   │   ├── local.go                 # 本地文件系统实现
│   │   └── s3.go                    # S3 兼容存储实现
│   ├── upstream/
│   │   ├── pool.go                  # 上游连接池，持有 http.Client（含 proxy）
│   │   ├── selector.go              # 当前仅按优先级选择健康上游
│   │   └── health.go                # 后台健康检查（定时 HEAD 请求）
│   ├── db/
│   │   ├── models.go                # GORM 模型定义
│   │   └── repository.go            # 数据访问层（Repository pattern）
│   ├── api/
│   │   ├── router.go                # 路由注册总入口
│   │   ├── admin/
│   │   │   ├── dashboard.go         # GET /api/v1/admin/dashboard
│   │   │   ├── cache.go             # 缓存管理 CRUD
│   │   │   ├── upstream.go          # 上游源 CRUD
│   │   │   ├── user.go              # 用户管理 CRUD
│   │   │   ├── token.go             # API Token 管理
│   │   │   └── settings.go          # 系统设置读写
│   │   ├── public/
│   │   │   └── stats.go             # GET /api/v1/stats（公开，无需登录）
│   │   ├── auth.go                  # POST /api/v1/auth/login|logout|refresh
│   │   └── metrics.go               # GET /metrics（Prometheus）
│   └── middleware/
│       ├── auth.go                  # JWT 验证中间件
│       ├── logger.go                # Zap 请求日志中间件
│       └── recovery.go              # panic recovery
├── web/                             # 前端源码
│   ├── package.json
│   ├── vite.config.ts
│   └── src/
│       ├── main.tsx
│       ├── App.tsx                  # 路由：/ → Portal，/admin → AdminApp
│       ├── portal/                  # 用户门户（无需登录）
│       │   ├── PortalApp.tsx
│       │   ├── pages/
│       │   │   ├── QuickStart.tsx   # 快速开始（pip/apt 配置命令）
│       │   │   └── Monitor.tsx      # 服务状态与上游健康
│       │   └── components/
│       │       ├── CodeBlock.tsx    # 带复制按钮的代码块
│       │       ├── ConfigurePane.tsx# 当前生态配置面板
│       │       └── EcosystemCatalog.tsx # 左侧技术栈目录
│       └── admin/                   # 管理后台（需登录）
│           ├── AdminApp.tsx
│           ├── pages/
│           │   ├── Dashboard.tsx
│           │   ├── CacheManage.tsx
│           │   ├── Upstreams.tsx
│           │   ├── AccessLogs.tsx
│           │   ├── Users.tsx
│           │   └── Settings.tsx
│           └── components/
│               ├── MainLayout.tsx   # 侧边栏 + topbar 布局
│               ├── NowStrip.tsx
│               ├── TrendsCard.tsx
│               ├── WebhookTab.tsx
│               └── ProRequiredCallout.tsx
├── web/dist/                        # 前端构建产物（gitignore，由 make build 生成）
├── config.example.toml
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── go.mod
```

---

## 四、后端实现规范

### 4.1 配置文件结构（config.example.toml）

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
ttl_index    = "1h"         # 示例配置；内置缺省值为 5m
ttl_blob     = "72h"        # wheel / .deb 文件
lru_threshold = 90          # 使用率超过此百分比触发 LRU 清理

[auth]
enabled    = true
jwt_secret = "change-me-in-production"
token_ttl  = "168h"         # 7天

[[pypi.upstreams]]
name     = "tuna"
url      = "https://pypi.tuna.tsinghua.edu.cn"
priority = 1

[[pypi.upstreams]]
name     = "official"
url      = "https://pypi.org"
priority = 2
proxy    = "http://127.0.0.1:7890"   # 可选，单独给此上游配 HTTP 代理

[[apt.upstreams]]
name     = "tuna"
url      = "https://mirrors.tuna.tsinghua.edu.cn"
priority = 1

[[apt.upstreams]]
name     = "aliyun"
url      = "https://mirrors.aliyun.com"
priority = 2
```

### 4.2 核心接口定义

#### Adapter Interface（internal/adapter/interface.go）
```go
type Adapter interface {
    // 将路由注册到 gin.RouterGroup
    Register(rg *gin.RouterGroup)
    // 软件源类型标识，用于日志和统计
    Type() string
}
```

#### Storage Interface（internal/cache/store.go）
```go
type Storage interface {
    Exists(ctx context.Context, key string) (bool, error)
    Get(ctx context.Context, key string) (io.ReadCloser, int64, error) // reader, size, err
    Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
    Delete(ctx context.Context, key string) error
    Stat(ctx context.Context, key string) (*ObjectMeta, error)
    List(ctx context.Context, prefix string) ([]ObjectMeta, error)
    TotalSize(ctx context.Context) (int64, error)
}

type ObjectMeta struct {
    Key          string
    Size         int64
    ContentType  string
    LastModified time.Time
}
```

#### Upstream Selector（internal/upstream/selector.go）
```go
type Selector interface {
    // 从可用上游列表中选择一个
    Select(ctx context.Context) (*Upstream, error)
}

// 当前只实现 PrioritySelector：按 priority 字段顺序选择健康上游。
// 延迟数据用于监控，不参与选择决策。
```

### 4.3 缓存核心流程（internal/cache/manager.go）

```
请求进入 handler
    │
    ├─ cache.Get(key) 命中？
    │   ├─ 是：直接 stream 给客户端，记录 HIT 日志
    │   └─ 否：
    │       └─ 自研 inflight 合并同 key 的并发 miss：
    │               upstream := selector.Select()
    │               resp := upstream.Fetch(req)   // 含 per-upstream proxy
    │               同时：stream 给第一个等待的客户端
    │                     写入 cache store
    │
    │           其余并发请求等待 inflight owner 完成后从 cache 读取
    │
    └─ 写入 AccessLog（异步，不阻塞响应）
```

**关键约束：**
- 流式传输：不得将整个响应体 buffer 到内存，必须边读边写边转发
- 写缓存失败不影响响应，记录 warn 日志即可
- inflight key 与 cache key 保持一致；后台刷新另用 singleflight 去重

### 4.4 PyPI 适配器要点（internal/adapter/pypi/）

- `GET /pypi/simple/` → 代理上游 simple index
- `GET /pypi/simple/:package/` → 代理并缓存，**必须重写响应 HTML 中所有下载 URL**
  - 将 `https://files.pythonhosted.org/packages/...` 重写为 `http://<本服务地址>/pypi/files/...`
  - 这是 PyPI 代理的关键步骤，遗漏则客户端会绕过缓存直接回源
  - TTL 未过期时直接返回本地索引；TTL 过期或管理员手动刷新时，使用缓存的 `ETag` / `Last-Modified` 条件回源验证。304 延长 TTL 并复用缓存，200 在当前请求返回新索引，自动刷新失败时降级旧索引
- `GET /pypi/files/*filepath` → 下载包文件（wheel/sdist），走缓存流程
- Cache key 规范：`pypi/simple/{package}/index.html`、`pypi/files/{path}`

### 4.5 APT 适配器要点（internal/adapter/apt/）

- 完全 passthrough，**不修改任何响应内容**（保护 GPG 签名链）
- 缓存以下文件类型：
  - `InRelease` / `Release` / `Release.gpg`：短 TTL（同 ttl_index）
  - `Packages` / `Packages.gz` / `Packages.xz`：短 TTL
  - `*.deb`：长 TTL（同 ttl_blob）
- 路由格式：`GET /apt/:repo/dists/:dist/:component/binary-:arch/Packages`
- Cache key 规范：`apt/{repo}/{完整路径}`

### 4.6 npm 适配器要点（internal/adapter/npm/）

- 代理 npm registry 协议，**必须重写 JSON 中所有 `dist.tarball` URL**
- 支持 scoped packages（`@scope/package`），URL 编码和非编码形式均需处理
- 透传 `Accept: application/vnd.npm.install-v1+json` 头以获取精简元数据
- 客户端配置：`npm config set registry http://HOST:PORT/npm/`
- Cache key 规范：`npm/{package}/metadata.json`、`npm/{package}/-/{filename}`

### 4.7 Go Modules 适配器要点（internal/adapter/goproxy/）

- 完全兼容 GOPROXY 协议，**不修改响应内容**
- 5 个端点：`@v/list`、`@latest`（短 TTL）；`.info`、`.mod`、`.zip`（长 TTL，版本不可变）
- 模块路径大写编码（`Azure` → `!azure`）由客户端处理，代理直接透传
- 客户端配置：`GOPROXY=http://HOST:PORT/go,direct`

### 4.8 Cargo 适配器要点（internal/adapter/cargo/）

- 代理 crates.io Sparse Registry 协议
- **必须重写 `config.json` 中的 `dl` 字段**指向本服务
- Index 元数据（NDJSON 格式）passthrough，短 TTL
- `.crate` 文件不可变，长 TTL，SHA-256 校验由客户端完成
- 客户端配置：`~/.cargo/config.toml` 中 `[source.crates-io] replace-with = "depsilo"`

### 4.9 Maven 适配器要点（internal/adapter/maven/）

- 纯文件目录代理，**不修改任何响应内容**
- `maven-metadata.xml` 和 `-SNAPSHOT` 路径：短 TTL
- `.jar`、`.pom`、`.sha1` 等 release 版本文件：长 TTL（不可变）
- 客户端配置：`~/.m2/settings.xml` 的 `<mirrors>` 中配置

### 4.10 RubyGems 适配器要点（internal/adapter/rubygems/）

- Passthrough 代理，支持 compact index 协议
- `/versions`、`/info/*`：短 TTL
- `/gems/*.gem`：长 TTL（不可变）
- 客户端配置：`bundle config mirror.https://rubygems.org http://HOST:PORT/rubygems/`

### 4.11 Composer 适配器要点（internal/adapter/composer/）

- **必须重写 `packages.json` 中的 `metadata-url` 字段**指向本服务
- **必须在 `packages.json` 中注入 `mirrors` dist-url 模板**（`/composer/dist/%package%/%version%/%reference%.%type%`，`preferred: true`）——否则 dist 下载直连 GitHub 绕过代理，缓存和 quarantine 都失效；composer 在 mirror 失败时自动回退原始 URL，注入不降低可用性
- **执行力边界（重要）**：composer 客户端把 mirror 的 451 也当作"mirror 失败"回退原始 URL 直连下载——与 npm/pypi/cargo（重写后客户端无原始 URL 可回退）不同，composer 的 Gate 只能提供缓存 + 审计 + best-effort 拦截；硬执行需要网络出口管控（proxy-only）或 p2 元数据过滤（产品决策，见 11.4）
- dist 请求处理（`dist.go` + `handler.go handleDist`）：解析路径 →（经自身缓存）读 p2 元数据 → minified 展开（per-key `"__unset"` 哨兵）→ 匹配要求 `version_normalized` **且** `dist.reference` 一致（防元数据漂移后串 commit / 缓存污染；reference 单独匹配可兜底）→ ext 必须等于 `dist.type`（防任选 cache key 放大）→ 用 pretty version 过 QuarantineGate → `Upstream.FetchURL` 绝对地址回源（不计入上游健康统计）
- 支持 Packagist V2（p2）协议；p2 元数据 passthrough
- 元数据：短 TTL；dist 下载文件：长 TTL
- 客户端配置：`composer config -g repo.packagist composer http://HOST:PORT/composer/`

### 4.12 NuGet 适配器要点（internal/adapter/nuget/）

- **必须重写 service index（`index.json`）中所有 `@id` 字段**指向本服务
- 兼容 NuGet V3 协议
- `.nupkg` 文件：长 TTL；注册信息、搜索结果：短 TTL
- 客户端配置：`dotnet nuget add source http://HOST:PORT/nuget/v3/index.json -n depsilo`

### 4.13 Conda 适配器要点（internal/adapter/conda/）

- Passthrough 代理，不修改响应
- `repodata.json`、`channeldata.json`：短 TTL
- `.tar.bz2`、`.conda` 包文件：长 TTL（不可变）
- 客户端配置：`~/.condarc` 中配置 channels

### 4.14 CRAN 适配器要点（internal/adapter/cran/）

- Passthrough 代理，纯文件目录
- `PACKAGES`、`PACKAGES.gz`：短 TTL
- `.tar.gz`、`.zip`、`.tgz` 包文件：长 TTL
- 客户端配置：`options(repos = c(CRAN = "http://HOST:PORT/cran/"))`

### 4.15 Helm 适配器要点（internal/adapter/helm/）

- Passthrough 代理
- `index.yaml`：短 TTL
- `.tgz` chart 包：长 TTL（不可变）
- 客户端配置：`helm repo add depsilo http://HOST:PORT/helm/`

### 4.15a Alpine 适配器要点（internal/adapter/alpine/）

- Passthrough 代理，**不修改任何响应内容**（`APKINDEX.tar.gz` 有签名，改动会破坏 apk 校验）
- `APKINDEX.tar.gz`、`*.txt`：短 TTL（同 ttl_index）
- `*.apk` 包文件：长 TTL（不可变，同 ttl_blob）
- 路由格式：`GET /alpine/*path`，路径直接镜像上游布局（如 `v3.19/main/x86_64/...`）
- Cache key 规范：`alpine/{完整路径}`
- 客户端配置：编辑 `/etc/apk/repositories` 指向 `http://HOST:PORT/alpine/v3.19/main`

### 4.16 GORM 数据模型（internal/db/models.go）

```go
type CacheEntry struct {
    ID           uint      `gorm:"primarykey"`
    Key          string    `gorm:"uniqueIndex;size:512"`
    AdapterType  string    `gorm:"size:16;index"`  // pypi | apt | npm | go | cargo | maven | rubygems | composer | nuget | conda | cran | helm
    PackageName  string    `gorm:"size:256;index"` // 从 key 提取的可读包名，用于搜索
    StoragePath  string    `gorm:"size:512"`
    Size         int64
    HitCount     int64     `gorm:"default:0"`
    ContentType  string    `gorm:"size:128"`
    ExpiresAt    time.Time `gorm:"index"`
    LastAccessed time.Time `gorm:"index"`
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

type AccessLog struct {
    ID          uint      `gorm:"primarykey"`
    AdapterType string    `gorm:"size:16;index"`
    CacheKey    string    `gorm:"size:512"`
    PackageName string    `gorm:"size:256;index"` // 从 key 提取的可读包名
    Hit         bool      `gorm:"index"`
    Upstream    string    `gorm:"size:128"`       // 未命中时填写实际使用的上游
    LatencyMs   int64
    StatusCode  int
    ClientIP    string    `gorm:"size:64"`
    BytesSent   int64
    CreatedAt   time.Time `gorm:"index"`
}

type UpstreamRecord struct {
    ID            uint      `gorm:"primarykey"`
    AdapterType   string    `gorm:"size:16;index"`
    Name          string    `gorm:"size:128;uniqueIndex"`
    URL           string    `gorm:"size:512"`
    Proxy         string    `gorm:"size:256"`
    Priority      int
    Healthy       bool      `gorm:"default:true"`
    AvgLatencyMs  int64
    SuccessRate   float64
    LastCheckedAt time.Time
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

type User struct {
    ID           uint      `gorm:"primarykey"`
    Username     string    `gorm:"uniqueIndex;size:64"`
    PasswordHash string    `gorm:"size:256"`
    Role         string    `gorm:"size:16;default:'readonly'"` // admin | readonly
    Enabled      bool      `gorm:"default:true"`
    LastLoginAt  *time.Time
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

type APIToken struct {
    ID          uint      `gorm:"primarykey"`
    UserID      uint      `gorm:"index"`
    Name        string    `gorm:"size:128"`
    TokenHash   string    `gorm:"uniqueIndex;size:256"` // 存 hash，不存明文
    Permissions string    `gorm:"size:32"`              // readonly | readwrite
    ExpiresAt   *time.Time
    LastUsedAt  *time.Time
    CreatedAt   time.Time
}
```

### 4.17 API 路由总表（internal/api/router.go）

> 本表为早期骨架，此后新增了大量路由（`/api/v1/discover`、`/mcp`、`/events/stream`、`/setup/*`、admin 下的 bandwidth / audit-logs / rules / security / quarantine / webhooks / license / projects 等）。**以 `internal/api/router.go` 为准**；quarantine 管理端点见 4.21。

```
# 公开路由（无需认证）
GET  /                              → 前端 Portal SPA
GET  /admin                         → 前端 Admin SPA
GET  /assets/*                      → 前端静态资源（embed）

GET  /api/v1/stats                  → 公开统计（命中率、Top包、上游状态）
GET  /health                        → 健康检查
GET  /metrics                       → Prometheus metrics

# PyPI 代理
GET  /pypi/simple/
GET  /pypi/simple/:package/
GET  /pypi/files/*filepath

# APT 代理
GET  /apt/:repo/dists/*filepath
GET  /apt/:repo/pool/*filepath

# npm 代理
GET  /npm/:package                         # 包元数据（JSON，需重写 tarball URL）
GET  /npm/@:scope/:package                 # scoped 包元数据
GET  /npm/:package/-/:filename             # tarball 下载
GET  /npm/@:scope/:package/-/:filename     # scoped tarball 下载

# Go Modules 代理（GOPROXY 协议）
GET  /go/*path                             # @v/list, @v/version.info/.mod/.zip, @latest

# Cargo 代理（Sparse Registry 协议）
GET  /crates/*path                         # config.json, index, api/v1/crates 下载

# Maven 代理
GET  /maven/*path                          # 纯文件目录代理

# RubyGems 代理
GET  /rubygems/*path                       # compact index + gem 下载

# Composer 代理
GET  /composer/*path                       # packages.json, p2 元数据, dist 下载

# NuGet 代理（V3 协议）
GET  /nuget/*path                          # service index, registration, package 下载

# Conda 代理
GET  /conda/*path                          # repodata.json + 包下载

# CRAN 代理
GET  /cran/*path                           # PACKAGES + 源码/二进制下载

# Helm 代理
GET  /helm/*path                           # index.yaml + chart tgz 下载

# 认证
POST /api/v1/auth/login
POST /api/v1/auth/logout
POST /api/v1/auth/refresh

# 管理 API（需 JWT 认证，Role: admin）
GET  /api/v1/admin/dashboard
GET  /api/v1/admin/cache                    # 列表，支持分页和搜索
DELETE /api/v1/admin/cache/:id
POST /api/v1/admin/cache/cleanup            # 触发清理过期/LRU
GET  /api/v1/admin/upstreams
POST /api/v1/admin/upstreams
PUT  /api/v1/admin/upstreams/:id
DELETE /api/v1/admin/upstreams/:id
POST /api/v1/admin/upstreams/:id/check      # 手动触发健康检查
GET  /api/v1/admin/logs                     # 访问日志，分页+过滤
GET  /api/v1/admin/users
POST /api/v1/admin/users
PUT  /api/v1/admin/users/:id
DELETE /api/v1/admin/users/:id
GET  /api/v1/admin/tokens
POST /api/v1/admin/tokens
DELETE /api/v1/admin/tokens/:id
GET  /api/v1/admin/settings
PUT  /api/v1/admin/settings
```

### 4.18 公开统计 API 响应格式（GET /api/v1/stats）

```json
{
  "service": {
    "version": "0.8.0",
    "uptime_seconds": 86400,
    "status": "healthy"
  },
  "today": {
    "total_requests": 12840,
    "hit_count": 11209,
    "miss_count": 1631,
    "hit_rate": 0.873,
    "bytes_served": 36700000000,
    "bytes_saved": 34200000000
  },
  "week": {
    "total_requests": 88410,
    "hit_count": 78040,
    "hit_rate": 0.883,
    "bytes_saved": 231000000000
  },
  "cache": {
    "total_files": 5263,
    "total_size_bytes": 9021800000,
    "pypi_files": 3421,
    "apt_files": 1842
  },
  "upstreams": [
    {
      "name": "tuna-pypi",
      "adapter": "pypi",
      "healthy": true,
      "avg_latency_ms": 42,
      "success_rate": 0.999
    }
  ],
  "top_packages": {
    "pypi": [
      { "name": "requests", "hit_count": 4312 }
    ],
    "apt": [
      { "name": "curl", "hit_count": 2640 }
    ]
  }
}
```

### 4.19 错误处理规范

Adapter/API handler 目前直接用 `gin.H` 返回 JSON，没有集中式 error middleware。
错误响应至少保留稳定的机器码 `code` 和人类可读 `message`；供应链 Gate 额外返回
`ecosystem`、`package`、`version`。新增错误应复用相邻 handler 的状态码与格式，
不要声明代码中不存在的全局 `ErrorResponse` 或自动映射层。

### 4.20 embed 前端打包（web/embed.go + internal/server/server.go）

```go
// web/embed.go
//go:embed all:dist
var DistFS embed.FS

// internal/server/server.go 使用 fs.Sub(DistFS, "dist")，通过 NoRoute：
// - /assets/* → 静态文件
// - 其余前端路由 → /index.html（SPA fallback）
```

### 4.21 供应链隔离接入规范（internal/quarantine/ + internal/adapter/quarantine.go）

**每个新增/修改的 adapter 必须在制品下载 handler 顶部接入 QuarantineGate**（这是新 adapter 的强制契约，composer 曾因遗漏此步导致隔离对整个生态失效）：

```go
// 1. 从请求路径/文件名解析出 (pkg, version)（解析函数放 internal/adapter/packagekey/）
if pkg, version := packagekey.ParseXxxPath(path); pkg != "" && version != "" {
    // 2. Gate：true = 已写 451 响应，handler 必须立即 return
    if blocked := adapter.QuarantineGate(c, "<ecosystem>", pkg, version); blocked {
        return
    }
}
```

- 决策链（`internal/quarantine/checker.go`）：**第 0 步恶意封锁**（`internal/blocklist/`；同步 npm/PyPI/Cargo/RubyGems/Composer/NuGet/Go/Maven 的 OSV MAL-* 精确版本或全版本记录，无显式版本的 bounded range 会跳过；命中即 451 `MALICIOUS_BLOCKED`，阈值 0 生态和 allowlist 均不可绕过，唯一豁免是 24h 审计 override）→ 阈值 0 放行 → allow 规则放行 → 管理员批准放行 → 三级查发布时间（内存 → DB → 上游 registry API）→ 年龄不足则 451 `QUARANTINED` + 审计事件 + Webhook
- **篡改检测**：不可变制品 miss 路径调用 `Verify`，无基线则建立首见 SHA-256；
  后台刷新不匹配时保留首见字节、不覆盖缓存、写 `tamper_detected` 事件 + critical
  webhook，并延长 TTL 防告警风暴。LRU 淘汰后重取仍会告警，但旧字节已不存在，
  无法恢复首见副本。不可变判定用 TTL 阈值（默认取 `cache.ttl_blob`）。
- 每个生态需在 `internal/quarantine/resolvers/` 提供发布时间 resolver（真 API 或 Last-Modified 近似）
- 默认阈值（`policy.go DefaultThresholds`）：npm 7d，多数生态 3d，go/apt 0（免检）；**空配置也生效**。not-found/unsupported 默认 fail-closed，真实上游故障始终放行并告警
- 元数据请求不 gate，只 gate 制品下载；版本字符串必须与 resolver 在上游元数据中能匹配到的形式一致（如 composer 用 pretty version 而非 normalized）
- 集成测试注意：`tests/integration/main_test.go` 已把所有生态阈值归零（resolver 会查真实 registry，mock 包不存在会被 fail-closed 拦截）

---

## 五、前端实现规范

### 5.1 技术栈

```
React 19 + TypeScript
Vite（构建工具）
Tailwind CSS v4（@theme token；组件大量使用内联 style + CSS 变量）
自研 "Instrument" 设计系统（src/index.css 角色化 token；暗色为默认主题）
React Router v7（路由，viewTransition）
TanStack Query v5（数据请求 + 缓存）
axios（HTTP 客户端）
i18next + react-i18next（i18n，zh 默认/回退）
Material Symbols（图标）+ 自托管 @fontsource 字体（Inter Variable / Inter Tight / JetBrains Mono / Noto Sans SC）
recharts（图表）
```

### 5.2 基础组件

**不使用 shadcn/ui**（历史文档遗留，已被自研组件替代）。基础组件在 `web/src/components/`（Button / Badge / Modal / DataTable / Tabs / StatusDot / EcosystemIcon / UpstreamCard 等 18 个），新 UI 优先复用这些组件，风格与 `src/index.css` 的 Instrument token 保持一致。

### 5.3 路由结构（src/App.tsx，2026-07 实况）

```
（首次启动 needs_setup 时整站渲染 src/setup/SetupWizard）

/                    → PortalApp（用户门户，无需登录）
  /                  → QuickStart（生态目录 + 配置面板 + AI 集成 CTA）
  /monitor           → Monitor（命中率/流量 KPI + 上游健康面板）

/admin               → AdminApp（管理后台，RequireAuth）
  /admin/login       → Login
  /admin             → Dashboard（默认页）
  /admin/bandwidth   → BandwidthReport
  /admin/logs        → AccessLogs
  /admin/audit       → AuditLogs
  /admin/quarantine  → Quarantine（隔离事件 + 审批，开源功能）
  /admin/cache       → CacheManage
  /admin/upstreams   → Upstreams
  /admin/rules       → Rules
  /admin/security    → Security
  /admin/projects    → Projects（唯一 Pro 门控页）
  /admin/users       → Users
  /admin/license     → License
  /admin/settings    → Settings
```

### 5.4 Portal 页面（用户门户）

#### PortalApp.tsx 布局
- 顶部 header：Logo + 导航（快速开始 / 服务状态）+ 右侧状态 pill + "管理后台 →" 链接
- 内容区：无侧边栏，全宽内容
- 无需登录，直接访问

#### QuickStart.tsx（快速开始页）
**设计要求：**
- 服务地址由 `PortalApp` 全局 header 展示；配置面板统一使用 `window.location.origin`
- 左侧 14 个技术栈目录项，右侧展示所选项的配置步骤；Docker OCI 在 Container 分组
- 覆盖 pip、apt、npm、Go、Cargo、Maven、RubyGems、Composer、NuGet、Conda、CRAN、Helm、Alpine、Hugging Face，以及 Docker OCI 配置
- 每个代码块使用 `CodeBlock` 组件，支持一键复制
- 底部 tip 提示（首次回源说明 / GPG 签名说明）
- 服务地址从 `window.location.origin` 自动拼接，**不得写死**

#### Monitor.tsx（服务状态页）
**设计要求：**
- 汇总上游总数、健康/降级/失败计数，并在有流量时展示 7 天命中率与节省流量
- 可搜索的分组上游卡片：名称、URL、健康状态、成功率、延迟与 latency beats
- 统计每 30 秒刷新；较重的 latency series 每 60 秒刷新

#### CodeBlock 组件（portal/components/CodeBlock.tsx）
```tsx
interface CodeBlockProps {
  filename?: string     // 可选文件名标注
  code: string          // 代码内容（支持多行）
  language?: string     // 语法高亮标识（展示用）
}
// 右上角复制按钮，点击后变为"已复制"并 2 秒后恢复
// 使用项目自研 Instrument 样式和 monospace 字体
```

`ServiceUrlBar.tsx` 仍在仓库中但当前路由未使用；不要把它写成 QuickStart 的现役依赖。

### 5.5 Admin 页面（管理后台）

#### MainLayout.tsx
- 左侧固定侧边栏（200px）：Logo + 导航菜单 + 底部用户信息
- 顶部 topbar：页面标题 + 服务状态 badge + 刷新按钮
- 侧边栏导航项：
  ```
  [监控]
  - Dashboard（总览）
  - 访问日志
  [管理]
  - 缓存管理
  - 上游源
  - 用户管理
  - 系统设置
  ```
- 底部展示当前登录用户名和角色，点击可退出登录

#### Dashboard.tsx
- 顶部使用共享 `Metric` 组件展示关键指标
- 请求量折线图（近 7 日，按天，PyPI/APT 两条线，使用 recharts LineChart）
- 上游源状态卡片（同 Portal，但包含更多细节：可用率 %）
- 热门包 Top10（PyPI + APT 并排）

#### CacheManage.tsx
- 顶部使用共享 `Metric` 组件展示存储概览和总用量
- 搜索栏 + 类型筛选下拉（全部/PyPI/APT）+ "清理过期缓存"按钮（红色，需确认弹窗）
- 表格列：包名/文件名、类型、大小、命中次数、最后访问、过期时间、操作（删除）
- 分页（每页 20 条）
- 删除操作需 Dialog 确认

#### Upstreams.tsx
- 右上角"添加上游源"按钮
- 14 个类型选项（pypi / apt / npm / go / cargo / maven / rubygems / composer / nuget / conda / cran / alpine / helm / docker）
- 新增/编辑弹窗包含类型选择器（新建时可选，编辑时置灰）
- 每个上游源以卡片行展示：健康状态圆点、名称、URL（monospace）、延迟、可用率、操作按钮（编辑/删除）
- 特殊展示：若配置了 proxy，在名称后以灰色小字展示 `· 代理: xxx`
- 编辑/新增使用 Dialog 表单：
  - 名称（text input）
  - URL（text input）
  - 优先级（number input）
  - HTTP 代理（text input，可选）
  - 提交前验证 URL 格式

#### AccessLogs.tsx
- 搜索栏（包名）+ 类型筛选 + 命中/未命中/失败 筛选 + 搜索按钮
- 表格列：时间、类型 badge（PyPI/APT）、包名（monospace）、结果 badge（命中/未命中/失败）、上游、耗时（ms）、客户端 IP
- 时间格式：`HH:mm:ss`（今天）或 `MM-DD HH:mm`（更早）
- 分页（每页 50 条）

#### Users.tsx
- 用户表：头像（首字母圆形）、用户名、角色 badge、状态 badge、最后登录、创建时间、操作（编辑/禁用/启用）
- API Token 子表（用 card 分组）：Token 名称、权限、最后使用、过期时间、撤销按钮
- 新增用户 Dialog：用户名、密码、角色选择
- 生成 Token Dialog：名称、权限（只读/读写）、有效期（7天/30天/90天/永不过期）
  - 生成后展示完整 Token 一次（之后不可再查），提醒用户复制

#### Settings.tsx
- 4 个 Tab：基础配置、缓存策略、存储后端、认证安全

**基础配置 Tab：**
- 监听地址（input）、监听端口（input）
- 日志级别（select: debug/info/warn/error）
- Toggle：开启 Prometheus 指标、访问日志持久化

**缓存策略 Tab：**
- 最大缓存容量 GB（number input）
- 清理阈值 %（number input，触发 LRU 的水位）
- 索引 TTL（text input，如 "5m"）
- 文件 TTL（text input，如 "72h"）
- Toggle：LRU 自动淘汰开关

**存储后端 Tab：**
- 存储类型 select（本地/S3），切换时动态显示不同表单
- 本地：缓存目录 path input
- S3：Endpoint、Bucket、Access Key、Secret Key（password）、Region
- 数据库类型只读展示；当前服务端仅支持 SQLite，PostgreSQL 选项不得作为可用能力宣传

**认证安全 Tab：**
- Toggle：启用登录认证
- Toggle：代理端点匿名访问（允许 pip/apt 无 Token 使用）
- JWT Secret input（password 类型）+ "重新生成"按钮
- Token 有效期 select（7天/30天/90天/永不过期）

### 5.6 API 请求封装（src/lib/api.ts）

```typescript
// 统一封装 axios 实例
// baseURL: '/api/v1'
// 请求拦截：自动附加 Authorization: Bearer <token>（token 存 localStorage）
// 响应拦截：401 自动跳转 /admin/login
// 导出各模块的 API 函数，例如：
export const statsApi = { getStats: () => axios.get('/api/v1/stats') }
export const cacheApi = { list: (params) => ..., delete: (id) => ..., cleanup: () => ... }
export const upstreamApi = { ... }
export const logsApi = { ... }
export const authApi = { login: (body) => ..., logout: () => ... }
```

### 5.7 认证流程

1. 访问 `/admin/*` 任何页面，若 localStorage 无 token，重定向到 `/admin/login`
2. 登录成功后 token 存入 localStorage，跳转到 `/admin`
3. token 过期时（401 响应）自动清除 token 并跳转登录页
4. 登出：调用 `/api/v1/auth/logout`，清除 localStorage token

---

## 六、构建与运行

### Makefile 目标

```makefile
.PHONY: dev build frontend test lint docker

dev:           # 构建后在后台启动 Depsilo，写 .dev.log / .server.pid
frontend:      # cd web && npm run build → 产物在 web/dist
build:         # make frontend && go build -o bin/depsilo ./cmd/depsilo
test:          # go test ./tests/unit/...
test-integration: # go test -tags integration ./tests/integration/...
lint:          # i18n audit + go vet ./...
docker-build:  # docker build -t depsilo/depsilo:local .
```

### Dockerfile

```dockerfile
# 多阶段构建
# Stage 1: 构建前端
FROM node:20-alpine AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: 构建后端
FROM golang:alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -o depsilo ./cmd/depsilo

# Stage 3: 最终镜像
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=backend /app/depsilo .
EXPOSE 23333
ENTRYPOINT ["./depsilo"]
CMD ["serve"]
```

### docker-compose.yml

```yaml
version: '3.8'
services:
  depsilo:
    build: .
    ports:
      - "23333:23333"
    volumes:
      - ./data:/app/data
      - ./config.toml:/app/config.toml
    environment:
      - DEPSILO_CONFIG=/app/config.toml
    restart: unless-stopped
```

---

## 七、开发启动顺序

Claude Code 应按以下顺序实现，每完成一步确保可运行后再进行下一步：

### Phase 1：后端骨架（可跑通健康检查）
1. `go mod init depsilo`，添加所有依赖到 go.mod
2. 实现 `config.go` + `loader.go`（viper 读取 TOML）
3. 实现 `main.go`（初始化 Gin、注册 `/health` 路由）
4. 实现 GORM 模型 + AutoMigrate（SQLite）
5. 实现本地存储 `local.go`
6. **验收：`go run ./cmd/depsilo serve` 启动无报错，`GET /health` 返回 200**

### Phase 2：PyPI 代理核心（已完成）
1. 实现 upstream pool + priority selector
2. 实现 cache manager（单飞 + 流式写入）
3. 实现 PyPI adapter（simple API + URL 重写 + 文件下载）

### Phase 3：APT 代理（已完成）
1. 实现 APT adapter（Release/Packages/.deb passthrough）

### Phase 4：管理 API（已完成）
1. 实现 JWT 认证（login/logout/middleware）
2. 实现所有 `/api/v1/admin/*` 路由
3. 实现公开统计 `/api/v1/stats`

### Phase 5：前端（已完成）
1. 初始化 React 项目；当前已迁移为 Tailwind CSS v4 + 自研 Instrument 组件
2. 实现 Portal（用户门户）：当前保留 QuickStart + Monitor；早期 PackageBrowse / LiveStream 页面已移除
3. 实现 Admin（管理后台）：Dashboard（趋势图表 + 延迟 sparkline） → 缓存（Treemap 可视化） → 上游 → 日志 → 用户 → 设置

### Phase 6：收尾（已完成）
1. S3 存储实现（`s3.go`）
2. Prometheus metrics
3. Dockerfile + docker-compose
4. 健康检查完善（upstream 定时检查 + 延迟日志）
5. LRU 淘汰后台 goroutine

### Phase 7：npm 支持（已完成）
1. npm adapter（metadata JSON tarball URL 重写 + tarball 下载缓存）
2. scoped packages 支持（@scope/package）

### Phase 8：Go Modules 支持（已完成）
1. GOPROXY 协议适配器（5 个端点，纯 passthrough）

### Phase 9：Cargo 支持（已完成）
1. Sparse Registry 协议适配器（config.json 重写 + index + crate 下载）

### Phase 10：Maven / RubyGems / Composer / NuGet / Conda / CRAN / Helm（已完成）
1. Maven — 纯文件目录代理
2. RubyGems — compact index + gem 下载
3. Composer — packages.json metadata-url 重写
4. NuGet — V3 service index @id 字段重写
5. Conda — channel repodata passthrough
6. CRAN — PACKAGES + 包文件 passthrough
7. Helm — chart repository passthrough

### 后续 Phase（历史项，均已完成）
- Docker Registry 代理
- 审计日志
- 包 allow/deny 规则
- 简化版 License Key + Trial 系统

前瞻开发顺序以 `docs/DIRECTION.md` 为准；当前待开发重点是 Freeze / Golden
Snapshot、CRA 完整 SBOM、签名发布和基础 Helm chart。

---

## 八、AI 协作行为准则

> 以下规则约束 AI 在本项目中的**工作方式**，而非具体实现细节。

### 8.1 先想清楚再动手

- **遇到歧义时停下来问**，不要默默假设。例如："新增一个生态" 可能指 adapter + 前端 + 配置 + 文档全套，也可能只是后端 handler。不确定就先确认范围。
- **修改前先说出你的理解**：我要改哪些文件、为什么这样改、预期影响是什么。不要直接开始写代码。
- **如果有更简单的方案，提出来**。不要用户说 A 就做 A，如果 B 更合理，先建议。
- 自检：_我能用一句话说清这次改动的目的吗？说不清就是还没想清楚。_

### 8.2 最小改动原则

- **只改被要求的部分**。不要顺手重构旁边的代码、调整缩进、补注释、重命名变量。
- **不加推测性功能**。用户没要求的 feature flag、兼容层、抽象包装，一律不加。
- **三行重复优于一个过早抽象**。除非同一逻辑已出现三次以上，否则不要提取函数/组件。
- **删代码时只删你这次改动产生的孤儿**。不要删"看起来没用"的旧代码，那可能是别处的依赖。
- 自检：_每一行改动都能追溯到用户的具体要求吗？追溯不到的改动就不该存在。_

### 8.3 目标驱动，可验证

- 把模糊任务转成**可验证的目标**。"修复 bug" → 先写一个能复现 bug 的测试，然后让测试通过。
- 多步任务**先列计划**，每步有明确的验收标准：

  ```text
  Step 1: 实现 handler.go  → 验证: go build 通过
  Step 2: 注册路由         → 验证: curl /new-path 返回 200
  Step 3: 更新前端         → 验证: npm run build 无报错
  ```

- **改完后跑验证命令**，不要说"应该可以了"就结束。`make test`、`npx tsc --noEmit` 是最低要求。
- 自检：_我怎么证明这次改动是正确的？如果只能说"我觉得对了"，那就是没验证。_

### 8.4 本项目特有的 AI 陷阱

以下是 AI 在 Depsilo 项目中**反复犯的错误**，必须特别注意：

| 陷阱                                                         | 正确做法                                                | 为什么                              |
| ------------------------------------------------------------ | ------------------------------------------------------- | ----------------------------------- |
| 在 passthrough adapter（APT/Go/Maven/CRAN/Helm）中修改响应体 | **绝对不能修改**，原样透传                              | 修改会破坏 GPG 签名，客户端拒绝安装 |
| 把整个响应 body 读进内存再转发                               | **必须流式传输** `io.Copy` / `io.TeeReader`             | torch 包 2GB+，buffer 会 OOM        |
| 前端改了文字但只改了 zh.ts                                   | **zh.ts 和 en.ts 必须同时更新**                         | 否则另一语言显示 key 而非文字       |
| URL 重写时遗漏某些链接                                       | 用**正则/HTML parser 全量替换**，不要只替换"看到的那个" | 遗漏一个 href，pip/npm 就会绕过缓存 |
| 在前端代码中硬编码 `localhost:23333`                        | 用 `window.location.origin`                             | 部署地址不是 localhost              |
| 忽略 Go error（`_ = xxx`）                                   | **所有 error 必须处理或传递**                           | 静默吞错导致难以排查的线上问题      |
| 新增文件放在错误的目录                                       | 严格按照第三节目录结构                                  | 项目约定不可随意创建顶层目录        |
| 新增/改造 adapter 忘接 QuarantineGate                        | 制品下载 handler 顶部必须过 Gate（见 4.21）             | 漏接 = 该生态供应链隔离整体失效     |

---

## 九、编码规范

### Go 规范
- 错误处理：**不得忽略 error**，必须处理或向上传递
- 日志：使用 `zap.L()` 全局 logger，key-value 结构化字段
- context：所有 IO 操作必须传入 `context.Context`，支持超时取消
- goroutine：后台任务（健康检查、LRU 清理）必须监听 context done 信号
- 测试：核心逻辑（cache manager、URL rewriter、key normalizer）必须有单元测试

### 命名约定
- 文件名：`snake_case.go`
- 结构体/接口/类型：`PascalCase`
- 函数/变量：`camelCase`
- 常量：`SCREAMING_SNAKE_CASE` 或 `PascalCase`（导出常量）

### TypeScript/React 规范
- 所有组件使用函数组件 + hooks
- Props 用 interface 定义，文件内同名 `Props` 即可
- API 调用统一通过 `src/lib/api.ts`，不在组件内直接 axios
- 使用 TanStack Query 管理服务端状态，不自己写 loading/error state
- 服务地址从 `window.location.origin` 读取，**禁止硬编码 IP 或端口**

---

## 九、注意事项（避坑清单）

1. **PyPI URL 重写是核心**：`/pypi/simple/:package/` 返回的 HTML 中，所有 `href` 属性指向外部的下载链接必须被重写为本服务地址，否则 pip 会绕过缓存
2. **APT 不能修改响应**：任何对 Release/InRelease/Packages 文件的修改都会破坏 GPG 签名校验，apt 会报错拒绝安装
3. **流式传输**：wheel 文件可能几百 MB（torch ~2GB），必须 stream，不能 buffer
4. **inflight 与流式的兼容**：第一个 miss 请求 stream 给客户端的同时写缓存，后续相同 key 的请求等待完成后从缓存读取（不复用第一个请求的 body）；后台刷新由 singleflight 去重
5. **服务地址动态化**：前端代码块中所有配置命令里的服务地址，必须用 `window.location.origin` 动态生成，不能写死
6. **Token 安全**：数据库只存 token 的 hash（bcrypt 或 SHA-256），明文只在生成时展示一次
7. **SQLite 并发**：开启 WAL 模式（`PRAGMA journal_mode=WAL`），避免写锁争用
8. **npm URL 重写**：和 PyPI 类似，registry 响应中的 `dist.tarball` URL 必须重写为本服务地址，否则 `npm install` 会绕过缓存直接回源
9. **Cargo config.json 重写**：`/crates/` 下的 `config.json` 文件中 `dl` 字段必须指向本服务，这是 Cargo 找到下载地址的关键
10. **NuGet service index 重写**：`index.json` 中所有服务端点 `@id` URL 必须重写，包括 SearchQueryService、RegistrationsBaseUrl、PackageBaseAddress 等
11. **Go Modules 无需重写**：GOPROXY 协议本身不包含绝对 URL，客户端始终向 GOPROXY 环境变量指定的地址请求，纯 Passthrough 即可
12. **Composer metadata-url 重写**：`packages.json` 中的 `metadata-url` 字段必须重写为本服务地址，否则 Composer 拉取包元数据时会绕过缓存

---

## 十、参考资料

- [PyPI Simple Repository API (PEP 503)](https://peps.python.org/pep-0503/)
- [PyPI JSON API (PEP 691)](https://peps.python.org/pep-0691/)
- [APT Repository Format](https://wiki.debian.org/DebianRepository/Format)
- [npm Registry API](https://github.com/npm/registry/blob/main/docs/REGISTRY-API.md)
- [Go Module Proxy Protocol](https://go.dev/ref/mod#goproxy-protocol)
- [Cargo Sparse Registry (RFC 2789)](https://rust-lang.github.io/rfcs/2789-sparse-index.html)
- [Maven Repository Layout](https://maven.apache.org/repository/layout.html)
- [RubyGems Compact Index](https://guides.rubygems.org/rubygems-org-compact-index-api/)
- [Composer Repository API](https://getcomposer.org/doc/05-repositories.md)
- [NuGet V3 Protocol](https://learn.microsoft.com/en-us/nuget/api/overview)
- [Helm Chart Repository](https://helm.sh/docs/topics/chart_repository/)
- [Gin 文档](https://gin-gonic.com/docs/)
- [GORM 文档](https://gorm.io/docs/)
- [TanStack Query 文档](https://tanstack.com/query/latest)

---

## 十一、项目现状快照（2026-07-10 更新）

> 本节记录项目深度审查的结论，供后续开发决策参考。上次更新：2026-07-10（战略与路线图以 `docs/DIRECTION.md` + ADR-0004 为权威）。

### 11.1 规模统计

- ~31,800 行 Go 代码（含测试）；56 个 Go 测试文件、334 个 `Test*`
- 前端 66 个 TSX/TS 文件，~13,200 行；i18n en/zh 各 655 键（同步）
- **15 个 install surfaces**：14 个表驱动常规 adapter（含 Hugging Face）+ 独立 Docker OCI `/v2/`
- 27 个 GORM 模型；CLI 13 个命令（serve/start/stop/status/doctor/init-agent/prompt/version/activate/warmup/flush/backup/restore）
- **quarantine 子系统**（`internal/quarantine/`，~3,400 行含测试）：最小发布年龄隔离 + 审批流 + 审计事件 + Webhook，2026-07 上线（T1/1–T1/7 提交系列）
- **blocklist + tamper detection** 已落到 `master`，列在 `CHANGELOG.md` 的 Unreleased
- **真实客户端 Docker E2E**：`make test-docker-all` 覆盖 13 个非 Docker 生态（Alpine 尚缺）；Docker Registry 因需 dind 单独 opt-in
- 设计 spec 文档（`docs/specs/`）+ 审查报告（`docs/reviews/`）+ 术语表（`CONTEXT.md`）+ ADR-0001~0004 + 战略文档 `docs/DIRECTION.md`

### 11.2 架构优势

- **Adapter 模式**：接口只有 `Register()` + `Type()`，新增生态不碰核心代码
- **分类型缓存刷新**：可变索引过期后先同步刷新，确保新版本首次请求可见；不可变制品后台校验，上游故障均可降级返回旧缓存
- **并发 miss 防击穿**：自研 inflight 让同一 key 并发请求只回源一次；后台刷新用 singleflight
- **流式传输**：上游响应通过 `countingReader` 直接流向 `storage.Put`，不做内存缓冲
- **依赖注入**：`router.go` 的 `Deps` 结构体传递所有依赖，无全局变量
- **表驱动初始化**：`server.go` 使用 `ecosystemDef` 表 + 循环注册 14 个常规生态
- **前端完成度高**：React 19 + TanStack Query + i18n + 暗色主题 + Instrument 设计系统

### 11.3 已修复的技术债

| 问题                                       | 修复方式                                                          | Commits                            |
| ------------------------------------------ | ----------------------------------------------------------------- | ---------------------------------- |
| Cache Manager 大文件 OOM                   | `bytes.Buffer` → `countingReader` 流式直写 storage                | c55b7ce, a5c76b2, 9264951, c6b5d13 |
| `io.Copy` 错误被忽略                       | 15 处 adapter 改为 `zap.L().Warn` 记录错误                        | 5bdbba4                            |
| `server.go` 过长 (408 行)                  | 表驱动循环，-64 行 (→344 行)                                      | 7b3a44a                            |
| 前端工具函数重复                           | 提取到 `web/src/lib/utils.ts`，11 文件清理                        | 5380960                            |
| Docker Registry Cloudflare 403             | 所有上游请求加 `User-Agent: docker/27.0.0 depsilo`                | b04314f                            |
| 前端 `formatTime` 3 处重复                 | 扩展 `lib/utils.ts` 支持 auto/time/relative 模式                  | ecaa8ef                            |
| `license/middleware.go` 23 行碎片          | 合并到 `license.go`                                               | ecaa8ef                            |
| `cache` 包内 securityScanner 全局变量      | 改为 `Manager.securityScanner` 字段 + DI                          | ecaa8ef                            |
| `internal/server/server.go` 未被 commit    | 入库 + 删 `cmd/server` 副本 + 收紧 `.gitignore`                   | a647104                            |
| `cache/manager.go` 内混入 package-key 解析 | 剥离到 `internal/adapter/packagekey/`                             | a647104                            |
| `api.Deps` 12 个独立 `*Pool` 字段          | 统一为 `map[string]*Pool` + `Ecosystems []string`（ADR-0001）     | 2b44d70                            |
| E2E 测试是 monolithic shell 路线           | 13 个独立 `testground/docker-<eco>/Dockerfile` + Makefile 驱动    | 2718bdf                            |

### 11.4 剩余已知问题

| 问题                              | 严重度 | 说明                                                                  |
| --------------------------------- | ------ | --------------------------------------------------------------------- |
| composer 451 可被客户端回退绕过   | 高     | composer 把 mirror 的 451 当"mirror 失败"回退原始 URL 直连；审计事件仍记 blocked（信号与实际不符）。硬执行需 p2 元数据过滤（需处理事件语义/审批联动/minified 重发，建议出 ADR）或部署层出口管控 |
| packagist dist 走 GitHub API 限流 | 中     | 上游为 repo.packagist.org 时 dist.url 指向 api.github.com（未认证 60 req/h/IP），冷缓存 CI 突发会 403；默认 aliyun 上游无此问题（dist 自托管） |
| Selector 读 `u.Healthy` 未加锁    | 中     | `selector.go` 无锁读与 Report/health check 加锁写并存，存在数据竞争   |
| apt handler 硬编码剥 `/apt` 前缀  | 中     | `/p/:slug/apt` 项目路由下疑似会算错上游路径（未验证）                 |
| license 任意非空 key 即激活 Pro   | 中     | Lemon Squeezy 校验已随 Enterprise 合同制转向移除，无远端验证          |
| 前端 ESLint 基线为红               | 中     | `npm run lint` 当前 137 errors / 1 warning；CI 只跑 type-check、build、i18n audit |
| 年龄隔离 Gate 缺 HTTP 级测试       | 低     | checker/resolver 单测充分；blocklist/tamper 有集成测试，但年龄 gate 的 adapter 接线仍缺 HTTP 断言 |
| 5 个生态发布时间用 Last-Modified 近似 | 低  | maven/helm/conda/alpine/docker 无发布时间 API，用制品 Last-Modified 兜底 |
| Docker Registry E2E 需手动 opt-in | 低     | `test-docker-docker` 需 dind/特权，默认不进 all       |
| Docker token 缓存非持久化         | 低     | 重启后丢失，影响首次请求延迟（~1s），非功能性问题                     |
| Security.tsx / Monitor.tsx 偏大   | 低     | 单文件较大、多合一功能，可按 tab 拆但 ROI 低                          |

已于 2026-07-08 修复：composer dist 绕过代理且未接 QuarantineGate（补 mirrors 注入 + dist handler + Gate + E2E 验证）；集成测试套件被 quarantine fail-closed 默认策略整体打挂（测试配置阈值归零）；`TestRules_CommunityBypass` 断言 6-28 定价重置前的旧行为（改为断言社区版强制执行规则）。同日对抗复审后加固：dist 匹配要求 reference 一致（防元数据漂移串 commit/缓存污染）、ext 必须等于 dist.type（防任选 cache key）、minified `__unset` 改为 per-key 哨兵格式（原实现为不存在的数组格式）、composer resolver 支持 `~dev.json`（否则 fail-closed 误拦全部 dev dist）、`FetchURL` 不再把第三方主机延迟记入上游健康统计。

### 11.5 定位（2026-06-30 ADR-0004 之后）

不再按"更轻的 Nexus"做竞品对比。定位是**供应链执行层**：与 Artifact Keeper / Nexus 共存（README 甚至主动推荐它们做通用 registry），Depsilo 作为前置隔离墙串联部署（chainable proxy）。差异化 = 只做"唯有 serve-path 代理才能做"的事：最小发布年龄隔离、恶意包封锁、篡改检测均已落地；下一步是 freeze 快照和 CRA 完整 SBOM。卖点是"组织级强制 + 审计"，而非生态数量或速度。GTM 姿态："honesty as positioning"（详见 `docs/DIRECTION.md`、`docs/research/2026-06-30-competitive-landscape.md`）。

### 11.6 路线图优先级

**P0（必须做）：**

1. ~~修复 cache manager 大文件内存问题~~ ✅ 已完成（`countingReader` 流式写入）
2. ~~Docker Registry 代理完善~~ ✅ 已完成（`docker pull` 验证通过，修复 Cloudflare UA 拦截）
3. ~~端到端测试~~ ✅ 已完成（**13 个非 Docker 生态**由 `make test-docker-all` 串跑，Alpine 尚缺；Docker Registry 单独 opt-in）
4. ~~修复 fresh clone 不可 build 的 P0 bug~~ ✅ 已完成（commit a647104，rescue `internal/server/server.go` + 删 dead copy + 修 .gitignore 过宽规则）

**P1（应该做）：**

1. ~~带宽/流量统计仪表盘~~ ✅ 已完成（后端聚合 API + 前端 5 图表 + Dashboard 摘要，审查时发现已实现）
2. 高可用已降级：当前仅 SQLite；先实现 PostgreSQL 才能编写 PostgreSQL + S3 + 多实例指南
3. ~~CLI 工具（`depsilo status` / `depsilo cache warmup` / `depsilo doctor`）~~ ✅ 已完成（`internal/cli/` 13 个命令 + `doctor` 端到端自检带颜色 + JSON + 退出码）
4. ~~macOS menu-bar app~~ ✅ 已完成（`cmd/depsilo-tray/` + `internal/tray/` + `fyne.io/systray`；`make app-macos` 打 .app；LSUIElement 让 app 只在状态栏，无 Dock 图标；菜单含实时状态轮询、Open Admin / Portal、Run Doctor → osascript 通知、Quit）
5. ~~Linux tray 桌面集成~~ ✅ 已完成（`make install-linux` 装到 `~/.local/bin` + `.desktop` 应用菜单 + 自动渲染 icon；`make autostart-linux` 开机自启 symlink；同一 tray 二进制跨平台。GNOME on Wayland 需装 [AppIndicator extension](https://extensions.gnome.org/extension/615/appindicator-support/)）

**P2（可以做）：**

1. ~~包安全扫描完整流程~~ ✅ 已完成（OSV 集成 + Security 页 + 按生态策略）
2. ~~访问控制 allow/deny 规则~~ ✅ 已完成（`internal/rules/`，2026-06-28 起开源）
3. ~~Webhook 通知~~ ✅ 已完成（`internal/notify/`，Slack/钉钉/企微/飞书 + quarantine 阻断事件）
4. macOS app 代码签名 + notarization（v1 是 unsigned，Gatekeeper 会拦）
5. Linux `.deb` / `.rpm` / AppImage 正式包（`make install-linux` 已覆盖手动安装；正式 release 时用 `nfpm` 一键打包）
6. Windows 系统托盘 installer（systray 库已跨平台支持 Windows，只缺 `.msi` / `.exe` 打包脚本）

**建议推迟：**

- Wails 桌面版：投入产出比低，维护成本高（已被 menu-bar app 路线覆盖，更轻量）
- ~~License Key 系统~~ 已落地简化版（任意非空 key 激活 + 14 天试用，无远端校验）

> **2026-06-30 起，前瞻路线图以 `docs/DIRECTION.md` 的 T1/T2 build order 为准**。恶意包 blocklist 与篡改检测已完成；当前剩余重点是签名发布、freeze 快照和 CRA 完整 SBOM。本节仅保留历史记录。

### 11.7 商业化现状（2026-06-28 定价重置后）

**Open Core，但 Pro 面已大幅收窄**——审计日志、包规则（allow/deny）、安全扫描、供应链隔离全部下放开源（治理原语必须开源是 ADR-0003/0004 的原则）：

| 开源版（免费）                               | Pro 版（付费，Enterprise 合同制）  |
| -------------------------------------------- | ---------------------------------- |
| 14 个常规 adapter + Docker OCI（15 个 install surfaces）+ 缓存 | 多项目工作区（Projects） |
| 审计日志 / 包规则 / 安全扫描 / 隔离+审批流   | 每项目 SBOM 导出                   |
| Webhook 通知、带宽报表、CLI、托盘应用        | 优先技术支持                       |

实现：`internal/license/`（任意非空 key 即激活，无远端校验）+ `internal/trial/`（14 天本地试用）+ `internal/entitlement/`（`RequirePro` → 402）。

**目标用户：**

1. 国内中小企业（内网环境、代理加速、不想运维 Nexus）
2. CI/CD 场景（自建 runner 重复下载依赖）
3. 教育/科研（实验室统一管理 Python/R/Conda 包源）

### 11.8 推广策略

- README 需要 GIF/视频展示 "docker run → pip install 成功" 全流程（30 秒内）
- 目标渠道：Awesome 列表、Hacker News、Reddit r/selfhosted
- Docker Hub 一键部署：`docker run -p 23333:23333 depsilo/depsilo` 必须能直接跑

---

## Agent skills

### Issue tracker

Issues are tracked in GitHub Issues on `depsilo/depsilo`. See `docs/agents/issue-tracker.md`.

### Triage labels

Default label vocabulary (needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.


## gstack
Use /browse from gstack for all web browsing.
Available skills: /office-hours, /plan-ceo-review, /plan-eng-review,
/plan-design-review, /design-consultation, /design-shotgun, /design-html,
/review, /ship, /land-and-deploy, /qa, /cso, /autoplan, /investigate,
/retro, /learn ...
