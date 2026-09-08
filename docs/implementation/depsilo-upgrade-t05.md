# 改造执行记录：T05 访问日志详情

候选基线：`e75b7e1`  
记录日期：2026-09-08

状态：**IMPLEMENTED**。

- Access Logs 新增详情按钮，使用现有 Drawer 原语；桌面侧滑、窄屏可滚动，保留 Escape、焦点回收和浏览器返回行为。
- 详情通过 T04 的 `GET /admin/logs/:id` 加载，缓存、策略、交付分别展示；`unknown`、`not_recorded` 和旧记录空值统一显示为“未记录”。
- 展示请求 ID、时间、HTTP 状态、上游及关联审计事件；错误沿用脱敏 API 错误，不拼接相近时间的记录。
- 支持复制脱敏诊断摘要；没有新增规则测试、放行或配置修改动作。
- 详情 ID 写入当前 URL 的 `detail` 参数，筛选、分页、关闭和返回均不会丢失原页面状态。
- 中英文文案同步，增加 Playwright 覆盖详情加载与返回。

验证（PASS）：

```text
npm --prefix web run type-check
npm --prefix web run lint -- --quiet
make lint-i18n
cd web && npx playwright test admin-tables-actions.spec.ts --grep='access log details'
```

建议 commit 标题：`feat(admin): add access log request details`。
