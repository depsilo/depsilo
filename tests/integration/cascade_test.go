//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/cascade"
)

// Each node has a new database and cache. Passive probes and a disabled
// blocklist keep all source traffic attributable to this test's requests.
func startCascadeNode(t *testing.T, originURL, parentURL string, directFallback bool) string {
	t.Helper()
	dir := t.TempDir()
	port := getFreePort()
	var config strings.Builder
	fmt.Fprintf(&config, `
[server]
host = "127.0.0.1"
port = %d

[database]
driver = "sqlite"
dsn = %q

[storage]
type = "local"
path = %q

[cache]
max_size_gb = 1
ttl_index = "1h"
ttl_blob = "1h"
lru_threshold = 90

[auth]
enabled = false
jwt_secret = "integration-artifact-signing-secret-0123456789abcdef"

[supply_chain]
min_release_age_enabled = false

[supply_chain.blocklist]
enabled = false

[cascade]
enabled = true
token = %q
`, port, filepath.Join(dir, "node.db"), filepath.Join(dir, "cache"), integrationCascadeToken)
	if parentURL != "" {
		fmt.Fprintf(&config, "\n[[cascade.peers]]\nname = \"parent\"\nurl = %q\n", parentURL)
	}
	for _, ecosystem := range []string{"npm", "pypi"} {
		fmt.Fprintf(&config, "\n[[%s.upstreams]]\nname = \"primary\"\nurl = %q\npriority = 1\nprobe_mode = \"passive\"\nprobe_interval = \"30m\"\n", ecosystem, originURL)
		if parentURL != "" {
			config.WriteString("via = \"parent\"\n")
		}
		if directFallback {
			fmt.Fprintf(&config, "\n[[%s.upstreams]]\nname = \"direct\"\nurl = %q\npriority = 2\nprobe_mode = \"passive\"\nprobe_interval = \"30m\"\n", ecosystem, originURL)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done, err := startDepsilo(ctx, dir)
	if err != nil {
		cancel()
		t.Fatalf("start cascade node: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		if err := waitForDepsiloShutdown(done); err != nil {
			t.Errorf("stop cascade node: %v", err)
		}
	})
	nodeURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitForReady(nodeURL+"/ready", 20*time.Second); err != nil {
		t.Fatalf("cascade node not ready: %v", err)
	}
	return nodeURL
}

func cascadeFetch(t *testing.T, url string) string {
	t.Helper()
	response := httpGet(t, url)
	defer response.Body.Close()
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d, body %s", url, response.StatusCode, body)
	}
	return body
}

func cascadeMetric(t *testing.T, nodeURL, series string) float64 {
	t.Helper()
	for _, line := range strings.Split(cascadeFetch(t, nodeURL+"/metrics"), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == series {
			value, err := strconv.ParseFloat(fields[1], 64)
			if err != nil {
				t.Fatalf("parse metric %s: %v", series, err)
			}
			return value
		}
	}
	t.Fatalf("metric %s missing from %s", series, nodeURL)
	return 0
}

