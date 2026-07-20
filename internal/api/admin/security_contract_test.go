package admin

import (
	"bytes"
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
	"depsilo/internal/security"
)

func newSecurityContractRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	return newSecurityContractRouterWithInvalidator(t, nil)
}

func newSecurityContractRouterWithInvalidator(
	t *testing.T,
	invalidateRules func(),
) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "security.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(
		&db.Vulnerability{},
		&db.VulnerabilityCheck{},
		&db.SecurityPolicy{},
		&db.DismissedVuln{},
		&db.PackageRule{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := NewSecurityHandler(database, security.NewScanner(database, nil, nil), nil, invalidateRules)
	r := gin.New()
	r.GET("/security/dashboard", h.Dashboard)
	r.GET("/security/vulnerabilities", h.ListVulnerabilities)
	r.GET("/security/packages", h.ListPackages)
	r.GET("/security/suggestions", h.ListSuggestions)
	r.POST("/security/suggestions/:vuln_id/approve", h.ApproveSuggestion)
	r.GET("/security/policies", h.ListPolicies)
	r.PUT("/security/policies/:ecosystem", h.UpdatePolicy)
	return r, database
}

func TestSecurityApproveSuggestionInvalidatesRulesAfterCreate(t *testing.T) {
	invalidations := 0
	r, database := newSecurityContractRouterWithInvalidator(t, func() { invalidations++ })
	vulnerability := db.Vulnerability{
		OSVID:       "OSV-APPROVE-CACHE",
		Ecosystem:   "pypi",
		PackageName: "approved-package",
		CVSSScore:   8.4,
	}
	if err := database.Create(&vulnerability).Error; err != nil {
		t.Fatalf("seed vulnerability: %v", err)
	}

	recorder := httptest.NewRecorder()
	path := "/security/suggestions/" + strconv.FormatUint(uint64(vulnerability.ID), 10) + "/approve"
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("approve status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if invalidations != 1 {
		t.Fatalf("rule cache invalidations = %d, want 1", invalidations)
	}
}

func TestSecurityApproveSuggestionDoesNotInvalidateWhenCreateFails(t *testing.T) {
	invalidations := 0
	r, database := newSecurityContractRouterWithInvalidator(t, func() { invalidations++ })
	vulnerability := db.Vulnerability{
		OSVID:       "OSV-APPROVE-FAILURE",
		Ecosystem:   "pypi",
		PackageName: "failed-package",
		CVSSScore:   8.4,
	}
	if err := database.Create(&vulnerability).Error; err != nil {
		t.Fatalf("seed vulnerability: %v", err)
	}
	injected := errors.New("injected package rule create failure")
	const callbackName = "test:fail_security_package_rule_create"
	if err := database.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "package_rules" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatalf("register create callback: %v", err)
	}
	t.Cleanup(func() { _ = database.Callback().Create().Remove(callbackName) })

	recorder := httptest.NewRecorder()
	path := "/security/suggestions/" + strconv.FormatUint(uint64(vulnerability.ID), 10) + "/approve"
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("approve status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if invalidations != 0 {
		t.Fatalf("rule cache invalidations = %d, want 0", invalidations)
	}
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

	rec := httptest.NewRecorder()
	path := "/security/vulnerabilities?package=&q=requests"
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, body = %s", path, rec.Code, rec.Body.String())
	}
	var body struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if body.Total != 2 || len(body.Items) != 2 {
		t.Fatalf("GET %s applied q despite package presence: %#v", path, body)
	}
}

