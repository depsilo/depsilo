package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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
	"go.uber.org/zap/zaptest/observer"
)

// startServerWithDockerRegistry starts a server whose only Docker registry,
// private, has registryURL and no proxy of its own, with the [egress] default
// proxy set to proxyURL through its environment variable.
func startServerWithDockerRegistry(t *testing.T, registryURL, proxyURL string) *http.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	directory := t.TempDir()
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

[docker]
default_registry = "private"

[[docker.registries]]
name = "private"
url = %q
`, filepath.Join(directory, "server.db"), filepath.Join(directory, "cache"), registryURL)
	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_ADMIN_PASSWORD", "integration-Test-password-123")
	t.Setenv("DEPSILO_EGRESS_UPSTREAM_PROXY", proxyURL)

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
	return server
}

// recordingEgressProxy is a forward proxy that records the host and path of
// every request it carries and answers with handle.
func recordingEgressProxy(t *testing.T, handle http.HandlerFunc) (*httptest.Server, func() []string) {
	t.Helper()
	var (
		mu        sync.Mutex
		requested []string
	)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.Host+r.URL.Path)
		mu.Unlock()
		handle(w, r)
	}))
	t.Cleanup(proxy.Close)
	return proxy, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(requested)
	}
}

// recordDockerDials replaces the dialer of the Docker registry clients that
// StartServer builds for the rest of the test. A dial to proxyAddress reaches
// that proxy. Any other dial is direct: it is recorded and connected to
// directAddress, or refused when directAddress is empty, so no registry
// connection leaves the test machine. It returns the recorded direct dials.
func recordDockerDials(t *testing.T, proxyAddress, directAddress string) func() []string {
	t.Helper()
	var (
		mu     sync.Mutex
		dialed []string
	)
	previous := dockerRegistryDialer
	dockerRegistryDialer = func(ctx context.Context, network, address string) (net.Conn, error) {
		var dialer net.Dialer
		if address == proxyAddress {
			return dialer.DialContext(ctx, network, address)
		}
		mu.Lock()
		dialed = append(dialed, address)
		mu.Unlock()
		if directAddress == "" {
			return nil, errors.New("unexpected direct dial")
		}
		return dialer.DialContext(ctx, network, directAddress)
	}
	t.Cleanup(func() { dockerRegistryDialer = previous })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(dialed)
	}
}

const widgetIndex = `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`

// serveWidgetIndex answers as a registry without authentication that holds
// the image index of library/widget:latest.
func serveWidgetIndex(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v2/":
		w.WriteHeader(http.StatusOK)
	case "/v2/library/widget/manifests/latest":
		w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
		_, _ = io.WriteString(w, widgetIndex)
	default:
		http.NotFound(w, r)
	}
}

// getWidgetIndex pulls the image index of library/widget:latest through
// server and fails the test unless the server answers with it.
func getWidgetIndex(t *testing.T, server *http.Server) {
	t.Helper()
	requestContext, cancelRequest := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRequest()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v2/library/widget/manifests/latest", nil).WithContext(requestContext)
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != widgetIndex {
		t.Fatalf("manifest status = %d, body = %s", response.Code, response.Body.String())
	}
}

// The [egress] default proxy, here from its environment variable, reaches the
// Docker registry clients that StartServer builds: a registry without its own
// proxy sends every request whose host answers publicly through it, and
// nothing is dialed directly.
func TestStartServerRoutesDockerRegistriesThroughTheEgressProxy(t *testing.T) {
	proxy, requested := recordingEgressProxy(t, serveWidgetIndex)
	directDials := recordDockerDials(t, proxy.Listener.Addr().String(), "")
	// A public address literal needs no DNS, so the test does not depend on
	// the resolver of the machine it runs on.
	server := startServerWithDockerRegistry(t, "http://93.184.216.34", proxy.URL)

	getWidgetIndex(t, server)
	want := []string{"93.184.216.34/v2/", "93.184.216.34/v2/library/widget/manifests/latest"}
	if got := requested(); !slices.Equal(got, want) {
		t.Fatalf("egress proxy carried %v, want %v", got, want)
	}
	if got := directDials(); len(got) != 0 {
		t.Fatalf("direct dials = %v, want none", got)
	}
}

// A registry written as a private address literal keeps the default proxy's
// route, but its private answer is dialed directly: the proxy carries
// nothing, and the server log names the registry and the host it dials. The
// test's dialer connects the registry's address to a local registry.
func TestStartServerDialsAPrivateLiteralDockerRegistryDirectly(t *testing.T) {
	proxy, requested := recordingEgressProxy(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unexpected proxy request", http.StatusBadGateway)
	})
	registry := httptest.NewServer(http.HandlerFunc(serveWidgetIndex))
	t.Cleanup(registry.Close)
	directDials := recordDockerDials(t, proxy.Listener.Addr().String(), registry.Listener.Addr().String())
	core, logs := observer.New(zap.InfoLevel)
	t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
	server := startServerWithDockerRegistry(t, "http://10.255.255.1", proxy.URL)

	getWidgetIndex(t, server)
	if got := requested(); len(got) != 0 {
		t.Fatalf("egress proxy carried %v, want nothing", got)
	}
	got := directDials()
	if len(got) == 0 {
		t.Fatal("nothing was dialed directly, want the registry's address")
	}
	for _, address := range got {
		if address != "10.255.255.1:80" {
			t.Fatalf("direct dials = %v, want only the registry's address", got)
		}
	}
	routes := logs.FilterMessage("docker registry egress").All()
	if len(routes) != 1 || routes[0].ContextMap()["route"] != "default proxy (private answers direct)" {
		t.Fatalf("egress lines = %v, want the default proxy's route", routes)
	}
	dialed := logs.FilterMessage("docker registry host dialed directly").All()
	if len(dialed) != 1 || dialed[0].ContextMap()["registry"] != "private" || dialed[0].ContextMap()["host"] != "10.255.255.1" {
		t.Fatalf("direct dial lines = %v, want one for the registry's host", dialed)
	}
}
