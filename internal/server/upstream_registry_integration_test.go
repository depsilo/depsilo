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
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/adapter/pypi"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestStartServerReturnsBindErrorBeforeRegistryWorkersStart(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	_, portText, err := net.SplitHostPort(occupied.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	var probeHits atomic.Int64
	probeTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		probeHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer probeTarget.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	document := fmt.Sprintf(`
[server]
host = "127.0.0.1"
port = %d
log_level = "info"

[database]
driver = "sqlite"
dsn = %q

[storage]
type = "local"
path = %q

[[pypi.upstreams]]
name = "probe"
url = %q
priority = 1
probe_mode = "active"
probe_interval = "10ms"
`, port, filepath.Join(dir, "server.db"), filepath.Join(dir, "cache"), probeTarget.URL)
	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if srv, err := StartServer(ctx, zap.NewAtomicLevel()); err == nil || srv != nil {
		t.Fatalf("StartServer = (%v, %v), want nil bind error", srv, err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := probeHits.Load(); got != 0 {
		t.Fatalf("registry worker probed %d times after bind failure", got)
	}
}

func startLifecycleTestServer(t *testing.T, handler http.Handler, cleanup func() error) (*http.Server, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{}
	resources := newServerResources(nil, nil)
	resources.closeDatabase = newAsyncCloseAdapter(cleanup)
	lifecycle := registerServerLifecycle(srv, resources)
	srv.Handler = lifecycle.track(handler)
	go serveHTTP(srv, listener)
	return srv, "http://" + listener.Addr().String()
}

func TestShutdownKeepsDependenciesAliveUntilActiveHandlerDrains(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	cleanupCalled := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		close(exited)
		w.WriteHeader(http.StatusNoContent)
	})
	srv, url := startLifecycleTestServer(t, handler, func() error {
		select {
		case <-exited:
		default:
			t.Error("cleanup ran before active handler exited")
		}
		close(cleanupCalled)
		return nil
	})
	requestDone := make(chan error, 1)
	go func() {
		response, err := http.Get(url)
		if err == nil {
			response.Body.Close()
		}
		requestDone <- err
	}()
	<-entered

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- Shutdown(context.Background(), srv) }()
	value, _ := serverLifecycles.Load(srv)
	lifecycle := value.(*serverLifecycle)
	deadline := time.Now().Add(time.Second)
	for {
		lifecycle.mu.Lock()
		closing := lifecycle.closing
		lifecycle.mu.Unlock()
		if closing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shutdown gate did not close")
		}
		time.Sleep(time.Millisecond)
	}
	late := httptest.NewRecorder()
	srv.Handler.ServeHTTP(late, httptest.NewRequest(http.MethodGet, "/late", nil))
	if late.Code != http.StatusServiceUnavailable {
		t.Fatalf("late request status=%d want=%d", late.Code, http.StatusServiceUnavailable)
	}
	select {
	case <-cleanupCalled:
		t.Fatal("cleanup ran while active handler was blocked")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatal(err)
	}
	<-cleanupCalled
}

func TestShutdownTimeoutCancelsHandlerBeforeCleanup(t *testing.T) {
	entered := make(chan struct{})
	exited := make(chan struct{})
	cleanupCalled := make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(entered)
		<-request.Context().Done()
		close(exited)
	})
	srv, url := startLifecycleTestServer(t, handler, func() error {
		select {
		case <-exited:
		default:
			t.Error("cleanup ran before cancelled handler exited")
		}
		close(cleanupCalled)
		return nil
	})
	go func() { _, _ = http.Get(url) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := Shutdown(ctx, srv); err == nil {
		t.Fatal("Shutdown returned nil after deadline")
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("handler context was not cancelled by forced close")
	}
	select {
	case <-cleanupCalled:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not run after cancelled handler exited")
	}
}

type failingListener struct{ err error }

func (listener failingListener) Accept() (net.Conn, error) { return nil, listener.err }
func (failingListener) Close() error                       { return nil }
func (failingListener) Addr() net.Addr                     { return staticAddr("failing") }

type staticAddr string

func (addr staticAddr) Network() string { return "test" }
func (addr staticAddr) String() string  { return string(addr) }

