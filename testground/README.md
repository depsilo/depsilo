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

此目标使用两个独立、空镜像缓存的 dind 客户端拉取无前缀的 Docker Hub
镜像，并检查 Depsilo 的请求数及第二个客户端的缓存命中数；镜像客户端回退到
Docker Hub 而未访问 Depsilo 时测试失败。需要本机有 `jq`、`curl` 和特权
Docker 支持。如果 DNS 把 Docker Hub 解析到 `198.18.0.0/15` 等非公网地址，
Depsilo 的出站安全检查会拒绝该请求，Docker 可能回退直连；应使用公网 DNS
或给仓库配置可信的出站代理，不要关闭出站检查。

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

## npm / Python 安装基准

`scripts/benchmark-install.sh` 默认使用 `testground/benchmark/` 的固定输入：
npm 的 lodash 4.17.21 锁文件，以及 Python requests 2.32.3 与四个固定依赖的
纯 Python wheel SHA-256。样例只用于建立可复现的小规模对照，不代表真实项目。

先在独立实例上运行一次功能检查，再在空闲环境至少运行五轮：

```bash
bash scripts/benchmark-install.sh --ecosystem npm --runs 5 \
  --depsilo-url https://depsilo.example --out npm-benchmark.json
bash scripts/benchmark-install.sh --ecosystem pypi --runs 5 \
  --depsilo-url https://depsilo.example --out pypi-benchmark.json
```

`--input-dir DIR` 可替换固定输入；npm 目录需要 `package.json` 和版本 3 的
`package-lock.json`，Python 目录需要每行包含精确版本和 SHA-256 的
`requirements.txt`。不支持 URL 依赖、额外索引、workspace 或本地链接。
Python 使用 `--require-hashes --only-binary=:all:`，不接受源码构建。

npm 锁文件可用以下命令生成，保留精确版本和完整性哈希：

```bash
npm install --package-lock-only --ignore-scripts --no-audit --no-fund \
  --omit-lockfile-registry-resolved
```

