# Access Log Storage v2 — Rollup + Retention

> 状态：草案 · 作者：Claude · 日期：2026-06-26 · 相关 ADR：[0002-access-log-rollup](../adr/0002-access-log-rollup.md)

## 一、问题陈述

`internal/db/AccessLog` 当前以**行存**形式记录每个代理请求（11 个字段：adapter_type、method、cache_key、package_name、hit、upstream、latency_ms、status_code、client_ip、bytes_sent、created_at）。SQLite 行存对该工作负载有两个根本痛点：

1. **存储线性增长无上限**。每行 ~256–1024 字节，重复字符串（`pypi`、上游名、客户端 IP）没有字典编码。100K req/天 ≈ 50 MB/天 → 一年 ~18 GB。
2. **聚合查询代价高**。`internal/api/admin/dashboard.go`、`bandwidth.go`、`public/stats.go`、`public/mcp.go` 共 ~10 个查询全是 `COUNT/SUM/AVG + GROUP BY date|adapter|hit|upstream|package`；每次都要扫描时间窗口内的全部原始行。

**关键观察**：90% 查询都是**固定维度**聚合，唯一的明细查询是 `internal/api/admin/logs.go:21`（分页列表）。这意味着可以预聚合，而无需引入新的列存引擎。

## 二、设计决策

**双轨架构：**

- **热聚合（rollup）表**：增量 UPSERT 维护，固定维度。仪表盘 / 带宽 / stats / mcp 全部走 rollup（毫秒级返回，~MB 量级）。
- **原始 `AccessLog`**：仅服务"近期访问日志"页面（`logs.go`），激进 retention（默认 7 天）。

