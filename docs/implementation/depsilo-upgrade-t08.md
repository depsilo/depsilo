# T08 实现记录：Dashboard 统计口径修正

Dashboard 原始访问聚合现在只把明确的 `hit` / `miss` 记录纳入命中率分母、服务字节和延迟统计。T04 引入的 `unknown` 结果（阻断或响应未完成）不会被误算为缓存未命中；诊断字段上线前的空值仍兼容保留。趋势原始查询使用同一过滤规则，窗口、导航和指标名称不变。

验证：`go test ./internal/api/admin -run TestDashboardTrends_RawQueryIsBoundedAndZeroFillsGaps -count=1`、`git diff --check`。