func TestUnexpectedServeExitRunsCleanupAndDeletesLifecycle(t *testing.T) {
	srv := &http.Server{}
	cleanupCalled := make(chan struct{})
	resources := newServerResources(nil, nil)
	resources.closeDatabase = newAsyncCloseAdapter(func() error { close(cleanupCalled); return nil })
	registerServerLifecycle(srv, resources)
	serveHTTP(srv, failingListener{err: errors.New("accept failed")})
	<-cleanupCalled
	deadline := time.Now().Add(time.Second)
	for {
		if _, ok := serverLifecycles.Load(srv); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("unexpected Serve exit retained lifecycle entry")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestConcurrentAndRepeatedShutdownCleansUpExactlyOnce(t *testing.T) {
	var cleanupCalls atomic.Int64
	srv, _ := startLifecycleTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), func() error { cleanupCalls.Add(1); return nil })
	const callers = 8
	errorsOut := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsOut <- Shutdown(context.Background(), srv)
		}()
	}
	group.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := Shutdown(context.Background(), srv); err != nil {
		t.Fatalf("repeated Shutdown: %v", err)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls=%d want=1", got)
	}
}

func TestCleanupErrorDoesNotPoisonLaterShutdown(t *testing.T) {
	sentinel := errors.New("cleanup sentinel")
	var cleanupCalls atomic.Int64
	srv, _ := startLifecycleTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), func() error {
		if cleanupCalls.Add(1) == 1 {
			return sentinel
		}
		return nil
	})

	if err := Shutdown(context.Background(), srv); !errors.Is(err, sentinel) {
		t.Fatalf("first Shutdown error=%v", err)
	}
	if _, ok := serverLifecycles.Load(srv); !ok {
		t.Fatal("failed cleanup deleted lifecycle entry")
	}
	if err := Shutdown(context.Background(), srv); err != nil {
		t.Fatalf("retry Shutdown error=%v", err)
	}
	if got := cleanupCalls.Load(); got != 2 {
		t.Fatalf("cleanup calls=%d want=2", got)
	}
	if _, ok := serverLifecycles.Load(srv); ok {
		t.Fatal("completed lifecycle retained sync.Map entry")
	}
	if err := Shutdown(context.Background(), srv); err != nil {
		t.Fatalf("completed Shutdown error=%v", err)
	}
	if got := cleanupCalls.Load(); got != 2 {
		t.Fatalf("completed cleanup calls=%d want=2", got)
	}
}

func TestShutdownTimeoutDefersCleanupUntilIgnoringHandlerEventuallyExits(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	cleanupCalled := make(chan struct{})
	var cleanupCalls atomic.Int64
	srv, url := startLifecycleTestServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	}), func() error {
		cleanupCalls.Add(1)
		close(cleanupCalled)
		return nil
	})
	go func() { _, _ = http.Get(url) }()
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	shutdownErr := Shutdown(ctx, srv)
	if !errors.Is(shutdownErr, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error=%v want deadline", shutdownErr)
	}
	select {
	case <-cleanupCalled:
		t.Fatal("cleanup ran under cancellation-ignoring handler")
	default:
	}
	if _, ok := serverLifecycles.Load(srv); !ok {
		t.Fatal("lifecycle deleted before ignoring handler exited")
	}

	close(release)
	select {
	case <-cleanupCalled:
	case <-time.After(time.Second):
		t.Fatal("deferred cleanup did not run after handler release")
	}
	deadline := time.Now().Add(time.Second)
	for {
		if _, ok := serverLifecycles.Load(srv); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("deferred cleanup did not delete lifecycle entry")
		}
		time.Sleep(time.Millisecond)
	}
	if err := Shutdown(context.Background(), srv); err != nil {
		t.Fatalf("later Shutdown error=%v want nil after deferred cleanup", err)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls=%d want=1", got)
	}
}

