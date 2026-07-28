---
target: Admin 上游源页面 /admin/upstreams
total_score: 23
max_score: 40
na_heuristics:
p0_count: 0
p1_count: 5
timestamp: 2026-07-28T13-05-29Z
slug: web-src-admin-pages-upstreams-tsx
---
Method: dual-agent (A: /root/upstreams_design_a · B: /root/upstreams_detector_b)

## Design Health Score

| # | Heuristic | Score | Key Issue |
|---|-----------|------:|-----------|
| 1 | Visibility of System Status | 2 | 批量检测没有进度、部分失败结果或结束摘要。 |
| 2 | Match System / Real World | 3 | 生态、延迟和探测术语准确，但优先级与探测行为缺少上下文。 |
| 3 | User Control and Freedom | 3 | 模态框退出和重试完善；不可逆删除没有撤销。 |
| 4 | Consistency and Standards | 3 | 组件与状态规则统一，但浅色模式状态文字存在系统性对比度失败。 |
| 5 | Error Prevention | 2 | 删除确认不指名对象，批量检测期间仍可重复发起单行检测。 |
| 6 | Recognition Rather Than Recall | 2 | URL、优先级、代理和探测设置必须逐个打开编辑框才能比较。 |
| 7 | Flexibility and Efficiency | 2 | 有单项/全部检测，但无搜索、状态筛选或长列表压缩。 |
| 8 | Aesthetic and Minimalist Design | 3 | Instrument 风格克制明确，但固定宽度心跳图造成桌面无效扫描距离。 |
| 9 | Error Recovery | 2 | 初始/刷新/保存错误可恢复；单项检测、批量检测和删除错误缺少反馈。 |
| 10 | Help and Documentation | 1 | 优先级、代理、探测模式和删除影响缺少就地说明。 |
| **Total** | | **23/40** | **Acceptable；视觉基础强，操作闭环需要补齐。** |

## Design Specificity Verdict

### LLM assessment

页面具有很强的 Depsilo 特异性：生态图标、24 小时心跳、统一健康分级和 mono 数据形成了真正的运维仪表感。问题不是“太普通”，而是共享的 Monitor 展示模型压过了 Admin 管理任务。当前行主要回答“现在健康吗”，却不能直接回答“实际连接哪个 URL、按什么优先级、怎样探测”。

### Deterministic scan

源码级检测器返回 `[]`，共 0 个确定性问题。运行时浏览器证据补充了源码扫描无法发现的缺陷：

- Axe 在桌面浅色英文页面发现 1 个 serious `color-contrast` 规则、46 个节点，均来自健康/降级行的 11px 延迟和状态文字。
- Live overlay 报告 9 个目标组、11 条规则项；与本页直接相关的是组摘要文字过小和类型层级偏平。
- `layout-transition` 与 `marquee` 来自全局加载但本路由未渲染的样式，属于本页误报。
- 6 条 10px 文字来自共享 Admin shell，是真实问题但不属于本页修复边界。

### Visual overlays

可变脚本注入成功，`detect.js` 已在独立浏览器页运行并生成 8 个元素覆盖层和 1 个页面级横幅。当前环境不能展示用户可见的 `[Human]` 标签页，因此可靠证据来自注入后的 console、序列化结果、覆盖层 DOM 与截图检查。

## Overall Impression

第一眼专业、稳定、很像 Depsilo；开始管理多个源之后，页面却要求操作者靠记忆和长距离滚动完成工作。最大机会是把“健康心跳列表”升级为“可检索、可核对、反馈闭环的上游配置清单”。

## What's Working

1. 生态分组、状态点、延迟和心跳条构成了明确的 Depsilo 仪表语言，两种主题下都保持克制。
2. 初始 pending、初始错误、stale 数据和延迟历史失败都遵守独立 query boundary；27 个源只请求一次批量延迟接口。
3. 行操作都有准确名称和 41×41px 目标；心跳支持方向键、Home/End/Escape；移动端没有文档级横向溢出。

## Priority Issues

### [P1] 配置管理页隐藏了关键配置

**Why it matters:** 操作者无法在清单中核对 URL、优先级、代理、探测模式/间隔与成功率；只读用户甚至没有任何查看入口。

**Fix:** 保留紧凑行与心跳图，但增加 Admin 专属元数据层，至少直接展示 URL 主机、优先级、探测方式和成功率；完整值通过 title/非修改性信息呈现。

**Suggested command:** `$impeccable layout`

### [P1] 27+ 项没有检索与压缩模型

