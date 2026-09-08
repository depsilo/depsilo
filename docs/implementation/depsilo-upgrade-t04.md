# 改造执行记录：T04 请求诊断后端

候选基线：`605eea0`  
记录日期：2026-09-08

状态：**IMPLEMENTED（最小纵向切片）**。

本任务为请求建立有界关联 ID，并在真实访问记录中保存可证明的诊断维度：

- 每次请求生成服务端 UUID，并通过 `X-Request-ID` 回传；不信任或保存客户端同名头，防止重复 ID 关联到其他请求或写入任意敏感值。
- `AccessLog` 增加请求 ID、缓存结果/原因、策略结果/原因、交付结果/原因；旧记录保持空值，读取时代表未记录。
- `LogAccess` 只根据实际的 `hit` 与 HTTP 状态写入缓存事实；策略与完整交付在当前调用链没有可靠事件粒度时明确写为 `unknown`，不把 200 推断为客户端完整收到。
- `LogPolicyBlock` 将请求 ID写入已有 `AuditLog`，不制造伪造的缓存 MISS。
- 新增只读 Admin API：`GET /api/v1/admin/logs/:id`。它返回单条访问记录及同一请求 ID 关联的有限审计事件；不存在或非法 ID 返回统一 404，数据库错误不向客户端泄露内部细节。
- 新增 schema v4 迁移。只增加可选列和索引，不复制凭证、请求正文、完整制品或原始请求头。

实际覆盖：进入 `LogAccess` 的共享适配器访问路径，以及规则阻断的审计路径。未覆盖的策略细节、流结束/客户端取消和未进入适配器的早期失败仍返回 `unknown`/`not_recorded`，留给后续在真实事件点接入；没有从当前配置反推历史事实。

验证（PASS）：

```text
go test ./internal/requestid ./internal/middleware ./internal/accesslog ./internal/adapter ./internal/api/admin ./internal/db ./internal/api -count=1
go test ./internal/db -run TestCurrentSchemaMatchesDomainModels -count=1
npm --prefix web run type-check
git diff --check
```

建议 commit 标题：`feat(diagnostics): record correlated request facts`。
