# Package Security Intelligence

## Overview

Add a Pro feature that monitors cached packages for known vulnerabilities by querying OSV.dev, stores results locally, displays security insights in the admin UI, and optionally auto-blocks packages exceeding configurable CVSS thresholds per ecosystem. Supports offline vulnerability data import for air-gapped environments.

## Data Model

### Vulnerability

Stores individual vulnerability entries from OSV.dev or manual import.

```go
type Vulnerability struct {
    ID              uint      `gorm:"primarykey"`
    OSVID           string    `gorm:"size:64;uniqueIndex"`    // "GHSA-xxx" or "CVE-2021-44228"
    Ecosystem       string    `gorm:"size:16;index"`          // "pypi", "npm", etc.
    PackageName     string    `gorm:"size:256;index"`         // normalized package name
    AffectedRanges  string    `gorm:"type:text"`              // JSON: [{"introduced":"0","fixed":"2.28.0"},...]
    Severity        string    `gorm:"size:16;index"`          // "critical", "high", "medium", "low"
    CVSSScore       float32   `gorm:"default:0"`
    Summary         string    `gorm:"type:text"`              // one-line description
    Details         string    `gorm:"type:text"`              // full description (optional)
    Aliases         string    `gorm:"size:512"`               // comma-separated: "CVE-2023-xxx,GHSA-xxx"
    References      string    `gorm:"type:text"`              // JSON array of URLs
    PublishedAt     time.Time `gorm:"index"`
    ModifiedAt      time.Time
    CreatedAt       time.Time
    UpdatedAt       time.Time
}
```

**AffectedRanges JSON format** (mirrors OSV schema):

```json
[
  {"type": "SEMVER", "events": [{"introduced": "0"}, {"fixed": "2.28.0"}]},
  {"type": "SEMVER", "events": [{"introduced": "3.0.0"}, {"fixed": "3.1.2"}]}
]
```

### VulnerabilityCheck

Tracks when each package was last checked against OSV. Caches "no vulnerabilities" results to avoid redundant API calls.

```go
type VulnerabilityCheck struct {
    ID                uint      `gorm:"primarykey"`
    Ecosystem         string    `gorm:"size:16;uniqueIndex:idx_eco_pkg"`
    PackageName       string    `gorm:"size:256;uniqueIndex:idx_eco_pkg"`
    HasVulnerabilities bool     `gorm:"default:false"`
    VulnerabilityCount int      `gorm:"default:0"`
    LastFetchedAt     time.Time `gorm:"index"`
    NextFetchAt       time.Time `gorm:"index"`     // last_fetched_at + TTL
    CreatedAt         time.Time
    UpdatedAt         time.Time
}
```

### SecurityPolicy

Per-ecosystem auto-block configuration.

```go
type SecurityPolicy struct {
    ID              uint      `gorm:"primarykey"`
    Ecosystem       string    `gorm:"size:16;uniqueIndex"`   // "pypi", "npm", etc.
    AutoBlockEnabled bool     `gorm:"default:false"`
    MinCVSSScore    float32   `gorm:"default:9.0"`           // >= this score → auto-block
    CreatedBy       string    `gorm:"size:64"`
    CreatedAt       time.Time
    UpdatedAt       time.Time
}
```

### DismissedVuln

