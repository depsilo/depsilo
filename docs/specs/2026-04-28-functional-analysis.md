# Depsilo 功能成熟度分析报告

> 日期：2026-04-28
> 角色：产品设计师
> 范围：全量功能盘点 + 差距分析

---

## 一、概述

Depsilo 经过 208 次 commits 的迭代，已经从一个初级的 pip/apt 缓存代理，成长为支持 **13 种生态**、具备**企业级功能**的依赖缓存网关。本文从产品视角，按功能模块逐层分析。

---

## 二、功能大盘点

### 2.1 核心缓存引擎

| 功能 | 状态 | 分析 |
|------|------|------|
| Singleflight 防击穿 | ✅ 已完成 | `golang.org/x/sync/singleflight`，同一 key 并发请求只回源一次 |
| 流式传输（大文件无 OOM） | ✅ 已完成 | `countingReader` 边读边写，不缓冲内存 |
| Stale-While-Revalidate | ✅ 已完成 | 过期数据先返回，后台异步刷新 |
| 离线兜底 | ✅ 已完成 | 上游全挂时返回过期缓存 |
| LRU 淘汰 | ✅ 已完成 | 超阈值自动清理 |
| 双 TTL 策略 | ✅ 已完成 | `ttl_index` 元数据短 TTL, `ttl_blob` 包文件长 TTL |

**评价：** 缓存引擎非常成熟，结构清晰，边界 case 处理完善。

### 2.2 生态适配器（13 种）

| 生态 | 类型 | 状态 | 备注 |
|------|------|------|------|
| PyPI | URL 重写 | ✅ | HTML 中下载 URL 重写 |
| APT | Passthrough | ✅ | 保护 GPG 签名链 |
| npm | URL 重写 | ✅ | JSON 中 `dist.tarball` 重写 |
| Go Modules | Passthrough | ✅ | GOPROXY 协议全兼容 |
| Cargo | config.json 重写 | ✅ | `dl` 字段重写 |
| Maven | Passthrough | ✅ | SNAPSHOT 短 TTL |
| RubyGems | Passthrough | ✅ | Compact index 支持 |
| Composer | metadata-url 重写 | ✅ | Packagist V2 协议 |
| NuGet | service index 重写 | ✅ | V3 协议全兼容 |
| Conda | Passthrough | ✅ | repodata 短 TTL |
| CRAN | Passthrough | ✅ | PACKAGES 文件短 TTL |
| Helm | Passthrough | ✅ | index.yaml 短 TTL |
| Docker Registry | Token 认证 | ✅ | `docker pull` 手动验证通过 |

**评价：** 13 种生态全部实现，覆盖了所有主流包管理器。唯一缺口是 Docker Registry E2E 测试未集成到 `make test-e2e`。

### 2.3 存储层

| 功能 | 状态 | 分析 |
|------|------|------|
| 本地文件系统 | ✅ | 默认存储 |
| S3 兼容存储 | ✅ | 支持 MinIO、AWS S3 |
| 存储可视化 | ✅ | 前端 Dashboard 显示存储分布 |

**评价：** 存储层接口清晰，扩展方便，但缺少**按生态配额**的能力。

### 2.4 上游管理

| 功能 | 状态 | 分析 |
|------|------|------|
| 多上游源 | ✅ | 每个生态独立配置 |
| 优先级选择 | ✅ | `PrioritySelector` |
| 延迟优选 | ✅ | `LatencySelector` 并发探测 |
| 健康检查 | ✅ | 后台定时 HEAD 请求 |
| 熔断器 | ✅ | `gobreaker` |
| 限流 | ✅ | 令牌桶，每上游独立 |
| 独立代理 | ✅ | 每上游可配 HTTP 代理 |
| 延迟历史 | ✅ | 趋势图表展示 |

**评价：** 上游管理极其完善，甚至超出大多数竞品。

### 2.5 管理后台 API

| 端点 | 状态 | 分析 |
|------|------|------|
| Dashboard | ✅ | 今日请求数、缓存命中率、传输量 |
| Dashboard 趋势 | ✅ | 7d/30d 趋势图数据 |
| 缓存管理 CRUD | ✅ | 列表、删除、清理 |
| 缓存分布 | ✅ | 各生态存储占比 |
| 缓存预热 | ✅ | POST 手动触发 |
| 上游管理 CRUD | ✅ | 包含连通性检查 |
| 上游延迟历史 | ✅ | 时间序列数据 |
| 访问日志 | ✅ | 分页+生态筛选 |
| 用户管理 CRUD | ✅ | 含角色、禁用 |
| API Token 管理 | ✅ | Hash 存储 |
| 系统设置 | ✅ | 读取+写入 |

