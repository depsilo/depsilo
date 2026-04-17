# Project Management & Download Tracking

## Overview

Add project-based package tracking to Depsilo. Users create projects, each with a unique proxy URL and API token. Downloads through a project's URL (or authenticated with its token) are recorded per-project, enabling per-project SBOM generation (separate spec). Cache storage remains global — projects only track which packages were downloaded, not duplicate the files.

## Data Model

### Project

```go
type Project struct {
    ID          uint      `gorm:"primarykey" json:"id"`
    Name        string    `gorm:"size:128;uniqueIndex" json:"name"`
    Slug        string    `gorm:"size:128;uniqueIndex" json:"slug"`
    Description string    `gorm:"size:512" json:"description"`
    TokenHash   string    `gorm:"size:256;uniqueIndex" json:"-"`
    CreatedBy   string    `gorm:"size:64" json:"created_by"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

`Slug` is a URL-safe version of the name (lowercase, hyphens, no spaces). Used in URL paths: `/p/{slug}/pypi/...`.

`TokenHash` stores a SHA-256 hash of the project's API token. The plaintext token is shown once at creation time.

### ProjectPackage

Records which packages a project has actually downloaded (package files only, not metadata).

```go
type ProjectPackage struct {
    ID            uint      `gorm:"primarykey" json:"id"`
    ProjectID     uint      `gorm:"index;uniqueIndex:idx_proj_pkg" json:"project_id"`
    Ecosystem     string    `gorm:"size:16;uniqueIndex:idx_proj_pkg" json:"ecosystem"`
    PackageName   string    `gorm:"size:256;uniqueIndex:idx_proj_pkg" json:"package_name"`
    Version       string    `gorm:"size:128;uniqueIndex:idx_proj_pkg" json:"version"`
    FirstSeenAt   time.Time `json:"first_seen_at"`
    LastSeenAt    time.Time `gorm:"index" json:"last_seen_at"`
    DownloadCount int       `gorm:"default:1" json:"download_count"`
    CreatedAt     time.Time `json:"created_at"`
    UpdatedAt     time.Time `json:"updated_at"`
}
```

The unique index on `(project_id, ecosystem, package_name, version)` ensures each package+version is recorded once per project. Subsequent downloads increment `DownloadCount` and update `LastSeenAt`.

No package file data is stored here — this is metadata only. The actual cached files live in the global cache storage.

## Routing

### Method A: URL Path

All existing ecosystem routes are duplicated under `/p/{slug}/`:

```
/p/{slug}/pypi/simple/...
/p/{slug}/pypi/files/...
/p/{slug}/npm/{package}
/p/{slug}/npm/@{scope}/{package}
/p/{slug}/apt/{repo}/dists/...
/p/{slug}/go/*path
/p/{slug}/crates/*path
/p/{slug}/maven/*path
/p/{slug}/rubygems/*path
/p/{slug}/composer/*path
/p/{slug}/nuget/*path
/p/{slug}/conda/*path
/p/{slug}/cran/*path
/p/{slug}/helm/*path
/p/{slug}/v2/*path
```

Implementation: A Gin middleware group at `/p/:slug` that:
1. Looks up the project by slug
2. Sets `projectID` in the Gin context (`c.Set("project_id", project.ID)`)
3. Strips the `/p/{slug}` prefix from the request path
4. Forwards to the same adapter handlers

This avoids duplicating adapter registration. The middleware rewrites the URL and calls `c.Next()`.

### Method C: Token Authentication

Requests to the normal URLs (`/pypi/...`, `/npm/...`, etc.) can include a project token:

```
Authorization: Bearer {project-token}
```

A middleware checks for Bearer tokens that match a project's `TokenHash`. If matched, sets `projectID` in context. If no token or token doesn't match a project, the request proceeds without project tracking (existing behavior).

This middleware runs before the adapter handlers but after the existing auth middleware. It does NOT require JWT — project tokens are separate from admin tokens.

### Both Methods Coexist

- URL path method: simpler client config (just change the URL)
- Token method: no URL change needed, works in CI with env vars

If a request uses both (project URL + project token), the URL slug takes precedence.

