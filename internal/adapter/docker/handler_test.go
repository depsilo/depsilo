package docker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/egress"
)

func TestTagListPreservesQueryAndIsolatesCachedPages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var tagRequests atomic.Int64
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
		case "/v2/library/widget/tags/list":
			tagRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"query":%q}`, request.URL.RawQuery)
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(registry.Close)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "docker-query.db"))
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
	handler := New(manager, config.CacheConfig{TTLIndex: time.Hour}, database, config.DockerConfig{
		DefaultRegistry: "test",
		Registries: []config.RegistryConfig{{
			Name: "test",
			URL:  registry.URL,
		}},
	}, egress.Policy{})
	router := gin.New()
	handler.Register(router.Group("/v2"))

	requestPage := func(target string) string {
		t.Helper()
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, body=%s", target, response.Code, response.Body.String())
		}
		return response.Body.String()
	}

	if got := requestPage("/v2/library/widget/tags/list?n=1&last=alpha"); got != `{"query":"n=1&last=alpha"}` {
		t.Fatalf("first page body = %s", got)
	}
	if got := requestPage("/v2/library/widget/tags/list?n=1&last=beta"); got != `{"query":"n=1&last=beta"}` {
		t.Fatalf("second page body = %s", got)
	}
	if got := requestPage("/v2/library/widget/tags/list?n=1&last=alpha"); got != `{"query":"n=1&last=alpha"}` {
		t.Fatalf("cached first page body = %s", got)
	}
	if got := tagRequests.Load(); got != 2 {
		t.Fatalf("tag-list upstream requests = %d, want 2 distinct pages", got)
	}
}

func TestManifestBoundsUpstreamErrorBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/":
			_, _ = io.WriteString(w, `{}`)
		case "/v2/library/widget/manifests/latest":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, strings.Repeat("x", 256<<10)+"DO_NOT_INCLUDE")
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(registry.Close)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "docker-error.db"))
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
	handler := New(manager, config.CacheConfig{TTLIndex: time.Hour}, database, config.DockerConfig{
		DefaultRegistry: "test",
		Registries:      []config.RegistryConfig{{Name: "test", URL: registry.URL}},
	}, egress.Policy{})
	router := gin.New()
	handler.Register(router.Group("/v2"))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v2/library/widget/manifests/latest", nil)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "DO_NOT_INCLUDE") {
		t.Fatal("public error included bytes beyond the bounded diagnostic prefix")
	}
	if response.Body.Len() > maxDockerErrorBodyBytes+1024 {
		t.Fatalf("public error bytes = %d, want bounded response", response.Body.Len())
	}
}

// Clients such as containerd resolve a tag with HEAD before pulling. While
// the registry is unreachable, the cached manifest answers that HEAD, so a
// previously pulled image keeps pulling offline.
func TestManifestHeadAnswersFromCacheWhileTheRegistryIsUnreachable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const mediaType = "application/vnd.oci.image.index.v1+json"
	manifest := `{"schemaVersion":2,"mediaType":"` + mediaType + `","manifests":[]}`
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(manifest)))
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/":
			_, _ = io.WriteString(w, `{}`)
		case "/v2/library/widget/manifests/1.0":
			w.Header().Set("Content-Type", mediaType)
			w.Header().Set("Docker-Content-Digest", digest)
			w.Header().Set("Content-Length", fmt.Sprint(len(manifest)))
			if request.Method == http.MethodGet {
				_, _ = io.WriteString(w, manifest)
			}
		default:
			http.NotFound(w, request)
		}
	}))
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "docker-head.db"))
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
		_ = manager.Close(ctx)
	})
	// An expired index TTL makes every offline read take the stale path.
	handler := New(manager, config.CacheConfig{TTLIndex: time.Millisecond, TTLBlob: time.Hour}, database, config.DockerConfig{
		DefaultRegistry: "test",
		Registries:      []config.RegistryConfig{{Name: "test", URL: registry.URL}},
	}, egress.Policy{})
	router := gin.New()
	handler.Register(router.Group("/v2"))
	request := func(method, target string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, target, nil))
		return response
	}

	if got := request(http.MethodGet, "/v2/library/widget/manifests/1.0"); got.Code != http.StatusOK || got.Body.String() != manifest {
		t.Fatalf("online GET = %d %q", got.Code, got.Body.String())
	}
	if got := request(http.MethodHead, "/v2/library/widget/manifests/1.0"); got.Code != http.StatusOK || got.Header().Get("Docker-Content-Digest") != digest {
		t.Fatalf("online HEAD = %d digest %q", got.Code, got.Header().Get("Docker-Content-Digest"))
	}

	registry.Close()
	time.Sleep(5 * time.Millisecond)
	head := request(http.MethodHead, "/v2/library/widget/manifests/1.0")
	if head.Code != http.StatusOK || head.Header().Get("Docker-Content-Digest") != digest ||
		head.Header().Get("Content-Type") != mediaType || head.Header().Get("Content-Length") != fmt.Sprint(len(manifest)) || head.Body.Len() != 0 {
		t.Fatalf("offline HEAD = %d headers=%v body=%q", head.Code, head.Header(), head.Body.String())
	}
	if got := request(http.MethodGet, "/v2/library/widget/manifests/1.0"); got.Code != http.StatusOK || got.Body.String() != manifest {
		t.Fatalf("offline GET = %d %q", got.Code, got.Body.String())
	}
	if got := request(http.MethodHead, "/v2/library/widget/manifests/2.0"); got.Code != http.StatusBadGateway {
		t.Fatalf("offline HEAD of an uncached tag = %d, want 502", got.Code)
	}
}

// newRoutedDockerRouter serves one registry whose client resolves and dials
// through the given fakes.
func newRoutedDockerRouter(
	t *testing.T,
	registry config.RegistryConfig,
	policy egress.Policy,
	lookup ipResolver,
	dialContext func(context.Context, string, string) (net.Conn, error),
	observationRequired bool,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "docker-routed.db"))
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
		_ = manager.Close(ctx)
	})
	dockerCfg := config.DockerConfig{DefaultRegistry: registry.Name, Registries: []config.RegistryConfig{registry}}
	handler := New(manager, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour}, database, dockerCfg, policy)
	handler.resolver = newResolver(dockerCfg, policy, lookup, dialContext)
	handler.SetProvenanceRequired(observationRequired)
	router := gin.New()
	handler.Register(router.Group("/v2"))
	return router
}

// A HEAD answer has no body, and the observation gate's tag resolution fails
// before any fetch is logged, so the server log is where an operator reads
// why such a manifest request answered 502, for example the fake-IP remedy.
func TestManifestBadGatewayLogsItsReason(t *testing.T) {
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Location", "http://cdn.example/manifest")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(registry.Close)

	fakeIPRegistry := staticIPResolver{"registry.example": {{IP: net.ParseIP("198.18.0.10")}}}
	for _, tt := range []struct {
		name        string
		method      string
		observation bool
		answers     staticIPResolver
		wantLogged  string
	}{
		{
			name:       "HEAD whose token discovery is refused",
			method:     http.MethodHead,
			answers:    fakeIPRegistry,
			wantLogged: "registry.example resolved to 198.18.0.10, a fake-IP address",
		},
		{
			name:   "HEAD whose redirect is refused",
			method: http.MethodHead,
			answers: staticIPResolver{
				"registry.example": {{IP: net.ParseIP("93.184.216.34")}},
				"cdn.example":      {{IP: net.ParseIP("198.18.0.12")}},
			},
			wantLogged: "cdn.example resolved to 198.18.0.12, a fake-IP address",
		},
		{
			name:        "GET whose tag resolution for the observation gate is refused",
			method:      http.MethodGet,
			observation: true,
			answers:     fakeIPRegistry,
			wantLogged:  "registry.example resolved to 198.18.0.10, a fake-IP address",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zap.ErrorLevel)
			t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
			dialer := &routedDialer{direct: registry.Listener.Addr().String()}
			router := newRoutedDockerRouter(t, config.RegistryConfig{Name: "hub", URL: "http://registry.example"},
				egress.Policy{}, tt.answers, dialer.DialContext, tt.observation)

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(tt.method, "/v2/library/widget/manifests/latest", nil))
			if response.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want 502", response.Code)
			}
			var logged []string
			for _, entry := range logs.All() {
				logged = append(logged, fmt.Sprint(entry.ContextMap()["error"]))
			}
			if !slices.ContainsFunc(logged, func(text string) bool { return strings.Contains(text, tt.wantLogged) }) {
				t.Fatalf("logged errors = %q, want one containing %q", logged, tt.wantLogged)
			}
		})
	}
}

// Anonymous clients read a 502 body. The transport's error for a proxy it
// cannot reach names the proxy's address, so the body says only that the
// proxy connection failed, and the server log keeps the address.
func TestUnreachableProxyAddressStaysOutOfTheBadGatewayBody(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyAddress := listener.Addr().String()
	_ = listener.Close()
	policy, err := egress.NewPolicy("http://operator:proxy-secret@"+proxyAddress, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	core, logs := observer.New(zap.ErrorLevel)
	t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
	var dialer net.Dialer
	router := newRoutedDockerRouter(t, config.RegistryConfig{Name: "hub", URL: "http://registry.example"}, policy,
		staticIPResolver{"registry.example": {{IP: net.ParseIP("93.184.216.34")}}}, dialer.DialContext, false)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/library/widget/manifests/latest", nil))
	var body struct{ Code, Message string }
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("status = %d, body = %s: %v", response.Code, response.Body.String(), err)
	}
	const want = `auth discovery failed: Get "http://registry.example/v2/": ` +
		"connection to the egress proxy failed (details in the Depsilo server log)"
	if response.Code != http.StatusBadGateway || body.Code != "UPSTREAM_UNAVAILABLE" || body.Message != want {
		t.Fatalf("answer = %d %+v, want 502 UPSTREAM_UNAVAILABLE %q", response.Code, body, want)
	}

	var logged []string
	for _, entry := range logs.All() {
		logged = append(logged, fmt.Sprint(entry.ContextMap()["error"]))
	}
	if !slices.ContainsFunc(logged, func(text string) bool {
		return strings.Contains(text, "proxyconnect") && strings.Contains(text, proxyAddress)
	}) {
		t.Fatalf("logged errors = %q, want the proxy connection failure with %s", logged, proxyAddress)
	}
}
