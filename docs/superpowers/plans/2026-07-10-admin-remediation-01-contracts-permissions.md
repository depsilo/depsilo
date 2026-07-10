# Admin Contracts and Permissions Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Admin API contracts explicit and consistent, and make server-issued Principal capabilities the sole authority for Admin read/write access.

**Architecture:** Each affected handler maps database models into endpoint-specific DTOs, with canonical query names and one-release aliases at the HTTP boundary. Authentication resolves every JWT or API token to a fresh database user and stores one typed `middleware.Principal` in Gin context; separate read and write route groups enforce capabilities before handlers run. The React client consumes the same TypeScript contracts through typed Axios methods and a cached `usePrincipal()` hook instead of trusting the login snapshot in localStorage.

**Tech Stack:** Go 1.25.6, Gin 1.12, GORM 1.31 with SQLite, `golang-jwt/jwt/v5`, zap, React 19, TypeScript 5.9, Axios 1.14, TanStack Query 5.

## Global Constraints

- Preserve all pre-existing worktree changes. Before editing any listed file, run `git diff` for that exact file and reread the whole affected function; do not run `git reset`, `git checkout`, or a bulk formatter over unrelated files.
- `internal/api/router.go` and `web/src/lib/api.ts` are already modified in the starting worktree. Merge with those edits and stage only this plan's hunks with `git add -p`; never replace either file from `HEAD`.
- Go handlers use explicit request/response structs; GORM models are persistence types and must not be returned as the long-term HTTP contract.
- Create `web/src/lib/adminApi.types.ts` and make this plan own Principal, Security, Logs, Audit, Projects, User, and API-token types. The Settings and Upstream plans append their own exact types to the same file; do not pre-implement or overwrite those contracts here.
- Keep the existing `{code,message}` JSON error shape and existing `/api/v1` paths; do not add `/v2` endpoints.
- Canonical query parameters are `package` for Security and Audit. Accept Security `q` and Audit `search` for one release only, log a deprecation warning, and return canonical response fields only.
- Every Admin `GET`, including CSV and SBOM exports, is a read capability. `POST /api/v1/admin/rules/test` is the only non-GET read capability.
- Every other Admin `POST`, `PUT`, `PATCH`, and `DELETE` is a write capability, including health checks, scans, cleanup, warmup, sync, webhook tests, token generation, and license mutations.
- JWT requests reload the current user on every request. Disabled users receive `401`; role changes take effect without waiting for the JWT to expire.
- API-token writes require both an enabled current owner with role `admin` and token permissions `readwrite`; API tokens cannot call `/api/v1/auth/refresh`.
- Never allow deletion, disabling, or demotion of the final enabled admin. Never allow a user to delete, disable, or demote themselves; self password changes remain allowed.
- Readonly responses must not disclose credentials. Webhook URLs are redacted server-side; token hashes, raw project tokens, password hashes, license keys, and storage credentials remain absent or masked.
- Do not add frontend runtime dependencies. Browser contract coverage and broad action-by-action UI migration belong to the later Playwright/UI remediation plan; this plan establishes typed APIs and the shared capability hook.
- Required backend gates: `go test -race ./internal/middleware ./internal/api/...` and `go test ./...`.
- Required frontend gates: `cd web && npm run type-check && npm run build`.

---

## File Structure

- `internal/api/admin/security_contract.go` — create: Security request/response DTOs and model-to-DTO mapping only.
- `internal/api/admin/security_contract_test.go` — create: canonical field, compatibility alias, and CVSS validation contract tests.
- `internal/api/admin/security.go` — modify: consume the Security DTOs, canonical `package`, deprecated `q`, and `0..10` policy validation.
- `internal/api/admin/logs.go` — modify: explicit log DTO/filter, shared list/export filter, and CSV encoder.
- `internal/api/admin/logs_test.go` — create: list/export parity and CSV contract tests.
- `internal/api/admin/audit_contract.go` — create: Audit response DTOs and mapping.
- `internal/api/admin/audit_contract_test.go` — create: `package`/`search` compatibility and canonical response tests.
- `internal/api/admin/audit.go` — modify: canonical parameter precedence, deprecated alias logging, and DTO mapping.
- `internal/api/admin/projects_contract.go` — create: Project request/response DTOs, package mapping, and proxy URL construction.
- `internal/api/admin/projects_contract_test.go` — create: list envelope, package field, and `/p/{slug}` proxy tests.
- `internal/api/admin/projects.go` — modify: stop embedding GORM models in responses and return canonical Project DTOs.
- `internal/middleware/principal.go` — create: Principal type, context accessor, and read/write capability middleware.
- `internal/middleware/auth.go` — modify: fresh-user JWT resolution, API-token permission resolution, generic `Authenticate`, and `JWTOnly`.
- `internal/middleware/auth_test.go` — create: JWT/API-token/disabled/stale-role permission matrix.
- `internal/api/auth.go` — modify: `GET /auth/me` handler and refresh from the resolved Principal.
- `internal/api/auth_test.go` — create: exact Principal response and JWT-only refresh regression tests.
- `internal/api/router.go` — modify carefully: add `/auth/me`, use `JWTOnly` for refresh, and register Admin handlers on explicit read/write groups.
- `internal/api/router_permissions_test.go` — create: AST-backed declaration test that prevents routes bypassing capability groups.
- `internal/api/admin/credentials.go` — create: readonly credential redaction helpers.
- `internal/api/admin/credentials_test.go` — create: webhook and URL-userinfo redaction tests.
- `internal/api/admin/webhook.go` — modify: explicit list response with server-side readonly URL masking.
- `internal/api/admin/upstream.go` — modify narrowly: redact URL userinfo in readonly list responses until the Registry plan replaces this mapper.
- `internal/api/admin/user.go` — modify: serialize user mutations and enforce transactional self-lockout/final-admin invariants.
- `internal/api/admin/user_test.go` — create: invariant, password, and concurrent mutation tests.
- `web/src/lib/adminApi.types.ts` — create: canonical Principal, Security, Logs, Audit, Projects, User, and API-token DTOs; later Settings/Upstream plans append their types.
- `web/src/lib/adminApi.types.type-test.ts` — create: compile-time assertions that affected Axios response data is exact and not `any`.
- `web/src/lib/api.ts` — modify carefully: typed auth and affected Admin methods.
- `web/src/hooks/usePrincipal.ts` — create: shared Principal query and pure `canWrite()` helper.
- `web/src/admin/AdminApp.tsx` — modify: resolve the authenticated Principal before rendering protected routes.
- `web/src/admin/components/MainLayout.tsx` — modify: render server Principal and stop parsing `localStorage.user`.
- `web/src/admin/pages/Login.tsx` — modify: persist only the bearer token; invalidate/fetch Principal after login.
- `web/src/admin/pages/Users.tsx` — modify: use Principal identity for self-action hiding and capability-aware mutation controls.
- `web/src/admin/pages/Security.tsx` — modify: canonical Security fields and `package` query.
- `web/src/admin/pages/AccessLogs.tsx` — modify: typed filter and response items.
- `web/src/admin/pages/AuditLogs.tsx` — modify: canonical `package` query and `cache_result` response.
- `web/src/admin/pages/Projects.tsx` — modify: canonical package fields, typed DTOs, and `/p/{slug}` fallback.

---

### Task 1: Audit Query Compatibility and Explicit Responses

**Files:**
- Create: `internal/api/admin/audit_contract.go`
- Create: `internal/api/admin/audit_contract_test.go`
- Modify: `internal/api/admin/audit.go:25-76`

**Interfaces:**
- Consumes: `audit.Query`, `audit.QueryResult`, and `db.AuditLog` internally.
- Produces: `auditLogResponse` and `auditListResponse`; no GORM model crosses the handler boundary.
- Produces: canonical `package`; deprecated `search` is used only when `package` is absent. List and Export both call the same `parseQuery`.

- [ ] **Step 1: Write failing canonical/alias tests**

Create `internal/api/admin/audit_contract_test.go`:

```go
package admin

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
)

func TestAuditPackageQueryAndDeprecatedSearchAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "audit.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	items := []db.AuditLog{
		{Ecosystem: "pypi", PackageName: "requests", Version: "2.32.0", Action: "download", CacheResult: "hit", ClientIP: "10.0.0.1", LatencyMs: 8, BytesSent: 1200, StatusCode: 200, CreatedAt: now},
		{Ecosystem: "pypi", PackageName: "flask", Version: "3.1.0", Action: "download", CacheResult: "miss", ClientIP: "10.0.0.2", LatencyMs: 30, BytesSent: 900, StatusCode: 200, CreatedAt: now.Add(-time.Minute)},
	}
	if err := database.Create(&items).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := NewAuditHandler(database)
	r := gin.New()
	r.GET("/audit-logs", h.List)
	r.GET("/audit-logs/export", h.Export)

	for _, path := range []string{
		"/audit-logs?package=requests",
		"/audit-logs?search=requests",
		"/audit-logs?package=requests&search=flask",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, body = %s", path, rec.Code, rec.Body.String())
		}
		var body struct {
			Items []map[string]any `json:"items"`
			Total int64            `json:"total"`
			Page  int              `json:"page"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if body.Total != 1 || len(body.Items) != 1 || body.Items[0]["package_name"] != "requests" {
			t.Fatalf("GET %s body = %#v", path, body)
		}
		if _, exists := body.Items[0]["result"]; exists {
			t.Fatalf("GET %s returned noncanonical result alias", path)
		}
		if body.Items[0]["cache_result"] != "hit" {
			t.Fatalf("GET %s cache_result = %v", path, body.Items[0]["cache_result"])
		}
	}

	exportRec := httptest.NewRecorder()
	r.ServeHTTP(exportRec, httptest.NewRequest(http.MethodGet, "/audit-logs/export?search=requests", nil))
	records, err := csv.NewReader(strings.NewReader(exportRec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if exportRec.Code != http.StatusOK || len(records) != 2 || records[1][2] != "requests" {
		t.Fatalf("export status = %d, records = %#v", exportRec.Code, records)
	}
}
```

- [ ] **Step 2: Run the test and verify `search` is ignored**

Run: `go test ./internal/api/admin -run TestAuditPackageQueryAndDeprecatedSearchAlias -count=1`

Expected: FAIL because `/audit-logs?search=requests` returns both rows.

- [ ] **Step 3: Add explicit Audit DTOs**

Create `internal/api/admin/audit_contract.go`:

```go
package admin

import (
	"time"

	"depsilo/internal/audit"
	"depsilo/internal/db"
)

type auditLogResponse struct {
	ID          uint      `json:"id"`
	Ecosystem   string    `json:"ecosystem"`
	PackageName string    `json:"package_name"`
	Version     string    `json:"version"`
	Action      string    `json:"action"`
	CacheResult string    `json:"cache_result"`
	ClientIP    string    `json:"client_ip"`
	UserAgent   string    `json:"user_agent"`
	UpstreamURL string    `json:"upstream_url"`
	LatencyMs   int64     `json:"latency_ms"`
	BytesSent   int64     `json:"bytes_sent"`
	StatusCode  int       `json:"status_code"`
	CreatedAt   time.Time `json:"created_at"`
}

type auditListResponse struct {
	Items []auditLogResponse `json:"items"`
	Total int64              `json:"total"`
	Page  int                `json:"page"`
}

func toAuditLogResponse(item db.AuditLog) auditLogResponse {
	return auditLogResponse{
		ID: item.ID, Ecosystem: item.Ecosystem, PackageName: item.PackageName,
		Version: item.Version, Action: item.Action, CacheResult: item.CacheResult,
		ClientIP: item.ClientIP, UserAgent: item.UserAgent, UpstreamURL: item.UpstreamURL,
		LatencyMs: item.LatencyMs, BytesSent: item.BytesSent, StatusCode: item.StatusCode,
		CreatedAt: item.CreatedAt,
	}
}

func toAuditListResponse(result *audit.QueryResult) auditListResponse {
	items := make([]auditLogResponse, len(result.Items))
	for i, item := range result.Items {
		items[i] = toAuditLogResponse(item)
	}
	return auditListResponse{Items: items, Total: result.Total, Page: result.Page}
}
```

- [ ] **Step 4: Resolve the canonical query once and map the list**

In `internal/api/admin/audit.go`, import `go.uber.org/zap`, change `List` to `c.JSON(http.StatusOK, toAuditListResponse(result))`, and replace the `PackageName` initialization in `parseQuery` with:

```go
	packageName := c.Query("package")
	if packageName == "" {
		packageName = c.Query("search")
		if packageName != "" {
			zap.L().Warn("deprecated admin query parameter", zap.String("endpoint", "audit-logs"), zap.String("parameter", "search"), zap.String("replacement", "package"))
		}
	}

	q := audit.Query{
		Ecosystem:   c.Query("ecosystem"),
		PackageName: packageName,
		ClientIP:    c.Query("ip"),
		CacheResult: c.Query("result"),
		Page:        page,
		PageSize:    pageSize,
	}
```

Do not duplicate alias logic in `Export`; it must continue to call `h.parseQuery(c)`.

- [ ] **Step 5: Run focused tests and the audit package**

Run: `gofmt -w internal/api/admin/audit.go internal/api/admin/audit_contract.go internal/api/admin/audit_contract_test.go && go test ./internal/api/admin -run TestAuditPackageQueryAndDeprecatedSearchAlias -count=1 && go test ./internal/audit ./internal/api/admin -count=1`

Expected: PASS.

- [ ] **Step 6: Commit the Audit contract**

```bash
git add internal/api/admin/audit.go internal/api/admin/audit_contract.go internal/api/admin/audit_contract_test.go
git commit -m "fix(admin): align audit query contract"
```

---

### Task 2: Project DTOs and Proxy Paths

**Files:**
- Create: `internal/api/admin/projects_contract.go`
- Create: `internal/api/admin/projects_contract_test.go`
- Modify: `internal/api/admin/projects.go:48-323`

**Interfaces:**
- Produces: `projectListResponse{items,total}`, `projectSummaryResponse`, `projectDetailResponse`, `createProjectResponse`, `projectPackageResponse`, and `projectPackagesResponse`.
- Produces: Project package fields exactly `ecosystem`, `package_name`, `version`, `first_seen_at`, `last_seen_at`, and `download_count`.
- Produces: `projectProxyURL(*http.Request, slug string) string`, always using `/p/{slug}` and honoring `X-Forwarded-Proto`.
- Preserves: raw project token is returned only by create/regenerate write endpoints; list/detail never expose `token_hash`.

- [ ] **Step 1: Write failing Project envelope and field tests**

Create `internal/api/admin/projects_contract_test.go`:

```go
package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
)

func newProjectsContractRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "projects.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.Project{}, &db.ProjectPackage{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := NewProjectsHandler(database)
	r := gin.New()
	r.GET("/projects", h.List)
	r.POST("/projects", h.Create)
	r.GET("/projects/:id/packages", h.ListPackages)
	return r, database
}

func TestProjectsListAndPackageContracts(t *testing.T) {
	r, database := newProjectsContractRouter(t)
	project := db.Project{Name: "AI Platform", Slug: "ai-platform", Description: "training", TokenHash: "must-not-leak", CreatedBy: "admin"}
	if err := database.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	first := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	last := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	pkg := db.ProjectPackage{ProjectID: project.ID, Ecosystem: "pypi", PackageName: "requests", Version: "2.32.0", FirstSeenAt: first, LastSeenAt: last, DownloadCount: 47}
	if err := database.Create(&pkg).Error; err != nil {
		t.Fatalf("create package: %v", err)
	}

	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/projects", nil))
	var listBody map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listRec.Code != http.StatusOK || listBody["total"] != float64(1) {
		t.Fatalf("list status = %d, body = %#v", listRec.Code, listBody)
	}
	listItems, ok := listBody["items"].([]any)
	if !ok || len(listItems) != 1 {
		t.Fatalf("list items = %#v", listBody["items"])
	}
	summary := listItems[0].(map[string]any)
	if summary["package_count"] != float64(1) || summary["last_activity_at"] != last.Format(time.RFC3339) {
		t.Fatalf("summary = %#v", summary)
	}
	if _, exists := summary["token_hash"]; exists {
		t.Fatal("project list leaked token_hash")
	}

	packagesRec := httptest.NewRecorder()
	r.ServeHTTP(packagesRec, httptest.NewRequest(http.MethodGet, "/projects/"+jsonNumber(project.ID)+"/packages", nil))
	var packagesBody struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
		Page  int              `json:"page"`
	}
	if err := json.Unmarshal(packagesRec.Body.Bytes(), &packagesBody); err != nil {
		t.Fatalf("decode packages: %v", err)
	}
	if packagesRec.Code != http.StatusOK || len(packagesBody.Items) != 1 {
		t.Fatalf("packages status = %d, body = %#v", packagesRec.Code, packagesBody)
	}
	item := packagesBody.Items[0]
	for _, key := range []string{"ecosystem", "package_name", "version", "first_seen_at", "last_seen_at", "download_count"} {
		if _, exists := item[key]; !exists {
			t.Fatalf("package item missing %s: %#v", key, item)
		}
	}
	for _, key := range []string{"id", "project_id", "name", "first_seen", "last_seen", "downloads", "created_at", "updated_at"} {
		if _, exists := item[key]; exists {
			t.Fatalf("package item leaked noncontract field %s: %#v", key, item)
		}
	}
}

func TestCreateProjectProxyURLUsesProjectPrefix(t *testing.T) {
	r, _ := newProjectsContractRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewBufferString(`{"name":"AI Platform","description":"training"}`))
	req.Host = "depsilo.example.test"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["proxy_url"] != "https://depsilo.example.test/p/ai-platform" {
		t.Fatalf("proxy_url = %v", body["proxy_url"])
	}
}
```

Add this helper and import `strconv` in the test:

```go
func jsonNumber(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}
```

- [ ] **Step 2: Run tests and verify the current bare-array/model leakage**

Run: `go test ./internal/api/admin -run 'TestProjects|TestCreateProjectProxy' -count=1`

Expected: FAIL because list returns a bare array, summary lacks `last_activity_at`, and package rows include persistence-only fields.

- [ ] **Step 3: Add Project contract types and mappers**

Create `internal/api/admin/projects_contract.go`:

```go
package admin

import (
	"net/http"
	"time"

	"depsilo/internal/db"
)

type createProjectRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

type updateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type projectSummaryResponse struct {
	ID             uint       `json:"id"`
	Name           string     `json:"name"`
	Slug           string     `json:"slug"`
	Description    string     `json:"description"`
	PackageCount   int64      `json:"package_count"`
	LastActivityAt *time.Time `json:"last_activity_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type projectListResponse struct {
	Items []projectSummaryResponse `json:"items"`
	Total int                       `json:"total"`
}

type createProjectResponse struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Token       string    `json:"token"`
	ProxyURL    string    `json:"proxy_url"`
	CreatedAt   time.Time `json:"created_at"`
}

type projectDetailResponse struct {
	ID                 uint             `json:"id"`
	Name               string           `json:"name"`
	Slug               string           `json:"slug"`
	Description        string           `json:"description"`
	ProxyURL           string           `json:"proxy_url"`
	PackageCount       int64            `json:"package_count"`
	EcosystemBreakdown map[string]int64 `json:"ecosystem_breakdown"`
	LastActivityAt     *time.Time       `json:"last_activity_at"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
}

type projectPackageResponse struct {
	Ecosystem     string    `json:"ecosystem"`
	PackageName   string    `json:"package_name"`
	Version       string    `json:"version"`
	FirstSeenAt   time.Time `json:"first_seen_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
	DownloadCount int       `json:"download_count"`
}

type projectPackagesResponse struct {
	Items []projectPackageResponse `json:"items"`
	Total int64                    `json:"total"`
	Page  int                      `json:"page"`
}

type regenerateProjectTokenResponse struct {
	Token    string `json:"token"`
	ProxyURL string `json:"proxy_url"`
}

func projectProxyURL(req *http.Request, slug string) string {
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	if forwarded := req.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	return scheme + "://" + req.Host + "/p/" + slug
}

func toProjectPackageResponses(items []db.ProjectPackage) []projectPackageResponse {
	responses := make([]projectPackageResponse, len(items))
	for i, item := range items {
		responses[i] = projectPackageResponse{
			Ecosystem: item.Ecosystem, PackageName: item.PackageName, Version: item.Version,
			FirstSeenAt: item.FirstSeenAt, LastSeenAt: item.LastSeenAt, DownloadCount: item.DownloadCount,
		}
	}
	return responses
}
```

- [ ] **Step 4: Return DTOs from every Project handler**

In `List`, check each DB operation, query `MAX(last_seen_at)` into `*time.Time`, and return this exact envelope:

```go
	items := make([]projectSummaryResponse, len(projects))
	for i, project := range projects {
		var count int64
		if err := h.db.Model(&db.ProjectPackage{}).Where("project_id = ?", project.ID).Count(&count).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
			return
		}
		var lastActivity *time.Time
		var latest db.ProjectPackage
		if err := h.db.Where("project_id = ?", project.ID).Order("last_seen_at DESC").First(&latest).Error; err == nil {
			lastActivity = &latest.LastSeenAt
		} else if err != gorm.ErrRecordNotFound {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
			return
		}
		items[i] = projectSummaryResponse{
			ID: project.ID, Name: project.Name, Slug: project.Slug, Description: project.Description,
			PackageCount: count, LastActivityAt: lastActivity, CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
		}
	}
	c.JSON(http.StatusOK, projectListResponse{Items: items, Total: len(items)})
```

Use `createProjectRequest` and `updateProjectRequest` for binding. In Create, remove the inline scheme construction and return:

```go
	c.JSON(http.StatusCreated, createProjectResponse{
		ID: project.ID, Name: project.Name, Slug: project.Slug, Description: project.Description,
		Token: token, ProxyURL: projectProxyURL(c.Request, project.Slug), CreatedAt: project.CreatedAt,
	})
```

In Detail, return `projectDetailResponse` and set `ProxyURL: projectProxyURL(c.Request, project.Slug)`. In ListPackages, return:

```go
	c.JSON(http.StatusOK, projectPackagesResponse{Items: toProjectPackageResponses(packages), Total: total, Page: page})
```

In RegenerateToken, include the canonical proxy path without exposing the stored hash:

```go
	c.JSON(http.StatusOK, regenerateProjectTokenResponse{Token: token, ProxyURL: projectProxyURL(c.Request, project.Slug)})
```

For Update, reload the saved record and map only `id`, `name`, `slug`, `description`, `created_at`, and `updated_at`; do not return `db.Project` directly.

- [ ] **Step 5: Run Project contract tests**

Run: `gofmt -w internal/api/admin/projects.go internal/api/admin/projects_contract.go internal/api/admin/projects_contract_test.go && go test ./internal/api/admin -run 'TestProjects|TestCreateProjectProxy' -count=1`

Expected: PASS. Then run `go test ./internal/api/admin -count=1`; expected PASS.

- [ ] **Step 6: Commit the Project contract**

```bash
git add internal/api/admin/projects.go internal/api/admin/projects_contract.go internal/api/admin/projects_contract_test.go
git commit -m "fix(admin): make project responses explicit"
```

---

### Task 3: Security HTTP Contract

**Files:**
- Create: `internal/api/admin/security_contract.go`
- Create: `internal/api/admin/security_contract_test.go`
- Modify: `internal/api/admin/security.go:26-255`

**Interfaces:**
- Consumes: `db.Vulnerability`, `db.VulnerabilityCheck`, and `db.SecurityPolicy` as persistence records only.
- Produces: `securityDashboardResponse`, `vulnerabilityResponse`, `vulnerabilityCheckResponse`, `securityPolicyResponse`, `updateSecurityPolicyRequest`, and `securityPage[T]` with canonical JSON names.
- Produces: `GET /security/vulnerabilities?page=1&package=x`; deprecated `q=x` behaves identically only when `package` is absent.
- Produces: `PUT /security/policies/:ecosystem` accepts `{auto_block_enabled,min_cvss_score}` and returns `422 {"code":"INVALID_POLICY","message":"min_cvss_score must be between 0 and 10"}` outside `0..10`.

- [ ] **Step 1: Write the failing contract tests**

Create `internal/api/admin/security_contract_test.go`:

```go
package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
)

func newSecurityContractRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "security.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.Vulnerability{}, &db.SecurityPolicy{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := NewSecurityHandler(database, nil, nil)
	r := gin.New()
	r.GET("/security/vulnerabilities", h.ListVulnerabilities)
	r.PUT("/security/policies/:ecosystem", h.UpdatePolicy)
	return r, database
}

func TestSecurityVulnerabilityPackageQueryAndDeprecatedAlias(t *testing.T) {
	r, database := newSecurityContractRouter(t)
	for _, vuln := range []db.Vulnerability{
		{OSVID: "OSV-REQUESTS", Ecosystem: "pypi", PackageName: "requests", Severity: "high", CVSSScore: 8.1},
		{OSVID: "OSV-FLASK", Ecosystem: "pypi", PackageName: "flask", Severity: "medium", CVSSScore: 5.0},
	} {
		if err := database.Create(&vuln).Error; err != nil {
			t.Fatalf("seed vulnerability: %v", err)
		}
	}

	for _, path := range []string{
		"/security/vulnerabilities?package=requests",
		"/security/vulnerabilities?q=requests",
		"/security/vulnerabilities?package=requests&q=flask",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, body = %s", path, rec.Code, rec.Body.String())
		}
		var body struct {
			Items []map[string]any `json:"items"`
			Total int64            `json:"total"`
			Page  int              `json:"page"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if body.Total != 1 || len(body.Items) != 1 || body.Items[0]["package_name"] != "requests" {
			t.Fatalf("GET %s body = %#v", path, body)
		}
		if _, exists := body.Items[0]["package"]; exists {
			t.Fatalf("GET %s leaked noncanonical package field", path)
		}
	}
}

