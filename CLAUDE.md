# CLAUDE.md — Depsilo 项目全量指南

> 本文件是 Claude Code 的唯一权威参考。实现时严格遵循此文档，不得自行发明架构或引入未列出的依赖。

---

## 一、项目概述

**Depsilo** 是一个轻量级依赖包代理缓存网关，用 Go 编写，单二进制部署。

### 核心价值
- 支持 12 种主流包管理生态：pip、apt、npm、Go Modules、Cargo、Maven、RubyGems、Composer、NuGet、Conda、CRAN、Helm
- 局域网内秒级响应，所有生态共享同一套缓存引擎、存储后端和 Web UI
- 多上游源支持，自动健康检查与延迟优选
- 每个上游源可单独配置 HTTP 代理
- 统一 Web 入口：用户门户（无需登录）+ 管理后台（需登录）
- 新增生态通过 Adapter 插件实现，不影响核心逻辑

### 已支持的生态

| # | 协议 | 路径 | 语言/生态 | 代理类型 |
|---|------|------|-----------|----------|
| 1 | PyPI | `/pypi/` | Python (pip / uv / Poetry) | URL 重写（HTML） |
| 2 | APT | `/apt/` | Debian / Ubuntu | Passthrough |
| 3 | npm | `/npm/` | Node.js (npm / yarn / pnpm) | URL 重写（JSON） |
| 4 | Go Modules | `/go/` | Go | Passthrough |
| 5 | Cargo | `/crates/` | Rust | config.json 重写 |
| 6 | Maven | `/maven/` | Java / Kotlin / Gradle | Passthrough |
| 7 | RubyGems | `/rubygems/` | Ruby (bundler / gem) | Passthrough |
| 8 | Composer | `/composer/` | PHP (Packagist) | metadata-url 重写 |
| 9 | NuGet | `/nuget/` | .NET (dotnet) | service index 重写 |
| 10 | Conda | `/conda/` | Python 数据科学 | Passthrough |
| 11 | CRAN | `/cran/` | R | Passthrough |
| 12 | Helm | `/helm/` | Kubernetes Charts | Passthrough |

### 竞品定位
比 Nexus Repository 更轻量，10 分钟内完成部署，无复杂企业概念。

---

## 二、技术栈（不得替换）

| 组件 | 选型 | 说明 |
|------|------|------|
| 语言 | Go 1.21+ | 标准库优先 |
| HTTP 框架 | **Gin** (`github.com/gin-gonic/gin`) | v1.9+ |
| ORM | **GORM** (`gorm.io/gorm`) | 配套 SQLite/PostgreSQL driver |
| DB 迁移 | GORM `AutoMigrate` | 启动时自动执行 |
| S3 客户端 | `github.com/aws/aws-sdk-go-v2` | 兼容 MinIO |
| 配置 | `github.com/spf13/viper` | 读取 TOML 配置文件 |
| 日志 | `go.uber.org/zap` | 结构化日志 |
| 单飞 | `golang.org/x/sync/singleflight` | 防并发回源 |
| 限流 | `golang.org/x/time/rate` | 令牌桶，每上游独立限流 |
| 熔断 | `github.com/sony/gobreaker` | 上游请求熔断 |
| Metrics | `github.com/prometheus/client_golang` | 暴露 `/metrics` |
| 前端 | React 18 + TypeScript + Vite | 见第五节 |
| 前端组件库 | **shadcn/ui** + Tailwind CSS | 见第五节 |
| 前端打包 | Go `embed` | 编译进二进制 |

---

## 三、项目目录结构

严格按照以下结构创建文件，不得随意新增顶层目录：

