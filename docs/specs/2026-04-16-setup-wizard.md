# First-Run Setup Wizard

## Overview

When no config.toml exists, Depsilo starts with built-in defaults (no upstream sources). Users are redirected to a `/setup` wizard in the Web UI to configure port, storage path, ecosystems, and upstream mirrors. The wizard generates `~/.depsilo/config.toml` and restarts the server.

## Default Paths

All paths default to the user's home directory:

| Item | Default Path | Override |
|------|-------------|----------|
| Config file | `~/.depsilo/config.toml` | `DEPSILO_CONFIG` env var |
| Database | `~/.depsilo/data/depsilo.db` | `[database].dsn` in config |
| Cache storage | `~/.depsilo/data/cache` | `[storage].path` in config |

The `~/.depsilo/` directory is created automatically on first start if it doesn't exist.

## Backend

### Config Loader Changes

**File:** `internal/config/loader.go`

Current behavior: if no config file found, return error.

New behavior:
1. Check `DEPSILO_CONFIG` env var → check `./config.toml` → check `~/.depsilo/config.toml`
2. If none found: return a default config with `IsDefault: true`
3. Default config uses `~/.depsilo/` paths, port 23333, SQLite, local storage, auth disabled, empty upstream lists for all ecosystems
4. Server starts normally — Web UI works, proxy endpoints return 502 (no upstreams configured)

**File:** `internal/config/config.go`

Add to `Config` struct:

```go
IsDefault  bool   `mapstructure:"-"`  // true when using built-in defaults (no config file)
ConfigPath string `mapstructure:"-"`  // resolved path where config will be saved
```

### Config Writer

**File:** `internal/config/writer.go`

New file. Converts the setup wizard request into TOML and writes to disk.

```go
func WriteConfig(path string, data SetupRequest) error
```

- Creates parent directory (`~/.depsilo/`) if needed
- Writes TOML with comments (human-readable, matching config.example.toml style)
- Sets file permissions to 0644

`SetupRequest` struct:

```go
type SetupRequest struct {
    Server struct {
        Port int `json:"port"`
    } `json:"server"`
    Storage struct {
        Path string `json:"path"`
    } `json:"storage"`
    Ecosystems map[string]EcosystemSetup `json:"ecosystems"`
}

type EcosystemSetup struct {
    Enabled   bool             `json:"enabled"`
    Upstreams []UpstreamSetup  `json:"upstreams"`
}

type UpstreamSetup struct {
    Name     string `json:"name"`
    URL      string `json:"url"`
    Priority int    `json:"priority"`
    Proxy    string `json:"proxy,omitempty"`
}
```

### Setup API Endpoints

**File:** `internal/api/setup.go`

Two endpoints, no authentication required:

#### GET /api/v1/setup/status

```json
{
  "needs_setup": true,
  "config_path": "/home/user/.depsilo/config.toml"
}
```

`needs_setup` is true when `cfg.IsDefault == true`.

#### POST /api/v1/setup/complete

Request body: `SetupRequest` (see above).

Steps:
1. Validate: port range (1-65535), storage path not empty, at least one ecosystem enabled
2. Call `config.WriteConfig(cfg.ConfigPath, request)`
3. Return `200 { "status": "ok", "message": "restarting" }`
4. After 1-second delay, call `os.Exit(0)` — the process manager (systemd, Docker restart policy, or user) restarts the server, which now loads the new config

If setup is called when `needs_setup=false`, return `409 { "code": "ALREADY_CONFIGURED" }`.

### Route Registration

**File:** `internal/api/router.go`

Register before auth middleware (public routes):

```go
setupHandler := NewSetupHandler(deps.Config)
apiV1.GET("/setup/status", setupHandler.Status)
apiV1.POST("/setup/complete", setupHandler.Complete)
```

### Deps Changes

Pass `cfg` (with `IsDefault` and `ConfigPath`) to the API layer. Already available via `deps.Config`.

## Frontend

### Setup Status Check

**File:** `web/src/App.tsx`

At app startup, before rendering any routes:

```tsx
const { data: setupStatus } = useQuery({
  queryKey: ['setup-status'],
  queryFn: () => api.get('/setup/status'),
})

if (setupStatus?.data?.needs_setup) {
  return <SetupWizard />
}
```

This means ALL routes (Portal, Admin) are blocked until setup is complete. The wizard renders full-screen, not inside any layout.

### Setup Wizard Page

**File:** `web/src/setup/SetupWizard.tsx`

Full-screen centered card (max-width 720px), with step progress bar at top and prev/next buttons at bottom.

#### Step 1: Welcome

- Depsilo logo (large)
- One-line description: "Lightweight dependency cache for your team"
- "Get Started" button

#### Step 2: Basic Settings

- Port number input (default: 23333)
- Cache storage path input (default: `~/.depsilo/data/cache`, shown from setup/status response or hardcoded)
- Both fields pre-filled with defaults

