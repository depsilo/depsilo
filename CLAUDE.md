# CLAUDE.md — RepoCache 项目全量指南

> 本文件是 Claude Code 的唯一权威参考。实现时严格遵循此文档，不得自行发明架构或引入未列出的依赖。

---

## 一、项目概述

**RepoCache** 是一个轻量级依赖包代理缓存网关，用 Go 编写，单二进制部署。

### 核心价值
- 缓存 pip / apt 依赖包，局域网内秒级响应
- 多上游源支持，自动健康检查与延迟优选
- 每个上游源可单独配置 HTTP 代理
- 统一 Web 入口：用户门户（无需登录）+ 管理后台（需登录）

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
repocache/
├── cmd/
│   └── server/
│       └── main.go                  # 入口：加载配置、初始化、启动
├── internal/
│   ├── config/
│   │   ├── config.go                # 配置结构体定义
│   │   └── loader.go                # viper 加载逻辑
│   ├── adapter/
│   │   ├── interface.go             # Adapter interface 定义
│   │   ├── pypi/
│   │   │   ├── handler.go           # Gin handler（simple API + 文件下载）
│   │   │   ├── rewriter.go          # HTML 响应中的下载 URL 重写
│   │   │   └── keyer.go             # cache key 生成
│   │   └── apt/
│   │       ├── handler.go           # Gin handler（Release/Packages/.deb）
│   │       └── keyer.go             # cache key 生成
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
dsn    = "./data/repocache.db"

[storage]
type = "local"              # local | s3
path = "./data/cache"

# S3 配置（type = "s3" 时生效）
# bucket   = "repocache"
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

### 4.6 GORM 数据模型（internal/db/models.go）

```go
type CacheEntry struct {
    ID           uint      `gorm:"primarykey"`
    Key          string    `gorm:"uniqueIndex;size:512"`
    AdapterType  string    `gorm:"size:16;index"`  // pypi | apt
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

### 4.7 API 路由总表（internal/api/router.go）

```
# 公开路由（无需认证）
GET  /                              → 前端 Portal SPA
GET  /admin                         → 前端 Admin SPA
GET  /assets/*                      → 前端静态资源（embed）

GET  /api/v1/stats                  → 公开统计（命中率、Top包、上游状态）
GET  /health                        → 健康检查
GET  /metrics                       → Prometheus metrics

# PyPI 代理（无需认证，支持 Token 可选认证）
GET  /pypi/simple/
GET  /pypi/simple/:package/
GET  /pypi/files/*filepath

# APT 代理（无需认证）
GET  /apt/:repo/dists/*filepath
GET  /apt/:repo/pool/*filepath

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

### 4.8 公开统计 API 响应格式（GET /api/v1/stats）

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

### 4.9 错误处理规范

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

### 4.10 embed 前端打包（cmd/server/main.go）

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
- pip / apt 两个 Tab（shadcn Tabs 组件）
- **pip Tab** 分三个步骤：
  1. 临时使用（单次命令）
  2. 永久配置（`~/.config/pip/pip.conf`）
  3. Poetry / uv 用户配置
- **apt Tab** 分三个步骤：
  1. 添加 sources.list.d 配置文件
  2. 一键替换现有源（sed 命令）
  3. 验证配置（apt update 命令）
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
- PyPI / APT 两个 Tab
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
build:         # make frontend && go build -o bin/repocache ./cmd/server
test:          # go test ./...
lint:          # golangci-lint run && cd web && npm run type-check
docker:        # docker build -t repocache .
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
RUN go build -o repocache ./cmd/server

# Stage 3: 最终镜像
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=backend /app/repocache .
EXPOSE 8080
CMD ["./repocache"]
```

### docker-compose.yml

```yaml
version: '3.8'
services:
  repocache:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data
      - ./config.toml:/app/config.toml
    environment:
      - REPOCACHE_CONFIG=/app/config.toml
    restart: unless-stopped
```

---

## 七、开发启动顺序

Claude Code 应按以下顺序实现，每完成一步确保可运行后再进行下一步：

### Phase 1：后端骨架（可跑通健康检查）
1. `go mod init repocache`，添加所有依赖到 go.mod
2. 实现 `config.go` + `loader.go`（viper 读取 TOML）
3. 实现 `main.go`（初始化 Gin、注册 `/health` 路由）
4. 实现 GORM 模型 + AutoMigrate（SQLite）
5. 实现本地存储 `local.go`
6. **验收：`go run ./cmd/server` 启动无报错，`GET /health` 返回 200**

### Phase 2：PyPI 代理核心
1. 实现 upstream pool + priority selector
2. 实现 cache manager（单飞 + 流式写入）
3. 实现 PyPI adapter（simple API + URL 重写 + 文件下载）
4. **验收：`pip install requests -i http://localhost:8080/pypi/simple/ --trusted-host localhost` 成功**

### Phase 3：APT 代理
1. 实现 APT adapter（Release/Packages/.deb passthrough）
2. **验收：配置 sources.list 后 `apt update` 成功**

### Phase 4：管理 API
1. 实现 JWT 认证（login/logout/middleware）
2. 实现所有 `/api/v1/admin/*` 路由
3. 实现公开统计 `/api/v1/stats`

### Phase 5：前端
1. 初始化 React 项目，配置 shadcn/ui
2. 实现 Portal（用户门户）：QuickStart + ServiceStatus
3. 实现 Admin（管理后台）：按 Dashboard → 缓存 → 上游 → 日志 → 用户 → 设置 顺序

### Phase 6：收尾
1. S3 存储实现（`s3.go`）
2. Prometheus metrics
3. Dockerfile + docker-compose
4. 健康检查完善（upstream 定时检查）
5. LRU 淘汰后台 goroutine

---

## 八、编码规范

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
5. **服务地址动态化**：前端代码块中的 pip/apt 配置命令里的服务地址，必须用 `window.location.origin` 动态生成，不能写死
6. **Token 安全**：数据库只存 token 的 hash（bcrypt 或 SHA-256），明文只在生成时展示一次
7. **SQLite 并发**：开启 WAL 模式（`PRAGMA journal_mode=WAL`），避免写锁争用

---

## 十、参考资料

- [PyPI Simple Repository API (PEP 503)](https://peps.python.org/pep-0503/)
- [PyPI JSON API (PEP 691)](https://peps.python.org/pep-0691/)
- [APT Repository Format](https://wiki.debian.org/DebianRepository/Format)
- [Gin 文档](https://gin-gonic.com/docs/)
- [GORM 文档](https://gorm.io/docs/)
- [TanStack Query 文档](https://tanstack.com/query/latest)
- [shadcn/ui 文档](https://ui.shadcn.com/)