## Package Version Extraction

New function `ExtractPackageVersion(ecosystem, cacheKey string) string` in `internal/cache/manager.go` (alongside existing `ExtractPackageName`).

| Ecosystem | Cache Key Example | Extracted Version |
|-----------|-------------------|-------------------|
| pypi | `pypi/files/.../requests-2.31.0-py3-none-any.whl` | `2.31.0` |
| pypi | `pypi/files/.../Flask-2.0.0.tar.gz` | `2.0.0` |
| npm | `npm/lodash/-/lodash-4.17.21.tgz` | `4.17.21` |
| go | `go/github.com/gin-gonic/gin/@v/v1.9.1.zip` | `v1.9.1` |
| cargo | `cargo/crates/serde/1.0.0.crate` | `1.0.0` |
| apt | `apt/.../curl_7.68.0-1ubuntu2_amd64.deb` | `7.68.0-1ubuntu2` |
| maven | `maven/.../commons-lang3/3.14.0/commons-lang3-3.14.0.jar` | `3.14.0` |
| rubygems | `rubygems/gems/rails-7.0.0.gem` | `7.0.0` |
| composer | `composer/dist/monolog/monolog/abc123.zip` | `abc123` |
| nuget | `nuget/v3/package/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg` | `13.0.3` |
| conda | `conda/.../numpy-1.24.0-py39h.tar.bz2` | `1.24.0` |
| cran | `cran/.../ggplot2_3.4.0.tar.gz` | `3.4.0` |
| helm | `helm/nginx-15.0.0.tgz` | `15.0.0` |
| docker | `docker/dockerhub/manifests/library/nginx/latest` | `latest` |

If version cannot be extracted, return empty string. SBOM entries with empty version are still valid but less useful.

## Metadata vs Package File Filter

Only package file downloads are recorded in `ProjectPackage`. Metadata requests are ignored.

**Package file detection** — a request is a package file download if the cache key matches:
- `.whl`, `.tar.gz`, `.egg`, `.zip` (pypi)
- `.tgz` (npm, helm)
- `.deb` (apt)
- `.jar`, `.pom`, `.aar` (maven)
- `.crate` (cargo)
- `.gem` (rubygems)
- `.nupkg` (nuget)
- `.tar.bz2`, `.conda` (conda)
- `.zip` (go modules — only `.zip` files, not `.info` or `.mod`)

Function: `IsPackageFile(ecosystem, cacheKey string) bool`

## Download Recording

When a request passes through the project middleware (either URL or token method), and the downstream adapter handler completes successfully (status 200), the middleware records the download asynchronously:

```go
go func() {
    if !IsPackageFile(ecosystem, cacheKey) {
        return // skip metadata
    }
    name := ExtractPackageName(ecosystem, cacheKey)
    version := ExtractPackageVersion(ecosystem, cacheKey)
    if name == "" {
        return
    }
    // Upsert ProjectPackage
    db.Clauses(clause.OnConflict{
        Columns: [...],
        DoUpdates: clause.AssignmentColumns([]string{"last_seen_at", "download_count", "updated_at"}),
    }).Create(&ProjectPackage{...})
}()
```

Recording is async (goroutine) to avoid blocking the response.

## API Endpoints

All under `proGroup` (requires JWT auth + Pro license).

### Project CRUD

```
GET    /api/v1/admin/projects                — List all projects
POST   /api/v1/admin/projects                — Create project (returns token once)
GET    /api/v1/admin/projects/:id            — Project detail + package count
PUT    /api/v1/admin/projects/:id            — Update name/description
DELETE /api/v1/admin/projects/:id            — Delete project + all its package records
```

#### POST /api/v1/admin/projects

Request:
```json
{
  "name": "AI Platform",
  "description": "Main ML training service"
}
```

Response:
```json
{
  "id": 1,
  "name": "AI Platform",
  "slug": "ai-platform",
  "token": "depsilo_proj_a1b2c3d4e5f6...",
  "proxy_url": "http://HOST:PORT/p/ai-platform",
  "description": "Main ML training service"
}
```

`token` is shown ONCE. `proxy_url` uses the request's Host header to generate the correct URL.

