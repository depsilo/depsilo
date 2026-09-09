# T09 实现记录：升级预检与备份恢复证据

- 当前候选升级脚本的最终 schema 断言从 3 更新为 4，与 T04 的 `CurrentSchemaVersion` 一致；旧版状态仍从 v0.9.0/v0.9.1 固定 tag 构建。
- `scripts/test-prepare-v090-compose-upgrade.sh` 通过，验证隔离状态准备和备份安全约束。
- `scripts/test-v090-upgrade.sh` 通过：配置、SQLite 身份、密码/JWT/API Token、试用/付费授权、旧 npm 缓存离线失败和新签名来源恢复均保留。
- `scripts/test-v091-image-upgrade.sh` 通过：发布镜像 digest、命名卷布局、配置、schema v4、密码/JWT/API Token、授权和安全包规则迁移均保留。
- 演练均使用脚本创建的临时状态/Compose 项目；没有操作真实运行实例。备份仍明确只包含配置和一致的 SQLite 快照，不包含缓存对象。
