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
	"depsilo/internal/packagepolicy"
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
		{name: "npm compound range", body: `{"ecosystem":"npm","package_name":"pkg","version":">= 1.0.0 < 2.0.0","action":"deny"}`},
		{name: "range on exact-only ecosystem", body: `{"ecosystem":"go","package_name":"example.com/pkg","version":">= 1.0.0","action":"deny"}`},
		{name: "APT exact version without request provenance", body: `{"ecosystem":"apt","package_name":"libc6","version":"1:2.36-9","action":"deny"}`},
		{name: "Composer exact version without authoritative dist provenance", body: `{"ecosystem":"composer","package_name":"vendor/pkg","version":"1.0.0","action":"deny"}`},
		{name: "RubyGems package rules unavailable", body: `{"ecosystem":"rubygems","package_name":"nokogiri","version":"*","action":"deny"}`},
		{name: "Helm package rules unavailable", body: `{"ecosystem":"helm","package_name":"my-chart","version":"*","action":"deny"}`},
		{name: "abbreviated Cargo semver", body: `{"ecosystem":"cargo","package_name":"pkg","version":"1.0","action":"deny"}`},
		{name: "ambiguous wildcard ecosystem", body: `{"ecosystem":"*","package_name":"pkg","version":"*","action":"deny"}`},
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

func TestRulesCreateAcceptsAuthenticatedNPMVersionSelectors(t *testing.T) {
	router, _ := newRulesAdminTestRouter(t)
	for _, version := range []string{"1.0.0", ">= 1.0.0"} {
		body := fmt.Sprintf(
			`{"ecosystem":"npm","package_name":"@Scope/pkg","version":%q,"action":"deny"}`,
			version,
		)
		response := rulesAdminRequest(router, http.MethodPost, "/rules", body)
		if response.Code != http.StatusCreated {
			t.Fatalf("npm selector %q status=%d body=%s", version, response.Code, response.Body.String())
		}
	}
}

func TestRulesCreateAcceptsPackageWideSelectorsWithoutVersionProvenance(t *testing.T) {
	router, _ := newRulesAdminTestRouter(t)
	for _, body := range []string{
		`{"ecosystem":"apt","package_name":"libc6","version":"*","action":"deny"}`,
		`{"ecosystem":"npm","package_name":"left-pad","version":"*","action":"deny"}`,
		`{"ecosystem":"composer","package_name":"vendor/package","version":"*","action":"deny"}`,
	} {
		response := rulesAdminRequest(router, http.MethodPost, "/rules", body)
		if response.Code != http.StatusCreated {
			t.Fatalf("package-wide create status = %d, body = %s", response.Code, response.Body.String())
		}
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
	for _, ecosystem := range []string{"unknown", "rubygems", "helm", "docker", "huggingface", "*"} {
		response := rulesAdminRequest(router, http.MethodPost, "/rules/test", `{"ecosystem":"`+ecosystem+`","package":"pkg","version":"1.0.0"}`)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("ecosystem %q status = %d, body = %s", ecosystem, response.Code, response.Body.String())
		}
	}
}