func TestSecurityPolicyCanonicalFieldsAndCVSSRange(t *testing.T) {
	r, database := newSecurityContractRouter(t)
	for _, score := range []float64{-0.1, 10.1} {
		payload, err := json.Marshal(map[string]any{"auto_block_enabled": true, "min_cvss_score": score})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/security/policies/pypi", bytes.NewReader(payload)))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("score %v status = %d, body = %s", score, rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if body["code"] != "INVALID_POLICY" {
			t.Fatalf("score %v code = %v", score, body["code"])
		}
	}

	payload := []byte(`{"auto_block_enabled":true,"min_cvss_score":7.5}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/security/policies/pypi", bytes.NewReader(payload)))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid policy status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	if body["auto_block_enabled"] != true || body["min_cvss_score"] != 7.5 {
		t.Fatalf("policy body = %#v", body)
	}
	if _, exists := body["auto_block"]; exists {
		t.Fatal("response includes deprecated auto_block")
	}
	if _, exists := body["cvss_threshold"]; exists {
		t.Fatal("response includes deprecated cvss_threshold")
	}
	var count int64
	if err := database.Model(&db.SecurityPolicy{}).Count(&count).Error; err != nil {
		t.Fatalf("count policies: %v", err)
	}
	if count != 1 {
		t.Fatalf("policy count = %d, want 1", count)
	}
}
```

- [ ] **Step 2: Run the tests and verify the existing drift**

Run: `go test ./internal/api/admin -run 'TestSecurity(Vulnerability|Policy)' -count=1`

Expected: FAIL. The `q` request returns both seeded rows, and `min_cvss_score=10.1` returns `200` instead of `422`.

- [ ] **Step 3: Add explicit Security DTOs**

Create `internal/api/admin/security_contract.go` with these declarations and complete mappers:

```go
package admin

import (
	"time"

	"depsilo/internal/db"
)

type securityDashboardResponse struct {
	TotalVulnerabilities int64            `json:"total_vulnerabilities"`
	AffectedPackages     int64            `json:"affected_packages"`
	BySeverity           map[string]int64 `json:"by_severity"`
	AutoBlockedCount     int64            `json:"auto_blocked_count"`
	LastScanAt          *string          `json:"last_scan_at"`
	ScanInProgress      bool             `json:"scan_in_progress"`
}

type vulnerabilityResponse struct {
	ID             uint      `json:"id"`
	OSVID          string    `json:"osv_id"`
	Ecosystem      string    `json:"ecosystem"`
	PackageName    string    `json:"package_name"`
	AffectedRanges string    `json:"affected_ranges"`
	Severity       string    `json:"severity"`
	CVSSScore      float32   `json:"cvss_score"`
	Summary        string    `json:"summary"`
	Details        string    `json:"details"`
	Aliases        string    `json:"aliases"`
	References     string    `json:"references"`
	PublishedAt    time.Time `json:"published_at"`
	ModifiedAt     time.Time `json:"modified_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type vulnerabilityCheckResponse struct {
	ID                 uint      `json:"id"`
	Ecosystem          string    `json:"ecosystem"`
	PackageName        string    `json:"package_name"`
	HasVulnerabilities bool      `json:"has_vulnerabilities"`
	VulnerabilityCount int       `json:"vulnerability_count"`
	LastFetchedAt      time.Time `json:"last_fetched_at"`
	NextFetchAt        time.Time `json:"next_fetch_at"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type securityPolicyResponse struct {
	ID               uint      `json:"id"`
	Ecosystem        string    `json:"ecosystem"`
	AutoBlockEnabled bool      `json:"auto_block_enabled"`
	MinCVSSScore     float32   `json:"min_cvss_score"`
	CreatedBy        string    `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type updateSecurityPolicyRequest struct {
	AutoBlockEnabled bool    `json:"auto_block_enabled"`
	MinCVSSScore     float32 `json:"min_cvss_score"`
}

type securityPage[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
}

func toVulnerabilityResponse(v db.Vulnerability) vulnerabilityResponse {
	return vulnerabilityResponse{
		ID: v.ID, OSVID: v.OSVID, Ecosystem: v.Ecosystem, PackageName: v.PackageName,
		AffectedRanges: v.AffectedRanges, Severity: v.Severity, CVSSScore: v.CVSSScore,
		Summary: v.Summary, Details: v.Details, Aliases: v.Aliases, References: v.References,
		PublishedAt: v.PublishedAt, ModifiedAt: v.ModifiedAt, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func toVulnerabilityResponses(items []db.Vulnerability) []vulnerabilityResponse {
	responses := make([]vulnerabilityResponse, len(items))
	for i, item := range items {
		responses[i] = toVulnerabilityResponse(item)
	}
	return responses
}

func toVulnerabilityCheckResponses(items []db.VulnerabilityCheck) []vulnerabilityCheckResponse {
	responses := make([]vulnerabilityCheckResponse, len(items))
	for i, item := range items {
		responses[i] = vulnerabilityCheckResponse{
			ID: item.ID, Ecosystem: item.Ecosystem, PackageName: item.PackageName,
			HasVulnerabilities: item.HasVulnerabilities, VulnerabilityCount: item.VulnerabilityCount,
			LastFetchedAt: item.LastFetchedAt, NextFetchAt: item.NextFetchAt,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		}
	}
	return responses
}

func toSecurityPolicyResponse(policy db.SecurityPolicy) securityPolicyResponse {
	return securityPolicyResponse{
		ID: policy.ID, Ecosystem: policy.Ecosystem, AutoBlockEnabled: policy.AutoBlockEnabled,
		MinCVSSScore: policy.MinCVSSScore, CreatedBy: policy.CreatedBy,
		CreatedAt: policy.CreatedAt, UpdatedAt: policy.UpdatedAt,
	}
}

func toSecurityPolicyResponses(items []db.SecurityPolicy) []securityPolicyResponse {
	responses := make([]securityPolicyResponse, len(items))
	for i, item := range items {
		responses[i] = toSecurityPolicyResponse(item)
	}
	return responses
}
```

- [ ] **Step 4: Make handlers consume canonical fields**

In `internal/api/admin/security.go`, add `go.uber.org/zap`, replace the dashboard `gin.H` with `securityDashboardResponse`, map every list through the DTO functions, and replace `UpdatePolicy` with:

```go
func (h *SecurityHandler) UpdatePolicy(c *gin.Context) {
	ecosystem := c.Param("ecosystem")
	var req updateSecurityPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid request body"})
		return
	}
	if req.MinCVSSScore < 0 || req.MinCVSSScore > 10 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "INVALID_POLICY", "message": "min_cvss_score must be between 0 and 10"})
		return
	}
	policy := db.SecurityPolicy{
		Ecosystem: ecosystem, AutoBlockEnabled: req.AutoBlockEnabled,
		MinCVSSScore: req.MinCVSSScore, CreatedBy: "admin",
	}
	result := h.db.Where("ecosystem = ?", ecosystem).Assign(policy).FirstOrCreate(&policy)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": result.Error.Error()})
		return
	}
	c.JSON(http.StatusOK, toSecurityPolicyResponse(policy))
}
```

Use canonical-first query resolution in `ListVulnerabilities`:

```go
	pkg := c.Query("package")
	if pkg == "" {
		pkg = c.Query("q")
		if pkg != "" {
			zap.L().Warn("deprecated admin query parameter", zap.String("endpoint", "security/vulnerabilities"), zap.String("parameter", "q"), zap.String("replacement", "package"))
		}
	}
	if pkg != "" {
		query = query.Where("package_name LIKE ?", "%"+pkg+"%")
	}
```

Return the lists with exact envelopes:

```go
	c.JSON(http.StatusOK, securityPage[vulnerabilityResponse]{Items: toVulnerabilityResponses(vulns), Total: total, Page: page})
```

```go
	c.JSON(http.StatusOK, securityPage[vulnerabilityCheckResponse]{Items: toVulnerabilityCheckResponses(checks), Total: total, Page: page})
```

```go
	c.JSON(http.StatusOK, toSecurityPolicyResponses(policies))
```

- [ ] **Step 5: Run focused and package tests**

Run: `gofmt -w internal/api/admin/security.go internal/api/admin/security_contract.go internal/api/admin/security_contract_test.go && go test ./internal/api/admin -run 'TestSecurity(Vulnerability|Policy)' -count=1`

Expected: PASS. Then run `go test ./internal/api/admin -count=1`; expected PASS.

- [ ] **Step 6: Commit the Security contract**

```bash
git add internal/api/admin/security.go internal/api/admin/security_contract.go internal/api/admin/security_contract_test.go
git commit -m "fix(admin): align security API contract"
```

---

### Task 4: Access Log List and Export Contract

**Files:**
- Modify: `internal/api/admin/logs.go:1-61`
- Create: `internal/api/admin/logs_test.go`
- Modify later in Task 6: `internal/api/router.go` to mount `GET /admin/logs/export`

**Interfaces:**
- Produces: `accessLogResponse` with the 12 canonical fields currently consumed by the UI.
- Produces: `accessLogListResponse{items,total,page,page_size}`.
- Produces: `parseAccessLogFilter(*gin.Context) (accessLogFilter, error)` and `applyAccessLogFilter(*gorm.DB, accessLogFilter) *gorm.DB`; List and Export must call the same functions.
- Produces: `encodeAccessLogsCSV([]accessLogResponse) ([]byte,error)` and `GET /admin/logs/export` with the same filters as List, capped at 10,000 rows.

- [ ] **Step 1: Write list/export parity tests**

Create `internal/api/admin/logs_test.go`:

```go
package admin

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
)

func TestAccessLogListAndExportShareFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logs.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.AccessLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Date(2026, 7, 10, 8, 30, 0, 0, time.UTC)
	rows := []db.AccessLog{
		{AdapterType: "pypi", Method: "GET", PackageName: "requests", CacheKey: "pypi/requests.whl", Hit: true, Upstream: "primary", LatencyMs: 12, StatusCode: 200, ClientIP: "10.0.0.1", BytesSent: 1200, CreatedAt: now},
		{AdapterType: "pypi", Method: "GET", PackageName: "requests", CacheKey: "pypi/requests.json", Hit: false, Upstream: "primary", LatencyMs: 40, StatusCode: 200, ClientIP: "10.0.0.2", BytesSent: 300, CreatedAt: now.Add(-time.Minute)},
		{AdapterType: "npm", Method: "GET", PackageName: "react", CacheKey: "npm/react", Hit: true, Upstream: "npmjs", LatencyMs: 10, StatusCode: 200, ClientIP: "10.0.0.3", BytesSent: 800, CreatedAt: now.Add(-2 * time.Minute)},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := NewAccessLogHandler(database)
	r := gin.New()
	r.GET("/logs", h.List)
	r.GET("/logs/export", h.Export)
	filter := "search=requests&adapter_type=pypi&hit=true"

	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/logs?"+filter, nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var listBody struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
		Page  int              `json:"page"`
		PageSize int           `json:"page_size"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listBody.Total != 1 || len(listBody.Items) != 1 || listBody.Items[0]["package_name"] != "requests" || listBody.Items[0]["hit"] != true {
		t.Fatalf("list body = %#v", listBody)
	}

	exportRec := httptest.NewRecorder()
	r.ServeHTTP(exportRec, httptest.NewRequest(http.MethodGet, "/logs/export?"+filter, nil))
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", exportRec.Code, exportRec.Body.String())
	}
	if !strings.HasPrefix(exportRec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("content type = %q", exportRec.Header().Get("Content-Type"))
	}
	records, err := csv.NewReader(strings.NewReader(exportRec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("csv rows = %d, want header + one row: %#v", len(records), records)
	}
	if records[1][3] != "requests" || records[1][4] != "true" {
		t.Fatalf("csv data row = %#v", records[1])
	}
	if strings.Contains(exportRec.Body.String(), "react") || strings.Contains(exportRec.Body.String(), "10.0.0.2") {
		t.Fatalf("export ignored list filters: %s", exportRec.Body.String())
	}
}

func TestAccessLogFilterRejectsInvalidHitValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "invalid.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.AccessLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := NewAccessLogHandler(database)
	r := gin.New()
	r.GET("/logs", h.List)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/logs?hit=sometimes", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run the tests and verify Export is absent**

Run: `go test ./internal/api/admin -run TestAccessLog -count=1`

Expected: FAIL to compile because `(*AccessLogHandler).Export` is undefined.

- [ ] **Step 3: Replace the handler with one shared filter and explicit DTOs**

Replace `internal/api/admin/logs.go` completely with:

```go
package admin

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

const maxAccessLogExportRows = 10000

type AccessLogHandler struct {
	db *gorm.DB
}

func NewAccessLogHandler(database *gorm.DB) *AccessLogHandler {
	return &AccessLogHandler{db: database}
}

type accessLogFilter struct {
	Search      string
	AdapterType string
	Hit         *bool
	Page        int
	PageSize    int
}

type accessLogResponse struct {
	ID          uint      `json:"id"`
	AdapterType string    `json:"adapter_type"`
	Method      string    `json:"method"`
	CacheKey    string    `json:"cache_key"`
	PackageName string    `json:"package_name"`
	Hit         bool      `json:"hit"`
	Upstream    string    `json:"upstream"`
	LatencyMs   int64     `json:"latency_ms"`
	StatusCode  int       `json:"status_code"`
	ClientIP    string    `json:"client_ip"`
	BytesSent   int64     `json:"bytes_sent"`
	CreatedAt   time.Time `json:"created_at"`
}

type accessLogListResponse struct {
	Items    []accessLogResponse `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

func parseAccessLogFilter(c *gin.Context) (accessLogFilter, error) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if err != nil || pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	adapterType := c.Query("adapter_type")
	if adapterType == "" {
		adapterType = c.Query("type")
	}
	filter := accessLogFilter{Search: c.Query("search"), AdapterType: adapterType, Page: page, PageSize: pageSize}
	if raw := c.Query("hit"); raw != "" {
		hit, err := strconv.ParseBool(raw)
		if err != nil {
			return accessLogFilter{}, fmt.Errorf("hit must be true or false")
		}
		filter.Hit = &hit
	}
	return filter, nil
}

func applyAccessLogFilter(database *gorm.DB, filter accessLogFilter) *gorm.DB {
	query := database.Model(&db.AccessLog{})
	if filter.Search != "" {
		query = query.Where("package_name LIKE ?", "%"+filter.Search+"%")
	}
	if filter.AdapterType != "" {
		query = query.Where("adapter_type = ?", filter.AdapterType)
	}
	if filter.Hit != nil {
		query = query.Where("hit = ?", *filter.Hit)
	}
	return query
}

func toAccessLogResponses(items []db.AccessLog) []accessLogResponse {
	responses := make([]accessLogResponse, len(items))
	for i, item := range items {
		responses[i] = accessLogResponse{
			ID: item.ID, AdapterType: item.AdapterType, Method: item.Method, CacheKey: item.CacheKey,
			PackageName: item.PackageName, Hit: item.Hit, Upstream: item.Upstream,
			LatencyMs: item.LatencyMs, StatusCode: item.StatusCode, ClientIP: item.ClientIP,
			BytesSent: item.BytesSent, CreatedAt: item.CreatedAt,
		}
	}
	return responses
}

func (h *AccessLogHandler) List(c *gin.Context) {
	filter, err := parseAccessLogFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	query := applyAccessLogFilter(h.db.WithContext(c.Request.Context()), filter)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	var logs []db.AccessLog
	if err := query.Order("datetime(created_at) DESC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, accessLogListResponse{Items: toAccessLogResponses(logs), Total: total, Page: filter.Page, PageSize: filter.PageSize})
}

func encodeAccessLogsCSV(items []accessLogResponse) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"Time", "Method", "Ecosystem", "Package", "Hit", "Status", "Latency(ms)", "Bytes", "Upstream", "Client IP", "Cache Key"}); err != nil {
		return nil, err
	}
	for _, item := range items {
		if err := w.Write([]string{
			item.CreatedAt.Format(time.RFC3339), item.Method, item.AdapterType, item.PackageName,
			strconv.FormatBool(item.Hit), strconv.Itoa(item.StatusCode), strconv.FormatInt(item.LatencyMs, 10),
			strconv.FormatInt(item.BytesSent, 10), item.Upstream, item.ClientIP, item.CacheKey,
		}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (h *AccessLogHandler) Export(c *gin.Context) {
	filter, err := parseAccessLogFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	var logs []db.AccessLog
	query := applyAccessLogFilter(h.db.WithContext(c.Request.Context()), filter)
	if err := query.Order("datetime(created_at) DESC").Limit(maxAccessLogExportRows).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "EXPORT_ERROR", "message": err.Error()})
		return
	}
	data, err := encodeAccessLogsCSV(toAccessLogResponses(logs))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "EXPORT_ERROR", "message": err.Error()})
		return
	}
	filename := fmt.Sprintf("depsilo-access-logs-%s.csv", time.Now().UTC().Format("2006-01-02"))
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}
```

- [ ] **Step 4: Run focused tests**

Run: `gofmt -w internal/api/admin/logs.go internal/api/admin/logs_test.go && go test ./internal/api/admin -run TestAccessLog -count=1`

Expected: PASS, with one list record and one CSV data record for the same filter.

- [ ] **Step 5: Commit the handler; route registration follows in Task 6**

```bash
git add internal/api/admin/logs.go internal/api/admin/logs_test.go
git commit -m "feat(admin): add filtered access log export"
```

---

### Task 5: Fresh Principal Authentication and JWT-Only Refresh

**Files:**
- Create: `internal/middleware/principal.go`
- Modify: `internal/middleware/auth.go:18-113`
- Create: `internal/middleware/auth_test.go`
- Modify: `internal/api/auth.go:91-106`
- Create: `internal/api/auth_test.go`
- Modify later in Task 6: `internal/api/router.go:116-129`

**Interfaces:**
- Produces: `middleware.Principal{ID uint, Username string, Role string, Enabled bool, AuthMethod string, TokenPermissions *string, CanWrite bool}` with the exact JSON contract.
- Produces: `Authenticate(secret string, database *gorm.DB) gin.HandlerFunc`, accepting JWT or API token and always reloading the current user.
- Produces: `JWTOnly(secret string, database *gorm.DB) gin.HandlerFunc`, accepting only a valid JWT and always reloading the current user.
- Produces: `PrincipalFromContext(*gin.Context) (Principal, bool)`, `ReadRequired() gin.HandlerFunc`, and `WriteRequired() gin.HandlerFunc`.
- Preserves: legacy context keys `user_id`, `username`, and `role` for handlers not yet migrated; their values are populated from the current DB user, never stale claims.
- Produces: `AuthHandler.Me` for `GET /api/v1/auth/me`; JWT uses `token_permissions:null`, API token uses `readonly` or `readwrite`.

- [ ] **Step 1: Write the failing middleware matrix**

Create `internal/middleware/auth_test.go`:

```go
package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
)

const authTestSecret = "0123456789abcdef0123456789abcdef"

func newAuthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.User{}, &db.APIToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func createAuthTestUser(t *testing.T, database *gorm.DB, username, role string, enabled bool) db.User {
	t.Helper()
	user := db.User{Username: username, PasswordHash: "unused", Role: role, Enabled: enabled}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func createAuthTestAPIToken(t *testing.T, database *gorm.DB, userID uint, raw, permissions string) {
	t.Helper()
	digest := sha256.Sum256([]byte(raw))
	token := db.APIToken{UserID: userID, Name: raw, TokenHash: hex.EncodeToString(digest[:]), Permissions: permissions}
	if err := database.Create(&token).Error; err != nil {
		t.Fatalf("create API token: %v", err)
	}
}

func authRequest(r *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestJWTUsesCurrentRoleAndRejectsDisabledUser(t *testing.T) {
	database := newAuthTestDB(t)
	user := createAuthTestUser(t, database, "operator", "admin", true)
	token, err := GenerateJWT(authTestSecret, user.ID, user.Username, user.Role, time.Hour)
	if err != nil {
		t.Fatalf("generate JWT: %v", err)
	}
	r := gin.New()
	r.GET("/read", Authenticate(authTestSecret, database), ReadRequired(), func(c *gin.Context) {
		principal, _ := PrincipalFromContext(c)
		c.JSON(http.StatusOK, principal)
	})
	r.POST("/write", Authenticate(authTestSecret, database), WriteRequired(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	if err := database.Model(&user).Update("role", "readonly").Error; err != nil {
		t.Fatalf("downgrade user: %v", err)
	}
	readRec := authRequest(r, http.MethodGet, "/read", token)
	if readRec.Code != http.StatusOK {
		t.Fatalf("read status = %d, body = %s", readRec.Code, readRec.Body.String())
	}
	var principal Principal
	if err := json.Unmarshal(readRec.Body.Bytes(), &principal); err != nil {
		t.Fatalf("decode principal: %v", err)
	}
	if principal.Role != "readonly" || principal.CanWrite || principal.AuthMethod != AuthMethodJWT || principal.TokenPermissions != nil {
		t.Fatalf("principal = %#v", principal)
	}
	if rec := authRequest(r, http.MethodPost, "/write", token); rec.Code != http.StatusForbidden {
		t.Fatalf("stale admin JWT write status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := database.Model(&user).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if rec := authRequest(r, http.MethodGet, "/read", token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("disabled JWT status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAPITokenPermissionMatrix(t *testing.T) {
	database := newAuthTestDB(t)
	admin := createAuthTestUser(t, database, "admin-owner", "admin", true)
	readonly := createAuthTestUser(t, database, "reader-owner", "readonly", true)
	createAuthTestAPIToken(t, database, admin.ID, "admin-readonly-token", "readonly")
	createAuthTestAPIToken(t, database, admin.ID, "admin-readwrite-token", "readwrite")
	createAuthTestAPIToken(t, database, readonly.ID, "reader-readwrite-token", "readwrite")
	r := gin.New()
	r.GET("/read", Authenticate(authTestSecret, database), ReadRequired(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.POST("/write", Authenticate(authTestSecret, database), WriteRequired(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	tests := []struct {
		name       string
		token      string
		readStatus int
		writeStatus int
	}{
		{name: "admin readonly token", token: "admin-readonly-token", readStatus: http.StatusNoContent, writeStatus: http.StatusForbidden},
		{name: "admin readwrite token", token: "admin-readwrite-token", readStatus: http.StatusNoContent, writeStatus: http.StatusNoContent},
		{name: "readonly owner readwrite token", token: "reader-readwrite-token", readStatus: http.StatusNoContent, writeStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := authRequest(r, http.MethodGet, "/read", tt.token); rec.Code != tt.readStatus {
				t.Fatalf("read status = %d, want %d", rec.Code, tt.readStatus)
			}
			if rec := authRequest(r, http.MethodPost, "/write", tt.token); rec.Code != tt.writeStatus {
				t.Fatalf("write status = %d, want %d", rec.Code, tt.writeStatus)
			}
		})
	}
}

func TestJWTOnlyRejectsAPIToken(t *testing.T) {
	database := newAuthTestDB(t)
	admin := createAuthTestUser(t, database, "admin", "admin", true)
	createAuthTestAPIToken(t, database, admin.ID, "readwrite-api-token", "readwrite")
	r := gin.New()
	r.POST("/refresh", JWTOnly(authTestSecret, database), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	if rec := authRequest(r, http.MethodPost, "/refresh", "readwrite-api-token"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("API token refresh status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run and verify the new interfaces are absent**

Run: `go test ./internal/middleware -run 'TestJWTUses|TestAPIToken|TestJWTOnly' -count=1`

Expected: FAIL to compile because `Principal`, `Authenticate`, `ReadRequired`, `WriteRequired`, and `JWTOnly` are undefined.

- [ ] **Step 3: Add the Principal and capability middleware**

Create `internal/middleware/principal.go`:

```go
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const ContextKeyPrincipal = "principal"

const (
	AuthMethodJWT      = "jwt"
	AuthMethodAPIToken = "api_token"
)

type Principal struct {
	ID               uint    `json:"id"`
	Username         string  `json:"username"`
	Role             string  `json:"role"`
	Enabled          bool    `json:"enabled"`
	AuthMethod       string  `json:"auth_method"`
	TokenPermissions *string `json:"token_permissions"`
	CanWrite         bool    `json:"can_write"`
}

func PrincipalFromContext(c *gin.Context) (Principal, bool) {
	value, exists := c.Get(ContextKeyPrincipal)
	if !exists {
		return Principal{}, false
	}
	principal, ok := value.(Principal)
	return principal, ok
}

func setPrincipal(c *gin.Context, principal Principal) {
	c.Set(ContextKeyPrincipal, principal)
	c.Set(ContextKeyUserID, principal.ID)
	c.Set(ContextKeyUsername, principal.Username)
	c.Set(ContextKeyRole, principal.Role)
}

func ReadRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := PrincipalFromContext(c)
		if !ok || (principal.Role != "admin" && principal.Role != "readonly") {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "read capability required"})
			return
		}
		c.Next()
	}
}

func WriteRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := PrincipalFromContext(c)
		if !ok || !principal.CanWrite {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "write capability required"})
			return
		}
		c.Next()
	}
}
```

- [ ] **Step 4: Resolve both credential kinds against the database**

In `internal/middleware/auth.go`, retain `Claims`, `GenerateJWT`, and `extractToken`, remove `AdminRequired`, and replace the existing `JWTAuth` body with these exact helpers:

```go
func Authenticate(secret string, database *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := extractToken(c)
		if tokenString == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "missing authorization token"})
			return
		}
		if principal, err := resolveJWTPrincipal(secret, database, tokenString); err == nil {
			setPrincipal(c, principal)
			c.Next()
			return
		}
		principal, err := resolveAPITokenPrincipal(database, tokenString)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "invalid or expired token"})
			return
		}
		setPrincipal(c, principal)
		c.Next()
	}
}