**评价：** 基础管理面完善。注意缓存预热和后端 `Deps` 结构有 12 个独立 Pool 字段的技术债。

### 2.6 Pro 功能（需 License）

| 功能 | 状态 | 分析 |
|------|------|------|
| 审计日志 | ✅ | 所有管理操作记录、可导出 CSV |
| 规则引擎（Allow/Deny） | ✅ | 包级别准入控制，in-memory 缓存 |
| 安全扫描（OSV） | ✅ | 自动拉取 OSV 漏洞库、触发扫描 |
| 安全策略 | ✅ | 按生态配置自动更新策略 |
| 安全建议 | ✅ | 可审批/忽略的修复建议 |
| 项目管理 | ✅ | 多项目、项目级 Token、SBOM 导出 |
| SBOM 生成 | ✅ | SPDX 2.3 + CycloneDX 1.5（双格式 JSON，按 `?format=` 切换） |

**评价：** Pro 功能完整性出乎意料，OSV 集成、SBOM、项目管理都实现了。但有几点需要注意：
- OSV 数据导入和展示的**端到端流程**未验证（CLAUDE.md 提到"需跑通展示"）
- Rules 前端页面（Rules.tsx）仅 118 行，功能完整度可能有缺口
- 安全页面 704 行前端，是否所有 API 都有对应 UI 操作？

### 2.7 前端 Portal（用户门户）

| 页面 | 状态 | 分析 |
|------|------|------|
| QuickStart（快速开始） | ✅ | 682 行，包含 13 种生态的配置命令、环境变量模板、Dockerfile 示例 |
| Monitor（监控/实时流） | ✅ | 220 行，服务状态、缓存命中统计、实时 SSE 事件流、上游健康面板 |
| 设置向导（SetupWizard） | ✅ | 430 行，首次部署引导页 |

**评价：** Portal 简洁实用。Monitor 页面的 Top Packages 目前只展示 pypi 和 apt 两种，其他 11 种生态缺失——这是一个功能缺口。

### 2.8 前端 Admin（管理后台）

| 页面 | 行数 | 状态 | 分析 |
|------|------|------|------|
| Dashboard | 291 | ✅ | 卡片式布局，MetricCard 展示，生态统计 |
| Bandwidth Report | 319 | ✅ | 7d/30d/90d 时间范围，面积图+柱图+饼图，流量节省时间 |
| CacheManage | 355 | ✅ | 缓存列表+搜索+删除+分布图（饼图） |
| Upstreams | 239 | ✅ | 上游 CRUD + 健康状态 |
| AccessLogs | 212 | ✅ | 日志列表+生态筛选 |
| AuditLogs | 264 | ✅ | Pro 功能，可导出 CSV |
| Rules | 118 | ✅ | 包规则 CRUD，但行数偏少，功能可能受限 |
| Security | 704 | ✅ | 安全仪表盘，漏洞列表，策略配置，内容丰富 |
| Projects | 457 | ✅ | 项目管理，项目级 Token，SBOM 导出 |
| Users | 108 | ✅ | 用户管理 CRUD |
| Settings | 239 | ✅ | 系统设置读写 |
| Login | 98 | ✅ | JWT 登录 |
| MainLayout | 173 | ✅ | 侧边栏+顶栏布局 |

**评价：** 管理后台功能完整度高。Dashboard、Bandwidth Report、Security、Projects 页面内容丰富。

### 2.9 前端基础组件

| 组件 | 状态 | 分析 |
|------|------|------|
| 共享组件库 | ✅ | Button(compatible), Card, Badge, Icon, Logo, Modal, Tabs, Select, Input, DataTable, EmptyState, EcosystemIcon, LangToggle, ThemeToggle, MetricCard, UpstreamCard |
| i18n（中英文） | ✅ | 两个语言文件，478 keys |
| 暗色/亮色主题 | ✅ | CSS 变量体系 |
| Stripe 设计系统 | ✅ | DESIGN.md 完整定义，前端已应用 |
| shadcn/ui + Tailwind | ✅ | 现代 UI 框架 |