func TestRawShutdownAndCloseFallbackWaitForEnteredHandlers(t *testing.T) {
	for _, operation := range []string{"shutdown", "close"} {
		t.Run(operation, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			cleanupCalled := make(chan struct{})
			srv, url := startLifecycleTestServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				close(entered)
				<-release
			}), func() error { close(cleanupCalled); return nil })
			go func() { _, _ = http.Get(url) }()
			<-entered
			operationDone := make(chan error, 1)
			go func() {
				if operation == "shutdown" {
					operationDone <- srv.Shutdown(context.Background())
					return
				}
				operationDone <- srv.Close()
			}()
			select {
			case <-cleanupCalled:
				t.Fatal("fallback cleanup ran before entered handler exited")
			case <-time.After(50 * time.Millisecond):
			}
			close(release)
			if err := <-operationDone; err != nil {
				t.Fatal(err)
			}
			select {
			case <-cleanupCalled:
			case <-time.After(time.Second):
				t.Fatal("fallback cleanup did not run")
			}
		})
	}
}

func TestProductionEntrypointsAwaitServerShutdownHelper(t *testing.T) {
	paths := []string{
		"../cli/serve.go",
		"../cli/daemon.go",
		"../../cmd/server/main_server.go",
		"../../cmd/depsilo-tray/main.go",
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "server.Shutdown(") {
			t.Errorf("%s does not await server.Shutdown", path)
		}
		if strings.Contains(string(body), "srv.Shutdown(") {
			t.Errorf("%s still calls raw http.Server.Shutdown", path)
		}
	}
}

func serverTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	return database
}

func testCacheManager(t *testing.T, database *gorm.DB) *cache.Manager {
	t.Helper()
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})
	return manager
}

func requestUniquePyPIPath(t *testing.T, handler http.Handler, path string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s: status=%d body=%s", path, recorder.Code, recorder.Body.String())
	}
}

func TestStandardEcosystemDefinitionsHaveExactOrderRoutesAndFactories(t *testing.T) {
	cfg := &config.Config{}
	definitions := standardEcosystemDefinitions(cfg)
	got := make([][2]string, 0, len(definitions))
	for _, definition := range definitions {
		if definition.factory == nil {
			t.Fatalf("nil factory for %s", definition.name)
		}
		got = append(got, [2]string{definition.name, definition.route})
	}
	want := [][2]string{
		{"pypi", "/pypi"}, {"apt", "/apt"}, {"npm", "/npm"}, {"go", "/go"},
		{"cargo", "/crates"}, {"maven", "/maven"}, {"rubygems", "/rubygems"},
		{"composer", "/composer"}, {"nuget", "/nuget"}, {"conda", "/conda"},
		{"cran", "/cran"}, {"alpine", "/alpine"}, {"helm", "/helm"},
		{"huggingface", "/huggingface"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("definitions=%v want=%v", got, want)
	}
}

func TestSeedSourcesExcludeDefaultedAlpineAndActiveDefinitionsExcludeInactive(t *testing.T) {
	cfg := &config.Config{
		ExplicitUpstreamEcosystems: map[string]bool{"pypi": true},
		PyPI:                       config.AdapterConfig{Upstreams: []config.UpstreamConfig{{Name: "pypi", URL: "https://pypi.example", Priority: 1}}},
		Alpine:                     config.AdapterConfig{Upstreams: []config.UpstreamConfig{{Name: "built-in", URL: "https://dl-cdn.alpinelinux.org/alpine", Priority: 1}}},
	}
	definitions := standardEcosystemDefinitions(cfg)
	result, err := upstream.ReconcileBootstrap(serverTestDB(t), seedSources(definitions))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pypi"}; !reflect.DeepEqual(result.ActiveEcosystems, want) {
		t.Fatalf("active=%v want=%v", result.ActiveEcosystems, want)
	}
	active, err := activeDefinitions(definitions, result.ActiveEcosystems)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].name != "pypi" {
		t.Fatalf("definitions=%#v", active)
	}
}

func TestRegisterActiveAdaptersAddsOnlyStandardAndProjectPyPIRoutes(t *testing.T) {
	database := serverTestDB(t)
	record := db.UpstreamRecord{AdapterType: "pypi", Name: "primary", URL: "https://pypi.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := upstream.NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := activeDefinitions(standardEcosystemDefinitions(&config.Config{}), []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	project := engine.Group("/p/:slug")
	if err := registerActiveAdapters(engine, project, definitions, registry.Pools(), testCacheManager(t, database), config.CacheConfig{}, database); err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0)
	for _, route := range engine.Routes() {
		paths = append(paths, route.Path)
	}
	if !containsString(paths, "/pypi/simple/") || !containsString(paths, "/p/:slug/pypi/simple/") {
		t.Fatalf("paths=%v", paths)
	}
	for _, path := range paths {
		if strings.Contains(path, "/npm") || strings.Contains(path, "/v2") {
			t.Fatalf("inactive/config-owned route registered: %s", path)
		}
	}
}

