package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func trustedProxyTestClientIP(t *testing.T, trustedProxies []string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	if err := configureTrustedProxies(engine, trustedProxies); err != nil {
		t.Fatalf("configureTrustedProxies: %v", err)
	}
	var clientIP string
	engine.GET("/ip", func(c *gin.Context) {
		clientIP = c.ClientIP()
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/ip", nil)
	request.RemoteAddr = "127.0.0.1:43210"
	request.Header.Set("X-Forwarded-For", "203.0.113.10")
	engine.ServeHTTP(httptest.NewRecorder(), request)
	return clientIP
}

func TestConfigureTrustedProxiesDefaultsToNone(t *testing.T) {
	if got := trustedProxyTestClientIP(t, nil); got != "127.0.0.1" {
		t.Fatalf("ClientIP with no trusted proxies = %q, want direct peer", got)
	}
}

func TestConfigureTrustedProxiesUsesExplicitCIDR(t *testing.T) {
	if got := trustedProxyTestClientIP(t, []string{"127.0.0.0/8"}); got != "203.0.113.10" {
		t.Fatalf("ClientIP with explicit trusted proxy = %q, want forwarded client", got)
	}
}

func TestConfigureTrustedProxiesRejectsInvalidEntry(t *testing.T) {
	if err := configureTrustedProxies(gin.New(), []string{"not-a-proxy-cidr"}); err == nil {
		t.Fatal("configureTrustedProxies accepted an invalid entry")
	}
}

func TestStartServerRejectsInvalidTrustedProxyBeforeOpeningDatabase(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "server.db")
	configPath := filepath.Join(dir, "config.toml")
	document := fmt.Sprintf(`[server]
host = "127.0.0.1"
port = 23333
trusted_proxies = ["not-a-proxy-cidr"]

[database]
driver = "sqlite"
dsn = %q

[storage]
type = "local"
path = %q
`, databasePath, filepath.Join(dir, "cache"))
	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)

	srv, err := StartServer(context.Background(), zap.NewAtomicLevel())
	if err == nil || srv != nil {
		t.Fatalf("StartServer = (%v, %v), want invalid trusted-proxy error", srv, err)
	}
	if !strings.Contains(err.Error(), "server.trusted_proxies") {
		t.Fatalf("StartServer error = %v, want trusted-proxy context", err)
	}
	if _, statErr := os.Stat(databasePath); !os.IsNotExist(statErr) {
		t.Fatalf("database was opened before proxy validation, Stat error = %v", statErr)
	}
}
