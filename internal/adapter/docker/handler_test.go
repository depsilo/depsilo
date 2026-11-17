package docker

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"

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
	}, egress.Policy{}, nil)
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
	}, egress.Policy{}, nil)
	router := gin.New()
	handler.Register(router.Group("/v2"))

	core, logs := observer.New(zap.ErrorLevel)
	t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
	response := serveWithRequestID(router, http.MethodGet, "/v2/library/widget/manifests/latest", testRequestID)
	// The registry's answer stays out of the 502 body: only its status is
	// repeated, because it did not use the distribution error format.
	code, message := badGatewayBody(t, response)
	want := "manifest failed at " + registry.URL + ": returned 502" + testRequestSuffix
	if code != "UPSTREAM_UNAVAILABLE" || message != want {
		t.Fatalf("answer = %s %q, want UPSTREAM_UNAVAILABLE %q", code, message, want)
	}
	if strings.Contains(response.Body.String(), "xxxx") || strings.Contains(response.Body.String(), "DO_NOT_INCLUDE") {
		t.Fatalf("502 body repeats the registry's answer: %.200s", response.Body.String())
	}
	// The server log keeps a bounded prefix of the answer for diagnosis.
	logged := loggedErrorsOf(logs, testRequestID)
	if len(logged) != 1 || !strings.Contains(logged[0], strings.Repeat("x", 1024)) ||
		strings.Contains(logged[0], "DO_NOT_INCLUDE") || len(logged[0]) > maxDockerErrorBodyBytes+1024 {
		t.Fatalf("logged error has %d bytes, want the answer's first %d bytes", len(strings.Join(logged, "")), maxDockerErrorBodyBytes)
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
	}, egress.Policy{}, nil)
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
// through the given fakes. It returns the database that holds the access log.
func newRoutedDockerRouter(
	t *testing.T,
	registry config.RegistryConfig,
	policy egress.Policy,
	lookup ipResolver,
	dialContext func(context.Context, string, string) (net.Conn, error),
	observationRequired bool,
) (*gin.Engine, *gorm.DB) {
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
	handler := New(manager, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour}, database, dockerCfg, policy, nil)
	handler.resolver = newResolver(dockerCfg, policy, lookup, dialContext)
	handler.SetProvenanceRequired(observationRequired)
	router := gin.New()
	handler.Register(router.Group("/v2"))
	return router, database
}

// A HEAD answer has no body, and the observation gate's tag resolution fails
// before any fetch is logged, so the server log is where an operator reads
// why such a manifest request answered 502, for example the fake-IP remedy.
// The log line and the access log carry the request ID that the 502 message
// names, so Admin, MCP and the log lead to each other.
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

	const remedy = ", a fake-IP address from proxy software in TUN/fake-IP mode; " +
		"set [egress] upstream_proxy or DEPSILO_EGRESS_UPSTREAM_PROXY to the proxy software's HTTP port and restart, " +
		"see docs/deployment.md#upstream-network-egress"
	fakeIPRegistry := staticIPResolver{"registry.example": {{IP: net.ParseIP("198.18.0.10")}}}
	for _, tt := range []struct {
		name        string
		method      string
		observation bool
		answers     staticIPResolver
		wantCode    string
		wantMessage string
		wantLogged  string
	}{
		{
			name:        "HEAD whose token discovery is refused",
			method:      http.MethodHead,
			answers:     fakeIPRegistry,
			wantCode:    "AUTH_FAILED",
			wantMessage: "auth discovery failed at http://registry.example: registry.example resolved to 198.18.0.10" + remedy,
			wantLogged:  "registry.example resolved to 198.18.0.10, a fake-IP address",
		},
		{
			name:   "HEAD whose redirect is refused",
			method: http.MethodHead,
			answers: staticIPResolver{
				"registry.example": {{IP: net.ParseIP("93.184.216.34")}},
				"cdn.example":      {{IP: net.ParseIP("198.18.0.12")}},
			},
			wantCode:    "UPSTREAM_UNAVAILABLE",
			wantMessage: "manifest HEAD failed at http://cdn.example: cdn.example resolved to 198.18.0.12" + remedy,
			wantLogged:  "cdn.example resolved to 198.18.0.12, a fake-IP address",
		},
		{
			name:        "GET whose tag resolution for the observation gate is refused",
			method:      http.MethodGet,
			observation: true,
			answers:     fakeIPRegistry,
			wantCode:    "UPSTREAM_UNAVAILABLE",
			wantMessage: "auth discovery failed at http://registry.example: registry.example resolved to 198.18.0.10" + remedy,
			wantLogged:  "registry.example resolved to 198.18.0.10, a fake-IP address",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zap.ErrorLevel)
			t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
			dialer := &routedDialer{direct: registry.Listener.Addr().String()}
			router, database := newRoutedDockerRouter(t, config.RegistryConfig{Name: "hub", URL: "http://registry.example"},
				egress.Policy{}, tt.answers, dialer.DialContext, tt.observation)

			response := serveWithRequestID(router, tt.method, "/v2/library/widget/manifests/latest", testRequestID)
			code, message := badGatewayBody(t, response)
			if want := tt.wantMessage + testRequestSuffix; code != tt.wantCode || message != want {
				t.Fatalf("answer = %s %q, want %s %q", code, message, tt.wantCode, want)
			}
			logged := loggedErrorsOf(logs, testRequestID)
			if !slices.ContainsFunc(logged, func(text string) bool { return strings.Contains(text, tt.wantLogged) }) {
				t.Fatalf("errors logged under request %s = %q, want one containing %q", testRequestID, logged, tt.wantLogged)
			}

			var entries []db.AccessLog
			if err := database.Where("request_id = ?", testRequestID).Find(&entries).Error; err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Method != tt.method || entries[0].StatusCode != http.StatusBadGateway ||
				entries[0].CacheKey != ManifestCacheKey("hub", "library/widget", "latest") {
				t.Fatalf("access log entries of request %s = %+v, want one %s answered 502", testRequestID, entries, tt.method)
			}
		})
	}
}

