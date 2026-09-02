package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
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

// newPersistentSetupTestHandler builds the same directory layout that a real
// first-run process uses. The fault-boundary test closes this database and
// reloads config.toml through config.Load, so it cannot accidentally pass by
// carrying IsDefault or an in-memory GORM view across a simulated restart.
func newPersistentSetupTestHandler(t *testing.T) (*SetupHandler, *config.Config, string, *int) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create test home: %v", err)
	}
	t.Chdir(root)
	t.Setenv("HOME", home)
	// Keep the loader and the setup request on the same deterministic token,
	// while clearing every stateful override a developer shell may provide.
	for _, key := range []string{
		"DEPSILO_CONFIG",
		"DEPSILO_DATABASE_DSN",
		"DEPSILO_DATABASE_DRIVER",
		"DEPSILO_STORAGE_PATH",
		"DEPSILO_COMPILE_CACHE_STORAGE_PATH",
		"DEPSILO_AUTH_JWT_SECRET",
		"DEPSILO_SERVER_HOST",
		"DEPSILO_SERVER_PORT",
		"DEPSILO_ADMIN_USERNAME",
		"DEPSILO_ADMIN_PASSWORD",
	} {
		t.Setenv(key, "")
	}
	const bootstrapToken = "bootstrap-token-0123456789abcdef"
	t.Setenv("DEPSILO_BOOTSTRAP_TOKEN", bootstrapToken)
	configPath := filepath.Join(home, ".depsilo", "config.toml")
	databasePath := filepath.Join(home, ".depsilo", "data", "depsilo.db")
	database, err := db.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open persistent setup db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDatabase, dbErr := database.DB(); dbErr == nil {
			_ = sqlDatabase.Close()
		}
	})
	if err := database.AutoMigrate(&db.User{}, &db.APIToken{}, &db.ControlPlaneState{}); err != nil {
		t.Fatalf("migrate persistent setup db: %v", err)
	}
	cfg := &config.Config{
		IsDefault:      true,
		ConfigPath:     configPath,
		BootstrapToken: bootstrapToken,
		Server:         config.ServerConfig{Host: "127.0.0.1", Port: 23333},
		Database:       config.DatabaseConfig{Driver: "sqlite", DSN: databasePath},
		Auth:           config.AuthConfig{Enabled: true, JWTSecret: authTestJWTSecret, TokenTTL: time.Hour},
	}
	restarts := 0
	handler := NewSetupHandler(cfg, database)
	handler.scheduleRestart = func() { restarts++ }
	return handler, cfg, root, &restarts
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat %s: %v", path, err)
	return false
}

// restartSetupState closes the old SQL pool, reloads the on-disk config, and
// opens a fresh pool against the DSN selected by that config. It intentionally
// returns only durable state visible to a new process.
func restartSetupState(t *testing.T, database *gorm.DB) (*config.Config, *gorm.DB) {
	t.Helper()
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatalf("access setup db before restart: %v", err)
	}
	if err := sqlDatabase.Close(); err != nil {
		t.Fatalf("close setup db before restart: %v", err)
	}
	restartedCfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload config after restart: %v", err)
	}
	restartedDB, err := db.Open(restartedCfg.Database.Driver, restartedCfg.Database.DSN)
	if err != nil {
		t.Fatalf("reopen setup db after restart: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, dbErr := restartedDB.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	if err := restartedDB.AutoMigrate(&db.User{}, &db.APIToken{}, &db.ControlPlaneState{}); err != nil {
		t.Fatalf("migrate reopened setup db: %v", err)
	}
	return restartedCfg, restartedDB
}

type setupStatusResponse struct {
	NeedsSetup    bool `json:"needs_setup"`
	TokenRequired bool `json:"token_required"`
}

func setupStatus(t *testing.T, handler *SetupHandler) setupStatusResponse {
	t.Helper()
	router := gin.New()
	router.GET("/setup/status", handler.Status)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/setup/status", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("setup status code = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var status setupStatusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode setup status: %v", err)
	}
	return status
}

