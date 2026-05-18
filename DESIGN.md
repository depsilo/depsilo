# Depsilo Design System

> 这份文档描述 Depsilo **当前已实现**的设计语言，不是想要成为的样子。所有 token、字体、组件均可在 `web/src/index.css` 与 `web/src/components/` 找到对应实现。

---

## 0. 设计哲学：双层语言

Depsilo 同时面向两类人，因此采用**统一 token、双层表达**的策略：

| 层 | 路径 | 用户 | 性格 | 视觉态度 |
|----|------|------|------|----------|
| **Portal** | `/`, `/status`, `/monitor` | 开发者来"看一眼怎么用" | 有性格、有 brand、有姿态 | 紫色渐变 hero、大胆排版、品牌锚定 |
| **Admin** | `/admin/*` | 运维来"日常管理" | 克制、密集、工具属性 | chrome-light、小号 caps 标签、网格密集、零视觉噪音 |

两层共享：色彩 token、字体栈、阴影、状态色、网格、组件。差异**只在视觉强度**：Portal 把品牌色当资产用（gradient text、aurora glow、page wash），Admin 几乎只用品牌色作为 active 态。

**类比**：Portal 像 Stripe 的 marketing site，Admin 像 Stripe 的 dashboard。同一套语言，不同的音量。

---

## 1. Color System

