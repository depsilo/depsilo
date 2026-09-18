# Depsilo Brand Assets

`docs/brand/` 是 Depsilo 品牌资产的唯一母版，来自设计方交付的
`depsilo-brand-kit`（kit 版本 `1.0.0-draft`）。Web、桌面应用、文档和发布物
都从这里同步，不要在下游维护另一套图形。

## 品牌概念

标记由**开放的仓库边界**与中心一枚**已缓存的依赖模块**组成：两片不对称的
蓝色仓壁围出开口，中间的绿色模块表示已经落进本地的依赖。仓壁的开口是标记的
识别特征，因此不要把两半收成封闭的正六边形。

- 正式名称：**Depsilo**（中文可作“依仓”，但不能替代正式字标）
- Descriptor：`Repository Cache Control`
- Tagline：`Dependencies closer. Builds faster.`
- Logo 本身不附带 tagline；需要 tagline 时使用 `lockups/*-tagline-*`

## 资产

| 文件 | 用途 |
| --- | --- |
| `mark/depsilo-mark-color.svg` | 首选标记，带蓝色渐变 |
| `mark/depsilo-mark-flat.svg` | 平涂标记，**UI 与代码场景使用这个** |
| `mark/depsilo-mark-monochrome-light.svg` | 单色白标记，用于深色底或单色印刷 |
| `mark/depsilo-mark-monochrome-dark.svg` | 单色墨色标记，用于浅色底或单色印刷 |
| `lockups/depsilo-horizontal-{light,dark}.svg` | 默认横排锁定（标记 + 字标） |
| `lockups/depsilo-horizontal-compact-{light,dark}.svg` | 紧凑横排锁定，窄版头使用 |
| `lockups/depsilo-horizontal-tagline-{light,dark}.svg` | 带 descriptor 与 tagline 的横排 |
| `lockups/depsilo-stacked-light.svg` | 堆叠锁定，README、启动页 |
| `wordmark/depsilo-wordmark-{light,dark}.svg` | 仅字标 |
| `depsilo-favicon.svg` | 浏览器图标（已按 16px 光学校正） |
| `depsilo-app-icon-{light,dark}.svg` | 应用图标：底色 + 标记 |
| `depsilo-og-image.svg`、`depsilo-github-social-preview.svg` | 分享图 |

文件名中的 light / dark 指**适用背景**，不是图形本身的明暗。同一场景要同时
提供两版时，无法识别主题就以 light 版本回退。

PNG 导出可按需从 SVG 重新生成；本目录只保留 SVG 母版，避免同一图形多份来源。
锁定版本的字标使用 `<text>` 引用 Inter，未转曲：发布环境需保证 Inter 可用，
或改用导出的 PNG。

## 色彩

| Token | 色值 | 角色 |
| --- | --- | --- |
| Electric Blue | `#3B82F6` | 主品牌色、速度、基础设施 |
| Deep Blue | `#2563EB` | 结构与深度；浅色主题的交互色 |
| Cache Green | `#22C55E` | 缓存命中、已存储依赖、效率 |
| Deep Green | `#16A34A` | 缓存模块的深度 |
| Ink | `#0F172A` | 字标、正文、深色底 |
| Slate | `#64748B` | 次级文案 |
| Surface | `#E5E7EB` | 边框与中性面 |
| Paper / Dark surface | `#FFFFFF` / `#0B1220` | 浅色画布 / 深色画布 |

标记只使用平涂与 kit 自带的两道蓝色渐变，不添加投影、发光或描边；状态色由
产品设计 token 管理，不能为了匹配标记而改变警告、错误或健康状态的语义。

## 使用约束

- 净空：标记四周至少保留 **0.25× 标记宽度**；紧凑场景中，相邻图标或文字不得
  进入标记内部的开口。
- 最小尺寸：单独标记 16px（推荐 24px）；横排锁定 140px 宽（推荐 180px）；
  带 descriptor/tagline 时 240px 宽。
- 不要：把两片仓壁闭合成正六边形；单独给缓存模块改色来表示某个随机 UI 状态；
  给正式标记加投影；拉伸、倾斜、旋转或加描边；在低对比的蓝/绿底上使用彩色版。
- 不要用标记代替状态图标：健康、命中、警告继续使用各自语义组件。
- SVG 必须自包含，不加载远程字体、滤镜或其他网络资源。

Web 端的落地位置：`web/public/favicon.svg`（浏览器图标）与
`web/src/components/app/logo.tsx`（应用与侧栏标记）；桌面端为
`assets/macos/icon.svg`。改母版时同步这三处。
