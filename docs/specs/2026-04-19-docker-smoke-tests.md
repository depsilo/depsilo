# Docker 冒烟测试（E2E Smoke Tests）

## 目标

验证 Depsilo 在 Docker 容器中部署后，真实客户端（pip / npm / go / apt）能通过代理完成包安装。

## 架构

两个容器通过 Docker Compose 编排：

- **depsilo**：从项目 Dockerfile 构建，配置真实上游镜像站
- **e2e-runner**：基于 ubuntu:22.04，安装 pip/npm/go/apt，执行测试脚本

e2e-runner 通过 HTTP 访问 depsilo:23333，执行包安装命令，退出码决定测试成败。

## 文件结构

```
tests/e2e/
├── docker-compose.e2e.yml    # 编排：depsilo + e2e-runner
├── config.e2e.toml           # Depsilo 配置（真实上游，认证关闭）
├── runner/
│   ├── Dockerfile            # e2e-runner 镜像（pip + npm + go + apt）
│   └── run-tests.sh          # 测试脚本
```

## 测试用例

| # | 生态 | 代理类型 | 安装命令 | 验证方式 |
| - | ---- | -------- | -------- | -------- |
| 1 | pip | URL 重写 | `pip install requests` | `python3 -c "import requests"` 退出码 0 |
| 2 | npm | JSON 重写 | `npm install lodash` | `node -e "require('lodash')"` 退出码 0 |
| 3 | Go | Passthrough | `go install golang.org/x/text/cmd/gotext@latest` | 退出码 0 |
| 4 | apt | Passthrough（GPG） | `apt-get update && apt-get install -y jq` | `jq --version` 退出码 0 |

## config.e2e.toml

```toml
[server]
host = "0.0.0.0"
port = 23333

[database]
driver = "sqlite"
dsn = "/app/data/depsilo.db"

[storage]
type = "local"
path = "/app/data/cache"

[cache]
max_size_gb = 5
ttl_index = "5m"
ttl_blob = "72h"
lru_threshold = 90

[auth]
enabled = false

[[pypi.upstreams]]
name = "tuna"
url = "https://pypi.tuna.tsinghua.edu.cn"
priority = 1

[[pypi.upstreams]]
name = "official"
url = "https://pypi.org"
priority = 2

[[npm.upstreams]]
name = "npmmirror"
url = "https://registry.npmmirror.com"
priority = 1

[[npm.upstreams]]
name = "official"
url = "https://registry.npmjs.org"
priority = 2

[[go.upstreams]]
name = "goproxy-cn"
url = "https://goproxy.cn"
priority = 1

[[go.upstreams]]
name = "official"
url = "https://proxy.golang.org"
priority = 2

[[apt.upstreams]]
name = "tuna"
url = "https://mirrors.tuna.tsinghua.edu.cn"
priority = 1

[[apt.upstreams]]
name = "official"
url = "http://archive.ubuntu.com"
priority = 2
```

## docker-compose.e2e.yml

```yaml
services:
  depsilo:
    build:
      context: ../..
      dockerfile: Dockerfile
    volumes:
      - ./config.e2e.toml:/app/config.toml
    environment:
      - DEPSILO_CONFIG=/app/config.toml
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:23333/health"]
      interval: 2s
      timeout: 5s
      retries: 15

  e2e-runner:
    build:
      context: ./runner
    depends_on:
      depsilo:
        condition: service_healthy
    environment:
      - DEPSILO_URL=http://depsilo:23333
```

## runner/Dockerfile

基于 ubuntu:22.04，安装：
- python3 + pip
- node.js + npm
- golang
- apt（已内置）

## runner/run-tests.sh

脚本逻辑：

1. 等待 depsilo 就绪（`/health` 返回 200，最多 30s）
2. **pip 测试**：配置 `pip --index-url $DEPSILO_URL/pypi/simple/`，安装 requests，验证 import
3. **npm 测试**：配置 `npm --registry $DEPSILO_URL/npm/`，安装 lodash，验证 require
4. **go 测试**：设置 `GOPROXY=$DEPSILO_URL/go,direct`，go install golang.org/x/text/cmd/gotext@latest
5. **apt 测试**：替换 sources.list 指向 `$DEPSILO_URL/apt/ubuntu`，apt-get update + install jq

每步打印 PASS/FAIL，最终汇总。任一步 FAIL 则脚本以非零退出。

## Makefile 集成

```makefile
test-e2e:
	docker compose -f tests/e2e/docker-compose.e2e.yml up --build --abort-on-container-exit --exit-code-from e2e-runner
	docker compose -f tests/e2e/docker-compose.e2e.yml down -v
```

## 不在范围内

- 认证流程测试
- Docker Registry 代理测试
- S3 存储后端测试
- GitHub Actions CI 集成（后续单独添加）
- 性能/负载测试
