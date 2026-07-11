# 篡改检测 Tamper Detection（DIRECTION T1 执行原语）— 设计文档

日期：2026-07-10
状态：已实现并合入 `master@1259728`（信任首见 + 告警不阻断；被动比对）

## 目标

对**不可变制品**（版本一经发布上游不得重发的产物：wheel/.crate/.gem/GOPROXY zip
/npm tarball 等），首次抓取时记录内容 SHA-256；同一制品自然重抓时，若上游字节
与首见哈希不一致 = 篡改（上游被静默换字节 / 镜像被投毒 / 传输中被 MITM）。
后台 stale-while-revalidate 刷新时，Depsilo 保留首见可信字节、拒绝覆盖缓存，并写
审计事件 + 触发 critical webhook。若制品已被 LRU 淘汰，旧字节已不存在，miss
路径仍会发现哈希变化并告警，但无法恢复已淘汰的首见副本。

## 已锁定决策

1. **检测姿态：信任首见 + 告警**。后台刷新不匹配时保留已缓存的可信字节，不阻断
   请求，新字节不落缓存。LRU 后的 miss 只能告警，基线哈希仍不覆盖。
2. **触发覆盖：被动**。仅在制品自然重抓（后台刷新 / 淘汰后重取）再次经过抓取
   路径时比对。零额外上游流量；活跃安装的包（stale-while-revalidate 周期刷新）
   自然获覆盖，冷门包不查。主动重验扫描列为后续增强。

## 哈希落点（`internal/cache/`）

- 扩展现有 `countingReader`：Read 时同步喂入 `sha256.Hash`（流式，零额外遍历）。
  miss 存储路径（`storeAndCommit`）与后台刷新路径（`backgroundRefresh`）本就都
  用 `countingReader` 包裹，计算搭车。新增 `SumHex()` 方法在流耗尽后取哈希。
- 加法性改动：`countingReader` 增字段，不改其现有 `BytesRead()` 契约。

## 不可变判定（零 adapter 改动）

- 信号 = 现成的 TTL。adapter 对制品传 `cfg.TTLBlob`（默认 72h），对元数据传
  `cfg.TTLIndex`（内置缺省 5m，示例配置 1h）。`Manager` 在构造时接收一个
  `immutableThreshold`，默认从 `cache.ttl_blob` 推导，也可通过测试/特殊部署配置
  显式 override；`ttl >= immutableThreshold` 即判为不可变制品。
- 退化保护：若运营者把 `ttl_index` 配到 ≥ 阈值，会把元数据也误判为不可变，
  导致合法变化的元数据触发误报。启动时若 `TTLIndex >= immutableThreshold`
  记一条 warn 提示运营者调低 index TTL 或调高阈值。默认配置不会触发。

## 数据模型（`internal/db/tamper.go`）

```go
type TamperRecord struct {
    Key            string    // cache key，主键（1:1 制品）
    Ecosystem      string    // adapterType
    Package        string    // packagekey.ExtractName(...)
    Version        string    // 从 key 解析（best-effort，用于事件可读性）
    SHA256         string    // 首见内容哈希（hex）
    Size           int64
    FirstSeenAt    time.Time
    LastVerifiedAt time.Time
    VerifyCount    int64     // 通过比对的次数（可信度展示）
}
```

告警复用 `db.QuarantineEvent`，新增 action 常量 `tamper_detected`（写在
`internal/quarantine/store.go` 的 action 区，与其他决策动作同处）。

## 决策流程（`internal/tamper/`）

新包 `internal/tamper/`：`Recorder`（DB 基线 + 事件 + OnTamper 钩子），经由
Manager 的 setter 注入（沿用 `SetSecurityScanner` / `SetQuarantineChecker`
的包级注入模式，保持 cache 包不反向依赖）。

- **首次抓取 / LRU 后重取（miss，`storeAndCommit`）**：统一调用 `Recorder.Verify`。
  无记录时插入基线；已有记录且匹配则更新验证计数；已有记录但不匹配则告警且不
  覆盖基线。由于 `storage.Put` 在验证前完成，LRU 后已无首见字节可保留，此路径会
  缓存本次字节并向 Operator 发出变化告警。
