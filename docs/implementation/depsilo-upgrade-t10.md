# T10 实现记录：本地脱敏诊断包

- 新增 `depsilo diagnose [--out FILE] [--json]`，默认写 `depsilo-diagnostic.json`。
- 输出采用固定白名单：CLI/服务版本、健康、就绪检查和已认证能力摘要；枚举值、时间和失败标记再次校验，不输出完整配置、环境变量、凭据、请求头/样本、包名、客户端地址或任意日志文本。
- 每个响应限制为 1 MiB、总采集时间限制为 15 秒，拒绝重定向和带凭据/路径/query/fragment 的服务源；文件使用 `0600` 且不覆盖已有文件。命令不上传或调用第三方服务；能力接口未授权/不可用时只记录省略说明。
- 诊断报告与 `depsilo backup` 的配置/SQLite 恢复包保持不同用途，缓存对象和恢复信息不包含在诊断报告中。

验证：`go test ./internal/cli -run TestDiagnose -count=1`、`go test ./cmd/depsilo -count=1`、`git diff --check`；覆盖来源文本诱饵和不安全服务源。