**不引入新存储引擎**（不上 DuckDB / ClickHouse / Parquet）。理由见 [ADR-0002](../adr/0002-access-log-rollup.md#alternatives-considered)。

## 三、数据模型

新增 3 张表，追加到 `internal/db/models.go`：

```go
// AccessLogHourly 粗粒度小时聚合，不含 package_name，避免维度爆炸。
// 用于 today 视图、kpiSeries（5 分钟桶）、按上游分布。
type AccessLogHourly struct {
    Date         string    `gorm:"size:10;primaryKey" json:"date"`         // "2026-06-26"
    Hour         int       `gorm:"primaryKey" json:"hour"`                  // 0-23 (UTC)
    AdapterType  string    `gorm:"size:16;primaryKey" json:"adapter_type"`
    Hit          bool      `gorm:"primaryKey" json:"hit"`
    Upstream     string    `gorm:"size:128;primaryKey;default:''" json:"upstream"`
    RequestCount int64     `json:"request_count"`
    TotalBytes   int64     `json:"total_bytes"`
    SumLatencyMs int64     `json:"sum_latency_ms"`   // avg = sum/count
    ErrorCount   int64     `json:"error_count"`      // status_code >= 500
    UpdatedAt    time.Time `json:"updated_at"`
}

// AccessLogDaily 天级聚合，由 hourly 滚下来。用于 7d/30d/90d 视图。
type AccessLogDaily struct {
    Date         string    `gorm:"size:10;primaryKey" json:"date"`
    AdapterType  string    `gorm:"size:16;primaryKey" json:"adapter_type"`
    Hit          bool      `gorm:"primaryKey" json:"hit"`
    Upstream     string    `gorm:"size:128;primaryKey;default:''" json:"upstream"`
    RequestCount int64     `json:"request_count"`
    TotalBytes   int64     `json:"total_bytes"`
    SumLatencyMs int64     `json:"sum_latency_ms"`
    ErrorCount   int64     `json:"error_count"`
    UpdatedAt    time.Time `json:"updated_at"`
}

// AccessLogPackageDaily 包粒度按天聚合，独立表。用于 top_packages。
type AccessLogPackageDaily struct {
    Date         string    `gorm:"size:10;primaryKey" json:"date"`
    AdapterType  string    `gorm:"size:16;primaryKey" json:"adapter_type"`
    PackageName  string    `gorm:"size:256;primaryKey" json:"package_name"`
    Hit          bool      `gorm:"primaryKey" json:"hit"`
    RequestCount int64     `json:"request_count"`
    TotalBytes   int64     `json:"total_bytes"`
    UpdatedAt    time.Time `json:"updated_at"`
}
```

**为什么三张表而非一张：**

- 维度爆炸防护：若把 `package_name` 放进 hourly，最坏情况 `24 × 13 ecosystems × N packages × 2 hit × M upstreams` 一天就能炸成几百万行。
- 查询模式不同：top_packages 不关心 `upstream`，dashboard 的 `today` 不关心 `package_name`。三表各司其职。
- `AccessLogDaily` 是 `AccessLogHourly` 的 daily compact，便于 90d 查询只扫天级。

**Hour 时区约定：** 全部 UTC。和现有 `db.Open` 的 `NowFunc: UTC` 一致（见 `repository.go:38`）。前端按需转本地时区。

**注册到 AutoMigrate**（`internal/db/repository.go:74`）：

```go
return db.AutoMigrate(
    &CacheEntry{}, &AccessLog{}, /* ... 现有 ... */
    &AccessLogHourly{}, &AccessLogDaily{}, &AccessLogPackageDaily{},
)
```

## 四、配置

`internal/config/config.go` 新增 `AccessLogConfig`：

```go
type AccessLogConfig struct {
    RetentionDays       int           `mapstructure:"retention_days"`        // 原始表保留天数
    BatchSize           int           `mapstructure:"batch_size"`            // 批量 insert 条数
    BatchInterval       time.Duration `mapstructure:"batch_interval"`        // 批量 flush 间隔
    RollupEnabled       bool          `mapstructure:"rollup_enabled"`        // false 走旧路径
    RollupRetentionDays int           `mapstructure:"rollup_retention_days"` // rollup 表保留天数
    BackfillOnStart     bool          `mapstructure:"backfill_on_start"`     // 首次启动回填
}
```

挂到 `Config.AccessLog`，并在 `loader.go` 设默认值：

```go
v.SetDefault("access_log.retention_days", 7)
v.SetDefault("access_log.batch_size", 100)
v.SetDefault("access_log.batch_interval", "5s")
v.SetDefault("access_log.rollup_enabled", true)
v.SetDefault("access_log.rollup_retention_days", 365)
v.SetDefault("access_log.backfill_on_start", true)

// loader.go 末尾追加
if raw := v.GetString("access_log.batch_interval"); raw != "" {
    d, err := time.ParseDuration(raw)
    if err != nil {
        return nil, fmt.Errorf("parse access_log.batch_interval: %w", err)
    }
    cfg.AccessLog.BatchInterval = d
}
```

`config.example.toml` 追加示例段。

## 五、新增 package：`internal/accesslog/`

### 5.1 目录结构

```
internal/accesslog/
├── event.go         # Event 类型与 hourlyKey/pkgDailyKey 派生
├── recorder.go      # Recorder 接口 + batchedRecorder 实现
├── aggregator.go    # 内存聚合 + UPSERT 到 rollup 表（hourly + pkgDaily）
├── compactor.go     # 每日凌晨：hourly → daily
├── retention.go     # 定期清理超 retention 的原始 AccessLog 与 rollup 表
├── backfill.go      # 首次启动从 AccessLog 表回填 rollup
├── upsert.go        # 跨方言（sqlite / postgres）的 UPSERT 抽象
└── *_test.go        # 单元测试
```

### 5.2 `event.go`

```go
package accesslog

import "time"

type Event struct {
    AdapterType string
    Method      string
    CacheKey    string
    PackageName string
    Upstream    string
    ClientIP    string
    Hit         bool
    LatencyMs   int64
    StatusCode  int
    BytesSent   int64
    At          time.Time // UTC
}

// hourlyKey 是 AccessLogHourly 的 PK
type hourlyKey struct {
    Date        string // YYYY-MM-DD
    Hour        int
    AdapterType string
    Hit         bool
    Upstream    string
}

func (e Event) hourlyKey() hourlyKey {
    t := e.At.UTC()
    return hourlyKey{
        Date:        t.Format("2006-01-02"),
        Hour:        t.Hour(),
        AdapterType: e.AdapterType,
        Hit:         e.Hit,
        Upstream:    e.Upstream,
    }
}

type pkgDailyKey struct {
    Date        string
    AdapterType string
    PackageName string
    Hit         bool
}

func (e Event) pkgDailyKey() (pkgDailyKey, bool) {
    if e.PackageName == "" {
        return pkgDailyKey{}, false
    }
    return pkgDailyKey{
        Date:        e.At.UTC().Format("2006-01-02"),
        AdapterType: e.AdapterType,
        PackageName: e.PackageName,
        Hit:         e.Hit,
    }, true
}

type counters struct {
    RequestCount int64
    TotalBytes   int64
    SumLatencyMs int64
    ErrorCount   int64
}

func (c *counters) add(e Event) {
    c.RequestCount++
    c.TotalBytes += e.BytesSent
    c.SumLatencyMs += e.LatencyMs
    if e.StatusCode >= 500 {
        c.ErrorCount++
    }
}
```

### 5.3 `recorder.go`

```go
package accesslog

import (
    "context"
    "sync"
    "time"

    "go.uber.org/zap"
    "gorm.io/gorm"

    "depsilo/internal/db"
)

// Recorder 抽象给 adapter/accesslog.go 使用。
type Recorder interface {
    Record(e Event)
    Flush(ctx context.Context) error
    Close(ctx context.Context) error
}

// nullRecorder 在 rollup_enabled=false 时做兼容（写原始表，不做 rollup）。
type nullRecorder struct {
    db *gorm.DB
}

func (n *nullRecorder) Record(e Event) {
    entry := toAccessLog(e)
    go func() {
        if err := n.db.Create(&entry).Error; err != nil {
            zap.L().Warn("failed to write access log", zap.Error(err))
        }
    }()
}
func (n *nullRecorder) Flush(_ context.Context) error  { return nil }
func (n *nullRecorder) Close(_ context.Context) error  { return nil }

// batchedRecorder 真正的实现。
type batchedRecorder struct {
    db            *gorm.DB
    in            chan Event
    rawBatchSize  int
    flushInterval time.Duration

    mu        sync.Mutex
    rawBuf    []db.AccessLog
    aggHourly map[hourlyKey]*counters
    aggPkg    map[pkgDailyKey]*counters

    stopOnce sync.Once
    stop     chan struct{}
    done     chan struct{}
}

func NewRecorder(database *gorm.DB, cfg RecorderConfig) Recorder {
    if !cfg.Enabled {
        return &nullRecorder{db: database}
    }
    r := &batchedRecorder{
        db:            database,
        in:            make(chan Event, 4096), // 背压保护
        rawBatchSize:  cfg.BatchSize,
        flushInterval: cfg.BatchInterval,
        aggHourly:     make(map[hourlyKey]*counters),
        aggPkg:        make(map[pkgDailyKey]*counters),
        stop:          make(chan struct{}),
        done:          make(chan struct{}),
    }
    go r.loop()
    return r
}

type RecorderConfig struct {
    Enabled       bool
    BatchSize     int
    BatchInterval time.Duration
}

func (r *batchedRecorder) Record(e Event) {
    select {
    case r.in <- e:
    default:
        // channel 满：丢弃 + 计数 metric（不能阻塞热路径）
        zap.L().Warn("access log recorder channel full, dropping event")
    }
}

func (r *batchedRecorder) loop() {
    defer close(r.done)
    ticker := time.NewTicker(r.flushInterval)
    defer ticker.Stop()

    for {
        select {
        case <-r.stop:
            r.flushAll(context.Background())
            return
        case e := <-r.in:
            r.ingest(e)
            if r.rawLen() >= r.rawBatchSize {
                r.flushAll(context.Background())
            }
        case <-ticker.C:
            r.flushAll(context.Background())
        }
    }
}

func (r *batchedRecorder) ingest(e Event) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.rawBuf = append(r.rawBuf, toAccessLog(e))
    h := e.hourlyKey()
    if r.aggHourly[h] == nil {
        r.aggHourly[h] = &counters{}
    }
    r.aggHourly[h].add(e)
    if k, ok := e.pkgDailyKey(); ok {
        if r.aggPkg[k] == nil {
            r.aggPkg[k] = &counters{}
        }
        r.aggPkg[k].add(e)
    }
}

func (r *batchedRecorder) rawLen() int {
    r.mu.Lock()
    defer r.mu.Unlock()
    return len(r.rawBuf)
}

func (r *batchedRecorder) flushAll(ctx context.Context) {
    r.mu.Lock()
    rawBuf := r.rawBuf
    aggH := r.aggHourly
    aggP := r.aggPkg
    r.rawBuf = nil
    r.aggHourly = make(map[hourlyKey]*counters)
    r.aggPkg = make(map[pkgDailyKey]*counters)
    r.mu.Unlock()

    if len(rawBuf) > 0 {
        if err := r.db.WithContext(ctx).CreateInBatches(rawBuf, 200).Error; err != nil {
            zap.L().Warn("failed to flush raw access logs", zap.Error(err), zap.Int("count", len(rawBuf)))
        }
    }
    if len(aggH) > 0 {
        if err := upsertHourly(ctx, r.db, aggH); err != nil {
            zap.L().Warn("failed to upsert hourly rollup", zap.Error(err))
        }
    }
    if len(aggP) > 0 {
        if err := upsertPackageDaily(ctx, r.db, aggP); err != nil {
            zap.L().Warn("failed to upsert package daily rollup", zap.Error(err))
        }
    }
}

func (r *batchedRecorder) Flush(ctx context.Context) error {
    r.flushAll(ctx)
    return nil
}

func (r *batchedRecorder) Close(ctx context.Context) error {
    r.stopOnce.Do(func() { close(r.stop) })
    select {
    case <-r.done:
    case <-ctx.Done():
        return ctx.Err()
    }
    return nil
}

func toAccessLog(e Event) db.AccessLog {
    return db.AccessLog{
        AdapterType: e.AdapterType,
        Method:      e.Method,
        CacheKey:    e.CacheKey,
        PackageName: e.PackageName,
        Hit:         e.Hit,
        Upstream:    e.Upstream,
        LatencyMs:   e.LatencyMs,
        StatusCode:  e.StatusCode,
        ClientIP:    e.ClientIP,
        BytesSent:   e.BytesSent,
        CreatedAt:   e.At.UTC(),
    }
}
```

**关键设计点：**

- 背压保护：channel 满直接丢弃 + warn 日志。这是异步访问日志，不能阻塞主请求。
- 双缓冲：raw 批和 agg map 是同一 mutex 保护的，flush 时一次性把指针换走，避免持锁做 IO。
- batch size 触发 + interval 触发双重：高并发场景 5s 内可能堆积上万行，先按 100 行 flush 一次；空闲时也保证 5s 落盘。

### 5.4 `upsert.go` — UPSERT 抽象

GORM 的 `clause.OnConflict` 已经能跨 sqlite/postgres，直接用：

```go
package accesslog

import (
    "context"
    "time"

    "gorm.io/gorm"
    "gorm.io/gorm/clause"

    "depsilo/internal/db"
)

func upsertHourly(ctx context.Context, gdb *gorm.DB, m map[hourlyKey]*counters) error {
    rows := make([]db.AccessLogHourly, 0, len(m))
    now := time.Now().UTC()
    for k, c := range m {
        rows = append(rows, db.AccessLogHourly{
            Date:         k.Date,
            Hour:         k.Hour,
            AdapterType:  k.AdapterType,
            Hit:          k.Hit,
            Upstream:     k.Upstream,
            RequestCount: c.RequestCount,
            TotalBytes:   c.TotalBytes,
            SumLatencyMs: c.SumLatencyMs,
            ErrorCount:   c.ErrorCount,
            UpdatedAt:    now,
        })
    }
    return gdb.WithContext(ctx).Clauses(clause.OnConflict{
        Columns: []clause.Column{
            {Name: "date"}, {Name: "hour"}, {Name: "adapter_type"},
            {Name: "hit"}, {Name: "upstream"},
        },
        DoUpdates: clause.Assignments(map[string]interface{}{
            "request_count":  gorm.Expr("access_log_hourly.request_count + excluded.request_count"),
            "total_bytes":    gorm.Expr("access_log_hourly.total_bytes + excluded.total_bytes"),
            "sum_latency_ms": gorm.Expr("access_log_hourly.sum_latency_ms + excluded.sum_latency_ms"),
            "error_count":    gorm.Expr("access_log_hourly.error_count + excluded.error_count"),
            "updated_at":     now,
        }),
    }).CreateInBatches(rows, 200).Error
}

// 同样模式实现 upsertPackageDaily（PK 不同）。
```

**SQLite 注意事项：** SQLite 3.24+ 支持 `INSERT ... ON CONFLICT DO UPDATE` 且 `excluded.X` 引用新值，glebarez/sqlite 驱动支持。表名是 GORM 复数化的（`access_log_hourly` 默认会变 `access_log_hourlies`）—— 需要在 model 上加 `TableName()` 方法固定为 `access_log_hourly`，否则 SQL 字符串里写错表名会报 `no such table`。三张表都要：

```go
func (AccessLogHourly) TableName() string       { return "access_log_hourly" }
func (AccessLogDaily) TableName() string        { return "access_log_daily" }
func (AccessLogPackageDaily) TableName() string { return "access_log_package_daily" }
```

### 5.5 `compactor.go`

每天 UTC 00:05 把昨天的 `AccessLogHourly` 24 行汇总成 `AccessLogDaily` 一行（按 adapter/hit/upstream 聚合）。

```go
package accesslog

import (
    "context"
    "time"

    "go.uber.org/zap"
    "gorm.io/gorm"

    "depsilo/internal/db"
)

// StartCompactor 后台 goroutine，每天 UTC 00:05 跑一次。
func StartCompactor(ctx context.Context, gdb *gorm.DB) {
    runOnce(ctx, gdb) // 启动时立刻补一次昨日
    for {
        next := nextRunUTC(time.Now().UTC())
        select {
        case <-ctx.Done():
            return
        case <-time.After(time.Until(next)):
            runOnce(ctx, gdb)
        }
    }
}

func nextRunUTC(now time.Time) time.Time {
    tomorrow := now.Add(24 * time.Hour)
    return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 5, 0, 0, time.UTC)
}

func runOnce(ctx context.Context, gdb *gorm.DB) {
    yesterday := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02")
    if err := compactDate(ctx, gdb, yesterday); err != nil {
        zap.L().Warn("compactor failed", zap.String("date", yesterday), zap.Error(err))
    } else {
        zap.L().Info("compactor done", zap.String("date", yesterday))
    }
}

// compactDate 将给定日期的 hourly 行 SUM 到 daily 表。
// 用 INSERT ... SELECT ... ON CONFLICT 一条 SQL 解决，幂等。
func compactDate(ctx context.Context, gdb *gorm.DB, date string) error {
    // 注意：因为 hourly 表里的同一 (date, adapter, hit, upstream)
    // 可能横跨 24 行，需要先 GROUP BY 再 UPSERT。
    sql := `
        INSERT INTO access_log_daily
            (date, adapter_type, hit, upstream,
             request_count, total_bytes, sum_latency_ms, error_count, updated_at)
        SELECT date, adapter_type, hit, upstream,
               SUM(request_count), SUM(total_bytes),
               SUM(sum_latency_ms), SUM(error_count), ?
        FROM access_log_hourly
        WHERE date = ?
        GROUP BY date, adapter_type, hit, upstream
        ON CONFLICT(date, adapter_type, hit, upstream) DO UPDATE SET
            request_count  = excluded.request_count,
            total_bytes    = excluded.total_bytes,
            sum_latency_ms = excluded.sum_latency_ms,
            error_count    = excluded.error_count,
            updated_at     = excluded.updated_at
    `
    return gdb.WithContext(ctx).Exec(sql, time.Now().UTC(), date).Error
}
```

**幂等性：** `ON CONFLICT DO UPDATE SET = excluded` 不是累加，是覆盖。这是关键 —— 重跑 compaction 不会双倍计数。

### 5.6 `retention.go`

```go
package accesslog

import (
    "context"
    "time"

    "go.uber.org/zap"
    "gorm.io/gorm"

    "depsilo/internal/db"
)

type RetentionConfig struct {
    RawDays    int // 原始 AccessLog 保留天数
    RollupDays int // hourly/daily/pkg-daily 保留天数
}

// StartRetention 每小时跑一次。
func StartRetention(ctx context.Context, gdb *gorm.DB, cfg RetentionConfig) {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()

    runRetention(ctx, gdb, cfg) // 启动立刻跑一次
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            runRetention(ctx, gdb, cfg)
        }
    }
}

func runRetention(ctx context.Context, gdb *gorm.DB, cfg RetentionConfig) {
    if cfg.RawDays > 0 {
        cutoff := time.Now().UTC().AddDate(0, 0, -cfg.RawDays)
        res := gdb.WithContext(ctx).
            Where("created_at < ?", cutoff).
            Delete(&db.AccessLog{})
        if res.Error != nil {
            zap.L().Warn("retention: access_logs delete failed", zap.Error(res.Error))
        } else if res.RowsAffected > 0 {
            zap.L().Info("retention: pruned access_logs",
                zap.Int64("rows", res.RowsAffected),
                zap.Time("before", cutoff))
        }
    }
    if cfg.RollupDays > 0 {
        cutoffDate := time.Now().UTC().AddDate(0, 0, -cfg.RollupDays).Format("2006-01-02")
        for _, table := range []string{"access_log_hourly", "access_log_daily", "access_log_package_daily"} {
            if err := gdb.WithContext(ctx).
                Exec("DELETE FROM "+table+" WHERE date < ?", cutoffDate).Error; err != nil {
                zap.L().Warn("retention: rollup delete failed", zap.String("table", table), zap.Error(err))
            }
        }
    }
}
```

### 5.7 `backfill.go`

首次启动时，如果 rollup 表是空的（或没有今日数据），从原始 `AccessLog` 表回填一次。

```go
package accesslog

import (
    "context"

    "go.uber.org/zap"
    "gorm.io/gorm"

    "depsilo/internal/db"
)

// BackfillIfEmpty 如果 hourly 表为空，则从原始 AccessLog 全量回填。
// 仅启动时调一次。回填 SQL 也是 INSERT ... SELECT ... ON CONFLICT 模式，可重入。
func BackfillIfEmpty(ctx context.Context, gdb *gorm.DB) error {
    var hourlyCount int64
    if err := gdb.WithContext(ctx).Model(&db.AccessLogHourly{}).Count(&hourlyCount).Error; err != nil {
        return err
    }
    if hourlyCount > 0 {
        zap.L().Info("rollup tables already populated, skipping backfill")
        return nil
    }
    zap.L().Info("backfilling rollup tables from access_logs")
    if err := backfillHourly(ctx, gdb); err != nil {
        return err
    }
    if err := backfillDaily(ctx, gdb); err != nil {
        return err
    }
    if err := backfillPackageDaily(ctx, gdb); err != nil {
        return err
    }
    zap.L().Info("rollup backfill complete")
    return nil
}

func backfillHourly(ctx context.Context, gdb *gorm.DB) error {
    // SQLite 的小时提取用 strftime('%H', ...)
    // PostgreSQL 用 EXTRACT(HOUR FROM ...) — 需要分方言或先只支持 sqlite
    sql := `
        INSERT INTO access_log_hourly
            (date, hour, adapter_type, hit, upstream,
             request_count, total_bytes, sum_latency_ms, error_count, updated_at)
        SELECT
            strftime('%Y-%m-%d', created_at) as date,
            CAST(strftime('%H', created_at) AS INTEGER) as hour,
            adapter_type,
            hit,
            COALESCE(upstream, '') as upstream,
            COUNT(*),
            COALESCE(SUM(bytes_sent), 0),
            COALESCE(SUM(latency_ms), 0),
            SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END),
            datetime('now')
        FROM access_logs
        GROUP BY date, hour, adapter_type, hit, upstream
    `
    return gdb.WithContext(ctx).Exec(sql).Error
}
// backfillDaily / backfillPackageDaily 类似。
```

