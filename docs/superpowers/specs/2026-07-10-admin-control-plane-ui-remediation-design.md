# Admin 控制面与 UI 全面修复设计

日期：2026-07-10

状态：已确认（配置权威、权限契约、UI 与验证三部分均经用户确认）

## 背景

Admin 已基本采用 Portal 的 Instrument 视觉语言，但一次完整审查发现三类系统性问题：

1. 部分操作显示成功，实际运行状态或持久化状态没有改变；
2. 前后端 DTO、权限和错误状态存在漂移；
3. 移动布局、表格、键盘交互、颜色对比度和旧组件没有统一收敛。

本期不是重新设计 Admin，而是把它修成可信、可操作、可验证的运维控制面。
Portal 继续作为视觉和交互基线，Admin 保持密集、安静、适合重复操作的定位。

## 目标

- Settings 保存后持久化到配置文件，并准确区分立即生效与重启生效。
- Upstream CRUD 在请求成功后立即改变实际代理流量。
- Admin 的读取、写入、readonly 和 API Token 权限在前后端保持一致。
- 修复 Security、Logs、Audit、Projects 等已知 API 契约漂移。
- 所有 Admin 页面明确区分 loading、empty、error、stale 和 permission denied。
- 13 个 Admin 路由在移动端、桌面端、明暗主题和中英文下可用。
- 共享表单、Dialog、Tabs、Tooltip、Toast 和图标按钮达到 WCAG 2.1 AA 的核心要求。
- 建立可重复执行的 Go 契约测试和真实浏览器回归测试。

## 非目标

- 不改变 Portal 的信息架构或增加营销页面。
- 不把所有启动级配置强行改成热更新。
- 不自动重启 Depsilo 进程；进程管理仍由部署环境负责。
- 不让 Docker registry 复用普通 HTTP Upstream CRUD；Docker 保留独立配置语义。
- 不引入新的前端运行时设计系统；继续包装现有 `@base-ui/react`。
- 不在本期清零整个仓库的历史 ESLint 基线，只要求修改文件不新增问题，并修复触及范围内的相关错误。

## 总体架构

控制面分成三类权威数据：

| 数据 | 权威源 | 生效方式 |
| --- | --- | --- |
| 启动级 Settings | `config.toml` | 原子持久化；部分立即生效，其余等待重启 |
| 动态 Upstreams | 数据库 | Registry 原子更新运行 Pool，立即生效 |
| 用户、Token、规则、Webhook、安全策略等 | 数据库 | 现有 handler 迁移到显式契约和权限模型 |

前端不得根据 HTTP 200 自行推断“已生效”。成功提示必须来自服务端返回的
`applied_now`、`restart_required` 或资源运行快照。

## Settings 控制面

### 权威与写入

- `config.toml` 是 Settings 的持久化权威源。
- 新增并发安全的 `config.Store`，每次更新都重新读取当前文件，只修改白名单字段。
- 更新流程为：解析当前 TOML、合并白名单 patch、完整校验、写入同目录临时文件、
  `fsync`、保留文件权限并原子 rename。
- 未修改的 section、键、注释和排版必须保留；不得把整份配置重新序列化成新格式。
- 配置只读、目录不可写或原子替换失败时返回 `CONFIG_READ_ONLY` 或
  `CONFIG_WRITE_FAILED`，运行状态和 UI 状态都不得伪装成功。
- 环境变量覆盖通过响应的 `sources` 映射标记为 `env`，并在 `overrides` 中给出变量名。
  文件仍可更新，但 UI 必须说明当前 effective 值仍由环境变量决定。

### 可编辑字段

本期允许 Admin 修改：

- `server.log_level`
- `cache.max_size_gb`
- `cache.ttl_index`
- `cache.ttl_blob`
- `cache.lru_threshold`
- `auth.token_ttl`

`server.host`、`server.port`、数据库和存储后端继续只读展示。`auth.enabled` 当前没有完整的
运行语义，从 Admin 表单移除，但不删除已有配置键。

### 生效语义

- `server.log_level` 在持久化成功后通过共享的 `zap.AtomicLevel` 立即应用。
- Cache 和 Auth 字段先持久化，列入 `restart_required`；重启前 effective 值保持不变。
- 非法 duration、缓存容量小于等于零、LRU 阈值不在 `1..100`、不支持的
  `token_ttl=never` 都返回 `422 INVALID_SETTING`，不得静默忽略。

