package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"depsilo/internal/config"
	"go.uber.org/zap"
)

func TestStartServerPublishesCurrentBootstrapTokenBeforeServing(t *testing.T) {
	for _, test := range []struct {
		name       string
		configured bool
		blocked    bool
	}{
		{name: "reopened setup replaces prior runtime token"},
		{name: "configured instance clears prior runtime token", configured: true},
		{name: "pending setup refuses startup when publication fails", blocked: true},
		{name: "configured instance tolerates cleanup failure", configured: true, blocked: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
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
jwt_secret = "startup-token-test-signing-secret-with-sufficient-length"
token_ttl = "1h"
`, filepath.Join(directory, "server.db"), filepath.Join(directory, "cache"))
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
			const currentToken = "current-process-bootstrap-token-0123456789"
			t.Setenv("DEPSILO_BOOTSTRAP_TOKEN", currentToken)
			if test.configured {
				t.Setenv("DEPSILO_ADMIN_USERNAME", "operator")
				t.Setenv("DEPSILO_ADMIN_PASSWORD", "Startup-token-test-password-012345")
			}
			if err := config.WriteBootstrapToken(configPath, "previous-process-bootstrap-token-0123456789"); err != nil {
				t.Fatal(err)
			}
			path, err := config.BootstrapTokenPath(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if test.blocked {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			server, err := StartServer(context.Background(), zap.NewAtomicLevel())
			if test.blocked && !test.configured {
				if server != nil || err == nil || !strings.Contains(err.Error(), "publish initial setup token") {
					t.Fatalf("StartServer = (%v, %v), want token-publication failure", server, err)
				}
				return
			}
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
			recorder := httptest.NewRecorder()
			server.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
			if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), currentToken) {
				t.Fatalf("setup status failed or disclosed bootstrap token: %d", recorder.Code)
			}
			if test.blocked {
				return
			}
			token, err := config.ReadBootstrapToken(configPath)
			if test.configured {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("configured startup retained a bootstrap token: %v", err)
				}
			} else if err != nil || token != currentToken {
				t.Fatalf("setup server did not publish its current runtime token: %v", err)
			}
		})
	}
}