#### Step 3: Select Ecosystems

- 4-column grid of 13 ecosystem cards
- Each card: EcosystemIcon + name + checkbox
- All checked by default
- User unchecks ecosystems they don't need
- Ecosystems: pip, apt, npm, Go, Cargo, Maven, RubyGems, Composer, NuGet, Conda, CRAN, Helm, Docker

#### Step 4: Configure Upstreams

- One collapsible section per selected ecosystem
- Each section pre-filled with default mirrors:
  - Chinese mirrors first (tuna, aliyun, npmmirror, etc.) with priority 1
  - Official source second with priority 2
  - Matching config.example.toml defaults
- User can edit name/URL/priority, add new upstreams, remove upstreams
- Optional proxy field per upstream (collapsed by default)

Default upstream data is hardcoded in `web/src/setup/defaults.ts` (matching config.example.toml).

#### Step 5: Complete

- Configuration summary (port, storage path, N ecosystems, total upstreams)
- "Save & Start" button
- On click:
  1. POST /api/v1/setup/complete with all wizard data
  2. Show loading spinner + "Saving configuration..."
  3. On success: show "Restarting server..." message
  4. Poll GET /health every second until server responds
  5. Redirect to `/` (Portal home)

### API Client

**File:** `web/src/lib/api.ts`

```typescript
// Setup (no auth)
getSetupStatus: () => api.get('/setup/status'),
completeSetup: (data: any) => api.post('/setup/complete', data),
```

### i18n Keys

Add to both `en.ts` and `zh.ts`:

```
setup.welcome: "Welcome to Depsilo" / "欢迎使用 Depsilo"
setup.welcomeDesc: "Lightweight dependency cache for your team" / "为团队打造的轻量级依赖缓存"
setup.getStarted: "Get Started" / "开始配置"

setup.basicSettings: "Basic Settings" / "基础设置"
setup.port: "Listen Port" / "监听端口"
setup.portDesc: "HTTP port for package manager clients" / "包管理器客户端使用的 HTTP 端口"
setup.storagePath: "Cache Storage Path" / "缓存存储路径"
setup.storagePathDesc: "Directory for cached packages" / "缓存包文件的存储目录"

setup.selectEcosystems: "Select Ecosystems" / "选择生态"
setup.selectEcosystemsDesc: "Choose which package managers to proxy" / "选择需要代理的包管理器"

setup.configureUpstreams: "Configure Upstream Sources" / "配置上游源"
setup.configureUpstreamsDesc: "Set mirror URLs for each ecosystem" / "为每个生态设置镜像源地址"
setup.addUpstream: "Add Upstream" / "添加上游源"
setup.removeUpstream: "Remove" / "移除"
setup.upstreamName: "Name" / "名称"
setup.upstreamUrl: "URL" / "地址"
setup.upstreamPriority: "Priority" / "优先级"
setup.upstreamProxy: "HTTP Proxy (optional)" / "HTTP 代理（可选）"

setup.complete: "Setup Complete" / "配置完成"
setup.summary: "Configuration Summary" / "配置摘要"
setup.saveAndStart: "Save & Start" / "保存并启动"
setup.saving: "Saving configuration..." / "正在保存配置..."
setup.restarting: "Restarting server..." / "正在重启服务..."
setup.waitingRestart: "Waiting for server..." / "等待服务重启..."

setup.prev: "Previous" / "上一步"
setup.next: "Next" / "下一步"
setup.step: "Step %d of %d" / "第 %d 步，共 %d 步"

setup.ecosystemCount: "%d ecosystems" / "%d 个生态"
setup.upstreamCount: "%d upstream sources" / "%d 个上游源"
```

## Files Changed

| File | Action |
|------|--------|
| `internal/config/config.go` | Add `IsDefault`, `ConfigPath` fields |
| `internal/config/loader.go` | Return defaults when no config file, resolve `~/.depsilo/` paths |
| `internal/config/writer.go` | Create: TOML config writer |
| `internal/api/setup.go` | Create: setup status + complete endpoints |
| `internal/api/router.go` | Register setup routes |
| `web/src/App.tsx` | Add setup status check + redirect |
| `web/src/setup/SetupWizard.tsx` | Create: 5-step wizard |
| `web/src/setup/defaults.ts` | Create: default upstream data per ecosystem |
| `web/src/lib/api.ts` | Add setup API methods |
| `web/src/i18n/en.ts` | Add setup i18n keys |
| `web/src/i18n/zh.ts` | Add setup i18n keys (Chinese) |

## Scope Boundaries

- No database driver selection (always SQLite)
- No S3 storage option (always local)
- No auth/JWT configuration (uses defaults)
- No Docker registry configuration (advanced)
- No security scanner configuration (uses defaults)
- Wizard only appears once (when no config.toml exists)
- After first setup, configuration changes go through Admin Settings page
- No "re-run wizard" button (user can delete config.toml to trigger it again)