生成的锁文件省略 `resolved` 地址。npm 随后向所选 registry 查询下载地址，以取得 Depsilo
签名制品 URL；带固定未签名地址的旧锁文件会被拒绝。
这一行为见 [npm 配置说明](https://docs.npmjs.com/cli/v10/using-npm/config/#omit-lockfile-registry-resolved)。

`--client-image IMAGE` 可选择客户端镜像。脚本优先使用本地镜像，不存在时拉取，
随后所有容器使用同一个不可变仓库摘要（本地构建镜像则使用镜像 ID）；报告保存镜像标识和客户端版本。每次安装
都使用新容器及新安装目录，热客户端场景只复用包管理器下载缓存。镜像预检、
就绪探测和预热不计入耗时，容器启动及安装计入耗时。报告旁保留输入副本、
SHA-256、每个样本的命令输出和耗时中位数、最小值、最大值。

本地 HTTP 必须显式使用 `--allow-local-http`；地址要能从 Docker 容器访问。
服务端缓存由操作者管理，脚本不清空它，也不推断预热后一定命中。所有代理场景
均标记 `server_cache_state: uncontrolled`，`measurement_ready: false` 表示
执行完成尚不能满足四周计划 T04 的全部测量条件。正式结果还需记录服务端版本、
隔离与缓存重置步骤、实际命中和上游读取字节；一次 smoke 不输出提速比例。

### PyTorch 与 vLLM 固定场景

目前准备了三个历史发布版的固定安装输入，不代表推荐生产使用这些版本：

| 输入目录 | 固定根依赖 | 来源 | 平台 |
| --- | --- | --- | --- |
| `benchmark/pytorch-cpu` | `torch==2.8.0+cpu`，共 9 个固定依赖 | PyTorch CPU 专用索引 | Linux amd64 / Python 3.11 |
| `benchmark/pytorch-cu128` | `torch==2.8.0+cu128`，共 25 个固定依赖 | PyTorch CUDA 12.8 专用索引 | Linux amd64 / Python 3.11 |
| `benchmark/vllm-pypi` | `vllm==0.10.2`，共 156 个固定依赖，含 torch 2.8.0 | PyPI | Linux amd64 / Python 3.11 |

`requirements.in` 保存根依赖，`requirements.txt` 保存完整版本与哈希，
`profile.json` 保存解析目标、索引和生成命令。解析使用 uv 0.12.18；uv 多行
requirements 的续行合并为一行。vLLM 哈希进一步筛选为记录的 Python 3.11
Linux x86_64 客户端兼容 wheel，并核对每个哈希存在于原始解析锁文件。
只准备锁文件并不证明所有依赖能经 Depsilo 安装。

```bash
# 首先运行一次功能检查；正式对照改成 --runs 5，使用空闲原生 amd64 主机。
bash scripts/benchmark-install.sh --ecosystem pypi --pytorch-channel cpu \
  --runs 1 --depsilo-url https://depsilo.example --out pytorch-cpu.json
bash scripts/benchmark-install.sh --ecosystem pypi --pytorch-channel cu128 \
  --runs 1 --depsilo-url https://depsilo.example --out pytorch-cu128.json
bash scripts/benchmark-install.sh --ecosystem pypi \
  --input-dir testground/benchmark/vllm-pypi --platform linux/amd64 \
  --runs 1 --depsilo-url https://depsilo.example --out vllm.json
```

PyTorch 直连使用 `https://download.pytorch.org/whl/<channel>/`，代理使用
`/pypi-torch/<channel>/simple/` 的默认预设。服务器若关闭或改名此预设，需要
恢复相应测试配置；脚本不自动编辑实例。vLLM 此场景使用 PyPI 发布包，
不包含 nightly、自定义 CUDA wheel、源码构建或模型权重下载。

`--platform` 明确选择 Docker 容器架构；镜像检查也按该平台执行，需要支持
`docker image inspect --platform` 的 Docker 版本。脚本检查锁定 profile 与
所选平台、渠道及实际 Python 版本是否一致。ARM 主机上的 amd64 模拟运行
只能用于兼容性检查，不作为原生部署的性能证据。CUDA 包的安装检查也不证明
GPU 可运行；推理验收另需匹配驱动、硬件及独立模型场景。

PyTorch 的渠道和历史版本见 [官方安装说明](https://pytorch.org/get-started/previous-versions/)，
vLLM 的环境要求见 [0.10.2 安装说明](https://docs.vllm.ai/en/v0.10.2/getting_started/installation/gpu.html)。

大制品功能检查默认给 pip 设置 `--timeout 600`（socket 等待时间，不是整个
安装的截止时间），以覆盖慢速下载及同一制品正在被另一请求填充缓存时的等待。
可用 `--pip-timeout SECONDS` 调整，范围 1–3600；报告记录实际设置。正式对照
各场景须使用相同超时，串行执行并单独保存错误和重试，不能把增加超时视为提速。

服务端的 PyPI 与 PyTorch 文件下载使用 3 分钟无读取进展超时，不受缓存层默认
10 分钟总时限限制；持续传输的大 wheel 可以超过 10 分钟。索引请求仍使用原有
总时限，连接与响应头等待仍由上游传输层限制。中断或长度不符的文件不会提交缓存。

PyPI 与 PyTorch 文件流中途读取失败时，若初始响应提供已知长度、强 ETag 和
identity 编码，服务端可向同一来源最多续传两次，退避分别为 100、200 ms。
续传使用 `Range` 与 `If-Range`，仅接受最终 URL、ETag、Content-Range 及剩余
长度全部一致的 206 响应。没有强校验器、对象变化、忽略 Range 或返回编码内容时
中止传输，不把重试响应追加到已有字节之后；失败内容不能提交缓存。续传的实际
读取仍计入回源流量。这只覆盖同一对象的中途断流，不代表已支持停滞恢复、持续
慢速切源或跨来源续传。正常慢速传输仍遵循上面的无读取进展超时。

大依赖集的安装功能检查可分为缓存准备和安装两个阶段。先在服务实际运行的网络中，
对同一 wheel 比较候选上游的持续传输速度，再为隔离实例选定一个上游；不要只凭
索引延迟或很小的 Range 响应选源。用最多 4 个客户端分别下载不重叠的固定依赖批次，
全部经过 Depsilo，并保留 `--require-hashes --only-binary=:all: --no-deps`。
ARM 主机可用原生 pip 的 `download` 加目标 Python、ABI 和完整兼容平台标签准备
x86_64 wheel；随后仍需用两个独立的 x86_64 客户端执行完整安装。每个阶段分别记录
耗时、退出状态、制品命中及实际上游流量。此流程只能算显式预热缓存后的功能检查，
不能替代上面的冷安装对照或 GPU 推理检查。
