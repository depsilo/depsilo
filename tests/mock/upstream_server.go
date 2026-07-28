package mock

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RecordedRequest stores details of an HTTP request received by the mock.
type RecordedRequest struct {
	Method string
	Path   string
	Time   time.Time
}

// MockUpstream is a configurable mock HTTP server for testing upstream interactions.
type MockUpstream struct {
	server     *httptest.Server
	mux        *http.ServeMux
	mu         sync.Mutex
	requests   []RecordedRequest
	tamperBody atomic.Value // string; the current bytes served at /tamperpkg/-/tamperpkg-1.0.0.tgz
}

// NewMockUpstream creates and starts a new mock upstream server.
func NewMockUpstream() *MockUpstream {
	m := &MockUpstream{
		mux: http.NewServeMux(),
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.requests = append(m.requests, RecordedRequest{Method: r.Method, Path: r.URL.Path, Time: time.Now()})
		m.mu.Unlock()
		m.mux.ServeHTTP(w, r)
	}))
	return m
}

// URL returns the base URL of the mock server.
func (m *MockUpstream) URL() string { return m.server.URL }

// Close shuts down the mock server.
func (m *MockUpstream) Close() { m.server.Close() }

// Requests returns a copy of all recorded requests.
func (m *MockUpstream) Requests() []RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]RecordedRequest, len(m.requests))
	copy(cp, m.requests)
	return cp
}

// RequestCount returns the number of recorded requests.
func (m *MockUpstream) RequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

// --- Register mock responses for each ecosystem ---

// RegisterPyPI adds PyPI-compatible endpoints.
func (m *MockUpstream) RegisterPyPI() {
	m.mux.HandleFunc("/simple/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><body><a href="https://files.pythonhosted.org/packages/ab/cd/testpkg-1.0.0.tar.gz#sha256=abcd1234">testpkg-1.0.0.tar.gz</a></body></html>`)
	})
	m.mux.HandleFunc("/packages/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write([]byte("FAKE_PACKAGE_DATA_1234567890"))
	})
}

// RegisterAPT adds APT repository endpoints.
func (m *MockUpstream) RegisterAPT() {
	m.mux.HandleFunc("/ubuntu/dists/jammy/Release", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "Origin: Ubuntu\nLabel: Ubuntu\nSuite: jammy\nCodename: jammy\n")
	})
	m.mux.HandleFunc("/ubuntu/dists/jammy/main/binary-amd64/Packages.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write([]byte("FAKE_PACKAGES_GZ"))
	})
	m.mux.HandleFunc("/ubuntu/pool/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		w.Write([]byte("FAKE_DEB_PACKAGE"))
	})
}

// RegisterNpm adds npm registry endpoints.
func (m *MockUpstream) RegisterNpm() {
	m.mux.HandleFunc("/testpkg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":"testpkg","dist-tags":{"latest":"1.0.0"},"versions":{"1.0.0":{"name":"testpkg","version":"1.0.0","dist":{"tarball":"%s/testpkg/-/testpkg-1.0.0.tgz","shasum":"abc123","integrity":"sha512-test"}}}}`, m.URL())
	})
	m.mux.HandleFunc("/testpkg/-/testpkg-1.0.0.tgz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write([]byte("FAKE_NPM_TARBALL"))
	})
}

// RegisterGoModules adds Go module proxy endpoints.
func (m *MockUpstream) RegisterGoModules() {
	m.mux.HandleFunc("/github.com/test/mod/@v/list", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "v1.0.0\nv1.1.0\n")
	})
	m.mux.HandleFunc("/github.com/test/mod/@v/v1.0.0.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}`)
	})
	m.mux.HandleFunc("/github.com/test/mod/@v/v1.0.0.mod", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "module github.com/test/mod\n\ngo 1.21\n")
	})
	m.mux.HandleFunc("/github.com/test/mod/@v/v1.0.0.zip", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("FAKE_GO_ZIP"))
	})
	m.mux.HandleFunc("/github.com/test/mod/@latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"Version":"v1.1.0","Time":"2024-06-01T00:00:00Z"}`)
	})
}

