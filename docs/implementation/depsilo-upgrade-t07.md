# T07 实现记录：安全能力状态与支持矩阵 UI

## 已实现

- Security 总览接入 `GET /api/v1/admin/capabilities/summary`，按能力折叠、按生态展开。
- 支持范围、当前模式、数据状态、最后成功时间和最近失败分开显示；`alert_only`、`safety_disabled`、`never_synced`、`stale` 等状态使用明确文字。
- 接口不可用、空数据和旧服务缺少 `capabilities` 字段时显示降级说明，不把空数据渲染为“0 风险”。
- 使用原生 `details/summary`，移动端逐项卡片可读，键盘可展开；没有引入新的写操作或伪造开关。
- 中英文文案同步，Playwright Admin API 默认夹具覆盖新增只读请求。

## 验证

- `npm --prefix web run type-check`
- `npm --prefix web run lint`
- `make lint-i18n`
- `npm --prefix web run test:unit`
- `npx playwright test e2e/admin-security-workspace.spec.ts`（11/11）

服务 `/ready` 与安全数据状态仍是独立事实；本任务没有改动策略执行和权限写入路径。
