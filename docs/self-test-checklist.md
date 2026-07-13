# Depsilo 自测前置清单

> 部署前后可重复执行的最小验证路径。示例使用 `v0.8.0`、端口
> `23333` 和单节点 SQLite。`master` 上尚未发布的 blocklist / tamper
> detection 不包含在 `v0.8.0` 镜像中。

## 0. 部署前

- [ ] Docker 20.10+：`docker --version`
- [ ] 端口可用：`ss -tlnp | grep :23333` 应无结果
- [ ] 磁盘至少 30 GB：默认缓存上限 20 GB，另留数据库、日志和清理余量
- [ ] 明确单节点边界：当前数据库只支持 SQLite；制品可存本地或 S3，
      但 S3 不会把 SQLite 部署变成多实例/HA
- [ ] 列出团队真实使用的生态。Depsilo 有 14 个常规生态路由，另有独立
      Docker OCI `/v2/`，共 15 个 install surfaces

## 1. 首次部署

空配置部署要同时固定配置、数据库和缓存路径。Setup Wizard 会把默认相对路径
写回 `config.toml`；下面的环境变量将它们覆盖为 volume 内的绝对路径，避免
容器重建后落到未挂载的 `/app/data`。

```bash
mkdir -p depsilo-state
docker run -d --name depsilo \
  -p 23333:23333 \
  -v "$PWD/depsilo-state:/root/.depsilo" \
  -e DEPSILO_DATABASE_DSN=/root/.depsilo/data/depsilo.db \
  -e DEPSILO_STORAGE_PATH=/root/.depsilo/data/cache \
  --restart unless-stopped \
  depsilo/depsilo:0.8.0
```

- [ ] `curl -sf http://localhost:23333/health | jq .` 返回 healthy
- [ ] 首次打开 `http://localhost:23333/`，完成 Setup Wizard。向导配置端口、
      存储和上游，不创建管理员
- [ ] `/api/v1/discover` 的 `ecosystems` 长度为 14；Docker OCI 单独走 `/v2/`
- [ ] 使用默认 `admin` / `admin` 登录 `/admin`，立即修改密码
- [ ] 容器重启后配置、数据库和缓存目录仍存在
- [ ] doctor 没有 fail：

```bash
docker exec depsilo /app/depsilo doctor --json \
  | jq '.checks[] | select(.level == "fail")'
```

## 2. 客户端烟测

每个实际使用的生态执行“配置客户端 -> 第一次 MISS -> 第二次 HIT ->
Admin 核对日志/缓存”。不要把公网 hello-world 包当成唯一回归基准。

### PyPI

```bash
pip config set global.index-url http://localhost:23333/pypi/simple/
pip install --no-cache-dir requests
pip uninstall -y requests
pip install --no-cache-dir requests
```

- [ ] Access Logs 有 MISS 和 HIT
- [ ] Cache 页面有对应 wheel/sdist
- [ ] 不要配置 `extra-index-url` 作为自动 fallback；pip 会同时考虑多个源，
      可能绕过 Depsilo 的 451 策略

### npm

```bash
npm config set registry http://localhost:23333/npm/
npm install --no-save lodash
```

- [ ] Cache 页面有 tarball
- [ ] 客户端没有 integrity mismatch

### Go modules

```bash
GOPROXY=http://localhost:23333/go,direct \
  go install github.com/spf13/cobra@latest
```

- [ ] Cache 页面有 `@v/*.zip`
- [ ] 保持 GOSUMDB 开启
- [ ] `,direct` 只在代理返回 404/410 时继续，不是 Depsilo 宕机 fallback；
      不要改成会绕过 451 的 `|direct`

### 其他生态

按 Portal 给出的当前配置测试 cargo、maven、rubygems、composer、nuget、
conda、cran、helm、huggingface、apt、alpine 和 Docker。Docker daemon 的
`registry-mirrors` 必须指向服务根 URL，客户端会自行请求 `/v2/`；不要追加
`/docker/`。

Clean Setup Wizard 配置不会写入 `[[huggingface.upstreams]]` 或
`[[docker.registries]]`。测试 Hugging Face / Docker 前先按
`config.example.toml` 手工添加对应服务端配置并重启。

## 3. 功能验证

- [ ] Dashboard 命中率和带宽统计随 Phase 2 请求变化
- [ ] 趋势图粒度：1h/24h/7d/30d 分别返回约 360/288/336/360 个点；持续请求时 1h 图在 5 秒内更新，移动端无横向溢出。
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
- [ ] MCP initialize 可用：

```bash
curl -sf -X POST http://localhost:23333/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | jq .
```

## 4. 压力和边缘场景

