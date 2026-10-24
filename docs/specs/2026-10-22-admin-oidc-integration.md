# Admin 第三方登录与 Casdoor 接入计划

日期：2026-10-22。代码基线：`b1e5286`。

## 本轮实现边界（2026-10-22 更新）

在隔离 Casdoor 准备完成后，用户授权实现，并补充了账户绑定与解绑需求。
当前代码按以下收敛边界实现；本节记录范围，**不表示完整验证或部署已经完成**：

- 配置一个 OIDC 提供方，使用文件或环境变量配置；账户安全页只读展示运行
  配置。没有浏览器密钥编辑器，也没有独立的“检查连接 / 诊断试登录”流程。
- 保留本地密码登录；已有本地账户可验证密码后显式绑定或解绑 OIDC 身份。
  不按同名或同邮箱自动合并，不移除本地密码。外部管理账户不能解绑其必要
  身份、添加本地密码或把权限来源转换成本地。
- 提供方组映射到现有 `admin` / `readonly`；外部会话默认 30 分钟绝对期限，
  可配置 5 分钟至 1 小时，刷新不延长。长效用户/API Token/编译缓存凭据和
  Cascade 配置管理保留本地管理员认证要求。
- schema v12 分离账户权限来源、外部身份与可撤销会话，保留既有本地凭据。
  仍未实现多提供方、账户合并、SCIM、即时远端撤权或项目级权限。

当前操作契约见 [OIDC 指南](../oidc.md)、
[Admin 控制面](../admin-control-plane.md) 和
[ADR-0007](../adr/0007-operator-oidc-identities.md)。本文以下保留最初方案及其
基线分析作为设计记录；其中“首版不提供绑定/解绑”、浏览器配置编辑、诊断
试登录等条目已由上述实际范围替代，不能作为已交付能力说明。隔离提供方
测试与 Depsilo 端到端验证是不同证据，验证结果以实际测试记录为准。

提供方事实与来源见 [Casdoor 调研](../research/2026-10-22-casdoor-oidc.md)。

## 目标与优先级

下一阶段优先交付一个完整的管理后台登录闭环：通过 Casdoor 登录，按明确的
外部组映射获得 Depsilo 权限，在用户被本地禁用、外部授权过期或登录服务
异常时给出准确行为，并保留管理员恢复能力。

首版采用通用 OpenID Connect（OIDC），Casdoor 是首个经过实测的提供方。
协议实现不绑定 Casdoor SDK；其他 OIDC 提供方需要各自通过兼容测试后才能
宣称支持。SSO 建议作为无额外许可证要求的管理能力，沿用当前用户管理的
授权边界，不顺带改变多项目/SBOM 的 Pro 规则。

| 优先级 | 交付 | 完成条件 |
| --- | --- | --- |
| P0，当前准备 | 隔离 Casdoor、真实 claims 样本、接入设计 | 固定镜像，独立测试数据；明确实际 issuer、PKCE、UserInfo 和组格式 |
| P1 | 身份与会话后端 | 正确的 code flow、身份绑定、准入/两角色映射、有限期会话、本地恢复；协议和迁移测试通过 |
| P2 | Admin 登录与管理 UI | 配置、试登录、正常登录、失败恢复、用户身份来源与权限来源连贯可用 |
| P3 | 真实端到端验收与调试部署 | 真实 Casdoor 浏览器登录与权限用例通过；完整门禁、备份与 mcs 升级完成 |
| P4，后续 | 企业目录撤权与细粒度权限评估 | 先确定撤权时效，再选择事件/在线校验/同步；细粒度授权另立模型和验收 |

不把 SCIM、LDAP、SAML、外部 Casbin 决策、项目隔离或多租户混入首版。
Casdoor 可管理它自己的登录上游，但 Depsilo 首版只消费约定的 OIDC 身份。

## 现状与需要修订的决策

