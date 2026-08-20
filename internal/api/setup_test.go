package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func validSetupRequest(t *testing.T, root string) []byte {
	t.Helper()
	req := config.SetupRequest{
		Ecosystems: map[string]config.EcosystemSetup{
			"npm": {
				Enabled: true,
				Upstreams: []config.UpstreamSetup{{
					Name: "official", URL: "https://registry.npmjs.org", Priority: 1,
				}},
			},
		},
	}
	req.Server.Port = 23333
	req.Storage.Path = filepath.Join(root, "cache")
	req.Admin.Username = "operator"
	req.Admin.Password = "Tr0ub4dor&Correct"
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal setup request: %v", err)
	}
	return body
}

func decodeValidSetupRequest(t *testing.T, root string) config.SetupRequest {
	t.Helper()
	var req config.SetupRequest
	if err := json.Unmarshal(validSetupRequest(t, root), &req); err != nil {
		t.Fatalf("decode valid setup request: %v", err)
	}
	return req
}

func setupRequest(handler *SetupHandler, remoteAddr, token string, body []byte) *httptest.ResponseRecorder {
	router := gin.New()
	router.POST("/setup/complete", handler.Complete)
	req := httptest.NewRequest(http.MethodPost, "/setup/complete", bytes.NewReader(body))
	req.RemoteAddr = remoteAddr
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(BootstrapTokenHeader, token)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestHealthHandlerSupportsCrossOriginSetupProbe(t *testing.T) {
	router := gin.New()
	router.GET("/health", healthHandler)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://127.0.0.1:23333")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func newSetupTestHandler(t *testing.T) (*SetupHandler, *config.Config, string, *int) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{
		IsDefault:      true,
		ConfigPath:     filepath.Join(root, "private", "config.toml"),
		BootstrapToken: "bootstrap-token-0123456789abcdef",
	}
	database := newAPIAuthTestDB(t)
	if err := database.AutoMigrate(&db.ControlPlaneState{}); err != nil {
		t.Fatalf("migrate setup state: %v", err)
	}
	restarts := 0
	handler := NewSetupHandler(cfg, database)
	handler.scheduleRestart = func() { restarts++ }
	return handler, cfg, root, &restarts
}

func TestSetupCompleteRequiresBootstrapTokenFromRemotePeer(t *testing.T) {
	handler, _, root, restarts := newSetupTestHandler(t)
	recorder := setupRequest(handler, "203.0.113.10:4321", "", validSetupRequest(t, root))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if *restarts != 0 {
		t.Fatalf("restart count = %d, want 0", *restarts)
	}
}