**方言支持：** 当前项目只支持 sqlite（见 `repository.go:18`，`default:` 直接 return 错误），所以 backfill 暂时只写 sqlite 版本。日后接 postgres 时再加 dialect 分支。

## 六、改造点 1：`internal/adapter/accesslog.go`

把现有的 `LogAccess`（73 行）改成调 `Recorder.Record`：

```go
package adapter

import (
    "strings"
    "time"

    "gorm.io/gorm"

    "depsilo/internal/accesslog"
    "depsilo/internal/adapter/packagekey"
    "depsilo/internal/db"
)

var (
    auditLogger interface{ Log(entry db.AuditLog) }
    recorder    accesslog.Recorder // 通过 SetRecorder 注入
)

func SetAuditLogger(l interface{ Log(entry db.AuditLog) }) { auditLogger = l }
func SetRecorder(r accesslog.Recorder)                     { recorder = r }

func LogAccess(database *gorm.DB, adapterType, method, cacheKey string, hit bool,
    upstreamName string, latency time.Duration, statusCode int, clientIP string, bytesSent int64) {

    pkgName := packagekey.ExtractName(adapterType, cacheKey)
    now := time.Now().UTC()

    if recorder != nil {
        recorder.Record(accesslog.Event{
            AdapterType: adapterType,
            Method:      method,
            CacheKey:    cacheKey,
            PackageName: pkgName,
            Upstream:    upstreamName,
            ClientIP:    clientIP,
            Hit:         hit,
            LatencyMs:   latency.Milliseconds(),
            StatusCode:  statusCode,
            BytesSent:   bytesSent,
            At:          now,
        })
    } else {
        // 兜底：recorder 未初始化（极早期 boot 阶段或测试），走旧路径。
        go func() {
            _ = database.Create(&db.AccessLog{
                AdapterType: adapterType,
                Method:      method,
                CacheKey:    cacheKey,
                PackageName: pkgName,
                Hit:         hit,
                Upstream:    upstreamName,
                LatencyMs:   latency.Milliseconds(),
                StatusCode:  statusCode,
                ClientIP:    clientIP,
                BytesSent:   bytesSent,
                CreatedAt:   now,
            }).Error
        }()
    }

    // 审计 logger 保持原状（Pro 功能）
    if auditLogger != nil {
        // ... 现有 action/cacheResult 逻辑不变
    }
}
```