func TestRulesTestRejectsInvalidDialectPackageName(t *testing.T) {
	router, _ := newRulesAdminTestRouter(t)
	response := rulesAdminRequest(
		router,
		http.MethodPost,
		"/rules/test",
		`{"ecosystem":"cargo","package":"bad/name","version":"1.0.0"}`,
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid Cargo package status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestRulesTestExplainsCandidatesAndStableWinner(t *testing.T) {
	router, database := newRulesAdminTestRouter(t)
	seed := []db.PackageRule{
		{Ecosystem: "pypi", PackageName: "*", Version: "*", Action: "deny", Reason: "package baseline"},
		{Ecosystem: "pypi", PackageName: "requests", Version: ">= 1.0.0", Action: "allow", Reason: "approved releases"},
		{Ecosystem: "pypi", PackageName: "requests", Version: "1.0.0", Action: "deny", Reason: "incident pin"},
	}
	for index := range seed {
		if err := database.Create(&seed[index]).Error; err != nil {
			t.Fatalf("seed rule %d: %v", index, err)
		}
		// The admin fixture writes directly through GORM, so prepare the
		// dialect columns exactly as Store.Create would before the Engine reads
		// the row. This keeps the test focused on the explain response shape.
		prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
			Ecosystem: seed[index].Ecosystem, PackageName: seed[index].PackageName, Version: seed[index].Version,
		})
		if err != nil {
			t.Fatalf("prepare rule %d: %v", index, err)
		}
		if err := database.Model(&db.PackageRule{}).Where("id = ?", seed[index].ID).Updates(map[string]any{
			"ecosystem":               prepared.Ecosystem,
			"package_name":            prepared.PackageName,
			"version":                 prepared.Version,
			"normalized_package_name": prepared.NormalizedPackageName,
			"normalized_version":      prepared.NormalizedVersion,
			"dialect_revision":        prepared.DialectRevision,
			"action":                  seed[index].Action,
			"reason":                  seed[index].Reason,
		}).Error; err != nil {
			t.Fatalf("prepare persisted rule %d: %v", index, err)
		}
	}

	response := rulesAdminRequest(router, http.MethodPost, "/rules/test", `{"ecosystem":"pypi","package":"requests","version":"1.0.0"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("test status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Allowed      bool            `json:"allowed"`
		MatchedRule  *db.PackageRule `json:"matched_rule"`
		WinningRule  *db.PackageRule `json:"winning_rule"`
		Reason       string          `json:"reason"`
		WinnerReason string          `json:"winner_reason"`
		Precedence   string          `json:"precedence_reason"`
		Candidates   []struct {
			Rule        db.PackageRule        `json:"rule"`
			Specificity rules.RuleSpecificity `json:"specificity"`
			MatchLevels rules.RuleMatchLevels `json:"match_levels"`
			Matched     bool                  `json:"matched"`
			Selected    bool                  `json:"selected"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode test response: %v", err)
	}
	if payload.Allowed || payload.MatchedRule == nil || payload.WinningRule == nil {
		t.Fatalf("payload decision = allowed %v matched=%+v winning=%+v", payload.Allowed, payload.MatchedRule, payload.WinningRule)
	}
	if payload.MatchedRule.ID != seed[2].ID || payload.WinningRule.ID != seed[2].ID {
		t.Fatalf("winner IDs = matched %d winning %d, want %d", payload.MatchedRule.ID, payload.WinningRule.ID, seed[2].ID)
	}
	if payload.Reason != "incident pin" || payload.WinnerReason != "incident pin" {
		t.Fatalf("decision reasons = reason %q winner_reason %q", payload.Reason, payload.WinnerReason)
	}
	if payload.Precedence != "version_specificity" {
		t.Fatalf("precedence reason = %q, want version_specificity", payload.Precedence)
	}
	if len(payload.Candidates) != 3 || !payload.Candidates[0].Selected || !payload.Candidates[0].Matched {
		t.Fatalf("candidates = %+v, want winner-first selected list", payload.Candidates)
	}
	if payload.Candidates[0].Rule.ID != seed[2].ID || payload.Candidates[0].MatchLevels.Package != "exact" || payload.Candidates[0].MatchLevels.Version != "exact" {
		t.Fatalf("winner candidate = %+v, want exact package/version", payload.Candidates[0])
	}
	for index := 1; index < len(payload.Candidates); index++ {
		if payload.Candidates[index].Selected {
			t.Fatalf("candidate %d is unexpectedly selected", index)
		}
		if payload.Candidates[index-1].Specificity.Compare(payload.Candidates[index].Specificity) < 0 {
			t.Fatalf("candidate order is not descending at %d", index)
		}
	}
}

func TestRulesTestReturnsEmptyCandidateArrayOnNoMatch(t *testing.T) {
	router, _ := newRulesAdminTestRouter(t)
	response := rulesAdminRequest(router, http.MethodPost, "/rules/test", `{"ecosystem":"pypi","package":"unlisted","version":"1.0.0"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("no-match status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Allowed    bool             `json:"allowed"`
		Candidates []map[string]any `json:"candidates"`
		Matched    *db.PackageRule  `json:"matched_rule"`
		Precedence string           `json:"precedence_reason"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode no-match response: %v", err)
	}
	if !payload.Allowed || payload.Matched != nil || payload.Candidates == nil || len(payload.Candidates) != 0 || payload.Precedence != "default_allow" {
		t.Fatalf("no-match payload = %+v, want allow/null/empty array", payload)
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