- **后台刷新（`backgroundRefresh`）= 比对点**：不可变 key 且已有基线时，走
  **只验证不重写**分支：重抓字节流入 hash sink（`io.Discard` + hasher，**不调
  `storage.Put`**——不可变字节本该相同，无需重写盘），比对基线：
    - 匹配 → `LastVerifiedAt=now`、`VerifyCount++`、延长 CacheEntry TTL。
    - **不匹配 → 写 `tamper_detected` 事件 + OnTamper（critical webhook），保留
      首见字节，不覆盖 storage、不更新基线哈希**。信任首见零成本达成（新字节从
      未落盘）。同时延长 CacheEntry TTL，避免热包每次请求都重抓并形成告警风暴。
  不可变 key 无基线（feature 上线前的旧缓存）→ 本次重抓的哈希作为基线记录
  （首建，不告警——无可比对的历史）。
- **可变兜底**：非不可变 key（元数据，`ttl < immutableThreshold`）一律不进入
  篡改逻辑（不记录、不比对、不告警）。唯一的元数据误报风险来自上面的 TTL 误配
  场景，由启动 warn 兜底提示，不在比对逻辑里做特判——保持决策单一。

> 实现注记：不可变制品在存储层因此变为 write-once，后台刷新退化为廉价完整性
> 校验，不重新下载到盘——这是正确语义（不可变 = 字节不该变）的自然结果。

## 配置（`[supply_chain.tamper_detection]`）

```toml
[supply_chain.tamper_detection]
enabled = true   # 默认开启（wedge 姿态：空配置也受保护）
```

`enabled=false` 时 Manager 不注入 Recorder，因此不查询/写入基线、不做比对或告警。
`countingReader` 仍会随存储流计算 SHA-256；这是固定的单遍流式成本。

## Webhook

新事件类型 `notify.EventTamperDetected`，severity=critical。server.go 的
OnTamper 闭包桥接到 `webhookNotifier.Dispatch`，复用现有引擎（Slack/钉钉/企微/
飞书），fire-and-forget。事件时间戳用 `now()`（避免 blocklist 曾踩的零值坑）。

## Admin API + UI（本期最小）

- 篡改事件走**现有** `GET /admin/quarantine/events`（action 过滤扩展
  `tamper_detected`），前端 Quarantine → Events 流加对应 badge（danger 色）。
- 不新开页面、不加 CRUD。可信度计数（VerifyCount 汇总）等专门面板列为后续。
- i18n zh/en 同步新增 action badge 文案。

## 测试与验收

1. 单元：`countingReader` 哈希正确性（对已知字节比对 sha256）；`Recorder` 三态
   （首见建基线 / 匹配 verify++ / 不匹配告警且不覆盖）；可变 key 不进入；
   `OnTamper` hook 会触发。
2. 集成：mock 上游对制品先返回内容 A（哈希 H），触发后台刷新时改返回内容 B
   （哈希 H'）→ 断言：产生 `tamper_detected` 事件、客户端**仍拿到 A 的字节**
   （storage 未被覆盖）。集成配置显式短 blob TTL 以便触发刷新。
3. 尚缺：notifier transport 的端到端断言；当前由 Recorder hook 单测与 server bridge
   装配分别覆盖。手动验收可确认 Events 流 badge 和真实 webhook 投递。

## 范围外（本期不做）

- 主动定期重验扫描（选了被动，列为后续增强）。
- 硬阻断 / 451（选了告警不阻断）。
- 可变元数据的内容监控（合法变化，纯噪声）。
- 专门的完整性仪表盘 / VerifyCount 展示面板。
- 跨镜像一致性比对（多上游同一制品哈希交叉验证）——独立课题。

## 实现涉及文件

- `internal/cache/manager.go`（countingReader 扩展、storeAndCommit、backgroundRefresh 比对分支、SetTamperRecorder、immutableThreshold）
- `internal/tamper/`（recorder.go、recorder_test.go）
- `internal/db/tamper.go`（TamperRecord）+ repository.go AutoMigrate
- `internal/quarantine/store.go`（action 常量）
- `internal/config/config.go`（TamperDetectionConfig）
- `internal/notify/event.go`（EventTamperDetected）
- `internal/server/server.go`（装配 + OnTamper 桥接 webhook）
- `web/src/admin/pages/Quarantine.tsx`（badge）+ i18n zh/en
- `config.example.toml`、`CLAUDE.md`（4.21 决策链补第 5.5 步）、`CHANGELOG.md`
- `tests/mock/upstream_server.go`、`tests/integration/tamper_test.go`
