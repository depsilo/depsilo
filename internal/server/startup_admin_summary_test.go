package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"depsilo/internal/api"
	"depsilo/internal/db"
	"go.uber.org/zap"
)

// startConfiguredServerForSummary starts a server from a minimal config.toml
// with every network-facing background job disabled, and returns the console
// summary of that start together with the database path.
func startConfiguredServerForSummary(t *testing.T, adminUsername, adminPassword string) (summary, databasePath string) {
	t.Helper()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.toml")
	databasePath = filepath.Join(directory, "server.db")
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
jwt_secret = "startup-admin-summary-signing-secret-with-sufficient-length"
token_ttl = "1h"
`, databasePath, filepath.Join(directory, "cache"))
	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"DEPSILO_DATABASE_DSN", "DEPSILO_DATABASE_DRIVER", "DEPSILO_STORAGE_PATH",
		"DEPSILO_SERVER_HOST", "DEPSILO_SERVER_PORT", "DEPSILO_AUTH_JWT_SECRET",
		"DEPSILO_BOOTSTRAP_TOKEN",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_ADMIN_USERNAME", adminUsername)
	t.Setenv("DEPSILO_ADMIN_PASSWORD", adminPassword)

	var server *http.Server
	var err error
	summary = captureStartupSummary(t, func() {
		server, err = StartServer(context.Background(), zap.NewAtomicLevel())
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := Shutdown(ctx, server); err != nil {
			t.Errorf("shutdown server: %v", err)
		}
	})
	return summary, databasePath
}

// With a config.toml and only a username, the administrator gets a random
// password. It is that account's lasting credential, so the console summary
// must carry it instead of leaving it in one structured log line.
//
// The draws are supplied here, so the test cannot meet a token that happens to
// contain "ops": the first draw does so on purpose, the password policy
// rejects it, and the start must go on with the second.
func TestStartServerPrintsGeneratedAdministratorPasswordInSummary(t *testing.T) {
	const rejected = "k3Zq-OpS-rejected-draw-0123456789-ABCDEFG"
	const accepted = "k3Zq-accepted-draw-0123456789-ABCDEFGHIJK"
	draws := []string{rejected, accepted}
	original := newInitialAdminPassword
	t.Cleanup(func() { newInitialAdminPassword = original })
	newInitialAdminPassword = func() (string, error) {
		if len(draws) == 0 {
			t.Fatal("drew a third administrator password")
		}
		token := draws[0]
		draws = draws[1:]
		return token, nil
	}

	summary, databasePath := startConfiguredServerForSummary(t, "ops", "")

	match := regexp.MustCompile(`(?m)^  Generated password: (\S+)$`).FindStringSubmatch(summary)
	if match == nil || !strings.Contains(summary, "Initial administrator:\n  Username: ops\n") {
		t.Fatalf("startup summary omitted the generated administrator password:\n%s", summary)
	}
	if match[1] != accepted || len(draws) != 0 || strings.Contains(summary, rejected) {
		t.Fatalf("printed password = %q with %d draws left, want the second draw %q:\n%s", match[1], len(draws), accepted, summary)
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
	if err := api.VerifyExistingAdminCredentials(database, "ops", match[1]); err != nil {
		t.Fatalf("the printed password does not sign in: %v", err)
	}
}

func TestStartServerNeverPrintsSuppliedAdministratorPassword(t *testing.T) {
	const supplied = "Startup-summary-test-password-012345"
	summary, _ := startConfiguredServerForSummary(t, "ops", supplied)

	if strings.Contains(summary, supplied) || strings.Contains(summary, "Initial administrator") {
		t.Fatalf("startup summary disclosed a supplied administrator password:\n%s", summary)
	}
	if !strings.Contains(summary, "Portal:") {
		t.Fatalf("startup summary was not captured:\n%s", summary)
	}
}
