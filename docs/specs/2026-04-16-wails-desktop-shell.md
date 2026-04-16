# Wails Desktop Shell

## Overview

Wrap the existing Depsilo server in a Wails v2 desktop application. The same codebase produces two binaries via Go build tags: a headless server (default, no GUI dependencies) and a desktop app with a native window showing the Web UI. No system tray in this phase.

## Architecture

### Build Tag Strategy

Use `//go:build desktop` to separate desktop-specific code from the server-only binary.

```
cmd/server/
├── server.go           — StartServer() function (shared core logic)
├── main_server.go      — //go:build !desktop — CLI entry (default)
└── main_desktop.go     — //go:build desktop — Wails window + --headless fallback
```

**Why build tags instead of a runtime `--headless` flag:**
- Server binary has zero Wails/WebView dependencies — no CGO, no system libraries
- Docker, GoReleaser, existing CI all build the server binary unchanged
- Desktop binary is a separate build target via `wails build`

### Shared Core: server.go

Extract all initialization logic from the current `main.go` into a reusable `StartServer()` function:

```go
// cmd/server/server.go
package main

func StartServer(ctx context.Context) (*http.Server, error) {
    // 1. Load config (viper)
    // 2. Init logger (zap)
    // 3. Open database + AutoMigrate
    // 4. Init storage
    // 5. Create upstream pools
    // 6. Start background goroutines (health, LRU, security scanner, audit)
    // 7. Setup Gin router + register all routes
    // 8. Register all adapter handlers
    // 9. Serve embedded frontend (SPA fallback)
    // 10. Start HTTP listener
    // Returns *http.Server for lifecycle control
}
```

The function takes a `context.Context` for graceful shutdown coordination and returns the `*http.Server` so the caller can control the lifecycle.

### Server Entry: main_server.go

```go
//go:build !desktop

package main

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer cancel()

    srv, err := StartServer(ctx)
    if err != nil {
        log.Fatal(err)
    }

    <-ctx.Done()
    srv.Shutdown(context.Background())
}
```

This is the default build. Behavior is identical to the current `main.go`.

### Desktop Entry: main_desktop.go

```go
//go:build desktop

package main

func main() {
    // Check --headless flag
    for _, arg := range os.Args[1:] {
        if arg == "--headless" {
            runHeadless()
            return
        }
    }

    // Desktop mode
    ctx, cancel := context.WithCancel(context.Background())

    srv, err := StartServer(ctx)
    if err != nil {
        log.Fatal(err)
    }

    err = wails.Run(&options.App{
        Title:            "Depsilo",
        Width:            1280,
        Height:           800,
        MinWidth:         800,
        MinHeight:        600,
        AssetServer:      &assetserver.Options{Assets: webDistFS},
        BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 1},
        OnShutdown: func(ctx context.Context) {
            srv.Shutdown(ctx)
            cancel()
        },
    })
    if err != nil {
        log.Fatal(err)
    }
}

func runHeadless() {
    // Same as main_server.go
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer cancel()
    srv, err := StartServer(ctx)
    if err != nil {
        log.Fatal(err)
    }
    <-ctx.Done()
    srv.Shutdown(context.Background())
}
```

### Wails Window Content

The desktop window loads the same embedded `web/dist` frontend via Wails' `AssetServer`. The user experience is identical to accessing the Web UI in a browser — Portal, Admin, all pages work the same way. No additional frontend code is needed.

The Gin HTTP server still listens on the configured port (default 23333) for package manager clients (pip, npm, apt, etc.). The Wails window and the HTTP server run independently.

### Frontend Asset Sharing

Both modes use the same `//go:embed all:web/dist` directive. The embedded `fs.FS` is:
- Used by Gin's SPA fallback handler (server mode)
- Used by Wails' `AssetServer` (desktop mode)
- Defined in a shared file accessible to both entry points

## wails.json

```json
{
  "$schema": "https://wails.io/schemas/config.v2.json",
  "name": "Depsilo",
  "outputfilename": "depsilo-desktop",
  "frontend:dir": "web",
  "frontend:install": "npm ci",
  "frontend:build": "npm run build",
  "frontend:dev:watcher": "npm run dev",
  "frontend:dev:serverUrl": "auto",
  "wailsjsdir": "web/src",
  "tags": "desktop",
  "author": {
    "name": "Depsilo",
    "email": "hello@depsilo.com"
  }
}
```

Key setting: `"tags": "desktop"` ensures `wails build` compiles with `-tags desktop`, selecting `main_desktop.go`.

## Build Matrix

| Target | Command | Build Tag | CGO | Output |
|--------|---------|-----------|-----|--------|
| Server (default) | `go build ./cmd/server` | `!desktop` | No | `depsilo` |
| Server (GoReleaser) | `goreleaser release` | `!desktop` | No | 6 platform archives |
| Docker | `docker build .` | `!desktop` | No | Container image |
| Desktop (local) | `wails build` | `desktop` | Yes (WebView) | `depsilo-desktop` |
| Desktop (dev) | `wails dev` | `desktop` | Yes | Hot-reload dev |

## Dependencies

Desktop build adds:
- `github.com/wailsapp/wails/v2` — only compiled when `desktop` tag is set

Server build is unaffected — Wails is not imported without the build tag.

System requirements for desktop build:
- Linux: `libgtk-3-dev`, `libwebkit2gtk-4.0-dev`
- macOS: Xcode command line tools
- Windows: WebView2 Runtime (bundled with Windows 10 21H2+)

## Refactoring Scope

The main refactoring is splitting `cmd/server/main.go` into:

1. **`server.go`** — the `StartServer()` function with all initialization logic
2. **`main_server.go`** — 10 lines: signal handling + `StartServer()` call
3. **`main_desktop.go`** — 40 lines: Wails setup + `StartServer()` call + `--headless` fallback

The embedded `webDistFS` variable (currently in `main.go`) moves to a shared file or `server.go` so both entry points can access it.

## Files Changed

| File | Action |
|------|--------|
| `cmd/server/main.go` | Delete (split into below) |
| `cmd/server/server.go` | Create — `StartServer()` + shared vars |
| `cmd/server/main_server.go` | Create — `//go:build !desktop` entry |
| `cmd/server/main_desktop.go` | Create — `//go:build desktop` entry |
| `wails.json` | Create — Wails build config |
| `go.mod` | Add `github.com/wailsapp/wails/v2` (only used with desktop tag) |

## Scope Boundaries

- No system tray (waiting for Wails v3)
- No native settings window (P3)
- No platform-specific packaging (.dmg/.msi/.AppImage) (P4)
- No CI workflow for desktop builds (P4)
- No auto-updater
- No changes to existing Web UI frontend
- Existing server behavior is 100% preserved
