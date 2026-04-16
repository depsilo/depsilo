# GitHub Releases — Cross-Platform Binary Distribution

## Overview

Add automated cross-platform binary releases to GitHub Releases. When a semver tag (`v*.*.*`) is pushed, GitHub Actions builds binaries for 6 platform/arch combinations, packages them with config files, and uploads to GitHub Releases. Users can download and run Depsilo without Docker.

## Build Matrix

| OS | Arch | Archive | Binary Name |
|---|---|---|---|
| Linux | amd64 | `depsilo_linux_amd64.tar.gz` | `depsilo` |
| Linux | arm64 | `depsilo_linux_arm64.tar.gz` | `depsilo` |
| macOS | amd64 | `depsilo_darwin_amd64.tar.gz` | `depsilo` |
| macOS | arm64 | `depsilo_darwin_arm64.tar.gz` | `depsilo` |
| Windows | amd64 | `depsilo_windows_amd64.zip` | `depsilo.exe` |
| Windows | arm64 | `depsilo_windows_arm64.zip` | `depsilo.exe` |

Each archive contains:
- The binary
- `config.example.toml`

## SQLite Driver Migration

**Current:** `gorm.io/driver/sqlite` (wraps `mattn/go-sqlite3`, requires CGO)

**Target:** Pure Go SQLite driver to enable `CGO_ENABLED=0` cross-compilation.

**Migration path:**
1. Replace `gorm.io/driver/sqlite` with the GORM-compatible pure Go driver: `github.com/glebarez/sqlite`
2. This package wraps `modernc.org/sqlite` (pure Go, no CGO) and provides a GORM dialector with the same API
3. In `internal/db/repository.go`, change the import from `"gorm.io/driver/sqlite"` to `"github.com/glebarez/sqlite"`
4. No other code changes needed — the `sqlite.Open(dsn)` call is identical
5. Remove `gorm.io/driver/sqlite` and `github.com/mattn/go-sqlite3` from go.mod
6. Run `go mod tidy`

**WAL mode:** `PRAGMA journal_mode=WAL` works identically with `modernc.org/sqlite`.

**Testing:** Run existing test suite to verify no behavioral differences.

## Version Injection

Add `-ldflags` to inject version at build time:

```go
// cmd/server/main.go (or a new internal/version/version.go)
var (
    Version   = "dev"
    Commit    = "unknown"
    BuildDate = "unknown"
)
```

GoReleaser sets these automatically via `ldflags` config.

The existing `/health` endpoint and frontend can display the version.

## GoReleaser Configuration

**File:** `.goreleaser.yaml`

```yaml
version: 2

before:
  hooks:
    - go mod tidy

builds:
  - id: depsilo
    main: ./cmd/server
    binary: depsilo
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w
      - -X depsilo/internal/version.Version={{.Version}}
      - -X depsilo/internal/version.Commit={{.Commit}}
      - -X depsilo/internal/version.BuildDate={{.Date}}

archives:
  - id: default
    format: tar.gz
    format_overrides:
      - goos: windows
        format: zip
    files:
      - config.example.toml
    name_template: "depsilo_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: "checksums.txt"
  algorithm: sha256

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^chore:"

release:
  github:
    owner: depsilo
    name: depsilo
  prerelease: auto
  draft: false
```

## GitHub Actions Workflow

**File:** `.github/workflows/release.yml`

Triggers on semver tags. Steps:

1. Checkout code
2. Setup Node.js 20
3. Build frontend: `cd web && npm ci && npm run build`
4. Setup Go
5. Run GoReleaser (builds all 6 platforms + uploads to GitHub Releases)

```yaml
name: Release

on:
  push:
    tags:
      - 'v*.*.*'

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Set up Node.js
        uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'npm'
          cache-dependency-path: web/package-lock.json

      - name: Build frontend
        working-directory: web
        run: npm ci && npm run build

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.21'

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v6
        with:
          version: '~> v2'
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

## Relationship to Existing CI

| Workflow | Trigger | Action |
|----------|---------|--------|
| `ci.yml` | push to main, PRs | Test + Docker build to Hub |
| `docker-publish.yml` | `v*.*.*` tags | Multi-arch Docker images to Hub + GHCR |
| **`release.yml` (new)** | `v*.*.*` tags | Cross-platform binaries to GitHub Releases |

`docker-publish.yml` and `release.yml` both trigger on the same tag — they run in parallel independently.

## Version Package

**File:** `internal/version/version.go`

```go
package version

var (
    Version   = "dev"
    Commit    = "unknown"
    BuildDate = "unknown"
)
```

Update `/health` endpoint to include version:

```go
c.JSON(http.StatusOK, gin.H{
    "status":  "healthy",
    "version": version.Version,
    "uptime":  time.Since(startTime).String(),
})
```

Update `/api/v1/stats` to include version in the `service` block (if not already).

## Files Changed

| File | Action |
|------|--------|
| `.goreleaser.yaml` | Create |
| `.github/workflows/release.yml` | Create |
| `internal/version/version.go` | Create |
| `internal/db/repository.go` | Modify import: `gorm.io/driver/sqlite` → `github.com/glebarez/sqlite` |
| `go.mod` / `go.sum` | Replace sqlite driver dependency |
| `cmd/server/main.go` | Import version package, no other changes |
| `internal/api/router.go` | Update health endpoint to include version |
| `Dockerfile` | Remove `gcc musl-dev` from build stage, add `CGO_ENABLED=0` |

## Scope Boundaries

- No install scripts (P2/P3 scope)
- No systemd/launchd service files (P2 scope)
- No desktop application (P2 scope)
- No Homebrew formula or apt repo (could be follow-up)
- No code signing for macOS/Windows (could be follow-up)
- Docker workflow unchanged — it continues to work independently