### API 契约

Settings 统一使用下列完整数据结构。字符串 duration 始终采用 Go duration 格式；缺失键先应用
内置默认值，因此 `configured` 和 `effective` 中的字段始终存在且不可为 `null`。

```ts
interface AdminSettingsSnapshot {
  server: { host: string; port: number; log_level: 'debug' | 'info' | 'warn' | 'error' }
  database: { driver: string }
  storage: { type: string; path: string }
  cache: {
    max_size_gb: number
    ttl_index: string
    ttl_blob: string
    lru_threshold: number
  }
  auth: { token_ttl: string }
}

type SettingPath =
  | 'server.host' | 'server.port' | 'server.log_level'
  | 'database.driver' | 'storage.type' | 'storage.path'
  | 'cache.max_size_gb' | 'cache.ttl_index' | 'cache.ttl_blob'
  | 'cache.lru_threshold' | 'auth.token_ttl'

type EditableSettingPath =
  | 'server.log_level'
  | 'cache.max_size_gb' | 'cache.ttl_index' | 'cache.ttl_blob'
  | 'cache.lru_threshold' | 'auth.token_ttl'

type SettingSource = 'default' | 'file' | 'env'

interface AdminSettingsResponse {
  configured: AdminSettingsSnapshot
  effective: AdminSettingsSnapshot
  pending_restart: EditableSettingPath[]
  overrides: Partial<Record<SettingPath, string>>
  sources: Record<SettingPath, SettingSource>
  editable: EditableSettingPath[]
  config_writable: boolean
}
```

`GET /api/v1/admin/settings` 返回：

```json
{
  "configured": {
    "server": {"host": "127.0.0.1", "port": 23333, "log_level": "info"},
    "database": {"driver": "sqlite"},
    "storage": {"type": "local", "path": "./data/cache"},
    "cache": {"max_size_gb": 20, "ttl_index": "5m", "ttl_blob": "96h", "lru_threshold": 90},
    "auth": {"token_ttl": "168h"}
  },
  "effective": {
    "server": {"host": "127.0.0.1", "port": 23333, "log_level": "debug"},
    "database": {"driver": "sqlite"},
    "storage": {"type": "local", "path": "./data/cache"},
    "cache": {"max_size_gb": 20, "ttl_index": "5m", "ttl_blob": "72h", "lru_threshold": 90},
    "auth": {"token_ttl": "168h"}
  },
  "pending_restart": ["cache.ttl_blob"],
  "overrides": {"server.log_level": "DEPSILO_SERVER_LOG_LEVEL"},
  "sources": {
    "server.host": "file",
    "server.port": "file",
    "server.log_level": "env",
    "database.driver": "file",
    "storage.type": "file",
    "storage.path": "file",
    "cache.max_size_gb": "file",
    "cache.ttl_index": "file",
    "cache.ttl_blob": "file",
    "cache.lru_threshold": "file",
    "auth.token_ttl": "file"
  },
  "editable": ["server.log_level", "cache.max_size_gb", "cache.ttl_index", "cache.ttl_blob", "cache.lru_threshold", "auth.token_ttl"],
  "config_writable": true
}
```

`sources` 必须覆盖全部 `SettingPath`。`pending_restart` 只包含未被环境变量覆盖、且
configured 与 effective 不同的重启生效字段。

`PUT /api/v1/admin/settings` 只接收以下嵌套白名单 patch，所有字段可选，但 body 至少包含
一个字段：

```ts
interface UpdateAdminSettingsRequest {
  server?: { log_level?: 'debug' | 'info' | 'warn' | 'error' }
  cache?: {
    max_size_gb?: number
    ttl_index?: string
    ttl_blob?: string
    lru_threshold?: number
  }
  auth?: { token_ttl?: string }
}

interface UpdateAdminSettingsResponse extends AdminSettingsResponse {
  changed: EditableSettingPath[]
  applied_now: EditableSettingPath[]
  restart_required: EditableSettingPath[]
  blocked_by_override: EditableSettingPath[]
}
```

响应返回完整 configured/effective snapshot 和精确结果集合。例如在 log level 被环境变量
覆盖，同时修改 cache TTL 时：

