# Depsilo AI Agent 工具箱嵌入方案设计

> 日期：2026-04-28
> 角色：产品设计师
> 范围：Depsilo 作为基础缓存组件嵌入 Claude Code / Hermes Agent 等 AI 编码工具

---

## 一、背景与目标

### 要解决的问题

AI 编码代理（Claude Code、Hermes Agent 等）在生成代码时，频繁执行包管理器命令：

```
pip install requests numpy torch   # Python
npm install express react          # Node.js
go get github.com/gin-gonic/gin   # Go
cargo add serde                    # Rust
```

每次 `install` 都从公网下载，在 AI 工作流中产生两个问题：

| 问题 | 影响 |
|------|------|
| **延迟累积** | 每次 `pip install` 2-30 秒，一个 agent session 可能有 5-20 次 = 数分钟等待 |
| **重复下载** | 不同 session 下载同一包，带宽浪费 + 时间浪费 |
| **网络不稳定** | 上游挂掉时 build 中断，打断 agent 工作流 |
| **离线不可用** | 无网环境无法工作 |

### 目标

让 Depsilo **透明地嵌入**到 AI 编码工具的工作流中，实现：

1. 零手动配置：agent 启动时自动拉起 Depsilo
2. 零感知延迟：所有包管理操作自动走缓存
3. 跨 session 持久缓存：重启后缓存依然有效
4. 离线可用：即使公网中断，已缓存包仍然可用

### 衡量标准

| 指标 | 目标 |
|------|------|
| 首次安装加速 | ≥2x（首次回源有限）|
| 重复安装加速 | ≥10x（纯本地缓存）|
| 用户手动操作数 | 0（完全自动）|
| Depsilo 内存占用 | ≤50MB |
| 启动时间 | ≤1 秒（Go 二进制）|

---

## 二、方案对比：四种集成深度

### Level 1: 手动 Sidecar（当前最简）

用户手工在另一个终端或后台启动 Depsilo，AI agent 通过环境变量感知。

```bash
# 终端 1: 启动 Depsilo
docker run -d -p 23333:23333 depsilo/depsilo

# 终端 2: 启动 AI agent
export PIP_INDEX_URL=http://localhost:23333/pypi/simple/
export GOPROXY=http://localhost:23333/go,direct
claude
```

**优点：** 零代码改动
**缺点：** 用户需要知道并记住启动 Depsilo，无法自动注入环境变量

### Level 2: Agent 技能/插件自动注入（推荐起始）

AI agent 启动时自动检测并启动 Depsilo，注入环境变量。

- **Hermes Agent**: 一个 Depsilo skill，启动时执行
- **Claude Code**: Hooks / 自定义指令

```python
# Hermes Agent skill 的行为
1. 检查 Depsilo 是否运行 (GET /health)
2. 如果未运行，自动启动 (docker run 或本地二进制)
3. 设置 PIP_INDEX_URL / GOPROXY 等环境变量
4. 提供管理命令: `depsilo status`, `depsilo flush`
```

**优点：** 用户零操作，对 agent 透明
**缺点：** 需要每个 agent 各自实现集成

### Level 3: MCP 服务器（跨平台集成）