func TestCascadeChildrenReuseParentCache(t *testing.T) {
	const npmArtifact = "complete npm artifact fixture"
	const pypiArtifact = "complete Python artifact fixture"
	var mu sync.Mutex
	requests := make(map[string]int)
	var origin *httptest.Server
	origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/cascade-fixture":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"name":"cascade-fixture","versions":{"1.0.0":{"name":"cascade-fixture","version":"1.0.0","dist":{"tarball":"%s/cascade-fixture/-/cascade-fixture-1.0.0.tgz"}}}}`, origin.URL)
		case "/cascade-fixture/-/cascade-fixture-1.0.0.tgz":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = io.WriteString(w, npmArtifact)
		case "/simple/cascadepkg/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body><a href="%s/packages/cascadepkg-1.0.0-py3-none-any.whl">cascadepkg-1.0.0-py3-none-any.whl</a></body></html>`, origin.URL)
		case "/packages/cascadepkg-1.0.0-py3-none-any.whl":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, pypiArtifact)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(origin.Close)
	parent := startCascadeNode(t, origin.URL, "", false)
	children := []string{
		startCascadeNode(t, origin.URL, parent, false),
		startCascadeNode(t, origin.URL, parent, false),
	}
	for childIndex, child := range children {
		// Each child starts cold and has to consume and rewrite the raw parent
		// metadata itself. Repetition then exercises the child's own cache.
		for attempt := 0; attempt < 2; attempt++ {
			raw := cascadeFetch(t, child+"/npm/cascade-fixture")
			var packument struct {
				Versions map[string]struct {
					Dist struct {
						Tarball string `json:"tarball"`
					} `json:"dist"`
				} `json:"versions"`
			}
			if err := json.Unmarshal([]byte(raw), &packument); err != nil {
				t.Fatalf("decode child npm metadata: %v", err)
			}
			tarball := packument.Versions["1.0.0"].Dist.Tarball
			if !strings.HasPrefix(tarball, child+"/npm/") {
				t.Fatalf("npm artifact URL %q does not belong to child %s", tarball, child)
			}
			if body := cascadeFetch(t, tarball); body != npmArtifact {
				t.Fatalf("npm artifact = %q", body)
			}

			index := cascadeFetch(t, child+"/pypi/simple/cascadepkg/")
			match := regexp.MustCompile(`href="(` + regexp.QuoteMeta(child) + `/pypi/files/[^"#]+)`).FindStringSubmatch(index)
			if len(match) != 2 {
				t.Fatalf("PyPI index has no child artifact link: %s", index)
			}
			if body := cascadeFetch(t, html.UnescapeString(match[1])); body != pypiArtifact {
				t.Fatalf("PyPI artifact = %q", body)
			}
		}
		mu.Lock()
		for _, path := range []string{"/cascade-fixture", "/cascade-fixture/-/cascade-fixture-1.0.0.tgz", "/simple/cascadepkg/", "/packages/cascadepkg-1.0.0-py3-none-any.whl"} {
			if got := requests[path]; got != 1 {
				t.Errorf("after child %d, origin GET %s = %d, want exactly 1", childIndex+1, path, got)
			}
		}
		mu.Unlock()
		if got := cascadeMetric(t, child, `depsilo_upstream_exchanges_total{route="direct"}`); got != 0 {
			t.Errorf("child direct upstream exchanges = %v, want 0", got)
		}
		if got := cascadeMetric(t, child, `depsilo_upstream_exchanges_total{route="peer"}`); got != 4 {
			t.Errorf("child peer exchanges = %v, want 4 cold metadata/artifact fetches", got)
		}
	}
	if got := cascadeMetric(t, parent, `depsilo_upstream_exchanges_total{route="direct"}`); got != 4 {
		t.Errorf("parent direct exchanges = %v, want 4; second child must reuse parent cache", got)
	}
	if got := cascadeMetric(t, parent, `depsilo_upstream_exchanges_total{route="peer"}`); got != 0 {
		t.Errorf("parent peer exchanges = %v, want 0", got)
	}
	parentBytes := cascadeMetric(t, parent, `depsilo_upstream_response_bytes_total{route="direct"}`)
	for _, child := range children {
		if got := cascadeMetric(t, child, `depsilo_upstream_response_bytes_total{route="peer"}`); got != parentBytes {
			t.Errorf("child peer bytes = %v, want one copy of parent source bytes %v", got, parentBytes)
		}
	}
}

