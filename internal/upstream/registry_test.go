package upstream

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/db"
	"gorm.io/gorm"
)

func TestNewRegistryRejectsActiveEcosystemWithoutRows(t *testing.T) {
	database := bootstrapDB(t)
	_, err := NewRegistry(database, []string{"pypi"})
	if err == nil || err.Error() != "active ecosystem pypi has no upstreams" {
		t.Fatalf("err=%v", err)
	}
}

func TestRegistryBuildsOnlyActivePoolsAndReturnsOwnershipCopies(t *testing.T) {
	database := bootstrapDB(t)
	for _, row := range []db.UpstreamRecord{
		{AdapterType: "pypi", Name: "one", URL: "https://one.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true},
		{AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true},
	} {
		if err := database.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	pools := registry.Pools()
	if _, ok := pools["pypi"]; !ok {
		t.Fatal("missing active pool")
	}
	if _, ok := pools["npm"]; ok {
		t.Fatal("inactive pool was built")
	}
	delete(pools, "pypi")
	if _, ok := registry.Pools()["pypi"]; !ok {
		t.Fatal("caller mutated registry pool ownership")
	}
	active := registry.ActiveEcosystems()
	active[0] = "npm"
	if got := registry.ActiveEcosystems(); !reflect.DeepEqual(got, []string{"pypi"}) {
		t.Fatalf("active=%v", got)
	}
}

func TestRegistryCanonicalizesActiveEcosystemOrder(t *testing.T) {
	database := bootstrapDB(t)
	for _, row := range []db.UpstreamRecord{
		{AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true},
		{AdapterType: "pypi", Name: "pypi", URL: "https://pypi.org/simple", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true},
	} {
		if err := database.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	registry, err := NewRegistry(database, []string{"npm", "pypi"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := registry.ActiveEcosystems(), []string{"pypi", "npm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("active=%v want=%v", got, want)
	}
}

func TestRegistryListAndGetReturnRuntimeState(t *testing.T) {
	database := bootstrapDB(t)
	checkedAt := time.Date(2026, time.July, 11, 1, 2, 3, 0, time.UTC)
	record := db.UpstreamRecord{
		AdapterType: "pypi", Name: "one", URL: "https://one.example", Proxy: "http://proxy.example",
		Priority: 2, ProbeMode: "passive", ProbeInterval: "45s", Healthy: true,
		AvgLatencyMs: 17, SuccessRate: 0.75, LastCheckedAt: checkedAt,
	}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := registry.Get(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != record.ID || got.AdapterType != "pypi" || got.Name != "one" || got.URL != "https://one.example" || got.Proxy != "http://proxy.example" || got.Priority != 2 || got.ProbeMode != "passive" || got.ProbeInterval != "45s" || !got.Healthy || got.AvgLatencyMS != 17 || got.SuccessRate != 0.75 || !got.LastCheckedAt.Equal(checkedAt) || got.WorkerRunning {
		t.Fatalf("runtime=%#v", got)
	}
	listed := registry.List()
	if len(listed) != 1 || !reflect.DeepEqual(listed[0], got) {
		t.Fatalf("list=%#v get=%#v", listed, got)
	}
	if _, err := registry.Get(record.ID + 1); err != ErrNotFound {
		t.Fatalf("missing err=%v", err)
	}
}

func TestRegistryCloseStopsEveryWorker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	database := bootstrapDB(t)
	record := db.UpstreamRecord{AdapterType: "pypi", Name: "one", URL: server.URL, Priority: 1, ProbeMode: "active", ProbeInterval: "10ms", Healthy: true}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	registry.Start(ctx)
	handle := registryWorker(t, registry, record.ID)
	if !registry.WorkerRunning(record.ID) {
		t.Fatal("active worker was not started")
	}
	cancel()
	registry.Close()
	select {
	case <-handle.done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit")
	}
	if registry.WorkerRunning(record.ID) {
		t.Fatal("closed worker remains registered")
	}
}

func TestRegistryParentCancellationRemovesWorkerWithoutClose(t *testing.T) {
	registry, id := activeRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())
	registry.Start(ctx)
	handle := registryWorker(t, registry, id)
	cancel()
	waitDone(t, handle.done)
	if registry.WorkerRunning(id) {
		t.Fatal("completed worker remains registered")
	}
}

func TestRegistrySequentialLifecycleIsIdempotentAndRestartable(t *testing.T) {
	registry, id := activeRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry.Start(ctx)
	first := registryWorker(t, registry, id)
	registry.Start(ctx)
	if again := registryWorker(t, registry, id); again.generation != first.generation || again.done != first.done {
		t.Fatal("idempotent Start replaced the running generation")
	}
	registry.Close()
	registry.Close()
	waitDone(t, first.done)

	restartCtx, restartCancel := context.WithCancel(context.Background())
	defer restartCancel()
	registry.Start(restartCtx)
	second := registryWorker(t, registry, id)
	if second.generation == first.generation || second.done == first.done {
		t.Fatal("restart reused the stopped generation")
	}
	registry.Close()
	waitDone(t, second.done)
}

func TestRegistryStartWaitsForConcurrentCloseGeneration(t *testing.T) {
	registry, handle, cancelObserved, allowFinish, _ := controlledRegistryWorker()
	closeReturned := make(chan struct{})
	go func() {
		registry.Close()
		close(closeReturned)
	}()
	waitDone(t, cancelObserved)
	registry.workersMu.Lock()
	registry.workersMu.Unlock()

	startReturned := make(chan struct{})
	startCtx, startCancel := context.WithCancel(context.Background())
	defer startCancel()
	go func() {
		registry.Start(startCtx)
		close(startReturned)
	}()
	select {
	case <-startReturned:
		t.Fatal("Start crossed an in-progress Close transition")
	case <-time.After(20 * time.Millisecond):
	}
	close(allowFinish)
	waitDone(t, closeReturned)
	waitDone(t, handle.done)
	waitDone(t, startReturned)
	registry.Close()
}

func TestRegistryConcurrentClosesJoinTheRunningGeneration(t *testing.T) {
	registry, handle, cancelObserved, allowFinish, cancelCalls := controlledRegistryWorker()
	firstReturned := make(chan struct{})
	secondReturned := make(chan struct{})
	go func() { registry.Close(); close(firstReturned) }()
	waitDone(t, cancelObserved)
	registry.workersMu.Lock()
	registry.workersMu.Unlock()
	go func() { registry.Close(); close(secondReturned) }()
	select {
	case <-secondReturned:
		t.Fatal("concurrent Close bypassed the active lifecycle transition")
	case <-time.After(20 * time.Millisecond):
	}
	if got := cancelCalls.Load(); got != 1 {
		t.Fatalf("worker generation canceled %d times before first Close joined", got)
	}
	close(allowFinish)
	waitDone(t, firstReturned)
	waitDone(t, secondReturned)
	waitDone(t, handle.done)
	if registry.WorkerRunning(1) {
		t.Fatal("Close returned with its generation still running")
	}
}

func TestRegistryCloseContextTimeoutCanRetry(t *testing.T) {
	registry, handle, cancelObserved, allowFinish, cancelCalls := controlledRegistryWorker()
	closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := registry.CloseContext(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first CloseContext err=%v, want deadline exceeded", err)
	}
	waitDone(t, cancelObserved)
	if got := cancelCalls.Load(); got != 1 {
		t.Fatalf("worker generation canceled %d times after timeout, want 1", got)
	}

	retryReturned := make(chan error, 1)
	go func() {
		retryReturned <- registry.CloseContext(context.Background())
	}()
	select {
	case err := <-retryReturned:
		t.Fatalf("retry returned before worker finished: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(allowFinish)
	if err := <-retryReturned; err != nil {
		t.Fatalf("retry CloseContext: %v", err)
	}
	waitDone(t, handle.done)
	if registry.WorkerRunning(1) {
		t.Fatal("retried CloseContext left worker registered")
	}
	if got := cancelCalls.Load(); got != 1 {
		t.Fatalf("worker generation canceled %d times after retry, want 1", got)
	}
}

func TestRegistryCloseContextNilUsesBackground(t *testing.T) {
	registry, handle, cancelObserved, allowFinish, _ := controlledRegistryWorker()
	returned := make(chan error, 1)
	go func() {
		returned <- registry.CloseContext(nil)
	}()
	waitDone(t, cancelObserved)
	close(allowFinish)
	if err := <-returned; err != nil {
		t.Fatalf("CloseContext(nil): %v", err)
	}
	waitDone(t, handle.done)
}

func TestWaitWorkerHandlesPrefersDoneOverCanceledContext(t *testing.T) {
	done := make(chan struct{})
	close(done)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := waitWorkerHandles(ctx, []workerHandle{{done: done}}); err != nil {
		t.Fatalf("completed worker returned stale context error: %v", err)
	}
}

func TestProbeUsesUpstreamProxy(t *testing.T) {
	proxied := make(chan *http.Request, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		proxied <- req.Clone(req.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "pypi", Name: "one", URL: "http://unreachable.invalid", Proxy: proxy.URL,
		Priority: 1, ProbeMode: "active", ProbeInterval: "1s", Healthy: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	result := probe(context.Background(), pool.Snapshot()[0])
	if result.Err != nil || !result.Healthy {
		t.Fatalf("result=%#v", result)
	}
	select {
	case req := <-proxied:
		if req.Method != http.MethodHead || req.URL.Host != "unreachable.invalid" {
			t.Fatalf("request=%s %s", req.Method, req.URL)
		}
		if got := req.Header.Get("User-Agent"); got != "depsilo/0.1" {
			t.Fatalf("User-Agent=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("probe did not use configured proxy")
	}
}

func TestRunUpstreamHealthCheckProbesImmediately(t *testing.T) {
	probed := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		probed <- struct{}{}
	}))
	defer server.Close()

	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "pypi", Name: "recovering", URL: server.URL,
		Priority: 1, ProbeMode: "active", ProbeInterval: "1h", Healthy: false,
	}})
	if err != nil {
		t.Fatal(err)
	}

	u := pool.Snapshot()[0]
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runUpstreamHealthCheck(ctx, u, nil, time.Hour)
		close(done)
	}()

	select {
	case <-probed:
	case <-time.After(300 * time.Millisecond):
		cancel()
		t.Fatal("active health worker did not probe on startup")
	}
	deadline := time.Now().Add(300 * time.Millisecond)
	for !u.IsHealthy() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !u.IsHealthy() {
		cancel()
		t.Fatal("startup probe did not restore active upstream health")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("active health worker did not stop after cancellation")
	}
}

func TestProbeCanonicalizesDirectoryBaseURLBeforeHead(t *testing.T) {
	var nonCanonicalHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/index/":
			if request.Method != http.MethodHead {
				t.Errorf("request method=%s want=HEAD", request.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/index":
			nonCanonicalHits.Add(1)
			http.Redirect(w, request, "http://127.0.0.1:1/index/", http.StatusMovedPermanently)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "cargo", Name: "directory", URL: server.URL + "/index",
		Priority: 1, ProbeMode: "active", ProbeInterval: "1s", Healthy: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	result := probe(context.Background(), pool.Snapshot()[0])
	if result.Err != nil || !result.Healthy {
		t.Fatalf("result=%#v", result)
	}
	if nonCanonicalHits.Load() != 0 {
		t.Fatalf("probe requested non-canonical directory URL %d time(s)", nonCanonicalHits.Load())
	}
}

func TestPersistProbeRejectsMissingNonzeroUpstreamWithoutLog(t *testing.T) {
	database := bootstrapDB(t)
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 999, AdapterType: "pypi", Name: "gone", URL: "https://gone.example",
		Priority: 1, ProbeMode: "active", ProbeInterval: "1s", Healthy: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	u := pool.Snapshot()[0]
	result := ProbeResult{Healthy: false, Latency: time.Millisecond, CheckedAt: time.Now().UTC()}
	u.applyProbe(result)
	if err := persistProbe(context.Background(), database, u, result); err == nil {
		t.Fatal("missing upstream was accepted")
	}
	var count int64
	if err := database.Model(&db.UpstreamLatencyLog{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("latency logs=%d", count)
	}
}

func TestPersistProbeLogsConfigOwnedIDZeroUpstream(t *testing.T) {
	database := bootstrapDB(t)
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		AdapterType: "extra:private", Name: "private", URL: "https://private.example",
		Priority: 1, ProbeMode: "active", ProbeInterval: "1s", Healthy: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	u := pool.Snapshot()[0]
	result := ProbeResult{Healthy: true, Latency: 7 * time.Millisecond, CheckedAt: time.Now().UTC()}
	u.applyProbe(result)
	if err := persistProbe(context.Background(), database, u, result); err != nil {
		t.Fatal(err)
	}
	var logs []db.UpstreamLatencyLog
	if err := database.Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].UpstreamID != 0 || logs[0].Name != "private" || logs[0].LatencyMs != 7 {
		t.Fatalf("logs=%#v", logs)
	}
}

func TestRestoreFromDBSeparatesSameNameUpstreamsByID(t *testing.T) {
	database := bootstrapDB(t)
	firstAt := time.Now().UTC().Add(-time.Minute)
	secondAt := time.Now().UTC()
	for _, log := range []db.UpstreamLatencyLog{
		{UpstreamID: 11, Name: "shared", LatencyMs: 11, Healthy: true, CreatedAt: firstAt},
		{UpstreamID: 22, Name: "shared", LatencyMs: 22, Healthy: true, CreatedAt: secondAt},
		{UpstreamID: 0, Name: "shared", LatencyMs: 99, Healthy: true, CreatedAt: secondAt},
	} {
		if err := database.Create(&log).Error; err != nil {
			t.Fatal(err)
		}
	}
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{
		{ID: 11, AdapterType: "pypi", Name: "shared", URL: "https://one.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "1s"},
		{ID: 22, AdapterType: "npm", Name: "shared", URL: "https://two.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "1s"},
		{ID: 0, AdapterType: "extra:private", Name: "shared", URL: "https://extra.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "1s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	RestoreFromDB(pool, database)
	upstreams := pool.Snapshot()
	if got := upstreams[0].HealthSnapshot().AvgLatency; got != 11*time.Millisecond {
		t.Fatalf("id 11 latency=%s", got)
	}
	if got := upstreams[1].HealthSnapshot().AvgLatency; got != 22*time.Millisecond {
		t.Fatalf("id 22 latency=%s", got)
	}
	if got := upstreams[2].HealthSnapshot().AvgLatency; got != 99*time.Millisecond {
		t.Fatalf("id 0 latency=%s", got)
	}
}

func activeRegistry(t *testing.T) (*Registry, uint) {
	t.Helper()
	database := bootstrapDB(t)
	record := db.UpstreamRecord{AdapterType: "pypi", Name: "one", URL: "https://one.example", Priority: 1, ProbeMode: "active", ProbeInterval: "1h", Healthy: true}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	return registry, record.ID
}

func registryWorker(t *testing.T, registry *Registry, id uint) workerHandle {
	t.Helper()
	registry.workersMu.Lock()
	defer registry.workersMu.Unlock()
	handle, ok := registry.workers[id]
	if !ok {
		t.Fatalf("worker %d is not registered", id)
	}
	return handle
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("operation did not finish")
	}
}

func controlledRegistryWorker() (*Registry, workerHandle, <-chan struct{}, chan<- struct{}, *atomic.Int32) {
	workerCtx, workerCancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	cancelObserved := make(chan struct{})
	allowFinish := make(chan struct{})
	cancelCalls := &atomic.Int32{}
	registry := &Registry{
		pools:   make(map[string]*Pool),
		workers: make(map[uint]workerHandle),
		ctx:     context.Background(),
		cancel:  func() {},
		started: true,
	}
	handle := workerHandle{
		generation: 1,
		cancel: func() {
			if cancelCalls.Add(1) == 1 {
				workerCancel()
			}
		},
		done: done,
	}
	registry.workers[1] = handle
	go func() {
		<-workerCtx.Done()
		close(cancelObserved)
		<-allowFinish
		registry.finishWorker(1, handle.generation, done)
	}()
	return registry, handle, cancelObserved, allowFinish, cancelCalls
}

func TestPersistProbeWritesHealthAndLatencyLogTogether(t *testing.T) {
	database := bootstrapDB(t)
	record := db.UpstreamRecord{AdapterType: "pypi", Name: "one", URL: "https://one.example", Priority: 1, ProbeMode: "active", ProbeInterval: "1s", Healthy: true}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{record})
	if err != nil {
		t.Fatal(err)
	}
	u := pool.Snapshot()[0]
	checkedAt := time.Date(2026, time.July, 11, 2, 3, 4, 0, time.UTC)
	result := ProbeResult{Healthy: false, Latency: 25 * time.Millisecond, CheckedAt: checkedAt}
	u.applyProbe(result)
	if err := persistProbe(context.Background(), database, u, result); err != nil {
		t.Fatal(err)
	}
	var stored db.UpstreamRecord
	if err := database.First(&stored, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Healthy || stored.AvgLatencyMs != 25 || stored.SuccessRate != 0 || !stored.LastCheckedAt.Equal(checkedAt) {
		t.Fatalf("stored=%#v", stored)
	}
	var logs []db.UpstreamLatencyLog
	if err := database.Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].UpstreamID != record.ID || logs[0].Name != "one" || logs[0].LatencyMs != 25 || logs[0].Healthy || !logs[0].CreatedAt.Equal(checkedAt) {
		t.Fatalf("logs=%#v", logs)
	}
}

func TestPersistProbeRollsBackHealthWhenLatencyLogFails(t *testing.T) {
	database := bootstrapDB(t)
	record := db.UpstreamRecord{AdapterType: "pypi", Name: "one", URL: "https://one.example", Priority: 1, ProbeMode: "active", ProbeInterval: "1s", Healthy: true}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{record})
	if err != nil {
		t.Fatal(err)
	}
	u := pool.Snapshot()[0]
	result := ProbeResult{Healthy: false, Latency: 25 * time.Millisecond, CheckedAt: time.Now().UTC()}
	u.applyProbe(result)

	callbackName := "test:fail_latency_log"
	if err := database.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "UpstreamLatencyLog" {
			tx.AddError(errors.New("latency log unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Create().Remove(callbackName) })

	if err := persistProbe(context.Background(), database, u, result); err == nil || err.Error() != "latency log unavailable" {
		t.Fatalf("err=%v", err)
	}
	var stored db.UpstreamRecord
	if err := database.First(&stored, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.Healthy {
		t.Fatal("health update was not rolled back")
	}
	var count int64
	if err := database.Model(&db.UpstreamLatencyLog{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("latency logs=%d", count)
	}
}
