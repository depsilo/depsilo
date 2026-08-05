# 0002 — Access log storage: pre-aggregated rollup tables, not a column store

## Context

`internal/db/AccessLog` 是 Depsilo 增长最快的表：每个代理请求一行（11 字段，含 `cache_key` `package_name` `client_ip` 等字符串），在 100K req/天的负载下大约 50 MB/天、年线性增长到 18 GB。所有仪表盘 / 带宽报表 / mcp 统计端点都对它做时间窗口聚合（COUNT/SUM/AVG + GROUP BY），导致 SQLite 每次 dashboard 请求都要扫一遍时间窗内的全部原始行。

矛盾点：

- **SQLite 行存的痛**：每行 ~256–1024 字节，重复字符串（`pypi`、上游名、IP）没有字典编码 → 浪费磁盘。
- **查询痛**：固定维度的聚合（adapter × hit × upstream × package × date）每次都全表扫。
- **产品约束**：Depsilo 的核心 slogan 是 "10 分钟部署、单二进制"。任何破坏单二进制部署（CGO 依赖、外部组件）的方案都会损害产品定位。

## Decision

引入预聚合（rollup）表 + retention 策略，**不换存储引擎**。

- 增量维护三张 rollup 表：`access_log_hourly` / `access_log_daily` / `access_log_package_daily`。
- 写入路径：异步批量 Recorder，5 秒窗口内内存聚合后 UPSERT 到 hourly + package_daily。
- 后台任务：daily compactor（hourly → daily）+ retention（清理原始 + 老 rollup）。
- 查询路径：所有 dashboard / bandwidth / stats / mcp 聚合查询切到 rollup，原始 `AccessLog` 仅服务"近期访问日志"明细页面。
- 原始 `AccessLog` 默认保留 7 天，rollup 保留 365 天。

实施细节见 [2026-06-26-access-log-rollup spec](../specs/2026-06-26-access-log-rollup.md)。

## Alternatives considered

### A. 引入 DuckDB（嵌入式列存）替代 AccessLog

**优点**：列存 + 字典编码，存储压缩 10–20×；向量化执行，聚合 5–10×。

**否决理由**：

1. `go-duckdb` 驱动需要 CGO。Depsilo 当前是纯 Go 单二进制（`cmd/server` 用 `glebarez/sqlite` 这个 pure-go SQLite 驱动恰好就是为此），引入 CGO 会破坏 `CGO_ENABLED=0` 的静态二进制构建，影响 Docker 镜像体积和跨平台编译矩阵。
2. 收益边际：本场景 90% 查询是固定维度的小数据集聚合，rollup 后查询表只有几十到几千行，列存的向量化优势体现不出来。
3. DuckDB 不擅长高频小写入，仍需在前面挂一层 batch buffer —— 等于 rollup 方案的写入侧没省，反而引入了存储引擎。

### B. 引入 ClickHouse / TDengine（外部 OLAP）

**优点**：50–100× 压缩，聚合极快。

**否决理由**：直接违反轻量、单实例、自托管的当前运行约束（见
`PRODUCT.md`）。Depsilo 当前规模目标远不到外部 OLAP 的甜区；若未来需要，
应在新的 ADR 中重新定义部署与数据权威，而不是把它隐式塞进现有接口。

### C. Parquet 文件冷归档（保留 SQLite 热）

**优点**：超 N 天的原始日志归档为 Parquet 落地，配合 DuckDB CLI 临时查询历史，热路径仍是 SQLite。

**否决理由**：

- 复杂度高（写归档管道、Parquet schema 维护、历史查询入口设计）。
- 不解决"查询慢"的核心问题 —— 只解决"存储多"。
- 不阻塞 rollup 方案。本 ADR 落地后，Parquet 归档可作为后续 P2 增量功能。

### D. 行存 SQLite + 只加索引 + 不引入 rollup

**优点**：零代码改动，加几个复合索引就能让 Count/SUM 走 index。

**否决理由**：

