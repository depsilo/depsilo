package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	npmadapter "depsilo/internal/adapter/npm"
	"depsilo/internal/asyncruntime"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

func TestWarmupMissPersistsNonEmptyBodyBeforeCompleting(t *testing.T) {
	payload := strings.Repeat("<a href=\"https://files.pythonhosted.org/packages/requests-2.32.3.whl\">requests</a>", 2048)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/simple/requests/" {
			t.Errorf("upstream path = %q, want /simple/requests/", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "warmup.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close warmup test DB: %v", err)
		}
	})

	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	pool, err := upstream.NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "pypi", Name: "mock", URL: server.URL,
		Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: false,
	}})
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), 72*time.Hour)
	closeWarmupTestManager(t, manager)
	handler := NewWarmupHandler(nil, manager, map[string]*upstream.Pool{"pypi": pool}, &config.Config{
		Cache: config.CacheConfig{TTLIndex: time.Hour},
	})

	done := make(chan struct{})
	go func() {
		handler.doWarmup(context.Background(), "pypi", []string{"requests==2.32.0"}, pool)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("warmup did not complete")
	}

	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls.Load())
	}
	key := "pypi/simple/requests/index.html"
	reader, _, err := storage.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("warmup returned before cache storage completed: %v", err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	want, err := rewriteWarmupIndex("pypi", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("cached body was not rewritten through the PyPI proxy: got length %d, want %d", len(got), len(want))
	}
	var entry db.CacheEntry
	if err := database.Where("key = ?", key).First(&entry).Error; err != nil {
		t.Fatalf("warmup returned before cache metadata committed: %v", err)
	}
}

func TestWarmupNPMUsesAdapterKeyAndRewritesTarballs(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"name": "@scope/widget",
		"versions": map[string]any{
			"1.2.3": map[string]any{
				"dist": map[string]any{"tarball": "https://registry.example/@scope/widget/-/widget-1.2.3.tgz"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/@scope/widget" {
			t.Errorf("upstream path = %q, want /@scope/widget", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "npm-warmup.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "npm-cache"))
	if err != nil {
		t.Fatal(err)
	}
	pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "mock", URL: server.URL, Priority: 1, ProbeMode: "passive"}})
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), 72*time.Hour)
	closeWarmupTestManager(t, manager)
	handler := NewWarmupHandler(nil, manager, map[string]*upstream.Pool{"npm": pool}, &config.Config{Cache: config.CacheConfig{TTLIndex: time.Hour}})

	handler.doWarmup(context.Background(), "npm", []string{"@scope/widget@1.2.3"}, pool)

	reader, _, err := storage.Get(context.Background(), npmadapter.ScopedMetadataCacheKey("scope", "widget"))
	if err != nil {
		t.Fatalf("read npm warmup entry: %v", err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"tarball":"depsilo:npm-artifact-reference:v1:`) {
		t.Fatalf("npm tarball reference was not provenance-prepared before caching: %s", got)
	}
}

func closeWarmupTestManager(t *testing.T, manager *cache.Manager) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})
}

func TestNormalizeWarmupPackages(t *testing.T) {
	tests := []struct {
		name      string
		ecosystem string
		raw       []string
		want      []string
		wantErr   bool
	}{
		{name: "pypi specifiers and duplicates", ecosystem: "pypi", raw: []string{" Requests>=2 ", "requests==1", "# comment", "-r other.txt"}, want: []string{"Requests"}},
		{name: "npm versions and case-distinct legacy names", ecosystem: "npm", raw: []string{"react@19", "@scope/widget@1.2.3", "REACT", "legacy!pkg"}, want: []string{"react", "@scope/widget", "REACT", "legacy!pkg"}},
		{name: "unsupported ecosystem", ecosystem: "maven", raw: []string{"artifact"}, wantErr: true},
		{name: "invalid scoped npm", ecosystem: "npm", raw: []string{"@scope"}, wantErr: true},
		{name: "path injection", ecosystem: "pypi", raw: []string{"requests?source=other"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeWarmupPackages(tt.ecosystem, tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("packages = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeWarmupPackagesEnforcesLimit(t *testing.T) {
	raw := make([]string, maxWarmupPackages+1)
	for index := range raw {
		raw[index] = "package"
	}
	if _, err := normalizeWarmupPackages("pypi", raw); err == nil {
		t.Fatal("oversized warmup list was accepted")
	}
}

type captureTaskRunner struct {
	task      asyncruntime.Task
	submitErr error
	submits   int
}

func (runner *captureTaskRunner) Submit(task asyncruntime.Task) error {
	runner.submits++
	if runner.submitErr != nil {
		return runner.submitErr
	}
	runner.task = task
	return nil
}

func TestWarmupRejectsUnsupportedEcosystemBeforeScheduling(t *testing.T) {
	runner := &captureTaskRunner{}
	handler := NewWarmupHandler(runner, nil, nil, &config.Config{})
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/warmup", strings.NewReader(`{"ecosystem":"maven","packages":["artifact"]}`))
	ginContext.Request.Header.Set("Content-Type", "application/json")

	handler.Warmup(ginContext)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if runner.task != nil {
		t.Fatal("unsupported warmup was scheduled")
	}
}

func TestWarmupReportsUnavailableWhenRuntimeRejects(t *testing.T) {
	runner := &captureTaskRunner{submitErr: asyncruntime.ErrClosed}
	pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "mock", URL: "https://example.test", ProbeMode: "passive"}})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewWarmupHandler(runner, nil, map[string]*upstream.Pool{"pypi": pool}, &config.Config{})
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/warmup", strings.NewReader(`{"ecosystem":"pypi","packages":["requests"]}`))
	ginContext.Request.Header.Set("Content-Type", "application/json")

	handler.Warmup(ginContext)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if runner.task != nil {
		t.Fatal("rejected warmup retained a task")
	}

	runner.submitErr = nil
	recorder = httptest.NewRecorder()
	ginContext, _ = gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/warmup", strings.NewReader(`{"ecosystem":"pypi","packages":["requests"]}`))
	ginContext.Request.Header.Set("Content-Type", "application/json")
	handler.Warmup(ginContext)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("retry status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestWarmupAllowsOnlyOneInFlightTaskAndReleasesOnCompletion(t *testing.T) {
	runner := &captureTaskRunner{}
	pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "mock", URL: "https://example.test", ProbeMode: "passive"}})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewWarmupHandler(runner, nil, map[string]*upstream.Pool{"pypi": pool}, &config.Config{})
	request := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		ginContext, _ := gin.CreateTestContext(recorder)
		ginContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/warmup", strings.NewReader(`{"ecosystem":"pypi","packages":["requests"]}`))
		ginContext.Request.Header.Set("Content-Type", "application/json")
		handler.Warmup(ginContext)
		return recorder
	}

	if recorder := request(); recorder.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	firstTask := runner.task
	if recorder := request(); recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "WARMUP_RUNNING") {
		t.Fatalf("concurrent status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if runner.submits != 1 {
		t.Fatalf("runtime submissions = %d, want 1", runner.submits)
	}

	taskContext, cancelTask := context.WithCancel(context.Background())
	cancelTask()
	firstTask(taskContext)
	if recorder := request(); recorder.Code != http.StatusAccepted {
		t.Fatalf("post-completion status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