`database` 参数仍保留，避免改全部 adapter 的 callsite。

## 七、改造点 2：`internal/server/server.go`

在 `db.AutoMigrate` 之后、注册路由之前注入 Recorder：

```go
// 紧跟现有 backfillPackageNames(database) 之后
if err := accesslog.BackfillIfEmpty(ctx, database); err != nil {
    zap.L().Warn("rollup backfill failed", zap.Error(err))
}

recorder := accesslog.NewRecorder(database, accesslog.RecorderConfig{
    Enabled:       cfg.AccessLog.RollupEnabled,
    BatchSize:     cfg.AccessLog.BatchSize,
    BatchInterval: cfg.AccessLog.BatchInterval,
})
adapter.SetRecorder(recorder)

go accesslog.StartCompactor(ctx, database)
go accesslog.StartRetention(ctx, database, accesslog.RetentionConfig{
    RawDays:    cfg.AccessLog.RetentionDays,
    RollupDays: cfg.AccessLog.RollupRetentionDays,
})
```

**graceful shutdown：** 在 `cmd/server/main.go`（或当前 shutdown 处）收到 SIGTERM 时调 `recorder.Close(ctx)`，等内存里还没 flush 的 batch 落盘。建议在 server.go 返回一个 `cleanup func()`，让 main 在 server.Shutdown 之后调用。

