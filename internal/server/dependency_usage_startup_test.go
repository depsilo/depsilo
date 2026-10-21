package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"depsilo/internal/db"
)

func TestStartServerRejectsUnrecordableUsageObservation(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "server.db")
	database, err := db.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	// A clean state must not remain authoritative while unobserved requests
	// are served. Refuse dirty markers but allow shutdown to save unknown.
	if err := database.Exec(`CREATE TRIGGER reject_usage_session
BEFORE INSERT ON control_plane_states
WHEN NEW.key = 'dependency_usage_v11' AND json_extract(NEW.value, '$.in_progress') = 1
BEGIN SELECT RAISE(FAIL, 'cannot start observation'); END`).Error; err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDatabase.Close(); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(directory, "config.toml")
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
[auth]
enabled = true
jwt_secret = "usage-startup-test-secret-with-sufficient-length"
token_ttl = "1h"
`, databasePath, filepath.Join(directory, "cache"))
	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"DEPSILO_DATABASE_DSN", "DEPSILO_DATABASE_DRIVER", "DEPSILO_STORAGE_PATH",
		"DEPSILO_SERVER_HOST", "DEPSILO_SERVER_PORT", "DEPSILO_AUTH_JWT_SECRET",
		"DEPSILO_ADMIN_USERNAME", "DEPSILO_ADMIN_PASSWORD",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_BOOTSTRAP_TOKEN", "usage-startup-bootstrap-token-0123456789")
	server, err := StartServer(context.Background(), zap.NewAtomicLevel())
	if server != nil || err == nil || !strings.Contains(err.Error(), "initialize dependency usage observation") {
		if server != nil {
			_ = Shutdown(context.Background(), server)
		}
		t.Fatalf("StartServer=(%v, %v), want observation initialization failure", server, err)
	}
}