Depsilo 作为一个 [MCP (Model Context Protocol)](https://modelcontextprotocol.io) 服务器运行，向 AI agent 暴露工具调用接口。

Claude Desktop / Claude Code / Hermes Agent / 任何 MCP 客户端都可以连接。

```
depsilo-mcp-server  ← MCP stdio/HTTP →  AI Agent
    │
    ├─ Tool: get_proxy_config()        → 返回各生态配置
    ├─ Tool: check_cache_status()      → 缓存命中率/大小
    ├─ Tool: warmup_package(eco, pkg)  → 预热特定包
    ├─ Tool: flush_cache(eco)          → 清除缓存
    ├─ Resource: depsilo://status      → 实时服务状态
    ├─ Resource: depsilo://packages/   → 已缓存包列表
    └─ Resource: depsilo://stats       → 统计指标
```

**优点：** 一次实现，所有 MCP 客户端可用；模型可直接操作缓存
**缺点：** 需要实现 MCP 协议层，Depsilo 本身仍是独立进程

### Level 4: Go 嵌入式 Library（深度集成）

将 Depsilo 缓存的**核心逻辑**提取为 Go 库，可以直接被导入使用。

```go
import "github.com/depsilo/depsilo/cache"

// 在 AI agent 中直接使用
cacher, _ := cache.New(cache.Config{
    Store: cache.NewLocalStore("./data/cache"),
})

// 透明代理任何 HTTP 请求
resp, _ := cacher.Fetch(ctx, "https://pypi.org/simple/requests/")
// 响应已缓存，下次直接返回
```

或者：Depsilo 本身提供 **嵌入式模式**，启动时不监听端口，而是暴露 Go API：

```go
import "github.com/depsilo/depsilo/embed"

// AI agent 内部启动
depsilo, _ := embed.Start(embed.Config{
    CachePath: "./data/cache",
    MaxSizeGB: 5,
})

// 自动配置环境变量
depsilo.ApplyEnv() // 设置 PIP_INDEX_URL, GOPROXY 等

// 调用子进程时自动使用
exec.Command("pip", "install", "requests") // 自动走缓存
```

**优点：** 极致集成，零额外进程，最小资源占用
**缺点：** Go 依赖要求（AI agent 也必须是 Go 项目），耦合度高

---

## 三、推荐架构：Level 2 + Level 3 组合

### 为什么选这个组合

| 维度 | Level 1 | Level 2 | Level 3 | Level 4 |
|------|---------|---------|---------|---------|
| 用户零操作 | ❌ | ✅ | ✅ | ✅ |
| 跨平台兼容 | ✅ | ❌ (各 agent 实现) | ✅ (MCP 标准) | ❌ (仅 Go) |
| 实现成本 | 0 | 低 | 中 | 高 |
| 功能丰富度 | 低 | 中 | 高 | 极高 |
| 维护负担 | 0 | 低 | 中 | 高 |

**Level 2** 解决 "零操作启动" 问题（最痛点）
**Level 3** 解决 "跨平台兼容" 问题（长期价值）
两者互补，不冲突

### 整体架构

```
┌──────────────────────────────────────────┐
│           AI Agent 会话                   │
│                                          │
│  ┌──────────────────────────────┐       │
│  │   Hermes Agent / Claude Code  │       │
│  │                              │       │
│  │   ┌─────────────────┐       │       │
│  │   │  Depsilo Skill   │       │       │
│  │   │  (Level 2)       │       │       │
│  │   │  ◦ 自动启动      │       │       │
│  │   │  ◦ 环境变量注入  │       │       │
│  │   │  ◦ 状态检查      │       │       │
│  │   └────────┬────────┘       │       │
│  │            │ MCP 客户端      │       │
│  └────────────┼─────────────────┘       │
│               │                         │
└───────────────┼─────────────────────────┘
                │
    ┌───────────┴──────────────┐
    │   Depsilo 服务            │
    │                          │
    │  ┌──────────────────┐  │
    │  │  HTTP 代理 Server  │  │
    │  │  (已实现)           │  │
    │  │  :23333             │  │
    │  └────────┬─────────┘  │
    │           │             │
    │  ┌────────┴─────────┐  │
    │  │  MCP Server       │  │
    │  │  (新增)            │  │
    │  │  :23334            │  │
    │  └──────────────────┘  │
    │                          │
    │  ┌──────────────────┐  │
    │  │  缓存引擎 + 存储   │  │
    │  │  (已实现)           │  │
    │  └──────────────────┘  │
    └──────────────────────────┘
```

---

## 四、Level 2: Agent 技能/插件设计

### 4.1 Hermes Agent 技能设计

创建一个 `depsilo` 技能，放在 `~/.hermes/skills/depsilo/` 下，功能如下：

#### 技能触发条件

- 用户安装该技能后，每次 Hermes Agent 启动时自动激活
- 或用户显式输入 `depsilo enable` / `depsilo start`

#### 技能行为

```python
on_session_start:
  1. 检查 Depsilo 是否运行
     → GET http://localhost:23333/health
     → 如果返回 200，跳转到步骤 4

  2. 如果未运行，尝试启动
     → 优先: 本地二进制 (which depsilo)
     → 次选: docker run (which docker)
     → 最后: 提示用户安装 (提供安装命令)

  3. 等待 Depsilo 就绪
     → 轮询 /health，最多等 5 秒

  4. 注入环境变量
     → export PIP_INDEX_URL=http://localhost:23333/pypi/simple/
     → export PIP_TRUSTED_HOST=localhost
     → export GOPROXY=http://localhost:23333/go,direct
     → export npm_config_registry=http://localhost:23333/npm/
     → 等等（按已配置的生态决定）

  5. 检查缓存状态
     → 报告缓存命中率和大小
     → "Depsilo ready — 2.3GB cached, 87% hit rate"

on_command:
  depsilo status  → 显示缓存状态、命中率、各生态统计
  depsilo warmup [eco] [pkg] → 预热特定生态或包
  depsilo flush [eco] → 清除缓存
  depsilo config → 显示当前代理配置
```

#### 技能文件清单

```
~/.hermes/skills/depsilo/
├── SKILL.md           # 技能定义
└── scripts/
    ├── start.sh       # 启动 Depsilo 的逻辑
    └── check.sh       # 检查 Depsilo 状态的逻辑
```

### 4.2 Claude Code 集成方案

Claude Code 没有 Hermes 的技能系统，但可以通过以下方式集成：

#### 方案 A: Claude Code Hook

在项目根目录创建 `.claude/hooks/init.sh`:

```bash
#!/bin/bash
# 自动启动 Depsilo (如果未运行)
if ! curl -sf http://localhost:23333/health > /dev/null 2>&1; then
  if command -v depsilo &> /dev/null; then
    depsilo start --daemon
  elif command -v docker &> /dev/null; then
    docker run -d --name depsilo -p 23333:23333 depsilo/depsilo
  fi
fi

# 注入环境变量
export PIP_INDEX_URL=http://localhost:23333/pypi/simple/
export PIP_TRUSTED_HOST=localhost
export GOPROXY=http://localhost:23333/go,direct
```

#### 方案 B: Claude Code 自定义指令

在 `CLAUDE.md` 或项目指令中添加：

```
When running package manager commands (pip install, npm install, go get, etc.),
the Depsilo proxy is available at http://localhost:23333.
Ensure environment variables are set:
  PIP_INDEX_URL=http://localhost:23333/pypi/simple/
  GOPROXY=http://localhost:23333/go,direct
  npm_config_registry=http://localhost:23333/npm/
```

### 4.3 通用 Shell 方案

提供一个 `depsilo-activate.sh` 脚本，source 即可一键激活：

```bash
# 在 .bashrc / .zshrc 中添加
eval "$(depsilo activate)"

# 或手动
source <(depsilo activate)
```

`depsilo activate` 的输出：

```bash
# 检查并启动 Depsilo
if ! curl -sf http://localhost:23333/health &>/dev/null; then
  # 尝试启动
  depsilo start --daemon
fi

# 设置环境变量
export PIP_INDEX_URL=http://localhost:23333/pypi/simple/
export PIP_TRUSTED_HOST=localhost
export GOPROXY=http://localhost:23333/go,direct
export npm_config_registry=http://localhost:23333/npm/

# 打印状态
echo "✓ Depsilo proxy active — http://localhost:23333"
```

---

## 五、Level 3: MCP 服务器设计

### 5.1 什么是 MCP

Model Context Protocol（MCP）是 Anthropic 推出的模型上下文协议，允许 AI 应用和模型交互。

- **Claude Desktop**: 原生支持 MCP 服务器
- **Claude Code**: 支持 MCP
- **Hermes Agent**: 通过 `native-mcp` 技能支持 MCP
- **VS Code + Cline / Continue.dev**: 支持 MCP

一个 MCP 服务器实现后，所有这些客户端都能用。

### 5.2 Depsilo MCP 服务器设计

#### 端点

| 端点 | 类型 | 说明 |
|------|------|------|
| `depsilo://status` | Resource | 服务健康、缓存大小、命中率 |
| `depsilo://packages/list` | Resource | 已缓存的包列表 |
| `depsilo://packages/{eco}/{name}` | Resource | 特定包缓存信息 |
| `depsilo://stats` | Resource | 各生态统计 |
| `depsilo://ecosystems` | Resource | 已启用的生态列表 |

#### 工具

| 工具 | 参数 | 说明 |
|------|------|------|
| `get_proxy_config` | `ecosystem?: string` | 返回指定生态的代理配置命令 |
| `check_cache_status` | 无 | 返回缓存命中率、大小、文件数 |
| `warmup_package` | `ecosystem, package` | 预热指定包到缓存 |
| `flush_cache` | `ecosystem?: string` | 清除缓存（可指定生态） |
| `install_configured` | `ecosystem` | 输出该生态的安装配置命令 |
| `apply_proxy_env` | 无 | 输出需设置的环境变量 |

#### 传输协议

- **stdio**: 用于 Claude Code / 本地 agent，通过子进程启动
- **HTTP/SSE**: 用于 Claude Desktop / 远程连接，服务端模式

#### 启动方式

```bash
# 内嵌在 Depsilo 主进程中 (推荐)
depsilo start --mcp        # 在 :23334 启动 MCP 服务器
depsilo start --mcp-stdio  # 通过 stdin/stdout 提供 MCP

# Claude Desktop 配置
# ~/.claude/servers.json
{
  "depsilo": {
    "command": "depsilo",  // 或 docker run depsilo/depsilo mcp
    "args": ["mcp", "--stdio"]
  }
}

# Claude Code
claude --mcp '{"depsilo": {"command": "depsilo", "args": ["mcp", "--stdio"]}}'
```

### 5.3 MCP 工具的 AI 交互模式

当用户或 AI agent 需要安装包时：

```
用户: "帮我创建一个 Flask 项目"

Agent 内部:
1. Agent 意识到需要 pip install
2. Agent 调用 MCP 工具 get_proxy_config("pypi")
   → 返回: "配置 pip 使用 http://localhost:23333/pypi/simple/"
3. Agent 设置 PIP_INDEX_URL 环境变量
4. Agent 执行 pip install flask
   → pip 自动走 Depsilo 缓存
   → 首次: 回源 pypi.org + 缓存 (3秒)
   → 后续: 本地缓存 (0.1秒)
5. 如果需要更多包，Agent 可调用 warmup_package 预取
```

### 5.4 实现方案

#### 方案 A: Depsilo 内置 MCP 服务器（推荐）

在 `cmd/server/` 中新增 MCP 服务器，与现有 HTTP 服务器并行运行：

```
internal/
├── mcp/
│   ├── server.go       # MCP 服务器启动
│   ├── tools.go        # MCP 工具实现
│   ├── resources.go    # MCP 资源实现
│   └── transport.go    # stdio / HTTP 传输
```

**优点：** 复用所有内部依赖（缓存、DB、配置），无额外进程
**成本估算：** ~300-400 行 Go 代码

#### 方案 B: 独立 MCP 代理（更简单）

提供一个轻量 MCP 适配器，将现有 REST API 包装成 MCP 协议。

```bash
depsilo-mcp --api-url http://localhost:23333
```

**优点：** 不修改 Depsilo 核心代码
**缺点：** 需要额外维护一个进程

---

## 六、集成配置总览

### 6.1 用户安装步骤

| 集成方式 | 步骤数 | 典型命令 |
|----------|--------|----------|
| **手动 Sidecar** | 2 | `docker run depsilo` + 设置 env |
| **Hermes Skill** | 1 | `hermes skill install depsilo` |
| **Claude Code Hook** | 1 | 复制脚本到 `.claude/hooks/` |
| **MCP 服务器** | 1 | 添加到 `claude_desktop_config.json` |
| **Shell Activate** | 1 | `echo 'eval "$(depsilo activate)"' >> ~/.zshrc` |

### 6.2 配置项

```toml
# 新增 config.toml 配置
[agent_integration]
# 自动注入环境变量到子进程
auto_inject_env = true

# 允许注入的生态列表
enabled_ecosystems = ["pypi", "npm", "go", "cargo", "apt"]

# MCP 服务器端口 (0=禁用)
mcp_port = 23334

# 启动时预热热门包列表
warmup_on_start = false
warmup_packages = [
  { ecosystem = "pypi", packages = ["requests", "numpy", "torch"] },
  { ecosystem = "npm", packages = ["express", "react", "lodash"] },
]
```

### 6.3 环境变量表

AI agent 需要设置的变量（由集成层自动注入）：

| 生态 | 变量 | 值 |
|------|------|----|
| PyPI | `PIP_INDEX_URL` | `http://localhost:23333/pypi/simple/` |
| PyPI | `PIP_TRUSTED_HOST` | `localhost` |
| npm | `npm_config_registry` | `http://localhost:23333/npm/` |
| Go | `GOPROXY` | `http://localhost:23333/go,direct` |
| Cargo | 配置文件修改 | `~/.cargo/config.toml` 中 `[source.depsilo]` |
| Maven | `MAVEN_OPTS` | `-Dmaven.repo.remote...` |
| Conda | `CONDA_CHANNELS` | `http://localhost:23333/conda/...` |

---

## 七、实现路线图

### Phase 1: Hermes Agent 技能（1-2 天）

```
P1a. 编写 depsilo SKILL.md
P1b. 实现 start/check 脚本
P1c. 实现环境变量注入逻辑
P1d. 测试：Hermes Agent 启动时自动拉起 Depsilo
```

### Phase 2: 嵌入式启动 CLI（2-3 天）

```
P2a. 新增 depsilo start --daemon 命令
P2b. 新增 depsilo status 命令
P2c. 新增 depsilo activate 命令（输出 shell 配置）
P2d. 新增 depsilo mcp --stdio 子命令
P2e. 新增 depsilo warmup 命令
```

### Phase 3: MCP 服务器（3-5 天）

```
P3a. 实现 MCP 协议基础框架 (internal/mcp/)
P3b. 实现 Resources: status, packages, stats
P3c. 实现 Tools: get_proxy_config, check_cache, warmup, flush
P3d. 集成到 Depsilo 主进程启动路径
P3e. 编写 mcp 子命令
P3f. 测试：Claude Desktop 连接验证
```

### Phase 4: 文档与推广（1-2 天）

```
P4a. README 添加 AI agent 集成章节
P4b. 编写 Claude Code Hook 示例
P4c. 编写 MCP 配置示例
P4d. 录制演示：AI agent → pip install → Depsilo 秒级响应
```

---

## 八、风险与注意

### 兼容性

- **MCP 协议版本**: MCP 协议仍在演进（当前 2025-03），需关注兼容性
- **Shell 环境变量**: 注入的环境变量只在当前进程有效，子进程继承问题需测试
- **Docker vs 本地二进制**: 优先二进制（更快、无依赖），备选 Docker

### 性能影响

- Depsilo 本身约 50MB 内存，对 AI agent 几乎无影响
- MCP stdio 模式在 agent 进程内启动，0 额外网络延迟
- HTTP 模式的 Depsilo 代理引入 <1ms 本地延迟

### 安全考虑

- 本地 Depsilo 默认监听 localhost，不暴露给外部
- 环境变量仅配置本地地址，无凭据泄露风险
- MCP 工具不应暴露危险操作（无验证的远程清除等）

### 已知限制

- **Cargo 和 Maven** 需要配置文件修改，不能仅靠环境变量注入
- **apt** 需要 root 权限修改 `/etc/apt/sources.list`
- 首次请求仍然回源（无缓存时），预热可缓解

---

## 九、总结与建议

### 推荐优先级

```
Phase 1 (Hermes Skill) ──── 立即开始，1-2 天
         │
         ▼
Phase 2 (CLI 增强) ──────── 紧接着，2-3 天
         │
         ▼
Phase 3 (MCP Server) ────── 有条件时，3-5 天
         │
         ▼
Phase 4 (文档推广) ──────── 贯穿始终
```

### 核心定位

Depsilo 不是另一个需要运维的服务，而是 **AI 开发者的"隐形加速器"**——它应该自动运行、自动配置、被开发者遗忘，只有拿到结果时才会意识到它存在。

### 一句话卖点

> "让你的 AI 编码助手告别等待 `pip install` 的每一天。"

---

*文档版本：v1.0*