## 八、改造点 3：handler 查询迁移

下面给每个聚合查询的替换方案。**所有改造都遵循"rollup 优先 + 原始表 fallback"模式**：

```go
if h.useRollup() { /* 走 rollup */ } else { /* 走旧逻辑 */ }

func (h *DashboardHandler) useRollup() bool {
    return h.cfg.AccessLog.RollupEnabled
}
```

（`useRollup` 也可以全局放 `accesslog.Enabled(cfg) bool` helper。）

### 8.1 `internal/api/admin/dashboard.go:GetDashboard`

| 旧查询 | 新查询（rollup） |
|---|---|
| `Where("created_at >= ?", todayStart).Count` (total) | `AccessLogHourly WHERE date = today`，`SUM(request_count)` |
| 同上 + `hit = true` Count | 同表 `WHERE date = today AND hit = true` |
| `SUM(bytes_sent)` | `SUM(total_bytes)` |
| `AVG(latency_ms)` | `SUM(sum_latency_ms) / SUM(request_count)` |
| `DATE(created_at), adapter_type, COUNT(*)` GROUP BY (7 天) | `AccessLogDaily WHERE date >= today-7 GROUP BY date, adapter_type SUM(request_count)` |
| top_packages pypi/apt | `AccessLogPackageDaily WHERE adapter_type=? GROUP BY package_name ORDER BY SUM(request_count) DESC` |

