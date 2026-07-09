# 恶意包 Blocklist（DIRECTION Task 2）— 设计文档

日期：2026-07-09
状态：与产品负责人对齐（默认开启+降级运行；UI 为隔离页第三个 Tab）

## 目标

硬封锁**已知恶意**（区别于"有漏洞"）的包版本：命中即拒绝服务（HTTP 451
`MALICIOUS_BLOCKED`），绝不放行；提供显式、带审计、**24h 自动过期**的管理员
误报豁免。与 CVE 处理完全分离（CVE=警告可服务；malware=硬封锁）。

## 数据源与同步

- 源：OSV.dev 官方按生态批量数据 `{mirror_url}/{OSV生态名}/all.zip`
  （默认 `https://osv-vulnerabilities.storage.googleapis.com`），导入时仅取
  ID 前缀 `MAL-` 的条目（ossf/malicious-packages 数据集；GHSA malware 通过
  alias 已被其覆盖）。
- 生态映射：npm→npm、pypi→PyPI、cargo→crates.io、rubygems→RubyGems、
  composer→Packagist、nuget→NuGet、go→Go、maven→Maven（其余生态暂无该数据
  集覆盖，不同步）。
- 周期：默认 6h（`sync_interval` 可配）；启动即跑一次。逐生态事务内全量替换。
- **降级姿态（已拍板）**：默认开启；同步失败仅 `zap` 告警 + 状态表记录，
  继续用上次成功的数据拦截；本地无数据则不拦截（不阻断代理主功能）。
- 网络：`[supply_chain.blocklist] proxy` 可配（国内环境必需）；`mirror_url`
  可指向自建镜像。

## 数据模型（internal/db/blocklist.go）

- `MaliciousPackage`：Ecosystem+PackageName（联合索引）、Versions（JSON 数组，
  空=全版本恶意）、SourceID（MAL-*）、Aliases、Summary、Modified、ImportedAt。
  唯一键 (SourceID, Ecosystem, PackageName)。
- `MalwareOverride`：Ecosystem、PackageName、Version（空=该包全部版本）、
  Reason（必填 ≥3 字符）、ActorID、CreatedAt、**ExpiresAt（创建 +24h，不可续
  期只可重建，保证豁免是短时应急而非常态）**。
- `BlocklistSyncState`（单行）：LastSyncAt、LastSuccessAt、LastError、
  EntryCount、DurationMs。

## 匹配与执行

- 新包 `internal/blocklist/`：Syncer（下载/过滤/入库/调度）+ Store
  （`IsMalicious(ctx, eco, pkg, version)`）。包名按生态规范化（npm 小写、
  pypi PEP503），版本匹配 = 精确命中 Versions 列表，或 Versions 为空的全版本
  兜底；不做 semver 区间求值（恶意包数据集几乎全部 introduced=0 全版本）。
- **执行点：`quarantine.Checker.Check` 第 0 步**，先于阈值/allowlist/审批——
  quarantine 的 allowlist **不能**绕过恶意封锁；唯一豁免是未过期的
  MalwareOverride。
- Decision 增加 `Code` 字段（`QUARANTINED` | `MALICIOUS_BLOCKED`），
  adapter Gate 按 Code 写响应体；对客户端仍是 451。
- 事件：复用 `QuarantineEvent`，新增 action `malware_blocked` /
  `malware_bypassed`（override 命中）/ `override_created` / `override_revoked`。
- Webhook：新事件类型 `EventMalwareBlocked`，severity=critical，复用
  notify 引擎与 Checker.OnBlock 通道。

## 配置（[supply_chain.blocklist]）

```toml
[supply_chain.blocklist]
enabled       = true        # 默认开启（wedge 哲学：空配置也受保护）
sync_interval = "6h"
mirror_url    = "https://osv-vulnerabilities.storage.googleapis.com"
proxy         = ""          # 国内环境配置 HTTP 代理
```

## Admin API（开源，不设 Pro 门）

- `GET  /api/v1/admin/blocklist/status` — 同步状态 + 条目数（按生态分组）
- `POST /api/v1/admin/blocklist/sync` — 手动触发（异步，立即返回）
- `GET  /api/v1/admin/blocklist/overrides` / `POST .../overrides`（reason 必填）
  / `DELETE .../overrides/:id` — 创建/撤销均写审计事件
- 拦截事件走现有 `GET /admin/quarantine/events`（action 过滤扩展）

## Admin UI（Quarantine 页第三个 Tab "恶意封锁"）

- 状态卡：最近同步时间/结果、条目总数、下次同步、手动同步按钮
- Override 列表（含剩余有效期倒计时）+ 新建 Dialog（生态/包/版本/原因）+ 撤销
- Events Tab 的 action 筛选器与 badge 增加 `malware_blocked`（danger 色）
- i18n zh/en 同步

## 测试与验收（对应 DIRECTION Acceptance）

1. 单测：OSV zip 解析过滤（fixture 含 MAL-* 与非 MAL 条目）、Store 匹配
   （版本列表/全版本/规范化/override 过期）、Checker 顺序（malicious 优先于
   allowlist，override 放行）。
2. 集成：mock OSV zip 服务器 → 同步 → 请求已知恶意版本 → 451
   `MALICIOUS_BLOCKED`（端到端）；集成测试配置显式指向 mock、interval 拉长。
3. 手动验收：真实同步（走代理）+ Monitor 事件流可见 + Webhook 送达。

## 范围外

- semver 区间求值、按 CVSS 分级、SBOM 联动 —— 后续任务
- Docker/HuggingFace/Helm 等无数据集覆盖的生态
- 门户端展示（属于被推迟的护栏带范围）
