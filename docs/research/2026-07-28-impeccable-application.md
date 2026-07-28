# Impeccable 研究与 Depsilo 应用建议 — 2026-07-28

> 研究记录，冻结于 2026-07-28。只使用 `pbakaus/impeccable` 官方仓库、
> 仓库源码和仓库指向的第一方文档。基线为 `main`
> [`1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d`](https://github.com/pbakaus/impeccable/commit/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d)，
> 其 `package.json` 版本为 3.4.0、要求 Node.js 22.12.0 以上并采用
> Apache-2.0 许可证
> ([package.json](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/package.json#L1-L40))。

## 结论

Impeccable 适合 Depsilo，但应把它当成一套“让 AI 按项目上下文设计、
分层评审并做机械检查”的工作流，而不是新的组件库或必须服从的视觉风格。
它提供一个 `impeccable` 技能、23 个面向意图的命令、浏览器迭代工具和
60 条确定性检测规则；CLI 检测不需要 LLM 或 API key
([README](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/README.md#L1-L16))。

Depsilo 最有价值的采用点是：

1. 把已存在于 `CONTEXT.md`、`DESIGN.md` 和源码中的产品/设计事实整理成
   Impeccable 能稳定读取的分层上下文。
2. 将 Admin、Setup 和 Monitor 明确按 **Operate** 模式处理，把熟悉性、
   扫读效率、完整状态和一致组件置于视觉炫技之前。
3. 将确定性检测加入 Codex UI 编辑反馈和 CI，但只把它当缺陷信号；
   UX 判断仍由 `audit`、`critique` 和浏览器验证承担。
4. 保留 Depsilo 的 Instrument 设计系统、Inter 字体、中英双语、40px
   控件和现有 Playwright 矩阵；对与明确项目决策冲突的通用规则建立
   可审查的窄范围例外。

不建议立即用 `bolder`、`overdrive` 或新视觉世界流程重做 Admin。
Impeccable 自己对 Operate 界面的要求也是“工具消失在任务中”：允许单一
sans 字体、标准侧栏/标签页、较高信息密度，并强调一致性胜过惊喜
([Operate 指南](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/operate.md#L1-L16),
[Operate 权限](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/operate.md#L54-L61))。

## 它的核心模型

### 1. 四层上下文，而不是一次性提示词

- `PRODUCT.md` 保存长期稳定的用户、任务、定位、能力、限制、证据和品牌
  事实；`init` 不负责发明视觉世界，也不会写 `DESIGN.md`
  ([init 源码说明](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/init.md#L1-L15),
  [PRODUCT.md schema](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/init.md#L56-L99))。
- `DESIGN.md` 保存机器可读 token 和人可读设计规则；官方工作流要求先从
  现有 CSS、组件、token 和渲染结果扫描，不得静默覆盖已有文件
  ([document 规范](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/document.md#L1-L7),
  [已有文件与 scan mode](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/document.md#L64-L92))。
- 页面级 brief 只保存该页面的访问者模式、任务、内容、约束和已选方向；
  全局产品事实与全局 token 不应复制进去
  ([new-work](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/new-work.md#L1-L12))。
- `.impeccable/` 保存配置、检测例外、critique 记录和 live 运行状态；官方
  明确区分应提交的共享产物和应忽略的本地临时文件
  ([README](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/README.md#L297-L335))。

这套分层对 Depsilo 很合适：当前 [CONTEXT.md](../../CONTEXT.md) 已有
Operator、Buyer、End User 和产品术语；[DESIGN.md](../../DESIGN.md) 已有
Instrument token、Admin/Portal 边界、响应式和查询状态契约。迁移应以这两份
现有事实为输入，不应让工具重新发明它们。

### 2. 先判断页面成功方式

技能把页面分为四种模式：

- **Persuade**：让访客作出决定并行动，适用于营销/落地页。
- **Operate**：让用户完成任务，适用于应用、后台、设置和工具。
- **Read**：让读者理解内容，适用于文档、文章和指南。
- **Experience**：让作品本身主导，适用于作品集和展览。

模式按具体页面而不是整个产品选择；同一个工具的落地页仍可属于
Persuade，文档页仍属于 Read
([技能模式](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/SKILL.src.md#L34-L47))。

对 Depsilo 的建议映射：

| Depsilo 表面 | 模式 | 设计成功标准 |
| --- | --- | --- |
| `/admin/*` | Operate | Operator 能快速判断状态、完成配置并从错误恢复 |
| 首次安装 Setup | Operate | 用最少歧义完成端口、存储、生态和上游设置 |
| `/monitor` | Operate | 快速发现健康、降级、延迟和流量变化 |
| `/` Quick Start | Read | 在首屏理解并复制正确的客户端配置 |
| 后续营销官网（若独立建设） | Persuade | Buyer 理解差异、证据和下一步；不得把该模式反向套到 Admin |

### 3. Brief 优先，精修与重设计必须分开

官方规则规定：明确 brief 高于通用偏好；精修保留当前身份、行为和范围外
内容，重设计才替换视觉世界，而且两者不能混成“悄悄换风格”
([核心规则](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/SKILL.src.md#L23-L32))。

因此 Depsilo 后续的默认命令应是 `audit`、`critique`、`layout`、
`harden`、`adapt` 和 `polish`。除非用户明确要求更换 Instrument 身份，
否则不应让 `new-work`、`bolder` 或 `overdrive` 改写品牌、导航模型或
控制平面语义。

## 可安装能力和命令

Impeccable 安装为一个可调用技能，命令统一通过
`/impeccable <command> <target>` 路由；Codex 中则通过技能面板或
`$impeccable` 调用，而不是 `/prompts:` 命令
([命令入口](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/README.md#L20-L38),
[Codex 用法](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/README.md#L273-L295))。

| 类别 | 命令 | 用途 |
| --- | --- | --- |
| 建立上下文/构建 | `init`, `document`, `extract`, `shape`, `craft` | 产品事实、现有设计系统、组件/token 抽取、动手前塑形 |
| 评估 | `critique`, `audit` | 前者评 UX/层级/认知，后者评 a11y/性能/主题/响应式/实现完整性 |
| 精修 | `polish`, `bolder`, `quieter`, `distill`, `harden`, `onboard` | 收尾、调节表达强度、减法、异常/i18n/边界、首次使用 |
| 增强 | `animate`, `colorize`, `typeset`, `layout`, `delight`, `overdrive` | 动效、颜色、排版、布局、个性和高强度视觉效果 |
| 修复/迭代 | `clarify`, `adapt`, `optimize`, `live` | 文案、设备适配、性能和浏览器可视化迭代 |

完整的 23 个命令和官方用途见
[README 命令表](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/README.md#L36-L66)；
技能会为显式命令加载对应 playbook，没有参数时只给上下文相关建议，不会
擅自执行
([路由规则](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/SKILL.src.md#L81-L85))。

## 检测器和 Hook

### 能做什么

独立 CLI 可扫描目录、单个 HTML 或 URL，也能输出 JSON；60 条规则同时
覆盖 AI 模板化痕迹和常规质量问题
([CLI](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/README.md#L365-L380))。
Codex Hook 在 UI 文件编辑后运行：即时层处理较明确的溢出、对比度、可读性、
渐变文本、发光和设计系统漂移，Stop 深层再处理文案、字体、色板和布局节奏，
避免每次编辑都产生噪声
([Hook 两层规则](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/hooks.md#L1-L15))。

### 不能证明什么

检测器干净不等于设计优秀。官方 `audit` 还要求分别检查可访问性、性能、
主题、响应式和实现完整性，并在上下文中验证检测结果和假阳性
([audit 五维模型](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/audit.md#L1-L61))。
所以 Depsilo 现有的 type-check、Playwright、axe、i18n 和包体预算仍是
发布门槛，Impeccable 不替代它们。

### Depsilo 当前只读基线

在 2026-07-28 从仓库根目录执行：

```bash
npx --yes impeccable@3.4.0 detect --json web/src
```

结果仅产生 2 条 `overused-font` warning，均位于
[`web/src/index.css`](../../web/src/index.css)，对应既有 `Inter` /
`Inter Tight` 选择；未产生其他确定性 finding。这个结果只说明当前
源码扫描命中了什么，不代表视觉、交互或运行态审计已经通过。

该字体告警对 Admin 属于有依据的例外：Impeccable 的 Operate 指南明确
允许一个经过调校的 sans 家族和熟悉的 sans 默认值
([Operate 字体](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/operate.md#L11-L16),
[Operate permissions](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/operate.md#L54-L61))。
官方支持按具体 value 添加带原因的共享例外，并要求优先使用最窄例外而不是
关闭整个规则
([例外策略](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/hooks.md#L51-L68))。

## 值得直接吸收的规则

这些规则与 Depsilo 现有方向一致，应成为 UI 变更的默认检查项：

- 正文/placeholder 对比度至少 4.5:1，大字至少 3:1；有色表面上的次要
  文字应从该色相或前景色派生，而不是随手使用灰色
  ([craft floor](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/craft-floor.md#L5-L16))。
- 相关内容紧密、不同组之间留出更大间隔；标题上方空间应大于下方空间；
  长文正文控制在约 65–75ch，并用真实中英文内容验证溢出
  ([craft floor](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/craft-floor.md#L9-L16))。
- 每个交互组件覆盖 default、hover、focus、active、disabled、loading、
  error；空状态应帮助下一步，而不是只写“没有数据”
  ([Operate components](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/operate.md#L30-L37))。
- 产品动效保持约 150–250ms，并只表达状态变化，不做编排式页面入场
  ([Operate motion](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/operate.md#L39-L43))。
- 使用 proximity、type 和 divider 表达层级，避免把每一组都包成卡片；
  不使用装饰性渐变字、彩色发光、技术感伪装的等宽字体和无意义 pulse
  ([craft floor](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/craft-floor.md#L18-L44))。
- `audit` 的报告必须区分 P0–P3、解释用户影响、验证假阳性并保留正向发现，
  不能把所有问题都标成最高优先级
  ([audit 输出契约](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/audit.md#L63-L135))。

## 建议的 Depsilo 落地顺序

### 阶段 1：项目级、可回滚安装

使用固定版本和 Codex 项目作用域，先查看生成的 diff：

```bash
npx impeccable@3.4.0 install --providers=codex --scope=project
```

官方推荐 CLI installer；Codex 项目安装会放置技能和
`.codex/hooks.json`，安装或更新后还必须由用户在 `/hooks` 中批准 Hook
([安装参数](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/README.md#L96-L114),
[Codex 文件布局](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/README.md#L200-L213),
[Hook 批准](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/README.md#L337-L354))。

固定版本是本研究对可重复构建的建议，不是 Impeccable 的强制要求。更新时
先审查上游 changelog/diff，再显式升级，不在 CI 中浮动执行最新版本。

### 阶段 2：建立兼容上下文，不重写现有设计

1. 运行 `$impeccable init`，让它从 `CONTEXT.md`、README、功能文档和
   源码整理 `PRODUCT.md`；只补真正缺失的问题，并由用户确认推断。
   官方要求 init 先读仓库、只询问材料没有回答的关键空白
   ([init 探索与访谈](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/init.md#L17-L41))。
2. 对现有 `DESIGN.md` 选择“merge/refresh”，不得 overwrite。
   将当前 Instrument token 和契约保留下来，同时补充它期望的 YAML
   frontmatter；不要把现有文档降级成通用 Material 命名。
3. 为 Admin、Setup、Monitor、Quick Start 分别记录模式和任务边界，
   防止 Portal 的表达方式渗入 Admin，也防止后台密度规则污染 Quick Start。

### 阶段 3：校准检测器

在用户确认 Inter/Inter Tight 继续作为既有设计系统后，分别添加 value 级
共享例外，不要禁用整个 `overused-font` 规则。官方 CLI 已提供
`ignores add-value ... --reason`，且手动扫描与 Hook 读取同一套配置
([CLI 示例和配置](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/README.md#L365-L382))。

然后再启用 Codex Hook。先保持默认的“即时规则 + Stop 深层规则”，不要一开始
就设置 `hook.perEditRules: "all"`；默认分层正是为了减少每次编辑的设计噪声
([Hook 两层规则](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/hooks.md#L1-L11))。

### 阶段 4：从 Admin 小范围试点

建议顺序：

1. `$impeccable audit web/src/admin`：只读技术审计，建立 P0–P3 backlog。
2. `$impeccable critique web/src/admin/pages/Dashboard.tsx`：独立评价信息
   层级、认知负担、产品特异性和 Operator 任务。
3. 逐项使用 `layout`、`harden`、`adapt` 或 `clarify`，一次只改一个有
   明确证据的问题。
4. `$impeccable polish <target>`：在完整状态、桌面/移动和中英内容上收尾。
5. 继续执行 Depsilo 自己的 type-check、lint、build、Playwright、axe、
   i18n audit 和 bundle budget；Impeccable 只是额外门禁。

`audit` 官方定义为只报告、不修复，且明确区别于设计 critique
([audit 定义](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/audit.md#L1-L9))；
这能维持“先证据、后修改”的节奏，避免工具在一次命令里扩大范围。

## 不应照搬的部分

1. **不要因为 README 把 Inter 列为常见 AI 痕迹就更换字体。** 通用反模式
   列表确实点名 Inter
   ([README](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/README.md#L82-L90))，
   但同一项目的 Operate 指南明确允许单一、熟悉的 sans。Depsilo 的字体已
   与中英文 fallback、数据字体和截图矩阵绑定，除非有真实可读性或品牌证据，
   换字体只会制造回归。
2. **不要把 Admin 当 Persuade 页面。** Admin 需要标准导航、密度和可预测
   控件；官方 Operate 指南明确反对无目的奇特控件、装饰动效和 display font
   UI label
   ([Operate](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/operate.md#L5-L9),
   [约束](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/operate.md#L45-L52))。
3. **不要把 detector 当审美裁判或完整 a11y 测试。** 官方 audit 要求人工
   验证每个 finding、识别假阳性，并另外检查键盘、ARIA、主题、性能和响应式
   ([audit](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/audit.md#L11-L61))。
4. **不要静默覆盖 `DESIGN.md`。** 官方 document 流程明确要求已有文件由
   用户选择 refresh、overwrite 或 merge
   ([document](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/reference/document.md#L64-L78))。
5. **不要提交全部 `.impeccable/` 运行产物。** screenshot、session、cache
   和本地配置是临时文件；共享 config、design spec 和 critique 记录才是
   官方建议跟踪的产物
   ([README](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/README.md#L297-L335))。
6. **不要无限截图和微调。** 技能要求有界验证：一次桌面/移动批量检查、
   一次集中修复、最多再确认一轮，避免开放式自我 QA
   ([技能核心原则](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/skill/SKILL.src.md#L15-L21))。
7. **不要在安装脚本上浮动 latest。** 官方包需要 Node.js 22.12.0 以上，
   并会安装技能与项目 Hook
   ([package.json](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/package.json#L20-L40),
   [installer](https://github.com/pbakaus/impeccable/blob/1cf7d7ab0f1ac0bb3319fd20be389a3009f4037d/README.md#L96-L114))；
   对 Depsilo 应固定版本、审查生成 diff，并让 Hook 信任保持显式。

## 验收标准

完成集成不以“插件装上了”为准，而以这些结果为准：

- `PRODUCT.md` 与现有 `CONTEXT.md` 没有冲突，用户、术语、真实能力和限制均
  可追溯。
- `DESIGN.md` 保留 Instrument 的项目特异性，同时具备工具可读 token；
  `web/src/index.css` 仍是实际 source of truth。
- Admin、Setup、Monitor、Quick Start 的模式与任务边界明确。
- Inter 例外按 value 记录原因，其他 `overused-font` 值仍可被检测。
- Hook 不覆盖现有 Codex 配置，并已由用户显式批准。
- Impeccable finding、Playwright/axe 失败和人工 UX 评价分别记录，不相互
  冒充。
- 所有产品代码修改仍通过 Depsilo 当前前端验证矩阵。

## 后续复查点

- 升级 Impeccable 时重新核对命令数、detector 规则数、Node.js 最低版本、
  Codex Hook 事件和 `PRODUCT.md` / `DESIGN.md` schema。
- 先观察 2–3 次 Admin 变更中的 Hook 噪声，再决定是否纳入 CI；不要根据
  一次静态源码扫描就设为阻断。
- 若未来建设独立营销站，应创建单独的 Persuade surface brief，而不是修改
  Admin 的 Operate 原则。