Tracks suggestions that the admin has dismissed (so they don't reappear).

```go
type DismissedVuln struct {
    ID              uint      `gorm:"primarykey"`
    VulnerabilityID uint      `gorm:"uniqueIndex:idx_dismissed"`
    DismissedBy     string    `gorm:"size:64"`
    CreatedAt       time.Time
}
```

**No "SuggestedRule" table.** Suggestions are computed dynamically by joining Vulnerability + CacheEntry, filtered to exclude:
1. Packages that already have a matching PackageRule
2. Vulnerabilities in the DismissedVuln table

When approved, a suggestion creates a standard PackageRule with `reason` containing the OSV ID.

## Backend Architecture

### Package: `internal/security/`

#### fetcher.go — OSV.dev API Client

Wraps the OSV.dev v1 API.

**Endpoints used:**
- `POST https://api.osv.dev/v1/query` — single package query
- `POST https://api.osv.dev/v1/querybatch` — batch query (up to 1000 per request)

**Query format:**

```json
{
  "package": {
    "name": "requests",
    "ecosystem": "PyPI"
  }
}
```

**Ecosystem mapping** (Depsilo → OSV):

| Depsilo | OSV ecosystem |
|---------|--------------|
| pypi | PyPI |
| npm | npm |
| go | Go |
| cargo | crates.io |
| maven | Maven |
| nuget | NuGet |
| composer | Packagist |
| rubygems | RubyGems (upstream mapping only; automatic scans safety-disabled until cache identities carry trusted index/gemspec provenance) |
| conda | (not supported by OSV — skip) |
| cran | CRAN |
| helm | (not supported by OSV — skip) |
| apt | Debian |
| docker | (not supported by OSV — skip) |

Unsupported ecosystems are silently skipped during scanning.
Depsilo also skips NuGet and RubyGems automatic scans even though OSV defines
those ecosystems: the current cache keys do not preserve a trustworthy NuGet
catalog ID or an unambiguous RubyGems name/version/platform identity. Persisting
an empty response for either guessed identity would create false-clean state.
For PyPI, simple-index cache keys and strict PEP 427 wheel or PEP 625 sdist
filenames are accepted. Other `/files/` artifacts retain an empty package
identity and are skipped by both immediate and periodic scans instead of
guessing a name from the filename.

**Rate limiting:** Max 1 request/second to OSV.dev (use `time.Ticker`). Batch queries reduce total calls.

**Retry:** 3 attempts with exponential backoff (1s, 2s, 4s) on 5xx errors. 429 responses honor `Retry-After` header.

**HTTP client:** Standard `net/http` with 30-second timeout. Configurable proxy via config.

#### scanner.go — Scan Coordinator

**ScanAll(ctx context.Context) error:**
1. Query `SELECT DISTINCT package_name, adapter_type FROM cache_entries`
2. Filter out ecosystems not supported by OSV
3. Filter out packages where `VulnerabilityCheck.next_fetch_at > now()` (not yet due)
4. Group remaining packages by ecosystem
5. Call `fetcher.QueryBatch()` per ecosystem (batches of 1000)
6. Parse responses → upsert Vulnerability rows (OSVID as unique key)
7. Update VulnerabilityCheck for every queried package (even zero-result)
8. For packages with vulnerabilities that match a SecurityPolicy threshold: auto-create PackageRule entries

**ScanPackage(ctx context.Context, ecosystem, packageName string) error:**
1. Check VulnerabilityCheck: if `next_fetch_at > now()`, return early (cached)
2. Call `fetcher.Query(ecosystem, packageName)`
3. Upsert Vulnerability rows
4. Update VulnerabilityCheck
5. Check SecurityPolicy → auto-create PackageRule if needed

**Background goroutine:**
- Runs `ScanAll` on startup (after 30-second delay to let server boot)
- Then every `scan_interval` (default 24h, configurable)
- Listens on `ctx.Done()` for graceful shutdown
- Logs progress: `zap.Info("security scan started")`, `zap.Info("security scan complete", zap.Int("packages", n), zap.Int("vulnerabilities", v))`

#### policy.go — Policy Evaluation

**ShouldBlock(ecosystem, packageName, version string) (block bool, vuln *Vulnerability, err error):**
1. Load SecurityPolicy for ecosystem. If `auto_block_enabled == false`, return false.
2. Query Vulnerability table: `WHERE ecosystem = ? AND package_name = ?`
3. For each vulnerability, check if `version` falls within `AffectedRanges`
4. If any matching vulnerability has `CVSSScore >= policy.MinCVSSScore`, return true + the highest-CVSS vulnerability

**Version range matching:**
- Parse `AffectedRanges` JSON
- For SEMVER type: use semver comparison
- For ECOSYSTEM type: use ecosystem-specific version comparison (reuse `compareVersions()` from rules/engine.go where applicable)
- Conservative: if version comparison fails (unparseable version), do NOT block (fail-open)

> **Erratum / current status (2026-09-02):** the shared comparator described
> above was unsafe across ecosystems and is not the current implementation.
> Automatic OSV-to-Rule projection is safety-disabled; reviewed manual rules
> use validated ecosystem dialects. See [Package Rule semantics](../package-rules.md).

**Caching:** In-memory cache keyed by `ecosystem:packageName:version`, TTL 5 minutes, invalidated when scanner updates Vulnerability data.

#### importer.go — Offline Import

**Import(ctx context.Context, data []byte) (count int, err error):**
1. Parse JSON — accept either:
   - OSV format: array of OSV vulnerability objects
   - Single OSV vulnerability object
2. For each entry: extract ecosystem, package name, affected ranges, severity
3. Upsert into Vulnerability table (OSVID as unique key)
4. Update VulnerabilityCheck for affected packages
5. Return count of imported/updated entries

### Integration Points

#### New package trigger

In `internal/cache/manager.go`, after a successful cache miss + upstream fetch + cache write, call the scanner asynchronously:

```go
// After writing to cache (existing code around line 320)
go func() {
    if securityScanner != nil {
        securityScanner.ScanPackage(context.Background(), adapterType, packageName)
    }
}()
```

The scanner is set on the Manager via a `SetSecurityScanner()` method (same pattern as `SetAuditLogger()`).

#### Auto-block integration

When `scanner.ScanPackage` or `scanner.ScanAll` finds a vulnerability matching a SecurityPolicy threshold, it creates a PackageRule:

```go
rule := db.PackageRule{
    Ecosystem:   ecosystem,
    PackageName: packageName,
    Version:     fmt.Sprintf("<%s", fixedVersion),  // e.g. "<2.28.0", derived from OSV "fixed" event
    Action:      "deny",
    Reason:      fmt.Sprintf("Auto-blocked: %s (CVSS %.1f)", osvID, cvssScore),
    CreatedBy:   "security-scanner",
}
```

Rules created by `security-scanner` are distinguishable from manual rules by the `CreatedBy` field.

## API Endpoints

All under `proGroup` (requires JWT auth + Pro license).

### Dashboard

```
GET /api/v1/admin/security/dashboard
```

Response:

```json
{
  "total_vulnerabilities": 42,
  "affected_packages": 18,
  "by_severity": {
    "critical": 3,
    "high": 12,
    "medium": 20,
    "low": 7
  },
  "auto_blocked_count": 5,
  "last_scan_at": "2026-04-15T10:00:00Z",
  "next_scan_at": "2026-04-16T10:00:00Z",
  "scan_in_progress": false
}
```

### Vulnerabilities List

```
GET /api/v1/admin/security/vulnerabilities?ecosystem=pypi&severity=critical&package=requests&page=1&per_page=20
```

Response: paginated list of Vulnerability records.

### Affected Packages

```
GET /api/v1/admin/security/packages?ecosystem=pypi&severity=high&page=1&per_page=20
```

Response: CacheEntry records joined with matching Vulnerability data. Each entry includes the package name, ecosystem, cached file count, total size, and list of matching CVEs with severity.

### Suggestions

```
GET /api/v1/admin/security/suggestions?ecosystem=pypi&page=1&per_page=20
```

Response: list of vulnerability + package combinations that do NOT yet have a corresponding PackageRule. Each suggestion includes:
- Vulnerability info (OSV ID, severity, CVSS, affected versions)
- Affected cached package info (name, ecosystem, cache hit count)
- Proposed rule (ecosystem, package_name, version constraint, action=deny)

```
POST /api/v1/admin/security/suggestions/:vuln_id/approve
```

Body (optional overrides):
```json
{
  "version": "<2.28.0",
  "reason": "CVE-2023-32681 — CVSS 7.5"
}
```

Creates a PackageRule with `action=deny`, `created_by=admin` (from JWT).

```
POST /api/v1/admin/security/suggestions/:vuln_id/dismiss
```

Marks the suggestion as dismissed (adds an `is_dismissed` field to Vulnerability or a separate small table). Dismissed suggestions won't appear in the list.

### Scan Control

```
POST /api/v1/admin/security/scan
```

Triggers a full scan immediately. Returns 202 Accepted if scan started, 409 Conflict if already running.

### Import

```
POST /api/v1/admin/security/import
Content-Type: multipart/form-data
```

Accepts a JSON file upload. Returns count of imported/updated vulnerabilities.

### Policies

```
GET /api/v1/admin/security/policies
```

Returns all SecurityPolicy records (one per ecosystem). Ecosystems without a policy return defaults (`auto_block_enabled=false, min_cvss_score=9.0`).

```
PUT /api/v1/admin/security/policies/:ecosystem
```

Body:
```json
{
  "auto_block_enabled": true,
  "min_cvss_score": 7.0
}
```

## Frontend

### Sidebar

Add to MainLayout under "管理" group, after "包治理":

```
包安全  [Pro badge]
```

Icon: `Shield` from lucide-react.

### Page: Security.tsx

Route: `/admin/security`

4 tabs:

#### Tab 1: 总览

- 4 MetricCards: 漏洞总数、受影响包数、Critical 数量、上次扫描时间
- Severity 分布饼图 (recharts PieChart)
- 「立即扫描」按钮（POST /scan, disabled when scan_in_progress）
- 最近发现的 5 条 Critical/High 漏洞列表

#### Tab 2: 漏洞列表

- 筛选器: 生态 select + severity select + 包名搜索
- 表格列: OSV ID (链接到 source_url)、生态 badge、包名、severity badge (颜色编码)、CVSS 分数、发布日期
- 分页（每页 20 条）
- 点击行可展开查看 affected ranges 和详情

#### Tab 3: 建议规则

- 卡片列表，每条包含:
  - 漏洞标题 + severity badge + CVSS
  - 受影响的已缓存包名 + 当前缓存命中次数
  - 建议的版本约束（如 `<2.28.0`）
  - 两个按钮：「审批拦截」(绿色) / 「忽略」(灰色)
- 审批后卡片消失，toast 提示「规则已创建」
- 空状态：「暂无建议规则」

#### Tab 4: 策略配置

- 表格：每个生态一行
  - 生态图标 + 名称
  - 开关：自动拦截
  - CVSS 阈值 number input（开关关闭时置灰）
  - 保存按钮（per row 或统一保存）
- 底部：离线导入区域
  - 文件上传 dropzone（接受 .json）
  - 上传后显示导入结果（N 条漏洞已导入/更新）

### i18n Keys

Add to both `en.ts` and `zh.ts`:

```
security.title: "Package Security" / "包安全"
security.overview: "Overview" / "总览"
security.vulnerabilities: "Vulnerabilities" / "漏洞列表"
security.suggestions: "Suggested Rules" / "建议规则"
security.policies: "Policies" / "策略配置"

security.totalVulnerabilities: "Total Vulnerabilities" / "漏洞总数"
security.affectedPackages: "Affected Packages" / "受影响包"
security.criticalCount: "Critical" / "严重漏洞"
security.lastScan: "Last Scan" / "上次扫描"

security.severity: "Severity" / "严重性"
security.critical: "Critical" / "严重"
security.high: "High" / "高危"
security.medium: "Medium" / "中危"
security.low: "Low" / "低危"

security.scanNow: "Scan Now" / "立即扫描"
security.scanning: "Scanning..." / "扫描中..."
security.scanStarted: "Scan started" / "扫描已启动"
security.scanConflict: "Scan already in progress" / "扫描正在进行中"

security.osvId: "OSV ID" / "OSV ID"
security.cvssScore: "CVSS Score" / "CVSS 分数"
security.affectedVersions: "Affected Versions" / "受影响版本"
security.publishedAt: "Published" / "发布日期"

security.approve: "Block" / "审批拦截"
security.dismiss: "Dismiss" / "忽略"
security.ruleCreated: "Rule created" / "规则已创建"
security.noSuggestions: "No suggestions" / "暂无建议规则"

security.autoBlock: "Auto Block" / "自动拦截"
security.minCvss: "Min CVSS" / "最低 CVSS"
security.importFile: "Import Vulnerabilities" / "导入漏洞数据"
security.importSuccess: "%d vulnerabilities imported" / "已导入 %d 条漏洞"
security.importFormat: "OSV JSON format" / "OSV JSON 格式"

security.proRequired: "Package Security requires Depsilo Pro" / "包安全功能需要 Depsilo Pro"
security.proDesc: "Monitor cached packages for known CVEs, auto-block vulnerable versions." / "监控缓存包的已知漏洞，自动拦截高危版本。"
```

## Configuration

Add to `config.example.toml`:

```toml
[security]
enabled = true
osv_api_url = "https://api.osv.dev"    # OSV.dev API endpoint
scan_interval = "24h"                   # full scan interval
check_ttl = "24h"                       # per-package check cache TTL
# proxy = "http://127.0.0.1:7890"      # optional proxy for OSV.dev API
```

Add to `internal/config/config.go`:

```go
type SecurityConfig struct {
    Enabled      bool          `mapstructure:"enabled"`
    OSVURL       string        `mapstructure:"osv_api_url"`
    ScanInterval time.Duration `mapstructure:"scan_interval"`
    CheckTTL     time.Duration `mapstructure:"check_ttl"`
    Proxy        string        `mapstructure:"proxy"`
}
```

Defaults: `enabled=true`, `osv_api_url=https://api.osv.dev`, `scan_interval=24h`, `check_ttl=24h`.

## OSV Query Caching Strategy

**Goal:** Minimize external API calls. A fresh Depsilo instance with 10,000 cached packages should complete initial scan in one batch, then make zero OSV calls for the next 24 hours.

**Flow:**

```
Package enters cache (miss → upstream → cache write)
  │
  ├─ Check VulnerabilityCheck table
  │   ├─ Record exists AND next_fetch_at > now()  → SKIP (cached, no API call)
  │   └─ No record OR next_fetch_at <= now()       → Query OSV.dev
  │       ├─ Vulnerabilities found → upsert Vulnerability rows
  │       │                        → set has_vulnerabilities=true
  │       └─ No vulnerabilities    → set has_vulnerabilities=false
  │       └─ Update next_fetch_at = now() + check_ttl
  │
  └─ Continue serving request (scan is async, non-blocking)

Full scan (every scan_interval):
  │
  ├─ SELECT DISTINCT ecosystem, package_name FROM cache_entries
  ├─ LEFT JOIN vulnerability_checks ON eco+pkg
  ├─ WHERE next_fetch_at IS NULL OR next_fetch_at <= now()
  │   (only packages that need refreshing)
  ├─ Group by ecosystem, batch query OSV (1000 per batch)
  └─ Update all VulnerabilityCheck rows (even zero-result)
```

**Cache invalidation:** VulnerabilityCheck is updated on every OSV query. The `check_ttl` config controls how long a check result is trusted. There is no manual cache invalidation — the "Scan Now" button runs a full scan which refreshes all due packages.

## Scope Boundaries

- No real-time blocking at request time based on live OSV queries (too slow). Blocking is based on locally stored Vulnerability data only.
- No version extraction from cache keys for most ecosystems (we know package name but not always version). Version matching is best-effort; packages matched by name show all known vulnerabilities regardless of specific cached version.
- No dependency tree analysis (only direct packages in cache are scanned).
- No webhook/email notifications (could be a follow-up).
- Conda, Helm, Docker ecosystems are not supported by OSV — they are silently skipped.
- No modification to the existing rules engine — auto-block creates standard PackageRule entries.
