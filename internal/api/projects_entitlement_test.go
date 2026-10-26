package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/entitlement"
	"depsilo/internal/license"
	"depsilo/internal/middleware"
	"depsilo/internal/trial"
)

type projectEntitlementFixture struct {
	database *gorm.DB
	router   *gin.Engine
	checker  *entitlement.Checker
	admin    string
	readonly string
}

func newProjectEntitlementFixture(t *testing.T, grant string) projectEntitlementFixture {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "projects.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.Logger = logger.Default.LogMode(logger.Silent)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.AutoMigrate(&db.User{}, &db.APIToken{}, &db.Project{}, &db.ProjectPackage{}, &db.TrialRecord{}, &db.AuditLog{}, &db.LicenseStorage{}, &db.TamperRecord{}); err != nil {
		t.Fatal(err)
	}
	fixture := projectEntitlementFixture{database: database}
	for _, role := range []string{"admin", "readonly"} {
		user := db.User{Username: role, PasswordHash: "unused", Role: role, Enabled: true}
		if err := database.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
		token, err := middleware.GenerateJWT(authTestJWTSecret, user, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if role == "admin" {
			fixture.admin = token
		} else {
			fixture.readonly = token
		}
	}
	if grant == "trial" || grant == "expired" {
		expiry := time.Now().Add(time.Hour)
		if grant == "expired" {
			expiry = time.Now().Add(-time.Hour)
		}
		if err := database.Create(&db.TrialRecord{ActivatedAt: expiry.Add(-trial.TrialDuration), ExpiresAt: expiry, ActivatedBy: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	trialManager, err := trial.NewManager(database)
	if err != nil {
		t.Fatal(err)
	}
	licenseManager := license.NewManager(config.LicenseConfig{}, database)
	if grant == "paid" {
		if err := licenseManager.SetKey(context.Background(), "project-test-license", 1); err != nil {
			t.Fatal(err)
		}
	}
	fixture.checker = entitlement.NewChecker(licenseManager, trialManager)
	fixture.router = newProjectEntitlementRouter(t, database, fixture.checker)
	return fixture
}

func newProjectEntitlementRouter(t *testing.T, database *gorm.DB, checker *entitlement.Checker) *gin.Engine {
	t.Helper()
	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: authTestJWTSecret, TokenTTL: time.Hour}}
	router := gin.New()
	RegisterRoutes(router, Deps{
		LifecycleContext: context.Background(), DB: database, Config: cfg,
		ConfigStore: config.NewStore(filepath.Join(t.TempDir(), "config.toml"), cfg, zap.NewAtomicLevel()),
		EventBus:    cache.NewEventBus(), Entitlement: checker,
	})
	return router
}

func projectEntitlementRequest(router *gin.Engine, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/admin/projects"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func requireProjectResponse(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("response = %d %s; want %d", response.Code, response.Body.String(), status)
	}
}