```
depsilo/
├── cmd/
│   └── server/
│       └── main.go                  # 入口：加载配置、初始化、启动
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
│   │   ├── selector.go              # 选择策略：优先级 / 延迟优先
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
│   ├── tailwind.config.js
│   ├── components.json              # shadcn/ui 配置
│   └── src/
│       ├── main.tsx
│       ├── App.tsx                  # 路由：/ → Portal，/admin → AdminApp
│       ├── portal/                  # 用户门户（无需登录）
│       │   ├── PortalApp.tsx
│       │   ├── pages/
│       │   │   ├── QuickStart.tsx   # 快速开始（pip/apt 配置命令）
│       │   │   └── ServiceStatus.tsx# 服务状态（统计/上游/Top包）
│       │   └── components/
│       │       ├── CodeBlock.tsx    # 带复制按钮的代码块
│       │       └── ServiceUrlBar.tsx# 服务地址展示栏
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
│               ├── MetricCard.tsx
│               ├── UpstreamRow.tsx
│               └── TopPackageChart.tsx
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
port = 8080

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
    // 上报请求结果，用于更新延迟统计
    Report(name string, latency time.Duration, success bool)
}

// 两种策略，通过配置选择：
// - PrioritySelector：按 priority 字段顺序，健康则使用
// - LatencySelector：并发探测，选最快的健康上游
```

### 4.3 缓存核心流程（internal/cache/manager.go）

```
请求进入 handler
    │
    ├─ cache.Get(key) 命中？
    │   ├─ 是：直接 stream 给客户端，记录 HIT 日志
    │   └─ 否：
    │       └─ singleflight.Do(key, func() {
    │               upstream := selector.Select()
    │               resp := upstream.Fetch(req)   // 含 proxy、熔断、限流
    │               同时：stream 给第一个等待的客户端
    │                     写入 cache store
    │           })
    │           其余并发请求等待 singleflight 完成后从 cache 读取
    │
    └─ 写入 AccessLog（异步，不阻塞响应）
```

**关键约束：**
- 流式传输：不得将整个响应体 buffer 到内存，必须边读边写边转发
- 写缓存失败不影响响应，记录 warn 日志即可
- singleflight key 与 cache key 保持一致

### 4.4 PyPI 适配器要点（internal/adapter/pypi/）

- `GET /pypi/simple/` → 代理上游 simple index
- `GET /pypi/simple/:package/` → 代理并缓存，**必须重写响应 HTML 中所有下载 URL**
  - 将 `https://files.pythonhosted.org/packages/...` 重写为 `http://<本服务地址>/pypi/files/...`
  - 这是 PyPI 代理的关键步骤，遗漏则客户端会绕过缓存直接回源
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
- 支持 Packagist V2（p2）协议
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
    "version": "0.1.0",
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

```go
// 统一错误响应格式
type ErrorResponse struct {
    Code    string `json:"code"`    // 业务错误码，如 "UPSTREAM_UNAVAILABLE"
    Message string `json:"message"` // 人类可读描述
}

// 使用 thiserror 风格定义，通过 gin middleware 统一转换为 HTTP 状态码
// 404 → NotFound
// 502 → UpstreamError
// 503 → AllUpstreamsUnhealthy
// 507 → StorageFull
```

### 4.20 embed 前端打包（cmd/server/main.go）

```go
//go:embed all:web/dist
var webFS embed.FS

// 在 Gin 中挂载：
// - /assets/* → 静态文件
// - / 和 /admin → 返回 index.html（SPA fallback）
```

---

## 五、前端实现规范

### 5.1 技术栈

```
React 18 + TypeScript
Vite（构建工具）
shadcn/ui（组件库，按需安装）
Tailwind CSS（样式）
React Router v6（路由）
TanStack Query v5（数据请求 + 缓存）
axios（HTTP 客户端）
lucide-react（图标）
recharts（图表，shadcn chart 组件依赖）
```

### 5.2 需要安装的 shadcn/ui 组件

```bash
npx shadcn@latest add button card table input select badge
npx shadcn@latest add dialog dropdown-menu tabs toast
npx shadcn@latest add sidebar chart
```

### 5.3 路由结构（src/App.tsx）