func JWTOnly(secret string, database *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := extractToken(c)
		principal, err := resolveJWTPrincipal(secret, database, tokenString)
		if tokenString == "" || err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "valid JWT required"})
			return
		}
		setPrincipal(c, principal)
		c.Next()
	}
}

func resolveJWTPrincipal(secret string, database *gorm.DB, tokenString string) (Principal, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !token.Valid || claims.UserID == 0 {
		return Principal{}, errors.New("invalid JWT")
	}
	var user db.User
	if err := database.First(&user, claims.UserID).Error; err != nil || !user.Enabled {
		return Principal{}, errors.New("user disabled or not found")
	}
	if user.Role != "admin" && user.Role != "readonly" {
		return Principal{}, errors.New("unsupported user role")
	}
	return Principal{
		ID: user.ID, Username: user.Username, Role: user.Role, Enabled: true,
		AuthMethod: AuthMethodJWT, TokenPermissions: nil, CanWrite: user.Role == "admin",
	}, nil
}

func resolveAPITokenPrincipal(database *gorm.DB, tokenString string) (Principal, error) {
	digest := sha256.Sum256([]byte(tokenString))
	tokenHash := hex.EncodeToString(digest[:])
	var apiToken db.APIToken
	if err := database.Where("token_hash = ?", tokenHash).First(&apiToken).Error; err != nil {
		return Principal{}, err
	}
	if apiToken.ExpiresAt != nil && time.Now().After(*apiToken.ExpiresAt) {
		return Principal{}, errors.New("token expired")
	}
	if apiToken.Permissions != "readonly" && apiToken.Permissions != "readwrite" {
		return Principal{}, errors.New("unsupported token permissions")
	}
	var user db.User
	if err := database.First(&user, apiToken.UserID).Error; err != nil || !user.Enabled {
		return Principal{}, errors.New("user disabled or not found")
	}
	if user.Role != "admin" && user.Role != "readonly" {
		return Principal{}, errors.New("unsupported user role")
	}
	now := time.Now()
	if err := database.Model(&apiToken).Update("last_used_at", &now).Error; err != nil {
		zap.L().Warn("failed to update API token last_used_at", zap.Uint("token_id", apiToken.ID), zap.Error(err))
	}
	permissions := apiToken.Permissions
	return Principal{
		ID: user.ID, Username: user.Username, Role: user.Role, Enabled: true,
		AuthMethod: AuthMethodAPIToken, TokenPermissions: &permissions,
		CanWrite: user.Role == "admin" && permissions == "readwrite",
	}, nil
}
```

Add `errors` to the imports. Keep a temporary compatibility wrapper only if a concurrently dirty call site still needs it; delete it in Task 6 after every route uses `Authenticate`:

```go
func JWTAuth(secret string, database *gorm.DB) gin.HandlerFunc {
	return Authenticate(secret, database)
}
```

- [ ] **Step 5: Add `/auth/me` and refresh from current Principal**

In `internal/api/auth.go`, add:

```go
func (h *AuthHandler) Me(c *gin.Context) {
	principal, ok := middleware.PrincipalFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "principal unavailable"})
		return
	}
	c.JSON(http.StatusOK, principal)
}
```

Replace `Refresh` with:

```go
func (h *AuthHandler) Refresh(c *gin.Context) {
	principal, ok := middleware.PrincipalFromContext(c)
	if !ok || principal.AuthMethod != middleware.AuthMethodJWT {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "valid JWT required"})
		return
	}
	token, err := middleware.GenerateJWT(h.cfg.JWTSecret, principal.ID, principal.Username, principal.Role, h.cfg.TokenTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "failed to generate token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "expires_at": time.Now().Add(h.cfg.TokenTTL).Unix()})
}
```

- [ ] **Step 6: Add handler-level response/refresh tests**

Create `internal/api/auth_test.go`. Define these helpers before the test and import `net/http`, `net/http/httptest`, `path/filepath`, Gin, glebarez/sqlite, GORM, and the silent GORM logger:

```go
const authTestJWTSecret = "0123456789abcdef0123456789abcdef"

func newAPIAuthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "api-auth.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.User{}, &db.APIToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func apiAuthRequest(r *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
```

Then register the real middleware and handlers:

```go
func TestAuthMeAndRefreshUseCurrentPrincipal(t *testing.T) {
	database := newAPIAuthTestDB(t)
	user := db.User{Username: "operator", PasswordHash: "unused", Role: "admin", Enabled: true}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	cfg := config.AuthConfig{JWTSecret: authTestJWTSecret, TokenTTL: time.Hour}
	h := NewAuthHandler(database, cfg)
	r := gin.New()
	r.GET("/auth/me", middleware.Authenticate(cfg.JWTSecret, database), h.Me)
	r.POST("/auth/refresh", middleware.JWTOnly(cfg.JWTSecret, database), h.Refresh)
	token, err := middleware.GenerateJWT(cfg.JWTSecret, user.ID, user.Username, "admin", time.Hour)
	if err != nil {
		t.Fatalf("generate JWT: %v", err)
	}
	if err := database.Model(&user).Update("role", "readonly").Error; err != nil {
		t.Fatalf("downgrade: %v", err)
	}

	me := apiAuthRequest(r, http.MethodGet, "/auth/me", token)
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", me.Code, me.Body.String())
	}
	var principal middleware.Principal
	if err := json.Unmarshal(me.Body.Bytes(), &principal); err != nil {
		t.Fatalf("decode principal: %v", err)
	}
	if principal.Role != "readonly" || principal.Enabled != true || principal.AuthMethod != "jwt" || principal.TokenPermissions != nil || principal.CanWrite {
		t.Fatalf("principal = %#v", principal)
	}

	refresh := apiAuthRequest(r, http.MethodPost, "/auth/refresh", token)
	if refresh.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", refresh.Code, refresh.Body.String())
	}
	var refreshed struct { Token string `json:"token"` }
	if err := json.Unmarshal(refresh.Body.Bytes(), &refreshed); err != nil {
		t.Fatalf("decode refresh: %v", err)
	}
	claims := &middleware.Claims{}
	parsed, err := jwt.ParseWithClaims(refreshed.Token, claims, func(token *jwt.Token) (any, error) { return []byte(cfg.JWTSecret), nil })
	if err != nil || !parsed.Valid || claims.Role != "readonly" {
		t.Fatalf("refreshed claims = %#v, err = %v", claims, err)
	}
}
```

- [ ] **Step 7: Run authentication tests with the race detector**

Run: `gofmt -w internal/middleware/auth.go internal/middleware/principal.go internal/middleware/auth_test.go internal/api/auth.go internal/api/auth_test.go && go test -race ./internal/middleware ./internal/api -run 'Test(JWT|API|Auth)' -count=1`

Expected: PASS.

- [ ] **Step 8: Commit Principal authentication**

```bash
git add internal/middleware/auth.go internal/middleware/principal.go internal/middleware/auth_test.go internal/api/auth.go internal/api/auth_test.go
git commit -m "feat(auth): resolve current request principal"
```

---

### Task 6: Read/Write Route Groups and Readonly Credential Redaction

**Files:**
- Modify carefully: `internal/api/router.go:116-270`
- Create: `internal/api/router_permissions_test.go`
- Create: `internal/api/admin/credentials.go`
- Create: `internal/api/admin/credentials_test.go`
- Modify: `internal/api/admin/webhook.go:28-39`
- Modify narrowly: `internal/api/admin/upstream.go:23-27`

**Interfaces:**
- Consumes: `middleware.Authenticate`, `ReadRequired`, `WriteRequired`, `JWTOnly`, and `PrincipalFromContext` from Task 5.
- Produces: Gin groups named `adminRead`, `adminWrite`, `proRead`, and `proWrite`; no Admin endpoint registers directly on `adminGroup` or a capability-free Pro group.
- Produces: `GET /api/v1/auth/me`, `GET /api/v1/admin/logs/export`, and the exact capability classification in Global Constraints.
- Produces: `maskWebhookURL(string) string` and `maskURLUserInfo(string) string`; write-capable principals receive operational values, readonly principals receive redacted credentials.

- [ ] **Step 1: Write a declaration-level route guard test**

Create `internal/api/router_permissions_test.go`:

```go
package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestAdminRoutesUseExplicitCapabilityGroups(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "router.go", nil, 0)
	if err != nil {
		t.Fatalf("parse router.go: %v", err)
	}
	httpMethods := map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
	readCount := 0
	writeCount := 0
	seenRulesTest := false
	seenLogExport := false

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !httpMethods[selector.Sel.Name] || len(call.Args) == 0 {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		pathLiteral, ok := call.Args[0].(*ast.BasicLit)
		if !ok || pathLiteral.Kind != token.STRING {
			return true
		}
		path, err := strconv.Unquote(pathLiteral.Value)
		if err != nil {
			t.Fatalf("unquote route path %s: %v", pathLiteral.Value, err)
		}
		method := selector.Sel.Name
		switch receiver.Name {
		case "adminGroup", "proGroup":
			t.Errorf("%s %s bypasses explicit capability group via %s", method, path, receiver.Name)
		case "adminRead", "proRead":
			readCount++
			if method != "GET" && !(receiver.Name == "adminRead" && method == "POST" && path == "/rules/test") {
				t.Errorf("%s %s incorrectly registered as read capability", method, path)
			}
			seenRulesTest = seenRulesTest || (method == "POST" && path == "/rules/test")
			seenLogExport = seenLogExport || (method == "GET" && path == "/logs/export")
		case "adminWrite", "proWrite":
			writeCount++
			if method == "GET" {
				t.Errorf("GET %s incorrectly registered as write capability", path)
			}
		}
		return true
	})
	if readCount == 0 || writeCount == 0 {
		t.Fatalf("route counts read=%d write=%d", readCount, writeCount)
	}
	if !seenRulesTest {
		t.Fatal("POST /rules/test is not registered on adminRead")
	}
	if !seenLogExport {
		t.Fatal("GET /logs/export is not registered on adminRead")
	}
}
```

- [ ] **Step 2: Run the route test and verify direct `adminGroup` registration fails**

Run: `go test ./internal/api -run TestAdminRoutesUseExplicitCapabilityGroups -count=1`

Expected: FAIL with findings for the existing `adminGroup.GET`, `adminGroup.POST`, `adminGroup.PUT`, and `adminGroup.DELETE` calls.

- [ ] **Step 3: Register auth and capability groups**

In `internal/api/router.go`, reread and preserve the current dirty diff. Replace the auth middleware block with:

```go
	authGroup.POST("/login", authHandler.Login)
	authGroup.POST("/logout", authHandler.Logout)
	authGroup.GET("/me", middleware.Authenticate(deps.Config.Auth.JWTSecret, deps.DB), authHandler.Me)
	authGroup.POST("/refresh", middleware.JWTOnly(deps.Config.Auth.JWTSecret, deps.DB), authHandler.Refresh)

	adminGroup := apiV1.Group("/admin")
	adminGroup.Use(middleware.Authenticate(deps.Config.Auth.JWTSecret, deps.DB))
	adminRead := adminGroup.Group("")
	adminRead.Use(middleware.ReadRequired())
	adminWrite := adminGroup.Group("")
	adminWrite.Use(middleware.WriteRequired())
```

After all non-Pro handler construction, create Pro groups with entitlement after capability enforcement:

```go
	proRead := adminRead.Group("")
	proRead.Use(entitlement.RequirePro(deps.Entitlement))
	proWrite := adminWrite.Group("")
	proWrite.Use(entitlement.RequirePro(deps.Entitlement))
