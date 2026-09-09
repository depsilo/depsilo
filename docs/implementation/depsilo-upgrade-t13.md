# T13 只读缓存清理预览

- `cache.Retention.Preview` 复用现有手动清理的过期优先、再按
  `last_accessed` 的 LRU 顺序和目标水位逻辑；只执行存储大小读取和有界
  数据库查询，不取得删除锁、不 touch TTL、不写入对象或元数据。
- 新增认证只读接口 `GET /api/v1/admin/cache/cleanup/preview`，支持有界
  `page`/`page_size`，返回候选数量、逻辑字节估计、物理使用量、阈值/目标
  水位、生成时间和候选样例。逻辑字节不被描述为可释放文件系统空间。
- Admin 清理弹窗在执行前展示只读摘要和最多 8 个候选样例；预览失败时仍
  明确说明旧清理动作范围，预览不会伪装成精确确认清单。
- `TestRetentionPreviewMatchesManualOrderWithoutMutation` 验证候选顺序、
  目标水位截断和预览前后数据库未变化；`go test ./internal/cache
  ./internal/api/admin ./internal/api`、前端类型检查、ESLint、i18n 和
  `make check` 均通过。

T14 才会讨论把预览与执行绑定以及并发竞态；当前旧的清理操作保留原有
范围和权限。

