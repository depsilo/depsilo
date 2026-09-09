# E01 MCP 只读请求解释

- 新增 `depsilo_request` MCP 工具，并扩展 `depsilo_recent` 支持按
  `request_id` 查询，返回当前数据库保留范围内的
  缓存结果/原因、策略决定/原因、交付结果/原因、状态码、延迟、字节数和
  最多 20 条关联审计事件。
- 查询直接复用 `AccessLog` 与 `AuditLog` 的现有事实源，沿用数据库保留结果，
  限制请求体、批量请求、请求 ID、结果字段大小和审计事件数量；不存在或已
  被保留清理时返回明确的未知状态，并继承 HTTP 请求取消和 5 秒预算。
- 外部包名、策略原因和交付原因标记 `external_strings_untrusted`，不作为
  工具指令执行；包含凭证或其他敏感路径的 URL 描述使用现有 fail-closed
  脱敏。枚举值采用白名单，未知值保持 `unknown`。MCP 仍要求认证读取权限，没有新增
  写工具或自动重试/切源行为。
- `TestMCPRequestFactsAreRecentAndCredentialRedacted` 覆盖凭证 URL 变体、
  长输入、恶意请求 ID 和缺失记录；现有 MCP、鉴权路由测试和 `make check`
  门禁通过。服务重启或日志清理后的请求只返回未知，不重建历史事实。