```
/                    → PortalApp（用户门户，无需登录）
  /                  → QuickStart（默认页，快速开始）
  /status            → ServiceStatus（服务状态）

/admin               → AdminApp（管理后台）
  /admin/login       → Login（登录页，未认证时重定向到此）
  /admin             → Dashboard（默认页）
  /admin/cache       → CacheManage
  /admin/upstreams   → Upstreams
  /admin/logs        → AccessLogs
  /admin/users       → Users
  /admin/settings    → Settings
```

### 5.4 Portal 页面（用户门户）

#### PortalApp.tsx 布局
- 顶部 header：Logo + 导航（快速开始 / 服务状态）+ 右侧状态 pill + "管理后台 →" 链接
- 内容区：无侧边栏，全宽内容
- 无需登录，直接访问

#### QuickStart.tsx（快速开始页）
**设计要求：**
- 顶部展示服务地址栏（`ServiceUrlBar` 组件），从 `/api/v1/stats` 获取服务信息
- 12 个包管理器 Tab（4 列网格布局），每个 Tab 包含对应生态的配置步骤
- 覆盖：pip、apt、npm、Go、Cargo、Maven、RubyGems、Composer、NuGet、Conda、CRAN、Helm
- 每个代码块使用 `CodeBlock` 组件，支持一键复制
- 底部 tip 提示（首次回源说明 / GPG 签名说明）
- 服务地址从 `window.location.origin` 自动拼接，**不得写死**

#### ServiceStatus.tsx（服务状态页）
**设计要求：**
- 4 个 MetricCard：今日请求数、缓存命中率、节省流量（本月）、平均响应时间
- 命中率环形图：PyPI 和 APT 分别展示（使用 recharts `RadialBarChart`）
- 上游源健康状态列表：名称、URL、状态（健康/延迟高/异常）、延迟 ms
- 热门包 Top 10：PyPI 和 APT 并排，横向进度条 + 请求次数
- 数据来源：`GET /api/v1/stats`，每 30 秒自动刷新

#### CodeBlock 组件（portal/components/CodeBlock.tsx）
```tsx
interface CodeBlockProps {
  filename?: string     // 可选文件名标注
  code: string          // 代码内容（支持多行）
  language?: string     // 语法高亮标识（展示用）
}
// 右上角复制按钮，点击后变为"已复制"并 2 秒后恢复
// 使用 shadcn 的 pre/code 样式，monospace 字体
```

#### ServiceUrlBar 组件（portal/components/ServiceUrlBar.tsx）
```tsx
// 展示：标签"服务地址" + URL 文本 + 复制按钮
// URL 从 window.location.origin 读取，自动适配部署地址
```

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
- 顶部 4 个 MetricCard（同 Portal 统计页）
- 请求量折线图（近 7 日，按天，PyPI/APT 两条线，使用 recharts LineChart）
- 上游源状态卡片（同 Portal，但包含更多细节：可用率 %）
- 热门包 Top10（PyPI + APT 并排）

#### CacheManage.tsx
- 顶部 3 个存储概览 MetricCard：PyPI 缓存大小/文件数、APT 缓存大小/文件数、总用量进度条
- 搜索栏 + 类型筛选下拉（全部/PyPI/APT）+ "清理过期缓存"按钮（红色，需确认弹窗）
- 表格列：包名/文件名、类型、大小、命中次数、最后访问、过期时间、操作（删除）
- 分页（每页 20 条）
- 删除操作需 Dialog 确认

#### Upstreams.tsx
- 右上角"添加上游源"按钮
- 12 个生态的 Pill 按钮筛选器（pypi / apt / npm / go / cargo / maven / rubygems / composer / nuget / conda / cran / helm）
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
- 数据库类型 select（SQLite/PostgreSQL），选 PostgreSQL 展示 DSN input

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

dev:           # 同时启动前端 dev server 和后端（go run，热重载用 air）
frontend:      # cd web && npm run build → 产物在 web/dist
build:         # make frontend && go build -o bin/depsilo ./cmd/server
test:          # go test ./...
lint:          # golangci-lint run && cd web && npm run type-check
docker:        # docker build -t depsilo .
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
FROM golang:1.21-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
RUN go build -o depsilo ./cmd/server