func loginRequestForSetup(t *testing.T, database *gorm.DB, cfg *config.Config) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	handler := NewAuthHandler(database, cfg.Auth)
	router.POST("/auth/login", handler.Login)
	return loginTestRequest(router, "127.0.0.1", "operator", "Tr0ub4dor&Correct")
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

func TestSetupFaultBoundariesAlwaysRestartIntoRecoveryOrLogin(t *testing.T) {
	type configExpectation uint8
	const (
		configMustBeAbsent configExpectation = iota
		configMustBePresent
		configMayBeEither
	)
	tests := []struct {
		name              string
		stage             setupStage
		configExpectation configExpectation
		wantAdmin         bool
	}{
		{name: "after administrator creation", stage: setupStageAfterAdminCreated},
		{name: "after onboarding save", stage: setupStageAfterOnboardingSaved},
		{name: "before database commit", stage: setupStageBeforeDBCommit},
		{name: "after database commit", stage: setupStageAfterDBCommit, wantAdmin: true},
		{name: "after temporary config write", stage: setupStageAfterConfigTempWrite, wantAdmin: true},
		{name: "after temporary config fsync", stage: setupStageAfterConfigTempSync, wantAdmin: true},
		{name: "before config rename", stage: setupStageBeforeConfigRename, wantAdmin: true},
		{name: "after config rename", stage: setupStageAfterConfigRename, configExpectation: configMayBeEither, wantAdmin: true},
		{name: "after config directory fsync", stage: setupStageAfterConfigDirSync, configExpectation: configMustBePresent, wantAdmin: true},
		{name: "after durable config write", stage: setupStageAfterConfigWrite, configExpectation: configMustBePresent, wantAdmin: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, cfg, root, restarts := newPersistentSetupTestHandler(t)
			crashErr := errors.New("injected process stop at " + string(test.stage))
			handler.stageHook = func(stage setupStage) error {
				if stage == test.stage {
					return crashErr
				}
				return nil
			}

			recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if *restarts != 0 {
				t.Fatalf("restart count = %d, want 0 after injected stop", *restarts)
			}
			configExists := fileExists(t, cfg.ConfigPath)
			// A real power loss immediately after rename can leave either the old
			// directory entry or the new one until the parent fsync is durable.
			// The subsequent branch checks both outcomes, so this boundary must not
			// turn that filesystem-level ambiguity into a false failure.
			switch test.configExpectation {
			case configMustBeAbsent:
				if configExists {
					t.Fatalf("config exists = true, want false")
				}
			case configMustBePresent:
				if !configExists {
					t.Fatalf("config exists = false, want true")
				}
			case configMayBeEither:
				// Both outcomes are handled by the restart branch below.
			default:
				t.Fatalf("unknown config expectation %d", test.configExpectation)
			}

			restartedCfg, restartedDB := restartSetupState(t, handler.db)
			var administrators int64
			if err := restartedDB.Model(&db.User{}).
				Where("username = ? AND role = ? AND enabled = ?", "operator", "admin", true).
				Count(&administrators).Error; err != nil {
				t.Fatal(err)
			}
			if got := administrators == 1; got != test.wantAdmin {
				t.Fatalf("operator administrator persisted = %t, want %t", got, test.wantAdmin)
			}
			var onboardingRows int64
			if err := restartedDB.Model(&db.ControlPlaneState{}).
				Where("key = ?", onboardingStatusStateKey).
				Count(&onboardingRows).Error; err != nil {
				t.Fatalf("count onboarding rows: %v", err)
			}
			if got := onboardingRows == 1; got != test.wantAdmin {
				t.Fatalf("onboarding state persisted = %t, want %t", got, test.wantAdmin)
			}

			status := setupStatus(t, NewSetupHandler(restartedCfg, restartedDB))
			if status.NeedsSetup {
				// Recovery path A: retrying the same request must create a rolled-back
				// administrator or verify the already committed one, then publish config.
				recovery := NewSetupHandler(restartedCfg, restartedDB)
				recovery.scheduleRestart = func() {}
				retry := setupRequest(
					recovery,
					"203.0.113.10:4321",
					restartedCfg.BootstrapToken,
					validSetupRequest(t, root),
				)
				if retry.Code != http.StatusOK {
					t.Fatalf("setup recovery status = %d, body = %s", retry.Code, retry.Body.String())
				}
				if err := VerifyExistingAdminCredentials(restartedDB, "operator", "Tr0ub4dor&Correct"); err != nil {
					t.Fatalf("recovered administrator credentials: %v", err)
				}
				return
			}

			// Recovery path B: when config is visible, the originally submitted
			// administrator credentials must already work through the login API.
			if login := loginRequestForSetup(t, restartedDB, restartedCfg); login.Code != http.StatusOK {
				t.Fatalf("login after restart status = %d, body = %s", login.Code, login.Body.String())
			}
		})
	}
}