func TestSecurityPolicyCanonicalFieldsAndCVSSRange(t *testing.T) {
	r, database := newSecurityContractRouter(t)
	for _, score := range []string{"-0.1", "10.1", "10.0000001", "1e309"} {
		t.Run("reject "+score, func(t *testing.T) {
			payload := []byte(`{"auto_block_enabled":true,"min_cvss_score":` + score + `}`)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/security/policies/pypi", bytes.NewReader(payload)))
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("score %s status = %d, body = %s", score, rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if body["code"] != "INVALID_POLICY" || body["message"] != "min_cvss_score must be between 0 and 10" {
				t.Fatalf("score %s body = %#v", score, body)
			}
		})
	}

	for _, score := range []string{"0", "10"} {
		payload := []byte(`{"auto_block_enabled":true,"min_cvss_score":` + score + `}`)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/security/policies/pypi", bytes.NewReader(payload)))
		if rec.Code != http.StatusOK {
			t.Fatalf("boundary score %s status = %d, body = %s", score, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	quotedNumber := []byte(`{"auto_block_enabled":true,"min_cvss_score":"7.5"}`)
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/security/policies/pypi", bytes.NewReader(quotedNumber)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("quoted score status = %d, body = %s", rec.Code, rec.Body.String())
	}

	payload := []byte(`{"auto_block_enabled":true,"min_cvss_score":7.5}`)
	rec = httptest.NewRecorder()
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

func assertExactJSONKeys(t *testing.T, body map[string]any, want ...string) {
	t.Helper()
	if len(body) != len(want) {
		t.Fatalf("keys = %#v, want exactly %#v", body, want)
	}
	for _, key := range want {
		if _, ok := body[key]; !ok {
			t.Fatalf("missing key %q in %#v", key, body)
		}
	}
}

func decodeJSONMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

func firstPageItem(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v", body["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item = %#v", items[0])
	}
	return item
}

func TestSecuritySuccessfulResponsesHaveExactSchemas(t *testing.T) {
	r, database := newSecurityContractRouter(t)
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	vuln := db.Vulnerability{
		OSVID: "OSV-REQUESTS", Ecosystem: "pypi", PackageName: "requests", AffectedRanges: ">=1",
		Severity: "high", CVSSScore: 8.1, Summary: "summary", Details: "details", Aliases: "alias",
		References: "reference", PublishedAt: now, ModifiedAt: now,
	}
	if err := database.Create(&vuln).Error; err != nil {
		t.Fatalf("seed vulnerability: %v", err)
	}
	check := db.VulnerabilityCheck{
		Ecosystem: "pypi", PackageName: "requests", HasVulnerabilities: true,
		VulnerabilityCount: 1, LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
	}
	if err := database.Create(&check).Error; err != nil {
		t.Fatalf("seed vulnerability check: %v", err)
	}
	policy := db.SecurityPolicy{Ecosystem: "pypi", AutoBlockEnabled: true, MinCVSSScore: 8, CreatedBy: "admin"}
	if err := database.Create(&policy).Error; err != nil {
		t.Fatalf("seed policy: %v", err)
	}

	dashboardRec := httptest.NewRecorder()
	r.ServeHTTP(dashboardRec, httptest.NewRequest(http.MethodGet, "/security/dashboard", nil))
	dashboard := decodeJSONMap(t, dashboardRec)
	assertExactJSONKeys(t, dashboard,
		"total_vulnerabilities", "affected_packages", "by_severity", "auto_blocked_count", "last_scan_at", "scan_in_progress",
	)
	bySeverity, ok := dashboard["by_severity"].(map[string]any)
	if !ok {
		t.Fatalf("by_severity = %#v", dashboard["by_severity"])
	}
	assertExactJSONKeys(t, bySeverity, "critical", "high", "medium", "low")

	vulnerabilityKeys := []string{
		"id", "osv_id", "ecosystem", "package_name", "affected_ranges", "severity", "cvss_score", "summary",
		"details", "aliases", "references", "published_at", "modified_at", "created_at", "updated_at",
	}
	for _, path := range []string{"/security/vulnerabilities", "/security/suggestions"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		body := decodeJSONMap(t, rec)
		assertExactJSONKeys(t, body, "items", "total", "page")
		assertExactJSONKeys(t, firstPageItem(t, body), vulnerabilityKeys...)
	}

	packagesRec := httptest.NewRecorder()
	r.ServeHTTP(packagesRec, httptest.NewRequest(http.MethodGet, "/security/packages", nil))
	packages := decodeJSONMap(t, packagesRec)
	assertExactJSONKeys(t, packages, "items", "total", "page")
	assertExactJSONKeys(t, firstPageItem(t, packages),
		"id", "ecosystem", "package_name", "has_vulnerabilities", "vulnerability_count",
		"last_fetched_at", "next_fetch_at", "created_at", "updated_at",
	)

	policiesRec := httptest.NewRecorder()
	r.ServeHTTP(policiesRec, httptest.NewRequest(http.MethodGet, "/security/policies", nil))
	if policiesRec.Code != http.StatusOK {
		t.Fatalf("policy list status = %d, body = %s", policiesRec.Code, policiesRec.Body.String())
	}
	var policies []map[string]any
	if err := json.Unmarshal(policiesRec.Body.Bytes(), &policies); err != nil || len(policies) != 1 {
		t.Fatalf("decode policies: policies=%#v err=%v", policies, err)
	}
	policyKeys := []string{"id", "ecosystem", "auto_block_enabled", "min_cvss_score", "created_by", "created_at", "updated_at"}
	assertExactJSONKeys(t, policies[0], policyKeys...)

	updateRec := httptest.NewRecorder()
	updatePayload := bytes.NewBufferString(`{"auto_block_enabled":false,"min_cvss_score":7.5}`)
	r.ServeHTTP(updateRec, httptest.NewRequest(http.MethodPut, "/security/policies/pypi", updatePayload))
	assertExactJSONKeys(t, decodeJSONMap(t, updateRec), policyKeys...)
}

func TestSecurityListEndpointsPropagateDatabaseErrors(t *testing.T) {
	r, database := newSecurityContractRouter(t)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	for _, path := range []string{
		"/security/vulnerabilities",
		"/security/packages",
		"/security/suggestions",
		"/security/policies",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("GET %s status = %d, body = %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestSecurityListsCheckCountAndFetchErrors(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		table      string
		occurrence int
	}{
		{name: "vulnerabilities count", path: "/security/vulnerabilities", table: "vulnerabilities", occurrence: 1},
		{name: "vulnerabilities fetch", path: "/security/vulnerabilities", table: "vulnerabilities", occurrence: 2},
		{name: "packages count", path: "/security/packages", table: "vulnerability_checks", occurrence: 1},
		{name: "packages fetch", path: "/security/packages", table: "vulnerability_checks", occurrence: 2},
		{name: "suggestions count", path: "/security/suggestions", table: "vulnerabilities", occurrence: 1},
		{name: "suggestions fetch", path: "/security/suggestions", table: "vulnerabilities", occurrence: 2},
		{name: "policies fetch", path: "/security/policies", table: "security_policies", occurrence: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, database := newSecurityContractRouter(t)
			injectGORMFailure(t, database, "query", tt.table, tt.occurrence)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			assertDBError(t, rec)
		})
	}
}

func TestSecurityDashboardChecksAggregateQueries(t *testing.T) {
	tests := []struct {
		name       string
		table      string
		occurrence int
	}{
		{name: "vulnerability total", table: "vulnerabilities", occurrence: 1},
		{name: "affected packages", table: "vulnerability_checks", occurrence: 1},
		{name: "severity breakdown", table: "vulnerabilities", occurrence: 2},
		{name: "auto blocked total", table: "package_rules", occurrence: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, database := newSecurityContractRouter(t)
			injectGORMFailure(t, database, "query", tt.table, tt.occurrence)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/security/dashboard", nil))
			assertDBError(t, rec)
		})
	}
}

func TestSecurityVulnerabilityLookupDistinguishesNotFoundFromDatabaseFailure(t *testing.T) {
	t.Run("database failure", func(t *testing.T) {
		r, database := newSecurityContractRouter(t)
		injectGORMFailure(t, database, "query", "vulnerabilities", 1)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/security/suggestions/1/approve", nil))
		assertDBError(t, rec)
	})

	t.Run("not found", func(t *testing.T) {
		r, _ := newSecurityContractRouter(t)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/security/suggestions/999/approve", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})
}

func TestSecurityPolicyUpdatePropagatesDatabaseErrors(t *testing.T) {
	r, database := newSecurityContractRouter(t)
	policy := db.SecurityPolicy{Ecosystem: "pypi", AutoBlockEnabled: false, MinCVSSScore: 8, CreatedBy: "admin"}
	if err := database.Create(&policy).Error; err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	injectGORMFailure(t, database, "update", "security_policies", 1)

	rec := httptest.NewRecorder()
	payload := bytes.NewBufferString(`{"auto_block_enabled":true,"min_cvss_score":7.5}`)
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/security/policies/pypi", payload))
	assertDBError(t, rec)
}
