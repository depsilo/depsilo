# Depsilo 自测前置清单

> 部署前 / 部署后必跑的检查项。从"docker run"到"可以让团队真正用"之间的最小验证路径。
> 命令默认假设端口 23333、docker-compose 部署。改成 binary 部署就把 `docker exec depsilo` 换成 `./depsilo`。

---

## Phase 0 — 部署前准备

- [ ] **Docker ≥ 20.10**（`docker --version`）。低于此版本的 buildkit / volume 行为有兼容问题。
- [ ] **端口 23333 空闲**（`ss -tlnp | grep :23333` 应无结果）。如冲突改 `config.toml` 的 `[server] port` 或映射成 `-p 8080:23333`。
- [ ] **磁盘空间 ≥ 30 GB**（`df -h .`）。默认 cache 上限 20 GB + 数据库 + 日志，留 50% 余量。
- [ ] **决定存储后端**：
  - 个人 / 单实例 → 本地文件系统 + SQLite（默认，零配置）
  - 多实例 / 高可用 → S3 + PostgreSQL（[ADR-pending] 暂无官方多实例文档，自行验证）
- [ ] **决定要测的生态**：列出你**真实用到**的包管理器，不要全测——Depsilo 支持 14 个生态，但 E2E 用的是 hello-world 级 package，**你自己依赖的真实包**才是回归基准。

---

## Phase 1 — 首次部署 + 健康检查

```bash
# 1. 拉镜像 + 启动
docker run -d --name depsilo \
  -p 23333:23333 \
  -v "$PWD/data:/app/data" \
  --restart unless-stopped \
  depsilo/depsilo:0.7.1
```

- [ ] **健康检查返回 200**：`curl -sf http://localhost:23333/health | jq .` → `{"status":"healthy"}`
- [ ] **Discover 端点工作**：`curl -sf http://localhost:23333/api/v1/discover | jq .ecosystems | length` → 应为 13+
- [ ] **Portal 首页能开**：浏览器访问 `http://localhost:23333/`，看到 13-ecosystem logo wall + Quickstart
- [ ] **Admin 能登录**：访问 `/admin`，首次会触发 Setup Wizard 创建管理员账号
- [ ] **doctor 全绿**：
  ```bash
  docker exec depsilo /app/depsilo doctor --json | jq '.checks[] | select(.status != "ok")'
  ```
  输出应为空。如果有 `degraded` / `fail` 项，按提示先修。

---

## Phase 2 — 逐生态烟测（只测你用到的）

每个生态走"配置客户端 → 安装一个真实包 → 验证缓存写入"三步。

### pip（PyPI）

```bash
# 客户端配置
pip config set global.index-url http://localhost:23333/pypi/simple/

# 触发一次 miss + 一次 hit
pip install --no-cache-dir requests
pip uninstall -y requests
pip install --no-cache-dir requests  # 这次应该秒装
```

- [ ] 第二次 install 明显比第一次快（>3x）
- [ ] Admin → Access Logs 能看到对应记录（一次 MISS 一次 HIT）
- [ ] Admin → Cache 能看到 `pypi/files/...requests-*.whl`

### npm

```bash
npm config set registry http://localhost:23333/npm/
npm install --no-save lodash
```

- [ ] Admin 看到 `npm/lodash/-/lodash-*.tgz` 缓存条目
- [ ] **没有客户端报"integrity mismatch"**——npm 对 tarball SHA 严格校验，URL 重写若漏一处会立即报错

### Go modules

```bash
GOPROXY=http://localhost:23333/go,direct go install github.com/spf13/cobra@latest
```

- [ ] Admin 看到 `go/github.com/spf13/cobra/@v/v*.zip` 缓存
- [ ] 再次 `go install` 走缓存（毫秒级返回）

### 其他生态

按需逐个走相同流程：cargo / maven / rubygems / composer / nuget / conda / cran / helm / huggingface / apt / docker。
**docker registry**：`docker pull` 需要 `daemon.json` 加 `registry-mirrors`，验证用 `docker pull library/nginx:alpine`。

---

## Phase 3 — 端到端验证（功能性）

- [ ] **命中率统计准确**：Phase 2 完成后，Admin Dashboard "命中率"应 > 0%
- [ ] **带宽节省统计**：Bandwidth Report 显示"节省流量"和"节省时间"非零
- [ ] 上游选择符合“最高优先级的健康源”。健康检查或一次请求结果把主源标记
      unhealthy 后，**后续请求**才会选择备用源；当前失败请求不会自动重试
- [ ] Admin Upstream 新增、修改或删除成功后，下一次真实代理请求立即使用数据库中的新 Pool 快照；无需重启
- [ ] 删除普通上游源后重启，确认首次 seed 已完成的生态不会被 `config.toml` 重新回填
- [ ] 删除 active ecosystem 的最后一个上游源返回 `409 LAST_UPSTREAM`
- [ ] Settings 修改 `server.log_level` 后，响应将该字段列入 `applied_now`，无需重启即可观察到新日志级别
- [ ] Settings 修改 Cache/Auth 字段后，响应将字段列入 `restart_required`，页面持续显示 `pending_restart`，重启后清除
- [ ] 用 `DEPSILO_SERVER_LOG_LEVEL` 覆盖日志级别后再保存，文件仍更新，响应和页面将该字段列入 `blocked_by_override` 并显示准确环境变量名
- [ ] Settings 保存前后对比 `config.toml`，确认未修改的注释、空白、键顺序和文件权限模式保持不变
- [ ] 将配置文件或其目录设为只读后保存，确认返回 `409 CONFIG_READ_ONLY`，运行中日志级别和页面草稿不变
- [ ] 不存在 60 秒 circuit breaker。上游恢复依赖健康检查和后续请求结果；
      默认周期见 `internal/upstream/pool.go`
