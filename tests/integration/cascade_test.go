//go:build integration

package integration

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

	"depsilo/internal/cascade"
)

// writeCascadeChildConfig points every npm/pypi upstream at the mock origin
// through the parent instance started by TestMain.
func writeCascadeChildConfig(t *testing.T, dir string, port int) {
	t.Helper()
	cfg := fmt.Sprintf(`
[server]
host = "127.0.0.1"
port = %d

[database]
driver = "sqlite"
dsn = "%s/child.db"

[storage]
type = "local"
path = "%s/cache"

[cache]
max_size_gb = 1
ttl_index = "5m"
# Keep the relayed artifact entry fresh for the whole suite; a very short TTL
# would make the parent's background refresh race unrelated request-count
# assertions in other integration tests.
ttl_blob = "1h"
lru_threshold = 90

[auth]
enabled = false
jwt_secret = "integration-artifact-signing-secret-0123456789abcdef"

[supply_chain.blocklist]
enabled = false

[cascade]
enabled = true
token = "%s"

[[cascade.peers]]
name = "parent"
url  = "%s"

[[npm.upstreams]]
name = "via-parent"
url = "%s"
priority = 1
probe_mode = "passive"
probe_interval = "30m"
via = "parent"

[[pypi.upstreams]]
name = "via-parent"
url = "%s"
priority = 1
probe_mode = "passive"
probe_interval = "30m"
via = "parent"
`, port, dir, dir, integrationCascadeToken, depsiloURL, mockServer.URL(), mockServer.URL())
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

func childFetch(t *testing.T, url string) string {
	t.Helper()
	response := httpGet(t, url)
	defer response.Body.Close()
	assertStatus(t, response, http.StatusOK)
	return readBody(t, response)
}

func TestCascadeChildFetchesThroughParentCache(t *testing.T) {
	childDir := t.TempDir()
	childPort := getFreePort()
	writeCascadeChildConfig(t, childDir, childPort)

	ctx, cancel := context.WithCancel(context.Background())
	done, err := startDepsilo(ctx, childDir)
	if err != nil {
		cancel()
		t.Fatalf("start cascade child: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		if err := waitForDepsiloShutdown(done); err != nil {
			t.Errorf("stop cascade child: %v", err)
		}
	})
	childURL := fmt.Sprintf("http://127.0.0.1:%d", childPort)
	if err := waitForReady(childURL+"/ready", 20*time.Second); err != nil {
		t.Fatalf("cascade child not ready: %v", err)
	}

	// Metadata: the parent performs the only origin fetch; the child then
	// serves the second request from its own cache.
	beforeMetadata := countMockPath("/cascade-fixture")
	for attempt := 0; attempt < 2; attempt++ {
		body := childFetch(t, childURL+"/npm/cascade-fixture")
		if !strings.Contains(body, `"cascade-fixture"`) {
			t.Fatalf("metadata body = %s", body)
		}
	}
	if got := countMockPath("/cascade-fixture") - beforeMetadata; got != 1 {
		t.Fatalf("origin metadata fetches = %d, want 1 (parent miss + child hit)", got)
	}

	// The parent relay cache serves the same raw exchange to another node
	// without contacting the origin again.
	probeRequest, err := http.NewRequest(http.MethodGet, depsiloURL+cascade.RelayPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	probeRequest.Header.Set(cascade.HeaderToken, integrationCascadeToken)
	probeRequest.Header.Set(cascade.HeaderChain, "probe-child")
	probeRequest.Header.Set(cascade.HeaderTarget, mockServer.URL()+"/cascade-fixture")
	probeRequest.Header.Set(cascade.HeaderSource, mockServer.URL())
	probeRequest.Header.Set(cascade.HeaderTTL, "300")
	probeRequest.Header.Set(cascade.HeaderKind, cascade.KindIndex)
	beforeProbe := countMockPath("/cascade-fixture")
	probeResponse, err := http.DefaultClient.Do(probeRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer probeResponse.Body.Close()
	if probeResponse.StatusCode != http.StatusOK {
		t.Fatalf("relay probe status = %d", probeResponse.StatusCode)
	}
	if got := probeResponse.Header.Get(cascade.HeaderCache); got != cascade.CacheHit {
		t.Fatalf("relay probe cache header = %q, want HIT", got)
	}
	if got := countMockPath("/cascade-fixture") - beforeProbe; got != 0 {
		t.Fatalf("relay probe fetched origin %d times, want 0", got)
	}

	// PyPI artifacts are the regression this design fixes: the child must be
	// able to resolve the parent's raw index and download the artifact through
	// the relay instead of re-deriving a broken first-party path.
	index := childFetch(t, childURL+"/pypi/simple/cascadepkg/")
	if !strings.Contains(index, childURL+"/pypi/files/") {
		t.Fatalf("child index was not rewritten to the child origin: %s", index)
	}
	linkPattern := regexp.MustCompile(`href="(` + regexp.QuoteMeta(childURL) + `/pypi/files/[^"#]+)`)
	match := linkPattern.FindStringSubmatch(index)
	if len(match) != 2 {
		t.Fatalf("child index has no artifact link: %s", index)
	}
	artifact := childFetch(t, match[1])
	if artifact != "FAKE_PACKAGE_DATA_1234567890" {
		t.Fatalf("artifact body = %q", artifact)
	}
}
