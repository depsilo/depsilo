# Depsilo 改造基线（T00）

核查日期：2026-09-08  
路线图：`/home/SENSETIME/ningxiangdong1/Downloads/docs/depsilo-codex-roadmap.md`

## 基线与边界

- 主仓库分支：`master`
- 主仓库 HEAD：`9b1714254ad5e06aeb5cfdd7ea7b60873b6cc4d8`（`chore: prepare v0.9.2 release`）
- 工作区：干净，无已有修改可归入本轮。
- 本地 `v0.9.2^{commit}` 与 HEAD 相同；远端 peeled tag 也指向该 SHA。GitHub Latest Release 在核查时为 `v0.9.2`（2026-09-02），见 <https://github.com/depsilo/depsilo/releases/tag/v0.9.2>。
- 官网源码不在主仓库，但同级存在独立仓库 `/data/codelab/depsilo_workspace/depsilo-landingpage`，HEAD 为 `f46521c4acc8f7312fa95d6e22da36a5c4fafe38`，工作区干净。官网目前 `src/data/content.ts` 默认版本仍为 `0.9.0`，因此 T02 必须在该仓库单独修改、构建和发布；主仓库不能代替线上部署。
- 执行环境：Go 1.26.7、Node 24.18.0、npm 11.16.0、Docker 可用。

## 当前实现核查

| 主题 | 状态 | 证据与验证入口 |
| --- | --- | --- |
| 首次接入与真实 HIT 验证 | 已有 | `web/src/admin/pages/ConnectProject.tsx`、`internal/api/onboarding.go`；`internal/api/onboarding_test.go`；`web/e2e/admin-onboarding.spec.ts`。流程按新的游标只消费新请求，并要求真实请求后再确认 HIT。 |
| Access Logs 与预热反馈 | 部分 | `internal/api/admin/logs.go` 提供分页/导出；`web/src/admin/pages/AccessLogs.tsx` 将普通 `MISS` 渲染成 `error` Badge。`internal/api/admin/warmup.go` 已异步提交并返回 202，但只返回数量；`CacheManage.tsx` 使用字符串结果、绿色成功容器，并在异常时显示固定英文 `Failed`。对应 T03 缺口已复现于当前代码。 |
| 请求日志、审计与规则事实 | 部分 | `db.AccessLog` 记录方法、缓存键、命中、上游、延迟、状态码、字节和客户端 IP；`db.AuditLog` 记录策略动作、版本、上游 URL 等。现有记录没有统一的请求阶段/策略快照/交付事实详情 API；这是 T04 的新增范围。规则测试入口是 `POST /api/v1/admin/rules/test`，属于当前只读评估，不是历史请求回放。 |
| 生态与能力目录 | 已有 | `internal/ecosystem/catalog.go` 定义 14 个标准路径生态；Docker 是独立 OCI 路由，Hugging Face 也有独立能力差异测试。`internal/ecosystem/catalog_test.go` 覆盖稳定顺序和能力。 |
| 安全能力与数据状态 | 部分 | 已有 `internal/quarantine/`、`internal/blocklist/`、`internal/security/` 及 Admin Security API。最小发布时间在 `internal/quarantine/policy.go` 启动时拒绝正阈值，OSV 自动规则在 `internal/api/admin/security.go` 明确 safety-disabled；支持范围按 `PRODUCT.md`、Release v0.9.2 和代码分别约束。缺少统一可复用能力摘要，属于 T06/T07。 |
| Dashboard 指标与跳转 | 已有/部分 | `internal/api/admin/dashboard.go` 提供滚动 24h、趋势、上游和 Top packages；`web/src/admin/pages/Dashboard.tsx`、`DashboardAttention.tsx` 已有异常跳转。策略/安全能力状态和请求详情尚未完整接入，T08 仍有增量工作。 |
| 预热 | 部分 | `internal/api/admin/warmup.go` 限制 PyPI/npm、100 项、单任务运行和超时，复用 `cache.Manager.Prefetch`；现有测试覆盖实际缓存写入、npm 重写、输入限制、并发拒绝。没有可查询 job ID、逐项结果、取消/恢复，T15/T16 仍缺失。 |
| 清理 | 已有 | `internal/cache/retention.go` 是唯一删除 owner，具备 mutation gate、本地/S3 路径和部分失败报告；`internal/api/admin/cache.go` 暴露旧的 `POST /cache/cleanup`。没有候选预览或绑定执行计划，T13/T14 尚未开始。 |
| doctor | 已有 | `internal/cli/doctor.go` 只读检查可达性、健康、版本、存储、上游和命中率；`internal/cli/doctor_test.go` 覆盖 JSON 失败语义。T09 可在此基础上补升级预检与演练材料。 |
| backup/restore | 已有 | `internal/cli/backup.go` 与 `internal/backup/` 提供 SQLite 一致快照、校验恢复、停止服务约束和旧状态保留；明确备份不含缓存对象。相关恢复/安全测试在 `internal/backup/*_test.go`。 |
| 升级、真实客户端、S3、生产 UI 门禁 | 已有 | `Makefile` 提供 `test-docker-*`、`test-docker-docker`、`test-s3`、`test-v090-upgrade`、`test-v090-compose-upgrade`、`test-v091-upgrade`、`test-ui-production`；`RELEASE_CHECKLIST.md` 与 `docs/development/testing.md` 已索引这些门禁。当前基线未重新运行网络/Docker/浏览器门禁。 |
| MCP / Agent | 已有/部分 | `internal/api/public/mcp.go` 提供状态、doctor、配置、搜索、近期访问和 warmup 请求模板；warmup 工具明确“不执行/不排队”。T04/T05/T10 后才评估 E01，不能把模板当作任务完成。 |
| 官网与默认文档 | 部分 | 官网独立仓库可修改，当前默认宣传仍引用 v0.9.0；主仓库 `README.md` 明确当前 master 与 tagged release 的区别，并已写明单实例、14+OCI、最小发布时间不可用等边界。官网版本断层属于 T02。 |
| 产品与 ADR 冲突 | 已记录 | `PRODUCT.md` 将通用制品仓库写成确认的未来方向；`docs/adr/0004-supply-chain-enforcement-layer.md` 仍包含较早的非目标表述。T01 只能新增 proposed ADR 或记录待决问题，不能静默改写已接受 ADR。 |