// RegisterCargo adds Cargo registry endpoints.
func (m *MockUpstream) RegisterCargo() {
	m.mux.HandleFunc("/config.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"dl":"%s/api/v1/crates","api":"%s"}`, m.URL(), m.URL())
	})
	m.mux.HandleFunc("/se/rd/serde", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"name":"serde","vers":"1.0.0","cksum":"abc123","deps":[],"features":{},"yanked":false}`)
	})
	m.mux.HandleFunc("/api/v1/crates/serde/1.0.0/download", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("FAKE_CRATE"))
	})
}

// RegisterMaven adds Maven repository endpoints.
func (m *MockUpstream) RegisterMaven() {
	m.mux.HandleFunc("/org/example/test/1.0.0/test-1.0.0.pom", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<project><groupId>org.example</groupId><artifactId>test</artifactId><version>1.0.0</version></project>`)
	})
	m.mux.HandleFunc("/org/example/test/1.0.0/test-1.0.0.jar", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("FAKE_JAR"))
	})
	m.mux.HandleFunc("/org/example/test/maven-metadata.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<metadata><groupId>org.example</groupId><artifactId>test</artifactId><versioning><latest>1.0.0</latest><versions><version>1.0.0</version></versions></versioning></metadata>`)
	})
}

// RegisterRubyGems adds RubyGems repository endpoints.
func (m *MockUpstream) RegisterRubyGems() {
	m.mux.HandleFunc("/specs.4.8.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("FAKE_SPECS"))
	})
	m.mux.HandleFunc("/info/testgem", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "---\n1.0.0 |checksum:abc123\n")
	})
	m.mux.HandleFunc("/gems/testgem-1.0.0.gem", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("FAKE_GEM"))
	})
}

// RegisterComposer adds Composer/Packagist endpoints.
func (m *MockUpstream) RegisterComposer() {
	m.mux.HandleFunc("/packages.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"packages":{},"metadata-url":"/p2/%package%.json"}`))
	})
	m.mux.HandleFunc("/p2/test/pkg.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// dist.url points back at this mock (resolved per-request
		// from the Host header) so the proxy's absolute dist fetch
		// stays inside the test sandbox.
		fmt.Fprintf(w, `{"packages":{"test/pkg":[{"name":"test/pkg","version":"1.0.0","version_normalized":"1.0.0.0","dist":{"url":"http://%s/composer-dist/test.zip","type":"zip","reference":"abc"}}]}}`, r.Host)
	})
	m.mux.HandleFunc("/composer-dist/test.zip", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("FAKE_COMPOSER_DIST"))
	})
}

// RegisterNuGet adds NuGet V3 endpoints.
func (m *MockUpstream) RegisterNuGet() {
	m.mux.HandleFunc("/v3/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"version":"3.0.0","resources":[{"@id":"https://api.nuget.org/v3/search","@type":"SearchQueryService"},{"@id":"https://api.nuget.org/v3/registration","@type":"RegistrationsBaseUrl"}]}`)
	})
	m.mux.HandleFunc("/v3/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	})
}

// RegisterConda adds Conda repository endpoints.
func (m *MockUpstream) RegisterConda() {
	m.mux.HandleFunc("/pkgs/main/linux-64/repodata.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"info":{"subdir":"linux-64"},"packages":{"testpkg-1.0.0.tar.bz2":{"name":"testpkg","version":"1.0.0"}}}`)
	})
	m.mux.HandleFunc("/pkgs/main/linux-64/testpkg-1.0.0.tar.bz2", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("FAKE_CONDA_PKG"))
	})
}