注意：top_packages 旧 SQL 用的是 "命中次数"，但语义其实是 "请求次数"（不分 hit/miss）。新表 GROUP BY 时也不 filter hit。

### 8.2 `internal/api/admin/dashboard.go:GetTrends`

整段查询替换为：

```sql
SELECT date,
       SUM(request_count) AS requests,
       SUM(CASE WHEN hit = 1 THEN request_count ELSE 0 END) AS hits,
       SUM(CASE WHEN hit = 0 THEN request_count ELSE 0 END) AS misses,
       SUM(total_bytes) AS bytes_served
FROM access_log_daily
WHERE date >= ?
GROUP BY date
ORDER BY date ASC
```

### 8.3 `internal/api/admin/bandwidth.go:GetReport`

最复杂的迁移点，5 个聚合查询：

1. **summary（按 hit 分组）**：`AccessLogDaily WHERE date BETWEEN ? AND ? GROUP BY hit`
2. **ecoLatencies（按 adapter+hit）**：同上 `GROUP BY adapter_type, hit`
3. **daily（按 date+hit）**：`AccessLogDaily WHERE date BETWEEN ? AND ? GROUP BY date, hit`
4. **byEcosystem（按 adapter+hit）**：同 2
5. **top_packages**：`AccessLogPackageDaily WHERE date BETWEEN ? AND ? AND package_name != '' GROUP BY package_name, adapter_type ORDER BY SUM(total_bytes) DESC LIMIT 10`
6. **byUpstream（hit=false，按 upstream）**：`AccessLogDaily WHERE date BETWEEN ? AND ? AND hit = false AND upstream != '' GROUP BY upstream`