```json
{
  "configured": {
    "server": {"host": "127.0.0.1", "port": 23333, "log_level": "info"},
    "database": {"driver": "sqlite"},
    "storage": {"type": "local", "path": "./data/cache"},
    "cache": {"max_size_gb": 20, "ttl_index": "5m", "ttl_blob": "96h", "lru_threshold": 90},
    "auth": {"token_ttl": "168h"}
  },
  "effective": {
    "server": {"host": "127.0.0.1", "port": 23333, "log_level": "debug"},
    "database": {"driver": "sqlite"},
    "storage": {"type": "local", "path": "./data/cache"},
    "cache": {"max_size_gb": 20, "ttl_index": "5m", "ttl_blob": "72h", "lru_threshold": 90},
    "auth": {"token_ttl": "168h"}
  },
  "pending_restart": ["cache.ttl_blob"],
  "overrides": {"server.log_level": "DEPSILO_SERVER_LOG_LEVEL"},
  "sources": {
    "server.host": "file",
    "server.port": "file",
    "server.log_level": "env",
    "database.driver": "file",
    "storage.type": "file",
    "storage.path": "file",
    "cache.max_size_gb": "file",
    "cache.ttl_index": "file",
    "cache.ttl_blob": "file",
    "cache.lru_threshold": "file",
    "auth.token_ttl": "file"
  },
  "editable": ["server.log_level", "cache.max_size_gb", "cache.ttl_index", "cache.ttl_blob", "cache.lru_threshold", "auth.token_ttl"],
  "config_writable": true,
  "changed": ["server.log_level", "cache.ttl_blob"],
  "applied_now": [],
  "restart_required": ["cache.ttl_blob"],
  "blocked_by_override": ["server.log_level"]
}
```

被环境变量覆盖的字段仍写入文件并列入 `changed`，但不列入 `applied_now` 或
`restart_required`，而是列入 `blocked_by_override`。只要环境覆盖存在，重启也不会改变其
effective 值。没有环境覆盖的 `server.log_level` 才进入 `applied_now`。

Settings 页面分别展示 configured/effective 差异、环境变量覆盖和等待重启状态。

## 动态 Upstream Registry

### 数据权威

- 首次升级时，从 `config.toml` 的普通生态 Upstream 列表导入数据库。
- 新增 `db.ControlPlaneState` 键值模型：`Key string` 为主键、`Value string`、
  `UpdatedAt time.Time`；以 `upstreams_seeded_v1=true` 记录一次性 seed 完成状态，并以
  `upstreams_active_ecosystems_v1` 保存 JSON string array。
- seed 未完成且数据库已有旧版同步记录时，只补齐 config 中缺失的 `(adapter_type,name)`，
  不覆盖现有记录，随后在同一事务写入 marker。之后数据库成为普通 Upstream 的权威源，重启不得把
  用户已经删除的记录重新从配置文件写回。
- `config.toml` 中的 Upstream 列表保留为首次启动默认值，文档必须明确这一迁移语义。
- `supported ecosystems` 是编译期 adapter 定义；`active ecosystems` 是其中在 config 或旧数据库
  至少存在一个 Upstream 的生态。首次 seed 计算 active 集合，并与 seed marker 在同一事务持久化。
  后续启动若 config 为此前未激活的受支持生态首次提供 Upstream，则把该生态导入并追加到 active
  集合；现有 active 生态不再被 config 覆盖。
- 服务启动顺序固定为：数据库迁移 -> seed/reconcile 元数据 -> 从数据库构建 Registry 和 Pools
  -> 仅为 active ecosystems 构建 Adapters/Routes -> 启动健康 workers。后续启动的 Pool 不能先从
  config 构建；inactive ecosystem 不创建 Pool、Adapter 或代理 route。
- active 集合中的每个生态启动时必须在数据库至少有一个 Upstream，否则启动失败并报告
  `active ecosystem <name> has no upstreams`。inactive 生态允许零记录，且不参与最后源保护。
- Registry 支持 PyPI、APT、npm、Go、Cargo、Maven、RubyGems、Composer、NuGet、Conda、
  CRAN、Alpine、Helm 和 Hugging Face。Docker 不进入此 Registry。
- 不在 active 集合中的生态不能通过 Admin 临时启用；创建请求返回
  `409 ECOSYSTEM_NOT_ACTIVE`，需要先修改启动配置并重启。

### 运行模型

