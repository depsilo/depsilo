# T15 预热任务化

- `POST /api/v1/admin/cache/warmup` 现在返回 `job_id` 和 `queued` 状态；每个
  任务最多 100 个规范化的 PyPI/npm 包，进程内最多保留 32 个任务。
- 新增 `GET /api/v1/admin/cache/warmup/:id` 查询任务状态和逐包结果，及
  `DELETE /api/v1/admin/cache/warmup/:id` 协作式取消。任务绑定创建者，
  readonly 只能读取自己的脱敏状态，不能取消或提交任务。
- 状态使用 `queued`、`running`、`cancelling`、`succeeded`、`partial`、
  `failed`、`interrupted`；成功项明确为 metadata cached，未把未知子依赖
  或制品下载伪装成进度。已有预热实现继续负责来源选择、策略链路和缓存写入。
- 任务仅保存在内存，服务重启后查询不到旧任务；运行时关闭通过已有
  `asyncruntime` 取消上下文，不静默重试或回滚已完成写入。

现有预热 API 的 `message`/`packages` 字段保留兼容性，同时增加任务字段。
新增状态和取消接口为 T16 进度 UI 提供真实数据源。

