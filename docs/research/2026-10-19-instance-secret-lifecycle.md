# 实例密钥的默认管理与文件注入

调研日期：2026-10-19。范围：单实例、自托管软件的签名密钥生命周期与
Docker 部署体验。本文是设计建议，不表示 Depsilo 已支持新的配置项。
资料只使用项目官方文档和官方源码；开发分支源码固定到本次核对的提交。

## 结论

对 Depsilo 更合适的默认体验是：**用户只挂载持久化目录，应用自己生成一次
实例签名密钥并持续复用；显式密钥和文件注入保留为高级选项。**
Verdaccio 的存储数据库和 Gitea 的持久化配置已经采用这种低配置方式。
不过 Depsilo 现有网页 Setup 也会生成并保存密钥，不能把它描述为尚无自动
管理能力。单独增加 `_FILE` 能改善外部注入，但仍要求用户先准备密钥文件，
不能代替默认生成流程。[Verdaccio 配置](https://www.verdaccio.org/docs/configuration/#the-verdaccio-db-database)、
[Gitea Docker 安装](https://docs.gitea.com/installation/install-with-docker/#managing-deployments-with-environment-variables)、
[Depsilo 配置写入](../../internal/config/writer.go)。

## Depsilo 当前行为

- 无配置首次启动会进入 Setup；完成网页 Setup 时生成新的 `auth.jwt_secret`
  并写入权限为 `0600` 的 `config.toml`。默认 Compose 把整个
  `/root/.depsilo` 挂入持久卷，因此正常新安装不需要用户生成 `.docker.env`。
  [配置加载](../../internal/config/loader.go)、[配置写入](../../internal/config/writer.go)、
  [官方 Compose](../../compose.yaml)。
- 已有手写配置但缺少有效密钥时，非 loopback 监听会拒绝启动。
  `AuthConfig` 目前没有 `jwt_secret_file` 或 `_FILE` 读取功能；仅设置一个
  `DEPSILO_AUTH_JWT_SECRET_FILE` 环境变量不会生效。
  [加载与校验](../../internal/config/loader.go)、[配置结构](../../internal/config/config.go)。
- 根密钥还用于派生 npm、PyPI 制品链接的签名密钥。因此换密钥不仅会使原
  JWT 登录凭据失效，还会影响先前签发的相关制品链接；把同一份有效密钥
  从环境变量迁到文件属于换存放位置，不能顺带重新生成。
  [服务组装](../../internal/server/server.go)、
  [签名密钥派生](../../internal/server/pypi_artifact_signing.go)、
  [npm 签名](../../internal/adapter/npm/provenance.go)、
  [PyPI 签名](../../internal/adapter/pypi/rewriter.go)。
- 内置备份目前包含配置和 SQLite 数据库，清单版本为 2，并对文件集合做
  严格校验。外部 `.docker.env` 不在备份内；若以后另建
  `data/secrets/jwt.key`，必须同步设计备份、恢复和旧备份兼容，不能只把
  密钥移出配置。[备份结构](../../internal/backup/archive.go)、
  [恢复校验](../../internal/backup/restore.go)。

## 代表性软件

以下“重启”均指保留相应持久化状态后的普通进程或容器重启。

| 软件 | 密钥用途 | 默认生成、保存和重启行为 | 外部文件注入 |
| --- | --- | --- | --- |
| Verdaccio | Web JWT；API 可选 JWT，另有 legacy token 路径 | 默认本地存储数据库自动生成并保存 `secret`，后续复用；官方明确更换它会使已有 token 失效。数据库常见名称有 `.verdaccio-db` / `.verdaccio-db.json`，应以所用版本和存储配置为准。 | 文档描述存储插件的 `getSecret` / `setSecret` 接口；本次核对没有把它认定为通用 `_FILE` 方案。[官方配置](https://www.verdaccio.org/docs/configuration/#the-verdaccio-db-database)、[本地存储实现](https://github.com/verdaccio/verdaccio/blob/5c806044c04545b4352357b0b948c3acc51b6c8d/packages/plugins/local-storage/src/local-database.ts#L53-L68)。 |
| Gitea | OAuth2/LFS JWT；`SECRET_KEY` 另用于加密敏感数据 | 安装时自动生成 secrets/tokens 并写入 `/data/gitea/conf/app.ini`。默认 OAuth2 签名算法是 RS256，私钥缺失时自动创建在 `APP_DATA_PATH/jwt/private.pem`；HMAC 的 `JWT_SECRET` 是另一条配置路径。保留相应数据和配置即可复用。 | 有 `JWT_SECRET_URI`、`LFS_JWT_SECRET_URI`、`SECRET_KEY_URI`；Docker 还有 `GITEA__section__KEY__FILE`。**不能把加密用 `SECRET_KEY` 当 JWT 密钥。**[Docker 安装](https://docs.gitea.com/installation/install-with-docker/#managing-deployments-with-environment-variables)、[配置表](https://docs.gitea.com/administration/config-cheat-sheet/#oauth2-oauth2)。 |
| Portainer | 普通登录 JWT 的 HS256 签名；Kubernetes 配置 JWT 有单独 scope | 所核对源码在每次 `NewService` 时生成普通登录密钥，只保留于内存；由此可推断普通登录 JWT 会在服务重启后失效。Kubernetes scope 的密钥则生成后写入数据库并复用。这是“每种用途独立决定生命周期”的例子。 | 本次所查普通 JWT 构造函数没有外部密钥输入。`portainer.key`、TLS key、数据库加密 secret 不能据文件名当作普通 JWT 密钥。[固定提交源码](https://github.com/portainer/portainer/blob/be20b574172a27251fd8648cefe1b810a8dad6a5/api/jwt/jwt.go#L47-L95)。 |
| Harbor | Registry token 的 RSA 私钥；另有加密目标凭据的 secret key | Compose 安装的 prepare 阶段自动生成并保存 `data_volume/secret/core/private_key.pem`，以后复用；另把 `data_volume/secret/keys/secretkey` 挂载到 core。生成器对已有文件执行读取，不随普通重启更新。 | 官方 Compose 直接绑定密钥文件。**secretkey 是加密密钥，registry token 私钥才是签名例子。**[生成/读取逻辑](https://github.com/goharbor/harbor/blob/9e79066f74cb371a9cbde7620b23c8fecbe0448b/make/photon/prepare/utils/cert.py#L15-L36)、[签名私钥复用](https://github.com/goharbor/harbor/blob/9e79066f74cb371a9cbde7620b23c8fecbe0448b/make/photon/prepare/utils/cert.py#L79-L102)、[挂载](https://github.com/goharbor/harbor/blob/9e79066f74cb371a9cbde7620b23c8fecbe0448b/make/photon/prepare/templates/docker_compose/docker-compose.yml.jinja#L158-L163)、[加密用途](https://pkg.go.dev/github.com/goharbor/harbor/src/lib/config#SecretKey)。 |
| Grafana | `security.secret_key` 用作数据库敏感数据的加密密钥，现代默认信封加密中为 KEK | 所核对 `defaults.ini` 带静态默认值，不能把 Grafana 描述成“每个新实例自动生成随机 JWT 密钥”。保留自定义密钥很重要；迁移/轮换有专门的数据密钥重加密流程。 | Docker 支持通用 `GF_Section_Key__FILE`，可用于 `GF_SECURITY_SECRET_KEY__FILE`。适合参考文件注入接口，不能照搬其默认值或将其称为登录 JWT 方案。[默认配置](https://github.com/grafana/grafana/blob/f77dc41d3397c99568600f7a00ca764b9bb2a881/conf/defaults.ini#L430-L450)、[加密与轮换](https://grafana.com/docs/grafana/latest/setup-grafana/configure-security/configure-database-encryption/)、[Docker 文件注入](https://grafana.com/docs/grafana/latest/setup-grafana/configure-docker/#configure-grafana-with-docker-secrets)。 |

这五个案例说明默认持久化与 `_FILE` 是两个可以同时存在的能力；
“所有软件都把 JWT 密钥自动保存到 data”或“任何 key 文件都能作为 JWT
先例”均不成立。尤其 Portainer 普通 JWT 的生命周期与 Depsilo 的持久签名
链接需求不同，不宜照搬每次启动生成的行为。

## Docker Compose secrets 的实际边界

单机 `docker compose` 会把声明的 secret 文件绑定挂载到
`/run/secrets/<name>`，并要求对服务显式授权。`*_FILE` 是镜像或应用实现
的约定，Compose 不会自动把任意 `_FILE` 变量转换成秘密值，也不负责为
应用生成密钥。[Compose 官方指南](https://docs.docker.com/compose/how-tos/use-secrets/)。

因此，文件方案能让 Compose 配置和容器创建时的环境变量中只有路径；
应用最好直接读取文件。它没有自动消除宿主机上的明文文件、备份权限与
root/Docker 管理权限带来的访问能力。不能把单机文件绑定挂载描述成
Swarm secrets 的加密 Raft 存储与内存交付模型。
[Compose 指南](https://docs.docker.com/compose/how-tos/use-secrets/)、
[Swarm secrets](https://docs.docker.com/engine/swarm/secrets/)。

文件来源的 Compose secret 不应依赖 `uid` / `gid` / `mode` 自动修复源
文件权限；官方规范说明这类来源使用 bind mount，不能实现这些重映射。
[服务 secrets 的 long syntax](https://docs.docker.com/reference/compose-file/services/#secrets)。

## 对 Depsilo 的建议

以下是结合上述案例及当前单实例部署边界得出的设计判断，不是其他项目的
既定要求，也不是已接受的架构决策。

1. **近期先利用已有能力。**正规新安装继续由 Setup 自动保存密钥；
   对 mcs 现有部署，可以把环境变量中当前生效的同一份值保存到持久化
   `config.toml` 的 `auth.jwt_secret`，再移除注入。操作前确认文件权限、
   覆盖来源、配置可加载及恢复路径；这不需要新增 `_FILE` 功能，也不必
   轮换密钥。本文未修改 mcs。
2. **产品默认统一自动管理。**若改进手写配置和只读配置场景，缺少显式
   密钥时由应用在实例数据目录生成并原子持久化，例如
   `data/secrets/jwt.key`。使用加密安全随机源；目录 `0700`、文件 `0600`；
   创建成功并持久化之后再开始服务，后续只读取。这个目录应属于实例
   控制状态，与可以清空、驱逐或放到 S3 的制品缓存目录区分。
   创建动作由 `serve` 生命周期显式触发；配置解析还被备份、恢复和审计
   命令调用，不应因一次只读配置加载而写入新密钥。
3. **兼容已有密钥。**现有环境变量和配置中有效的值保持权威，不因升级
   自动换成新随机值。若迁入默认文件，应明确是保留原值的迁移，而不是
   新建实例；同时为来源冲突规定清楚规则，避免默默选择另一把密钥。
   Setup 写配置时也应复用已选定的有效密钥；当前 writer 自行生成一把
   密钥的做法需要与新生命周期一并调整。
4. **把文件注入做成可选接口。**例如增加
   `DEPSILO_AUTH_JWT_SECRET_FILE=/run/secrets/depsilo_jwt_secret` 和/或
   `auth.jwt_secret_file`，由 Go 应用直接读取；同时配置值与文件时应拒绝
   含糊配置。外部管理的文件只读，不被应用覆盖。当前版本尚不支持这些
   新名称。
5. **错误要稳定、可恢复。**已有文件为空、损坏、权限不足或无法读取时
   拒绝启动，不能悄悄覆盖并使会话/链接失效。默认文件丢失后的恢复应有
   明确规则，不能把已运行实例的密钥丢失一律当作全新安装；只允许显式
   重置/轮换流程接受失效后果。
6. **备份与轮换一起设计。**默认持久化密钥必须随实例备份/恢复，外部
   Secret 则明确列为恢复依赖。新增文件前更新当前严格备份格式；覆盖
   新安装、重启、容器重建、旧配置升级、只读配置、丢失/损坏文件和
   备份恢复。若要让轮换期间旧 JWT/链接继续有效，需要专门的密钥版本
   与验证窗口，不能以“重新生成文件”代替。

最小摩擦的用户契约应是：**保留同一个实例数据卷，重建容器时无需填写
签名密钥。**单独文件和外部 Secret 用于需要集中管理、只读配置或避免
环境变量注入的部署，不成为普通用户安装前的必填步骤。