```

Register every existing endpoint on these exact receivers:

```go
	adminRead.GET("/dashboard", dashHandler.GetDashboard)
	adminRead.GET("/dashboard/trends", dashHandler.GetTrends)
	adminRead.GET("/bandwidth", bandwidthHandler.GetReport)
	adminRead.GET("/cache", cacheHandler.List)
	adminRead.GET("/cache/distribution", cacheHandler.GetDistribution)
	adminWrite.DELETE("/cache/:id", cacheHandler.Delete)
	adminWrite.POST("/cache/cleanup", cacheHandler.Cleanup)
	adminWrite.POST("/cache/warmup", warmupHandler.Warmup)
	adminRead.GET("/upstreams", upstreamHandler.List)
	adminRead.GET("/upstreams/:id/latency", latencyHandler.GetLatencyHistory)
	adminWrite.POST("/upstreams", upstreamHandler.Create)
	adminWrite.PUT("/upstreams/:id", upstreamHandler.Update)
	adminWrite.DELETE("/upstreams/:id", upstreamHandler.Delete)
	adminWrite.POST("/upstreams/:id/check", upstreamHandler.Check)
	adminRead.GET("/logs", logHandler.List)
	adminRead.GET("/logs/export", logHandler.Export)
	adminRead.GET("/users", userHandler.List)
	adminWrite.POST("/users", userHandler.Create)
	adminWrite.PUT("/users/:id", userHandler.Update)
	adminWrite.DELETE("/users/:id", userHandler.Delete)
	adminRead.GET("/tokens", tokenHandler.List)
	adminWrite.POST("/tokens", tokenHandler.Create)
	adminWrite.DELETE("/tokens/:id", tokenHandler.Delete)
	adminRead.GET("/settings", settingsHandler.Get)
	adminWrite.PUT("/settings", settingsHandler.Update)
	adminRead.GET("/webhooks", webhookHandler.List)
	adminWrite.POST("/webhooks", webhookHandler.Create)
	adminWrite.PUT("/webhooks/:id", webhookHandler.Update)
	adminWrite.DELETE("/webhooks/:id", webhookHandler.Delete)
	adminWrite.POST("/webhooks/:id/test", webhookHandler.Test)
	adminRead.GET("/license/status", licenseHandler.GetStatus)
	adminWrite.POST("/license/revalidate", licenseHandler.Revalidate)
	adminWrite.POST("/license/trial/activate", licenseHandler.ActivateTrial)
	adminWrite.PUT("/license/key", licenseHandler.SetKey)
	adminWrite.DELETE("/license/key", licenseHandler.ClearKey)
	adminRead.GET("/audit-logs", auditHandler.List)
	adminRead.GET("/audit-logs/export", auditHandler.Export)
	adminRead.GET("/rules", rulesHandler.List)
	adminRead.POST("/rules/test", rulesHandler.Test)
	adminWrite.POST("/rules", rulesHandler.Create)
	adminWrite.PUT("/rules/:id", rulesHandler.Update)
	adminWrite.DELETE("/rules/:id", rulesHandler.Delete)
	adminRead.GET("/security/dashboard", securityHandler.Dashboard)
	adminRead.GET("/security/vulnerabilities", securityHandler.ListVulnerabilities)
	adminRead.GET("/security/packages", securityHandler.ListPackages)
	adminRead.GET("/security/suggestions", securityHandler.ListSuggestions)
	adminRead.GET("/security/policies", securityHandler.ListPolicies)
	adminWrite.POST("/security/suggestions/:vuln_id/approve", securityHandler.ApproveSuggestion)
	adminWrite.POST("/security/suggestions/:vuln_id/dismiss", securityHandler.DismissSuggestion)
	adminWrite.POST("/security/scan", securityHandler.TriggerScan)
	adminWrite.POST("/security/import", securityHandler.ImportData)
	adminWrite.PUT("/security/policies/:ecosystem", securityHandler.UpdatePolicy)
	adminRead.GET("/quarantine/events", quarantineHandler.ListEvents)
	adminRead.GET("/quarantine/approvals", quarantineHandler.ListApprovals)
	adminWrite.POST("/quarantine/approve", quarantineHandler.Approve)
	adminWrite.DELETE("/quarantine/approvals/:id", quarantineHandler.Revoke)
	adminRead.GET("/blocklist/status", blocklistHandler.Status)
	adminRead.GET("/blocklist/overrides", blocklistHandler.ListOverrides)
	adminWrite.POST("/blocklist/sync", blocklistHandler.TriggerSync)
	adminWrite.POST("/blocklist/overrides", blocklistHandler.CreateOverride)
	adminWrite.DELETE("/blocklist/overrides/:id", blocklistHandler.RevokeOverride)
	proRead.GET("/projects", projectsHandler.List)
	proRead.GET("/projects/:id", projectsHandler.Detail)
	proRead.GET("/projects/:id/packages", projectsHandler.ListPackages)
	proRead.GET("/projects/:id/sbom", projectsHandler.ExportSBOM)
	proWrite.POST("/projects", projectsHandler.Create)
	proWrite.PUT("/projects/:id", projectsHandler.Update)
	proWrite.DELETE("/projects/:id", projectsHandler.Delete)
	proWrite.POST("/projects/:id/token", projectsHandler.RegenerateToken)
```

Keep each handler constructor exactly once. Delete the compatibility `JWTAuth` wrapper after `rg 'JWTAuth|AdminRequired' --glob '*.go'` returns only historical test text or no matches.

- [ ] **Step 4: Write credential-redaction tests**

Create `internal/api/admin/credentials_test.go`:

```go
package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
	"depsilo/internal/middleware"
)

func TestCredentialURLMasking(t *testing.T) {
	if got := maskWebhookURL("https://hooks.example.test/services/T000/B000/secret?token=hidden"); got != "https://hooks.example.test/***" {
		t.Fatalf("maskWebhookURL = %q", got)
	}
	if got := maskWebhookURL("not a URL"); got != "***" {
		t.Fatalf("invalid maskWebhookURL = %q", got)
	}
	if got := maskURLUserInfo("http://alice:password@proxy.example.test:8080/path"); got != "http://***:***@proxy.example.test:8080/path" {
		t.Fatalf("maskURLUserInfo = %q", got)
	}
}

func TestWebhookListMasksURLForReadonlyPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "webhooks.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.WebhookConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	config := db.WebhookConfig{Name: "ops", Platform: "slack", URL: "https://hooks.example.test/services/secret", Enabled: true, Events: "*"}
	if err := database.Create(&config).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := NewWebhookHandler(database, nil)
	request := func(canWrite bool) map[string]any {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set(middleware.ContextKeyPrincipal, middleware.Principal{ID: 1, Username: "operator", Role: "readonly", Enabled: true, AuthMethod: middleware.AuthMethodJWT, CanWrite: canWrite})
			c.Next()
		})
		r.GET("/webhooks", h.List)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/webhooks", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var items []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return items[0]
	}
	if got := request(false)["url"]; got != "https://hooks.example.test/***" {
		t.Fatalf("readonly url = %v", got)
	}
	if got := request(true)["url"]; got != config.URL {
		t.Fatalf("admin url = %v", got)
	}
}
```

- [ ] **Step 5: Implement redaction and explicit webhook list responses**

Create `internal/api/admin/credentials.go`:

```go
package admin

import (
	"net/url"

	"github.com/gin-gonic/gin"

	"depsilo/internal/middleware"
)

func principalCanViewCredentials(c *gin.Context) bool {
	principal, ok := middleware.PrincipalFromContext(c)
	return ok && principal.CanWrite
}

func maskWebhookURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "***"
	}
	return parsed.Scheme + "://" + parsed.Host + "/***"
}

