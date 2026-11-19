package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"depsilo/internal/api/contract"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
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
	if err := config.WriteBootstrapToken(cfg.ConfigPath, cfg.BootstrapToken); err != nil {
		t.Fatalf("publish setup test token: %v", err)
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
	// Initialize signing state before opening SQLite, matching server startup.
	// Read-only reloads after a fault then see the original durable key.
	cfg, err := config.LoadForServer()
	if err != nil {
		t.Fatalf("load persistent setup config: %v", err)
	}
	if err := config.WriteBootstrapToken(cfg.ConfigPath, cfg.BootstrapToken); err != nil {
		t.Fatalf("publish persistent bootstrap token: %v", err)
	}
	database, err := db.Open(cfg.Database.Driver, cfg.Database.DSN)
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
	restartedCfg, err := config.LoadForServer()
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

// The 403 must tell a scripted or remote operator what to do next, and it must
// not reveal whether a token was absent, from an earlier start, or simply wrong.
func TestSetupCompleteExplainsBootstrapTokenWithoutDistinguishingRejections(t *testing.T) {
	handler, cfg, root, restarts := newSetupTestHandler(t)
	stubRunningInContainer(t, false)
	stale, err := config.NewSecureToken()
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, candidate := range []string{"", stale, cfg.BootstrapToken + "x", "wrong"} {
		recorder := setupRequest(handler, "203.0.113.10:4321", candidate, validSetupRequest(t, root))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("token %q: status = %d, body = %s", candidate, recorder.Code, recorder.Body.String())
		}
		bodies = append(bodies, recorder.Body.String())
	}
	for _, body := range bodies[1:] {
		if body != bodies[0] {
			t.Fatalf("rejections are distinguishable:\n%s\n%s", bodies[0], body)
		}
	}
	var response struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "BOOTSTRAP_TOKEN_REQUIRED" {
		t.Fatalf("code = %q, want BOOTSTRAP_TOKEN_REQUIRED", response.Code)
	}
	for _, fragment := range []string{
		"A valid bootstrap token is required to complete initial setup",
		"X-Depsilo-Bootstrap-Token header",
		"changes every time Depsilo restarts",
		`"depsilo setup token"`,
	} {
		if !strings.Contains(response.Message, fragment) {
			t.Errorf("message %q missing %q", response.Message, fragment)
		}
	}
	if strings.Contains(bodies[0], cfg.BootstrapToken) {
		t.Fatal("rejection disclosed the bootstrap token")
	}
	if *restarts != 0 {
		t.Fatalf("restart count = %d, want 0", *restarts)
	}

	// The message names the command that reaches this listener, in the docker
	// exec form inside a container. SetupTokenCommand's own tests below hold
	// the flag rules; this proves the message passes it the listener and the
	// container state.
	cfg.Server.Port = 24646
	cfg.Server.Host = "::1"
	stubRunningInContainer(t, true)
	recorder := setupRequest(handler, "203.0.113.10:4321", "", validSetupRequest(t, root))
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Message, `run "docker exec <container> depsilo setup token --port 24646 --host ::1" on the server`) {
		t.Fatalf("rejection in a container = %s", response.Message)
	}
}

// stubRunningInContainer makes the process look as if it runs, or does not
// run, in a container for the rest of the test.
func stubRunningInContainer(t *testing.T, inContainer bool) {
	t.Helper()
	previous := runningInContainer
	runningInContainer = func() bool { return inContainer }
	t.Cleanup(func() { runningInContainer = previous })
}

// "depsilo setup token" asks 127.0.0.1:23333 unless told otherwise, so the
// command names a port and a host only when that default does not reach the
// listener.
func TestSetupTokenCommandNamesOnlyWhatItsDefaultsDoNotReach(t *testing.T) {
	for _, test := range []struct {
		host string
		port int
		want string
	}{
		{"127.0.0.1", 23333, "depsilo setup token"},
		{"127.0.0.1", 0, "depsilo setup token"},
		{"127.0.0.1", 24646, "depsilo setup token --port 24646"},
		// Hosts 127.0.0.1 reaches: no host, the wildcards, the loopback names.
		{"", 23333, "depsilo setup token"},
		{"0.0.0.0", 23333, "depsilo setup token"},
		{"::", 23333, "depsilo setup token"},
		{"[::]", 24646, "depsilo setup token --port 24646"},
		{"localhost", 23333, "depsilo setup token"},
		{"LocalHost", 23333, "depsilo setup token"},
		// Hosts it does not reach, IPv6 without the brackets the flag does not need.
		{"::1", 23333, "depsilo setup token --host ::1"},
		{"[::1]", 24646, "depsilo setup token --port 24646 --host ::1"},
		{" ::1 ", 23333, "depsilo setup token --host ::1"},
		{"192.0.2.10", 23333, "depsilo setup token --host 192.0.2.10"},
		{"2001:db8::10", 24646, "depsilo setup token --port 24646 --host 2001:db8::10"},
		{"depsilo.internal", 23333, "depsilo setup token --host depsilo.internal"},
		{"fe80::1%en0", 23333, "depsilo setup token --host fe80::1%en0"},
		// A zone is free text: anything a shell would read is quoted.
		{"::1%$(id)", 23333, "depsilo setup token --host '::1%$(id)'"},
		{"::1%a'b", 23333, `depsilo setup token --host '::1%a'\''b'`},
	} {
		if got := SetupTokenCommand(test.host, test.port, false); got != test.want {
			t.Errorf("SetupTokenCommand(%q, %d) = %q, want %q", test.host, test.port, got, test.want)
		}
	}
}

