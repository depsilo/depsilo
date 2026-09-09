# E01 MCP 只读请求解释

- 新增 `depsilo_request` MCP 工具，输入最近 7 天内的 `request_id`，返回
  缓存结果/原因、策略决定/原因、交付结果/原因、状态码、延迟、字节数和
  最多 20 条关联审计事件。
- 查询直接复用 `AccessLog` 与 `AuditLog` 的现有事实源，限制请求 ID 长度、
  结果字段大小和审计事件数量；不存在或超出保留期时返回明确的未知状态。
- 外部包名、策略原因和交付原因标记 `external_strings_untrusted`，不作为
  工具指令执行；凭证形式 URL 会被脱敏。MCP 仍要求认证读取权限，没有新增
  写工具或自动重试/切源行为。
- `TestMCPRequestFactsAreRecentAndCredentialRedacted` 覆盖凭证诱饵和过期
  查询；现有 MCP、前端和完整 `make check` 门禁通过。

