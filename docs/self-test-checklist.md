# Depsilo 自测前置清单

> 部署前后可重复执行的最小验证路径。示例使用官方 `latest` 镜像、端口
> `23333` 和单节点 SQLite；正式环境可将镜像 tag 固定为具体 `X.Y.Z` 版本。
> `v0.9.0` 起已包含 blocklist / tamper detection。

## 0. 部署前

- [ ] Docker 20.10+：`docker --version`
- [ ] 端口可用：`ss -tlnp | grep :23333` 应无结果
- [ ] 磁盘至少 30 GB：默认缓存上限 20 GB，另留数据库、日志和清理余量
- [ ] 明确单节点边界：当前数据库只支持 SQLite；制品可存本地或 S3，
      但 S3 不会把 SQLite 部署变成多实例/HA
- [ ] 列出团队真实使用的生态。Depsilo 有 14 个常规生态路由，另有独立
      Docker OCI `/v2/`，共 15 个 install surfaces

## 1. 首次部署

空配置部署只需将 Depsilo 的统一 state root 挂载为一个 volume。生成的配置、
SQLite 数据库、本地缓存和其他状态都会保存在该 volume 中。

```bash
docker run -d --name depsilo \
  -p 23333:23333 \
  -v depsilo-data:/root/.depsilo \
  --restart unless-stopped \
  ghcr.io/depsilo/depsilo:latest
```

- [ ] `curl -sf http://localhost:23333/health | jq .` 返回 healthy
- [ ] 运行 `docker exec depsilo depsilo setup token` 取得初始化令牌（启动输出里
      也有，可用 `docker logs depsilo` 查看；初始化完成前每次重启都会更换），首次打开
      `http://localhost:23333/` 后完成 Setup Wizard。向导配置服务并创建首个管理员
- [ ] `/api/v1/discover` 的 `ecosystems` 只列出已配置上游的生态：向导默认配置为
      13 个，手工加入 Hugging Face 后为 14 个；Docker OCI 单独走 `/v2/`
- [ ] 使用向导中创建的管理员登录 `/admin`；系统不存在默认 `admin/admin` 密码
- [ ] 容器重启后配置、数据库和缓存目录仍存在
- [ ] 完成初始化后 doctor 没有 fail。初始化完成前，doctor 的 `Initial setup`
      检查为 fail，退出码为 1：

```bash
docker exec depsilo depsilo doctor --json \
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
- [ ] 已有 lockfile 的项目在空的客户端缓存下安装后，Access Logs 中有这次安装的
      tarball 请求。安装没有报错不代表经过了 Depsilo：如果 Access Logs 中没有
      这些请求（安装绕过了 Depsilo），或者安装返回 404 `NPM_UNSIGNED_TARBALL`
      （Access Logs 会记录这次 404），按
      [Node lockfiles](package-rules.md#node-lockfiles) 调整 lockfile 后重试

### Go modules

```bash
GOPROXY=http://localhost:23333/go,direct \
  go mod download github.com/spf13/cobra@latest