func maskURLUserInfo(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	parsed.User = url.UserPassword("***", "***")
	return parsed.String()
}
```

In `internal/api/admin/webhook.go`, add an explicit list DTO and replace `List`:

```go
type webhookListResponse struct {
	ID              uint       `json:"id"`
	Name            string     `json:"name"`
	Platform        string     `json:"platform"`
	URL             string     `json:"url"`
	Enabled         bool       `json:"enabled"`
	Events          string     `json:"events"`
	CooldownMinutes int        `json:"cooldown_minutes"`
	LastSentAt      *time.Time `json:"last_sent_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (h *WebhookHandler) List(c *gin.Context) {
	var configs []db.WebhookConfig
	if err := h.DB.Order("created_at DESC").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	canViewCredentials := principalCanViewCredentials(c)
	items := make([]webhookListResponse, len(configs))
	for i, config := range configs {
		urlValue := config.URL
		if !canViewCredentials {
			urlValue = maskWebhookURL(urlValue)
		}
		items[i] = webhookListResponse{
			ID: config.ID, Name: config.Name, Platform: config.Platform, URL: urlValue,
			Enabled: config.Enabled, Events: config.Events, CooldownMinutes: config.CooldownMinutes,
			LastSentAt: config.LastSentAt, CreatedAt: config.CreatedAt, UpdatedAt: config.UpdatedAt,
		}
	}
	c.JSON(http.StatusOK, items)
}
```

In the current `UpstreamHandler.List`, copy records before redaction so persistence is never changed:

```go
	if !principalCanViewCredentials(c) {
		for i := range upstreams {
			upstreams[i].URL = maskURLUserInfo(upstreams[i].URL)
			upstreams[i].Proxy = maskURLUserInfo(upstreams[i].Proxy)
		}
	}
```

The later Registry implementation must preserve this mapper behavior when it replaces `db.UpstreamRecord` responses.

- [ ] **Step 6: Run route, redaction, and permission tests**

Run: `gofmt -w internal/api/router.go internal/api/router_permissions_test.go internal/api/admin/credentials.go internal/api/admin/credentials_test.go internal/api/admin/webhook.go internal/api/admin/upstream.go && go test -race ./internal/middleware ./internal/api/... -run 'Test(AdminRoutes|Credential|Webhook|JWT|API)' -count=1`

Expected: PASS. `rg -n 'adminGroup\.(GET|POST|PUT|PATCH|DELETE)|proGroup\.(GET|POST|PUT|PATCH|DELETE)' internal/api/router.go` must print no matches.

- [ ] **Step 7: Commit route capabilities and masking**

```bash
git add -p internal/api/router.go
git add internal/api/router_permissions_test.go internal/api/admin/credentials.go internal/api/admin/credentials_test.go internal/api/admin/webhook.go internal/api/admin/upstream.go
git commit -m "fix(auth): enforce admin route capabilities"
```

---

### Task 7: Self-Lockout and Final-Admin Invariants

**Files:**
- Modify: `internal/api/admin/user.go:14-143`
- Create: `internal/api/admin/user_test.go`

**Interfaces:**
- Consumes: `middleware.PrincipalFromContext` from Task 5.
- Produces: an effective admin predicate of `role = 'admin' AND enabled = true`.
- Produces: `409 SELF_LOCKOUT` for self-delete, self-disable, or self-demotion; self password updates remain `200`.
- Produces: `409 LAST_ADMIN` whenever a mutation would remove the final effective admin.
- Produces: serialized, transactional Update/Delete mutations so two concurrent admins cannot remove each other and leave zero effective admins.

- [ ] **Step 1: Write failing self and concurrency tests**

Create `internal/api/admin/user_test.go`:

```go
package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
	"depsilo/internal/middleware"
)

func newUserTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "users.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.User{}, &db.APIToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := NewUserHandler(database)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		actorID, err := strconv.ParseUint(c.GetHeader("X-Actor-ID"), 10, 64)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(middleware.ContextKeyPrincipal, middleware.Principal{ID: uint(actorID), Username: "actor", Role: "admin", Enabled: true, AuthMethod: middleware.AuthMethodJWT, CanWrite: true})
		c.Next()
	})
	r.PUT("/users/:id", h.Update)
	r.DELETE("/users/:id", h.Delete)
	return r, database
}

func createUserForMutationTest(t *testing.T, database *gorm.DB, username, role string, enabled bool) db.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user := db.User{Username: username, PasswordHash: string(hash), Role: role, Enabled: enabled}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func userMutationRequest(r *gin.Engine, method, path, body string, actorID uint) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Actor-ID", strconv.FormatUint(uint64(actorID), 10))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func responseCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct { Code string `json:"code"` }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return body.Code
}

func TestUserCannotLockOutSelfButCanChangePassword(t *testing.T) {
	r, database := newUserTestRouter(t)
	admin := createUserForMutationTest(t, database, "admin", "admin", true)
	path := "/users/" + strconv.FormatUint(uint64(admin.ID), 10)
	for _, body := range []string{`{"role":"readonly"}`, `{"enabled":false}`} {
		rec := userMutationRequest(r, http.MethodPut, path, body, admin.ID)
		if rec.Code != http.StatusConflict || responseCode(t, rec) != "SELF_LOCKOUT" {
			t.Fatalf("body %s status=%d response=%s", body, rec.Code, rec.Body.String())
		}
	}
	deleteRec := userMutationRequest(r, http.MethodDelete, path, "", admin.ID)
	if deleteRec.Code != http.StatusConflict || responseCode(t, deleteRec) != "SELF_LOCKOUT" {
		t.Fatalf("self delete status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	passwordRec := userMutationRequest(r, http.MethodPut, path, `{"password":"new-password"}`, admin.ID)
	if passwordRec.Code != http.StatusOK {
		t.Fatalf("password status=%d body=%s", passwordRec.Code, passwordRec.Body.String())
	}
	var saved db.User
	if err := database.First(&saved, admin.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(saved.PasswordHash), []byte("new-password")); err != nil {
		t.Fatalf("password not updated: %v", err)
	}
	if saved.Role != "admin" || !saved.Enabled {
		t.Fatalf("saved user = %#v", saved)
	}
}

func TestConcurrentAdminDemotionsLeaveOneEnabledAdmin(t *testing.T) {
	r, database := newUserTestRouter(t)
	first := createUserForMutationTest(t, database, "first", "admin", true)
	second := createUserForMutationTest(t, database, "second", "admin", true)
	type result struct { Status int; Code string }
	results := make(chan result, 2)
	var start sync.WaitGroup
	start.Add(2)
	run := func(actor, target uint) {
		defer start.Done()
		path := "/users/" + strconv.FormatUint(uint64(target), 10)
		rec := userMutationRequest(r, http.MethodPut, path, `{"role":"readonly"}`, actor)
		code := ""
		if rec.Code == http.StatusConflict {
			var body struct { Code string `json:"code"` }
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				code = "DECODE_ERROR"
			} else {
				code = body.Code
			}
		}
		results <- result{Status: rec.Code, Code: code}
	}
	go run(first.ID, second.ID)
	go run(second.ID, first.ID)
	start.Wait()
	close(results)
	statuses := map[int]int{}
	for item := range results {
		statuses[item.Status]++
		if item.Status == http.StatusConflict && item.Code != "LAST_ADMIN" {
			t.Fatalf("conflict code = %s", item.Code)
		}
	}
	if statuses[http.StatusOK] != 1 || statuses[http.StatusConflict] != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	var active int64
	if err := database.Model(&db.User{}).Where("role = ? AND enabled = ?", "admin", true).Count(&active).Error; err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if active != 1 {
		t.Fatalf("active admins = %d, want 1", active)
	}
}
```

- [ ] **Step 2: Run and verify the unsafe mutations**

Run: `go test -race ./internal/api/admin -run 'TestUserCannot|TestConcurrentAdmin' -count=1`

Expected: FAIL. Existing Update demotes/disables self, and concurrent demotions can leave zero admins.

- [ ] **Step 3: Serialize mutations and define invariant errors**

In `internal/api/admin/user.go`, add `errors`, `sync`, and `depsilo/internal/middleware`, then change the handler and constructor to:

```go
type UserHandler struct {
	db         *gorm.DB
	mutationMu sync.Mutex
}

func NewUserHandler(database *gorm.DB) *UserHandler {
	return &UserHandler{db: database}
}

var (
	errSelfLockout = errors.New("self lockout")
	errLastAdmin   = errors.New("last enabled admin")
	errUserMissing = errors.New("user not found")
)

func mutationPrincipal(c *gin.Context) (middleware.Principal, error) {
	principal, ok := middleware.PrincipalFromContext(c)
	if !ok {
		return middleware.Principal{}, errors.New("principal unavailable")
	}
	return principal, nil
}

func effectiveAdmin(user db.User) bool {
	return user.Role == "admin" && user.Enabled
}

func anotherEffectiveAdminExists(tx *gorm.DB, excludedID uint) (bool, error) {
	var count int64
	err := tx.Model(&db.User{}).Where("role = ? AND enabled = ? AND id != ?", "admin", true, excludedID).Count(&count).Error
	return count > 0, err
}

func writeUserMutationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errSelfLockout):
		c.JSON(http.StatusConflict, gin.H{"code": "SELF_LOCKOUT", "message": "current user cannot delete, disable, or demote itself"})
	case errors.Is(err, errLastAdmin):
		c.JSON(http.StatusConflict, gin.H{"code": "LAST_ADMIN", "message": "at least one enabled admin must remain"})
	case errors.Is(err, errUserMissing):
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "user not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
	}
}
```

- [ ] **Step 4: Replace Update with one guarded transaction**

Parse the ID and body before acquiring the mutex. Hash a non-empty requested password before the transaction. Then use this exact mutation core:

```go
	principal, err := mutationPrincipal(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": err.Error()})
		return
	}
	updates := map[string]any{}
	if req.Password != nil && *req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "failed to hash password"})
			return
		}
		updates["password_hash"] = string(hash)
	}
	if req.Role != nil {
		updates["role"] = *req.Role
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	h.mutationMu.Lock()
	defer h.mutationMu.Unlock()
	var saved db.User
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var target db.User
		if err := tx.First(&target, uint(id)).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errUserMissing
			}
			return err
		}
		selfDisable := req.Enabled != nil && !*req.Enabled
		selfDemote := req.Role != nil && *req.Role != "admin"
		if target.ID == principal.ID && (selfDisable || selfDemote) {
			return errSelfLockout
		}
		removesEffectiveAdmin := effectiveAdmin(target) && (selfDisable || selfDemote)
		if removesEffectiveAdmin {
			another, err := anotherEffectiveAdminExists(tx, target.ID)
			if err != nil {
				return err
			}
			if !another {
				return errLastAdmin
			}
		}
		if len(updates) > 0 {
			if err := tx.Model(&target).Updates(updates).Error; err != nil {
				return err
			}
		}
		return tx.First(&saved, target.ID).Error
	})
	if err != nil {
		writeUserMutationError(c, err)
		return
	}
	saved.PasswordHash = ""
	c.JSON(http.StatusOK, saved)
```

- [ ] **Step 5: Replace Delete with the same serialized invariant**

After parsing ID and Principal, use:

```go
	if uint(id) == principal.ID {
		c.JSON(http.StatusConflict, gin.H{"code": "SELF_LOCKOUT", "message": "current user cannot delete itself"})
		return
	}
	h.mutationMu.Lock()
	defer h.mutationMu.Unlock()
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var target db.User
		if err := tx.First(&target, uint(id)).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errUserMissing
			}
			return err
		}
		if effectiveAdmin(target) {
			another, err := anotherEffectiveAdminExists(tx, target.ID)
			if err != nil {
				return err
			}
			if !another {
				return errLastAdmin
			}
		}
		if err := tx.Where("user_id = ?", target.ID).Delete(&db.APIToken{}).Error; err != nil {
			return err
		}
		return tx.Delete(&target).Error
	})
	if err != nil {
		writeUserMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
```

- [ ] **Step 6: Run focused and race tests**

Run: `gofmt -w internal/api/admin/user.go internal/api/admin/user_test.go && go test -race ./internal/api/admin -run 'TestUserCannot|TestConcurrentAdmin' -count=10`

Expected: PASS in all ten runs. Then run `go test -race ./internal/middleware ./internal/api/...`; expected PASS.

- [ ] **Step 7: Commit invariant enforcement**

```bash
git add internal/api/admin/user.go internal/api/admin/user_test.go
git commit -m "fix(admin): prevent administrator lockout"
```

---

### Task 8: Typed Admin Client and `usePrincipal` Foundation

**Files:**
- Create: `web/src/lib/adminApi.types.ts`
- Create: `web/src/lib/adminApi.types.type-test.ts`
- Modify carefully: `web/src/lib/api.ts:1-143`
- Create: `web/src/hooks/usePrincipal.ts`
- Modify: `web/src/admin/AdminApp.tsx:18-40`
- Modify: `web/src/admin/components/MainLayout.tsx:42-57,155-167`
- Modify: `web/src/admin/pages/Login.tsx:25-55`
- Modify: `web/src/admin/pages/Users.tsx:17-118`
- Modify: `web/src/admin/pages/Security.tsx:163-181,427-480`
- Modify: `web/src/admin/pages/AccessLogs.tsx:20-41,125-173`
- Modify: `web/src/admin/pages/AuditLogs.tsx:34-62,175-208`
- Modify: `web/src/admin/pages/Projects.tsx:65-112,169-190,290-335`

**Interfaces:**
- Consumes: the Go JSON contracts from Tasks 1-7. Settings and Upstream remain owned by their dedicated plans.
- Produces: `authApi.me(): Promise<AxiosResponse<Principal>>` and typed Axios methods for Principal, Security, Logs, Audit, Projects, Users, and API tokens.
- Produces: `principalQueryKey`, `canWrite(principal)`, and `usePrincipal(enabled?)`, with one TanStack Query cache entry shared by shell and pages.
- Produces: protected Admin rendering only after `/auth/me` succeeds; localStorage contains the bearer token only and is never a role/capability source.
- Produces: Users self-actions hidden from the current Principal; the server remains authoritative if a client sends a forbidden request manually.

- [ ] **Step 1: Add compile-time contract assertions first**

Create `web/src/lib/adminApi.types.type-test.ts` before the types file exists:

```ts
import { adminApi, authApi } from './api'
import type {
  AccessLogListResponse,
  AuditLogListResponse,
  Principal,
  ProjectListResponse,
  ProjectPackagesResponse,
  SecurityPolicy,
  SecurityVulnerabilityPage,
} from './adminApi.types'

export type Equal<A, B> =
  (<T>() => T extends A ? 1 : 2) extends
  (<T>() => T extends B ? 1 : 2) ? true : false
export type Assert<T extends true> = T
export type ResponseData<T extends (...args: never[]) => unknown> =
  Awaited<ReturnType<T>> extends { data: infer Data } ? Data : never

export type PrincipalContract = Assert<Equal<ResponseData<typeof authApi.me>, Principal>>
export type LogsContract = Assert<Equal<ResponseData<typeof adminApi.listLogs>, AccessLogListResponse>>
export type AuditContract = Assert<Equal<ResponseData<typeof adminApi.listAuditLogs>, AuditLogListResponse>>
export type VulnerabilityContract = Assert<Equal<ResponseData<typeof adminApi.listVulnerabilities>, SecurityVulnerabilityPage>>
export type PolicyContract = Assert<Equal<ResponseData<typeof adminApi.listSecurityPolicies>, SecurityPolicy[]>>
export type ProjectsContract = Assert<Equal<ResponseData<typeof adminApi.listProjects>, ProjectListResponse>>
export type ProjectPackagesContract = Assert<Equal<ResponseData<typeof adminApi.listProjectPackages>, ProjectPackagesResponse>>
```

- [ ] **Step 2: Run type-check and verify the contract module is missing**

Run: `cd web && npm run type-check`

Expected: FAIL with `Cannot find module './adminApi.types'` and missing `authApi.me`.

- [ ] **Step 3: Define every affected TypeScript contract**

Create `web/src/lib/adminApi.types.ts`:

```ts
export type UserRole = 'admin' | 'readonly'
export type TokenPermissions = 'readonly' | 'readwrite'

export interface Principal {
  id: number
  username: string
  role: UserRole
  enabled: true
  auth_method: 'jwt' | 'api_token'
  token_permissions: TokenPermissions | null
  can_write: boolean
}

export interface LoginResponse {
  token: string
  expires_at: number
  user: { id: number; username: string; role: UserRole }
}

export interface RefreshResponse { token: string; expires_at: number }

export type SecuritySeverity = 'critical' | 'high' | 'medium' | 'low'

export interface SecurityDashboard {
  total_vulnerabilities: number
  affected_packages: number
  by_severity: Record<SecuritySeverity, number>
  auto_blocked_count: number
  last_scan_at: string | null
  scan_in_progress: boolean
}

export interface SecurityVulnerability {
  id: number
  osv_id: string
  ecosystem: string
  package_name: string
  affected_ranges: string
  severity: SecuritySeverity
  cvss_score: number
  summary: string
  details: string
  aliases: string
  references: string
  published_at: string
  modified_at: string
  created_at: string
  updated_at: string
}

export interface SecurityVulnerabilityCheck {
  id: number
  ecosystem: string
  package_name: string
  has_vulnerabilities: boolean
  vulnerability_count: number
  last_fetched_at: string
  next_fetch_at: string
  created_at: string
  updated_at: string
}

export interface SecurityPolicy {
  id: number
  ecosystem: string
  auto_block_enabled: boolean
  min_cvss_score: number
  created_by: string
  created_at: string
  updated_at: string
}

export interface SecurityPage<T> { items: T[]; total: number; page: number }
export type SecurityVulnerabilityPage = SecurityPage<SecurityVulnerability>
export type SecuritySuggestionPage = SecurityPage<SecurityVulnerability>
export type SecurityPackagePage = SecurityPage<SecurityVulnerabilityCheck>
export interface SecurityQuery { page?: number; per_page?: number; ecosystem?: string; severity?: SecuritySeverity; package?: string }
export interface UpdateSecurityPolicyRequest { auto_block_enabled: boolean; min_cvss_score: number }
export interface ApproveSuggestionRequest { version?: string; reason?: string }

export interface AccessLog {
  id: number
  adapter_type: string
  method: string
  cache_key: string
  package_name: string
  hit: boolean
  upstream: string
  latency_ms: number
  status_code: number
  client_ip: string
  bytes_sent: number
  created_at: string
}

export interface AccessLogQuery { page?: number; page_size?: number; search?: string; adapter_type?: string; hit?: boolean }
export interface AccessLogListResponse { items: AccessLog[]; total: number; page: number; page_size: number }

export interface AuditLog {
  id: number
  ecosystem: string
  package_name: string
  version: string
  action: string
  cache_result: 'hit' | 'miss' | 'error'
  client_ip: string
  user_agent: string
  upstream_url: string
  latency_ms: number
  bytes_sent: number
  status_code: number
  created_at: string
}

export interface AuditLogQuery {
  page?: number
  page_size?: number
  ecosystem?: string
  package?: string
  ip?: string
  result?: 'hit' | 'miss' | 'error'
  start?: string
  end?: string
}
export interface AuditLogListResponse { items: AuditLog[]; total: number; page: number }

export interface Project {
  id: number
  name: string
  slug: string
  description: string
  created_at: string
  updated_at: string
}

export interface ProjectSummary extends Project { package_count: number; last_activity_at: string | null }
export interface ProjectListResponse { items: ProjectSummary[]; total: number }
export interface CreateProjectRequest { name: string; description: string }
export interface UpdateProjectRequest { name?: string; description?: string }
export interface CreateProjectResponse { id: number; name: string; slug: string; description: string; token: string; proxy_url: string; created_at: string }
export interface ProjectDetail extends ProjectSummary { proxy_url: string; ecosystem_breakdown: Record<string, number> }
export interface ProjectPackage { ecosystem: string; package_name: string; version: string; first_seen_at: string; last_seen_at: string; download_count: number }
export interface ProjectPackageQuery { page?: number; per_page?: number; ecosystem?: string; search?: string }
export interface ProjectPackagesResponse { items: ProjectPackage[]; total: number; page: number }
export interface RegenerateProjectTokenResponse { token: string; proxy_url: string }

export interface AdminUser { id: number; username: string; role: UserRole; enabled: boolean; last_login_at: string | null; created_at: string; updated_at: string }
export interface CreateUserRequest { username: string; password: string; role: UserRole }
export interface UpdateUserRequest { password?: string; role?: UserRole; enabled?: boolean }
export interface APITokenSummary { id: number; user_id: number; name: string; permissions: TokenPermissions; expires_at: string | null; last_used_at: string | null; created_at: string }
export interface CreateAPITokenRequest { name: string; permissions: TokenPermissions; ttl: '7d' | '30d' | '90d' | 'never' }
export interface CreateAPITokenResponse { id: number; name: string; token: string; permissions: TokenPermissions; expires_at: string | null; warning: string }
```

- [ ] **Step 4: Type the affected Axios boundary**

At the top of the already-dirty `web/src/lib/api.ts`, add this exact type-only import. Preserve unrelated changes, then use the affected signatures below:

```ts
import type {
  AccessLogListResponse,
  AccessLogQuery,
  AdminUser,
  APITokenSummary,
  ApproveSuggestionRequest,
  AuditLogListResponse,
  AuditLogQuery,
  CreateAPITokenRequest,
  CreateAPITokenResponse,
  CreateProjectRequest,
  CreateProjectResponse,
  CreateUserRequest,
  LoginResponse,
  Principal,
  Project,
  ProjectDetail,
  ProjectListResponse,
  ProjectPackageQuery,
  ProjectPackagesResponse,
  RefreshResponse,
  RegenerateProjectTokenResponse,
  SecurityDashboard,
  SecurityPackagePage,
  SecurityPolicy,
  SecurityQuery,
  SecuritySuggestionPage,
  SecurityVulnerabilityPage,
  UpdateProjectRequest,
  UpdateSecurityPolicyRequest,
  UpdateUserRequest,
} from './adminApi.types'
```

```ts
export const authApi = {
  login: (data: { username: string; password: string }) => api.post<LoginResponse>('/auth/login', data),
  logout: () => api.post<{ message: string }>('/auth/logout'),
  me: () => api.get<Principal>('/auth/me'),
  refresh: () => api.post<RefreshResponse>('/auth/refresh'),
}

export const adminApi = {
  listLogs: (params: AccessLogQuery) => api.get<AccessLogListResponse>('/admin/logs', { params }),
  exportLogs: (params: AccessLogQuery) => api.get<Blob>('/admin/logs/export', { params, responseType: 'blob' }),
  listAuditLogs: (params: AuditLogQuery) => api.get<AuditLogListResponse>('/admin/audit-logs', { params }),
  exportAuditLogs: (params: AuditLogQuery) => api.get<Blob>('/admin/audit-logs/export', { params, responseType: 'blob' }),
  getSecurityDashboard: () => api.get<SecurityDashboard>('/admin/security/dashboard'),
  listVulnerabilities: (params: SecurityQuery) => api.get<SecurityVulnerabilityPage>('/admin/security/vulnerabilities', { params }),
  listVulnerablePackages: (params: SecurityQuery) => api.get<SecurityPackagePage>('/admin/security/packages', { params }),
  listSuggestions: (params: SecurityQuery) => api.get<SecuritySuggestionPage>('/admin/security/suggestions', { params }),
  approveSuggestion: (vulnerabilityID: number, data: ApproveSuggestionRequest = {}) => api.post<{ rule_id: number }>(`/admin/security/suggestions/${vulnerabilityID}/approve`, data),
  listSecurityPolicies: () => api.get<SecurityPolicy[]>('/admin/security/policies'),
  updateSecurityPolicy: (ecosystem: string, data: UpdateSecurityPolicyRequest) => api.put<SecurityPolicy>(`/admin/security/policies/${ecosystem}`, data),
  listProjects: () => api.get<ProjectListResponse>('/admin/projects'),
  createProject: (data: CreateProjectRequest) => api.post<CreateProjectResponse>('/admin/projects', data),
  getProject: (id: number) => api.get<ProjectDetail>(`/admin/projects/${id}`),
  updateProject: (id: number, data: UpdateProjectRequest) => api.put<Project>(`/admin/projects/${id}`, data),
  listProjectPackages: (id: number, params: ProjectPackageQuery) => api.get<ProjectPackagesResponse>(`/admin/projects/${id}/packages`, { params }),
  regenerateProjectToken: (id: number) => api.post<RegenerateProjectTokenResponse>(`/admin/projects/${id}/token`),
  listUsers: () => api.get<AdminUser[]>('/admin/users'),
  createUser: (data: CreateUserRequest) => api.post<AdminUser>('/admin/users', data),
  updateUser: (id: number, data: UpdateUserRequest) => api.put<AdminUser>(`/admin/users/${id}`, data),
  listTokens: () => api.get<APITokenSummary[]>('/admin/tokens'),
  createToken: (data: CreateAPITokenRequest) => api.post<CreateAPITokenResponse>('/admin/tokens', data),
}
```

Merge these members into the existing `adminApi` object rather than deleting unaffected dashboard, cache, rules, quarantine, blocklist, license, delete, import, export, or mutation members.

- [ ] **Step 5: Add the Principal query and replace local role inference**

Create `web/src/hooks/usePrincipal.ts`:

```ts
import { useQuery } from '@tanstack/react-query'
import { authApi } from '@/lib/api'
import type { Principal } from '@/lib/adminApi.types'

export const principalQueryKey = ['auth', 'principal'] as const

export function canWrite(principal: Principal | undefined): boolean {
  return principal?.can_write === true
}

export function usePrincipal(enabled = true) {
  const query = useQuery({
    queryKey: principalQueryKey,
    queryFn: async () => (await authApi.me()).data,
    enabled,
    retry: false,
    staleTime: 30_000,
  })
  return { ...query, principal: query.data, canWrite: canWrite(query.data) }
}
```

In `AdminApp.tsx`, call the hook unconditionally with `enabled: Boolean(token)`, show the existing full-height loading treatment while pending, and render the protected children only after `principal` exists:

```tsx
function RequireAuth({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const token = localStorage.getItem('token')
  const { principal, isPending, isError } = usePrincipal(Boolean(token))

  if (!token) return <Navigate to="/admin/login" state={{ from: location }} replace />
  if (isPending) return <div aria-busy="true" className="min-h-screen" />
  if (isError || !principal) return <div role="alert">Unable to load the authenticated user.</div>
  return <>{children}</>
}
```

In `MainLayout.tsx`, replace the localStorage parsing closure with `const { principal } = usePrincipal()`, then render `principal?.username` and `principal?.role`. Logout removes `token` and the Principal query; delete all reads/writes of the `user` localStorage key.

In `Login.tsx`, add `useQueryClient`, persist only `res.data.token`, remove the Principal cache, and navigate:

```ts
const queryClient = useQueryClient()

const response = await authApi.login({ username, password })
localStorage.setItem('token', response.data.token)
queryClient.removeQueries({ queryKey: principalQueryKey })
navigate('/admin', { replace: true })
```

- [ ] **Step 6: Consume canonical fields in the four affected pages**

Make these exact data-boundary changes; leave later visual/state redesign out of this task:

```ts
// Security.tsx
const params: SecurityQuery = { page, per_page: perPage }
if (ecosystem) params.ecosystem = ecosystem
if (severity) params.severity = severity as SecuritySeverity
if (search) params.package = search
const items = data?.data.items ?? []

type EditablePolicy = Pick<SecurityPolicy, 'auto_block_enabled' | 'min_cvss_score'>
const [localPolicies, setLocalPolicies] = useState<Record<string, EditablePolicy>>({})
const server = policies.find((policy) => policy.ecosystem === ecosystem)
return {
  auto_block_enabled: server?.auto_block_enabled ?? false,
  min_cvss_score: server?.min_cvss_score ?? 9.0,
}
```

Update Security toggle/number bindings and mutation payloads to those two canonical names. Do not send `q`, `auto_block`, or `cvss_threshold`.

```ts
// AccessLogs.tsx
const params: AccessLogQuery = { page, page_size: 50 }
const items: AccessLog[] = data?.data.items ?? []
const total = data?.data.total ?? 0
```

Replace the existing callback annotation `(row: any, i: number)` with `(row: AccessLog, i: number)`; keep its existing JSX cells unchanged because their property names already match `AccessLog`.

```ts
// AuditLogs.tsx
const params: AuditLogQuery = { page, page_size: 50, start: range.start, end: range.end }
if (appliedSearch) params.package = appliedSearch
const items = data?.data.items ?? []
// Render only the canonical value:
resultBadge(row.cache_result, t)
```

```ts
// Projects.tsx
const [selectedProject, setSelectedProject] = useState<ProjectSummary | null>(null)
const projects = data?.data.items ?? []
const projectDetail: ProjectDetail | ProjectSummary | null = detailData?.data ?? selectedProject
const packages = pkgData?.data.items ?? []
const proxyUrl = projectDetail && 'proxy_url' in projectDetail
  ? projectDetail.proxy_url
  : `${window.location.origin}/p/${projectDetail?.slug ?? ''}`

const pkgColumns = [
  { key: 'ecosystem', label: t('type') },
  { key: 'package_name', label: t('name') },
  { key: 'version', label: t('projects.version') },
  { key: 'first_seen_at', label: t('projects.firstSeen') },
  { key: 'last_seen_at', label: t('projects.lastSeen') },
  { key: 'download_count', label: t('projects.downloads') },
]
```

Use `last_activity_at` in the Project list. Every client fallback must contain `/p/`; remove both existing `/projects/` fallback strings.

In `Users.tsx`, use typed arrays and Principal capability:

```ts
const { principal, canWrite } = usePrincipal()
const users = usersData?.data ?? []
const tokens = tokensData?.data ?? []
const isSelf = (user: AdminUser) => user.id === principal?.id
```

Render add-user, generate-token, edit, disable/enable, and revoke actions only when `canWrite` is true. For the current user, keep Edit visible for password changes, hide disable, and render the role Select disabled so the UI does not offer self-demotion. The handler remains the final authorization boundary.

- [ ] **Step 7: Run the frontend contract gates**

Run: `cd web && npm run type-check && npm run build`

Expected: PASS. Then run:

```bash
rg -n "params\.q|params\.search = appliedSearch|auto_block\b|cvss_threshold\b|/projects/\$\{.*slug|localStorage\.(getItem|setItem)\('user'\)" web/src/admin web/src/lib
```

Expected: no matches in the affected Security, Audit, Projects, shell, or login paths.

- [ ] **Step 8: Commit the typed Principal foundation**

```bash
git add web/src/lib/adminApi.types.ts web/src/lib/adminApi.types.type-test.ts web/src/hooks/usePrincipal.ts web/src/admin/AdminApp.tsx web/src/admin/components/MainLayout.tsx web/src/admin/pages/Login.tsx web/src/admin/pages/Users.tsx web/src/admin/pages/Security.tsx web/src/admin/pages/AccessLogs.tsx web/src/admin/pages/AuditLogs.tsx web/src/admin/pages/Projects.tsx
git add -p web/src/lib/api.ts
git commit -m "fix(admin-ui): consume typed principal contracts"
```

---

## Final Verification

- [ ] Run `gofmt -w` only on Go files changed by these eight tasks, then run `git diff --check`.
- [ ] Run `go test -race ./internal/middleware ./internal/api/...` and expect PASS.
- [ ] Run `go test ./...` and expect PASS.
- [ ] Run `cd web && npm run type-check && npm run build` and expect PASS.
- [ ] Run `python3 scripts/i18n-audit.py` and expect equal English/Chinese leaf-key counts; this plan should not require copy changes.
- [ ] Run `git status --short`, verify unrelated starting changes remain intact, and verify each task commit contains only its declared files/hunks.
- [ ] Manually exercise four requests with a seeded test instance: readonly JWT `GET /admin/logs` is `200`; readonly JWT `POST /admin/security/scan` is `403`; admin/readwrite API token can perform the same POST; API token `POST /auth/refresh` is `401`.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-10-admin-remediation-01-contracts-permissions.md`. Two execution options:

1. **Subagent-Driven (recommended)** - dispatch a fresh subagent per task and review between tasks.
2. **Inline Execution** - use `superpowers:executing-plans` in this workspace with checkpoints after each task.