## T01–T16 / E01–E03 映射

| 任务 | 当前判断 | Owning module / 最小验证 |
| --- | --- | --- |
| T01 | 部分 | `PRODUCT.md`、`README.md`、`docs/README*.md`、配置文档；文档审阅与链接检查。 |
| T02 | 部分，外部仓库可用 | `depsilo-landingpage/src/`；官网独立运行 `npm run check`、`npm run build`。 |
| T03 | 缺失 | `web/src/admin/pages/AccessLogs.tsx`、`CacheManage.tsx`、i18n；Vitest/Playwright + `make lint-i18n`。 |
| T04 | 缺失 | `internal/accesslog`、`internal/db`、`internal/api/admin`；接口测试与必要的集成测试。 |
| T05 | 缺失 | `web/src/admin/pages/AccessLogs.tsx`、Admin API types；访问日志浏览器测试。 |
| T06 | 缺失 | `internal/ecosystem`、`internal/config`、`internal/api/admin`、security/quarantine owners；Go 合约测试。 |
| T07 | 缺失 | `web/src/admin/pages/Security.tsx`、`Rules.tsx`、i18n；Admin 浏览器/无权限状态。 |
| T08 | 部分 | Dashboard API 与 `web/src/admin/pages/Dashboard.tsx`；Dashboard 浏览器测试。 |
| T09 | 基础已有 | `internal/cli/doctor.go`、`internal/backup/`、`scripts/test-v090*`；升级与恢复脚本门禁。 |
| T10 | 缺失 | CLI 诊断 owner 与现有 doctor/backup 只读检查；脱敏诱饵测试。 |
| T11 | 缺失 | `testground/`、脚本与结果材料；无真实数字时保持 NOT_RUN。 |
| T12 | 基础已有 | `RELEASE_CHECKLIST.md`、现有 Make targets；按候选 commit 归档 PASS/FAIL/NOT_RUN/WAIVED。 |
| T13 | 缺失 | `internal/cache/retention.go`、Admin cache API/UI；只读候选一致性测试。 |
| T14 | 缺失 | retention 删除 owner、计划存储与 Admin API/UI；并发、过期、权限和部分失败测试。 |
| T15 | 部分 | `internal/api/admin/warmup.go`、`internal/asyncruntime`、cache；job 生命周期和重启测试。 |
| T16 | 缺失 | `CacheManage.tsx`、Admin API types/i18n；刷新恢复与终态轮询测试。 |
| E01 | 未选入，待 T04/T05/T10 | `internal/api/public/mcp.go`；只读权限、脱敏和请求 ID 查询测试。 |
| E02 | 未选入，待真实试用 | `testground/` 或客户端示例；单一锁文件格式的真实客户端验证。 |
| E03 | 未选入，待 T13/T14 与容量需求 | retention owner、迁移、Admin UI；配额与清理并发测试。 |

依赖调整：官网修改必须在独立仓库完成；T06 可先于 T04 设计但涉及共享 API 类型时必须串行；T09 的最终复验必须绑定本轮实际迁移后的候选 commit；T13–T16 与 E01–E03 不作为 1.0 无条件阻塞项。

## T00 验证记录

PASS：

```text
git status --short
git rev-parse HEAD
git tag --sort=-creatordate | head -10
git ls-remote --tags origin 'refs/tags/v0.9.2*'
go test ./internal/ecosystem ./internal/api/admin ./internal/api -run 'Test(Catalog|Warmup|AccessLog|Dashboard|Onboarding)' -count=1
```

最后一条命令通过 `internal/ecosystem`、`internal/api/admin`、`internal/api`。这是基线接口回归，不代表路线图任务已完成。

NOT_RUN：`make check`、`make verify`、真实包管理器、Docker Registry、S3、完整 Playwright、升级容器演练和官网构建；这些不属于 T00 的只读核查，且部分需要网络/浏览器/独立仓库操作。

## T00 结论

状态：**IMPLEMENTED（仅基线报告）**。本任务没有业务代码、数据库、运行时配置、官网或外部服务改动。首批建议按路线图执行 T01（文档边界）和 T03（状态语义 UI）；两者改动面相对独立。T02 应在官网仓库单独建立候选 diff 后再核对稳定版版本数据。T04/T06 涉及共享事实与类型，需在 T03/T01 完成并复核后串行推进。

建议 commit 标题：`docs: record Depsilo upgrade baseline`
