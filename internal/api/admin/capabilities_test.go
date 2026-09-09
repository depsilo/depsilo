package admin

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/rules"
)

type capabilityPolicyStub struct{ status rules.PolicyStatus }

func (s capabilityPolicyStub) PolicyStatus() rules.PolicyStatus { return s.status }

func TestCapabilitySummaryKeepsSupportModeAndDataIndependent(t *testing.T) {
	database, err := db.Open("sqlite", "file:capabilities?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.VulnerabilityCheck{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := database.Create(&db.VulnerabilityCheck{Ecosystem: "pypi", PackageName: "requests", LastFetchedAt: now, NextFetchAt: now.Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Security: config.SecurityConfig{Enabled: true}}
	provider := capabilityPolicyStub{status: rules.PolicyStatus{LastSuccessfulRefresh: now}}
	h := NewCapabilityHandler(database, cfg, nil, []string{"pypi", "docker"}, provider, nil, "warn")
	r := gin.New()
	r.GET("/summary", h.Summary)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest("GET", "/summary", nil))
	if response.Code != 200 {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var got capabilitySummary
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version == "" {
		t.Fatal("version missing")
	}
	var pypiVuln, dockerBlock capabilityFact
	for _, fact := range got.Capabilities {
		if fact.Name == "vulnerability_scanning" && fact.Ecosystem == "pypi" {
			pypiVuln = fact
		}
		if fact.Name == "malicious_blocklist" && fact.Ecosystem == "docker" {
			dockerBlock = fact
		}
	}
	if pypiVuln.Support != "supported" || pypiVuln.Mode != "alert_only" || pypiVuln.DataStatus != "fresh" {
		t.Fatalf("pypi vulnerability fact = %+v", pypiVuln)
	}
	if dockerBlock.Support != "unsupported" || dockerBlock.Mode != "off" {
		t.Fatalf("docker blocklist fact = %+v", dockerBlock)
	}
}

func TestCapabilitySummaryDoesNotRefreshPolicy(t *testing.T) {
	called := false
	provider := policyCallback{fn: func() { called = true }}
	h := NewCapabilityHandler(nil, &config.Config{}, nil, []string{"pypi"}, provider, nil, "")
	r := gin.New()
	r.GET("/summary", h.Summary)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest("GET", "/summary", nil))
	if response.Code != 200 || !called {
		t.Fatalf("status=%d called=%v", response.Code, called)
	}
}

type policyCallback struct{ fn func() }

func (p policyCallback) PolicyStatus() rules.PolicyStatus { p.fn(); return rules.PolicyStatus{} }
