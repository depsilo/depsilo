# T11 实现记录：可复现性能测试入口

- `scripts/benchmark-install.sh` 固定 Python `requests==2.32.3` 与 Node.js `lodash@4.17.21`，记录 `direct_cold`、`depsilo_client_cold`、`depsilo_hot_client_cold` 和 warm client 直连/代理五组原始样本。公网 PyPI/npm 端点与 Depsilo `/pypi/simple/`、`/npm/` 端点独立定义。
- `--runs N` 对每个场景生成恰好 N 个计时尝试；client-cold 样本使用独立临时缓存，Depsilo hot 场景先预热目标服务端再使用新客户端缓存。预热请求不计入计时；结果保留每次状态码、wall time、中位数、范围和失败错误，不计算或宣称加速/节省比例。
- `--dry-run` 只输出 `NOT_RUN` 计划；`scripts/test-benchmark-install.sh` 验证参数、场景隔离和输出格式，并已接入 `make verify-scripts`。
- 脚本测试使用 fake Docker 客户端记录真实参数，覆盖热/冷缓存目录、容器内
  端点探测、样本数、显式本地 HTTP 信任和预热/探测/安装失败；不访问公网、
  不运行真实包管理器，因此真实性能数据仍为 `NOT_RUN`。
- 本环境当前没有运行中的 `localhost:23333` 服务，因此没有生成性能数字；真实测量需在专用新状态服务上运行，例如：

  `scripts/benchmark-install.sh --ecosystem pypi --runs 3 --depsilo-url http://host.docker.internal:23333 --allow-local-http --out pypi-benchmark.json`

  生产或共享环境应使用 HTTPS；`--allow-local-http` 只对本次命令的
  `host.docker.internal`（或明确的本地回环/私网 host:port）启用 pip 信任，
  不修改全局 pip 配置。脚本会把 Docker、客户端、镜像身份、端点和每次命令
  的原始输出保存在报告旁的 evidence 目录。

结果受网络、镜像和上游状态影响，未上传官网或写入发布文案。