所有颜色用 [`oklch`](https://oklch.com/) 表达 — 在浅/深主题间切换时仅改 lightness，色调与饱和度保持稳定。**禁止**新增 `#hex` 色值，必须走 token。

### 1.1 中性 / 表面

| Token | Light | Dark | 用途 |
|-------|-------|------|------|
| `--bg-page` | `#FFFFFF` | `oklch(0.18 0.012 250)` | 页面背景 |
| `--bg-card` | `#FFFFFF` | `oklch(0.22 0.013 250)` | 卡片表面 |
| `--bg-soft` | `#F5F5F5` | `oklch(0.25 0.014 250)` | 嵌套表面、skeleton、tag 底色 |
| `--bg-hover` | `rgba(0,0,0,0.035)` | `oklch(0.32 ... / 0.45)` | 行/按钮 hover |
| `--border` | `rgba(0,0,0,0.09)` | `oklch(0.92 0.01 250 / 0.10)` | 标准边框（卡片、输入） |
| `--border-soft` | `rgba(0,0,0,0.05)` | `oklch(... / 0.05)` | 极弱分隔（侧栏分组） |
| `--border-strong` | `rgba(0,0,0,0.18)` | `oklch(... / 0.22)` | 强调边框（focus ring） |

### 1.2 文本层级

| Token | Light | 用途 |
|-------|-------|------|
| `--text` | `#0a0a0a` | 主体文本、heading、metric value |
| `--text-muted` | `#555555` | 次级正文 |
| `--text-soft` | `#767676` | 副标题、tooltip、章节标签 |
| `--text-subtle` | `#8a8a8a` | eyebrow、最弱辅助文字 |

### 1.3 品牌 / Aurora

| Token | 值 | 用途 |
|-------|----|------|
| `--brand` | `oklch(0.55 0.16 295)` | 主品牌紫色（CTA、active 态） |
| `--brand-strong` | `oklch(0.48 0.17 295)` | hover 态深紫 |
| `--brand-soft` | `oklch(0.55 0.16 295 / 0.09)` | active 背景填充 |
| `--brand-border` | `oklch(0.55 0.16 295 / 0.30)` | 紫色描边（次级 CTA） |
| `--brand-text` | `oklch(0.40 0.14 295)` | 紫色文字（active 态文字色） |
| `--spec-1` | `oklch(0.62 0.18 305)` | aurora 渐变第一色（magenta-violet） |
| `--spec-2` | `oklch(0.66 0.15 260)` | aurora 第二色（blue-violet） |
| `--spec-3` | `oklch(0.72 0.12 210)` | aurora 第三色（sky-blue） |
| `--spec-4` | `oklch(0.78 0.13 75)` | aurora 第四色（warm yellow） |

**核心 Aurora 思想**：4 色渐变（magenta → violet → blue → warm yellow）是 Depsilo 的视觉签名。它出现在三个地方：
1. `--grad-aurora` 完整 4 色渐变（用于 page-wash、grad-ring）
2. `--grad-brand` 仅前两色 violet→blue-violet 简化版（用于 hero gradient text）
3. `--grad-rim` 横向 violet→cyan 透明渐变（用于卡片顶部 1px 高光线）

### 1.4 状态色

| Token | 值 | 配套 fill / border / text |
|-------|----|---------------------------|
| `--ok` | `oklch(0.58 0.12 155)` | `--ok-fill / --ok-border / --ok-text` |
| `--warn` | `oklch(0.70 0.14 65)` | `--warn-fill / --warn-border / --warn-text` |
| `--danger` | `oklch(0.60 0.17 25)` | `--danger-fill / --danger-border / --danger-text` |

**约定**：`-fill` 是低不透明度的背景填充（≈10%），`-border` 是中透明度描边（≈28-30%），`-text` 是可读性合规的暗色文字版本。永远三件套使用，不混用。

---

## 2. Typography

### 2.1 字体栈

| 角色 | 栈 | 来源 |
|------|----|------|
| Sans | `"Inter Variable", "Noto Sans SC", "PingFang SC", "Microsoft YaHei", sans-serif` | `@fontsource-variable/inter` + `@fontsource/noto-sans-sc` |
| Mono | `"JetBrains Mono Variable", ui-monospace, monospace` | `@fontsource-variable/jetbrains-mono` |
| Icon | `"Material Symbols Outlined"` | `material-symbols/outlined.css` |

**OpenType features**：body 全局启用 `'cv11', 'ss01', 'ss03'`。数字栏目额外用 `font-variant-numeric: tabular-nums`（通过 `.tabular-nums` 或 `.num` 类）。

**为什么不是 sohne-var**：sohne-var 是 Stripe 付费字体，不能在开源项目使用。Inter Variable 在中文 fallback Noto Sans SC 下渲染稳定，是当前最务实选择。

### 2.2 标题层级（默认值）

> 全部 wrap 在 `@layer base` 中，组件可用 Tailwind 任意覆盖。

| 元素 | size | weight | letter-spacing | line-height |
|------|------|--------|----------------|-------------|
| `h1` | 44px | 700 | -0.04em | 1.02 |
| `h2` | 26px | 700 | -0.03em | 1.1 |
| `h3` | 17px | 700 | -0.02em | 1.2 |
| `h4` | 13px | 600 | 0 | 1.3 |
| body | 13px | 400 | normal | 1.5 |

**说明**：默认值是 Portal hero 那一刀的尺寸。Admin 章节标题应该用 `.eyebrow`（见下），而**不是**直接用 `<h2>` `<h3>` — 这是有意设计：默认 heading 偏大、偏粗，是为 Portal 的 brand-anchored 大标题服务的。

### 2.3 `.eyebrow` —— 章节标签的标准模式

```css
.eyebrow {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--text-subtle);
}
```

Admin 所有 section header / metric label 都应该走这个类（或用 Tailwind 实现等价 `text-[12px] uppercase tracking-wider font-[400] text-soft`）。这是项目唯一的"小号 caps 标签"模式，它定义了 Admin 的克制气质。

### 2.4 数字 / 数据展示

```css
.num, .mono, [data-tabular] {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.02em;
}
```

所有 metric value、延迟数字、字节数、版本号必须用 mono + tabular-nums。**关键**：tabular 让相邻 KPI 数字按位对齐，避免抖动。

---

## 3. Effects Vocabulary

这是 Portal 的「灵魂调色盘」。Admin **极少使用**，仅在 Dashboard hero 区域可酌情少量引入一处。

### 3.1 `.grad-text` —— 渐变文字（Portal hero 标志）

```html
<h1 className="grad-text" style="font-size: 44px; font-weight: 700;">
  快速开始
</h1>
```

实现：用 `--grad-brand` 作为 background，`background-clip: text` + `color: transparent`。这是 Portal 所有页面 hero 标题的统一处理。

### 3.2 `.grad-ring` —— 渐变 1px 描边

aurora 4 色渐变作为 1px 描边（用 mask-composite 镂空内容）。用于"想强调但不想填实"的卡片，例如 Portal 的 "AI 自动配置" 区块。

### 3.3 `.aurora-glow` —— 径向模糊光晕

用 `::after` 伪元素 + 24px 模糊 + 紫色径向渐变制造卡片下方的"晕光"。Portal hero 区背景使用。

### 3.4 `.aurora-rim` / `.aurora-rim-bottom` —— 1px 高光线

`::before` / `::after` 伪元素在元素顶部 / 底部加一条 1px 横向渐变线（violet→cyan→透明）。用于划分页面区段或替代裸 border。**这是 Effects 中唯一允许 Admin 使用的**：当前用于 Admin topbar 底部（`opacity 0.55`），给 chrome-light 风格补一点品牌温度。

### 3.5 `.page-wash` —— 全屏径向渐变背景

```css
.page-wash {
  position: fixed; inset: 0; pointer-events: none; z-index: 0;
  background:
    radial-gradient(900px 360px at 12% -10%, oklch(0.62 0.18 305 / 0.05), transparent 60%),
    radial-gradient(720px 320px at 88% -8%,  oklch(0.72 0.12 210 / 0.045), transparent 60%);
}
```

Portal 页面背景上的极淡左右两团光，是 Portal 区别于 Admin 的关键 atmosphere 信号。Admin **不要**使用。

---

## 4. Components

### 4.1 `CardV2`（共享）

```tsx
<CardV2 className="...">
  <h3 className="eyebrow mb-3">章节标签</h3>
  ...
</CardV2>
```

- 背景 `var(--bg-card)`
- 边框 `0.5px solid var(--border)`
- 圆角 `var(--r-card)`（10px）
- 默认内距 `p-5`（20px）
- 阴影 `var(--shadow-card)` —— 见 §6

适用：Admin 所有信息块、Portal 的内容容器。

### 4.2 `MetricCardV2`（共享）

```tsx
<MetricCardV2
  label="今日请求"
  value="12,840"
  icon={<Icon name="monitoring" />}
  change={+12.5}
  sparkline={<...>}
/>
```

- 容器：5px 圆角、1px 边框、白底
- label：使用 `.eyebrow` 类（10px mono caps text-subtle）
- value：mono 32px / weight 600 / letter-spacing -0.04em / `var(--text)`
- change：mono 11px，正值 `--ok-text` 绿、负值 `--danger-text` 红
- sparkline：可选，绝对定位右下角，opacity 0.7

适用：Dashboard、Bandwidth、Monitor 顶部 KPI tile 行。

### 4.3 按钮规范

| 类型 | 背景 | 文字 | 边框 | 圆角 | padding | 字号/字重 |
|------|------|------|------|------|---------|-----------|
| Primary | `var(--brand)` | `#fff` | none | 4px | 8px 16px | 14px / 500 |
| Brand-soft | `var(--brand-soft)` | `var(--brand-text)` | none | 4px | 8px 16px | 14px / 500 |
| Ghost | transparent | `var(--brand-text)` | `1px solid var(--brand-border)` | 4px | 8px 16px | 14px / 500 |
| Chrome | `var(--bg-soft)` | `var(--text-muted)` | none | 6px | 0 8px | 11px / 500 |

Chrome 是 Admin 顶部 EN/Auto/版本切换 那种"工具栏小按钮"，刻意比 Primary 小一号。

### 4.4 Pill / Badge / Tag

Tag 圆角 `var(--r-tag)`（6px）。状态 badge 用三件套：

```tsx
<span style={{
  background: 'var(--ok-fill)',
  color: 'var(--ok-text)',
  border: '1px solid var(--ok-border)',
  padding: '1px 6px',
  borderRadius: 4,
  fontSize: 10,
}}>健康</span>
```

### 4.5 输入

| 项 | 值 |
|----|----|
| 边框 | `1px solid var(--border)` |
| 圆角 | 4px |
| Focus | `outline: 2px solid var(--brand)` 或 `border-color: var(--brand)` |
| 字号 | 14px |
| 内距 | 8px 12px |

---

## 5. Layout & Spacing

### 5.1 间距 token

```
--spacing-1: 4px
--spacing-2: 8px
--spacing-3: 12px
--spacing-4: 16px
--spacing-5: 20px
--spacing-6: 24px
--spacing-8: 32px
--spacing-12: 48px
```

实操中绝大多数使用 Tailwind 的 4px 倍数（`gap-2`/`gap-4`/`gap-6`/`p-5`/`p-8`）。**避免**写 7px / 13px / 22px 这种非倍数值。

### 5.2 圆角 token

```
--r-pill:  4px   /* 按钮、徽章 */
--r-tag:   6px   /* 标签、chrome 按钮 */
--r-card:  10px  /* CardV2 标准卡片 */
--r-shell: 14px  /* 外壳容器（Portal 大区块） */
```

**不用 12px+ 的圆角和 pill 形按钮**。Depsilo 的"高级感"建立在保守圆角上，不是 bubbly。

### 5.3 网格

| 用途 | 规则 |
|------|------|
| Portal 内容宽度 | 最大 1280px，居中 |
| Admin 内容区 | `ml-[220px] p-8`（侧栏 220px + 主区 padding 32px） |
| Admin topbar | 高度 48px，固定，背景 `color-mix(... 88%)` + `backdrop-filter: blur(8px)` |
| 卡片网格 | `grid gap-4 grid-cols-{2,3,4}`，间距 16px |

### 5.4 断点

| 名 | 宽度 |
|----|------|
| Mobile | <640px |
| Tablet | 640-1024px |
| Desktop | 1024-1280px |
| Wide | >1280px |

Admin 在 <1024px 不保证可用（运维 admin 默认桌面）。Portal 必须 mobile-friendly。

---

## 6. Depth & Elevation

Depsilo 使用蓝调多层阴影系统，全部走 token，**不要写裸 box-shadow**。

### Light theme

```css
--shadow-card: rgba(50, 50, 93, 0.06) 0 6px 18px -6px,
               rgba(0, 0, 0, 0.04)    0 3px 9px -3px;
--shadow-pop:  rgba(50, 50, 93, 0.18) 0 30px 45px -30px,
               rgba(0, 0, 0, 0.10)    0 18px 36px -18px;
```

### Dark theme（白色阴影在暗底失效，改用紫色 ring + 黑色 drop）

```css
--shadow-card: 0 0 0 1px oklch(0.55 0.16 295 / 0.04),
               rgba(0, 0, 0, 0.25) 0 6px 18px -6px;
--shadow-pop:  0 0 0 1px oklch(0.55 0.16 295 / 0.08),
               rgba(0, 0, 0, 0.45) 0 24px 48px -16px;
```

### 使用规则

| Token | 强度 | 用途 |
|-------|------|------|
| `--shadow-card` | 极轻（6px 模糊，6% 不透明） | CardV2、MetricCardV2 默认卡片 |
| `--shadow-pop` | 中（30-45px 模糊） | 弹层、Popover、下拉、Modal |

强度刻意低于 Stripe spec —— Depsilo 是工具类 UI，不需要"营销卡片"的强烈悬浮。

---

## 7. Portal Patterns（仅 Portal 使用）

### 7.1 Hero 模板

```tsx
<div className="relative">
  <div className="page-wash" />          {/* 全屏极淡光晕 */}
  <div className="aurora-glow">           {/* 紫色光晕背景 */}
    <h1 className="grad-text"             {/* 渐变文字 */}
        style={{ fontSize: 44, fontWeight: 700, letterSpacing: '-0.04em', lineHeight: 1.02 }}>
      {title}
    </h1>
    <p style={{ fontSize: 17, fontWeight: 400, color: 'var(--text)', maxWidth: 580, marginTop: 14 }}>
      {subtitle} <span style={{ color: 'var(--text-soft)' }}>{subtitleAlt}</span>
    </p>
  </div>
</div>
```

### 7.2 服务地址栏 `<ServiceUrlBar>`

水平 pill：左侧绿色 dot + 单行 mono 文字 + 右侧 "复制" 按钮。圆角 4px，soft 描边，monoplate 风。

### 7.3 代码块 `<CodeBlock>`

```
- 容器：bg-soft 背景 + 4px radius + border-soft
- 顶栏：可选文件名 / 语言 + 右上 "Copy" 按钮
- 代码：JetBrains Mono 12-13px / line-height 1.6
- 复制后按钮短暂显示"已复制"绿色
```

### 7.4 语言导轨 `<LanguageRail>`

垂直堆叠的语言选择项：
- 包管理器图标（28×28 SVG）+ 名称
- active 态：`bg-brand-soft` + `text-brand`
- hover：`bg-hover`

---

## 8. Admin Patterns（仅 Admin 使用）

### 8.1 `MainLayout` 三段式

```
┌────────┬───────────────────────────────────┐
│        │  topbar 48px (页面标题 + chrome)  │
│ aside  ├───────────────────────────────────┤
│ 220px  │                                   │
│        │  main (p-8, scrollable)           │
│        │                                   │
└────────┴───────────────────────────────────┘
```

- 侧栏背景 `var(--bg-page)`，分组标题用 `.eyebrow` 类（监控 / 管理）
- topbar 背景 `color-mix(in oklab, var(--bg-page) 88%, transparent)` + `backdrop-filter: blur(8px)`
- topbar 底部走 `.aurora-rim-bottom` 类（不要用 `border-bottom`），让 chrome 和品牌发生一次轻接触
- topbar 标题：`<h1 className="text-[14px] font-[400]">` —— **必须用 Tailwind 覆盖默认 h1 大小**

### 8.2 章节标签

所有 CardV2 内的标题使用：

```tsx
<h3 className="text-[12px] uppercase tracking-wider font-[400]"
    style={{ color: 'var(--text-soft)' }}>
  章节名
</h3>
```

或等价的 `.eyebrow` 类。**禁止**在 Admin 内部使用裸 `<h2>` `<h3>` 带默认 26px/17px 粗体样式。

### 8.3 数据表

- Header row：`.eyebrow` 类标签
- Row：`text-[13px]`，hover 显示 `var(--bg-hover)` 背景
- 操作图标：右对齐，`size-sm` 18px Material Symbols
- 行间距：`py-2`（8px 上下）—— Admin 是密集表格

### 8.4 Heartbeat bar

90 个小方格代表 24 小时，颜色按延迟分桶：
- `< 50ms` → `var(--ok)`
- `50-200ms` → `var(--warn)`
- `> 200ms` → `var(--danger)`
- 无数据 → `var(--bg-soft)` 灰

格子高度 6px，间距 0.5px。是 Depsilo 时间序列的标志性可视化。

### 8.5 Pro 徽章

```tsx
<span className="text-[10px] px-1.5 py-0.5 rounded-[4px]"
      style={{
        background: 'var(--brand-soft)',
        color: 'var(--brand-text)',
        border: '1px solid var(--brand-border)',
      }}>
  Pro
</span>
```

---

## 9. Do's and Don'ts

### Do
- 用 `var(--token)` 引用所有颜色 / 圆角 / 间距
- Portal hero 一律用 `.grad-text` + 44px / 700
- Admin 章节标签一律用 `.eyebrow` 或等价 Tailwind 组合
- 数字栏一律 mono + `tabular-nums`
- 状态色三件套（`-fill / -border / -text`）一起用
- 圆角保持在 4-14px 范围
- 暗色模式同时验证

### Don't
- 不要把 Portal 的 `.grad-text` / `.aurora-glow` / `.page-wash` 用进 Admin —— 会破坏工具气质
- 不要在 Admin 用裸 `<h2>` `<h3>`（默认样式过粗），用 `.eyebrow`
- 不要写 `#hex` 或裸 `oklch()` —— 走 token
- 不要用 12px+ 圆角或 pill 形 —— 项目刻意保守
- 不要给 H1 用渐变文字以外的强调（不要描边、不要阴影、不要填实背景）
- 不要把品牌紫色用在装饰性图形（仅用作 active 态、CTA、Pro 徽章、aurora 渐变）
- 不要在数据表中给整行加品牌色（用 hover bg 即可）
- 不要使用付费字体（不要试图加载 sohne-var、SF Pro 等）

---

## 10. Agent Prompt Guide

### 10.1 用 token 表达，不用值

❌ "把这个标题改成 #7c5cf0"
✅ "用 var(--brand) 作为标题色"

### 10.2 引用现有 pattern

❌ "做一个紫色渐变标题"
✅ "套用 Portal hero 模板（.grad-text + 44px/700）"

❌ "在 admin 加一个小标题"
✅ "用 .eyebrow 类"

### 10.3 跨主题验证

每次改色 / 改阴影 / 改透明度后，必须验证：
- light 主题（默认）
- dark 主题（`html.dark` 或 `data-theme="dark"`）

### 10.4 Examples

- "Portal hero：套用 §7.1 模板，title='系统状态', subtitle='实时性能指标 ...'"
- "Admin 卡片：用 CardV2 + .eyebrow 标题 + MetricCardV2 三件套（label+value+change）"
- "状态徽章：var(--ok-fill) bg + var(--ok-text) text + var(--ok-border) border, 4px radius"
- "新按钮：Primary 规范（var(--brand) bg, white text, 4px radius, 14px/500）"

---

## 11. 设计决策日志

| 决策 | 时间 | 理由 |
|------|------|------|
| 放弃 sohne-var, 改用 Inter Variable | 项目早期 | sohne-var 付费且不可商用，Inter 在中文 fallback 下渲染稳定 |
| 默认 heading 走 weight 700（非 Stripe 的 300） | 项目早期 | weight 300 的中文笔画过细在小屏掉字；Inter 700 中英混排稳定 |
| h1/h2/h3/h4 wrap 在 `@layer base` | 2026-05-15 | Tailwind v4 的 @layer 机制下，未 wrap 会导致组件级 utility 失效，topbar h1 渲染成 44px 而非 14px |
| 双层语言（Portal 高表达 / Admin 克制） | 项目早期 | 同时面向"想用一下"和"日常运维"两类人，单一风格无法两边讨好 |
| 启用蓝调多层 `--shadow-card`，绑定 CardV2/MetricCardV2 | 2026-05-15 | flat 看起来"工具"但缺产品力；6% 不透明的极轻阴影既有"悬浮感"又不影响密集网格 |
| `--text` 由 `#0a0a0a` 改为 `#061b31` 深海军（含整套 muted/soft/subtle 调成 slate 系列） | 2026-05-15 | 纯灰黑显冷漠；深海军是 fintech 标志色，文字层级整体调成 slate 家族后温度更统一 |
| Admin topbar bottom 用 `.aurora-rim-bottom` 替代 `border-bottom` | 2026-05-15 | 裸 border 像静态 chrome；1px violet→cyan 渐变线（55% 不透明）让 admin 与 Portal 的品牌发生一次轻接触，不影响工具气质 |
| 所有设 `position` 的自定义类必须 `@layer utilities + :where()` | 2026-05-18 | 未分层的 `.aurora-rim-bottom { position: relative }` 静默覆盖 Tailwind 的 `.fixed`，admin topbar 退回 relative → 宽度 1060→1280 → 触发横向滚动。同型修复扩展到 `.grad-ring` / `.aurora-glow` / `.dot-halo`，见 §13 |

---

## 13. 已知陷阱：Tailwind v4 Cascade

### 13.1 问题

Tailwind v4 把所有 utilities 放在 `@layer utilities` 中。CSS cascade 规则规定：**未分层（unlayered）的规则总是胜过 `@layer` 内的规则，无论 specificity 如何**。

后果：自定义 CSS 类如果未分层、且设置了 Tailwind utility 也会管的属性（`position` / `font-size` / `font-weight` / `color` / `background` ...），会**静默覆盖** Tailwind utility。

```html
<!-- 期望：position: fixed (Tailwind .fixed)
     实际：position: relative (.aurora-rim-bottom 未分层胜出) -->
<header class="aurora-rim-bottom fixed top-0 left-[220px] right-0">
```

### 13.2 已踩过的两次

| 时间 | 类 | 被覆盖的 utility | 症状 |
|------|----|------------------|------|
| 2026-05-15 | `h1, h2, h3, h4 { font-size: 44px; font-weight: 700 }` | `text-[14px] font-[400]` | Admin topbar 标题渲染成 44px/700，把 48px 高的 header 撑爆 |
| 2026-05-18 | `.aurora-rim-bottom { position: relative }` | `fixed` | Admin topbar 退回 relative → 宽度 1060px → 1280px → 横向滚动条 → 右侧图标被推出屏幕 |

### 13.3 解药

**任何**设置以下属性的自定义类必须放进 `@layer base` 或 `@layer utilities`：

- `position` / `display` / `width` / `height`
- `font-family` / `font-size` / `font-weight` / `letter-spacing` / `line-height`
- `color` / `background` / `border` / `box-shadow`
- 任何 Tailwind utility 也会管的属性

**对于结构性辅助类**（仅给 `::before` / `::after` 提供定位上下文），用 `:where()` 把 specificity 降到 0，让任何 utility 都能干净覆盖：

```css
@layer utilities {
  :where(.aurora-rim, .aurora-rim-bottom) { position: relative; }
}
.aurora-rim::before { /* ::after / ::before 不会冲突，照常写 */ }
```

**对于视觉效果类**（如 `.grad-text` 必须让 `color: transparent` 生效），不用 `:where()`，但仍然要 wrap 进 `@layer utilities`。这样它和 Tailwind utilities 走同一套 specificity 规则——Tailwind 的 `text-red-500` 也能正常覆盖。

### 13.4 自检清单

新增 CSS 类时问自己：

1. 这个类有没有设置 `position`？→ 如果"是"，wrap 进 `@layer utilities` + `:where()`
2. 这个类有没有设置字体 / 颜色 / 间距 / 边框？→ wrap 进 `@layer utilities`（不一定要 `:where()`，看是否希望被 override）
3. 这个类有没有设置 default heading 样式？→ wrap 进 `@layer base`

宁可分层多余，也不可漏掉——cascade bug 永远是静默的、跨页面的，最难排查。

---

## 14. 文件位置

| 内容 | 路径 |
|------|------|
| 全局 CSS / token 定义 | `web/src/index.css` |
| Tailwind v4 主题 | `web/src/index.css` 顶部的 `@theme` 块 |
| 共享组件 | `web/src/components/{Card,MetricCard,Icon,...}.tsx` |
| Portal 组件 | `web/src/portal/components/` |
| Admin 布局 | `web/src/admin/components/MainLayout.tsx` |
| 字体加载 | `web/src/index.css:2-7`（Inter / JetBrains Mono / Noto Sans SC / Material Symbols） |