func TestRegisteredStandardAdapterRecoversUnhealthyPassiveUpstream(t *testing.T) {
	var upstreamHits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<a href="package.whl">package</a>`)
	}))
	defer target.Close()

	database := serverTestDB(t)
	record := db.UpstreamRecord{
		AdapterType: "pypi", Name: "recovering", URL: target.URL, Priority: 1,
		ProbeMode: "passive", ProbeInterval: "30m", Healthy: false,
	}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	// GORM applies the model's default=true tag when a false bool is inserted,
	// so persist the unhealthy runtime fixture explicitly after creation.
	if err := database.Model(&record).Updates(map[string]any{
		"healthy": false, "success_rate": 0,
	}).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := upstream.NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := activeDefinitions(standardEcosystemDefinitions(&config.Config{}), []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	if err := registerActiveAdapters(
		engine,
		engine.Group("/p/:slug"),
		definitions,
		registry.Pools(),
		testCacheManager(t, database),
		config.CacheConfig{},
		database,
	); err != nil {
		t.Fatal(err)
	}

	requestUniquePyPIPath(t, engine, "/pypi/simple/recovery-check/")
	if upstreamHits.Load() != 1 {
		t.Fatalf("upstream hits=%d, want 1", upstreamHits.Load())
	}
	selected := registry.Pools()["pypi"].Snapshot()[0]
	if !selected.IsHealthy() {
		t.Fatal("successful half-open request did not restore passive upstream health")
	}
}

func containsString(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}

func TestRegistryUpdateChangesNextRealPyPIProxyRequest(t *testing.T) {
	var firstHits, secondHits atomic.Int64
	token := strconv.FormatInt(time.Now().UnixNano(), 36)
	proxyTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasPrefix(request.URL.Path, "/"+token+"/first/"):
			firstHits.Add(1)
		case strings.HasPrefix(request.URL.Path, "/"+token+"/second/"):
			secondHits.Add(1)
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<a href="package.whl">package</a>`)
	}))
	defer proxyTarget.Close()
	firstURL := proxyTarget.URL + "/" + token + "/first"
	secondURL := proxyTarget.URL + "/" + token + "/second"

	database := serverTestDB(t)
	record := db.UpstreamRecord{AdapterType: "pypi", Name: "primary", URL: firstURL, Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := upstream.NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	pool := registry.Pools()["pypi"]
	engine := gin.New()
	cacheMgr := testCacheManager(t, database)
	handler := pypi.New(cacheMgr, upstream.NewPrioritySelector(pool), config.CacheConfig{TTLIndex: time.Hour}, database)
	handler.Register(engine.Group("/pypi"))

	requestUniquePyPIPath(t, engine, "/pypi/simple/before/")
	waitForAccessLogs(t, database, 1)
	waitForCacheEntries(t, database, 1)
	if firstHits.Load() == 0 || secondHits.Load() != 0 {
		t.Fatalf("before update: first=%d second=%d", firstHits.Load(), secondHits.Load())
	}
	_, err = registry.Update(context.Background(), record.ID, upstream.MutationInput{AdapterType: "pypi", Name: "primary", URL: secondURL, Priority: 1, ProbeMode: "passive", ProbeInterval: "30m"})
	if err != nil {
		t.Fatal(err)
	}
	if registry.Pools()["pypi"] != pool {
		t.Fatal("registry replaced the stable Pool pointer")
	}
	requestUniquePyPIPath(t, engine, "/pypi/simple/after/")
	if secondHits.Load() == 0 {
		t.Fatalf("first=%d second=%d", firstHits.Load(), secondHits.Load())
	}
}

func waitForCacheEntries(t *testing.T, database *gorm.DB, wanted int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var count int64
		if err := database.Model(&db.CacheEntry{}).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count >= wanted {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache entries=%d want>=%d", count, wanted)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForAccessLogs(t *testing.T, database *gorm.DB, wanted int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var count int64
		if err := database.Model(&db.AccessLog{}).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count >= wanted {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("access logs=%d want>=%d", count, wanted)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