**Why it matters:** 390px 页面高度超过 3200px；同一生态拥有大量源时形成极长单列，未来制品类型增加后会迅速失控。

**Fix:** 增加名称/URL/生态搜索与健康状态筛选；大分组横跨内容区并在内部响应式分栏，异常源优先；过滤为空时提供清除操作。

**Suggested command:** `$impeccable distill`

### [P1] “全部检测”隐藏进度和部分失败

**Why it matters:** 用户不知道完成了多少、哪些失败；27 个请求同时发出且单行检测仍可重复触发。一次失败在结束后看起来与完全成功相同。

**Fix:** 显示 `已检测 x/y`，通过 live region 宣告进度和结果；批量期间禁用重复单行检测；完成后明确健康、异常和请求失败数。

**Suggested command:** `$impeccable harden`

### [P1] 删除与检测错误恢复不完整

**Why it matters:** 删除确认不显示名称、生态或 URL，失败后也没有错误；单行检测拒绝同样静默。

**Fix:** 删除框指名对象并说明影响，保留上下文显示内联错误；单行检测用语义 Toast 告知健康结果或失败原因；服务错误映射成可操作的双语文案。

**Suggested command:** `$impeccable clarify`

### [P1] 浅色模式状态文字未达到 WCAG AA

**Why it matters:** Axe 在 23 个非失败行上记录了 46 个 serious 对比度节点，重复出现在页面最重要的状态信息中。

**Fix:** 状态点保留语义色，正文改用对比充分的 `--text` / `--text-muted`；需要强调时使用填充/边框而不是小字号彩色文本。

**Suggested command:** `$impeccable audit`

### [P2] 移动端顶层命令小于项目控制尺寸

**Why it matters:** `全部检测` 与 `添加上游源` 只有 32px 高；URL 错误也未与输入框程序化关联。

**Fix:** 移动端顶层命令至少 40px；使用 Input 的 `error` 属性建立 `aria-invalid` 和 `aria-describedby`；生态分组使用语义标题。

**Suggested command:** `$impeccable adapt`

## Persona Red Flags

### Alex（高频运维者）

- 无法按名称或 URL 搜索，也不能只看故障/降级。
- 优先级必须逐个打开编辑框才能比较。
- “全部检测”没有进度或结果，且允许重复单项检测。

### Sam（键盘/读屏用户）

- 行操作和心跳键盘行为良好。
- 分组在视觉上明显，但不是 heading/region。
- URL 错误未与输入框关联；批量状态和删除失败没有语义播报。
- 浅色模式小字号状态文字对比度失败。

### Morgan（小团队值班操作者）

- 能快速看到健康状态，却不能直接确认实际服务 URL 和路由顺序。
- 泛化的删除确认不足以支撑影响所有开发机和 CI 的高风险操作。
- 部分批量检测失败看起来像完全成功，会削弱故障期间对页面的信任。

## Minor Observations

- 空状态只显示“暂无上游源”，没有解释首次配置与只读场景。
- 长名称在移动端会被三个常驻行操作压缩。
- 成功率已经从接口返回并映射，但没有显示。
- Edit 锁定生态是正确行为，但未解释不可变原因。
- 27×44 个心跳刻度目前仍可接受；真正瓶颈是查找与比较，而不是绘制。

## Questions to Consider

- 这张页面首先应该回答“健康吗”，还是“当前究竟怎样配置并路由”？如果后者，为什么心跳比 URL 和优先级更醒目？
- 当 27 个探测中有 1 个请求失败时，“全部检测完成”应该怎样定义？
- 异常源是运维例外，为什么目前不是最快的扫描路径？
- 随着通用制品库方向推进，上游清单何时需要升级为仓库角色、路由顺序和权限共同可见的模型？

Questions skipped: 用户此前已明确要求一次完成全部问题，本轮问题证据与修复方向均已足够明确。

## Recommended Actions

1. **`$impeccable layout`**：加入管理专属配置摘要并重排大分组。
2. **`$impeccable distill`**：加入搜索、状态筛选与过滤空状态。
3. **`$impeccable harden`**：补齐批量检测进度、部分失败和并发冲突反馈。
4. **`$impeccable clarify`**：改造删除确认、字段说明和服务错误文案。
5. **`$impeccable audit`**：修复浅色对比度、语义标题和字段错误关联。
6. **`$impeccable adapt`**：修复移动端顶层触控尺寸和长列表布局。
7. **`$impeccable polish`**：完成中英文、明暗主题和响应式最终检查。
