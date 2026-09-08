# 改造执行记录：T01 与 T03

候选基线：`9b1714254ad5e06aeb5cfdd7ea7b60873b6cc4d8`  
记录日期：2026-09-08

## T01：产品边界文档

状态：**IMPLEMENTED**。

在 `README.md` 和 `docs/README_zh.md` 补充：通用制品仓库是未来方向，目前未交付；托管发布、仓库管理、保留策略及兼容/迁移规则仍待单独定义。没有修改运行时、历史 `docs/DIRECTION.md` 或已接受 ADR。该补充与 `PRODUCT.md` 的当前意图一致，也保留了当前单实例代理/缓存边界。

复核：中英文段落语义对应；`git diff --check` 通过。官网版本断层留给独立仓库的 T02。

## T03：状态语义与预热反馈

状态：**IMPLEMENTED**。

- `web/src/admin/pages/AccessLogs.tsx`：普通 MISS 使用中性色；保留 `HIT`/`MISS` 技术标签，并提供中英文 hover 和屏幕阅读器解释。
- `web/src/admin/pages/CacheManage.tsx`：预热状态改为 `idle`、`submitting`、`accepted`、`failed`；202 Accepted 只显示“已接受/后台处理中”，不会冒充全部完成。失败使用 `getApiError`，保留输入并阻止重复提交；空白或仅注释输入不可提交。
- `web/src/i18n/en.ts`、`web/src/i18n/zh.ts`：同步新增状态与结果文案。
- `web/e2e/admin-forms.spec.ts`：覆盖仅注释输入被禁用及已接受状态；既有访问日志表格用例继续覆盖窄屏渲染。

复核结论：满足 T03 验收项；未改变 API、数据库、权限、分页或筛选。未引入新的全局状态或 UI 依赖。

## 验证

PASS：

```text
git diff --check
make lint-i18n
npm --prefix web run type-check
npm --prefix web run lint -- --quiet
npm --prefix web run test:unit
cd web && npx playwright test admin-forms.spec.ts --grep='dynamic rule'
cd web && npx playwright test admin-tables-actions.spec.ts --grep='/admin/logs keeps data'
make check
```

本轮结果：`make check` 全部通过；其中包括 Go vet/短测试、前端类型检查、lint、i18n 审计、11 个前端单测文件（41 个测试）、前端构建，以及 6 个 Chromium smoke 用例。第一次 ESLint 因本地 `web/test-results` 目录不存在失败，创建该被忽略的本地目录后重跑通过，属于环境前置问题而非代码失败。

NOT_RUN：`make verify`、生产嵌入 UI、真实包管理器和网络门禁。T03 未改变协议或后端，不额外宣称这些检查通过。

## 人工复验

1. 登录 Admin → Access Logs，确认 `MISS` 为中性色，并用鼠标悬停或键盘辅助技术读取解释。
2. 登录 Admin → Cache → Warmup，输入一行 `# comment`，确认提交按钮禁用。
3. 输入一个真实包名并提交，确认显示“已接受/后台处理中”；模拟 4xx/5xx 时确认显示脱敏错误且输入仍在。

建议 commit 标题：`fix(admin): clarify cache miss and warmup states`