**评价：** 前端工程化成熟。但 i18n key 缺少编译时校验（已知）。

### 2.10 公共 API

| 端点 | 状态 | 分析 |
|------|------|------|
| GET /health | ✅ | 健康检查 |
| GET /metrics | ✅ | Prometheus 指标 |
| GET /api/v1/stats | ✅ | 公开统计 |
| GET /api/v1/packages | ✅ | 包列表搜索 |
| GET /api/v1/packages/:type/:name | ✅ | 包详情 |
| GET /api/v1/events/stream | ✅ | SSE 实时事件流 |
| GET /api/v1/setup/status | ✅ | 设置状态 |
| POST /api/v1/setup/complete | ✅ | 完成设置 |
| POST /api/v1/auth/login|logout|refresh | ✅ | 认证 |

**评价：** 公共 API 完整，特别是 SSE 实时流是一个差异化亮点。

### 2.11 测试覆盖

| 类型 | 文件数 | 行数 | 分析 |
|------|--------|------|------|
| 单元测试 | 7 | 1,323 | 缓存key、流式传输、counting reader、Docker resolver、URL 重写、规则引擎 |
| 集成测试 | 13 生态 | 1,082 | 每生态 mock upstream + 真实请求验证 |
| Mock 服务器 | 1 | 288 | 可注册所有生态的 mock 上游 |
| 总测试 | 22 | 2,452 | |

**评价：** 测试覆盖属于**中等偏上**，缓存核心和 URL 重写有充分单元测试，13 生态都有集成测试。但缺少端到端测试跑通安全扫描流程。

### 2.12 基础设施

| 项 | 状态 | 分析 |
|----|------|------|
| Go 1.25.6 | ✅ | 编译通过 |
| Makefile | ✅ | build/run/test/frontend/docker 等完整 |
| Dockerfile | ✅ | 多阶段构建 |
| docker-compose | ✅ | 单服务 |
| config.example.toml | ✅ | 204 行完整配置参考 |
| CLAUDE.md | ✅ | 1,159 行超级指南 |
| README.md | ✅ | 中英文双语 |
| 品牌资源 | ✅ | SVG Logo 多种格式 |
| GitHub Release 文档 | 有 spec | 待实际跑通 |

**评价：** 基础设施完善。Dockerfile 正确，Makefile 目标完整。

---

## 三、差距分析（Gap Analysis）

### 3.1 产品功能缺口

| # | 功能 | 优先级 | 说明 |
|---|------|--------|------|
| G1 | **CLI 工具** | P1 | `depsilo status` / `depsilo cache warmup` 等 CLI 命令，当前靠 HTTP API |
| G2 | **高可用部署文档** | P1 | PostgreSQL + S3 + 多实例的生产部署指南不存在 |
| G3 | **Webhook 通知** | P2 | 缓存未命中、上游异常 → Slack/DingTalk 等渠道推送 |
| G4 | **Docker Registry E2E 测试** | P2 | 手动验证通过，但不在自动化测试套件中 |
| G5 | **集群模式** | P3 | 多节点共享同一缓存后端 |
| G6 | **LDAP/SSO 集成** | P3 | 企业统一登录 |
| G7 | **Monitor Top Packages 全生态** | P2 | 目前只展示 pypi 和 apt |

### 3.2 技术债

| # | 问题 | 严重度 | 说明 |
|---|------|--------|------|
| T1 | `api.Deps` 12 独立 Pool 字段 | 低 | 可用 `map[string]*Pool` 统一，需联动改 stats 和 dashboard |
| T2 | i18n key 无编译时校验 | 低 | 两语言文件手动同步，缺少自动检查 |
| T3 | 部分 `formatTime` 变体未统一 | 低 | 多页面有不同实现 |
| T4 | Docker token 缓存非持久化 | 低 | 重启丢失，首次请求延迟增加 |

### 3.3 可优化的产品体验

| # | 项 | 说明 |
|---|------|------|
| U1 | **Dashboard 缺少快速操作** | 当前只有数据展示，没有"一键清理缓存"、"重启健康检查"等操作入口 |
| U2 | **上游选择器默认不可配置** | PrioritySelector 和 LatencySelector 选择未暴露到配置或 UI |
| U3 | **缓存预热无进度反馈** | POST warmup 是个异步操作，但前端没有进度条或完成通知 |
| U4 | **没有 "Getting Started" 交互式引导** | SetupWizard 只处理首次部署，没有引导用户完成第一个 `pip install` |
| U5 | **包搜索功能粗糙** | 只支持按名字模糊搜索，不支持版本、生态、大小范围等筛选 |