**日期范围处理**：rollup 用 string date（"YYYY-MM-DD"），传入 `start.Format("2006-01-02")`、`end.Format("2006-01-02")`，比较即字符串比较（合法因为是 ISO 格式）。不再需要 `datetime(created_at) >= datetime(?)` 的 hack。

### 8.4 `internal/api/public/stats.go`

- `kpiSeries`（12 个 5 分钟桶）：这是**亚小时粒度**，rollup 没法满足。建议保留走原始 `AccessLog`（在 retention 内即可，最长 5 分钟回看肯定有数据）。
- 其余 `today` 块、`top_packages` 块：同 dashboard，走 rollup。

### 8.5 `internal/api/public/mcp.go`

- `hits/misses Count`（最近 24h）：走 `AccessLogHourly WHERE date >= today-1 AND hit = ?` SUM。
- L555 的 `q := h.DB.Model(&db.AccessLog{}); ... var logs []db.AccessLog`：这是明细查询，**保留原样**走原始表。

### 8.6 `internal/api/admin/logs.go`

**不改动**。原本就是明细查询，受限于 `retention_days`，前端在表格头部加一条提示："仅显示最近 N 天的访问日志"（N 从 `/api/v1/admin/settings` 或新增 `/api/v1/admin/access-log/info` 端点取）。

## 九、迁移与回滚策略

**5 个阶段，每阶段可独立 ship + 回滚：**

| 阶段 | 范围 | 风险 | 回滚方式 |
|---|---|---|---|
| P1 | 新增 3 张表 + AutoMigrate | 极低 | 不需要回滚，表空着不影响 |
| P2 | Recorder 双写（原始 + rollup），handler 不变 | 低 | 改 config `rollup_enabled=false` 即停 rollup 写入 |
| P3 | handler 改为 rollup 优先 + 原表 fallback | 中 | 同 P2，回 fallback 自动走旧路径 |
| P4 | 启动 retention 清理原始 AccessLog（默认 7d） | 中 | 改 `retention_days=0` 关闭清理 |
| P5 | 删除 fallback 代码 + 文档定稿 | 低 | （此时已稳定，不应回滚） |