func TestSetupStageForConfigWriteRejectsUnknownStage(t *testing.T) {
	if _, err := setupStageForConfigWrite(config.WriteStage(255)); err == nil {
		t.Fatal("unknown config publication stage was silently accepted")
	}
}

func TestSetupCommitFailureLeavesSetupRecoverable(t *testing.T) {
	handler, cfg, root, restarts := newPersistentSetupTestHandler(t)
	handler.commit = func(*gorm.DB) error {
		return errors.New("injected commit failure")
	}

	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if fileExists(t, cfg.ConfigPath) {
		t.Fatal("config published after failed database commit")
	}
	if *restarts != 0 {
		t.Fatalf("restart count = %d, want 0", *restarts)
	}

	restartedCfg, restartedDB := restartSetupState(t, handler.db)
	var administrators int64
	if err := restartedDB.Model(&db.User{}).Where("role = ? AND enabled = ?", "admin", true).Count(&administrators).Error; err != nil {
		t.Fatal(err)
	}
	if administrators != 0 {
		t.Fatalf("administrator count after failed commit = %d, want 0", administrators)
	}
	if status := setupStatus(t, NewSetupHandler(restartedCfg, restartedDB)); !status.NeedsSetup {
		t.Fatalf("setup status after commit failure = %#v, want pending", status)
	}
	recovery := NewSetupHandler(restartedCfg, restartedDB)
	recovery.scheduleRestart = func() {}
	retry := setupRequest(recovery, "203.0.113.10:4321", restartedCfg.BootstrapToken, validSetupRequest(t, root))
	if retry.Code != http.StatusOK {
		t.Fatalf("recovery status = %d, body = %s", retry.Code, retry.Body.String())
	}
}

func TestSetupPersistsEffectiveDatabaseForCustomConfigPath(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	t.Setenv("HOME", home)
	configPath := filepath.Join(root, "state", "depsilo-config")
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_DATABASE_DSN", "")
	t.Setenv("DEPSILO_STORAGE_PATH", "")
	t.Setenv("DEPSILO_COMPILE_CACHE_STORAGE_PATH", "")
	t.Setenv("DEPSILO_SERVER_HOST", "")
	t.Setenv("DEPSILO_SERVER_PORT", "")
	t.Setenv("DEPSILO_BOOTSTRAP_TOKEN", "custom-path-bootstrap-token-012345")
	t.Setenv("DEPSILO_ADMIN_USERNAME", "")
	t.Setenv("DEPSILO_ADMIN_PASSWORD", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load missing custom config: %v", err)
	}
	database, err := db.Open(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	if err := database.AutoMigrate(&db.User{}, &db.APIToken{}, &db.ControlPlaneState{}); err != nil {
		t.Fatal(err)
	}
	handler := NewSetupHandler(cfg, database)
	handler.scheduleRestart = func() {}
	request := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	if request.Code != http.StatusOK {
		t.Fatalf("setup status = %d, body = %s", request.Code, request.Body.String())
	}

	restartedCfg, restartedDB := restartSetupState(t, database)
	if restartedCfg.IsDefault {
		t.Fatal("custom config remained in first-run state after setup")
	}
	if got, want := restartedCfg.Database.DSN, cfg.Database.DSN; got != want {
		t.Fatalf("restarted database DSN = %q, want committed DSN %q", got, want)
	}
	if err := VerifyExistingAdminCredentials(restartedDB, "operator", "Tr0ub4dor&Correct"); err != nil {
		t.Fatalf("administrator in effective database: %v", err)
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
