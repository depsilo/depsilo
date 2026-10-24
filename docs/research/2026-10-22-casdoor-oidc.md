# Casdoor OIDC 接入与权限映射评估

核对日期：2026-10-22。本文是接入方案的研究依据，**不表示 Depsilo 已实现
第三方登录**。初步调研后已按用户选择搭建隔离测试实例，实际验证范围见文末。
已核对官方文档、
OIDC/OAuth 标准，以及 Casdoor 源码快照
[`dd0cc5d7af104d7feeacd3b5859402bf0bc4d1f7`](https://github.com/casdoor/casdoor/tree/dd0cc5d7af104d7feeacd3b5859402bf0bc4d1f7)。
该快照来自核对时的 master，不是经过兼容性验证的发布版本。

## 结论与接入边界

建议让 Depsilo 实现通用 OIDC 客户端，以 Casdoor 作为第一个真实兼容目标。
Casdoor 官方支持标准 OIDC 库，包括 Go 的 `go-oidc`；因此无需为每种身份平台
另建登录流程。[标准客户端接入](https://casdoor.ai/docs/how-to-connect/oidc-client/)

第一阶段应实现三件事：第三方身份认证、明确的应用准入、外部组到 Depsilo
现有角色的映射。Casdoor 的 Casbin 模型、资源和动作是另一套授权系统；其
`Enforce` API 需要应用主动调用并定义一致的请求语义。接入 OIDC 不会自动
把这些权限带进 Depsilo，更不会自动获得项目级隔离。
[Casbin API](https://casdoor.ai/docs/permission/exposed-casbin-apis/)、
[权限配置](https://casdoor.ai/docs/permission/permission-configuration/)

建议首版保留本地管理员恢复入口；包管理器、机器凭证和浏览器登录分别处理。
已有 API Token 不应突然开始接受任意 Casdoor access token。以上为产品和
架构建议，需落实到 Depsilo 当前权限边界及相应测试。

## 协议与身份

| 项目 | 已核对事实 | Depsilo 接入建议 |
| --- | --- | --- |
| Discovery | 根路径 `/.well-known/openid-configuration` 提供端点与 JWKS；另有应用专属 discovery | 优先配置发行者根 URL，从 discovery 读取端点 |
| Issuer | 当前文档说明全局与应用 discovery 的 issuer 均为后端公开 origin；旧版曾含应用路径，导致严格校验失败 | 校验配置 issuer、metadata issuer、ID token `iss` 完全一致；修正配置或升级提供方，不关闭校验 |
| 授权方式 | 支持 Authorization Code 和 S256 PKCE | 后端交换授权码，使用随机 state、nonce 和 PKCE；secret 留在后端 |
| Subject | ID token 与 UserInfo 的 `sub` 来自 Casdoor `User.Id` | 唯一身份键采用 `(issuer, sub)`；用户名、邮箱只作属性 |
| 组织 | 应用关联组织；shared application 有额外 client ID/audience 规则 | 首版用专用单组织应用，避免隐式跨组织接入 |

来源：[OIDC 接入及 issuer 兼容说明](https://casdoor.ai/docs/how-to-connect/oidc-client/)、
[PKCE](https://casdoor.ai/docs/how-to-connect/oauth/#pkce)、
[令牌生成源码](https://github.com/casdoor/casdoor/blob/dd0cc5d7af104d7feeacd3b5859402bf0bc4d1f7/object/token_jwt.go)、
[应用概念](https://casdoor.ai/docs/basic/core-concepts/)。

实现必须验证签名、允许的算法、`iss`、`aud`、有效期和本次 nonce；使用
UserInfo 时，必须确认其 `sub` 等于已验证 ID token 的 `sub`。授权回调应
消费一次性登录事务，限制回跳路径，防止重放和开放重定向；不要复制文档中的
固定示例 state。后端机密客户端同样使用 PKCE。
[OIDC ID Token 校验](https://openid.net/specs/openid-connect-core-1_0.html#IDTokenValidation)、
[UserInfo 校验](https://openid.net/specs/openid-connect-core-1_0.html#UserInfoResponseValidation)、
[OAuth 安全最佳实践](https://www.rfc-editor.org/rfc/rfc9700.html)

一个需要专门实测的身份边界：源码的默认用户 ID 是随机生成，但应用可选择
`Incremental`，此时从组织的最后一个用户 ID 递增。不能假定任意 Casdoor
配置下 ID 都是全实例唯一 UUID。测试实例应使用默认 ID 生成规则，并覆盖
跨组织、改名、删除后重建和身份冲突；不兼容的主体唯一性配置应被明确拒绝，
不能回退到邮箱或用户名自动合并。
[用户 ID 生成与 UserInfo 源码](https://github.com/casdoor/casdoor/blob/dd0cc5d7af104d7feeacd3b5859402bf0bc4d1f7/object/user.go)

## Claims、准入和角色映射

Casdoor 的 token 格式影响 `roles` / `groups` 的来源及结构，不能只写一个
读取 `roles` 字符串的解析器便宣称兼容。

| Casdoor 配置 | 实际行为 | 建议 |
| --- | --- | --- |
| `JWT-Standard` | token 主要包含标准 claims；`profile` scope 下的 UserInfo 可返回 `groups`、角色名称数组 | 作为首个支持和实测配置 |
| 默认 `JWT` | `roles` 可以是完整角色对象数组 | 不把对象序列化结果当作角色名；不静默自动授权 |
| `JWT-Custom` | 可定义属性名/来源，`Array` 类型保持数组 | 作为明确配置的扩展能力，测试空值和类型错误 |
| Token group format | `ID` 使用组织/组标识，`Name` 使用组名，`Path` 使用组织和层级路径 | 首版推荐 `ID`，减少同名组歧义 |
| Token fields | 非空白名单也限制 UserInfo 字段 | 需要组/角色时保留对应字段，否则按缺失处理 |

来源：[官方 OAuth2 Proxy 接入中的 claims 对照](https://casdoor.ai/docs/integration/go/oauth2-proxy/)、
[Token fields 与自定义属性](https://casdoor.ai/docs/token/overview/)、
[UserInfo 和组格式源码](https://github.com/casdoor/casdoor/blob/dd0cc5d7af104d7feeacd3b5859402bf0bc4d1f7/object/user.go)。

建议的确定性规则：

1. 为应用建立专用组，例如 `<org>/depsilo-admins` 与
   `<org>/depsilo-readers`，精确映射到 Depsilo 已有角色；示例名称不作为默认授权。
2. 单独定义谁能进入 Depsilo。未命中准入规则、claims 缺失、类型错误或
   UserInfo 失败均不创建有效会话。不能把“角色未知”自动视为允许只读访问。
3. 映射只接受管理员明确配置的外部标识；不把 Casdoor `IsAdmin`、通用
   `admin` 字符串或邮箱域直接解释为 Depsilo 管理员。
4. 若同一人命中多个映射，采用明示且可测试的优先级；变更后重新计算，
   支持降权和失去准入。映射结果和拒绝原因写审计，日志不记录 token/secret。
5. 身份关联只通过稳定身份键或经重新认证的显式绑定；同邮箱的本地管理员
   与新 OIDC 身份不得自动合并。

以上是建议的 Depsilo 授权契约。提供方侧也可以设置应用 Tags 准入，但官方
明确豁免组织管理员、全局管理员及 `built-in` 中的用户/应用，不能用它替代
Depsilo 自身的准入规则。[应用 Tags](https://casdoor.ai/docs/application/tags/)

Casdoor User 的 Roles/Permissions 是由独立角色和权限资源计算的扩展字段；
不能靠 `/api/update-user` 中写入一个 roles 数组完成角色分配。自动化样例应
初始化真正的组/角色关系，然后实际读取 UserInfo 验证。
[用户与角色字段说明](https://casdoor.org/docs/user/overview/)

## 撤权、登出和目录同步

| 能力 | 提供方能力 | 对 Depsilo 的约束 |
| --- | --- | --- |
| 本地登出 | Depsilo 自己结束本地会话 | 不代表 Casdoor SSO 会话已结束；界面应准确命名 |
| RP-initiated logout | Casdoor `/api/logout`，支持 `id_token_hint` 和受注册列表约束的返回地址 | 使用 discovery 实际公布的端点；先结束本地会话，再尝试提供方登出 |
| Back-channel logout | 当前官方文档说明支持签名 logout token，并需配置应用回调 | Depsilo 需实现验签、issuer/audience/events/sid/sub 校验、防重放及本地会话撤销；pinned 版本需实测 |
| Token revocation/introspection | 源码有 revoke 与 introspect 路由；文档说明撤销令牌变为 inactive | 只离线验 JWT 签名不能感知撤销；提供方 token 撤销也不会自动撤销 Depsilo 自己签发的会话 |
| SCIM service provider | Casdoor 接收 User/Group 的 provisioning 请求 | 这是“同步进 Casdoor”，不自动同步进 Depsilo |
| SCIM syncer | Casdoor 从外部 SCIM 服务拉取用户，文档说明只读 | 不是向 Depsilo 推送停用事件的渠道 |

来源：[RP logout](https://casdoor.ai/docs/session/rp-initiated-logout/)、
[SSO 和 back-channel logout](https://casdoor.ai/docs/session/single-sign-out/)、
[Back-Channel Logout 标准](https://openid.net/specs/openid-connect-backchannel-1_0.html)、
[路由源码](https://github.com/casdoor/casdoor/blob/dd0cc5d7af104d7feeacd3b5859402bf0bc4d1f7/routers/router.go)、
[token 生命周期](https://casdoor.ai/docs/token/overview/)、
[SCIM 服务端](https://casdoor.ai/docs/scim/overview/)、
[SCIM 拉取同步器](https://casdoor.ai/docs/syncer/SCIM/)。

因此，第一阶段必须说明外部降权/停用的生效时机。建议在成功登录时重新
计算角色，并给外部会话设定明确的最长有效期；本地管理员禁用必须立即作用于
当前授权检查。若要承诺“企业侧停用后立即失效”，必须额外落地并实测
back-channel logout、在线检查或身份同步及其失败策略，不能仅靠短会话宣传。
外部用户自己创建的长期 API Token 还需要独立的撤权策略。

## 可重复的 Casdoor 实测方案

官方 `casbin/casdoor-all-in-one` 当前包含 SQLite，定位是临时试用。
固定源码的 entrypoint 默认使用 `driverName=sqlite` 和 SQLite 文件 DSN。
旧版官方 PDF 曾描述 MySQL，因此不能对任意历史 tag 照搬当前部署假设。
正式接入验证应先选定明确发布 tag，再记录解析后的镜像 digest、源码版本、
平台架构及 discovery 快照；不要让 CI 跟随 `latest`。
[Docker 试用说明](https://casdoor.ai/docs/basic/try-with-docker/)、
[SQLite entrypoint](https://github.com/casdoor/casdoor/blob/dd0cc5d7af104d7feeacd3b5859402bf0bc4d1f7/docker-entrypoint.sh)、
[历史 PDF](https://casdoor.org/pdf/Casdoor_Docs.pdf)

建议新增独立 opt-in 集成样例，保持正常离线测试无需拉镜像：

1. 固定镜像和初始化 JSON，创建专用测试组织、单组织 OIDC 应用、独立签名
   证书，以及管理员组、只读组、无权限组、禁用用户。随机生成样例 secret，
   写入临时目录；不要使用公开 demo、真实管理员或仓库中的常量凭证。
2. SQLite 数据放独立可清理卷；标准镜像需要核对 UID 1000 的写权限。
   从实际配置读取 `initDataFile`，不要假设所有版本都从同一路径初始化。
   [Docker 部署](https://casdoor.ai/docs/deployment/docker/)、
   [数据初始化](https://casdoor.org/docs/deployment/data-initialization/)
3. issuer 使用浏览器和 Depsilo 后端都可访问的同一地址，精确注册回调。
   localhost 环境可单独允许 HTTP loopback；远程 mcs 测试使用可信 HTTPS
   或受控隧道，不把内部 Docker 服务名当成浏览器 issuer。不要通过关闭
   TLS、issuer 或 audience 校验绕过网络配置问题。
4. 把样例配置为 `JWT-Standard`、`openid profile email`、组格式 `ID`，
   保留所需 Token fields。先记录脱敏后的实际 claims 结构，再验证映射。
5. Playwright 实际打开 Casdoor 登录表单，经浏览器完成授权回调，之后调用
   真实 Depsilo API 确认权限。不能用“直接伪造回调响应”代替兼容性验收。

建议测试矩阵：

| 层次 | 必须验证 |
| --- | --- |
| 协议单元/本地 HTTP 提供方 | 错误 issuer/audience/签名/nonce；state 过期、重放与跨浏览器；PKCE 错误；UserInfo sub 不一致；密钥轮换；端点失败与超时 |
| 权限集成 | 管理员、只读、未映射、缺失/畸形 groups；角色交集；本地禁用优先；降权/移除准入；同邮箱与同名不合并；provider 删除/重建不接管身份 |
| Casdoor 真实浏览器 | 登录成功、取消登录、凭证错误、未授权；改名仍映射同一人；禁用/重建；角色/组变更；本地恢复入口；第三方故障仍能本地登录 |
| 生命周期 | 本地 logout；提供方 logout；过期重登；禁用后旧会话与 API Token 的实际行为；实现后再验 back-channel 重放与撤销 |
| UI/运维 | 中文/英文、窄屏、键盘操作；设置 secret 脱敏；审计无授权码/token/secret；升级及回滚不丢失外部身份绑定 |

## 尚待真实实例确认

- 用户已有身份平台的版本、issuer、组织、应用 token 格式、组格式和可用 claims；
  隔离样例不代表其生产配置，不能承诺直接兼容其既有权限模型。
- 其他部署版本的 metadata、JWKS、PKCE、UserInfo 行为，以及尚未实测的
  logout/back-channel 支持；下文固定版本的提供方探针不能外推为全部版本兼容。
- 生产用户期望的停用时效，以及外部管理用户是否允许签发长期 API Token。
- 客户现有权限如果是 Casbin 的资源/动作策略而非组角色，需要另行确定
  Depsilo 的授权对象、策略模型、可用性和失败策略；不应混入首次 SSO 交付。

## 隔离实例实施记录

后续用户选择先搭建独立 Casdoor 测试实例，夹具位于
[`testground/oidc-casdoor`](../../testground/oidc-casdoor/README.md)。已启动本机
Docker 中的独立 Compose 项目 `depsilo-oidc-casdoor`，仅绑定
`127.0.0.1:18000`；没有修改或启动 Depsilo，也没有操作 mcs。

运行版本是官方 [v4.23.0](https://github.com/casdoor/casdoor/releases/tag/v4.23.0)，
源 tag 为 `1b91d9d319a127e9b1af88fa7a20bb35c052b9d8`。使用固定镜像
`casbin/casdoor-all-in-one:4.23.0@sha256:0a65b3cbca95ec16783e43eb7520f152de16e1eaa9b6b6547f01d7d006da6da6`，
已在本机 arm64 Docker 运行；amd64 manifest 已查询但没有实际启动验证。

已实际检查容器 healthy、精确的 discovery issuer、S256 声明及非空
global/application JWKS。已创建单独组织、专用 `JWT-Standard` 应用、独立
生成的签名证书、四个测试用户和三个组；只读查询确认其中一个用户被禁用，
所有测试用户均非 Casdoor 管理员。重复 `start` 不替换既有凭据。

实践中发现 all-in-one entrypoint 的默认 `dataSourceName` 环境变量会覆盖
挂载的 `app.conf`，使 SQLite 落在容器内。夹具已改为在 Compose 显式设置
非秘密 DSN，SQLite 及 WAL 现在实际位于私有宿主目录。第一次修正先停止
测试容器，再冷复制数据库，保留了已生成的 subject。

凭据、配置、初始化数据、SQLite 和验证证据均位于
`~/.cache/depsilo/oidc-casdoor/`；目录 `0700`，凭据文件 `0600`。仓库不含
生成密码或 client secret。提供 start/stop/status/metadata/paths 命令，停止
保留本地数据。协议/浏览器测试结果另由夹具输出记录；仅 discovery 声明
`backchannel_logout_supported=true` 不等于已验证完整登出联动。

真实 Playwright 浏览器和独立回调探针已通过六项：管理员组、只读组及未映射
组用户分别完成 Authorization Code + S256 PKCE，并验证 ID token 签名、
issuer、audience、nonce、UserInfo subject 和实际 groups；已消费的授权码
重放被拒绝；错误 PKCE verifier 被拒绝；禁用用户在 Casdoor 登录阶段被拒绝。
角色标签只是探针中的预期映射，**没有测试或实现 Depsilo 的权限处理**。

随后执行实际 stop/start，移除并重建本测试容器与网络；比较确认用户 subject、
禁用状态、签名密钥和凭据均保持不变，再次执行上述六项浏览器验证全部通过。
已单独验证 metadata 请求忽略系统/环境代理，并拒绝 HTTP 重定向。
私有报告为 `evidence/browser-smoke.json` 和 `evidence/lifecycle.json`。
尚未验证 MFA、外部 HTTPS、统一登出/即时撤权、目录同步或 Depsilo 会话行为。

为后续实现预注册的回调已统一为
`http://127.0.0.1:18081/api/v1/auth/oidc/callback`，匹配 Depsilo 当前 API 前缀。
此次精确修改测试应用允许列表时保留 subject、签名密钥和 client secret；
实际协议探针仍使用独立的 `http://127.0.0.1:18082/callback`。
