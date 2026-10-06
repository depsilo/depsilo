package pypi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/quarantine"
	"depsilo/internal/upstream"
)

func TestRewriteSignedJSONIndexCarriesUploadTimeAndRendersHTML(t *testing.T) {
	t.Parallel()
	document := `{
		"meta": {"api-version": "1.4"},
		"name": "demo",
		"files": [
			{
				"filename": "demo-1.0-py3-none-any.whl",
				"url": "https://cdn.example/demo-1.0-py3-none-any.whl",
				"hashes": {"sha256": "abc123"},
				"requires-python": ">=3.9",
				"upload-time": "2026-09-01T12:30:45.123456Z"
			},
			{
				"filename": "demo-0.9.tar.gz",
				"url": "https://cdn.example/demo-0.9.tar.gz"
			}
		]
	}`
	rewritten, err := rewriteSignedJSONIndex(
		[]byte(document),
		"/pypi",
		"https://index.example/simple/demo/",
		"pypi",
		"c291cmNl",
		testArtifactSigningKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Files []struct {
			URL string `json:"url"`
		} `json:"files"`
	}
	if err := json.Unmarshal(rewritten, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(parsed.Files))
	}
	claims := decodeTokenFromLocalURL(t, parsed.Files[0].URL, "pypi")
	if claims.Source != "c291cmNl" || claims.UploadTime != "2026-09-01T12:30:45.123456Z" {
		t.Fatalf("first claims = %#v", claims)
	}
	if second := decodeTokenFromLocalURL(t, parsed.Files[1].URL, "pypi"); second.UploadTime != "" {
		t.Fatalf("second claims = %#v, want no upload time", second)
	}

	rendered, err := renderSimpleHTMLFromJSON(rewritten)
	if err != nil {
		t.Fatal(err)
	}
	html := string(rendered)
	for _, want := range []string{
		`href="/pypi/files/_external/`,
		`#sha256=abc123`,
		`data-requires-python="&gt;=3.9"`,
		`demo-1.0-py3-none-any.whl`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered HTML missing %q: %s", want, html)
		}
	}
}

func decodeTokenFromLocalURL(t *testing.T, localURL, adapterID string) externalArtifactClaims {
	t.Helper()
	parsed, err := url.Parse(localURL)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(parsed.EscapedPath(), "/")
	marker := -1
	for index, part := range parts {
		if part == "_external" {
			marker = index
			break
		}
	}
	if marker < 0 || marker+1 >= len(parts) {
		t.Fatalf("local URL has no external token: %q", localURL)
	}
	claims, err := decodeExternalArtifactToken(testArtifactSigningKey, adapterID, parts[marker+1])
	if err != nil {
		t.Fatal(err)
	}
	return claims
}

