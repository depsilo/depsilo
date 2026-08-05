package pypi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

func newTestChannelFamily(t *testing.T, upstreamURL string) (*ChannelFamily, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "channels.db"))
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
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "pytorch", URL: upstreamURL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	family, err := NewChannelFamily(
		manager,
		upstream.NewPrioritySelector(pool),
		config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour},
		database,
		Options{
			PathPrefix:         "/pypi-torch",
			AdapterID:          "extra:pytorch",
			UpstreamSimplePath: "/",
			ArtifactSigningKey: testArtifactSigningKey,
			ArtifactSelector:   upstream.NewEgressSelector(pool),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	family.Register(router.Group("/pypi-torch"))
	return family, router
}

func TestChannelFamilyRoutesCPUCUDAAndROCmToIsolatedUpstreamPaths(t *testing.T) {
	var mu sync.Mutex
	var requested []string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><body>`+r.URL.Path+`</body></html>`)
	}))
	defer upstreamServer.Close()

	_, router := newTestChannelFamily(t, upstreamServer.URL)
	for _, channel := range []string{"cpu", "cu118", "rocm6.4"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(
			http.MethodGet,
			"/pypi-torch/"+channel+"/simple/torch/",
			nil,
		))
		if recorder.Code != http.StatusOK {
			t.Fatalf("channel %s status = %d, body = %s", channel, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), "/"+channel+"/torch/") {
			t.Fatalf("channel %s body = %q", channel, recorder.Body.String())
		}
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"/cpu/torch/", "/cu118/torch/", "/rocm6.4/torch/"}
	if strings.Join(requested, "|") != strings.Join(want, "|") {
		t.Fatalf("upstream paths = %#v, want %#v", requested, want)
	}
}

func TestChannelFamilySeparatesCacheAndArtifactAudienceByChannel(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		channel := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<a href="https://download.pytorch.org/whl/`+channel+`/torch-1.0.0-py3-none-any.whl">torch</a>`)
	}))
	defer upstreamServer.Close()

	family, router := newTestChannelFamily(t, upstreamServer.URL)
	requestIndex := func(channel string) string {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(
			http.MethodGet,
			"/pypi-torch/"+channel+"/simple/torch/",
			nil,
		))
		if recorder.Code != http.StatusOK {
			t.Fatalf("channel %s status = %d, body = %s", channel, recorder.Code, recorder.Body.String())
		}
		match := hrefRe.FindStringSubmatch(recorder.Body.String())
		return hrefFromMatch(match)
	}

	cuHref := requestIndex("cu118")
	_ = requestIndex("cpu")
	parsed, err := url.Parse(cuHref)
	if err != nil || cuHref == "" {
		t.Fatalf("rewritten href = %q, err = %v", cuHref, err)
	}
	reference := strings.TrimPrefix(parsed.Path, "/pypi-torch/cu118/files")
	cuHandler, _ := family.handlerForChannel("cu118")
	cpuHandler, _ := family.handlerForChannel("cpu")
	if _, external, err := cuHandler.resolveExternalArtifact(reference); err != nil || !external {
		t.Fatalf("origin channel rejected its token: external=%v err=%v", external, err)
	}
	if _, external, err := cpuHandler.resolveExternalArtifact(reference); !external || err == nil {
		t.Fatalf("cross-channel token was accepted: external=%v err=%v", external, err)
	}

	cuKey := signedIndexCacheKey(channelCacheNamespace("extra:pytorch", "cu118"), "torch", testArtifactSigningKey)
	cpuKey := signedIndexCacheKey(channelCacheNamespace("extra:pytorch", "cpu"), "torch", testArtifactSigningKey)
	if cuKey == cpuKey {
		t.Fatal("channel cache keys are not isolated")
	}
	if channel, pkg, ok := ChannelIndexFromCacheKey("extra:pytorch", cuKey); !ok || channel != "cu118" || pkg != "torch" {
		t.Fatalf("parsed channel cache key = (%q, %q, %v)", channel, pkg, ok)
	}
}

func TestChannelFamilyRejectsUnsafeChannelNames(t *testing.T) {
	family := &ChannelFamily{prototype: &Handler{adapterID: "extra:pytorch"}}
	for _, channel := range []string{
		"", ".", "..", "CU128", "-cpu", "cpu-", "rocm/6.4", "cu%2f128",
		strings.Repeat("a", maxIndexChannelBytes+1),
	} {
		if _, ok := family.handlerForChannel(channel); ok {
			t.Errorf("unsafe channel %q was accepted", channel)
		}
	}
}

func TestChannelFamilyRejectsEncodedChannelSegments(t *testing.T) {
	var upstreamCalls int
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls++
		_, _ = io.WriteString(w, "unexpected")
	}))
	defer upstreamServer.Close()
	_, router := newTestChannelFamily(t, upstreamServer.URL)

	for _, requestPath := range []string{
		"/pypi-torch/%63pu/simple/torch/",
		"/pypi-torch/cu%252f128/simple/torch/",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestPath, nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("path %q status = %d, body = %s", requestPath, recorder.Code, recorder.Body.String())
		}
	}
	if upstreamCalls != 0 {
		t.Fatalf("encoded channels reached upstream %d times", upstreamCalls)
	}
}
