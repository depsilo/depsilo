package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// The [egress] default proxy, here from its environment variable, reaches the
// Docker registry clients that StartServer builds: a registry without its own
// proxy sends every request through it.
func TestStartServerRoutesDockerRegistriesThroughTheEgressProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const index = `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`
	var (
		mu        sync.Mutex
		requested []string
	)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.Host+r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case "/v2/library/widget/manifests/latest":
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			_, _ = io.WriteString(w, index)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(proxy.Close)

	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.toml")
	// A registry address literal needs no DNS, so the test does not depend on
	// the resolver of the machine it runs on.
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

[docker]
default_registry = "private"

[[docker.registries]]
name = "private"
url = "http://10.255.255.1"
`, filepath.Join(directory, "server.db"), filepath.Join(directory, "cache"))
	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_ADMIN_PASSWORD", "integration-Test-password-123")
	t.Setenv("DEPSILO_EGRESS_UPSTREAM_PROXY", proxy.URL)

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

	requestContext, cancelRequest := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRequest()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v2/library/widget/manifests/latest", nil).WithContext(requestContext)
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != index {
		t.Fatalf("manifest status = %d, body = %s", response.Code, response.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"10.255.255.1/v2/", "10.255.255.1/v2/library/widget/manifests/latest"}
	if !slices.Equal(requested, want) {
		t.Fatalf("egress proxy carried %v, want %v", requested, want)
	}
}
