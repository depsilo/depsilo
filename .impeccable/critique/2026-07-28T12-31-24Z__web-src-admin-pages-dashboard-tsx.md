---
target: Admin 总览页
total_score: 28
max_score: 40
na_heuristics:
p0_count: 0
p1_count: 4
timestamp: 2026-07-28T12-31-24Z
slug: web-src-admin-pages-dashboard-tsx
---
Method: dual-agent (A: /root/admin_dashboard_design_a · B: /root/admin_dashboard_detector_b)

## Design Health Score

| # | Heuristic | Score | Key Issue |
|---|-----------|-------|-----------|
| 1 | Visibility of System Status | 3 | 实时服务、下载和查询状态清楚，但降级根因远在首屏下方，稳定态缺少语义状态信号。 |
| 2 | Match System / Real World | 3 | 请求、命中、带宽、延迟符合运维心智；热门包却只覆盖 PyPI/APT。 |
| 3 | User Control and Freedom | 3 | 趋势维度与时间范围可切换，失败可重试；告警没有就地进入处理页面。 |
| 4 | Consistency and Standards | 3 | 双主题、双语和共享组件整体一致；趋势控件没有采用既有 40px 控件尺度。 |
| 5 | Error Prevention | 3 | 主要为只读总览，危险操作不在本页；小触控目标仍会增加误触。 |
| 6 | Recognition Rather Than Recall | 3 | 标签和状态文案可识别；变化百分比的比较窗口、图表读屏含义仍不够直接。 |
| 7 | Flexibility and Efficiency | 2 | 关键降级信息与修复入口分离，移动端需要长距离滚动。 |
| 8 | Aesthetic and Minimalist Design | 3 | Instrument 视觉密集、安静且稳定；10–11px 文本和八个碎片化筛选按钮削弱可读性。 |
| 9 | Error Recovery | 3 | 各查询已有显式重试和陈旧数据保留；Dashboard 主查询失败仍会遮蔽独立模块。 |
| 10 | Help and Documentation | 2 | 空状态有解释，但健康/容量告警没有明确的下一步动作。 |
| **Total** | | **28/40** | **Good foundation, operational hierarchy needs work** |

## Design Specificity Verdict

**LLM assessment**：页面已经有 Depsilo 自己的运维语言：服务状态、实时下载、命中/未命中、上游心跳和节省带宽共同构成可信的缓存代理总览，不是通用 SaaS 模板。最大缺口不是“更漂亮”，而是关键状态与处理动作脱节；4/6 上游健康在首屏出现，具体问题却到桌面约 y=1498、移动端约 y=1881 才出现。

**Deterministic scan**：目标文件 CLI 扫描 0 项。浏览器 overlay 在完整 Admin DOM 中得到 28 个 finding groups / 29 条规则项：`undersized-ui-text` 23、`tiny-text` 4、`layout-transition` 1、`marquee` 1。后两项分别来自全局 body 过渡和尊重 reduced-motion 的装饰扫光，不能归因于 Dashboard；小字号结果与实际 10–11px 文本、约 25px 趋势按钮相互印证。

**Visual overlays**：注入成功并完成两种尺寸检查；临时 live server 已停止。overlay 属于检查会话，不是产品内持久功能。

## Overall Impression

总览的信息选取正确、双主题与双语稳定，但“发现异常 → 理解原因 → 前往处理”的路径尚未闭环。优先把状态变成行动入口，并让独立数据模块互不拖累，再处理排版细节。

## What's Working

- 实时服务条和最近下载把“系统是否在工作”放在 KPI 之前，符合小团队运维顺序。
- KPI 在 1440px 为四列、390px 为 2×2，文档无横向溢出，扫描距离受到 1440px 上限控制。
- 查询错误、陈旧数据、空数据和 reduced-motion 已有明确分支；浏览器 console、page error、failed request 与 axe 均为 0。

## Priority Issues

### [P1] 首屏降级状态没有根因与处理入口

**Why it matters**：操作者看到 4/6 上游健康后仍要长距离滚动，无法快速判断哪两个源有问题。

**Fix**：在首屏状态区域显示需关注上游的名称摘要，并把上游计数与告警链接到上游管理；缓存高水位告警同样链接到缓存管理。

**Suggested command**：`$impeccable clarify`

### [P1] 趋势筛选不适合触控

**Why it matters**：390px 下八个按钮只有约 24.5–26.5px 高，误触风险高，也让两个控制组看起来像散落文本。

**Fix**：改成两个清晰的 segmented rails，按钮最小高度 40px，窄屏各自四等分、桌面并排。

**Suggested command**：`$impeccable adapt`

### [P1] Dashboard 主查询遮蔽独立实时模块

**Why it matters**：主快照加载或失败时，服务状态和最近下载也被整体 return 掉，违反独立查询边界，延迟操作者最需要的信息。

**Fix**：移除整页 early return；实时、趋势、带宽各自挂载，Dashboard 快照只控制 KPI、热门包与上游区域。

**Suggested command**：`$impeccable harden`

### [P1] 热门包与多生态产品承诺不一致

**Why it matters**：标题写 Top 10，但数据源只查 PyPI/APT，npm、Maven、Cargo 等真实流量不可见。

**Fix**：后端按所有 adapter_type 全局聚合 Top 10，保持 `{ecosystem: items}` 响应兼容；前端动态合并全部键。

**Suggested command**：`$impeccable clarify`

### [P2] 图表和细节文本的可访问表达偏原始

**Why it matters**：Recharts 虽可聚焦，但 accessible name 只是轴刻度拼接；10–11px 辅助文本在高密度区域阅读成本偏高。

**Fix**：给图表提供本地化描述，保留键盘层；把关键标签提升到 11–12px，装饰图标从读屏树移除。

**Suggested command**：`$impeccable typeset`

## Persona Red Flags

**小团队值班开发者**：收到降级提示后，首屏只有健康数量，没有问题源名称和直达入口；在手机上必须滚动约两屏才能定位。

**首次部署的个人开发者**：空状态解释了尚无流量，但主查询加载会推迟实时模块挂载；缓存告警没有告诉他下一步去哪处理。

**平台工程师 / Power User**：趋势切换有键盘语义，但图表读屏名称不能表达当前维度和范围；Top 10 缺失非 PyPI/APT 生态会让判断失真。

## Minor Observations

- `NowStrip` 用独立的 `·` 元素分隔，窄屏换行可能留下孤立分隔符。
- 最后活动与运行时长使用单行 flex，长包名和英文文案下可能互相挤压。
- 首屏 skeleton 与真实模块顺序不一致，加载完成后会产生明显布局变化。

## Questions to Consider

- 如果操作者只有 30 秒，首屏是否已经回答“服务是否健康、哪里坏了、下一步去哪”？
- 趋势图的四个维度是否都需要同时显露，还是保留当前两组 segmented 控件已足够？
- 热门包未来是否应按项目或生态筛选，而不是继续扩大同一张全局榜单？
