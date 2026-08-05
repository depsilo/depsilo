# testground — 真实客户端端到端测试

该目录保存真实包管理器的 Docker 测试夹具。每个
`docker-<ecosystem>/Dockerfile` 都通过 `ARG DEPSILO_URL` 接收服务地址，
再使用该生态的官方客户端向 Depsilo 发起安装或下载请求。

这些测试会访问真实上游，需要 Docker 和可用网络；存在本地开发配置时会
复用它，否则使用 Depsilo 默认配置。它们不属于离线 `make verify`。

## 运行全部包管理器

```bash
make test-e2e
```

该命令会启动本地 Depsilo，并依次验证 14 个夹具：PyPI、APT、npm、Go、
Cargo、Maven、RubyGems、Composer、NuGet、Conda、CRAN、Alpine、Helm 和
Hugging Face。

完整真实客户端流程只在 GitHub Actions 的定时或手动工作流中运行，不加入
普通 push/PR 验证；本地仍可用同一个 `make test-e2e` 入口复现。

## 只运行一个生态

迭代某个适配器时，优先运行对应目标：

```bash
make test-docker-pypi
make test-docker-npm
make test-docker-apt
```

所有普通夹具都使用同一规则：

```bash
docker build \
  --build-arg DEPSILO_URL=http://host.docker.internal:23333 \
  --add-host=host.docker.internal:host-gateway \
  testground/docker-<ecosystem>
```

可通过 `DEPSILO_URL` 和 `DOCKER_HOST_ALIAS` 覆盖默认地址。

## Docker Registry（单独启用）

Docker Registry 验证需要在容器中执行真实 `docker pull`，因此使用
Docker-in-Docker 和 `--privileged`，不会包含在默认的 14 生态测试中：

```bash
make test-docker-docker
```

## 编译缓存夹具

`compiler-cache/hello.c` 由独立的 ccache/sccache 兼容性测试使用。该测试需要
已运行的服务、构建凭据以及本机安装的官方客户端：

```bash
COMPILER_CACHE_ENDPOINT=http://localhost:23333/ccache/v1/example \
COMPILER_CACHE_TOKEN='<read-write token>' \
make test-compiler-cache
```

## 清理

```bash
make stop
make test-clean
```

`test-clean` 删除测试镜像和其他端到端测试产物，不会删除项目源文件。

## 新增夹具

1. 新建 `testground/docker-<ecosystem>/Dockerfile`。
2. 只声明 `ARG DEPSILO_URL`，并由它派生该生态的代理路由。
3. 使用真实客户端执行一个小而稳定的安装或下载。
4. 将生态名加入 Makefile 的 `TEST_DOCKER_ALL_ECOS`。
5. 先运行单生态目标，再运行 `make test-e2e`。
