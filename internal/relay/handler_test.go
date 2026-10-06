package relay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/cache"
	"depsilo/internal/cascade"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

type relayFixture struct {
	handler *Handler
	origin  *httptest.Server
	hits    *atomic.Int64
	cache   *cache.Manager
}

func newRelayFixture(t *testing.T, originHandler http.HandlerFunc) *relayFixture {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})
	hits := &atomic.Int64{}
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hits.Add(1)
		originHandler(writer, request)
	}))
	t.Cleanup(origin.Close)

	handler := &Handler{
		DB:       database,
		Cache:    manager,
		Identity: "parent-node",
		Token:    "shared-token",
		MaxHops:  3,
		MaxTTL:   24 * time.Hour,
		// The production default client refuses loopback targets. Tests point
		// ResolveEgress at the fixture origin, which is the same trust decision
		// the server makes when a child source matches a configured upstream.
		ResolveEgress: func(string, string) (*http.Client, bool) {
			return &http.Client{
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}, true
		},
	}
	return &relayFixture{handler: handler, origin: origin, hits: hits, cache: manager}
}

func relayRequest(t *testing.T, origin *httptest.Server, mutate func(*http.Request)) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, cascade.RelayPath, nil)
	request.Header.Set(cascade.HeaderToken, "shared-token")
	request.Header.Set(cascade.HeaderChain, "child-node")
	request.Header.Set(cascade.HeaderTarget, origin.URL+"/pkg")
	request.Header.Set(cascade.HeaderSource, origin.URL)
	request.Header.Set(cascade.HeaderTTL, "3600")
	request.Header.Set(cascade.HeaderKind, cascade.KindArtifact)
	if mutate != nil {
		mutate(request)
	}
	return request
}

func TestHandlerCachesOriginResponse(t *testing.T) {
	fixture := newRelayFixture(t, func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("ETag", `"v1"`)
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "payload")
	})

	first := httptest.NewRecorder()
	metadata := func(request *http.Request) {
		request.Header.Set(cascade.HeaderKind, cascade.KindIndex)
	}
	fixture.handler.ServeHTTP(first, relayRequest(t, fixture.origin, metadata))
	if first.Code != http.StatusOK || first.Body.String() != "payload" {
		t.Fatalf("first response = %d %q", first.Code, first.Body.String())
	}
	if got := first.Header().Get(cascade.HeaderCache); got != cascade.CacheMiss {
		t.Fatalf("first cache header = %q", got)
	}
	if got := first.Header().Get(cascade.HeaderInstance); got != "parent-node" {
		t.Fatalf("instance header = %q", got)
	}

	second := httptest.NewRecorder()
	fixture.handler.ServeHTTP(second, relayRequest(t, fixture.origin, metadata))
	if second.Code != http.StatusOK || second.Body.String() != "payload" {
		t.Fatalf("second response = %d %q", second.Code, second.Body.String())
	}
	if got := second.Header().Get(cascade.HeaderCache); got != cascade.CacheHit {
		t.Fatalf("second cache header = %q", got)
	}
	if got := fixture.hits.Load(); got != 1 {
		t.Fatalf("origin hits = %d, want 1", got)
	}
}

func TestHandlerRevalidatesStaleEntry(t *testing.T) {
	var (
		mu          sync.Mutex
		conditional bool
	)
	fixture := newRelayFixture(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("If-None-Match") == `"v1"` {
			mu.Lock()
			conditional = true
			mu.Unlock()
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.Header().Set("ETag", `"v1"`)
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "payload")
	})

	first := httptest.NewRecorder()
	metadata := func(request *http.Request) {
		request.Header.Set(cascade.HeaderKind, cascade.KindIndex)
	}
	fixture.handler.ServeHTTP(first, relayRequest(t, fixture.origin, metadata))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var count int64
		if err := fixture.handler.DB.Model(&db.CacheEntry{}).Where("adapter_type = ?", cacheAdapterType).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("relay cache entry was not committed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	update := fixture.handler.DB.Model(&db.CacheEntry{}).
		Where("adapter_type = ?", cacheAdapterType).
		Update("expires_at", time.Now().Add(-time.Hour))
	if err := update.Error; err != nil {
		t.Fatal(err)
	}
	if update.RowsAffected != 1 {
		t.Fatalf("expired %d relay cache rows, want 1", update.RowsAffected)
	}
	var stored db.CacheEntry
	if err := fixture.handler.DB.Where("adapter_type = ?", cacheAdapterType).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("stored relay entry key=%s etag=%q last_modified=%q", stored.Key, stored.ETag, stored.LastModified)

	second := httptest.NewRecorder()
	fixture.handler.ServeHTTP(second, relayRequest(t, fixture.origin, metadata))
	if second.Code != http.StatusOK || second.Body.String() != "payload" {
		t.Fatalf("second response = %d %q", second.Code, second.Body.String())
	}
	mu.Lock()
	usedConditional := conditional
	mu.Unlock()
	if !usedConditional {
		t.Fatal("stale relay entry did not revalidate with the stored ETag")
	}
}