# Stage 3: 最终镜像
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=backend /app/depsilo .
EXPOSE 8080
CMD ["./depsilo"]
```

### docker-compose.yml

```yaml
version: '3.8'
services:
  depsilo:
    build: .
    ports:
      - "8080:8080"
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
6. **验收：`go run ./cmd/server` 启动无报错，`GET /health` 返回 200**

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
1. 初始化 React 项目，配置 shadcn/ui
2. 实现 Portal（用户门户）：QuickStart + ServiceStatus + PackageBrowse + LiveStream
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

### 后续 Phase（待开发）
- Docker Registry 代理
- 审计日志
- 包 allow/deny 规则
- License Key 验证系统

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

| 陷阱 | 正确做法 | 为什么 |
| ---- | ------- | ------ |
| 在 passthrough adapter（APT/Go/Maven/CRAN/Helm）中修改响应体 | **绝对不能修改**，原样透传 | 修改会破坏 GPG 签名，客户端拒绝安装 |
| 把整个响应 body 读进内存再转发 | **必须流式传输** `io.Copy` / `io.TeeReader` | torch 包 2GB+，buffer 会 OOM |
| 前端改了文字但只改了 zh.ts | **zh.ts 和 en.ts 必须同时更新** | 否则另一语言显示 key 而非文字 |
| URL 重写时遗漏某些链接 | 用**正则/HTML parser 全量替换**，不要只替换"看到的那个" | 遗漏一个 href，pip/npm 就会绕过缓存 |
| 在前端代码中硬编码 `localhost:8080` | 用 `window.location.origin` | 部署地址不是 localhost |
| 忽略 Go error（`_ = xxx`） | **所有 error 必须处理或传递** | 静默吞错导致难以排查的线上问题 |
| 新增文件放在错误的目录 | 严格按照第三节目录结构 | 项目约定不可随意创建顶层目录 |

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
4. **singleflight 与流式的兼容**：第一个请求 stream 给客户端的同时写缓存，后续相同 key 的请求等待完成后从缓存读取（不复用第一个请求的 body）
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
- [shadcn/ui 文档](https://ui.shadcn.com/)

---

## 十一、项目现状快照（2026-04-19 更新）

> 本节记录项目深度审查的结论，供后续开发决策参考。上次更新：2026-04-19。

### 11.1 规模统计

- 208 个 commits，~13,500 行 Go 代码，117 个 Go 文件
- 前端 50 个 TSX/TS 文件，~7,000+ 行
- 22 个测试文件（单元 + 集成）
- 13 个生态适配器已实现（含 Docker Registry，已验证 `docker pull` 可用）
- 10 个设计 spec 文档
- Docker 冒烟测试覆盖 pip / npm / go / apt（`make test-e2e`）

### 11.2 架构优势

- **Adapter 模式**：接口只有 `Register()` + `Type()`，新增生态不碰核心代码
- **Stale-While-Revalidate 缓存**：过期不删除、后台异步刷新、上游故障降级返回旧缓存
- **Singleflight 防击穿**：同一 key 并发请求只回源一次
- **流式传输**：上游响应通过 `countingReader` 直接流向 `storage.Put`，不做内存缓冲
- **依赖注入**：`router.go` 的 `Deps` 结构体传递所有依赖，无全局变量
- **表驱动初始化**：`server.go` 使用 `ecosystemDef` 表 + 循环注册 12 个生态
- **前端完成度高**：React 19 + TanStack Query + i18n + 暗色主题 + Stripe 风格设计系统

### 11.3 已修复的技术债（2026-04-19）

| 问题 | 修复方式 | Commits |
| ---- | -------- | ------- |
| Cache Manager 大文件 OOM | `bytes.Buffer` → `countingReader` 流式直写 storage | c55b7ce, a5c76b2, 9264951, c6b5d13 |
| `io.Copy` 错误被忽略 | 15 处 adapter 改为 `zap.L().Warn` 记录错误 | 5bdbba4 |
| `server.go` 过长 (408 行) | 表驱动循环，-64 行 (→344 行) | 7b3a44a |
| 前端工具函数重复 | 提取到 `web/src/lib/utils.ts`，11 文件清理 | 5380960 |
| Docker Registry Cloudflare 403 | 所有上游请求加 `User-Agent: docker/27.0.0 depsilo` | b04314f |

### 11.4 剩余已知问题

| 问题 | 严重度 | 说明 |
| ---- | ------ | ---- |
| i18n key 无编译时校验 | 低 | en.ts 与 zh.ts 目前同步（478 key），但缺少自动检查机制 |
| `api.Deps` 仍有 12 个独立 Pool 字段 | 低 | 可改为 `Pools map[string]*Pool`，需联动改 dashboard.go / stats.go |
| 部分 `formatTime` 变体未统一 | 低 | Projects/Rules/Security/LiveStream/Monitor 有不同逻辑的本地实现 |
| Docker Registry 缺少 E2E 冒烟测试 | 低 | `docker pull` 已手动验证，但未加入 `make test-e2e`（需要 Docker-in-Docker） |
| Docker token 缓存非持久化 | 低 | 重启后丢失，影响首次请求延迟（~1s），非功能性问题 |

### 11.5 竞品对比定位

| &nbsp; | Nexus / Artifactory | Verdaccio | **Depsilo** |
| ------ | ------------------- | --------- | ----------- |
| 部署复杂度 | 高（JVM） | 低（仅 npm） | **极低（单二进制）** |
| 生态覆盖 | 广 | 仅 npm | **13 种** |
| 资源占用 | 重（GB 级内存） | 轻 | **轻（Go + SQLite）** |
| 学习成本 | 高 | 低 | **低** |

核心 slogan：**"10 分钟部署，13 种生态"**

### 11.6 路线图优先级

**P0（必须做）：**

1. ~~修复 cache manager 大文件内存问题~~ ✅ 已完成（`countingReader` 流式写入）
2. ~~Docker Registry 代理完善~~ ✅ 已完成（`docker pull` 验证通过，修复 Cloudflare UA 拦截）
3. ~~端到端测试~~ ✅ 已完成（Docker 冒烟测试：pip/npm/go/apt 4/4 通过，`make test-e2e`）

**P1（应该做）：**

1. 带宽/流量统计仪表盘（帮用户量化节省的带宽）
2. 高可用部署文档（PostgreSQL + S3 + 多实例）
3. CLI 工具（`depsilo status` / `depsilo cache warmup`）

**P2（可以做）：**

1. 包安全扫描完整流程（OSV 已集成，需跑通展示）
2. 访问控制 allow/deny 规则
3. Webhook 通知（缓存未命中、上游异常 → Slack/DingTalk）

**建议推迟：**

- Wails 桌面版：投入产出比低，维护成本高
- License Key 系统：除非确定走商业化 Pro 版

### 11.7 商业化方向

**推荐 Open Core 模式：**

| 开源版（免费） | Pro 版（付费） |
| -------------- | -------------- |
| 所有 13 种生态代理 | 审计日志 |
| 本地存储 + SQLite | SBOM 导出 |
| 单用户管理 | 多项目/多团队 |
| 基础统计 | 高级报表 + 趋势分析 |
| | LDAP/OIDC 集成 |
| | 包安全扫描 |
| | 优先技术支持 |

**目标用户：**

1. 国内中小企业（内网环境、代理加速、不想运维 Nexus）
2. CI/CD 场景（自建 runner 重复下载依赖）
3. 教育/科研（实验室统一管理 Python/R/Conda 包源）

### 11.8 推广策略

- README 需要 GIF/视频展示 "docker run → pip install 成功" 全流程（30 秒内）
- 目标渠道：Awesome 列表、Hacker News、Reddit r/selfhosted
- Docker Hub 一键部署：`docker run -p 8080:8080 depsilo/depsilo` 必须能直接跑