- 新增 `upstream.Registry`，统一拥有数据库 CRUD、Pool reconcile 和健康探测 worker 生命周期。
- `upstream.Pool` 使用 `atomic.Pointer[poolSnapshot]` 持有不可变的 Upstream slice，并提供
  `Snapshot()` 和原位 `Replace()`；单个 Upstream 的健康统计继续由其自身锁保护。
- Adapter 和路由继续持有同一个 Pool 指针；Registry 替换快照后，下一次请求立即看到新源。
- Selector 只读取不可变快照，不得无锁遍历可变 slice。
- 每个动态 worker 有独立 cancel；更新 interval 时重启对应 worker，删除时确保 worker 退出。

### Mutation 原子性

Registry 对每个 ecosystem 持有 mutation mutex。固定流程为：

1. 校验生态、URL、代理、priority、probe mode 和 interval；
2. 开启数据库事务，在事务视图内应用 mutation 并读取该生态的完整目标记录；
3. 在事务提交前预建不可变 Pool snapshot、HTTP clients 和无失败返回值的 worker plan；
4. 提交数据库事务；提交失败则丢弃预建对象，运行 Pool 不变；
5. 通过单次原子指针写入替换 Pool snapshot；该操作必须设计为不可失败；
6. 应用只包含 cancel/start 的 worker plan；探测网络失败只更新健康状态，不使 reconcile 失败；
7. 同步比较 Registry snapshot 与已提交记录，匹配后才返回成功。

进程如果在数据库 commit 后、内存 swap 前退出，下一次启动必须从数据库完整 reconcile。
如果 swap 后的同步不变量检查意外失败，Registry 立即从数据库重新构建一次；重建仍失败时
标记该生态 degraded、记录 error，并返回 `500 REGISTRY_RECONCILE_FAILED`。成功响应因此始终
表示数据库与请求流量使用的 snapshot 已一致。

每个 active ecosystem 至少保留一个 Upstream。删除最后一个返回 `409 LAST_UPSTREAM`。
手动 Check 必须复用该 Upstream 的代理 client，并把结果写入对应 `upstream_id`。

### API 契约

现有路由保持不变：

| Method | Path | 语义 |
| --- | --- | --- |
| GET | `/api/v1/admin/upstreams` | 返回所有 active ecosystem 的运行资源 |
| POST | `/api/v1/admin/upstreams` | 创建并立即进入运行 Pool |
| PUT | `/api/v1/admin/upstreams/:id` | 完整替换可编辑字段；`adapter_type` 不可改变 |
| DELETE | `/api/v1/admin/upstreams/:id` | 删除；成功返回 `200` |
| POST | `/api/v1/admin/upstreams/:id/check` | 立即探测并记录结果 |

Create/PUT body：

```ts
interface UpstreamMutationRequest {
  adapter_type: string
  name: string
  url: string
  proxy: string
  priority: number
  probe_mode: 'active' | 'passive'
  probe_interval: string
}
```

PUT 的 `adapter_type` 必须与现有资源相同，否则返回 `422 IMMUTABLE_ECOSYSTEM`。Create 和
PUT 成功均返回：

```ts
interface AdminUpstream {
  id: number
  adapter_type: string
  name: string
  url: string
  proxy: string
  priority: number
  probe_mode: 'active' | 'passive'
  probe_interval: string
  healthy: boolean
  avg_latency_ms: number
  success_rate: number
  last_checked_at: string | null
  worker_running: boolean
  created_at: string
  updated_at: string
}
```

List 返回 `{items: AdminUpstream[], total: number}`。Delete 返回
`{deleted_id: number, adapter_type: string}`。Check 无论目标健康与否都返回 `200` 和
`{upstream: AdminUpstream, check: {healthy: boolean, latency_ms: number, checked_at: string, error: string | null}}`；
只有请求无效或 Registry 自身失败才使用 4xx/5xx。

规范错误包括 `400 BAD_REQUEST`、`404 NOT_FOUND`、`409 CONFLICT`、
`409 LAST_UPSTREAM`、`409 ECOSYSTEM_NOT_ACTIVE`、`422 INVALID_UPSTREAM` 和
`500 REGISTRY_RECONCILE_FAILED`。

## API 契约修复

### 类型边界

- Go handler 使用显式 request/response struct，不直接把 GORM model 当作长期 API 设计。
- 前端新增 `web/src/lib/adminApi.types.ts`，覆盖 Settings、Principal、Upstream、Security、
  Logs、Audit 和 Projects；相关 Axios 调用使用泛型，移除这些路径上的 `any`。