func TestHandlerBypassesRedirects(t *testing.T) {
	fixture := newRelayFixture(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Location", "http://127.0.0.1:1/final")
		writer.WriteHeader(http.StatusFound)
	})

	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, relayRequest(t, fixture.origin, nil))
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Location") != "http://127.0.0.1:1/final" {
		t.Fatalf("Location = %q", recorder.Header().Get("Location"))
	}
	if got := recorder.Header().Get(cascade.HeaderCache); got != cascade.CacheBypass {
		t.Fatalf("cache header = %q", got)
	}
	if got := fixture.hits.Load(); got != 2 {
		t.Fatalf("origin hits = %d, want 2 (cache attempt + bypass attempt)", got)
	}
}

func TestHandlerDoesNotServeStaleBytesAfterAuthoritativeRemoval(t *testing.T) {
	var calls atomic.Int64
	fixture := newRelayFixture(t, func(writer http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			writer.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(writer, "payload")
			return
		}
		writer.WriteHeader(http.StatusNotFound)
	})
	metadata := func(request *http.Request) {
		request.Header.Set(cascade.HeaderKind, cascade.KindIndex)
	}
	first := httptest.NewRecorder()
	fixture.handler.ServeHTTP(first, relayRequest(t, fixture.origin, metadata))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var count int64
		if err := fixture.handler.DB.Model(&db.CacheEntry{}).Where("adapter_type = ?", cacheAdapterType).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("relay cache entry was not committed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := fixture.handler.DB.Model(&db.CacheEntry{}).
		Where("adapter_type = ?", cacheAdapterType).
		Update("expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}

	second := httptest.NewRecorder()
	fixture.handler.ServeHTTP(second, relayRequest(t, fixture.origin, metadata))
	if second.Code != http.StatusNotFound {
		t.Fatalf("second status = %d, want the authoritative 404", second.Code)
	}
}

func TestHandlerBypassesCacheWithoutTTL(t *testing.T) {
	fixture := newRelayFixture(t, func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "payload")
	})
	for index := 0; index < 2; index++ {
		recorder := httptest.NewRecorder()
		fixture.handler.ServeHTTP(recorder, relayRequest(t, fixture.origin, func(request *http.Request) {
			request.Header.Del(cascade.HeaderTTL)
		}))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d", recorder.Code)
		}
		if got := recorder.Header().Get(cascade.HeaderCache); got != cascade.CacheBypass {
			t.Fatalf("cache header = %q", got)
		}
	}
	if got := fixture.hits.Load(); got != 2 {
		t.Fatalf("origin hits = %d, want 2", got)
	}
}

func TestHandlerRejectsUnauthorizedLoopsAndHopLimits(t *testing.T) {
	fixture := newRelayFixture(t, func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})
	cases := []struct {
		name   string
		mutate func(*http.Request)
		status int
	}{
		{"bad token", func(request *http.Request) {
			request.Header.Set(cascade.HeaderToken, "wrong-token")
		}, http.StatusUnauthorized},
		{"loop", func(request *http.Request) {
			request.Header.Set(cascade.HeaderChain, "child-node,parent-node")
		}, http.StatusLoopDetected},
		{"hop limit", func(request *http.Request) {
			request.Header.Set(cascade.HeaderChain, "a,b,c")
		}, http.StatusLoopDetected},
		{"missing chain", func(request *http.Request) {
			request.Header.Del(cascade.HeaderChain)
		}, http.StatusBadRequest},
		{"bad target", func(request *http.Request) {
			request.Header.Set(cascade.HeaderTarget, "http:///missing-host")
		}, http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			fixture.handler.ServeHTTP(recorder, relayRequest(t, fixture.origin, testCase.mutate))
			if recorder.Code != testCase.status {
				t.Fatalf("status = %d, want %d (body=%s)", recorder.Code, testCase.status, recorder.Body.String())
			}
		})
	}
}

func TestHandlerDefaultClientRefusesLoopbackTargets(t *testing.T) {
	fixture := newRelayFixture(t, func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})
	fixture.handler.ResolveEgress = nil
	fixture.handler.DefaultClient = upstream.NewRelayClient()

	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, relayRequest(t, fixture.origin, func(request *http.Request) {
		request.Header.Del(cascade.HeaderTTL)
	}))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
}

func TestHandlerRequiresIdentityConfiguration(t *testing.T) {
	fixture := newRelayFixture(t, func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})
	fixture.handler.Identity = ""
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, relayRequest(t, fixture.origin, nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}
