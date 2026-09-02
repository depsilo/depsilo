package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"depsilo/internal/db"
	"depsilo/internal/rules"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestStartServerInjectsConfiguredExtraPyPIRoutesIntoPackageRules(t *testing.T) {
	gin.SetMode(gin.TestMode)

	directory := t.TempDir()
	databasePath := filepath.Join(directory, "server.db")
	configPath := filepath.Join(directory, "config.toml")
	document := fmt.Sprintf(`
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
jwt_secret = "integration-test-secret-with-sufficient-length"
token_ttl = "1h"

[extra_index_presets]
enabled = false

[[extra_indexes]]
name = "private"
path = "company/python/private"
simple_path = "/simple"

[[extra_indexes.upstreams]]
name = "private"
url = "http://127.0.0.1:1"
priority = 1
probe_mode = "passive"

[[extra_indexes]]
name = "torch"
kind = "pytorch"
path = "company/python/torch"
simple_path = "/"

[[extra_indexes.upstreams]]
name = "torch"
url = "http://127.0.0.1:1"
priority = 1
probe_mode = "passive"
`, databasePath, filepath.Join(directory, "cache"))
	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_ADMIN_PASSWORD", "integration-Test-password-123")

	database, err := db.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := rules.NewStore(database)
	if err := store.Create(&db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny",
	}); err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDatabase.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	server, err := StartServer(ctx, zap.NewAtomicLevel())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := Shutdown(shutdownContext, server); err != nil {
			t.Errorf("shutdown server: %v", err)
		}
	})

	for _, requestPath := range []string{
		"/company/python/private/simple/requests/",
		"/p/payments/company/python/private/simple/requests/",
		"/company/python/torch/cu128/simple/requests/",
		"/p/ml/company/python/torch/cpu/simple/requests/",
	} {
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, requestPath, nil))
		if response.Code != http.StatusForbidden {
			t.Errorf("GET %s status = %d, want %d; body = %s", requestPath, response.Code, http.StatusForbidden, response.Body.String())
		}
	}

	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/company/python/private/files/my-package-1.0.tar.gz",
		nil,
	))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("ambiguous artifact status = %d, want %d; body = %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
}