func TestSetupCompleteCreatesAdminAndProtectedConfig(t *testing.T) {
	handler, cfg, root, restarts := newSetupTestHandler(t)
	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if cfg.IsDefault || cfg.BootstrapToken != "" {
		t.Fatalf("setup state was not consumed: %#v", cfg)
	}
	if *restarts != 1 {
		t.Fatalf("restart count = %d, want 1", *restarts)
	}
	var response struct {
		ReconnectURL    string `json:"reconnect_url"`
		RestartStrategy string `json:"restart_strategy"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode setup response: %v", err)
	}
	if response.ReconnectURL != "http://example.com:23333/" || response.RestartStrategy != setupRestartExec {
		t.Fatalf("setup response = %#v", response)
	}
	info, err := os.Stat(cfg.ConfigPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", info.Mode().Perm())
	}
	document, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(document), "Tr0ub4dor&Correct") || strings.Contains(string(document), "change-me-in-production") {
		t.Fatalf("generated config contains a forbidden credential: %s", document)
	}
	var user db.User
	if err := handler.db.Where("username = ?", "operator").First(&user).Error; err != nil {
		t.Fatalf("load administrator: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("Tr0ub4dor&Correct")); err != nil {
		t.Fatalf("administrator password mismatch: %v", err)
	}
	status, err := loadOnboardingStatus(t.Context(), handler.db)
	if err != nil || status != onboardingStatusNotStarted {
		t.Fatalf("fresh setup onboarding status = %q, err=%v, want not_started", status, err)
	}
}

func TestSetupReconnectURLUsesRequestedPortAndDirectTLS(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "https://[::1]:8443/api/v1/setup/complete", nil)
	if got := setupReconnectURL(request, 24444); got != "https://[::1]:24444/" {
		t.Fatalf("setupReconnectURL = %q", got)
	}
	if got := setupRestartStrategy("windows"); got != setupRestartSupervisorRequired {
		t.Fatalf("Windows restart strategy = %q", got)
	}
}

func TestSetupCompleteRequiresTokenOnLoopback(t *testing.T) {
	handler, _, root, restarts := newSetupTestHandler(t)
	recorder := setupRequest(handler, "127.0.0.1:4321", "", validSetupRequest(t, root))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if *restarts != 0 {
		t.Fatalf("restart count = %d, want 0", *restarts)
	}
}

func TestSetupCompleteCapsAndSanitizesInvalidRequestBody(t *testing.T) {
	handler, cfg, _, restarts := newSetupTestHandler(t)
	secret := "body-secret-must-not-be-echoed"
	body := []byte(`{"admin":{"password":"` + secret + `"},"padding":"` + strings.Repeat("x", int(maxSetupRequestBodyBytes)) + `"}`)
	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, body)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "INVALID_BODY") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), secret) {
		t.Fatalf("invalid-body response leaked request data: %s", recorder.Body.String())
	}
	if *restarts != 0 {
		t.Fatalf("restart count = %d, want 0", *restarts)
	}
}

func TestSetupCompleteRejectsWeakAdministratorPassword(t *testing.T) {
	handler, cfg, root, _ := newSetupTestHandler(t)
	var request map[string]any
	if err := json.Unmarshal(validSetupRequest(t, root), &request); err != nil {
		t.Fatal(err)
	}
	request["admin"].(map[string]any)["password"] = "admin"
	body, _ := json.Marshal(request)
	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, body)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "INVALID_ADMIN_CREDENTIALS") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestSetupCompleteRejectsInvalidConfigurationInputs(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*config.SetupRequest)
		code        string
		secretValue string
	}{
		{
			name: "unknown ecosystem even when disabled",
			mutate: func(req *config.SetupRequest) {
				req.Ecosystems["unknown"] = config.EcosystemSetup{}
			},
			code: "INVALID_ECOSYSTEM",
		},
		{
			name: "enabled package ecosystem without upstream",
			mutate: func(req *config.SetupRequest) {
				req.Ecosystems["npm"] = config.EcosystemSetup{Enabled: true}
			},
			code: "INVALID_UPSTREAM",
		},
		{
			name: "blank upstream name",
			mutate: func(req *config.SetupRequest) {
				setup := req.Ecosystems["npm"]
				setup.Upstreams[0].Name = " \t "
				req.Ecosystems["npm"] = setup
			},
			code: "INVALID_UPSTREAM",
		},
		{
			name: "duplicate normalized upstream name",
			mutate: func(req *config.SetupRequest) {
				setup := req.Ecosystems["npm"]
				setup.Upstreams = append(setup.Upstreams, config.UpstreamSetup{
					Name: " official ", URL: "https://mirror.example", Priority: 2,
				})
				req.Ecosystems["npm"] = setup
			},
			code: "INVALID_UPSTREAM",
		},
		{
			name: "non-positive priority",
			mutate: func(req *config.SetupRequest) {
				setup := req.Ecosystems["npm"]
				setup.Upstreams[0].Priority = 0
				req.Ecosystems["npm"] = setup
			},
			code: "INVALID_UPSTREAM",
		},
		{
			name: "non-http upstream URL",
			mutate: func(req *config.SetupRequest) {
				setup := req.Ecosystems["npm"]
				setup.Upstreams[0].URL = "file:///private/registry"
				req.Ecosystems["npm"] = setup
			},
			code: "INVALID_UPSTREAM",
		},
		{
			name: "non-http proxy URL",
			mutate: func(req *config.SetupRequest) {
				setup := req.Ecosystems["npm"]
				setup.Upstreams[0].Proxy = "ftp://proxy-password@example.com"
				req.Ecosystems["npm"] = setup
			},
			code:        "INVALID_UPSTREAM",
			secretValue: "proxy-password",
		},
		{
			name: "blank storage path",
			mutate: func(req *config.SetupRequest) {
				req.Storage.Path = " \t "
			},
			code: "INVALID_STORAGE_PATH",
		},
		{
			name: "storage path with control character",
			mutate: func(req *config.SetupRequest) {
				req.Storage.Path = "cache\nother"
			},
			code: "INVALID_STORAGE_PATH",
		},
		{
			name: "docker no-op setup",
			mutate: func(req *config.SetupRequest) {
				req.Ecosystems = map[string]config.EcosystemSetup{
					"docker": {Enabled: true},
				}
			},
			code: "INVALID_ECOSYSTEM",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, cfg, root, restarts := newSetupTestHandler(t)
			req := decodeValidSetupRequest(t, root)
			test.mutate(&req)
			body, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, body)
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), test.code) {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if test.secretValue != "" && strings.Contains(recorder.Body.String(), test.secretValue) {
				t.Fatalf("validation response leaked URL credentials: %s", recorder.Body.String())
			}
			if *restarts != 0 {
				t.Fatalf("restart count = %d, want 0", *restarts)
			}
			if _, err := os.Stat(cfg.ConfigPath); !os.IsNotExist(err) {
				t.Fatalf("config was written for invalid request: %v", err)
			}
		})
	}
}

func TestValidateAndNormalizeSetupRequest(t *testing.T) {
	root := t.TempDir()
	req := decodeValidSetupRequest(t, root)
	setup := req.Ecosystems["npm"]
	setup.Upstreams[0].Name = " official "
	setup.Upstreams[0].URL = " https://user:private-token@registry.example/path "
	setup.Upstreams[0].Proxy = " https://proxy.example:8443 "
	req.Ecosystems["npm"] = setup
	req.Ecosystems["apt"] = config.EcosystemSetup{
		Enabled: false,
		Upstreams: []config.UpstreamSetup{{
			Name: "ignored", URL: "not a URL", Priority: 0,
		}},
	}
	req.Storage.Path = " " + filepath.Join(root, "cache", "..", "cache") + " "

	if issue := validateAndNormalizeSetupRequest(&req); issue != nil {
		t.Fatalf("validation issue = %#v", issue)
	}
	got := req.Ecosystems["npm"].Upstreams[0]
	if got.Name != "official" || got.URL != "https://user:private-token@registry.example/path" || got.Proxy != "https://proxy.example:8443" {
		t.Fatalf("normalized upstream = %#v", got)
	}
	if req.Storage.Path != filepath.Join(root, "cache") {
		t.Fatalf("normalized storage path = %q", req.Storage.Path)
	}
	if req.Ecosystems["apt"].Upstreams != nil {
		t.Fatalf("disabled upstream defaults were retained: %#v", req.Ecosystems["apt"].Upstreams)
	}
}

func TestValidateSetupRejectsDockerNoOp(t *testing.T) {
	req := config.SetupRequest{Ecosystems: map[string]config.EcosystemSetup{
		"docker": {Enabled: true},
	}}
	req.Server.Port = 23333
	req.Storage.Path = t.TempDir()
	issue := validateAndNormalizeSetupRequest(&req)
	if issue == nil || issue.code != "INVALID_ECOSYSTEM" {
		t.Fatalf("validation issue = %#v, want INVALID_ECOSYSTEM", issue)
	}
}

func TestSetupRecoveryRequiresExistingAdministratorCredentials(t *testing.T) {
	handler, cfg, root, restarts := newSetupTestHandler(t)
	if err := CreateInitialAdmin(handler.db, "existing-admin", "Existing&Secure123"); err != nil {
		t.Fatal(err)
	}
	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "ADMIN_CREDENTIALS_MISMATCH") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(cfg.ConfigPath); !os.IsNotExist(err) {
		t.Fatalf("config was written despite credential mismatch: %v", err)
	}
	if *restarts != 0 {
		t.Fatalf("restart count = %d, want 0", *restarts)
	}
}

func TestSetupRecoveryDoesNotOverwriteExistingOnboardingState(t *testing.T) {
	handler, cfg, root, restarts := newSetupTestHandler(t)
	const username = "existing-admin"
	const password = "Existing&Secure123"
	if err := CreateInitialAdmin(handler.db, username, password); err != nil {
		t.Fatal(err)
	}
	if err := saveOnboardingStatus(t.Context(), handler.db, onboardingStatusSkipped); err != nil {
		t.Fatal(err)
	}
	request := decodeValidSetupRequest(t, root)
	request.Admin.Username = username
	request.Admin.Password = password
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}

	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if *restarts != 1 {
		t.Fatalf("restart count = %d, want 1", *restarts)
	}
	status, err := loadOnboardingStatus(t.Context(), handler.db)
	if err != nil || status != onboardingStatusSkipped {
		t.Fatalf("recovery onboarding status = %q, err=%v, want skipped", status, err)
	}
}

func TestSetupStatusDoesNotDiscloseTokenOrConfigPath(t *testing.T) {
	handler, cfg, _, _ := newSetupTestHandler(t)
	router := gin.New()
	router.GET("/setup/status", handler.Status)
	for _, test := range []struct {
		remote        string
		tokenRequired string
	}{
		{remote: "127.0.0.1:1234", tokenRequired: "true"},
		{remote: "203.0.113.10:1234", tokenRequired: "true"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/setup/status", nil)
		req.RemoteAddr = test.remote
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		body := recorder.Body.String()
		if !strings.Contains(body, `"token_required":`+test.tokenRequired) {
			t.Fatalf("status body for %s = %s", test.remote, body)
		}
		if strings.Contains(body, cfg.BootstrapToken) || strings.Contains(body, cfg.ConfigPath) {
			t.Fatalf("status leaked setup secrets: %s", body)
		}
	}
}
