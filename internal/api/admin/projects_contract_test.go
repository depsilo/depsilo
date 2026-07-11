package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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
	r.GET("/projects/:id", h.Detail)
	r.PUT("/projects/:id", h.Update)
	r.DELETE("/projects/:id", h.Delete)
	r.GET("/projects/:id/packages", h.ListPackages)
	r.GET("/projects/:id/sbom", h.ExportSBOM)
	r.POST("/projects/:id/regenerate-token", h.RegenerateToken)
	return r, database
}

func createContractProject(t *testing.T, database *gorm.DB) db.Project {
	t.Helper()
	project := db.Project{Name: "AI Platform", Slug: "ai-platform", Description: "training", TokenHash: "original-hash", CreatedBy: "admin"}
	if err := database.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	return project
}

func injectGORMFailure(t *testing.T, database *gorm.DB, operation, table string, occurrence int) {
	t.Helper()
	injected := errors.New("injected database failure")
	callbackName := "test:inject_" + operation + "_failure"
	seen := 0
	callback := func(tx *gorm.DB) {
		if tx.Statement.Table != table {
			return
		}
		seen++
		if seen == occurrence {
			tx.AddError(injected)
		}
	}

	switch operation {
	case "query":
		if err := database.Callback().Query().Before("gorm:query").Register(callbackName, callback); err != nil {
			t.Fatalf("register query callback: %v", err)
		}
		t.Cleanup(func() { _ = database.Callback().Query().Remove(callbackName) })
	case "update":
		if err := database.Callback().Update().Before("gorm:update").Register(callbackName, callback); err != nil {
			t.Fatalf("register update callback: %v", err)
		}
		t.Cleanup(func() { _ = database.Callback().Update().Remove(callbackName) })
	case "delete":
		if err := database.Callback().Delete().Before("gorm:delete").Register(callbackName, callback); err != nil {
			t.Fatalf("register delete callback: %v", err)
		}
		t.Cleanup(func() { _ = database.Callback().Delete().Remove(callbackName) })
	default:
		t.Fatalf("unsupported callback operation %q", operation)
	}
}

func assertDBError(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body["code"] != "DB_ERROR" {
		t.Fatalf("body = %#v", body)
	}
}

func assertNotFound(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode not-found response: %v", err)
	}
	if body["code"] != "NOT_FOUND" {
		t.Fatalf("body = %#v", body)
	}
}

type failingCommitPool struct {
	gorm.ConnPool
	beginner interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	}
}

func (p failingCommitPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	pool, err := p.beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &failingCommitTx{ConnPool: pool, TxCommitter: pool}, nil
}

type failingCommitTx struct {
	gorm.ConnPool
	gorm.TxCommitter
}

func (tx failingCommitTx) Commit() error {
	return errors.New("injected commit failure")
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

func TestProjectLookupsDistinguishNotFoundFromDatabaseFailure(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "detail", method: http.MethodGet, path: "/projects/1"},
		{name: "update", method: http.MethodPut, path: "/projects/1", body: `{"description":"changed"}`},
		{name: "delete", method: http.MethodDelete, path: "/projects/1"},
		{name: "packages", method: http.MethodGet, path: "/projects/1/packages"},
		{name: "export", method: http.MethodGet, path: "/projects/1/sbom"},
		{name: "regenerate", method: http.MethodPost, path: "/projects/1/regenerate-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, database := newProjectsContractRouter(t)
			injectGORMFailure(t, database, "query", "projects", 1)
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			assertDBError(t, rec)
		})
	}
}

func TestProjectLookupsReturnNotFoundOnlyForMissingProjects(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "detail", method: http.MethodGet, path: "/projects/999"},
		{name: "update", method: http.MethodPut, path: "/projects/999", body: `{"description":"changed"}`},
		{name: "delete", method: http.MethodDelete, path: "/projects/999"},
		{name: "packages", method: http.MethodGet, path: "/projects/999/packages"},
		{name: "export", method: http.MethodGet, path: "/projects/999/sbom"},
		{name: "regenerate", method: http.MethodPost, path: "/projects/999/regenerate-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := newProjectsContractRouter(t)
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			assertNotFound(t, rec)
		})
	}
}