| 当前事实 | 实现依据 | 对方案的影响 |
| --- | --- | --- |
| 本地密码登录后返回 Depsilo JWT | `internal/api/auth.go` | 后端验证 IdP 身份后仍签发 Depsilo 会话，不接受任意 Casdoor token 作为 Admin 凭据 |
| 只有全实例 `admin` / `readonly` | `internal/middleware/principal.go` | 首版做角色映射，不宣称拥有项目级 RBAC |
| 每次认证检查本地 enabled/role/credential_version | `internal/middleware/auth.go` | 复用即时本地撤销，不把 IdP claims 分散到每个业务 handler |
| 默认 JWT TTL 为 168 小时，refresh 能再次签发完整 TTL | `internal/config/loader.go`、`internal/api/auth.go` | 外部身份不能直接沿用无限续期，否则外部撤权无法生效 |
| logout 仅通知浏览器丢弃 JWT | `internal/api/auth.go` | 新外部会话需支持服务端撤销，不能假称已经完成全局单点退出 |
| API token 依赖本地 owner，不依赖 JWT 版本 | `internal/middleware/auth.go` | 外部身份的长期机器凭据另设限制，避免绕过会话期限 |
| Setup/最后管理员保护仅按 admin+enabled 计数 | `internal/api/auth.go`、`internal/api/admin/user.go` | 必须保留至少一个可用的本地密码管理员 |
| 用户模型没有外部身份字段，当前 schema v11 | `internal/db/models.go`、`internal/db/repository.go` | 新增编号迁移，保留现有用户 ID、密码、token 和归属关系 |
| Settings 草稿写入 sessionStorage | `web/src/admin/pages/Settings.tsx` | Client Secret 不进入该草稿，也不回显到配置 GET 接口 |
| 包下载匿名，项目 token 仅作归因 | `internal/middleware/project.go`、`docs/threat-model.md` | Admin SSO 不等于包下载鉴权，也不会产生项目缓存隔离 |

当前 [ADR-0005](../adr/0005-enforcement-first-until-1-0.md) 明确排除内置
SSO/LDAP/SAML/RBAC；`CONTEXT.md` 和威胁模型延续此决定。
用户此次需求是新的方向。P1 先新增 ADR，**部分替代**相关 SSO 非目标，限定
为标准 OIDC 客户端和现有角色映射，保持其他非目标。同步更新 PRODUCT、
CONTEXT、Admin authority、兼容性和威胁模型，不重写历史 ADR 来掩盖变更。

## 用户流程

1. Setup 继续创建本地管理员。管理员在“实例管理 → 设置 → 认证与安全”
   配置一个 OIDC 提供方，不在首次初始化中强制依赖 Casdoor。
2. 配置页显示启用状态、名称、Issuer、Client ID、密钥是否已配置、回调地址、
   组映射、会话期限、配置来源与重启状态。回调地址预览使用当前
   `window.location.origin`，与后台配置的固定回调地址不符时提示修正。
3. “检查连接”验证 discovery/JWKS 可用性；“测试登录”走真实浏览器授权，
   显示身份和映射结果。两者不能混称“登录已验证”。试登录结果不能自动
   覆盖当前管理员身份，也不凭一次连接检查就赋予访问权限。诊断事务不
   创建用户/普通会话、不更新真实角色或撤销其他会话；结果仅返回给仍有
   身份配置权限的发起者，并绑定配置版本，处理取消、超时与多标签页。
4. 登录页提供“使用 Casdoor 登录”及本地登录入口，不自动跳走而掩盖恢复
   入口。提供方列表读取失败可重试，不能当作“未配置”。
5. 浏览器跳转 Casdoor 完成其自身认证。后端完成验签、UserInfo 校验、准入
   和角色计算，创建或找到独立的外部账户，再返回原来的安全 Admin 路径。
6. 用户页展示“本地/Casdoor”“权限由身份提供方管理”“最近成功同步时间”。
   外部账户无本地密码重置；本地可禁用，禁用不会被下一次登录自动清除。
7. “退出 Depsilo”结束本地会话。只有实现并实测提供方注销后，才增加准确
   命名的提供方退出操作；不承诺退出用户在其他应用中的会话。
8. 会话到期显示明确的重新登录原因并保留安全深链接；重新登录后回到原页，
   不自动重放待提交写操作。沿用现有退出/认证失效清除非秘密草稿的行为，
   提示未提交修改需重新填写；密钥从不加入草稿恢复。

