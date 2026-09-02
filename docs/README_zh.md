<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="brand/logo-stacked-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="brand/logo-stacked-light.svg">
  <img src="brand/logo-stacked-light.svg" alt="Depsilo" width="200">
</picture>

**为个人和小团队打造的自托管依赖代理、缓存与供应链策略服务。**

用一个服务承接 14 个包生态和 Docker OCI。缓存重复下载、管理上游、查看依赖流量，
并在依赖安装链路上执行供应链策略。

[![Release](https://img.shields.io/github/v/release/depsilo/depsilo)](https://github.com/depsilo/depsilo/releases)
[![Verify](https://github.com/depsilo/depsilo/actions/workflows/verify.yml/badge.svg)](https://github.com/depsilo/depsilo/actions/workflows/verify.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](../LICENSE)

[网站](https://depsilo.com) &bull;
[快速开始](#快速开始) &bull;
[文档导航](#文档导航) &bull;
[容器镜像](https://github.com/depsilo/depsilo/pkgs/container/depsilo) &bull;
[English](../README.md)

</div>

---

## Depsilo 能做什么

```text
包管理器 / CI / 编码 Agent
            │
            ▼
         Depsilo
   缓存 · 执行 · 校验 · 审计
            │
            ▼
           上游
```

- **缓存（Cache）**：将制品流式写入本地或 S3 兼容存储，合并并发缓存未命中请求，
  并在上游不可用时继续提供符合条件的已缓存制品。
- **执行（Enforce）**：仅当代理请求携带可信的包身份时，才按
  [Package Rule 能力矩阵](package-rules.md)执行运维者定义的规则；已知恶意版本阻断
  仅覆盖下表列出的生态。最小发布年龄目前安全停用，等待制品来源与发布时间证明完成绑定。
- **校验（Verify）**：记录首次发现的哈希，并在不可变制品自然刷新发生变化时提示
  篡改。
- **审计（Audit）**：在管理后台、API、日志、Webhook 和 Prometheus 指标中呈现
  请求、策略决策与上游健康状态。

Depsilo 采用 MIT 许可证，不包含遥测；它是基于 SQLite 的轻量单实例服务，不是
多节点制品库或高可用控制面。

> 本 README 描述当前 `master` 分支。使用带版本标签的发行版时，请以该发行版附带的
> README 和配置参考为准。

## 快速开始

> **发行说明：**统一状态路径、启动摘要和首次项目接入从 `v0.9.1` 开始提供。运行
> `v0.9.0` 时，请以该版本附带的 README 为准。

从 Binary 或容器安装中任选一种。首次启动不需要克隆仓库，不需要提前准备
`config.toml`，也不需要设置数据库和缓存路径环境变量。

### Binary（Linux / macOS）

```bash
curl -fsSL https://depsilo.com/install.sh | bash
depsilo serve
```

替换以 daemon 模式运行的 v0.9.0 binary 前，必须先用 v0.9.0 binary 执行停止。
v0.9.1 会拒绝旧版未经认证的纯 PID 记录。如果已经替换 binary，请先确认旧进程确实
结束，再手动删除 `~/.local/share/depsilo/depsilo.pid`，然后启动 v0.9.1。

打开 <http://127.0.0.1:23333>。安装器会校验发行包 checksum；如需固定版本或选择
其他安装目录，可分别设置 `DEPSILO_VERSION` 和 `DEPSILO_INSTALL_DIR`。

也可以直接从 [GitHub Releases](https://github.com/depsilo/depsilo/releases)
下载对应平台的压缩包。

### Docker

```bash
docker run -d \
  --name depsilo \
  -p 23333:23333 \
  -v depsilo-data:/root/.depsilo \
  --restart unless-stopped \
  ghcr.io/depsilo/depsilo:latest

docker logs depsilo
```

打开 <http://127.0.0.1:23333>。命名 Volume 会保存自动生成的配置、SQLite 数据库、
缓存制品和其他运行状态，重启或重建容器不会丢失这些数据。

长期运行时，请将 `latest` 替换为完整的 `X.Y.Z` 发行标签。GHCR 是官方主镜像仓库，
Docker Hub 作为镜像源同步维护。

镜像从 v0.9.1 起固定以非 root UID/GID `10001:10001` 运行。复用 v0.9.0 创建的
旧 Volume 时，必须按[部署指南](deployment.md)完成一次性所有权迁移；使用 v0.9.0
官方 bind-mount Compose 布局时，必须使用指南中的独立兼容流程。

### Docker Compose

```bash
curl -fsSLO https://raw.githubusercontent.com/depsilo/depsilo/master/compose.yaml
docker compose up -d
docker compose logs depsilo
```

官方 Compose 文件只有一个服务、一个端口和一个持久化 Volume。可通过
`PORT=18080 docker compose up -d` 修改宿主机端口，然后打开
<http://127.0.0.1:18080>。启动日志显示的是容器监听地址；浏览器应使用宿主机发布
端口，日志用于查找 bootstrap token。

### 完成首次启动

启动摘要会告诉你服务器、数据库和缓存是否就绪。新的互动式安装还会显示 Portal
地址和一次性 bootstrap token。

1. 打开 Portal，输入 bootstrap token。
2. 创建第一个管理员。Depsilo 没有预设管理员密码。
3. 进入 **接入第一个项目**。
4. 选择生态和包管理器，复制自动生成的配置，然后运行建议的依赖命令。
5. Depsilo 会自动识别真实请求。需要验证缓存时，再运行一次请求；也可以随时跳过并
   前往总览。

浏览器中的配置以你实际访问 Depsilo 的 URL 为准，会保留局域网地址、反向代理域名、
自定义端口和 HTTPS。Portal 只提供透明的复制粘贴命令，不会修改本机包管理器配置。

已经配置过的部署在运维者登录 Admin 后不会被强制重新完成接入。以后仍可通过
**接入项目**重新打开同一个流程。

## 支持的生态

| 生态 | 常用客户端 |
| --- | --- |
| Python | pip、uv、Poetry、Pipenv、PDM |
| Debian / Ubuntu | apt |
| Node.js | npm、pnpm、Yarn、Bun |
| Go | `go` |
| Rust | Cargo |
| Java / Kotlin / Scala | Maven、Gradle、sbt |
| Ruby | RubyGems、Bundler |
| PHP | Composer |
| .NET | `dotnet`、NuGet |
| 数据科学 | Conda、R / CRAN |
| 基础设施 | Helm、Alpine apk |
| 模型与数据集 | Hugging Face `hf`、Transformers |
| 容器 | Docker、containerd、Podman，通过独立 OCI `/v2/` 路由接入 |

产品共有 14 个带路径前缀的标准生态，加上独立的 Docker OCI 路由，总计 15 个安装
入口。

OCI 路由是面向客户端 `GET` / `HEAD` 请求的 pull-through cache，不是用于推送或托管
镜像的通用 Registry。

Portal 已提供 Docker Registry 和 Hugging Face 的客户端说明，但首次 Setup Wizard
不会为这两者创建上游。在发送这些请求前，请先通过
[`config.example.toml`](../config.example.toml) 添加对应配置并重启 Depsilo。

## 供应链控制

各项控制彼此独立；启用或关闭其中一项不会隐式改变其他策略。

| 控制 | 默认值 | 行为 |
| --- | --- | --- |
| 已知恶意包列表 | 开启 | 为 npm、Cargo、Composer、NuGet、Go 和 Maven 同步 OSV MAL 的明确版本和全版本记录，并在返回软件包前阻断命中项。 |
| 最小发布年龄 | 暂不可用 | 制品来源与发布时间证明完成绑定前，启用状态下的正阈值会导致启动拒绝。 |
| 篡改检测 | 开启 | 在自然刷新时比较不可变制品与首次发现的 SHA-256，不一致时产生告警；它不会直接阻断请求。 |
| 包 Allow / Deny 规则 | 运维者定义 | 仅执行并记录[请求链路能力矩阵](package-rules.md)支持的选择器；不在不支持的入口猜测包或版本身份。 |

新安装会在首次成功同步恶意包数据集后开始执行对应阻断。后续同步失败时继续使用上一次
成功的数据，不会因此中断正常包流量。

策略阻断仍然表示请求已经到达 Depsilo，并被 Depsilo 正确处理。Portal 和管理后台会
区分缓存命中、未命中、策略阻断和上游错误，而不是把所有未安装结果视为同一种失败。

Composer 在镜像分发地址被拒绝后，可能回退到原始分发地址。要求 Composer 严格阻断
时，还必须限制客户端直接访问该原始地址。

完整策略结构和当前生态默认值见
[`config.example.toml`](../config.example.toml)。各生态精确的包名、版本和范围支持见
[Package Rule 能力矩阵](package-rules.md)。

## 状态、配置与健康检查

Zero-config 安装会把配置、SQLite 和本地缓存收拢到同一个状态根目录：Binary 使用
`~/.depsilo`，官方容器使用 `/root/.depsilo`。精确路径、持久化规则和覆盖方式见
[部署默认值（英文）](deployment.md)。

高级用户仍可覆盖配置文件、数据库、本地或 S3 存储、认证、服务和策略设置。配置
优先级为：

```text
CLI 参数 → DEPSILO_* 环境变量 → 配置文件 → 内置默认值
```

直接运行内置诊断，或通过容器执行：

```bash
depsilo doctor
docker exec depsilo /app/depsilo doctor
```

## 可选集成

### AI 编码 Agent

```bash
cd my-project
depsilo init-agent
```

`init-agent` 会在检测到的 `AGENTS.md`、`CLAUDE.md` 或 `.cursorrules` 中更新由
marker 管理的区块。支持 MCP 的客户端可以携带在 Admin 中创建的普通 API Token，
连接 `POST /mcp`。bootstrap token 只用于首次初始化，不能复用为包或 MCP 凭据。

### 编译缓存

Depsilo 为官方 ccache HTTP 和 sccache WebDAV 客户端提供独立的远程缓存。它不是
`sccache-dist` 调度器，也不是公共 S3 API。存储隔离、凭据、配额和客户端配置见
[编译缓存指南](compile-cache.md)。

## 安全与发行完整性

- 互动式 Setup 由一次性 bootstrap token 保护。
- 管理员会话使用 JWT 认证；API Token 仅存储哈希。
- 即使 Depsilo 仅监听 loopback 并由反向代理对外提供服务，也应使用至少 32 个随机
  字节的 JWT 签名密钥。
- 面向互联网的部署应在可信反向代理后提供 TLS，并按需增加外部身份感知访问层；
  Admin 仍然使用 Depsilo 自己的身份凭据。
- 已签名的 `checksums.txt` 用于认证发行压缩包；安装器和 SBOM 附件有各自的签名
  bundle，容器 digest 会被直接签名并附带 CycloneDX attestation。
- Depsilo 不会向 depsilo.com 上报包名、缓存活动或 onboarding 进度。

让服务暴露到互联网前请阅读 [`SECURITY.md`](../SECURITY.md)。发行产物和镜像的
验证方式见[发行验证指南](release-verification.md)。

## 从源码开发

```bash
git clone https://github.com/depsilo/depsilo.git
cd depsilo
make setup build
./bin/depsilo serve
```

当前工具版本、热更新和测试见[开发快速开始](development/quick-start.md)。提交普通改动
前运行 `make check`；完整离线验证入口是 `make verify`。

## 文档导航

| 目标 | 阅读 |
| --- | --- |
| 部署并定位持久状态 | [部署默认值（英文）](deployment.md) |
| 配置所有可用设置 | [`config.example.toml`](../config.example.toml) |
| 验证已经部署的实例 | [自检清单](self-test-checklist.md) |
| 理解 Admin 和实时配置权威 | [Admin 控制面](admin-control-plane.md) |
| 配置 ccache 或 sccache | [编译缓存](compile-cache.md) |
| 验证签名发行版、镜像和 SBOM | [发行验证](release-verification.md) |
| 查看各版本变化 | [更新日志](../CHANGELOG.md) · [GitHub Releases](https://github.com/depsilo/depsilo/releases) |
| 理解当前产品范围和约束 | [`PRODUCT.md`](../PRODUCT.md) |
| 开发或参与贡献 | [文档地图](README.md) · [贡献指南](../CONTRIBUTING.md) |

## 参与贡献

欢迎贡献。工作流程见 [`CONTRIBUTING.md`](../CONTRIBUTING.md)。编码 Agent 应从
[`AGENTS.md`](../AGENTS.md) 开始。

## 许可证

[MIT License](../LICENSE)

---

<div align="center">

[depsilo.com](https://depsilo.com)

</div>