- 保留现有 `{code,message}` 错误格式。
- 不引入 `/v2`。旧查询参数别名兼容一个发布版本，响应只返回规范字段。

### 已知漂移

- Security Policy 统一为 `auto_block_enabled` 和 `min_cvss_score`，阈值校验 `0..10`。
- Security 搜索规范参数为 `package`，后端临时兼容 `q`。
- Audit 搜索规范参数为 `package`，后端临时兼容 `search`。
- 新增 `GET /admin/logs/export`；List 和 Export 复用同一过滤器与 CSV 编码器。
- Projects package 使用 `package_name`、`first_seen_at`、`last_seen_at`、
  `download_count`；代理路径回退为 `/p/{slug}`。

## 权限模型

### Principal

- 新增 `GET /api/v1/auth/me`，由通用认证 middleware 保护，JWT 和 API Token 均可调用。
- Principal 响应固定为：

```ts
interface Principal {
  id: number
  username: string
  role: 'admin' | 'readonly'
  enabled: true
  auth_method: 'jwt' | 'api_token'
  token_permissions: 'readonly' | 'readwrite' | null
  can_write: boolean
}
```

JWT 的 `token_permissions` 为 `null`。API Token 的 `can_write` 只有在所属用户当前仍为 admin
且 Token permissions 为 `readwrite` 时才为 true。禁用用户不会得到 Principal，而是收到 401。
- 前端通过 `usePrincipal()` 使用服务端事实，不再让各页面解析 localStorage 推断权限。
- JWT 请求每次回查当前用户的 `enabled` 和 role；禁用或降权立即生效。

### 路由能力

- Admin 的全部 GET 路由（包括 CSV/SBOM 导出）属于读取能力，允许 `admin` 和 `readonly`。
- `POST /admin/rules/test` 是唯一归入读取能力的非 GET Admin 路由。
- 其余 POST、PUT、PATCH 和 DELETE 全部属于写能力，包括 Upstream Check、缓存预热/清理、
  Security Scan、blocklist sync、Webhook test、Token 生成和 License 操作，只允许可写 Principal。
- API Token 写请求同时要求所属用户仍为 admin 且 Token permissions 为 `readwrite`。
- `/auth/refresh` 使用 `JWTOnly`，API Token 不能换取完整 JWT。

### 安全约束

- 禁止禁用、删除或降级最后一个有效 admin。
- 当前用户不能删除自己，也不能禁用自己或把自己的 role 改为 readonly；服务端返回
  `409 SELF_LOCKOUT`。修改自己的密码仍然允许。前端不显示这些危险自操作。
- readonly 响应中的 Webhook URL、密钥及其他 credential 字段由服务端掩码。
- 前端隐藏或禁用不可用操作只是体验层；服务端始终执行最终授权。

## 前端状态契约

所有 query 遵循以下状态：

| 状态 | 行为 |
| --- | --- |
| 初次 loading | 容器 `aria-busy=true`；骨架 `aria-hidden`；不显示空态 |
| 初次 error | `QueryErrorState`，包含明确消息和 Retry |
| 成功且空 | 仅此时显示 `EmptyState` |
| 有缓存但刷新失败 | 保留旧数据并显示 stale/degraded，不替换成空态 |
| 403 | 显示 permission denied，不清空数据冒充无内容 |
| mutation pending | 控件尺寸稳定、禁用重复提交 |
| mutation error | 弹窗保持打开，显示 inline alert，不关闭、不显示成功 Toast |
| mutation success | 使用服务端结果更新 UI，并通过 polite live region 反馈 |

`NowStrip` 请求失败时显示 unavailable/degraded，不能默认回落到 Healthy。多 query 页面采用
section 级错误状态，次要请求失败不应让整页不可用。

## 共享 UI 组件

页面优先使用项目包装组件，不直接散落 Base UI primitive：

- `Modal`：内部迁移到 Base UI Dialog，保留现有外部 API，补初始焦点、焦点陷阱、
  背景隔离、Escape、关闭后焦点还原和 reduced-motion。
- `Tabs`：Base UI Tabs 包装，具备 `tablist/tab/tabpanel`、方向键、Home/End 和 panel 关联。
- `Input`、`Select`、新增 `Textarea`：`useId()` 关联 label，支持 hint/error、
  `aria-describedby` 和 `aria-invalid`。