func assertProjectCapabilities(t *testing.T, fixture projectEntitlementFixture, total int, limit *int, canCreate, canExport bool) {
	t.Helper()
	response := projectEntitlementRequest(fixture.router, http.MethodGet, "", "", fixture.admin)
	requireProjectResponse(t, response, http.StatusOK)
	var body struct {
		Total        int `json:"total"`
		Capabilities *struct {
			ProjectLimit  *int `json:"project_limit"`
			CanCreate     bool `json:"can_create"`
			CanExportSBOM bool `json:"can_export_sbom"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != total || body.Capabilities == nil || body.Capabilities.CanCreate != canCreate || body.Capabilities.CanExportSBOM != canExport {
		t.Fatalf("unexpected project capabilities: %s", response.Body.String())
	}
	gotLimit := body.Capabilities.ProjectLimit
	if (limit == nil) != (gotLimit == nil) || (limit != nil && *limit != *gotLimit) {
		t.Fatalf("unexpected project limit: %s", response.Body.String())
	}
}

func TestFreeProjectLifecycleAndRoutePermissions(t *testing.T) {
	fixture := newProjectEntitlementFixture(t, "free")
	limit := 1
	assertProjectCapabilities(t, fixture, 0, &limit, true, false)
	created := projectEntitlementRequest(fixture.router, http.MethodPost, "", `{"name":"First project"}`, fixture.admin)
	requireProjectResponse(t, created, http.StatusCreated)
	var project struct {
		ID        uint      `json:"id"`
		Token     string    `json:"token"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	if project.ID == 0 || !strings.HasPrefix(project.Token, "depsilo_proj_") || project.CreatedAt.IsZero() {
		t.Fatalf("invalid creation response: %s", created.Body.String())
	}
	path := fmt.Sprintf("/%d", project.ID)
	assertProjectCapabilities(t, fixture, 1, &limit, false, false)
	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "", ""}, {http.MethodGet, path, ""}, {http.MethodGet, path + "/packages", ""},
		{http.MethodGet, path + "/sbom", ""}, {http.MethodPost, "", `{"name":"Second"}`},
		{http.MethodPut, path, `{"description":"changed"}`}, {http.MethodDelete, path, ""}, {http.MethodPost, path + "/token", ""},
	} {
		requireProjectResponse(t, projectEntitlementRequest(fixture.router, route.method, route.path, route.body, ""), http.StatusUnauthorized)
		want := http.StatusForbidden
		if route.method == http.MethodGet {
			want = http.StatusOK
			if strings.HasSuffix(route.path, "/sbom") {
				want = http.StatusPaymentRequired
			}
		}
		requireProjectResponse(t, projectEntitlementRequest(fixture.router, route.method, route.path, route.body, fixture.readonly), want)
	}
	second := projectEntitlementRequest(fixture.router, http.MethodPost, "", `{"name":"Second"}`, fixture.admin)
	requireProjectResponse(t, second, http.StatusPaymentRequired)
	if !strings.Contains(second.Body.String(), `"code":"PRO_REQUIRED"`) || !strings.Contains(second.Body.String(), `"trial_available":true`) || strings.Contains(second.Body.String(), `"token":`) {
		t.Fatalf("unexpected quota response: %s", second.Body.String())
	}
	requireProjectResponse(t, projectEntitlementRequest(fixture.router, http.MethodPut, path, `{"description":"changed"}`, fixture.admin), http.StatusOK)
	requireProjectResponse(t, projectEntitlementRequest(fixture.router, http.MethodPost, path+"/token", "", fixture.admin), http.StatusOK)
	requireProjectResponse(t, projectEntitlementRequest(fixture.router, http.MethodGet, path+"/sbom", "", fixture.admin), http.StatusPaymentRequired)
	requireProjectResponse(t, projectEntitlementRequest(fixture.router, http.MethodDelete, path, "", fixture.admin), http.StatusOK)
	assertProjectCapabilities(t, fixture, 0, &limit, true, false)
	requireProjectResponse(t, projectEntitlementRequest(fixture.router, http.MethodPost, "", `{"name":"Replacement"}`, fixture.admin), http.StatusCreated)
}