// RegisterCRAN adds CRAN repository endpoints.
func (m *MockUpstream) RegisterCRAN() {
	m.mux.HandleFunc("/src/contrib/PACKAGES", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Package: testpkg\nVersion: 1.0.0\n\n")
	})
	m.mux.HandleFunc("/src/contrib/testpkg_1.0.0.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("FAKE_CRAN_PKG"))
	})
}

// RegisterHelm adds Helm chart repository endpoints.
func (m *MockUpstream) RegisterHelm() {
	m.mux.HandleFunc("/index.yaml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "apiVersion: v1\nentries:\n  testchart:\n  - name: testchart\n    version: 1.0.0\n    urls:\n    - %s/testchart-1.0.0.tgz\n", m.URL())
	})
	m.mux.HandleFunc("/testchart-1.0.0.tgz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("FAKE_HELM_CHART"))
	})
}

// RegisterHuggingFace adds HuggingFace Hub endpoints — model metadata,
// tree listing, and the resolve flow with a 302 redirect to a tracked
// "CDN" path served by the same mock server.
func (m *MockUpstream) RegisterHuggingFace() {
	const commit = "abc1234abc1234abc1234abc1234abc1234abc12"

	// Model metadata endpoint
	m.mux.HandleFunc("/api/models/bert-base-uncased", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"modelId":"bert-base-uncased","sha":"%s","siblings":[{"rfilename":"config.json"},{"rfilename":"pytorch_model.bin"}]}`, commit)
	})

	// Tree listing
	m.mux.HandleFunc("/api/models/bert-base-uncased/tree/main", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `[{"path":"config.json","type":"file","size":645},{"path":"pytorch_model.bin","type":"file","size":11}]`)
	})

	// Dataset metadata (proves the datasets branch works too)
	m.mux.HandleFunc("/api/datasets/squad", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"id":"squad","sha":"def5678"}`)
	})

	// Direct 200 file (config.json — not LFS)
	smallFile := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Repo-Commit", commit)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"hidden":false}`)
	}
	m.mux.HandleFunc("/bert-base-uncased/resolve/main/config.json", smallFile)
	m.mux.HandleFunc("/bert-base-uncased/resolve/"+commit+"/config.json", smallFile)

	// LFS file (pytorch_model.bin) — 302 redirects to mock CDN path
	lfsFile := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Linked-Etag", "deadbeefcafe")
		w.Header().Set("X-Linked-Size", "11")
		w.Header().Set("X-Repo-Commit", commit)
		w.Header().Set("Location", m.URL()+"/cdn-lfs/pytorch_model.bin?sig=mock-sig")
		w.WriteHeader(302)
	}
	m.mux.HandleFunc("/bert-base-uncased/resolve/main/pytorch_model.bin", lfsFile)
	m.mux.HandleFunc("/bert-base-uncased/resolve/"+commit+"/pytorch_model.bin", lfsFile)

	// "CDN" path serving the actual bytes
	m.mux.HandleFunc("/cdn-lfs/pytorch_model.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "11")
		w.WriteHeader(200)
		w.Write([]byte("FAKE_WEIGHT"))
	})
}