- [ ] **MCP 端点**（可选，给 AI 客户端用）：
  ```bash
  curl -X POST http://localhost:23333/mcp \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'
  ```
  应返回 `{"result":{"protocolVersion":...}}`

---

## Phase 4 — 压力 / 边缘场景

只测你 CI/CD 实际可能撞到的：

- [ ] **并发同包请求**：两个客户端同时 install 同一个未缓存的大包（如 `torch`）
  - 上游应只回源一次（singleflight）
  - 两个客户端都收到完整文件
  - Cache 只有一份
- [ ] **大文件流式**：装一个 ≥ 1GB 的包（如 `tensorflow` 或 `torch`），观察 `docker stats depsilo`——内存应保持平稳（< 200MB），**不应**线性涨到包大小
- [ ] **磁盘水位**：手动塞数据到接近 `lru_threshold`（默认 90%），观察是否自动 LRU 清理
  ```bash
  # 在 admin Settings 改 lru_threshold 到一个低值（如 50）做快速验证
  ```
- [ ] **客户端断开**：install 中途 Ctrl+C，重启 depsilo 看是否有遗留临时文件 / 数据库损坏

---

## Phase 5 — 监控接入

- [ ] **Prometheus 抓取**：`curl http://localhost:23333/metrics` 输出非空。挂到你的 Prometheus 上
- [ ] **关键指标**：
  - `depsilo_cache_hits_total{adapter="pypi"}` —— 命中数
  - `depsilo_cache_misses_total{adapter="pypi"}` —— 未命中数
  - `depsilo_upstream_latency_seconds_bucket{...}` —— 上游延迟分布
  - `depsilo_storage_size_bytes` —— 缓存占用
- [ ] **告警阈值**（建议）：
  - 健康检查连续 3 次失败
  - 任一上游连续 10 分钟 unhealthy
  - 存储使用 > 85%
  - 7 天滑动窗口命中率 < 50%（说明 cache 配置 / TTL 有问题）
- [ ] **Webhook 通知**（可选）：Admin → Settings → Webhooks 加一个 Slack/钉钉 URL，测试推送

---

## Phase 6 — 备份 / 恢复演练

**没演练过的备份不是备份。**

```bash
# 备份当前状态
docker exec depsilo /app/depsilo backup --out /tmp/depsilo-backup.tar.gz
docker cp depsilo:/tmp/depsilo-backup.tar.gz ./

# 模拟数据丢失
docker rm -f depsilo
rm -rf ./data

# 用备份起一个新实例
mkdir -p ./data
docker run -d --name depsilo \
  -p 23333:23333 \
  -v "$PWD/data:/app/data" \
  depsilo/depsilo:v0.7.1
docker cp ./depsilo-backup.tar.gz depsilo:/tmp/
docker exec depsilo /app/depsilo restore /tmp/depsilo-backup.tar.gz
docker restart depsilo
```

- [ ] 恢复后 Admin / 上游配置 / 用户 / 缓存条目都在
- [ ] Phase 2 装过的包不需要重装（缓存还在）

---

## Phase 7 — 7 天稳态观察

部署后**至少跑一周**才能下"能用"的结论。每天检查：

- [ ] `depsilo doctor` 全绿
- [ ] 命中率持续上升（前 2 天会偏低，后续应 > 70%）
- [ ] 没有 unhandled error 日志：`docker logs depsilo 2>&1 | grep -iE "panic|fatal|error" | head`
- [ ] 内存稳定，没有泄漏（`docker stats depsilo` 看 mem 占用不应单调上涨）
- [ ] 磁盘增长率匹配预期（按缓存 max_size 设定，应在阈值前止涨）

---

## Phase 8 — 决定能否推给团队

| 信号 | ✅ 推 | ⚠️ 缓 | ❌ 别推 |
|---|---|---|---|
| 7 天命中率 | > 60% | 30-60% | < 30%（配置有问题） |
| 上游切换次数 | 0-2 次 | 3-10 次 | > 10 次（上游不稳） |
| `depsilo doctor` | 一直 ok | 偶尔 degraded | 出现过 fail |
| 内存峰值 | < 500MB | 500MB-2GB | > 2GB（流式有问题） |
| panic / fatal 日志 | 0 | 偶发非崩溃 | 任何崩溃 |

---

## 常见坑 / 翻车点

| 现象 | 大概率原因 |
|---|---|
| `npm install` 报 EINTEGRITY | URL 重写漏了某个 dist.tarball，**升 Depsilo 到最新 patch** |
| `pip install` 总是 miss | `index-url` 没改成功，跑 `pip config list` 确认 |
| `go install` 走了 direct | `GOPROXY` 没设或被 `GOPRIVATE` 覆盖，检查 `go env` |
| Docker pull 401 | 上游需要登录，Depsilo 现在不缓存带 Authorization 的响应（设计如此，跨用户安全） |
| 大包装到一半挂 | 客户端超时 < 上游下载时长，**不是 Depsilo 问题**，调客户端 timeout |
| Admin 打不开但 `/health` 正常 | 前端构建产物有问题，重拉镜像 |
| 命中率长期低于 30% | 多半是 cache TTL 太短 → Settings 把 `ttl_blob` 调到 168h+ |

---

## 报问题

走完清单后如果撞到 bug 或 unhandled case：
1. 收集 `docker exec depsilo /app/depsilo doctor --json` 输出
2. 收集 `docker logs depsilo --tail 200`
3. 描述触发步骤
4. 提 GitHub issue：https://github.com/depsilo/depsilo/issues