func TestProjectEntitlementGrantsAndExpiry(t *testing.T) {
	for _, grant := range []string{"paid", "trial", "expired"} {
		t.Run(grant, func(t *testing.T) {
			fixture := newProjectEntitlementFixture(t, grant)
			if grant == "expired" {
				// Model projects retained from a formerly active trial.
				for i := range 2 {
					project := db.Project{Name: fmt.Sprintf("Existing %d", i), Slug: fmt.Sprintf("existing-%d", i), TokenHash: fmt.Sprintf("hash-%d", i)}
					if err := fixture.database.Create(&project).Error; err != nil {
						t.Fatal(err)
					}
				}
				limit := 1
				assertProjectCapabilities(t, fixture, 2, &limit, false, false)
				for _, path := range []string{"/1", "/2", "/1/packages", "/2/packages"} {
					requireProjectResponse(t, projectEntitlementRequest(fixture.router, http.MethodGet, path, "", fixture.admin), http.StatusOK)
				}
				requireProjectResponse(t, projectEntitlementRequest(fixture.router, http.MethodPut, "/2", `{"description":"still manageable"}`, fixture.admin), http.StatusOK)
				requireProjectResponse(t, projectEntitlementRequest(fixture.router, http.MethodPost, "/2/token", "", fixture.admin), http.StatusOK)
				response := projectEntitlementRequest(fixture.router, http.MethodPost, "", `{"name":"Another"}`, fixture.admin)
				requireProjectResponse(t, response, http.StatusPaymentRequired)
				if !strings.Contains(response.Body.String(), `"trial_available":false`) {
					t.Fatalf("expired trial offered again: %s", response.Body.String())
				}
				requireProjectResponse(t, projectEntitlementRequest(fixture.router, http.MethodGet, "/2/sbom", "", fixture.admin), http.StatusPaymentRequired)
				return
			}
			for i := range 3 {
				requireProjectResponse(t, projectEntitlementRequest(fixture.router, http.MethodPost, "", fmt.Sprintf(`{"name":"Project %d"}`, i), fixture.admin), http.StatusCreated)
			}
			assertProjectCapabilities(t, fixture, 3, nil, true, true)
			requireProjectResponse(t, projectEntitlementRequest(fixture.router, http.MethodGet, "/1/sbom", "", fixture.admin), http.StatusOK)
		})
	}
}

func TestFreeProjectConcurrentCreationCannotExceedAllowance(t *testing.T) {
	fixture := newProjectEntitlementFixture(t, "free")
	// Two independent handlers sharing the multi-connection SQLite pool prove
	// that a handler-local mutex cannot stand in for durable quota enforcement.
	routers := []*gin.Engine{fixture.router, newProjectEntitlementRouter(t, fixture.database, fixture.checker)}
	const requests = 12
	responses := make(chan *httptest.ResponseRecorder, requests)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := range requests {
		workers.Go(func() {
			<-start
			responses <- projectEntitlementRequest(routers[i%len(routers)], http.MethodPost, "", fmt.Sprintf(`{"name":"Concurrent %d"}`, i), fixture.admin)
		})
	}
	close(start)
	workers.Wait()
	close(responses)
	created := 0
	for response := range responses {
		if response.Code == http.StatusCreated {
			created++
		} else {
			requireProjectResponse(t, response, http.StatusPaymentRequired)
		}
	}
	if created != 1 {
		t.Fatalf("created %d projects concurrently; want 1", created)
	}
	limit := 1
	assertProjectCapabilities(t, fixture, 1, &limit, false, false)
}

func TestFreeProjectFailedWriteDoesNotConsumeAllowanceOrReturnToken(t *testing.T) {
	fixture := newProjectEntitlementFixture(t, "free")
	if err := fixture.database.Exec(`CREATE TRIGGER reject_project BEFORE INSERT ON projects
		BEGIN SELECT RAISE(ABORT, 'injected write failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	response := projectEntitlementRequest(fixture.router, http.MethodPost, "", `{"name":"Rejected"}`, fixture.admin)
	requireProjectResponse(t, response, http.StatusInternalServerError)
	if strings.Contains(response.Body.String(), `"token":`) || strings.Contains(response.Body.String(), "PRO_REQUIRED") {
		t.Fatalf("failed write returned a token or a quota error: %s", response.Body.String())
	}
	limit := 1
	assertProjectCapabilities(t, fixture, 0, &limit, true, false)
	if err := fixture.database.Exec("DROP TRIGGER reject_project").Error; err != nil {
		t.Fatal(err)
	}
	requireProjectResponse(t, projectEntitlementRequest(fixture.router, http.MethodPost, "", `{"name":"Accepted"}`, fixture.admin), http.StatusCreated)
}