## 身份、准入与权限规则

- 身份唯一键为经验证的 `(issuer, sub)`，关联其专属的持久化 User 记录，
  既有本地账户保持独立。账户来源不可由请求参数任意改写。显示名、用户名
  和邮箱是属性，不自动合并本地账户。首版不提供账户绑定/解绑功能。
- 外部用户使用独立、不碰撞的内部用户名；来自提供方的名称只作展示。
  同名本地管理员必须保持原身份、密码和权限。
- 首版只启用一个提供方，Casdoor 使用专用单组织应用和稳定的 UUID 用户 ID。
  不兼容的 subject 重用规则应修正提供方配置，不能放宽身份校验。
- 只有管理员配置的精确组标识可映射。建议示例：

  | 外部组（示例，不是内置授权） | Depsilo 结果 |
  | --- | --- |
  | `<org>/depsilo-admins` | `admin` |
  | `<org>/depsilo-readers` | `readonly` |
  | 未命中、字段缺失、类型错误 | 拒绝进入 Admin |

- 同时命中上述两组时，明确管理员映射优先；若将来引入拒绝规则，拒绝优先。
  不把 Casdoor 的组织管理员或 `IsAdmin` 自动映射为 Depsilo 管理员。
- Casdoor 首个支持样例采用 `JWT-Standard`，从 `profile` UserInfo 读取
  字符串数组形式的组，确认 UserInfo `sub` 与已验证 ID token 完全一致。
  自定义角色对象/claim 不做隐式类型转换。
- 每次成功第三方认证重新计算授权；角色改变与 credential_version 递增
  在同一事务内完成，失去准入时撤销关联会话。管理员的本地禁用与 IdP
  授权状态是不同字段/事实，拒绝登录不能意外改动另一个本地账户。
- 外部角色首版仅由提供方管理，本地只提供禁用，不提供下次登录会被覆盖
  的本地降权控件。外部身份的删除不等同永久封禁；首版以保留身份绑定的
  禁用操作阻止其再次登录，避免删除后 JIT 自动重建绕过管理员决定。
- 保留当前全实例只读含义：readonly 能读取全部现有 Admin GET/导出接口，
  并非“只能看自己项目”。前端继续消费 `/auth/me` 的服务端能力。
- 至少保留一个启用且具有有效本地密码的管理员；外部管理员不计入这个
  恢复条件。用户删除/降权、Setup 恢复和配置变更都需使用同一个不变量。

## 会话与撤权的首版契约

建议外部会话默认绝对有效期 **30 分钟**，独立于本地会话的既有 TTL，设置页
准确显示生效值。数值是拟议默认值，不是当前实现。

| 事件 | 首版预期行为 |
| --- | --- |
| 本地禁用或已同步的外部降权 | 在本地事务提交后的认证检查拒绝旧会话；不声称能取消已执行的操作 |
| 用户退出 Depsilo | 服务端撤销当前外部 session，旧 JWT 即使尚未到期也不能重用 |
| 普通 `/auth/refresh` | 可以更新短 JWT，但不得越过外部 session 的绝对到期时间 |
| 外部 session 到期 | 要求重新走 code flow，并重新读取授权信息；已有 IdP SSO 是否免输密码由提供方决定 |
| Casdoor 改组/禁用 | 下次真实认证时重新判定；现有本地会话不会收到 OIDC 自动推送，最多保留当前授权快照到其绝对期限 |
| IdP 宕机 | 新外部认证失败，既有会话不延长期限；本地管理员仍可登录 |
| 提供方停用或安全配置变更生效 | 使关联外部会话失效，重新认证；配置仍遵循保存值/运行值/重启语义 |

不能将上述有限期保证宣传为“企业侧立即撤权”。真实 Casdoor 测试必须确认
被禁用账户无法通过新的 code flow 再进入。若要求秒级停用，需要 P4 的
可靠事件/在线校验及失联策略；back-channel logout 本身也不等于全部目录
改组/停用事件都能到达。

