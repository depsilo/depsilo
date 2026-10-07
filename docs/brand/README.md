# Depsilo Brand Assets

`docs/brand/` 是 Depsilo 品牌资产的唯一母版。Web、桌面应用、文档和
发布物应从这里同步，不要在下游维护另一套图形。上游品牌包位于
`depsilo-brand-kit/`（含 PNG 导出、社交图和可选主题 token）。

## 品牌标记

正式标记是**开放仓界 + 缓存模块**：两片蓝色仓壁围出一个未闭合的六边形
轮廓，中央是绿色的缓存依赖模块。开口的仓界表示开放、可审计的仓库边界，
绿色模块表示已缓存、可复用的依赖；合在一起就是 Depsilo 的“依赖入仓并可
复用”。标记不使用首字母、数据库圆柱、包裹方块、盾牌或闪电等常见母题，
也不要把两片仓壁闭合成实心六边形。

正式字标始终写作 **Depsilo**，描述语为 **Repository Cache Control**，
品牌短句为 **Dependencies closer. Builds faster.**。中文名“依仓”可以作为
本地化说明出现，但不能替代正式字标。标记本身不附带 tagline。

## 资产

| 文件 | 背景 | 用途 |
| --- | --- | --- |
| `icon-light.svg` | 浅色 | 浅色 UI、独立图标母版 |
| `icon-dark.svg` | 深色 | 深色 UI、深色发布物 |
| `logo-horizontal-light.svg` | 浅色 | 网站页眉、横向文档版头 |
| `logo-horizontal-dark.svg` | 深色 | 深色页眉和横向版头 |
| `logo-stacked-light.svg` | 浅色 | README、启动页、关于页面 |
| `logo-stacked-dark.svg` | 深色 | 深色 README 和展示场景 |
| `web/src/assets/favicon.svg` | 自动 | 光学校正版，随系统主题切换外壳蓝；构建时由 Vite 生成内容哈希 URL |

文件名描述其适用的背景主题，而不是图形自身的明暗。标记本身是彩色的，
浅色与深色场景都使用同一套蓝 + 绿；深色版本只调整字标与描述语的前景
色，以及 favicon 的外壳蓝明度。

字标与描述语均为**已转曲轮廓**（源于 Inter，见 `web/node_modules/`
的 `@fontsource-variable/inter`），发布环境不依赖字体回退。重新生成时
必须保持这一点，不要把 `<text>` 直接写进正式资产。

## 色彩

| Token | 色值 | 角色 |
| --- | --- | --- |
| Electric Blue | `#3B82F6` | 主品牌色 / 速度 / 基础设施 |
| Deep Blue | `#2563EB` | 结构与纵深 |
| Cache Green | `#22C55E` | 缓存命中 / 已存依赖 |
| Deep Green | `#16A34A` | 缓存模块纵深 |
| Ink | `#0F172A` | 字标、排版、深色底 |
| Slate | `#64748B` | 次级说明文字 |
| Surface | `#E5E7EB` | 边框与中性面 |

浅色背景字标用 Ink，深色背景字标用白色、描述语用 `#CBD5E1` / `#94A3B8`。
状态色仍由产品设计 token 管理，不能为了匹配品牌色而改变警告、错误或健康
状态的语义。

## 使用约束

- 净空：标记四周至少保留 0.25× 标记宽度；紧凑 UI 中不要让相邻图标或文字
  进入标记内部的开口区域。
- 最小尺寸：符号 16px（推荐 24px）；横向字标 140px 宽（推荐 180px）；
  带描述语时不低于 240px。
- 不要闭合成实心六边形、不要单独给缓存模块换色表示状态、不要加阴影、
  不要拉伸/旋转/描边，也不要把彩色标记放在低对比度的蓝或绿底上。
- 标记在 16、24、28、32px 下必须清晰；UI 中图标与文字组合时使用正式
  大小写 `Depsilo`。
- 不把 Logo 当作状态图标；健康、命中和警告继续使用各自语义组件。
- SVG 必须自包含，不能加载远程字体、滤镜或其他网络资源。
- 修改母版时，同步 `web/src/components/Logo.tsx`、`web/src/assets/favicon.svg`
  和 `assets/macos/icon.svg`（Linux 安装脚本复用该文件），并检查浅色、
  深色及最小尺寸。favicon 经 Vite 资源管线输出为内容哈希 URL，改版后
  浏览器会立即拉取新图标，无需手动清缓存或改文件名。

## 与产品主题的关系

品牌包里的 `tokens/brand.css` 与 `tokens/shadcn-theme.css` 是可选的起步
映射，产品界面可以独立演进；本轮只同步标记、字标、favicon、应用图标与
品牌文案，不改动产品语义 token 与页面布局。