- `IconButton`：类型上强制 `label`，提供 Tooltip 和 `aria-label`，命中区固定至少 `40x40`。
- `SectionHeader`：移动端标题、hint、action 堆叠，`sm` 起恢复同一行。
- `DataTable` 与 `TableViewport`：表格使用局部可聚焦横向滚动区，页面本身不横滚；
  行激活必须有真实按钮或链接，不使用只有 mouse click 的 `<tr>`。
- `Toast`、`InlineNotice`、`QueryErrorState`：统一状态颜色、live region 和关闭行为。
- `Switch`：使用 Base UI Switch，提供 label 与 `aria-checked`。

## 响应式规则

| 宽度 | 规则 |
| --- | --- |
| 320/390 | 16px 页面 padding；表单单列；Settings 横向 Tabs；SectionHeader 堆叠；KPI 两列；分析区单列 |
| `sm` 640 | 表单可变两列；普通工具栏可同行，长操作组允许换行 |
| `md` 768 | Settings 恢复 180px 竖向 rail；完整 Tabs；NowStrip 显示扩展指标 |
| `lg` 1024 | 220px 固定主侧栏；KPI 四列 |
| `xl` 1280 | 三栏分析区；不再继续增加信息列数 |
| 1840+ | 主内容封顶 `1840px` 并居中 |

具体迁移要求：

- Settings：移动横向 tabs，`md` 竖向 rail；表单 `grid-cols-1 sm:grid-cols-2`。
- Bandwidth/Security：KPI `grid-cols-2 lg:grid-cols-4`。
- Cache/Bandwidth：分析区 `grid-cols-1 xl:grid-cols-3`。
- License：`grid-cols-1 md:grid-cols-2`。
- Trends 工具栏：两个 Segmented 组可换行，标签不得逐字折行。
- AccessLogs、AuditLogs、Cache、Quarantine 等原生表格全部进入 TableViewport。

## Instrument 视觉规则

- CTA：`--btn` / `--btn-fg`，hover 使用 `--btn-press`。
- 实心信号：`--hit` / `--on-hit`。
- 状态：`--ok-*`、`--warn-*`、`--danger-*` 的 fill/text/border 组合。
- Pro Badge 改为 `--brand-soft`、`--brand-text`、`--brand-border`，取消不具备稳定对比度的实心渐变。
- 亮色主题加深 secondary、subtle、warn 和 danger 文本 token，普通文本达到 `4.5:1`。
- 新增 `--focus-ring`，控件焦点边界达到 `3:1`。
- Rules 删除旧硬编码绿色；Webhook 删除 `.btn`、`--accent`、`--green`、`--red`。
- Treemap 标签不得在半透明浅色块上固定使用白字。
- Admin 侧栏品牌统一为小写 `depsilo`，页面壳层标题与页面内 section 标题不重复。

全局 token 修改必须同时回归 Portal `/` 与 `/monitor`。

## 测试策略

### 后端

- Handler 契约测试先复现字段、参数、状态码和 CSV 导出问题。
- Settings Store：合法 patch、非法值、不落盘、只读文件、并发更新、原子替换、
  未修改注释保留、环境变量覆盖、重启后读取。
- Upstream Registry：创建/更新/删除后下一次真实代理请求切换、最后源保护、worker 退出、
  重启不回填删除项、race 测试。
- 权限矩阵：admin/readonly JWT、readonly/readwrite API Token、disabled/stale-role JWT、
  refresh 提权回归、最后 admin 不变量。

必跑命令：

```bash
go test -race ./internal/config ./internal/upstream ./internal/middleware ./internal/api/...
go test ./...
```

### 前端浏览器测试

新增 `@playwright/test` 与 `@axe-core/playwright` 开发依赖，不新增测试运行时依赖。
API fixture 对未声明的 `/api/v1/**` 请求直接失败，避免 catch-all 空响应掩盖契约错误。

核心测试文件：

- `admin-shell.spec.ts`：drawer inert、焦点、Escape、恢复和桌面侧栏。
- `admin-dialog.spec.ts`：焦点陷阱、焦点还原、遮罩、Escape。
- `admin-tabs-forms.spec.ts`：Tabs 键盘语义、字段 label、Switch 和上传控件。
- `admin-actions.spec.ts`：IconButton 名称、Tooltip、`40x40`、权限隐藏和 mutation 反馈。
- `admin-query-states.spec.ts`：500 -> Retry -> success、stale data、NowStrip unavailable。
- `admin-contracts.spec.ts`：Settings、Upstreams、Security、Logs、Audit、Projects 请求/响应。
- `admin-axe.spec.ts`：13 个路由的 WCAG 2.1 A/AA 自动扫描。