首版不允许外部账户获得永久本地密码或签发 Admin API Token。仅“本地身份
来源 + 原有写权限”可创建/重置本地账户、修改本地管理员、发放管理机器
凭据；现有本地管理员 API Token 仍可执行其原先允许的操作。前后端通过
明确的 `can_manage_credentials` 能力表达该限制，不能只隐藏按钮。
提供方/Issuer、Client Secret、准入映射、会话安全配置与配置试登录也需要
这一能力，避免外部管理员改变身份权威后继续获得访问。
编译缓存/级联的持久机器凭据创建同样由本地管理员负责；项目 token 只是
归因，可以保持其原契约。现有机器凭据不因升级意外失效。

因此外部 admin 可执行日常业务运维，但不具有本地身份和持久凭据管理能力；
这一差异需在 UI/文档和权限测试中明确，不宣传为完全相同的管理员权限。
外部 admin 对缓存、上游和规则的持久变更仍是合法操作，会话到期不会回滚
这些业务配置；不能声称外部撤权会自动收回其产生的全部影响。

## 实现位置与安全契约

### OIDC 模块

建议新增 `internal/identity/`，集中拥有登录事务、提供方验证、身份绑定、
角色计算和外部会话。对 HTTP 层只暴露开始登录、完成认证、检查/撤销会话
等小接口；通过模块接口测试完整行为。依赖由 `internal/server` 注入，不在
handler 中临时组装独立客户端或重复授权规则。

