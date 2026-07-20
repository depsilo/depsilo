package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
	"depsilo/internal/middleware"
	"depsilo/internal/rules"
)

func TestRulesCreateUsesTypedValidatedInput(t *testing.T) {
	router, database := newRulesAdminTestRouter(t)

	response := rulesAdminRequest(router, http.MethodPost, "/rules", `{
		"ecosystem":" PyPI ","package_name":" requests ","version":"",
		"action":" DENY ","reason":" upgrade required "
	}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("valid create status = %d, body = %s", response.Code, response.Body.String())
	}
	var created db.PackageRule
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created rule: %v", err)
	}
	if created.Ecosystem != "pypi" || created.PackageName != "requests" || created.Version != "*" || created.Action != "deny" || created.Reason != "upgrade required" || created.CreatedBy != "operator" {
		t.Fatalf("created rule = %#v", created)
	}

	invalid := []struct {
		name string
		body string
	}{
		{name: "unknown ecosystem", body: `{"ecosystem":"unknown","package_name":"pkg","version":"*","action":"deny"}`},
		{name: "invalid action", body: `{"ecosystem":"pypi","package_name":"pkg","version":"*","action":"drop"}`},
		{name: "middle wildcard", body: `{"ecosystem":"pypi","package_name":"pkg*tail","version":"*","action":"deny"}`},
		{name: "unsupported comparison", body: `{"ecosystem":"pypi","package_name":"pkg","version":"~= 1.0","action":"deny"}`},
		{name: "database id field", body: `{"ecosystem":"pypi","package_name":"pkg","version":"*","action":"deny","id":999}`},
		{name: "created by field", body: `{"ecosystem":"pypi","package_name":"pkg","version":"*","action":"deny","created_by":"attacker"}`},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			response := rulesAdminRequest(router, http.MethodPost, "/rules", test.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}

	var count int64
	if err := database.Model(&db.PackageRule{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rule count = %d, want only the valid rule", count)
	}
}

func TestRulesUpdateWhitelistsFieldsAndDistinguishesNotFound(t *testing.T) {
	router, database := newRulesAdminTestRouter(t)
	rule := db.PackageRule{Ecosystem: "npm", PackageName: "left-pad", Version: "*", Action: "deny", CreatedBy: "original"}
	if err := database.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}

	response := rulesAdminRequest(router, http.MethodPut, "/rules/"+uintString(rule.ID), `{"action":"allow","id":999}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("internal-field update status = %d, body = %s", response.Code, response.Body.String())
	}
	if err := database.First(&rule, rule.ID).Error; err != nil {
		t.Fatal(err)
	}
	if rule.Action != "deny" || rule.CreatedBy != "original" {
		t.Fatalf("rejected update changed rule: %#v", rule)
	}

	response = rulesAdminRequest(router, http.MethodPut, "/rules/"+uintString(rule.ID), `{"action":" ALLOW ","reason":" reviewed "}`)
	if response.Code != http.StatusOK {
		t.Fatalf("valid update status = %d, body = %s", response.Code, response.Body.String())
	}
	if err := database.First(&rule, rule.ID).Error; err != nil {
		t.Fatal(err)
	}
	if rule.Action != "allow" || rule.Reason != "reviewed" || rule.CreatedBy != "original" {
		t.Fatalf("updated rule = %#v", rule)
	}

	response = rulesAdminRequest(router, http.MethodPut, "/rules/99999", `{"action":"deny"}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing update status = %d, body = %s", response.Code, response.Body.String())
	}
	response = rulesAdminRequest(router, http.MethodDelete, "/rules/99999", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing delete status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestRulesTestRejectsNonEnforceableEcosystem(t *testing.T) {
	router, _ := newRulesAdminTestRouter(t)
	for _, ecosystem := range []string{"unknown", "docker", "huggingface", "*"} {
		response := rulesAdminRequest(router, http.MethodPost, "/rules/test", `{"ecosystem":"`+ecosystem+`","package":"pkg","version":"1.0.0"}`)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("ecosystem %q status = %d, body = %s", ecosystem, response.Code, response.Body.String())
		}
	}
}

func newRulesAdminTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "admin-rules.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open rules database: %v", err)
	}
	if err := database.AutoMigrate(&db.PackageRule{}); err != nil {
		t.Fatalf("migrate rules database: %v", err)
	}
	store := rules.NewStore(database)
	handler := NewRulesHandler(store, rules.NewEngine(store, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyPrincipal, middleware.Principal{ID: 7, Username: "operator", Role: "admin", Enabled: true, CanWrite: true})
		c.Next()
	})
	router.POST("/rules", handler.Create)
	router.PUT("/rules/:id", handler.Update)
	router.DELETE("/rules/:id", handler.Delete)
	router.POST("/rules/test", handler.Test)
	return router, database
}

func rulesAdminRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func uintString(value uint) string {
	return fmt.Sprintf("%d", value)
}
