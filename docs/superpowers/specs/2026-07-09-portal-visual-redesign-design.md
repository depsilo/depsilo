# 门户视觉重设计（方向 A · 信号绿·进化）— 设计文档

日期：2026-07-09
状态：已与产品负责人对齐（三方向对比稿 → 选 A → 范围收敛 → 文案定稿）

## 背景与目标

门户（免登录，QuickStart + Monitor 两页）在产品转向"供应链执行层"后仍讲旧故事。
本期做**纯视觉与文案演化**：保留 Instrument 暗色 + 信号绿的品牌资产，提升质感，
文案开始携带"缓存加速 + 护栏"双价值叙事，并优化高分辨率屏幕的空间利用。

对比稿存档：三套方向 HTML（信号绿·进化 / 哨戒蓝·控制室 / 纸感·审计台账）由
会话临时目录生成，选定 A 后其余仅作记录，不入库。

## 范围内（deliverables）

### 1. 背景

- `.page-wash` 移除 44px 网格线（两条 `linear-gradient` 网格层删除）。
- 保留顶部品牌绿光晕（第一层 `linear-gradient`），维持 A 稿"一角透光"的克制氛围。

### 2. Hero 文案（QuickStart，中英同步）

| 位置 | zh | en |
| --- | --- | --- |
| 眉标 chip | 本地镜像 · 供应链护栏 | Local mirror · Supply-chain guardrail |
| H1 | 装得快，也装得放心 | Install fast. Install safe. |
| 副标 | 依赖全部走局域网缓存，热包秒回、上游宕机不断供；最小发布年龄护栏把刚上架的投毒版本拦在构建之外。 | Every dependency flows through the LAN cache — hot packages come back in milliseconds and upstream outages don't stop installs. The minimum-release-age guardrail keeps just-published poisoned versions out of your builds. |
| 提示行 | 复制提示词给 Claude Code / Cursor，一分钟接入 | Paste the prompt into Claude Code / Cursor — connected in a minute |

眉标 chip 不再使用 "✦ AI 一键集成"（AI 动作语义下移到按钮与提示行）。

### 3. 提示词预览卡升级（HeroAICTA 右列）

- 头部左侧加文件名标签 `depsilo-integrate.md`（mono 小字，代替现"提示词预览"文案的位置层级，原 label 保留为辅助）。
- 预览行加行号列（mono、`--text-subtle`、右对齐，选不中 `user-select: none`）。
- 底部 meta 行："共 {{n}} 行 · 覆盖 14 生态"（n 由 prompt 实际行数计算）。

### 4. 主 CTA 流光花边（用户点名需求）

- "复制完整提示词"按钮外圈加**多彩 conic 渐变流光描边**：
  - 技术：复用 `.ai-chip` 的 `@property --angle` + conic-gradient 旋转方案，独立 class `.cta-shimmer`；
  - 色相：绿 → 青 → 琥珀 → 绿 的谱带（品牌绿为主锚点，多彩但不出紫）；
  - 参数：描边 1.5px，`inset -2px`，8s linear infinite；
  - 状态：`disabled` 与 `copied` 状态下不流光；`prefers-reduced-motion: reduce` 下静止为固定渐变描边；
  - 全页唯一常驻动画位从 ai-chip 移交给主 CTA（ai-chip 眉标随文案更换取消动画描边）。

### 5. 高分辨率屏幕支持

- Portal 内容容器：`max-width: 1520px` → `clamp(1280px, 92vw, 1840px)`（`PortalApp` main 与 header 内层同步）。
- Hero H1 字号：`clamp(34px, 3vw, 48px)` → `clamp(34px, 3.2vw, 56px)`；预览卡字号/行高同步微调。
- QuickStart 控制台：`minHeight: min(66vh, 760px)` → `min(70vh, 860px)`；生态栏宽度 `300px` → `clamp(300px, 19vw, 340px)`。
- Monitor：上游卡片 `minColumnWidth 380` 的 auto-fit 网格因容器变宽自然获得 4-5 列，无需改组件。
- 验证基准：2560×1440（产品负责人主力屏）与 1440×900 双档截图。

## 明确不做（本期范围外）

- 护栏带（GuardrailBand）组件及其公开 API（`/api/v1/guardrail/*`）——推迟。
- 隔离事件流的门户公开展示——不公开。
- QuickStart 页的"实时监控摘要"条——不加。
- 网格背景——移除且不保留开关。
- Admin 后台、Monitor 页功能（本周已上线的搜索保持不动）、代理与 quarantine 核心逻辑。
- B（哨戒蓝）/ C（纸感）方向元素。

## 验收标准

1. `npx tsc --noEmit` 与 `npm run build` 通过；i18n en/zh 叶子键数一致。
2. 连接的 Chrome 实测截图：1440 与 2560 两档宽度下 QuickStart / Monitor 布局正确，
   大屏无夸张留白，Monitor ≥4 列。
3. CTA 流光在 hover/正常态可见、copied/disabled 态关闭；`prefers-reduced-motion` 模拟下无动画。
4. 背景无网格线；顶部绿光晕保留。
5. 文案按上表落地，中英一致。

## 实现涉及文件（预估）

- `web/src/index.css`（page-wash、.cta-shimmer、容器宽度、响应式）
- `web/src/portal/PortalApp.tsx`（容器宽度）
- `web/src/portal/pages/QuickStart.tsx`（控制台尺寸）
- `web/src/portal/components/HeroAICTA.tsx`（文案结构、预览卡、CTA class）
- `web/src/i18n/zh.ts` / `en.ts`（文案）