使用维护中的 [go-oidc](https://github.com/coreos/go-oidc) 和
[golang.org/x/oauth2](https://pkg.go.dev/golang.org/x/oauth2)；实施时选定并
锁定版本、审核新增传递依赖，运行依赖安全检查。OAuth 客户端提供 S256 PKCE
工具；具体 state/nonce、会话绑定与授权仍由我们的模块实现。

- Authorization Code + S256 PKCE，随机 state/nonce，短期一次性事务绑定
  发起浏览器，防止跨浏览器回调、重放和登录 CSRF。
- 严格验证签名、允许算法、issuer、audience/azp、时效及 nonce；UserInfo
  失败或 sub 不符不得创建会话。符合 [OAuth 安全最佳实践](https://www.rfc-editor.org/rfc/rfc9700.html)。
- 使用固定已注册的后端 callback，不相信任意 Host/X-Forwarded-Host 或
  用户提交的 redirect URI；return path 仅限同源安全 Admin 路由。
- 回调完成后跳转到无 token 的完成页；用短时、HttpOnly、SameSite、生产
  Secure 的浏览器绑定及一次性 POST 领取 Depsilo JWT。IdP token/secret
  不交给前端，JWT 不进 URL。OAuth callback query 和响应必须禁止缓存、
  隐藏敏感参数和 Referer 泄漏，前端路由位于 RequireAuth 之外。
- 普通 Admin API 保持 Bearer 契约；绑定 cookie 仅服务于登录事务，不能
  在本轮暗中扩大成通用 cookie 认证并引入未覆盖的 CSRF 面。
- 外部 JWT 必须绑定服务端 session ID、来源及绝对期限；不能伪装成没有
  外部期限的本地 JWT。认证根据数据库中的用户来源要求 session，不仅
  根据 JWT 是否带 sid 分支；外部用户缺失 sid 或持有意外残留 API Token
  均拒绝。refresh 保持 sid，不得退回普通 GenerateJWT 无限续期。
  保留现有本地登录和机器 token 兼容性。
- 登录事务限制容量、寿命、响应体、请求时长与频率；只对管理员配置的
  issuer 及经过约束的 metadata 端点出站。支持 homelab 私网的可信 HTTPS，
  不提供任意 URL 抓取/跳过 TLS 验证。HTTP 仅在显式本机 loopback 测试启用。
- 首版无需保存 IdP refresh token；取得必要 claims 后丢弃提供方凭据。
  登录结果和拒绝原因进入审计，日志不含 code、secret 或完整 token。

### 数据与配置

新增外部身份及可撤销 session 的编号迁移；身份唯一约束、并发首次登录与
失败回滚必须由数据库保证。外部身份记录来源和同步时间，session 记录身份、
签发/绝对到期/撤销时间与安全配置版本。过期记录有明确清理策略。

OIDC 配置由 `config.toml`/环境变量拥有，保持现有优先级及重启生效模式。
认证设置使用独立 DTO 和表单；Secret 写入不回读、不加入自动草稿、从环境
覆盖时不可在 UI 假装修改成功。保存与运行状态分开，提供方故障不能导致
整个 Depsilo 进程拒绝启动或使匿名包下载不可用。

新增接口建议：公开 provider 摘要、开始授权、callback、一次性完成交换；
受 `can_manage_credentials` 保护的配置检查/试登录。具体路径在 P1 契约测试中固定，新增公开
认证路由必须显式列入路由审计，不让包路由/机器路径落入 SPA。

主要修改范围：

- 后端：`internal/identity/`、`internal/api/auth.go`、`internal/api/router.go`、
  `internal/middleware/auth.go`、`internal/api/admin/user.go`、`token.go`、
  配置、DB 迁移、server wiring 和相邻测试。
- 前端：Login、AdminApp、Users、Settings、`lib/api.ts`、Principal 类型、
  安全 return path、zh/en 语言资源与相关 Playwright/Vitest 用例。
- 运维与证据：Casdoor 隔离夹具、接入文档、ADR/威胁模型/authority，
  schema 升级与回滚记录。

## 测试分层与验收

| 层次 | 核心场景 | 证明什么 |
| --- | --- | --- |
| Go 本地 HTTP IdP | 正确 code flow；state/nonce/PKCE/签名/issuer/aud/过期错误；密钥轮换；UserInfo sub 不符；超时；回调重放/并发 | 我们的协议与错误处理，不依赖公网 |
| DB/认证集成 | 旧账号保留；同名/同邮箱隔离；首次登录竞争；只读拒写；降权/禁用；最后本地管理员；外部 TTL/refresh/logout；缺 sid 拒绝；外部用户不能创建/重置本地账户或发放机器管理凭据 | 身份及权限契约 |
| Playwright 模拟 | 登录入口、回跳、取消/失败/重试、设置来源/secret、用户管理、320px/桌面、中英/深浅/键盘 | UI 行为与可访问性，不冒充提供方联调 |
| 真实 Casdoor | 真正输入测试账号，经提供方页面授权；实际回调/校验；读取真实 UserInfo；改组、禁用、重登、IdP 停机恢复 | 固定提供方版本兼容性 |
| 真实 Depsilo + Casdoor | 登录后实际读/写、退出/refresh/绝对到期；外部 admin 业务写成功但用户凭据/token/身份配置写拒绝；readonly 拒写；本地管理员和原 API Token 能力保持 | 完整功能交付；P0 提供方探针不能代替 |
| 发布验证 | `make verify`、新依赖 `make security`、`make test-ui-production`；schema 相关旧版升级契约 | 可构建、可升级、无既有认证回归 |

真实测试使用独立 Compose project、loopback 端口及私有数据目录，不修改
`mcs:/data/dev_server/depsilo` 的用户、密钥或数据。随机测试凭据只保存在
仓库外的私有文件，镜像固定 tag/digest。默认离线门禁不自动拉 Casdoor；
真实测试作为独立 opt-in 命令，完成时分别报告提供方探针与 Depsilo 端到端结果。

P3 通过后按 [mcs 部署指南](../development/mcs-debug.md) 升级调试服务，
保留冷备份、既有本地管理员和签名密钥。迁移后回滚需要成对恢复数据库和
配置，不能仅切回旧镜像。未在真实 IdP 跑通前不宣称已完成第三方登录。

## 下一步完成顺序

1. 固定 Casdoor 测试环境，记录真实可用的 claims、异常和发现端点。
2. 以该证据定稿有限 OIDC ADR，落地身份/会话模块及测试。
3. 完成设置、登录和用户管理 UI；保持统一 Principal 契约。
4. 运行真实 Depsilo/Casdoor 浏览器测试、迁移门禁和调试升级。
5. 再评估组织撤权时效与细粒度权限；既有扫描历史、投递结果持久化及
   级联配置并发控制可作为后续独立事项，不与登录改造捆绑发布。

实际执行记录另见隔离环境 README 和本次交付说明；本计划不把未来测试
写成已经通过。
