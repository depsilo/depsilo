# T12 发布候选证据

候选 commit：`202c2d87f761de7e3c4bd7406cffc617b1af03ab`  
分支：`master`  
记录时间：2026-09-09 UTC  

这份记录把“代码已完成”和“具备发布条件”分开。所有未在该候选
commit 上执行的项目都保留为 `NOT_RUN`。

| 检查项 | 状态 | 证据 |
| --- | --- | --- |
| `make verify`（Go、race、集成、前端、完整 Playwright、脚本） | PASS | 本地运行；260 个 Playwright 测试通过 |
| 生产嵌入 UI | PASS | `make test-ui-production`；Go 二进制嵌入前端冒烟通过 |
| GoReleaser 配置 | PASS | `make release-check` |
| v0.9.0 源码升级 | PASS | `scripts/test-v090-upgrade.sh`；schema v4、凭据和来源证明恢复通过 |
| v0.9.1 镜像/状态升级 | PASS | `scripts/test-v091-image-upgrade.sh`；固定镜像 digest 和安全规则迁移通过 |
| v0.9.0 Compose bind 布局升级 | NOT_RUN | 仅运行了准备状态的安全测试；完整 Docker/宿主机门禁待执行 |
| 14 个真实包管理器客户端 | NOT_RUN | `make test-e2e` 需要网络和 Docker |
| Docker Registry dind | NOT_RUN | `make test-docker-docker` 需要特权 Docker |
| ccache/sccache 资格测试 | NOT_RUN | `make test-compiler-cache-qualified` 需要客户端和 Docker |
| S3/MinIO | NOT_RUN | `make test-s3` 需要 Docker |
| 在线依赖安全扫描 | NOT_RUN | `make security` 需要网络 |
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