// Manifest requests ask for every manifest and index media type, so a
// registry does not fall back to its default format for a multi-platform
// image; blob and tag-list requests ask for none.
func TestManifestRequestsAskForEveryManifestMediaType(t *testing.T) {
	var mu sync.Mutex
	accepts := map[string]string{}
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		accepts[request.Method+" "+request.URL.Path] = request.Header.Get("Accept")
		mu.Unlock()
		if request.URL.Path != "/v2/" {
			w.Header().Set("Docker-Content-Digest", "sha256:4f1e")
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(registry.Close)
	router, _ := newRoutedDockerRouter(t, config.RegistryConfig{Name: "hub", URL: "http://registry.example"},
		egress.Policy{}, staticIPResolver{"registry.example": {{IP: net.ParseIP("93.184.216.34")}}},
		mappedDialer{"93.184.216.34:80": registry.Listener.Addr().String()}.DialContext, false)

	for _, request := range []struct{ method, path, want string }{
		{http.MethodGet, "/v2/library/widget/manifests/latest", manifestAccept},
		{http.MethodHead, "/v2/library/widget/manifests/latest", manifestAccept},
		{http.MethodGet, "/v2/library/widget/blobs/sha256:4f1e", ""},
		{http.MethodGet, "/v2/library/widget/tags/list", ""},
	} {
		if response := serveWithRequestID(router, request.method, request.path, testRequestID); response.Code != http.StatusOK {
			t.Fatalf("%s %s = %d %s", request.method, request.path, response.Code, response.Body.String())
		}
		mu.Lock()
		got, sent := accepts[request.method+" "+request.path]
		mu.Unlock()
		if !sent || got != request.want {
			t.Errorf("%s %s sent Accept %q (sent %v), want %q", request.method, request.path, got, sent, request.want)
		}
	}
}

// The registry's own answer to a manifest HEAD passes on with its status and
// no body. The access log records it under the request ID, and a 5xx that no
// cached manifest can answer also leaves its reason in the server log.
func TestManifestHeadPassesOnTheRegistryStatusAndLogsIt(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		// wantLogged is the error line expected under the request ID, if any.
		wantLogged string
	}{
		{
			name:       "registry error",
			status:     http.StatusBadGateway,
			wantLogged: "manifest HEAD failed: http://registry.example/v2/library/widget/manifests/latest returned 502",
		},
		{name: "unknown tag", status: http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			registry := httptest.NewServer(answerWith(tt.status, ""))
			t.Cleanup(registry.Close)
			core, logs := observer.New(zap.ErrorLevel)
			t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
			router, database := newRoutedDockerRouter(t, config.RegistryConfig{Name: "hub", URL: "http://registry.example"},
				egress.Policy{}, staticIPResolver{"registry.example": {{IP: net.ParseIP("93.184.216.34")}}},
				mappedDialer{"93.184.216.34:80": registry.Listener.Addr().String()}.DialContext, false)

			response := serveWithRequestID(router, http.MethodHead, "/v2/library/widget/manifests/latest", testRequestID)
			if response.Code != tt.status || response.Body.Len() != 0 {
				t.Fatalf("HEAD answer = %d %q, want the registry's %d without a body", response.Code, response.Body.String(), tt.status)
			}
			logged := loggedErrorsOf(logs, testRequestID)
			if tt.wantLogged == "" && len(logged) != 0 ||
				tt.wantLogged != "" && (len(logged) != 1 || !strings.Contains(logged[0], tt.wantLogged)) {
				t.Fatalf("errors logged under request %s = %q, want %q", testRequestID, logged, tt.wantLogged)
			}
			var entries []db.AccessLog
			if err := database.Where("request_id = ?", testRequestID).Find(&entries).Error; err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Method != http.MethodHead || entries[0].StatusCode != tt.status {
				t.Fatalf("access log entries of request %s = %+v, want one HEAD answered %d", testRequestID, entries, tt.status)
			}
		})
	}
}

