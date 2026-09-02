package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"depsilo/internal/db"
	"go.uber.org/zap"
)

func TestStartServerReopensSetupWhenConfiguredDatabaseHasNoLoginableAdmin(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.toml")
	databasePath := filepath.Join(directory, "server.db")
	storagePath := filepath.Join(directory, "cache")
	document := fmt.Sprintf(`config_version = 1

[server]
host = "127.0.0.1"
port = 0
log_level = "error"

[database]
driver = "sqlite"
dsn = %q

[storage]
type = "local"
path = %q

[access_log]
rollup_enabled = false
backfill_on_start = false
retention_days = 0
rollup_retention_days = 0

[upstream_updates]
enabled = false

[security]
enabled = false

[supply_chain.blocklist]
enabled = false

[supply_chain.tamper_detection]
enabled = false

[auth]
enabled = true
jwt_secret = "startup-recovery-test-secret-with-sufficient-length"
token_ttl = "1h"
`, databasePath, storagePath)
	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"DEPSILO_DATABASE_DSN",
		"DEPSILO_DATABASE_DRIVER",
		"DEPSILO_STORAGE_PATH",
		"DEPSILO_SERVER_HOST",
		"DEPSILO_SERVER_PORT",
		"DEPSILO_AUTH_JWT_SECRET",
		"DEPSILO_ADMIN_USERNAME",
		"DEPSILO_ADMIN_PASSWORD",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_BOOTSTRAP_TOKEN", "startup-recovery-bootstrap-token-012345")

	ctx, cancel := context.WithCancel(context.Background())
	server, err := StartServer(ctx, zap.NewAtomicLevel())
	if err != nil {
		cancel()
		t.Fatalf("StartServer: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := Shutdown(shutdownCtx, server); err != nil {
			t.Errorf("shutdown server: %v", err)
		}
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil)
	request.RemoteAddr = "127.0.0.1:4321"
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("setup status code = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var status struct {
		NeedsSetup    bool `json:"needs_setup"`
		TokenRequired bool `json:"token_required"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode setup status: %v", err)
	}
	if !status.NeedsSetup || !status.TokenRequired {
		t.Fatalf("setup status = %#v, want recoverable token-protected setup", status)
	}

	database, err := db.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	var administrators int64
	if err := database.Model(&db.User{}).
		Where("role = ? AND enabled = ?", "admin", true).
		Count(&administrators).Error; err != nil {
		t.Fatal(err)
	}
	if administrators != 0 {
		t.Fatalf("administrator count = %d, want zero while setup is reopened", administrators)
	}
}
