# T06 实现记录：运行时能力摘要

## 已实现

- 新增认证 Admin 只读接口 `GET /api/v1/admin/capabilities/summary`。
- 复用 `ecosystem` catalog、`rules.PolicyStatusProvider`、`blocklist.Store.SyncState`、`VulnerabilityCheck`、`quarantine.SupportsMinimumReleaseAge` 和 `TamperConfig`，没有增加独立 feature-flag 注册表。
- 每项能力按生态返回 `support`、`mode`、`data_status`，并在已有来源提供时返回 `last_success_at` 与 `recent_failure`。
- 返回构建版本、commit、构建时间；同时复用配置 Store 的 configured/effective/sources/pending_restart 快照，读取失败不会阻塞摘要。
- 最小发布时间当前明确为 `safety_disabled`；漏洞扫描自动阻断仍以 `alert_only` 表示；恶意包同步失败且存在最后成功数据时保持 `stale`，不覆盖最后成功时间。
- 摘要接口只读、有界，不主动探测上游、同步数据、刷新策略或评估任意包。

## 验证

- `go test ./internal/api/admin ./internal/api -count=1`
- `go test ./internal/blocklist ./internal/security ./internal/rules ./internal/quarantine -count=1`
- `git diff --check`

完整 `make check` / `make verify` 仍由交付前门禁负责；本任务未修改策略执行、故障降级或数据库 schema。