- [ ] 两个客户端同时请求同一个未缓存制品：自研 inflight 只回源一次，
      两个客户端都收到完整内容，缓存只有一份
- [ ] 下载 1 GB+ 制品时内存不随文件大小线性增长
- [ ] 把 LRU 水位临时调低后，缓存使用量超过阈值会被后台清理
- [ ] 客户端中途断开后，没有长期遗留临时文件，SQLite 仍可正常打开

## 5. Prometheus 和 Webhook

代码注册了五组 collector，但当前埋点并不完整：两个 cache gauge 尚未接入
实时统计，通常只显示 `0`；三个带 label 的 counter/histogram 目前只有 Docker
handler 会更新，普通 PyPI/npm 等请求不会生成对应 series。

- `depsilo_requests_total{adapter_type="docker",hit="true|false"}`
- `depsilo_request_duration_seconds_bucket{adapter_type="docker",...}`
- `depsilo_upstream_requests_total{upstream="...",success="true|false"}`
- `depsilo_cache_size_bytes`
- `depsilo_cache_files_total`

建议基于这些真实指标告警，不要引用不存在的 `cache_hits_total`、
`upstream_latency_seconds` 或 `storage_size_bytes`。在补齐全生态和 gauge 埋点前，
也不要把当前 `/metrics` 当作缓存容量或全生态请求量的可靠告警源。

- [ ] Prometheus 能抓取 `/metrics`
- [ ] 完成 Docker MISS/HIT 后能看到 `adapter_type="docker"` series；不要求
      PyPI/npm 等 handler 产生同名 series
- [ ] 在 Admin 配置 Webhook，并在测试环境触发一次 quarantine/malware block；
      block 才会推送，approve/revoke/override 只写审计事件

## 6. 备份和恢复演练

CLI backup **只包含 `config.toml` 和 SQLite 数据库**，不包含本地/S3 缓存对象。
恢复数据库中的缓存元数据不等于恢复制品字节；缓存需单独备份或重新回源。
当前 backup 直接读取 SQLite 主文件，不使用 online-backup API，也不归档 WAL；
生成一致备份前先停止 server。

```bash
mkdir -p backups
docker stop depsilo
docker run --rm \
  -e DEPSILO_CONFIG=/root/.depsilo/config.toml \
  -e DEPSILO_DATABASE_DSN=/root/.depsilo/data/depsilo.db \
  -e DEPSILO_STORAGE_PATH=/root/.depsilo/data/cache \
  -v "$PWD/depsilo-state:/root/.depsilo" \
  -v "$PWD/backups:/backup" \
  depsilo/depsilo:0.8.0 \
  backup --out /backup/depsilo-backup.tar.gz
docker start depsilo
tar -tzf backups/depsilo-backup.tar.gz
```

- [ ] 归档只有 manifest、config 和 database；不声称包含 cache
- [ ] 备份期间没有 Depsilo server 进程写 SQLite
- [ ] 在隔离目录执行恢复演练，不删除生产 `depsilo-state`

下面的 disposable container 不启动服务器，因此满足“restore 时服务必须停止”：

```bash
rm -rf restore-drill
mkdir -p restore-drill/data
tar -xOf backups/depsilo-backup.tar.gz config.toml > restore-drill/config.toml
cp backups/depsilo-backup.tar.gz restore-drill/

docker run --rm \
  -e DEPSILO_CONFIG=/state/config.toml \
  -e DEPSILO_DATABASE_DSN=/state/data/depsilo.db \
  -v "$PWD/restore-drill:/state" \
  depsilo/depsilo:0.8.0 \
  restore /state/depsilo-backup.tar.gz

test -s restore-drill/config.toml
test -s restore-drill/data/depsilo.db
```

- [ ] 实际恢复前停止 Depsilo；不要在运行中的 server 进程旁覆盖 SQLite
- [ ] 恢复后核对用户、上游、规则和审计数据
- [ ] 单独验证缓存对象备份，或接受首次安装重新回源

## 7. 稳态观察

至少观察一周：

- [ ] `depsilo doctor` 持续无 fail
- [ ] 没有 panic/fatal：`docker logs depsilo 2>&1 | grep -iE 'panic|fatal'`
- [ ] 内存没有单调增长
- [ ] 磁盘在 LRU 水位附近稳定
- [ ] 命中率符合团队依赖重复度；不要把统一百分比当成发布门槛
- [ ] 定期复核 default admin 已改密、JWT secret、HTTPS 和 Admin 网络边界

## 报问题

提交 issue 前附上：

1. `docker exec depsilo /app/depsilo doctor --json`
2. `docker logs depsilo --tail 200`
3. Depsilo image tag 和部署方式
4. 可复现步骤及所用生态

Issue tracker: <https://github.com/depsilo/depsilo/issues>