func TestCreateReturnsDatabaseFailureFromSlugLookup(t *testing.T) {
	r, database := newProjectsContractRouter(t)
	injectGORMFailure(t, database, "query", "projects", 1)
	req := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewBufferString(`{"name":"AI Platform"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assertDBError(t, rec)

	var count int64
	if err := database.Session(&gorm.Session{SkipHooks: true}).Model(&db.Project{}).Count(&count).Error; err != nil {
		t.Fatalf("count projects: %v", err)
	}
	if count != 0 {
		t.Fatalf("created %d projects after failed slug lookup", count)
	}
}

func TestProjectDetailChecksStatisticsQueries(t *testing.T) {
	tests := []struct {
		name       string
		occurrence int
	}{
		{name: "count", occurrence: 1},
		{name: "breakdown", occurrence: 2},
		{name: "latest", occurrence: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, database := newProjectsContractRouter(t)
			project := createContractProject(t, database)
			injectGORMFailure(t, database, "query", "project_packages", tt.occurrence)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/projects/"+jsonNumber(project.ID), nil))
			assertDBError(t, rec)
		})
	}
}

func TestListProjectPackagesChecksCountAndFind(t *testing.T) {
	for _, occurrence := range []int{1, 2} {
		t.Run(strconv.Itoa(occurrence), func(t *testing.T) {
			r, database := newProjectsContractRouter(t)
			project := createContractProject(t, database)
			injectGORMFailure(t, database, "query", "project_packages", occurrence)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/projects/"+jsonNumber(project.ID)+"/packages", nil))
			assertDBError(t, rec)
		})
	}
}

func TestExportProjectSBOMChecksPackageQuery(t *testing.T) {
	r, database := newProjectsContractRouter(t)
	project := createContractProject(t, database)
	injectGORMFailure(t, database, "query", "project_packages", 1)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/projects/"+jsonNumber(project.ID)+"/sbom", nil))
	assertDBError(t, rec)
}

func TestRegenerateProjectTokenReturnsTokenOnlyAfterUpdate(t *testing.T) {
	r, database := newProjectsContractRouter(t)
	project := createContractProject(t, database)
	injectGORMFailure(t, database, "update", "projects", 1)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/projects/"+jsonNumber(project.ID)+"/regenerate-token", nil))
	assertDBError(t, rec)
	if bytes.Contains(rec.Body.Bytes(), []byte(`"token"`)) {
		t.Fatalf("failed response leaked token: %s", rec.Body.String())
	}

	var stored db.Project
	if err := database.First(&stored, project.ID).Error; err != nil {
		t.Fatalf("reload project: %v", err)
	}
	if stored.TokenHash != project.TokenHash {
		t.Fatalf("token hash changed after failed update: %q", stored.TokenHash)
	}
}

func TestDeleteProjectRollsBackDeleteFailure(t *testing.T) {
	for _, table := range []string{"project_packages", "projects"} {
		t.Run(table, func(t *testing.T) {
			r, database := newProjectsContractRouter(t)
			project := createContractProject(t, database)
			pkg := db.ProjectPackage{ProjectID: project.ID, Ecosystem: "pypi", PackageName: "requests", Version: "1"}
			if err := database.Create(&pkg).Error; err != nil {
				t.Fatalf("create package: %v", err)
			}
			injectGORMFailure(t, database, "delete", table, 1)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/projects/"+jsonNumber(project.ID), nil))
			assertDBError(t, rec)
			assertProjectAndPackageExist(t, database, project.ID, pkg.ID)
		})
	}
}

func TestDeleteProjectChecksCommitFailure(t *testing.T) {
	r, database := newProjectsContractRouter(t)
	project := createContractProject(t, database)
	pkg := db.ProjectPackage{ProjectID: project.ID, Ecosystem: "pypi", PackageName: "requests", Version: "1"}
	if err := database.Create(&pkg).Error; err != nil {
		t.Fatalf("create package: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	failingDB := database.Session(&gorm.Session{NewDB: true})
	failingDB.Statement.ConnPool = failingCommitPool{ConnPool: sqlDB, beginner: sqlDB}
	h := NewProjectsHandler(failingDB)
	r = gin.New()
	r.DELETE("/projects/:id", h.Delete)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/projects/"+jsonNumber(project.ID), nil))
	assertDBError(t, rec)
	assertProjectAndPackageExist(t, database, project.ID, pkg.ID)
}

func assertProjectAndPackageExist(t *testing.T, database *gorm.DB, projectID, packageID uint) {
	t.Helper()
	if err := database.First(&db.Project{}, projectID).Error; err != nil {
		t.Fatalf("project was not preserved: %v", err)
	}
	if err := database.First(&db.ProjectPackage{}, packageID).Error; err != nil {
		t.Fatalf("package was not preserved: %v", err)
	}
}

func jsonNumber(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}
