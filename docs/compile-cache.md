# 编译缓存（ccache / sccache）

Depsilo 可以为下游构建机提供共享编译缓存：ccache 使用 HTTP remote storage，sccache 使用官方 WebDAV backend。它与包代理缓存使用独立的容量账本、对象目录（或 S3 bucket）和机器凭据；开启后，Operator 可在 Admin 的“编译缓存”页面查看状态、创建凭据、撤销凭据和手动执行 LRU 清理。

建议使用 ccache 4.7+ 和 sccache v0.15.0+。ccache 支持 4.7–4.12 的 33 字符 key、4.13+ 的 40 字符 key，以及默认 `subdirs` 和 `flat` 布局，不支持 Bazel layout。sccache 的 64 字符 key 与 ccache 分属不同协议空间，两者共享凭据、namespace、容量和 LRU，但不会互相命中缓存条目。

sccache 入口只实现官方客户端需要的窄 WebDAV 子集：启动探针、`GET`、`PUT`、`PROPFIND Depth: 0` 和 `MKCOL`。它不是通用 WebDAV 文件服务，也不是远程执行用的 `sccache-dist` scheduler/build server。`[compile_cache.storage] type = "s3"` 只是 Depsilo 的内部存储 adapter，不会把 Depsilo 变成 S3-compatible endpoint，也不能让客户端绕过 Depsilo 直接访问该 bucket。

## 启用

在 `config.toml` 中加入：

```toml
[compile_cache]
enabled = true
public_url = "http://127.0.0.1:23333"
allow_insecure_http = false
max_size_gb = 20
max_entries = 500000
max_entry_size_mb = 512
namespace_max_size_gb = 20
namespace_max_entries = 250000
max_concurrent_uploads = 8
max_queued_uploads = 32
upload_timeout = "15m"
max_concurrent_downloads = 64
download_timeout = "15m"
max_inflight_upload_size_mb = 1024
lru_threshold = 90

[compile_cache.storage]
type = "local"
path = "./data/compile-cache"
```

`public_url` 是客户端实际访问的外部 origin。远程使用应填写 TLS 反向代理地址，例如 `https://depsilo.example.com`。这个字段只负责校验和生成客户端配置，不会让 Depsilo 的通用 HTTP listener 自动获得 TLS；使用反向代理时，后端端口必须只对代理、loopback 或受信网络开放。只有明确运行在可信 LAN/VPN 时，才应同时使用远程 `http://` 地址和 `allow_insecure_http = true`；否则 bearer token 和编译产物可能被窃取或篡改。

本地存储会使用 0700 目录和 0600 文件。S3 必须使用不同于包缓存的独立 bucket；建议关闭 versioning，或配置生命周期规则清理历史版本。逻辑容量之外还要为上传暂存预留 `max_inflight_upload_size_mb` 的磁盘或对象存储空间。

重启服务后打开 `/admin/compile-cache`。创建凭据时会返回一次性的 token，以及 ccache 和 sccache 客户端配置；关闭弹窗后完整 token 不会再次显示。

## ccache 接入

把管理页面给出的完整值放进环境变量：

```bash
export CCACHE_REMOTE_STORAGE='http://127.0.0.1:23333/ccache/v1/linux-ci|bearer-token=depsilo_cc_...'
ccache --zero-stats
ccache gcc -c hello.c -o hello.o
ccache --show-stats
```

这里使用 pipe 属性语法，因为它同时兼容 ccache 4.9.x 和 4.13+。不要额外设置 `layout=bazel`。

ccache 默认先查本地缓存，再查远端；若临时 CI 节点只想使用 Depsilo，可额外设置 `CCACHE_REMOTE_ONLY=true`。为 `readonly` 凭据生成的配置会自动带上 `read-only=true`，避免 cache miss 后尝试无权限的 PUT。

