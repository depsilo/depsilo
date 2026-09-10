# T11 实现记录：可复现性能测试入口

- `scripts/benchmark-install.sh` 固定 Python `requests==2.32.3` 与 Node.js `lodash@4.17.21`，记录 `direct_cold`、`depsilo_client_cold`、`depsilo_hot_client_cold` 和 warm client 直连/代理五组原始样本。公网 PyPI/npm 端点与 Depsilo `/pypi/simple/`、`/npm/` 端点独立定义。
- `--runs N` 对每个场景生成恰好 N 个计时尝试；client-cold 样本使用独立临时缓存，Depsilo hot 场景先预热目标服务端再使用新客户端缓存。预热请求不计入计时；结果保留每次状态码、wall time、中位数、范围和失败错误，不计算或宣称加速/节省比例。
- `--dry-run` 只输出 `NOT_RUN` 计划；`scripts/test-benchmark-install.sh` 验证参数、场景隔离和输出格式，并已接入 `make verify-scripts`。
- 本环境当前没有运行中的 `localhost:23333` 服务，因此没有生成性能数字；真实测量需在专用新状态服务上运行，例如：

  `scripts/benchmark-install.sh --ecosystem pypi --runs 3 --depsilo-url http://host.docker.internal:23333 --out pypi-benchmark.json`

结果受网络、镜像和上游状态影响，未上传官网或写入发布文案。
