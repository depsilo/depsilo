# T12 发布候选证据

候选 commit：`202c2d87f761de7e3c4bd7406cffc617b1af03ab`  
分支：`master`  
记录时间：2026-09-09 UTC  

## 后续候选复验

预热历史 fail-closed 修复之后，当前候选为 `0f03968`（完整提交为
`0f039680077fcf3ec55b28afee552839b9f42919`）。本次在该候选上重新执行了 `make verify`，全量
Go/race、集成、前端构建、260 条 Playwright 和发布脚本检查均通过。下表中
需要网络、Docker、S3 或真实客户端的项目仍沿用原记录的 `NOT_RUN`，不能用
旧候选或离线门禁替代。

当前候选的离线复验结论仍为 **NO_GO**：代码门禁通过，但发布检查表要求的
外部环境证据尚未在 `0f03968` 上补齐。

本次复查修复仍在工作区，尚未形成候选 commit；下面新增的 R01/R04 证据只
记录本地工作区命令结果，不能移用到上述候选或任何 tag。

下方原始表格记录的是 `202c2d8` 候选上的逐项结果；除已明确写出当前复验的
`make verify` 外，不把这些结果移绑定到 `0f03968`。这份记录把“代码已完成”
和“具备发布条件”分开，所有未在对应候选上执行的项目都保留为 `NOT_RUN`。

| 检查项 | 状态 | 证据 |
| --- | --- | --- |
| `make verify`（Go、race、集成、前端、完整 Playwright、脚本） | PASS | `202c2d8` 本地运行；`0f03968` 已再次运行，260 个 Playwright 测试通过 |
| 生产嵌入 UI | PASS | `make test-ui-production`；Go 二进制嵌入前端冒烟通过 |
| GoReleaser 配置 | PASS | `make release-check` |
| v0.9.0 源码升级 | PASS | `scripts/test-v090-upgrade.sh`；schema v4、凭据和来源证明恢复通过 |
| v0.9.1 镜像/状态升级 | PASS | `scripts/test-v091-image-upgrade.sh`；固定镜像 digest 和安全规则迁移通过 |
| v0.9.0 Compose bind 布局升级 | NOT_RUN | 仅运行了准备状态的安全测试；完整 Docker/宿主机门禁待执行 |
| 14 个真实包管理器客户端 | NOT_RUN | `make test-e2e` 需要网络和 Docker |
| Docker Registry dind | NOT_RUN | `make test-docker-docker` 需要特权 Docker |
| ccache/sccache 资格测试 | NOT_RUN | `make test-compiler-cache-qualified` 需要客户端和 Docker |
| S3/MinIO | NOT_RUN | `make test-s3` 需要 Docker |
| 前端依赖审计（工作区复查） | PASS | 在未提交工作区锁文件上执行 `npm audit --audit-level=moderate`；升级受影响的 `@humanfs/node`、`js-yaml` 和 Vitest 4.1.11 后报告 0 vulnerabilities；需绑定新候选后重跑 |
| 在线 Go 依赖安全扫描 | NOT_RUN | `make security` 需要网络 |
| 二进制备份恢复 | NOT_RUN | 本次未执行 `depsilo backup --out ...` 的人工流程 |
| 安装器、发布工作流脚本 | PASS | `make verify` 中的脚本测试；`test-benchmark-install.sh` dry-run 通过 |
| 安装性能数字 | NOT_RUN | 本地 `/health` 返回 `service-unavailable`，没有生成伪造数据 |

因此当前候选建议为 **NO_GO**：离线代码门禁和已绑定的升级证据通过，
但发布检查表要求的网络、Docker、S3、真实客户端和备份恢复证据仍缺失。
维护者在具备相应环境后，应在同一 commit 上补跑这些项目，再重新作出
GO/NO_GO 判定。此记录不创建 tag、不发布镜像，也不代表官网已更新。

## 复验命令

```bash
make verify
make test-ui-production
make security
make test-e2e
make test-docker-docker
make test-compiler-cache-qualified
make test-s3
make test-v090-compose-upgrade
make release-check
```
