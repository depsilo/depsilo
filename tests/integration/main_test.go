//go:build integration

package integration

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"depsilo/tests/mock"
)

var (
	depsiloURL string
	mockServer *mock.MockUpstream
	testDir    string
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

	// 4. Start Depsilo in background
	go startDepsilo(testDir, port)

	// 5. Wait for ready
	if err := waitForReady(depsiloURL+"/health", 10*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "depsilo not ready: %v\n", err)
		os.Exit(1)
	}

	// 6. Run tests
	code := m.Run()
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
ttl_blob = "72h"
lru_threshold = 90

[auth]
enabled = false

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
`, port, dir, dir,
		upstreamURL, upstreamURL, upstreamURL, upstreamURL,
		upstreamURL, upstreamURL, upstreamURL, upstreamURL,
		upstreamURL, upstreamURL, upstreamURL, upstreamURL)

	os.WriteFile(dir+"/config.toml", []byte(cfg), 0644)
}

func startDepsilo(dir string, port int) {
	cmd := exec.Command("go", "run", "./cmd/server")
	cmd.Env = append(os.Environ(), "DEPSILO_CONFIG="+dir+"/config.toml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}
