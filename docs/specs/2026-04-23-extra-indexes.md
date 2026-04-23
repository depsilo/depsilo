# Extra PyPI Indexes（额外 PyPI 兼容源代理）

## 问题

PyTorch、vLLM 等项目维护独立的 PyPI 兼容源（如 `download.pytorch.org/whl/cu130`），提供特定 CUDA 版本的 wheel。用户通过 `pip --extra-index-url` 使用这些源。

当 `PIP_INDEX_URL` 指向 Depsilo 时：
1. Depsilo 从 pypi.org 代理到 CPU 版 `torch-2.11.0`
2. pip 优先选择无 local tag 的 CPU 版本（而非 CUDA `torch-2.11.0+cu130`）
3. 下载 2GB CPU wheel 失败或得到错误版本
4. extra-index-url 的 CUDA wheel 被忽略

## 目标

让 Depsilo 能代理任意数量的 PyPI 兼容源，每个挂在独立路径上，所有流量走缓存。

## 配置

```toml
# config.toml

# 已有的标准 PyPI（不变）
[[pypi.upstreams]]
name = "tuna"
url = "https://pypi.tuna.tsinghua.edu.cn"
priority = 1

# 新增：额外 PyPI 兼容源
[[extra_indexes]]
name = "pytorch-cu130"
path = "pypi-torch-cu130"
[[extra_indexes.upstreams]]
name = "pytorch"
url = "https://download.pytorch.org/whl/cu130"
priority = 1

[[extra_indexes]]
name = "vllm-cu130"
path = "pypi-vllm-cu130"
[[extra_indexes.upstreams]]
name = "vllm-wheels"
url = "https://wheels.vllm.ai/0.19.1/cu130"
priority = 1
```

每个 `extra_indexes` 条目生成一个独立的 PyPI adapter 实例，挂载到 `/{path}/` 路径。

## 路由

```
GET /{path}/simple/                  → 顶层索引（passthrough）
GET /{path}/simple/:package/         → 包索引（URL 重写，href 指向 /{path}/files/...）
GET /{path}/simple/:package          → 重定向到 /{path}/simple/:package/
GET /{path}/files/*filepath          → 文件下载（缓存）
```

示例：
- `GET /pypi-torch-cu130/simple/torch/` → 代理 `download.pytorch.org/whl/cu130/torch/`
- `GET /pypi-torch-cu130/files/cu130/torch-2.11.0+cu130-cp312-cp312-manylinux_2_28_x86_64.whl` → 缓存下载

## 实现改动

### 1. config.go — 新增 ExtraIndexConfig

```go
type ExtraIndexConfig struct {
    Name      string           `mapstructure:"name"`
    Path      string           `mapstructure:"path"`      // 路由路径（不含前导 /）
    Upstreams []UpstreamConfig `mapstructure:"upstreams"`
}
```

在 `Config` 结构体中加：

```go
ExtraIndexes []ExtraIndexConfig `mapstructure:"extra_indexes"`
```

### 2. PyPI adapter — 路径前缀可配置

当前 handler 中有 4 处硬编码 `/pypi/`：
- `handler.go:76` — redirect 路径
- `handler.go:140` — 缓存 HTML 中 URL 替换
- `rewriter.go:39,44` — URL 重写目标路径
- `keyer.go:7,12` — 缓存 key 前缀

改为通过 Handler 字段传入：

```go
type Handler struct {
    cacheMgr   *cache.Manager
    selector   upstream.Selector
    cfg        config.CacheConfig
    db         *gorm.DB
    pathPrefix string  // "/pypi" 或 "/pypi-torch-cu130"
    adapterID  string  // "pypi" 或 "extra:pytorch-cu130"（缓存 key + 日志）
}
```

- `New()` 签名不变（默认 pathPrefix="/pypi", adapterID="pypi"）
- 新增 `NewWithPrefix(cacheMgr, selector, cfg, db, pathPrefix, adapterID)` 用于 extra indexes

### 3. server.go — 注册额外实例

在 adapter 注册循环之后，加上：

```go
for _, idx := range cfg.ExtraIndexes {
    pool, err := upstream.NewPool(idx.Upstreams)
    if err != nil {
        return nil, fmt.Errorf("create %s pool: %w", idx.Name, err)
    }
    syncUpstreams(database, "extra:"+idx.Name, idx.Upstreams)
    handler := pypi.NewWithPrefix(cacheMgr, upstream.NewPrioritySelector(pool), cfg.Cache, database, "/"+idx.Path, "extra:"+idx.Name)
    handler.Register(r.Group("/" + idx.Path))
    go upstream.StartHealthCheck(ctx, pool, database, 30*time.Second)
    zap.L().Info("extra index registered", zap.String("name", idx.Name), zap.String("path", "/"+idx.Path))
}
```

### 4. 不需要改的

- 缓存引擎不变（不同 adapterID 自然隔离 key）
- 前端不改（admin 上游管理页面自动展示 `extra:*` 类型的上游）
- URL 重写逻辑不变（只是前缀换了）
- i18n 不需要新 key

## 用户使用

### config.toml
```toml
[[extra_indexes]]
name = "pytorch-cu130"
path = "pypi-torch-cu130"
[[extra_indexes.upstreams]]
name = "pytorch"
url = "https://download.pytorch.org/whl/cu130"
priority = 1
```

### Dockerfile
```dockerfile
ARG PIP_INDEX_URL=http://10.4.20.52:23333/pypi/simple/
ARG PIP_TRUSTED_HOST=10.4.20.52

RUN pip3 install vllm \
    --extra-index-url http://10.4.20.52:23333/pypi-torch-cu130/simple/ \
    --extra-index-url http://10.4.20.52:23333/pypi-vllm-cu130/simple/
```

### Makefile
```makefile
build: PIP_INDEX_URL       = http://10.4.20.52:23333/pypi/simple/
build: PIP_TRUSTED_HOST    = 10.4.20.52
build: PIP_EXTRA_TORCH     = http://10.4.20.52:23333/pypi-torch-cu130/simple/
build: PIP_EXTRA_VLLM      = http://10.4.20.52:23333/pypi-vllm-cu130/simple/
```

## 不在范围内

- npm/Cargo 等其他生态的 extra index（可后续复用相同模式）
- 前端 QuickStart 页面更新（config 足够清晰）
- 自动发现上游 index 类型