func TestProvenanceIndexFlowFeedsUploadTimeToArtifactGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const artifactBody = "provenance-pypi-wheel"
	now := time.Now().UTC()
	youngTime := now.Add(-time.Hour)
	oldTime := now.Add(-30 * 24 * time.Hour)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/simple/demo/":
			if !strings.Contains(r.Header.Get("Accept"), "application/vnd.pypi.simple.v1+json") {
				t.Errorf("index request Accept = %q, want JSON negotiation", r.Header.Get("Accept"))
			}
			w.Header().Set("Content-Type", "application/vnd.pypi.simple.v1+json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"meta": map[string]any{"api-version": "1.4"},
				"name": "demo",
				"files": []any{
					map[string]any{
						"filename":    "demo-1.0-py3-none-any.whl",
						"url":         "http://" + r.Host + "/packages/demo-1.0-py3-none-any.whl",
						"hashes":      map[string]any{"sha256": "abc123"},
						"upload-time": youngTime.Format(time.RFC3339),
					},
					map[string]any{
						"filename":    "demo-2.0-py3-none-any.whl",
						"url":         "http://" + r.Host + "/packages/demo-2.0-py3-none-any.whl",
						"hashes":      map[string]any{"sha256": "def456"},
						"upload-time": oldTime.Format(time.RFC3339),
					},
				},
			})
		case "/packages/demo-1.0-py3-none-any.whl", "/packages/demo-2.0-py3-none-any.whl":
			_, _ = io.WriteString(w, artifactBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstreamServer.Close)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "pypi-json.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), 72*time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewWithOptions(
		manager,
		upstream.NewPassiveRecoverySelector(pool),
		config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour},
		database,
		Options{
			PathPrefix:         "/pypi",
			AdapterID:          "pypi",
			UpstreamSimplePath: "/simple",
			ArtifactSigningKey: testArtifactSigningKey,
			ProvenanceRequired: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router.Group("/pypi"))
	enabled := true
	policy, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"pypi": "168h"},
	}, func(ecosystem string) bool { return ecosystem == "pypi" })
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance: %v", err)
	}
	store := quarantine.NewStore(database)
	checker, err := quarantine.NewChecker(policy, quarantine.NewLookup(store, nil), store)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	scoped := adapter.NewRequestScope(nil, nil, quarantine.Wrap(checker), nil).Wrap(router)

	indexResponse := httptest.NewRecorder()
	indexRequest := httptest.NewRequest(http.MethodGet, "/pypi/simple/demo/", nil)
	indexRequest.Header.Set("Accept", "application/vnd.pypi.simple.v1+json")
	scoped.ServeHTTP(indexResponse, indexRequest)
	if indexResponse.Code != http.StatusOK ||
		!strings.Contains(indexResponse.Header().Get("Content-Type"), "json") {
		t.Fatalf("index status=%d type=%q body=%s",
			indexResponse.Code, indexResponse.Header().Get("Content-Type"), indexResponse.Body.String())
	}
	var index struct {
		Files []struct {
			Filename string `json:"filename"`
			URL      string `json:"url"`
		} `json:"files"`
	}
	if err := json.Unmarshal(indexResponse.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Files) != 2 {
		t.Fatalf("index files = %#v", index.Files)
	}
	signed := map[string]string{}
	for _, file := range index.Files {
		if !strings.Contains(file.URL, "/_external/") {
			t.Fatalf("index file is not signed: %#v", file)
		}
		signed[file.Filename] = file.URL
	}
	youngURL, youngOK := signed["demo-1.0-py3-none-any.whl"]
	oldURL, oldOK := signed["demo-2.0-py3-none-any.whl"]
	if !youngOK || !oldOK {
		t.Fatalf("signed files = %#v", signed)
	}

	youngParsed, err := url.Parse(youngURL)
	if err != nil {
		t.Fatal(err)
	}
	youngResponse := httptest.NewRecorder()
	scoped.ServeHTTP(youngResponse, httptest.NewRequest(http.MethodGet, youngParsed.RequestURI(), nil))
	if youngResponse.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(youngResponse.Body.String(), `"code":"QUARANTINED"`) ||
		!strings.Contains(youngResponse.Body.String(), "source ") {
		t.Fatalf("young artifact status=%d body=%s", youngResponse.Code, youngResponse.Body.String())
	}

	oldParsed, err := url.Parse(oldURL)
	if err != nil {
		t.Fatal(err)
	}
	oldResponse := httptest.NewRecorder()
	scoped.ServeHTTP(oldResponse, httptest.NewRequest(http.MethodGet, oldParsed.RequestURI(), nil))
	if oldResponse.Code != http.StatusOK || oldResponse.Body.String() != artifactBody {
		t.Fatalf("old artifact status=%d body=%q", oldResponse.Code, oldResponse.Body.String())
	}

	// An HTML-only client gets the rendered legacy representation while the
	// cached document keeps the signed references.
	htmlResponse := httptest.NewRecorder()
	htmlRequest := httptest.NewRequest(http.MethodGet, "/pypi/simple/demo/", nil)
	htmlRequest.Header.Set("Accept", "text/html")
	scoped.ServeHTTP(htmlResponse, htmlRequest)
	if htmlResponse.Code != http.StatusOK ||
		!strings.Contains(htmlResponse.Header().Get("Content-Type"), "html") ||
		!strings.Contains(htmlResponse.Body.String(), "/_external/") {
		t.Fatalf("html fallback status=%d type=%q body=%s",
			htmlResponse.Code, htmlResponse.Header().Get("Content-Type"), htmlResponse.Body.String())
	}
}