func TestCascadeConcurrentRequestsShareParentFill(t *testing.T) {
	const workers = 8
	payload := strings.Repeat("complete streamed artifact\n", 8192)
	var originRequests atomic.Int64
	entered := make(chan struct{}, workers)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseOrigin := func() { releaseOnce.Do(func() { close(release) }) }
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originRequests.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
			_, _ = io.WriteString(w, payload)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(origin.Close)
	t.Cleanup(releaseOrigin)
	parent := startCascadeNode(t, origin.URL, "", false)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	written := make(chan struct{}, workers)
	results := make(chan error, workers)
	start := make(chan struct{})
	for worker := 0; worker < workers; worker++ {
		go func() {
			<-start
			requestCtx := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
				WroteRequest: func(httptrace.WroteRequestInfo) { written <- struct{}{} },
			})
			req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, parent+cascade.RelayPath, nil)
			if err != nil {
				results <- err
				return
			}
			req.Header.Set(cascade.HeaderToken, integrationCascadeToken)
			req.Header.Set(cascade.HeaderChain, fmt.Sprintf("worker-%d", worker))
			req.Header.Set(cascade.HeaderTarget, origin.URL+"/same-artifact")
			req.Header.Set(cascade.HeaderSource, origin.URL)
			req.Header.Set(cascade.HeaderKind, cascade.KindArtifact)
			req.Header.Set(cascade.HeaderTTL, "3600")
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				results <- err
				return
			}
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil || response.StatusCode != http.StatusOK || string(body) != payload {
				results <- fmt.Errorf("worker %d: status=%d size=%d read=%v", worker, response.StatusCode, len(body), readErr)
				return
			}
			results <- nil
		}()
	}
	close(start)
	// All callers have sent their cold request before the origin can finish.
	// The test uses one method, target, Accept variant and relay namespace;
	// it does not assume deduplication across different cache keys.
	for range workers {
		select {
		case <-written:
		case <-ctx.Done():
			t.Fatal("concurrent relay callers did not send requests before deadline")
		}
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("parent did not fetch the cold artifact")
	}
	releaseOrigin()
	for range workers {
		select {
		case err := <-results:
			if err != nil {
				t.Error(err)
			}
		case <-ctx.Done():
			t.Fatal("concurrent relay requests did not finish")
		}
	}
	if got := originRequests.Load(); got != 1 {
		t.Errorf("origin artifact fetches = %d, want 1 for %d successful callers", got, workers)
	}
	if got := cascadeMetric(t, parent, `depsilo_upstream_response_bytes_total{route="direct"}`); got != float64(len(payload)) {
		t.Errorf("parent direct bytes = %v, want one full artifact (%d)", got, len(payload))
	}
}

func TestCascadeDirectFallbackRequiresExplicitUpstream(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("direct_fallback_%t", fallback), func(t *testing.T) {
			var originRequests, parentRequests atomic.Int64
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				originRequests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"name":"fallback-fixture","versions":{}}`)
			}))
			t.Cleanup(origin.Close)
			parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				parentRequests.Add(1)
				http.Error(w, "parent unavailable", http.StatusServiceUnavailable)
			}))
			t.Cleanup(parent.Close)
			child := startCascadeNode(t, origin.URL, parent.URL, fallback)

			// npm records the failed parent exchange before selection can use
			// the lower-priority direct source on the following request.
			for attempt := 0; attempt < 2; attempt++ {
				response := httpGet(t, child+"/npm/fallback-fixture")
				body := readBody(t, response)
				_ = response.Body.Close()
				want := http.StatusBadGateway
				if fallback && attempt == 1 {
					want = http.StatusOK
				}
				if response.StatusCode != want {
					t.Fatalf("attempt %d status=%d, want %d; body=%s", attempt+1, response.StatusCode, want, body)
				}
			}
			if parentRequests.Load() == 0 {
				t.Fatal("child never attempted its configured parent")
			}
			wantOrigin := int64(0)
			if fallback {
				wantOrigin = 1
			}
			if got := originRequests.Load(); got != wantOrigin {
				t.Errorf("direct source requests = %d, want %d", got, wantOrigin)
			}
			if got := cascadeMetric(t, child, `depsilo_upstream_exchanges_total{route="direct"}`); got != float64(wantOrigin) {
				t.Errorf("direct exchanges = %v, want %d", got, wantOrigin)
			}
		})
	}
}