// RegisterDocker adds Docker Registry V2 endpoints with Bearer token auth.
func (m *MockUpstream) RegisterDocker() {
	const mockToken = "mock-docker-token-12345"

	// Token endpoint — accepts any credentials, returns fixed token
	m.mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"token":"%s","expires_in":300}`, mockToken)
	})

	// /v2/ — returns 401 with WWW-Authenticate to trigger token flow
	m.mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		// Check if authenticated
		auth := r.Header.Get("Authorization")
		if auth == "Bearer "+mockToken {
			// Authenticated — serve registry endpoints
			path := strings.TrimPrefix(r.URL.Path, "/v2/")
			if path == "" || path == "/" {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{}`)
				return
			}

			if strings.Contains(path, "/manifests/") {
				w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
				w.Header().Set("Docker-Content-Digest", "sha256:fakedigest")
				fmt.Fprint(w, `{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json","config":{"digest":"sha256:fakeconfig"}}`)
				return
			}
			if strings.Contains(path, "/blobs/") {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Write([]byte("FAKE_DOCKER_BLOB_DATA"))
				return
			}
			if strings.HasSuffix(path, "/tags/list") {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"name":"library/testimg","tags":["latest","v1.0"]}`)
				return
			}
			http.NotFound(w, r)
			return
		}

		// Not authenticated — send challenge
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/auth/token",service="mock-registry"`, m.URL()))
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"errors":[{"code":"UNAUTHORIZED"}]}`)
	})
}

// SetTamperBody swaps the bytes the tamper test endpoint serves, so a
// test can simulate an upstream silently changing an immutable artifact.
func (m *MockUpstream) SetTamperBody(s string) { m.tamperBody.Store(s) }

// RegisterTamper serves a fixed npm-shaped metadata doc plus a tarball
// whose bytes are controlled by SetTamperBody.
func (m *MockUpstream) RegisterTamper() {
	m.tamperBody.Store("ORIGINAL-BYTES")
	m.mux.HandleFunc("/tamperpkg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":"tamperpkg","dist-tags":{"latest":"1.0.0"},"versions":{"1.0.0":{"name":"tamperpkg","version":"1.0.0","dist":{"tarball":"%s/tamperpkg/-/tamperpkg-1.0.0.tgz","shasum":"x","integrity":"sha512-x"}}}}`, m.URL())
	})
	m.mux.HandleFunc("/tamperpkg/-/tamperpkg-1.0.0.tgz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write([]byte(m.tamperBody.Load().(string)))
	})
}

// RegisterAll registers mock responses for all supported ecosystems.
func (m *MockUpstream) RegisterAll() {
	m.RegisterPyPI()
	m.RegisterAPT()
	m.RegisterNpm()
	m.RegisterGoModules()
	m.RegisterCargo()
	m.RegisterMaven()
	m.RegisterRubyGems()
	m.RegisterComposer()
	m.RegisterNuGet()
	m.RegisterConda()
	m.RegisterCRAN()
	m.RegisterHelm()
	m.RegisterHuggingFace()
	m.RegisterDocker()
	m.RegisterOSVBlocklist()
	m.RegisterTamper()
}

// RegisterOSVBlocklist serves the OSV bulk-data layout the blocklist
// syncer downloads ({Ecosystem}/all.zip). Every ecosystem archive is
// the same tiny zip holding one MAL advisory that marks npm's
// "malicious-pkg" (every version) as malware — enough to drive the
// end-to-end 451 MALICIOUS_BLOCKED test.
func (m *MockUpstream) RegisterOSVBlocklist() {
	const advisory = `{
		"id": "MAL-2026-0001",
		"summary": "malicious-pkg exfiltrates environment variables",
		"modified": "2026-07-01T00:00:00Z",
		"affected": [{
			"package": {"ecosystem": "npm", "name": "malicious-pkg"},
			"ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}]}]
		}]
	}`
	const goAdvisory = `{
		"id": "MAL-2026-0002",
		"summary": "evil Go module runs curl|sh in go generate",
		"modified": "2026-07-01T00:00:00Z",
		"affected": [{
			"package": {"ecosystem": "Go", "name": "github.com/evil/module"},
			"ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}]}]
		}]
	}`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("MAL-2026-0001.json")
	_, _ = w.Write([]byte(advisory))
	w2, _ := zw.Create("MAL-2026-0002.json")
	_, _ = w2.Write([]byte(goAdvisory))
	_ = zw.Close()
	archive := buf.Bytes()

	for _, eco := range []string{"npm", "PyPI", "crates.io", "RubyGems", "Packagist", "NuGet", "Go", "Maven"} {
		m.mux.HandleFunc("/"+eco+"/all.zip", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(archive)
		})
	}
}