// A client path that cannot form the registry URL answers 500. Go's parse
// error quotes that URL, userinfo included, so only the log carries it.
func TestManifestHeadWithAnInvalidPathKeepsTheRegistryURLOutOfTheAnswer(t *testing.T) {
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/v2/") {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, request)
	}))
	t.Cleanup(registry.Close)
	core, logs := observer.New(zap.ErrorLevel)
	t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
	router, database := newRoutedDockerRouter(t,
		config.RegistryConfig{Name: "hub", URL: "http://operator:registry-secret@registry.example/mirror"},
		egress.Policy{}, staticIPResolver{"registry.example": {{IP: net.ParseIP("93.184.216.34")}}},
		mappedDialer{"93.184.216.34:80": registry.Listener.Addr().String()}.DialContext, false)

	response := serveWithRequestID(router, http.MethodHead, "/v2/library/widget/manifests/%25zz", testRequestID)
	const want = `{"code":"REQUEST_ERROR","message":"invalid Docker request path"}`
	if response.Code != http.StatusInternalServerError || response.Body.String() != want {
		t.Fatalf("answer = %d %s, want 500 %s", response.Code, response.Body.String(), want)
	}
	logged := loggedErrorsOf(logs, testRequestID)
	if len(logged) != 1 || !strings.Contains(logged[0], `parse "http://registry.example/mirror/v2/library/widget/manifests/%zz"`) ||
		strings.Contains(logged[0], "registry-secret") || strings.Contains(logged[0], "operator") {
		t.Fatalf("errors logged under request %s = %q, want the parse error without userinfo", testRequestID, logged)
	}
	var entries []db.AccessLog
	if err := database.Where("request_id = ?", testRequestID).Find(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Method != http.MethodHead || entries[0].StatusCode != http.StatusInternalServerError {
		t.Fatalf("access log entries of request %s = %+v, want one HEAD answered 500", testRequestID, entries)
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
	router, _ := newRoutedDockerRouter(t, config.RegistryConfig{Name: "hub", URL: "http://registry.example"}, policy,
		staticIPResolver{"registry.example": {{IP: net.ParseIP("93.184.216.34")}}}, dialer.DialContext, false)

	response := serveWithRequestID(router, http.MethodGet, "/v2/library/widget/manifests/latest", testRequestID)
	code, message := badGatewayBody(t, response)
	const want = "auth discovery failed at http://registry.example: connection to the egress proxy failed" + testRequestSuffix
	if code != "UPSTREAM_UNAVAILABLE" || message != want {
		t.Fatalf("answer = %s %q, want UPSTREAM_UNAVAILABLE %q", code, message, want)
	}
	for _, hidden := range []string{proxyAddress, "proxy-secret", "operator", "proxyconnect"} {
		if strings.Contains(response.Body.String(), hidden) {
			t.Fatalf("502 body %s names %q", response.Body.String(), hidden)
		}
	}

	logged := loggedErrorsOf(logs, testRequestID)
	if !slices.ContainsFunc(logged, func(text string) bool {
		return strings.Contains(text, "proxyconnect") && strings.Contains(text, proxyAddress)
	}) {
		t.Fatalf("logged errors = %q, want the proxy connection failure with %s", logged, proxyAddress)
	}
}

// Through the [egress] default proxy, a plain-http registry's 502, 503 or 504
// cannot be told from the proxy's own, so a manifest HEAD answers 502 with the
// reason instead of passing the status on, as a GET does; the server log
// keeps the first 64 KiB of a GET's answer with its URL credentials
// redacted. A cached manifest still answers such a HEAD.
func TestManifestHeadThroughTheDefaultProxyAnswersAGatewayStatusWith502(t *testing.T) {
	const mediaType = "application/vnd.oci.image.index.v1+json"
	manifest := `{"schemaVersion":2,"mediaType":"` + mediaType + `","manifests":[]}`
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(manifest)))
	var failing atomic.Bool
	proxy := newRecordingProxy(t, func(w http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/v2/":
			w.WriteHeader(http.StatusOK)
		case failing.Load():
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "maintenance, see http://status.example/?token=PROXY-SECRET "+strings.Repeat("x", 64<<10)+"DO_NOT_INCLUDE")
		default:
			w.Header().Set("Content-Type", mediaType)
			w.Header().Set("Docker-Content-Digest", digest)
			_, _ = io.WriteString(w, manifest)
		}
	})
	policy, err := egress.NewPolicy(proxy.URL, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	core, logs := observer.New(zap.ErrorLevel)
	t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
	dialer := &routedDialer{proxies: map[string]bool{proxy.address(): true}}
	router, database := newRoutedDockerRouter(t, config.RegistryConfig{Name: "hub", URL: "http://registry.example"}, policy,
		staticIPResolver{"registry.example": {{IP: net.ParseIP("93.184.216.34")}}}, dialer.DialContext, false)

	if got := serveWithRequestID(router, http.MethodGet, "/v2/library/widget/manifests/1.0", "online-id"); got.Code != http.StatusOK || got.Body.String() != manifest {
		t.Fatalf("online GET = %d %q", got.Code, got.Body.String())
	}
	failing.Store(true)

	const reason = "returned 503 through the egress proxy; if registry.example is reachable only without the proxy, " +
		"list it in [egress] no_proxy and restart, see docs/deployment.md#upstream-network-egress"
	code, message := badGatewayBody(t, serveWithRequestID(router, http.MethodHead, "/v2/library/widget/manifests/2.0", testRequestID))
	if want := "manifest HEAD failed at http://registry.example: " + reason + testRequestSuffix; code != "UPSTREAM_UNAVAILABLE" || message != want {
		t.Fatalf("HEAD answer = %s %q, want UPSTREAM_UNAVAILABLE %q", code, message, want)
	}
	if logged := loggedErrorsOf(logs, testRequestID); len(logged) != 1 || !strings.Contains(logged[0], reason) {
		t.Fatalf("errors logged under request %s = %q, want one with the reason", testRequestID, logged)
	}
	var entries []db.AccessLog
	if err := database.Where("request_id = ?", testRequestID).Find(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Method != http.MethodHead || entries[0].StatusCode != http.StatusBadGateway {
		t.Fatalf("access log entries of request %s = %+v, want one HEAD answered 502", testRequestID, entries)
	}

	code, message = badGatewayBody(t, serveWithRequestID(router, http.MethodGet, "/v2/library/widget/manifests/2.0", "get-id"))
	if want := "manifest failed at http://registry.example: " + reason + " (request get-id; details in the Depsilo server log)"; code != "UPSTREAM_UNAVAILABLE" || message != want {
		t.Fatalf("GET answer = %s %q, want UPSTREAM_UNAVAILABLE %q", code, message, want)
	}
	logged := loggedErrorsOf(logs, "get-id")
	if len(logged) != 1 || !strings.Contains(logged[0], reason+"; answer: maintenance, see http://status.example/?token=REDACTED xxxx") ||
		!strings.HasSuffix(logged[0], "x…") || strings.Contains(logged[0], "PROXY-SECRET") || strings.Contains(logged[0], "DO_NOT_INCLUDE") {
		t.Fatalf("errors logged under request get-id = %.300q, want the first 64 KiB of the redacted answer after the reason", logged)
	}

	cached := serveWithRequestID(router, http.MethodHead, "/v2/library/widget/manifests/1.0", "cached-id")
	if cached.Code != http.StatusOK || cached.Header().Get("Docker-Content-Digest") != digest {
		t.Fatalf("HEAD of a cached manifest = %d digest %q, want the cached manifest", cached.Code, cached.Header().Get("Docker-Content-Digest"))
	}
}