- 索引解决不了"存储无上限增长"。一年 18 GB 是文件大小问题，不是查询问题。
- 复杂聚合（多维 GROUP BY）即使有索引仍是全索引扫描，对 90 天范围还是慢。
- 不解决"客户端 IP 等高基数字符串重复存"的浪费。

### E. 直接换 PostgreSQL + 分区表 + BRIN 索引

**优点**：PG 的时间分区 + BRIN 对时序数据效果显著；保留 GORM 与现有 SQL 兼容。

**否决理由**：

- 同样破坏单二进制部署（需运维 PG）。
- 本质还是行存，没解决字符串重复。
- 项目当前根本没接 PG（`repository.go:18` 只支持 sqlite）。先把 sqlite 做好。

## Why three rollup tables, not one

`access_log_hourly` + `access_log_daily` + `access_log_package_daily` 而非单表的理由：

1. **维度爆炸防护**：把 `package_name` 放进 hourly 会让最坏情况一天上百万行（`24 × 13 ecosystems × N packages × 2 hit × M upstreams`）。包粒度独立成天表，乘数砍掉一半。
2. **查询模式天然分离**：top_packages 不关心 upstream，dashboard 的 today 不关心 package_name。三表各服务不同查询，每个查询都只扫自己关心的最少行。
3. **daily 是 hourly 的 compact**：90 天查询走 daily 表，最多扫 `90 × 13 × 2 × M_upstreams ≈ 几千行`；同样的查询走 hourly 要 `24 × 90 × ...`，多扫一个 24×。

## Why UTC for the Date column

- `db.Open` 里 `NowFunc: func() time.Time { return time.Now().UTC() }`（`repository.go:38`），原始 `AccessLog.CreatedAt` 已经全 UTC。Rollup 跟着走最一致。
- 跨时区一致性：避免运维同事在不同地区部署时看到"今天"的边界不一样。
- 代价：北京时间用户看到的 "today" 与服务器 UTC 错位 8 小时。短期接受，未来加 server-side display timezone 配置即可。

## Consequences

**好处**：

- 一年存储从 ~18 GB 降到 ~400 MB 封顶。
- Dashboard 聚合查询从全表扫降为索引扫几十行。
- 单二进制部署、零外部依赖、纯 Go、不破坏现有构建链。
- 新生态加入时无需关心日志写入路径（`adapter.LogAccess` 接口不变）。

**代价**：

- 写入路径多了一层 batch + 内存聚合 + UPSERT，复杂度上升。
- 内存里持有最多 5 秒的事件，进程崩溃会丢这段窗口。访问日志容忍丢失，**接受这个 tradeoff**。
- handler 改造涉及 5–7 个文件的聚合 SQL 重写。
- "现在时刻"以下的日志（不到一小时的窗口）：rollup 还没 flush 时数据滞后。`kpiSeries`（5 分钟桶）仍走原始表绕过这个问题，dashboard 的 "today" 可接受 5 秒延迟。

**新增依赖**：无。GORM 的 `clause.OnConflict` 已经支持跨 sqlite/postgres 的 UPSERT。

## Open questions

1. **Postgres 适配**：本方案的 backfill SQL 用了 sqlite 专属 `strftime`。若未来项目开 PG 支持，需要 dialect 分支。建议放 `internal/accesslog/sql_sqlite.go` 和未来的 `sql_postgres.go`。
2. **Top-N 包名截断**：极端用户单天可能万级 distinct 包名。本方案不限制 —— 先观察 `access_log_package_daily` 行数，必要时改成 "保留 TopN + others 合并"。
3. **5 秒 flush 间隔**：可调。高 QPS 场景可能需要降到 1 秒 + 增大 batch size。配置已暴露（`access_log.batch_interval`）。

## Follow-ups

- 实施 spec：[2026-06-26-access-log-rollup.md](../specs/2026-06-26-access-log-rollup.md)。
- 后续 ADR（视情况）：
  - 若包名维度爆炸成为问题 → ADR-N: TopN truncation for package daily rollup。
  - 若引入 PG 支持 → 通用 dialect 抽象层（不限 accesslog）。
  - 若需要历史长尾查询 → ADR-N: Parquet cold archive。