P3 推荐至少观察 1-2 周再进 P4。P4 一旦启动，旧数据物理删除，无法回滚到"全量原始日志可查"。

## 十、风险与开放问题

1. **包名维度爆炸**：极端用户单天 distinct package_name 上万 → `AccessLogPackageDaily` 单日万行。处理：
   - 添加监控：启动 retention 时打 `package_daily_rows` metric。
   - 必要时改成"只记 Top-N + others 合并"。短期不做。

2. **UPSERT 锁争用**：SQLite 写锁全局。`flushAll` 持续 100ms 期间会阻塞 admin 查询。已有 `busy_timeout=5000`（`repository.go:64`）兜底。极端高 QPS 下需要观察 `RowsAffected`/耗时。

3. **Channel 满丢弃**：4096 缓冲，5s flush，正常 QPS（千级）够用。万级 QPS 持续上行会丢日志。**接受这个 tradeoff**：访问日志不是计费数据，丢一些不影响功能。打 `recorder_dropped_total` Prometheus counter 即可。

4. **`kpiSeries` 仍打原始表**：retention=7d 时一定有数据，OK。但若用户改成 retention=1d，整点交界处可能短暂缺数据。可接受。

5. **时区**：rollup 全 UTC。前端展示要把 date string 转本地时区显示（已经在 `web/src/lib/utils.ts` 有 `formatTime`，date 格式同样适用）。今日"分界点"以 UTC 为准 —— 北京时间用户看到的"今天"会与 UTC 错位。**短期接受**，长期可加 server-side timezone 配置。

6. **Postgres 路径**：当前 `repository.go` 只支持 sqlite。本 spec 的 SQL 用了 sqlite 专属 `strftime`。如果未来开 postgres 支持，需要 dialect 分支（建议加 `internal/accesslog/sql_sqlite.go` 和 `sql_postgres.go`）。

## 十一、收益估算

| 指标 | 当前 | Rollup 后 |
|---|---|---|
| 100K req/天，30 天存储 | ~1.5 GB（持续涨） | ~350 MB 封顶（7 天原始 + 30 天 rollup） |
| 100K req/天，1 年存储 | **~18 GB** | **~400 MB 封顶** |
| `GetDashboard` today 块查询 | 全表 COUNT/SUM × 3 | 单表索引扫描 ~50 行 |
| `bandwidth(90d)` 查询 | 扫 90 天原始（最坏 9M 行） | 扫 90 天 daily（最多几千行） |
| 改造文件数 | — | 新增 8 个，修改 7 个 |

## 十二、测试清单

实施时需要新增的测试：

- [ ] `internal/accesslog/recorder_test.go`：
  - Record → flush → 验证原始表插入数 + hourly UPSERT 累加
  - channel 满丢弃路径
  - Close 触发 final flush
- [ ] `internal/accesslog/aggregator_test.go`：
  - hourlyKey/pkgDailyKey 派生正确性（特别是跨小时事件）
  - UPSERT 累加幂等性（连续 flush 两次同 key，count 翻倍）
- [ ] `internal/accesslog/compactor_test.go`：
  - 喂 24 行 hourly → compactDate → 1 行 daily 字段 SUM 正确
  - 重跑 compactDate 不双倍计数（幂等）
- [ ] `internal/accesslog/retention_test.go`：
  - retentionRaw=7d 后 8 天前的行被删
  - retentionRollup=365d 不影响近期数据
- [ ] `internal/accesslog/backfill_test.go`：
  - 空 hourly 表 + 100 行 AccessLog → 回填后 hourly 行数 = GROUP BY 数
  - 已有 hourly 数据时 BackfillIfEmpty 跳过
- [ ] 集成：手动验证 dashboard / bandwidth 在 rollup 启用前后数字完全一致（fallback 路径同时跑，diff < 1%）

## 十三、实施顺序（推荐）

1. 建表 + AutoMigrate（commit 1）—— 可独立 ship，无影响
2. 配置段 + 默认值（commit 2）—— 同上
3. `internal/accesslog/` 包骨架 + 单测（commit 3）
4. 接入 `adapter/accesslog.go` + `server.go` 启动（commit 4）—— 双写开始
5. backfill 一次性回填（commit 5，与 4 可合并）
6. handler 迁移 dashboard.go（commit 6）+ 联调 fallback
7. handler 迁移 bandwidth.go（commit 7）
8. handler 迁移 stats.go / mcp.go（commit 8）
9. 启动 retention（commit 9，文档同步说明）
10. 移除 fallback（commit 10，至少观察 2 周后）

每个 commit 都应过 `make test` + `make build`。

---

**附：与 ADR 的分工**

- 本 spec 关注 **如何实施**（数据模型、代码结构、迁移步骤）。
- [ADR-0002](../adr/0002-access-log-rollup.md) 关注 **为何这样设计**（备选方案对比、决策理由）。