// In a container "depsilo setup token" runs only inside it, so the command
// takes the docker exec form with the flags the bare command would get.
func TestSetupTokenCommandInAContainerRunsThroughDockerExec(t *testing.T) {
	for _, test := range []struct {
		host string
		port int
		want string
	}{
		{"0.0.0.0", 23333, "docker exec <container> depsilo setup token"},
		{"0.0.0.0", 24646, "docker exec <container> depsilo setup token --port 24646"},
		{"[::1]", 24646, "docker exec <container> depsilo setup token --port 24646 --host ::1"},
	} {
		if got := SetupTokenCommand(test.host, test.port, true); got != test.want {
			t.Errorf("SetupTokenCommand(%q, %d, true) = %q, want %q", test.host, test.port, got, test.want)
		}
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
	if _, err := config.ReadBootstrapToken(cfg.ConfigPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed setup retained its retrieval token: %v", err)
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
	// httptest addresses the request to "example.com" without a port, so it
	// did not name the listening port and the client returns to its own origin.
	if response.ReconnectURL != "http://example.com/" || response.RestartStrategy != string(contract.SetupRestartStrategyExec) {
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

func TestSetupPreservesSigningKeyAcrossRestart(t *testing.T) {
	for _, managed := range []bool{false, true} {
		name := "explicit environment key"
		if managed {
			name = "managed instance key"
		}
		t.Run(name, func(t *testing.T) {
			handler, cfg, root, restarts := newPersistentSetupTestHandler(t)
			originalSecret := cfg.Auth.JWTSecret
			if managed {
				cfg.ManagedJWTSecretPath = filepath.Join(filepath.Dir(cfg.ConfigPath), "data", "secrets", "jwt.key")
				if err := os.MkdirAll(filepath.Dir(cfg.ManagedJWTSecretPath), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cfg.ManagedJWTSecretPath, []byte(originalSecret), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				cfg.ManagedJWTSecretPath = ""
				t.Setenv("DEPSILO_AUTH_JWT_SECRET", originalSecret)
			}
			// Hidden fields must remain startup-owned even when a caller adds an
			// auth object that is not part of the setup browser contract.
			var body map[string]any
			if err := json.Unmarshal(validSetupRequest(t, root), &body); err != nil {
				t.Fatal(err)
			}
			body["auth"] = map[string]any{
				"jwt_secret": "attacker-key",
				"JWTSecret":  "attacker-key",
				"managed":    !managed,
				"Managed":    !managed,
			}
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, encoded)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if *restarts != 1 {
				t.Fatalf("restart count = %d, want 1", *restarts)
			}
			if strings.Contains(recorder.Body.String(), originalSecret) {
				t.Fatal("setup response leaked the effective signing key")
			}
			document, err := os.ReadFile(cfg.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if managed && strings.Contains(string(document), "jwt_secret") {
				t.Fatal("setup copied the managed signing key into config.toml")
			}
			// Explicit overrides may be removed after setup. The restart must
			// continue verifying already issued tokens with the original key.
			t.Setenv("DEPSILO_AUTH_JWT_SECRET", "")
			restartedCfg, restartedDB := restartSetupState(t, handler.db)
			if restartedCfg.Auth.JWTSecret != originalSecret {
				t.Fatal("setup changed the signing key after restart")
			}
			if managed && restartedCfg.ManagedJWTSecretPath != cfg.ManagedJWTSecretPath {
				t.Fatal("setup changed the managed signing-key authority")
			}
			if login := loginRequestForSetup(t, restartedDB, restartedCfg); login.Code != http.StatusOK {
				t.Fatalf("login after restart status = %d, body = %s", login.Code, login.Body.String())
			}
		})
	}
}

func TestSetupWriteFailureDoesNotExposeInternalError(t *testing.T) {
	handler, cfg, root, _ := newSetupTestHandler(t)
	const privateError = "secret-bearing internal failure"
	handler.writeConfig = func(string, config.SetupRequest, config.WriteStageHook) error {
		return errors.New(privateError)
	}
	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "WRITE_FAILED") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), privateError) {
		t.Fatal("setup write failure exposed an internal error")
	}
	if token, err := config.ReadBootstrapToken(cfg.ConfigPath); err != nil || token != cfg.BootstrapToken {
		t.Fatalf("failed setup lost its retrieval token: %v", err)
	}
}

const reopenedSetupDocument = "# written by hand\n[cache]\nmax_size_gb = 7\n"

// reopenSetupWithExistingConfig models a restart that found config.toml but no
// loginable administrator: setup is pending while the operator's file exists.
func reopenSetupWithExistingConfig(t *testing.T, cfg *config.Config) {
	t.Helper()
	if err := os.WriteFile(cfg.ConfigPath, []byte(reopenedSetupDocument), 0o644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}
}

func setupConfigBackups(t *testing.T, configPath string) []string {
	t.Helper()
	backups, err := filepath.Glob(configPath + ".pre-setup-*.bak")
	if err != nil {
		t.Fatalf("list config backups: %v", err)
	}
	return backups
}

// observeConfigBackupLogs captures the info-level entries that tell the
// operator where setup kept the previous config.toml.
func observeConfigBackupLogs(t *testing.T) func() []observer.LoggedEntry {
	t.Helper()
	observed, logs := observer.New(zap.InfoLevel)
	t.Cleanup(zap.ReplaceGlobals(zap.New(observed)))
	return func() []observer.LoggedEntry {
		return logs.FilterLevelExact(zap.InfoLevel).FilterFieldKey("backup_path").All()
	}
}

func TestSetupReopenedKeepsPreviousConfigAsPrivateBackup(t *testing.T) {
	handler, cfg, root, _ := newSetupTestHandler(t)
	reopenSetupWithExistingConfig(t, cfg)
	backupLogs := observeConfigBackupLogs(t)

	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	backups := setupConfigBackups(t, cfg.ConfigPath)
	if len(backups) != 1 {
		t.Fatalf("config backups = %v, want exactly one", backups)
	}
	if name := filepath.Base(backups[0]); !regexp.MustCompile(`^config\.toml\.pre-setup-\d{8}T\d{6}Z\.bak$`).MatchString(name) {
		t.Fatalf("backup name = %q, want config.toml.pre-setup-<UTC timestamp>.bak", name)
	}
	kept, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != reopenedSetupDocument {
		t.Fatalf("backup content = %q, want the previous config", kept)
	}
	info, err := os.Stat(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %o, want 600", info.Mode().Perm())
	}
	generated, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(generated) == reopenedSetupDocument || !strings.Contains(string(generated), "[server]") {
		t.Fatalf("setup did not publish the generated config: %s", generated)
	}
	if strings.Contains(recorder.Body.String(), filepath.Base(backups[0])) {
		t.Fatalf("setup response disclosed the backup path: %s", recorder.Body.String())
	}
	// The log is the operator's only record of where the previous file went.
	entries := backupLogs()
	if len(entries) != 1 {
		t.Fatalf("info entries naming the backup = %d, want exactly one", len(entries))
	}
	if logged := entries[0].ContextMap()["backup_path"]; logged != backups[0] {
		t.Fatalf("logged backup_path = %v, want %q", logged, backups[0])
	}
}

// A completed setup that cannot remove its credential still succeeds: the
// HTTP gate is closed and the next start retries. The warning names the
// credential the way every other message does.
func TestSetupCompleteWarnsWhenTheBootstrapTokenCannotBeRemoved(t *testing.T) {
	handler, cfg, root, _ := newSetupTestHandler(t)
	observed, logs := observer.New(zap.WarnLevel)
	t.Cleanup(zap.ReplaceGlobals(zap.New(observed)))
	tokenPath, err := config.BootstrapTokenPath(cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	// A directory in the token's place cannot be removed as a token.
	if err := os.Remove(tokenPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(tokenPath, 0o700); err != nil {
		t.Fatal(err)
	}

	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	if recorder.Code != http.StatusOK || cfg.IsDefault {
		t.Fatalf("status = %d, IsDefault = %v, body = %s", recorder.Code, cfg.IsDefault, recorder.Body.String())
	}
	warnings := logs.FilterLevelExact(zap.WarnLevel).All()
	if len(warnings) != 1 || warnings[0].Message != "failed to remove completed bootstrap token" {
		t.Fatalf("warnings = %#v, want the one about the bootstrap token", warnings)
	}
}

func TestSetupFirstRunKeepsNoConfigBackup(t *testing.T) {
	handler, cfg, root, _ := newSetupTestHandler(t)
	backupLogs := observeConfigBackupLogs(t)
	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if backups := setupConfigBackups(t, cfg.ConfigPath); len(backups) != 0 {
		t.Fatalf("first run kept config backups %v, want none", backups)
	}
	if entries := backupLogs(); len(entries) != 0 {
		t.Fatalf("first run logged %d config backups, want none", len(entries))
	}
}

func TestSetupBackupFailureKeepsExistingConfigAndAllowsRetry(t *testing.T) {
	handler, cfg, root, restarts := newSetupTestHandler(t)
	reopenSetupWithExistingConfig(t, cfg)
	const privateError = "secret-bearing backup failure"
	keepConfig := handler.backupConfig
	handler.backupConfig = func(string) (string, error) {
		return "", errors.New(privateError)
	}

	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "WRITE_FAILED") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), privateError) {
		t.Fatal("setup backup failure exposed an internal error")
	}
	existing, err := os.ReadFile(cfg.ConfigPath)
	if err != nil || string(existing) != reopenedSetupDocument {
		t.Fatalf("config after failed backup = %q, %v; want the previous config untouched", existing, err)
	}
	if !cfg.IsDefault || *restarts != 0 {
		t.Fatalf("failed backup closed setup: pending=%t restarts=%d", cfg.IsDefault, *restarts)
	}
	if token, err := config.ReadBootstrapToken(cfg.ConfigPath); err != nil || token != cfg.BootstrapToken {
		t.Fatalf("failed backup lost the retrieval token: %v", err)
	}
	// The administrator is already durable, so the operator retries with the
	// same credentials once the directory accepts the copy.
	if err := VerifyExistingAdminCredentials(handler.db, "operator", "Tr0ub4dor&Correct"); err != nil {
		t.Fatalf("administrator after failed backup: %v", err)
	}

	handler.backupConfig = keepConfig
	retry := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	if retry.Code != http.StatusOK {
		t.Fatalf("retry status = %d, body = %s", retry.Code, retry.Body.String())
	}
	backups := setupConfigBackups(t, cfg.ConfigPath)
	if len(backups) != 1 {
		t.Fatalf("config backups after retry = %v, want exactly one", backups)
	}
	if kept, err := os.ReadFile(backups[0]); err != nil || string(kept) != reopenedSetupDocument {
		t.Fatalf("backup after retry = %q, %v; want the previous config", kept, err)
	}
}

// A reopened setup may stop at any publication boundary. The operator's file
// must survive every one of them: untouched in place until the rename, and as
// exactly one backup from the moment the copy is made.
func TestSetupReopenedFaultBoundariesNeverLoseThePreviousConfig(t *testing.T) {
	tests := []struct {
		name            string
		stage           setupStage
		wantBackup      bool
		configUntouched bool
	}{
		{name: "after database commit", stage: setupStageAfterDBCommit, configUntouched: true},
		{name: "after config backup", stage: setupStageAfterConfigBackup, wantBackup: true, configUntouched: true},
		{name: "after temporary config write", stage: setupStageAfterConfigTempWrite, wantBackup: true, configUntouched: true},
		{name: "after temporary config fsync", stage: setupStageAfterConfigTempSync, wantBackup: true, configUntouched: true},
		{name: "before config rename", stage: setupStageBeforeConfigRename, wantBackup: true, configUntouched: true},
		{name: "after config rename", stage: setupStageAfterConfigRename, wantBackup: true},
		{name: "after config directory fsync", stage: setupStageAfterConfigDirSync, wantBackup: true},
		{name: "after durable config write", stage: setupStageAfterConfigWrite, wantBackup: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, cfg, root, restarts := newSetupTestHandler(t)
			reopenSetupWithExistingConfig(t, cfg)
			stopped := false
			handler.stageHook = func(stage setupStage) error {
				if stage == test.stage && !stopped {
					stopped = true
					return errors.New("injected process stop at " + string(stage))
				}
				return nil
			}

			recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
			if recorder.Code != http.StatusInternalServerError || *restarts != 0 {
				t.Fatalf("status = %d, restarts = %d, body = %s", recorder.Code, *restarts, recorder.Body.String())
			}
			backups := setupConfigBackups(t, cfg.ConfigPath)
			if got := len(backups) == 1; got != test.wantBackup || len(backups) > 1 {
				t.Fatalf("config backups = %v, want backup = %t", backups, test.wantBackup)
			}
			if test.wantBackup {
				if kept, err := os.ReadFile(backups[0]); err != nil || string(kept) != reopenedSetupDocument {
					t.Fatalf("backup = %q, %v; want the previous config", kept, err)
				}
			}
			current, err := os.ReadFile(cfg.ConfigPath)
			if err != nil {
				t.Fatalf("config.toml disappeared at the boundary: %v", err)
			}
			if got := string(current) == reopenedSetupDocument; got != test.configUntouched {
				t.Fatalf("config untouched = %t, want %t", got, test.configUntouched)
			}

			// Retrying in the same process finishes setup without collecting a
			// second copy or replacing the first with the generated document.
			retry := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
			if retry.Code != http.StatusOK {
				t.Fatalf("retry status = %d, body = %s", retry.Code, retry.Body.String())
			}
			backups = setupConfigBackups(t, cfg.ConfigPath)
			if len(backups) != 1 {
				t.Fatalf("config backups after retry = %v, want exactly one", backups)
			}
			if kept, err := os.ReadFile(backups[0]); err != nil || string(kept) != reopenedSetupDocument {
				t.Fatalf("backup after retry = %q, %v; want the previous config", kept, err)
			}
		})
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
		{name: "after config backup", stage: setupStageAfterConfigBackup, wantAdmin: true},
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
			if token, err := config.ReadBootstrapToken(cfg.ConfigPath); err != nil || token != cfg.BootstrapToken {
				t.Fatalf("interrupted setup lost its retrieval token: %v", err)
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

// competeWithSetupWrites makes another connection commit right before each of
// the setup transaction's first limit inserts. The setup transaction has
// already read by then, so SQLite invalidates its WAL snapshot and fails the
// insert at once with SQLITE_BUSY, which busy_timeout cannot wait out. This is
// what a background writer such as the first blocklist sync does to a scripted
// setup. The returned counter reports how many conflicts were injected.
func competeWithSetupWrites(t *testing.T, handler *SetupHandler, cfg *config.Config, limit int) *int {
	t.Helper()
	writer, err := db.Open(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		t.Fatalf("open competing writer: %v", err)
	}
	t.Cleanup(func() {
		if sqlDatabase, dbErr := writer.DB(); dbErr == nil {
			_ = sqlDatabase.Close()
		}
	})
	if err := handler.db.Exec("CREATE TABLE competing_writes (value INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	conflicts := 0
	if err := handler.db.Callback().Create().Before("gorm:create").Register("test:commit_competing_writer", func(tx *gorm.DB) {
		if conflicts >= limit {
			return
		}
		conflicts++
		if err := writer.Exec("INSERT INTO competing_writes VALUES (1)").Error; err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	return &conflicts
}

func TestSetupCompleteRetriesWhenAnotherWriterMakesTheDatabaseBusy(t *testing.T) {
	handler, cfg, root, restarts := newPersistentSetupTestHandler(t)
	conflicts := competeWithSetupWrites(t, handler, cfg, 2)

	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if *conflicts != 2 {
		t.Fatalf("injected conflicts = %d, want 2", *conflicts)
	}
	if *restarts != 1 {
		t.Fatalf("restart count = %d, want 1", *restarts)
	}
	var administrators int64
	if err := handler.db.Model(&db.User{}).Where("role = ?", "admin").Count(&administrators).Error; err != nil {
		t.Fatal(err)
	}
	if administrators != 1 {
		t.Fatalf("administrator count = %d, want exactly one after retried attempts", administrators)
	}
	if err := VerifyExistingAdminCredentials(handler.db, "operator", "Tr0ub4dor&Correct"); err != nil {
		t.Fatalf("administrator after busy retries: %v", err)
	}
}

// A scripted setup sent right after the first start meets the first blocklist
// sync, which commits batch after batch for tens of seconds. Setup keeps
// running its transaction again for as long as that takes, not a fixed small
// number of times.
func TestSetupCompleteOutlastsALongRunOfBusyAttempts(t *testing.T) {
	handler, cfg, root, restarts := newPersistentSetupTestHandler(t)
	conflicts := competeWithSetupWrites(t, handler, cfg, 8)

	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d after %d busy attempts, body = %s", recorder.Code, *conflicts, recorder.Body.String())
	}
	if *conflicts != 8 || *restarts != 1 {
		t.Fatalf("injected conflicts = %d, restarts = %d; want 8 and 1", *conflicts, *restarts)
	}
}

func TestSetupCompleteGivesUpOnAPersistentlyBusyDatabase(t *testing.T) {
	handler, cfg, root, restarts := newPersistentSetupTestHandler(t)
	handler.busyBudget = 400 * time.Millisecond
	conflicts := competeWithSetupWrites(t, handler, cfg, 1_000_000)

	started := time.Now()
	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	elapsed := time.Since(started)
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "ADMIN_CREATE_FAILED") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	// One attempt can outlast this short budget where hashing is slow (the
	// race detector), so the test checks the time it gave up, not a count;
	// TestSetupCompleteOutlastsALongRunOfBusyAttempts covers repeated attempts.
	if *conflicts < 1 {
		t.Fatalf("setup attempts = %d, want at least one", *conflicts)
	}
	if elapsed < handler.busyBudget || elapsed > handler.busyBudget+5*time.Second {
		t.Fatalf("setup gave up after %s, want about the busy budget of %s", elapsed, handler.busyBudget)
	}
	if *restarts != 0 || !cfg.IsDefault || fileExists(t, cfg.ConfigPath) {
		t.Fatalf("a failed setup closed the gate: restarts=%d pending=%t", *restarts, cfg.IsDefault)
	}
	var users int64
	if err := handler.db.Model(&db.User{}).Count(&users).Error; err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Fatalf("user count = %d, want none after every attempt rolled back", users)
	}
	if token, err := config.ReadBootstrapToken(cfg.ConfigPath); err != nil || token != cfg.BootstrapToken {
		t.Fatalf("failed setup lost its retrieval token: %v", err)
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
	cfg, err := config.LoadForServer()
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
	if got := setupReconnectURL(request, 8443, 24444); got != "https://[::1]:24444/" {
		t.Fatalf("setupReconnectURL = %q", got)
	}
	if got := setupRestartStrategy("windows"); got != contract.SetupRestartStrategySupervisorRequired {
		t.Fatalf("Windows restart strategy = %q", got)
	}
}

// reconnect_url tells a scripted client where the service is after the
// restart. Only a request that reached the listener directly can follow a
// port chosen in the setup form; behind a published container port or a
// reverse proxy the client's own origin is the only address it can use.
func TestSetupCompleteReconnectURLFollowsHowTheRequestArrived(t *testing.T) {
	for _, test := range []struct {
		name           string
		listenPort     int
		configuredPort int
		host           string
		forwardedProto string
		want           string
	}{
		{
			name:       "direct request with unchanged port",
			listenPort: 23333, configuredPort: 23333, host: "127.0.0.1:23333",
			want: "http://127.0.0.1:23333/",
		},
		{
			name:       "direct request with a changed port",
			listenPort: 23333, configuredPort: 24444, host: "depsilo.example.test:23333",
			want: "http://depsilo.example.test:24444/",
		},
		{
			name:       "direct request to an IPv6 address with a changed port",
			listenPort: 23333, configuredPort: 24444, host: "[::1]:23333",
			want: "http://[::1]:24444/",
		},
		{
			name:       "published container port",
			listenPort: 23333, configuredPort: 23333, host: "127.0.0.1:24901",
			want: "http://127.0.0.1:24901/",
		},
		{
			name:       "published container port ignores a changed port",
			listenPort: 23333, configuredPort: 24444, host: "127.0.0.1:24901",
			want: "http://127.0.0.1:24901/",
		},
		{
			name:       "TLS proxy with a Host without port",
			listenPort: 23333, configuredPort: 23333, host: "depsilo.example.test", forwardedProto: "https",
			want: "https://depsilo.example.test/",
		},
		{
			name:       "TLS proxy on its own port",
			listenPort: 23333, configuredPort: 23333, host: "depsilo.example.test:24920", forwardedProto: "https",
			want: "https://depsilo.example.test:24920/",
		},
		{
			name:       "proxy on the listening port still decides the address",
			listenPort: 23333, configuredPort: 24444, host: "depsilo.example.test:23333", forwardedProto: "https, http",
			want: "https://depsilo.example.test:23333/",
		},
		{
			name:       "plain proxy without a forwarded scheme",
			listenPort: 23333, configuredPort: 23333, host: "depsilo.example.test",
			want: "http://depsilo.example.test/",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, cfg, root, _ := newSetupTestHandler(t)
			cfg.Server.Port = test.listenPort
			setup := decodeValidSetupRequest(t, root)
			setup.Server.Port = test.configuredPort
			body, err := json.Marshal(setup)
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.POST("/setup/complete", handler.Complete)
			request := httptest.NewRequest(http.MethodPost, "/setup/complete", bytes.NewReader(body))
			request.Host = test.host
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(BootstrapTokenHeader, cfg.BootstrapToken)
			if test.forwardedProto != "" {
				request.Header.Set("X-Forwarded-Proto", test.forwardedProto)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var response map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode setup response: %v", err)
			}
			if response["reconnect_url"] != test.want {
				t.Fatalf("reconnect_url = %#v, want %q", response["reconnect_url"], test.want)
			}
		})
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

func setupStatusFields(t *testing.T, handler *SetupHandler) map[string]any {
	t.Helper()
	router := gin.New()
	router.GET("/setup/status", handler.Status)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/setup/status", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("setup status code = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var fields map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &fields); err != nil {
		t.Fatalf("decode setup status: %v", err)
	}
	return fields
}

func TestSetupStatusReportsListenPortAndExistingConfigWhilePending(t *testing.T) {
	handler, cfg, _, _ := newSetupTestHandler(t)
	cfg.Server.Port = 24646
	cfg.Server.Host = " ::1 "

	want := map[string]any{
		"needs_setup":    true,
		"token_required": true,
		"listen_port":    float64(24646),
		"token_command":  "depsilo setup token --port 24646 --host ::1",
		"config_exists":  false,
	}
	if got := setupStatusFields(t, handler); !reflect.DeepEqual(got, want) {
		t.Fatalf("first-run setup status = %#v, want exactly %#v", got, want)
	}

	reopenSetupWithExistingConfig(t, cfg)
	want["config_exists"] = true
	if got := setupStatusFields(t, handler); !reflect.DeepEqual(got, want) {
		t.Fatalf("reopened setup status = %#v, want exactly %#v", got, want)
	}
}

// A publication that fails after it has put the generated document in place
// leaves a file at the config path. Only a file that was there before setup
// began is the operator's, and only that one has a backup beside it.
func TestSetupStatusTellsAnOperatorConfigFromAHalfPublishedOne(t *testing.T) {
	for _, test := range []struct {
		name     string
		reopened bool
	}{
		{name: "first run", reopened: false},
		{name: "reopened setup", reopened: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, cfg, root, _ := newSetupTestHandler(t)
			if test.reopened {
				reopenSetupWithExistingConfig(t, cfg)
			}
			handler.writeConfig = func(path string, _ config.SetupRequest, _ config.WriteStageHook) error {
				if err := os.WriteFile(path, []byte("config_version = 1\n"), 0o600); err != nil {
					t.Fatalf("publish generated config: %v", err)
				}
				return errors.New("directory sync failed after the rename")
			}
			recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
			if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "WRITE_FAILED") {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}

			fields := setupStatusFields(t, handler)
			if fields["needs_setup"] != true {
				t.Fatalf("a failed publication closed setup: %#v", fields)
			}
			if fields["config_exists"] != test.reopened {
				t.Fatalf("config_exists = %v after a failed publication, want %v", fields["config_exists"], test.reopened)
			}
			if backups := setupConfigBackups(t, cfg.ConfigPath); (len(backups) == 1) != test.reopened {
				t.Fatalf("backups = %v, want one only for a reopened setup", backups)
			}
		})
	}
}

func TestSetupStatusDisclosesNothingAboutAConfiguredServer(t *testing.T) {
	handler, cfg, _, _ := newSetupTestHandler(t)
	cfg.Server.Port = 24646
	reopenSetupWithExistingConfig(t, cfg)
	cfg.IsDefault = false

	want := map[string]any{"needs_setup": false, "token_required": false}
	if got := setupStatusFields(t, handler); !reflect.DeepEqual(got, want) {
		t.Fatalf("configured setup status = %#v, want exactly %#v", got, want)
	}
}

// Between a completed setup and the restart, the process answers needs_setup
// false while it still serves no package route. restart_pending is the one
// signal a script has for that window, and it exists in no other state.
func TestSetupStatusReportsRestartPendingOnlyBetweenCompletionAndRestart(t *testing.T) {
	handler, cfg, root, restarts := newSetupTestHandler(t)
	if fields := setupStatusFields(t, handler); fields["needs_setup"] != true {
		t.Fatalf("status before setup = %#v", fields)
	} else if _, present := fields["restart_pending"]; present {
		t.Fatalf("status before setup carries restart_pending: %#v", fields)
	}

	rejected := setupRequest(handler, "203.0.113.10:4321", "wrong-token", validSetupRequest(t, root))
	if rejected.Code != http.StatusForbidden {
		t.Fatalf("rejected setup status = %d, body = %s", rejected.Code, rejected.Body.String())
	}
	if _, present := setupStatusFields(t, handler)["restart_pending"]; present {
		t.Fatal("a rejected setup request reported a pending restart")
	}

	recorder := setupRequest(handler, "203.0.113.10:4321", cfg.BootstrapToken, validSetupRequest(t, root))
	if recorder.Code != http.StatusOK || *restarts != 1 {
		t.Fatalf("status = %d, restarts = %d, body = %s", recorder.Code, *restarts, recorder.Body.String())
	}
	want := map[string]any{"needs_setup": false, "token_required": false, "restart_pending": true}
	if got := setupStatusFields(t, handler); !reflect.DeepEqual(got, want) {
		t.Fatalf("status after completion and before restart = %#v, want exactly %#v", got, want)
	}

	// The restarted process loads the written config and is constructed as
	// configured: it answers exactly what a configured server always answered.
	restartedCfg := *cfg
	restarted := NewSetupHandler(&restartedCfg, handler.db)
	want = map[string]any{"needs_setup": false, "token_required": false}
	if got := setupStatusFields(t, restarted); !reflect.DeepEqual(got, want) {
		t.Fatalf("status of a configured server = %#v, want exactly %#v", got, want)
	}
}

func setupRequestWithHeaders(handler *SetupHandler, remoteAddr string, headers map[string]string, body []byte) *httptest.ResponseRecorder {
	router := gin.New()
	router.POST("/setup/complete", handler.Complete)
	router.GET("/setup/status", handler.Status)
	method, target := http.MethodPost, "/setup/complete"
	if body == nil {
		method, target = http.MethodGet, "/setup/status"
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.RemoteAddr = remoteAddr
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestSetupWaivesAGeneratedTokenForADirectLoopbackRequest(t *testing.T) {
	for _, peer := range []string{"127.0.0.1:4321", "[::1]:4321"} {
		t.Run(peer, func(t *testing.T) {
			handler, cfg, root, restarts := newSetupTestHandler(t)
			cfg.BootstrapTokenGenerated = true
			status := setupRequestWithHeaders(handler, peer, nil, nil)
			if !strings.Contains(status.Body.String(), `"token_required":false`) {
				t.Fatalf("status = %s, want token_required false", status.Body.String())
			}
			recorder := setupRequestWithHeaders(handler, peer, nil, validSetupRequest(t, root))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if *restarts != 1 {
				t.Fatalf("restart count = %d, want 1", *restarts)
			}
		})
	}
}

// The token is waived only when it was generated and the request reaches the
// listener directly from loopback. A token the operator chose through
// DEPSILO_BOOTSTRAP_TOKEN stays required even from the server itself.
func TestSetupRequiresAGeneratedTokenFromAnyOtherRequest(t *testing.T) {
	for name, test := range map[string]struct {
		peer    string
		headers map[string]string
		preset  bool
	}{
		"preset token on loopback":        {peer: "127.0.0.1:4321", preset: true},
		"remote peer":                     {peer: "203.0.113.10:4321"},
		"container gateway":               {peer: "172.17.0.1:4321"},
		"loopback behind X-Forwarded-For": {peer: "127.0.0.1:4321", headers: map[string]string{"X-Forwarded-For": "203.0.113.10"}},
		"loopback behind Forwarded":       {peer: "127.0.0.1:4321", headers: map[string]string{"Forwarded": "for=203.0.113.10"}},
		"loopback behind X-Real-IP":       {peer: "127.0.0.1:4321", headers: map[string]string{"X-Real-IP": "203.0.113.10"}},
		"loopback behind Via":             {peer: "127.0.0.1:4321", headers: map[string]string{"Via": "1.1 proxy"}},
	} {
		t.Run(name, func(t *testing.T) {
			handler, cfg, root, restarts := newSetupTestHandler(t)
			cfg.BootstrapTokenGenerated = !test.preset
			status := setupRequestWithHeaders(handler, test.peer, test.headers, nil)
			if !strings.Contains(status.Body.String(), `"token_required":true`) {
				t.Fatalf("status = %s, want token_required true", status.Body.String())
			}
			if strings.Contains(status.Body.String(), cfg.BootstrapToken) || strings.Contains(status.Body.String(), cfg.ConfigPath) {
				t.Fatalf("status leaked setup secrets: %s", status.Body.String())
			}
			recorder := setupRequestWithHeaders(handler, test.peer, test.headers, validSetupRequest(t, root))
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if *restarts != 0 {
				t.Fatalf("restart count = %d, want 0", *restarts)
			}
		})
	}
}