视觉矩阵：

- 13 个 Admin 路由：390x844、1440x1000，light/dark、zh/en。
- Settings、Bandwidth、Cache、Logs、Security、Quarantine：额外验证 320、768、1024。
- Portal `/`、`/monitor`：390、1440，light/dark，验证全局 token 无回归。
- Webhook：loading、empty、单条、多条、disabled、pending、success、error、delete modal。
- 页面 `documentElement.scrollWidth === innerWidth`；只有 TableViewport 可以局部横滚。
- 无未命名按钮、可见图标操作至少 `40x40`、无 console error。

最终前端门槛：

```bash
cd web
npm run type-check
npm run build
npm run test:e2e
cd ..
python3 scripts/i18n-audit.py
```

## 实施顺序

1. 建立 Go/TypeScript DTO 与失败契约测试，修复 Security、Logs、Audit、Projects。
2. 修复认证、readonly、API Token、refresh 和最后 admin 不变量。
3. 建立 `config.Store` 与 Settings configured/effective API。
4. 建立 Upstream Registry、Pool 快照和 worker 生命周期。
5. 建立 Playwright API fixture 和核心失败浏览器测试。
6. 升级共享视觉 token、Dialog、Tabs、Field、IconButton、状态和表格组件。
7. 迁移 Settings、Webhook、MainLayout 和 NowStrip。
8. 迁移固定网格、表格、操作按钮和其余页面状态。
9. 执行全量权限、race、构建、i18n、axe、视觉矩阵与最终代码审查。
10. 同步 `DESIGN.md`、Admin API 文档、配置迁移说明和发布说明。

每个阶段都必须形成独立可测试的增量；后续 UI 不得依赖尚未完成的虚假后端语义。

## 兼容与迁移

- 首次升级将 config Upstreams 导入 DB，并记录 seed 状态；该过程幂等。
- 旧 Security/Audit 查询参数兼容一个发布版本并记录 deprecated 日志。
- Settings API 的旧 PUT body 仍是新白名单 patch 的子集，但响应改为明确的生效结果。
- readonly 用户升级后获得真实读权限；写操作由服务端和前端同时收敛。
- 配置文件只读的部署仍可读取 Settings，但无法从 Admin 修改启动级配置。

## 风险与缓解

- **配置文件并发编辑：** Store 每次更新重新读取文件，只 patch 请求中的键，并使用进程内互斥和原子替换。
- **Pool 并发读写：** Selector 只消费不可变快照；Registry 与 Pool 使用 race 测试验证。
- **DB 已有 Upstream 与 config 冲突：** 仅在 seed 未完成时导入；现有数据库非空时合并缺失记录后立即标记完成。
- **全局 token 影响 Portal：** token 由单一任务修改，并对 Portal 做明暗主题截图回归。
- **40px 按钮增加表格高度：** 接受略高行高，不使用重叠伪元素制造虚假点击区。
- **Base UI 行为迁移：** 保留项目包装层 API，通过真实 Chromium 验证焦点和关闭行为。
- **工作树已有改动：** 实施时逐文件读取并保留现有更改，不执行 reset、checkout 或批量回退。

## 验收标准

1. Settings 成功响应与磁盘内容一致，UI 能区分立即生效和等待重启。
2. Upstream CRUD 成功后下一次真实代理请求使用新 Pool，重启后状态保持。
3. readonly、Token permissions、disabled 用户和 refresh 全部通过权限矩阵测试。
4. 已知 DTO/路由漂移全部有回归测试且前后端字段一致。
5. 初次错误、stale、empty 和 permission denied 在所有 Admin 页面语义正确。
6. 320px 至 1840px 无页面级横向滚动、重叠或逐字折行；表格可局部滚动。
7. Dialog、drawer、Tabs、表单和图标操作可仅用键盘完成。
8. 普通文本、状态、焦点和控件边界达到规定对比度。
9. Go race/全量测试、TypeScript、build、i18n、Playwright 和 axe 通过。
10. Portal 明暗主题和移动/桌面基线无视觉回归。
