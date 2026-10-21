# shadcn 更新与活动趋势面积图评估

核对日期：2026-10-21。本文记录当前工作区和当天官方发布信息；这是升级评估，
不表示所列依赖已升级或通过兼容性验证。

## 结论

可以更新 shadcn 相关资源，但实现参考图中的完整面积图**不需要先升级依赖**。
仓库已经使用 Base UI、React 19、Tailwind 4 和 Recharts 3。官方当前
`base-nova/chart` 注册项仍指定 `recharts@3.8.0`，与本仓库锁定版本一致。
本轮宜复用现有 Recharts 完成图表，依赖维护另行逐项验证。
[官方 Chart 文档](https://ui.shadcn.com/docs/components/base/chart)、
[官方 base-nova Chart 注册项](https://ui.shadcn.com/r/styles/base-nova/chart.json)。

“升级 shadcn”需要分开看：CLI 是代码生成工具；本地 UI 组件是复制后由项目
维护的源码；Base UI、Recharts、Tailwind 才是有独立版本的底层依赖。
仅更新 CLI 不会自动改变已复制的界面。
[官方介绍](https://ui.shadcn.com/docs)、[CLI 文档](https://ui.shadcn.com/docs/cli)。

## 仓库现状与可用版本

当前版本来自 [package.json](../../web/package.json) 和
[package-lock.json](../../web/package-lock.json)，最新版本来自 npm 官方 registry
在核对当天的 `dist-tags.latest`，不包括 beta/canary。

| 项目 | 当前工作区 | 当天最新稳定版 | 本轮建议 |
| --- | --- | --- | --- |
| shadcn CLI | 未安装，也未锁定 | [4.22.1](https://registry.npmjs.org/shadcn/4.22.1) | 不为图表新增 CLI 依赖；以后用 CLI 时固定版本 |
| shadcn Tailwind 辅助样式 | 从 4.21.0 复制到本地 | [4.22.1 发布包](https://registry.npmjs.org/shadcn/-/shadcn-4.22.1.tgz) | 可独立同步很小的 reduced-motion 修正 |
| `@base-ui/react` | 1.8.0 | [1.9.0](https://registry.npmjs.org/@base-ui/react/1.9.0) | 可作为后续组件交互维护升级 |
| `recharts` | 3.8.0 | [3.10.1](https://registry.npmjs.org/recharts/3.10.1) | 当前足以实现面积图；升级单独验证 |
| `tailwindcss` / `@tailwindcss/vite` | 4.2.2 / 4.2.2 | [4.3.3](https://registry.npmjs.org/tailwindcss/4.3.3) / [4.3.3](https://registry.npmjs.org/@tailwindcss/vite/4.3.3) | 如升级，两者同步；检查全站构建和视觉 |
| `react` / `react-dom` | 19.2.8 / 19.2.8 | [19.3.1](https://registry.npmjs.org/react/19.3.1) / [19.3.1](https://registry.npmjs.org/react-dom/19.3.1) | 不并入这次图表外观修改 |

具体集成边界：

- [components.json](../../web/components.json) 为 `base-nova`、Vite/TSX、
  CSS variables、neutral 基色；没有需要迁移的旧 Tailwind 3 配置。
- [index.css](../../web/src/index.css) 已有 shadcn 语义 token、明暗主题桥接、
  本地字体及间距适配。组件目录在本次研究开始时没有 `ui/chart.tsx`，
  Dashboard 直接组合 Recharts。
- [shadcn-tailwind.css](../../web/src/styles/shadcn-tailwind.css) 的来源注释记录
  4.21.0，并说明历史上因 CLI 依赖审计问题选择只复制样式。
  该注释是历史原因，本次没有重新审计 CLI 全依赖树，不能据此断言 4.22.1
  当前仍有相同漏洞。
- 本地组件含产品适配：Button 的尺寸/危险色、Dialog 的短屏滚动、Input 的
  移动端字号、Select 的定位与字号、Table 的滚动区域所有权。
  `add --overwrite` 或重新初始化会危及这些改动，不能批量覆盖。

资料存在一处摘要滞后：[llms.txt](https://ui.shadcn.com/llms.txt) 顶部仍仅提
Radix primitives，而当前 Chart 页面和 `base-nova` 官方注册项均提供 Base UI
版本。判断本仓库适配性以具体组件文档及源码为准，无需迁移到 Radix。

## 参考图的实际实现

参考截图中的 `Total Visitors` 来自官方 `dashboard-01` 的
`components/chart-area-interactive.tsx`，Base UI/Nova 版本可直接读取
[官方 dashboard-01 注册项](https://ui.shadcn.com/r/styles/base-nova/dashboard-01.json)。
通用版本也可在 [Area Charts 示例](https://ui.shadcn.com/charts/area) 查看。

它使用一个完整的 `AreaChart` 绘图区、水平网格、底部时间轴、两条渐变面积曲线，
顶部是标题/说明与分段时间选择器；绘图区高 250px，窄屏切换为 Select。
这是布局和绘图配置，未依赖“新版 shadcn 专有图表引擎”。

对 Depsilo 的应用建议（设计判断）：

1. 恢复共享时间轴上的完整面积图，保留现有请求/流量/延迟/错误标签和周期
   控制范围。标题下可保留紧凑汇总，让绘图区占主要面积。
2. 采用淡水平网格和渐变填充，使用项目语义色及可读的 14px 提示。官方
   Chart Tooltip 的默认 `text-xs` 不宜照搬到本项目的 13px 根字号体系。
3. 不照搬示例 `stackId="a"`：服务请求/回源请求及交付/回源字节不是相互
   独立、可直接相加的分类。使用重叠且不堆叠的面积；错误率另用百分比轴。
4. 不为追求示例波形制造样本。零流量保留零线；缺少延迟/错误率计算样本
   保持空值和断线；请求少时仍是少量尖峰。曲线插值只能改变线形，不能改变
   每个时间桶的值，也不能把无请求区间补成有业务的波峰。
5. 官方示例为演示而使用固定数据、日期以及移动端自动切到 7 天；这些不是
   Depsilo 的产品需求，不应复制进真实统计逻辑。

## 可执行的升级路径

### 最小更新：本地辅助样式

只读下载并比较 `shadcn@4.22.1` 发布包后，排除本地来源注释，样式相对
4.21.0 只有 `shimmer` 工具内部的 reduced-motion 降级和相关兼容性注释。
可以保留 MIT 版权说明，更新本地样式及来源版本，不安装 CLI，不覆盖组件。
核对发布包 SHA-256：
`93c7018d30af36399f2ad886f046adc63f0f64c6865d9117d2599ccb5ab49226`。
[官方 npm 发布包](https://registry.npmjs.org/shadcn/-/shadcn-4.22.1.tgz)。

4.22.1 的 CLI 发布说明主要修复 `add --all` 和选择器只列出当前 style 可用
组件；这也不会改变已有图表。
[官方 4.22.1 发布说明](https://github.com/shadcn-ui/ui/releases/tag/shadcn%404.22.1)。

### 后续依赖维护：按影响面拆分

- Base UI 1.9.0 值得优先评估：包含 Select 列表/键盘状态、Popover 焦点、
  Dialog 触发器等修复，同时改变 `actionsRef` 与自动卸载的关联，升级后需
  回归选择器、提示、抽屉、对话框及焦点恢复。
  [官方 1.9.0 发布说明](https://github.com/mui/base-ui/releases/tag/v1.9.0)。
- Recharts 3.10.1 含 Tooltip 回退匹配等修复，但不是面积图所必需。其 React
  peer range 接受 19.x；这只是声明兼容，仍需检查空值、数值轴、键盘提示、
  隐藏面板尺寸和生产包。
  [官方发布说明](https://github.com/recharts/recharts/releases/tag/v3.10.1)、
  [官方包元数据](https://registry.npmjs.org/recharts/3.10.1)。
- Tailwind 4.3.3 同步升级 Vite 插件，尤其关注主题变量、任意值类名、生成
  CSS 与 CJK 字体变化；React/DOM 作为另一组维护。不要把全站依赖更新与
  单个图表的视觉验收混成一次难以归因的变更。
  [官方 Tailwind 4.3.3 发布说明](https://github.com/tailwindlabs/tailwindcss/releases/tag/v4.3.3)。

如需同步组件源码，先固定 CLI 版本或保存官方注册项，检查差异，再手工合并
本地适配。不要以 `init --force`、`--reinstall` 或全量覆盖充当升级步骤。
[CLI 的具体修改行为](https://ui.shadcn.com/docs/cli)。

验证按 [仓库测试约定](../development/testing.md)：局部图表跑相关 Playwright、
类型/lint/i18n/build 和 `make check`；共享组件升级扩展为 `make verify`；
依赖版本变更追加 `make security` 及生产构建检查。至少覆盖深浅主题、中英文、
320px 窄屏、键盘操作、零值/缺样本和统计失败。

## 本次研究的验证边界

已完成：阅读本地配置、锁文件、定制组件和主题；访问官方文档、registry
JSON、npm 包元数据及发布说明；只读下载发布包并比较辅助样式。

未完成：未安装/运行新 CLI，未变更依赖或锁文件，未对上述候选版本做安装、
构建、全站回归或漏洞审计。因此本文给出可升级路径，不宣称候选升级已验证。