---

## 四、竞品对标

### 功能覆盖矩阵

| 功能 | Nexus | Artifactory | Verdaccio | Depsilo |
|------|-------|-------------|-----------|---------|
| 多生态代理 | ✅ 10+ | ✅ 10+ | ❌ 仅 npm | ✅ **13** |
| 单二进制部署 | ❌ JVM | ❌ JVM | ✅ Node.js | ✅ **Go 静态** |
| 缓存引擎 | ✅ | ✅ | ✅ LRU | ✅ **SWR+Singleflight+LRU** |
| 多上游 | ✅ | ✅ | ✅ | ✅ **含独立代理** |
| 健康检查+熔断 | ✅ | ✅ | ❌ | ✅ |
| Web UI | ✅ 复杂 | ✅ 复杂 | ✅ 简洁 | ✅ **Stripe 风格** |
| Docker Registry | ❌ | ✅ | ❌ | ✅ |
| 安全扫描 | ✅ Pro | ✅ Pro | ❌ | ✅ **Pro（OSV）** |
| 审计日志 | ✅ Pro | ✅ Pro | ❌ | ✅ **Pro** |
| 项目管理 | ✅ Pro | ✅ Pro | ❌ | ✅ **Pro** |
| SBOM 导出 | ❌ | ❌ | ❌ | ✅ **Pro** |
| 规则引擎 | ✅ Pro | ✅ Pro | ❌ | ✅ **Pro** |
| CLI 工具 | ✅ | ✅ | ❌ | ❌ |
| 高可用/集群 | ✅ | ✅ | ❌ | ❌ |
| LDAP/SSO | ✅ | ✅ | ❌ | ❌ |
| Webhook | ✅ | ✅ | ❌ | ❌ |
| 内存占用 | 2GB+ | 2GB+ | ~50MB | **~50MB** |
| 部署时间 | 30min+ | 30min+ | 5min | **1min** |

### 结论

Depsilo 在**单实例功能密度**上已超越大多数竞品，但在**企业级集成**（LDAP、集群、Webhook）和**工具链完整性**（CLI）上还有差距。

核心差异化优势：**单二进制 + 13 生态 + Stripe 级 Web UI + 极低资源占用**

---

## 五、建议行动

### 立刻可做（低投入、高回报）

1. **补全 Monitor 页面的 Top Packages** — 按生态展示 Top N 包，SQL 查询已存在
2. **把 Docker E2E 测试集成到 CI** — 已有 spec，缺实现
3. **修复 i18n key 校验** — 写一个简单的 Python 脚本检查两语言 key 集合是否一致
4. **Dashboard 加快速操作按钮** — 清理缓存、重新检查上游

### 短期（P1 需求）

5. **CLI 工具设计** — 先出海报，再写设计文档
6. **高可用部署文档** — 面向 PostgreSQL/S3 的生产部署
7. **Webhook 通知设计** — 缓存事件 + 上游异常推送

### 中期（P2 需求）

8. **集群模式初步设计** — 共享缓存后端 + 分布式锁
9. **安全扫描端到端验收** — 确认 OSV 集成可用

### 战略取舍

**不推荐做的：**
- Wails 桌面版（投入产出比低）
- 完整仓库管理系统（偏离"轻量缓存代理"定位）

**可以考虑的：**
- 上线 depsilo.com 产品官网 + 文档站
- 录制部署演示动画（30 秒内）
- 提交 Awesome 列表、HN、r/selfhosted

---

## 六、总结

Depsilo 的**当前功能成熟度约 85%**。核心缓存引擎、生态覆盖、Web UI、Pro 功能都已实现且质量较高。剩余工作集中在：

1. **工具链完整性**（CLI、部署文档）
2. **企业级集成**（LDAP、Webhook、集群）
3. **体验雕琢**（Top Packages 补全、快速操作、搜索增强）

对比竞品，Depsilo 已经是**生态覆盖最广、部署最简单、资源占用最低**的轻量缓存代理网关。接下来的重心应该是 **"让更多用户知道并能用起来"**，而非继续堆功能。

---

*文档版本：v1.0*