```

- [ ] Cache 页面有 `@v/*.zip`
- [ ] 保持 GOSUMDB 为默认值，不要设为 `off`。Depsilo 会把校验和数据库请求
      （`/go/sumdb/`）转发给 Go 上游，由 go 命令自行验证；Access Logs 中有
      `go/sumdb/` 请求
- [ ] `curl -s -o /dev/null -w '%{http_code}\n' http://localhost:23333/go/sumdb/sum.golang.org/supported`
      在向导默认上游下返回 200。返回 404 表示没有 Go 上游镜像该数据库，或者
      Go 上游当前都连不上（先确认 Go 上游可达）；此时 go 命令会改为直连
      `sum.golang.org`
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
  -H "Authorization: Bearer ${DEPSILO_READ_TOKEN:?set a readonly API token}" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | jq .
```

## 4. 压力和边缘场景

- [ ] 两个客户端同时请求同一个未缓存制品：自研 inflight 只回源一次，
      两个客户端都收到完整内容，缓存只有一份
- [ ] 下载 1 GB+ 制品时内存不随文件大小线性增长
- [ ] 把 LRU 水位临时调低后，缓存使用量超过阈值会被后台清理
- [ ] 客户端中途断开后，没有长期遗留临时文件，SQLite 仍可正常打开

## 5. Prometheus 和 Webhook

每个生态的请求都会计入下面前两组指标，`adapter_type` 是生态名，例如 `npm`、
`go`、`docker`；回源的请求还会计入第三组。两个 cache gauge 取自缓存清单，
启动时刷新一次，之后每分钟刷新一次。

- `depsilo_requests_total{adapter_type="...",hit="true|false"}`
- `depsilo_request_duration_seconds_bucket{adapter_type="...",...}`
- `depsilo_upstream_requests_total{upstream="...",success="true|false"}`
- `depsilo_cache_size_bytes`
- `depsilo_cache_files_total`

建议基于这些真实指标告警，不要引用不存在的 `cache_hits_total`、
`upstream_latency_seconds` 或 `storage_size_bytes`。

- [ ] Prometheus 能抓取 `/metrics`
- [ ] 完成第 2 节某个生态的 MISS/HIT 后，能看到该生态的 series，例如
      `adapter_type="npm"`
- [ ] 有制品进入缓存后，约一分钟内 `depsilo_cache_files_total` 和
      `depsilo_cache_size_bytes` 不再是 0
- [ ] 在 Admin 配置 Webhook，并在测试环境触发一次 quarantine/malware block；
      block 才会推送，approve/revoke/override 只写审计事件

## 6. 备份和恢复演练

CLI backup **只包含 `config.toml` 和 SQLite 数据库**，不包含本地/S3 缓存对象。
恢复数据库中的缓存元数据不等于恢复制品字节；缓存需单独备份或重新回源。
backup v2 使用 SQLite `VACUUM INTO` 在线生成一致快照，会包含仍在 WAL 中的已提交事务；
manifest 记录格式版本、文件大小和 SHA-256。创建备份时无需停止 server。
restore 只接受完成校验的 v2 归档；旧 v1 归档应先在隔离环境中恢复并重新备份。
恢复以持久 journal 执行，进程在切换中途退出后，下一次 server 启动或 restore
会自动收敛到完整旧状态或完整新状态。Unix 上数据库目录必须由 Depsilo 服务账号
持有且不可被 group/other 写入（建议 `0750` 或 `0700`）；共享卷请在启动前完成
`chown`/`chmod`，否则服务会为保护运行锁而拒绝启动。

```bash
mkdir -p backups
docker exec depsilo depsilo backup --out /tmp/depsilo-backup.tar.gz
docker cp depsilo:/tmp/depsilo-backup.tar.gz backups/depsilo-backup.tar.gz
docker exec depsilo rm /tmp/depsilo-backup.tar.gz
tar -tzf backups/depsilo-backup.tar.gz
```

- [ ] 归档只有 manifest、config 和 database；不声称包含 cache
- [ ] `manifest.json` 为 `depsilo-backup` v2，两个状态文件都有 SHA-256
- [ ] server 正常运行时也能完成备份，并能在隔离 SQLite 中读到备份前已提交的数据
- [ ] 在隔离目录执行恢复演练，不删除生产 volume `depsilo-data`

下面的 disposable container 不启动服务器，因此满足“restore 时服务必须停止”：

```bash
restore_dir="$(mktemp -d "${TMPDIR:-/tmp}/depsilo-restore-drill.XXXXXX")"
mkdir -p "$restore_dir/data"
tar -xOf backups/depsilo-backup.tar.gz config.toml > "$restore_dir/config.toml"
cp backups/depsilo-backup.tar.gz "$restore_dir/"

docker run --rm \
  --user "$(id -u):$(id -g)" \
  -e HOME=/tmp/depsilo-home \
  -e DEPSILO_CONFIG=/state/config.toml \
  -e DEPSILO_DATABASE_DSN=/state/data/depsilo.db \
  -v "$restore_dir:/state" \
  ghcr.io/depsilo/depsilo:latest \
  restore /state/depsilo-backup.tar.gz \
    --config-target /state/config.toml \
    --database-target /state/data/depsilo.db

test -s "$restore_dir/config.toml"
test -s "$restore_dir/data/depsilo.db"
printf 'restore drill retained for inspection: %s\n' "$restore_dir"
```

- [ ] 实际恢复前停止 Depsilo；运行锁存在时 restore 必须拒绝覆盖 SQLite
- [ ] 数据库目录权限为 `0750` 或更严格，且归 Depsilo 服务账号所有
- [ ] checksum、TOML 或 SQLite 完整性校验失败时，现有状态保持不变
- [ ] 在测试环境中断一次 restore，确认下一次启动能自动完成或安全回滚
- [ ] 恢复后的 `config.toml` 和数据库权限为 `0600`，旧状态保留在
      `*.pre-restore-*.bak`
- [ ] 恢复后核对用户、上游、规则和审计数据
- [ ] 单独验证缓存对象备份，或接受首次安装重新回源

## 7. 稳态观察

至少观察一周：

- [ ] `depsilo doctor` 持续无 fail
- [ ] 没有 panic/fatal：`docker logs depsilo 2>&1 | grep -iE 'panic|fatal'`
- [ ] 内存没有单调增长
- [ ] 磁盘在 LRU 水位附近稳定
- [ ] 命中率符合团队依赖重复度；不要把统一百分比当成发布门槛
- [ ] 定期复核管理员账号和密码、JWT secret、HTTPS 和 Admin 网络边界

## 报问题

提交 issue 前附上：

1. `docker exec depsilo depsilo doctor --json`
2. `docker logs depsilo --tail 200`
3. Depsilo image tag 和部署方式
4. 可复现步骤及所用生态

Issue tracker: <https://github.com/depsilo/depsilo/issues>
