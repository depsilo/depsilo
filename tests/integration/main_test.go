//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"depsilo/tests/mock"
)

var (
	depsiloURL    string
	mockServer    *mock.MockUpstream
	testDir       string
	depsiloCancel context.CancelFunc
)

func TestMain(m *testing.M) {
	// 1. Create temp dir
	var err error
	testDir, err = os.MkdirTemp("", "depsilo-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(testDir)

	// 2. Start mock upstream
	mockServer = mock.NewMockUpstream()
	mockServer.RegisterAll()
	defer mockServer.Close()

	// 3. Generate test config
	port := getFreePort()
	depsiloURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	writeTestConfig(testDir, mockServer.URL(), port)

	// 4. Start Depsilo in background with cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	depsiloCancel = cancel
	go startDepsilo(ctx, testDir)

	// 5. Wait for ready
	if err := waitForReady(depsiloURL+"/health", 15*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "depsilo not ready: %v\n", err)
		cancel()
		os.Exit(1)
	}

	// 6. Run tests
	code := m.Run()

	// 7. Shutdown Depsilo before mock server closes
	cancel()
	time.Sleep(500 * time.Millisecond)

	os.Exit(code)
}

func writeTestConfig(dir, upstreamURL string, port int) {
	cfg := fmt.Sprintf(`
[server]
host = "127.0.0.1"
port = %d

[database]
driver = "sqlite"
dsn = "%s/test.db"

[storage]
type = "local"
path = "%s/cache"

[cache]
max_size_gb = 1
ttl_index = "5m"
ttl_blob = "2s"
lru_threshold = 90

[auth]
enabled = false

[supply_chain]
min_release_age_enabled = false

# The malicious blocklist syncs from the mock's OSV endpoint — it
# marks npm "malicious-pkg" as malware, driving the end-to-end 451
# MALICIOUS_BLOCKED test. The age-gate switch above does not disable the
# blocklist: malware blocking is checker step 0, before threshold logic.
[supply_chain.blocklist]
enabled = true
sync_interval = "6h"
mirror_url = "%s"

# Tamper detection ships enabled by default with the threshold derived
# from cache.ttl_blob. ttl_blob is shortened to 2s above so the tamper
# integration test can trigger a background refresh within test time;
# override the threshold to 1s here so that 2s-TTL blob still classifies
# as immutable (threshold must be <= the blob TTL it's meant to cover).
[supply_chain.tamper_detection]
enabled = true
immutable_threshold_seconds = 1

[[pypi.upstreams]]
name = "mock"
url = "%s"
priority = 1

[[apt.upstreams]]
name = "mock"
url = "%s"
priority = 1

[[npm.upstreams]]
name = "mock"
url = "%s"
priority = 1

[[go.upstreams]]
name = "mock"
url = "%s"
priority = 1

[[cargo.upstreams]]
name = "mock"
url = "%s"
priority = 1

[[maven.upstreams]]
name = "mock"
url = "%s"
priority = 1

[[rubygems.upstreams]]
name = "mock"
url = "%s"
priority = 1

[[composer.upstreams]]
name = "mock"
url = "%s"
priority = 1

[[nuget.upstreams]]
name = "mock"
url = "%s"
priority = 1

[[conda.upstreams]]
name = "mock"
url = "%s"
priority = 1

[[cran.upstreams]]
name = "mock"
url = "%s"
priority = 1

[[helm.upstreams]]
name = "mock"
url = "%s"
priority = 1

[[huggingface.upstreams]]
name = "mock"
url = "%s"
priority = 1

[docker]
default_registry = "mock"

[[docker.registries]]
name = "mock"
url = "%s"
`, port, dir, dir,
		upstreamURL, // blocklist mirror_url (mock OSV endpoint)
		upstreamURL, upstreamURL, upstreamURL, upstreamURL,
		upstreamURL, upstreamURL, upstreamURL, upstreamURL,
		upstreamURL, upstreamURL, upstreamURL, upstreamURL,
		upstreamURL, upstreamURL)

	os.WriteFile(dir+"/config.toml", []byte(cfg), 0644)
}

func startDepsilo(ctx context.Context, dir string) {
	// Resolve project root
	root, _ := os.Getwd()
	if _, err := os.Stat(root + "/go.mod"); err != nil {
		root, _ = filepath.Abs(root + "/../..")
	}

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/server")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"DEPSILO_CONFIG="+dir+"/config.toml",
		"DEPSILO_AUTH_JWT_SECRET=test-only-0123456789abcdef0123456789abcdef",
		"DEPSILO_ADMIN_USERNAME=admin",
		"DEPSILO_ADMIN_PASSWORD="+integrationAdminPassword,
	)
	// Discard output to avoid "I/O incomplete" on test exit
	devNull, _ := os.Open(os.DevNull)
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	// Use process group so we can kill all child processes
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Run()
}