ccache 内置 HTTP backend 使用 `http://`。当 `public_url` 是 `https://` 时，ccache 4.13+ 需要安装官方 [`ccache-storage-http-go`](https://github.com/ccache/ccache-storage-http-go)（或其他受支持的 storage helper）；管理页面会为这种地址生成 helper 所需的 `@bearer-token` 属性。详见 [ccache remote storage 手册](https://ccache.dev/manual/4.13.6.html#_remote_storage_backends)。旧客户端若无法使用 helper，应升级，或仅在受信网络/VPN 内使用显式允许的 HTTP。

## sccache v0.15 接入

sccache 使用原生 WebDAV backend，HTTP 和 HTTPS 都不需要 ccache 的 storage helper。稳定版 v0.15.0 的配置格式如下：

```toml
[cache.webdav]
endpoint = "https://depsilo.example.com/sccache/v1/linux-ci"
token = "depsilo_cc_..."
```

也可以使用等价的环境变量：

```bash
export SCCACHE_WEBDAV_ENDPOINT='https://depsilo.example.com/sccache/v1/linux-ci'
export SCCACHE_WEBDAV_TOKEN='depsilo_cc_...'
sccache --zero-stats
sccache gcc -c hello.c -o hello.o
sccache --show-stats
```

不要在面向稳定版 v0.15.0 的配置中加入 `rw_mode` 或 `SCCACHE_WEBDAV_RW_MODE`：该选项是在 v0.15.0 发布后才进入上游 `main`。v0.15.0 会用 `.sccache_check` 写探针判断 backend 是否可写；readonly 凭据会由 Depsilo 拒绝写入，客户端随后按只读 backend 使用。

## 真实客户端回归

仓库提供一个独立的 opt-in target，它不会加入默认 unit 或 integration 测试。先启动并启用 Depsilo，再在 Admin 创建同一 namespace 的 `readwrite` 构建凭据，然后运行：

```bash
export COMPILER_CACHE_ENDPOINT='http://127.0.0.1:23333/ccache/v1/linux-ci'
bash -c '
  read -rsp "Compile-cache token: " COMPILER_CACHE_TOKEN
  printf "\n"
  export COMPILER_CACHE_TOKEN
  make test-compiler-cache
'
```

这里显式使用 Bash 读取隐藏输入，因此从 zsh、Bash 或其他父 shell 运行都一致；token 只存在于这个短生命周期子进程的环境中，不会进入命令历史或客户端 argv。

也可传入 `/sccache/v1/linux-ci` endpoint，脚本会自动推导另一个协议的地址。它要求系统已安装 ccache 4.7+、官方 sccache v0.15.0+ 和 GCC/Clang；HTTPS endpoint 要求 ccache 4.13+。脚本会校验客户端版本；缺少工具或版本过旧时会直接失败并显示提示，不会静默跳过。测试为每个客户端执行一次唯一的 compile miss 和远端写入，清空该客户端的本地状态后再次编译，并以客户端统计确认 remote hit。sccache 测试会显式使用 v0.15 支持的 `disk,webdav` 多级链，第二次编译前删除隔离的 disk L0 并重启 daemon，因此命中只能来自 Depsilo。该测试会在指定 namespace 留下少量可由 LRU 回收的缓存条目。

## 凭据与信任边界

- `readwrite` 仅分配给受信 CI/构建节点，用于读取和写入。
- `readonly` 适合只消费团队缓存的开发机；cache miss 时它不能回填远端。
- 每个凭据只绑定一个 namespace，且默认有效期为 90 天（管理页面默认创建 30 天凭据）。
- 编译缓存凭据与 Admin API token 完全分离；泄露的构建 token 不会获得管理权限。
- ccache / sccache key 是客户端提供的输入摘要，不是 Depsilo 对响应体计算的内容地址。拥有写权限的机器能污染其 namespace，因此不要把 `readwrite` token 发给不受信设备。

撤销采用软撤销并保留创建人、撤销人和时间；后续认证立即失效。已经通过认证的在途上传不会被撤销操作主动中断，但会受 `upload_timeout` 限制。删除对象失败时会写入持久化重试队列，后台继续回收。

## 容量与部署限制

Depsilo 同时执行全局字节/条目上限和单 namespace 字节/条目上限。超限时按最近最少使用顺序淘汰；并发上传数量与暂存字节也分别受限，未完整上传的请求不会触发淘汰。

上传队列有界；队列已满时服务会快速返回可重试的 503，而不是无限积压连接。并发下载数与上传、下载传输时间也分别受限，慢客户端到期后会释放文件或 S3 响应体。启动时会清理未提交的对象代，并根据当前配置重新收敛全局与各 namespace 配额，因此调低容量或条目上限会自动淘汰旧条目。当前编译缓存是单实例实现：key 锁、上传预算和容量预留都在进程内。不要让多个 Depsilo 实例共享同一个编译缓存数据库、目录或 S3 bucket。需要多副本时，应先增加分布式租约、锁和原子容量账本。

Prometheus 指标包括：

- `depsilo_compile_cache_requests_total`
- `depsilo_compile_cache_bytes_total`
- `depsilo_compile_cache_operation_duration_seconds`
- `depsilo_compile_cache_size_bytes`
- `depsilo_compile_cache_entries`
- `depsilo_compile_cache_evictions_total`