#### GET /api/v1/admin/projects/:id

Response includes package statistics:
```json
{
  "id": 1,
  "name": "AI Platform",
  "slug": "ai-platform",
  "description": "Main ML training service",
  "package_count": 142,
  "ecosystem_breakdown": {
    "pypi": 89,
    "npm": 45,
    "apt": 8
  },
  "last_activity_at": "2026-04-17T10:00:00Z",
  "created_at": "2026-04-15T08:00:00Z"
}
```

### Project Packages

```
GET /api/v1/admin/projects/:id/packages?ecosystem=pypi&page=1&per_page=50
```

Response:
```json
{
  "items": [
    {
      "ecosystem": "pypi",
      "package_name": "requests",
      "version": "2.31.0",
      "first_seen_at": "2026-04-15T08:30:00Z",
      "last_seen_at": "2026-04-17T09:00:00Z",
      "download_count": 47
    }
  ],
  "total": 89,
  "page": 1
}
```

### Token Regeneration

```
POST /api/v1/admin/projects/:id/token
```

Response:
```json
{
  "token": "depsilo_proj_new_token_here..."
}
```

Old token is immediately invalidated.

## Frontend

### Sidebar

Add to MainLayout under "管理" group:
```
项目管理  [Pro badge]
```

Icon: `folder_managed` from Material Symbols.

### Projects Page

Route: `/admin/projects`

- Project list table: name, package count, ecosystems badges, last activity, created date, actions (edit/delete)
- "Create Project" button → dialog (name + description) → shows token + proxy URL once
- Click project → detail page

### Project Detail Page

Route: `/admin/projects/:id`

- Header: project name + description + edit button
- Info cards: proxy URL (copyable) + token status + package count
- Packages tab: filterable table (ecosystem select + search) showing all recorded packages with version, first/last seen, download count
- SBOM export button (disabled until Spec B is implemented)

## i18n Keys

Add to both `en.ts` and `zh.ts`:

```
projects.title / 项目管理
projects.create / 创建项目
projects.name / 项目名称
projects.slug / URL 标识
projects.description / 描述
projects.token / 项目 Token
projects.tokenWarning / Token 仅显示一次，请妥善保存
projects.proxyUrl / 代理地址
projects.packageCount / 包数量
projects.lastActivity / 最后活跃
projects.regenerateToken / 重新生成 Token
projects.confirmDelete / 确认删除项目？
projects.deleteWarning / 删除后所有包记录将被清除
projects.packages / 包列表
projects.version / 版本
projects.firstSeen / 首次使用
projects.lastSeen / 最后使用
projects.downloads / 下载次数
projects.noPackages / 暂无包记录
projects.exportSbom / 导出 SBOM
projects.proRequired / 项目管理需要 Depsilo Pro
projects.proDesc / 按项目追踪依赖使用，生成 SBOM 清单。
```

## Files Changed

| File | Action |
|------|--------|
| `internal/db/models.go` | Add Project, ProjectPackage |
| `internal/db/repository.go` | Add to AutoMigrate |
| `internal/cache/manager.go` | Add ExtractPackageVersion, IsPackageFile |
| `internal/middleware/project.go` | Create: project URL + token middleware |
| `internal/api/admin/projects.go` | Create: CRUD handlers |
| `internal/api/router.go` | Register project routes + project middleware |
| `cmd/server/server.go` | Register /p/:slug group with project middleware |
| `web/src/admin/pages/Projects.tsx` | Create: project list + detail page |
| `web/src/admin/AdminApp.tsx` | Add route |
| `web/src/admin/components/MainLayout.tsx` | Add sidebar item |
| `web/src/lib/api.ts` | Add project API methods |
| `web/src/i18n/en.ts` + `zh.ts` | Add i18n keys |

## Scope Boundaries

- No SBOM generation (Spec B)
- No license compliance scanning
- No dependency tree analysis (all downloads treated as direct dependencies)
- No project quotas or limits
- No multi-user project permissions (all admins can see all projects)
- Deleting a project removes its package records but not the cached files
- Packages downloaded without a project context are not tracked (existing behavior)
