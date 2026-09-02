package pypi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

var testArtifactSigningKey = []byte("0123456789abcdef0123456789abcdef")

type countingSelector struct {
	upstream *upstream.Upstream
	calls    atomic.Int64
}

type unavailableSelector struct{}

func (unavailableSelector) Select(context.Context) (*upstream.Upstream, error) {
	return nil, errors.New("test upstream unavailable")
}

type auditCapture struct {
	entries []db.AuditLog
}

func (capture *auditCapture) Log(entry db.AuditLog) {
	capture.entries = append(capture.entries, entry)
}

func (s *countingSelector) Select(context.Context) (*upstream.Upstream, error) {
	s.calls.Add(1)
	return s.upstream, nil
}

func TestPackageIndexUpstreamFailureIsRecordedAsAuditError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "pypi-error.db"))
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
	audit := &auditCapture{}
	release := adapter.InstallAccessHooks(nil, audit)
	t.Cleanup(release)

	handler := New(manager, unavailableSelector{}, config.CacheConfig{TTLIndex: time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/pypi"))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/pypi/simple/requests/", nil)
	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if len(audit.entries) != 1 {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
	entry := audit.entries[0]
	if entry.Ecosystem != "pypi" || entry.PackageName != "requests" || entry.Action != "metadata" ||
		entry.CacheResult != "error" || entry.StatusCode != http.StatusBadGateway {
		t.Fatalf("audit entry = %#v", entry)
	}
}

type recordingQuarantineChecker struct {
	ecosystem string
}

func (c *recordingQuarantineChecker) Check(_ context.Context, ecosystem, _, _, _ string) adapter.QuarantineDecision {
	c.ecosystem = ecosystem
	return adapter.QuarantineDecision{Allowed: false, Code: "TEST_BLOCK", Reason: "blocked by test"}
}

func TestPackageIndexUsesTTLAndSupportsForcedConditionalRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var mu sync.Mutex
	var validators []string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		validators = append(validators, r.Header.Get("If-None-Match"))
		mu.Unlock()
		if r.Header.Get("If-None-Match") == `"pillow-v2"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("ETag", `"pillow-v2"`)
		_, _ = io.WriteString(w, `<html><a href="https://files.pythonhosted.org/packages/aa/pillow-12.3.0.whl">pillow-12.3.0.whl</a></html>`)
	}))
	defer upstreamServer.Close()

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "pypi.db"))
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
	key := IndexCacheKey("pypi", "pillow")
	old := `<html><a href="/pypi/files/packages/aa/pillow-12.2.0.whl">pillow-12.2.0.whl</a></html>`
	if err := storage.Put(context.Background(), key, io.NopCloser(strings.NewReader(old)), int64(len(old)), "text/html"); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		PackageName: "pillow", StoragePath: key, ContentType: "text/html",
		ETag: `"pillow-v1"`, ExpiresAt: time.Now().Add(time.Hour), LastAccessed: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive"}})
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
	handler := New(manager, upstream.NewPrioritySelector(pool), config.CacheConfig{TTLIndex: time.Hour, TTLBlob: 72 * time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/pypi"))

	request := func(forced bool) string {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/pypi/simple/pillow/", nil)
		if forced {
			req = req.WithContext(cache.WithForceRefresh(req.Context()))
		}
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}

	if body := request(false); !strings.Contains(body, "pillow-12.2.0") {
		t.Fatalf("fresh TTL cache was not served directly: %s", body)
	}
	mu.Lock()
	if len(validators) != 0 {
		t.Fatalf("fresh TTL cache unexpectedly contacted upstream: %#v", validators)
	}
	mu.Unlock()

	if body := request(true); !strings.Contains(body, "pillow-12.3.0") {
		t.Fatalf("forced refresh did not return current project index: %s", body)
	}
	deadline := time.Now().Add(time.Second)
	for {
		var entry db.CacheEntry
		if err := database.Where("key = ?", key).First(&entry).Error; err != nil {
			t.Fatal(err)
		}
		if entry.ETag == `"pillow-v2"` {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("new validator was not persisted: %+v", entry)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if body := request(false); !strings.Contains(body, "pillow-12.3.0") {
		t.Fatalf("refreshed index was not served from TTL cache: %s", body)
	}
	if body := request(true); !strings.Contains(body, "pillow-12.3.0") {
		t.Fatalf("304 response did not serve cached current index: %s", body)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(validators) != 2 || validators[0] != `"pillow-v1"` || validators[1] != `"pillow-v2"` {
		t.Fatalf("conditional validators = %#v", validators)
	}
}

func TestOptionsUseConfiguredUpstreamSimplePath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var requestedPath string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><body></body></html>`)
	}))
	defer upstreamServer.Close()

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "pypi.db"))
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
	pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive"}})
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
	handler, err := NewWithOptions(manager, upstream.NewPrioritySelector(pool), config.CacheConfig{
		TTLIndex: time.Hour,
		TTLBlob:  72 * time.Hour,
	}, database, Options{
		PathPrefix:         "/pypi-torch-cu128",
		AdapterID:          "extra:pytorch-cu128",
		UpstreamSimplePath: "/",
		ArtifactSigningKey: testArtifactSigningKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router.Group("/pypi-torch-cu128"))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/pypi-torch-cu128/simple/torch/", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if requestedPath != "/torch/" {
		t.Fatalf("upstream path = %q, want /torch/", requestedPath)
	}
}

func TestExtraIndexRequiresStrongArtifactSigningKey(t *testing.T) {
	t.Parallel()
	for _, signingKey := range [][]byte{nil, []byte("too-short")} {
		_, err := NewWithOptions(nil, nil, config.CacheConfig{}, nil, Options{
			PathPrefix:         "/pypi-extra",
			AdapterID:          "extra:test",
			UpstreamSimplePath: "/",
			ArtifactSigningKey: signingKey,
		})
		if err == nil {
			t.Fatalf("signing key with %d bytes was accepted", len(signingKey))
		}
	}
}

func TestExtraIndexSigningKeyRotationDoesNotReuseSignedIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var mu sync.Mutex
	var validators []string
	indexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		validators = append(validators, r.Header.Get("If-None-Match"))
		mu.Unlock()
		if r.Header.Get("If-None-Match") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("ETag", `"torch-v1"`)
		_, _ = io.WriteString(w, `<html><a href="https://cdn.example/torch-2.7.1-py3-none-any.whl">torch-2.7.1-py3-none-any.whl</a></html>`)
	}))
	defer indexServer.Close()

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "rotation.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storageRoot := filepath.Join(t.TempDir(), "cache")
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock", URL: indexServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	newManager := func() *cache.Manager {
		storage, storageErr := cache.NewLocalStorage(storageRoot)
		if storageErr != nil {
			t.Fatal(storageErr)
		}
		return cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	}
	newHandler := func(manager *cache.Manager, signingKey []byte) *Handler {
		handler, handlerErr := NewWithOptions(
			manager,
			upstream.NewPrioritySelector(pool),
			config.CacheConfig{TTLIndex: time.Millisecond, TTLBlob: time.Hour},
			database,
			Options{
				PathPrefix:         "/pypi-torch-cu128",
				AdapterID:          "extra:pytorch-cu128",
				UpstreamSimplePath: "/",
				ArtifactSigningKey: signingKey,
			},
		)
		if handlerErr != nil {
			t.Fatal(handlerErr)
		}
		return handler
	}
	requestIndex := func(handler *Handler) string {
		router := gin.New()
		handler.Register(router.Group("/pypi-torch-cu128"))
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pypi-torch-cu128/simple/torch/", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("index status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}
	closeManager := func(manager *cache.Manager) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}

	managerA := newManager()
	handlerA := newHandler(managerA, testArtifactSigningKey)
	_ = requestIndex(handlerA)
	closeManager(managerA)
	time.Sleep(5 * time.Millisecond)

	rotatedKey := []byte("abcdef0123456789abcdef0123456789")
	managerB := newManager()
	defer closeManager(managerB)
	handlerB := newHandler(managerB, rotatedKey)
	bodyB := requestIndex(handlerB)

	mu.Lock()
	gotValidators := append([]string(nil), validators...)
	mu.Unlock()
	if len(gotValidators) != 2 || gotValidators[0] != "" || gotValidators[1] != "" {
		t.Fatalf("conditional validators after key rotation = %#v; old signed cache was reused", gotValidators)
	}

	match := hrefRe.FindStringSubmatch(bodyB)
	href := hrefFromMatch(match)
	parsed, err := url.Parse(href)
	if err != nil || href == "" {
		t.Fatalf("rotated index href = %q, err = %v", href, err)
	}
	reference := strings.TrimPrefix(parsed.Path, "/pypi-torch-cu128/files")
	if _, external, err := handlerB.resolveExternalArtifact(reference); err != nil || !external {
		t.Fatalf("rotated handler rejected its index reference: external=%v err=%v", external, err)
	}
	if _, external, err := handlerA.resolveExternalArtifact(reference); !external || err == nil {
		t.Fatalf("old handler accepted the rotated reference: external=%v err=%v", external, err)
	}
}

func TestExternalArtifactDownloadIsCachedAndSupportsPEP658Metadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var mu sync.Mutex
	wheelRequests := 0
	metadataRequests := 0
	const wheelPath = "/whl/cu128/torch-2.7.1%2Bcu128-py3-none-any.whl"
	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.EscapedPath() {
		case wheelPath:
			wheelRequests++
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "cached-wheel")
		case wheelPath + ".metadata":
			metadataRequests++
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "Metadata-Version: 2.4\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer artifactServer.Close()

	indexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/torch/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<a href="`+artifactServer.URL+wheelPath+`#sha256=abcd" data-core-metadata="sha256=ef01">torch</a>`)
	}))
	defer indexServer.Close()

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "pypi.db"))
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
	pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "mock", URL: indexServer.URL, Priority: 1, ProbeMode: "passive"}})
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
	metadataSelector := &countingSelector{upstream: pool.Snapshot()[0]}
	artifactSelector := &countingSelector{upstream: pool.Snapshot()[0]}
	handler, err := NewWithOptions(manager, metadataSelector, config.CacheConfig{
		TTLIndex: time.Hour,
		TTLBlob:  72 * time.Hour,
	}, database, Options{
		PathPrefix:         "/pypi-torch-cu128",
		AdapterID:          "extra:pytorch-cu128",
		UpstreamSimplePath: "/",
		ArtifactSigningKey: testArtifactSigningKey,
		ArtifactSelector:   artifactSelector,
	})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router.Group("/pypi-torch-cu128"))

	indexRecorder := httptest.NewRecorder()
	router.ServeHTTP(indexRecorder, httptest.NewRequest(http.MethodGet, "/pypi-torch-cu128/simple/torch/", nil))
	if indexRecorder.Code != http.StatusOK {
		t.Fatalf("index status = %d, body = %s", indexRecorder.Code, indexRecorder.Body.String())
	}
	hrefStart := strings.Index(indexRecorder.Body.String(), `href="`)
	if hrefStart < 0 {
		t.Fatalf("rewritten href missing: %s", indexRecorder.Body.String())
	}
	hrefStart += len(`href="`)
	hrefEnd := strings.Index(indexRecorder.Body.String()[hrefStart:], `"`)
	if hrefEnd < 0 {
		t.Fatalf("rewritten href is unterminated: %s", indexRecorder.Body.String())
	}
	href := indexRecorder.Body.String()[hrefStart : hrefStart+hrefEnd]
	parsedHref, err := url.Parse(href)
	if err != nil {
		t.Fatal(err)
	}
	if parsedHref.Fragment != "sha256=abcd" {
		t.Fatalf("artifact hash fragment = %q", parsedHref.Fragment)
	}

	get := func(requestPath string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestPath, nil))
		return recorder
	}
	for range 2 {
		recorder := get(parsedHref.EscapedPath())
		if recorder.Code != http.StatusOK || recorder.Body.String() != "cached-wheel" {
			t.Fatalf("wheel status = %d, body = %q", recorder.Code, recorder.Body.String())
		}
	}
	metadataRecorder := get(parsedHref.EscapedPath() + ".metadata")
	if metadataRecorder.Code != http.StatusOK || !strings.Contains(metadataRecorder.Body.String(), "Metadata-Version") {
		t.Fatalf("metadata status = %d, body = %q", metadataRecorder.Code, metadataRecorder.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if wheelRequests != 1 {
		t.Fatalf("wheel upstream requests = %d, want 1", wheelRequests)
	}
	if metadataRequests != 1 {
		t.Fatalf("metadata upstream requests = %d, want 1", metadataRequests)
	}
	if got := metadataSelector.calls.Load(); got != 1 {
		t.Fatalf("metadata selector calls = %d, want 1", got)
	}
	if got := artifactSelector.calls.Load(); got != 2 {
		t.Fatalf("artifact selector calls = %d, want 2 (wheel and metadata)", got)
	}
}

func TestExternalArtifactRouteRejectsTamperedToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, err := NewWithOptions(nil, nil, config.CacheConfig{}, nil, Options{
		PathPrefix:         "/pypi-torch-cu128",
		AdapterID:          "extra:pytorch-cu128",
		UpstreamSimplePath: "/",
		ArtifactSigningKey: testArtifactSigningKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router.Group("/pypi-torch-cu128"))
	target := "https://cdn.example/whl/cu128/torch-2.7.1-py3-none-any.whl"
	token, err := encodeExternalArtifactToken(testArtifactSigningKey, "extra:pytorch-cu128", target)
	if err != nil {
		t.Fatal(err)
	}
	tampered := token[:len(token)-1] + "A"
	if token[len(token)-1] == 'A' {
		tampered = token[:len(token)-1] + "B"
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pypi-torch-cu128/files/_external/"+tampered+"/torch-2.7.1-py3-none-any.whl", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestExternalArtifactReferenceValidatesFilenameAndMetadata(t *testing.T) {
	t.Parallel()
	handler := &Handler{
		adapterID:          "extra:pytorch-cu128",
		artifactSigningKey: append([]byte(nil), testArtifactSigningKey...),
	}
	target := "https://cdn.example/whl/torch-2.7.1%2Bcu128-py3-none-any.whl"
	token, err := encodeExternalArtifactToken(testArtifactSigningKey, handler.adapterID, target)
	if err != nil {
		t.Fatal(err)
	}

	valid, external, err := handler.resolveExternalArtifact("/_external/" + token + "/torch-2.7.1+cu128-py3-none-any.whl")
	if err != nil || !external || valid.url != target {
		t.Fatalf("valid reference = %+v, external=%v, err=%v", valid, external, err)
	}
	metadata, external, err := handler.resolveExternalArtifact("/_external/" + token + "/torch-2.7.1+cu128-py3-none-any.whl.metadata")
	if err != nil || !external || metadata.url != target+".metadata" {
		t.Fatalf("metadata reference = %+v, external=%v, err=%v", metadata, external, err)
	}
	sdistTarget := "https://cdn.example/source/demo-1.0.tar.gz"
	sdistToken, err := encodeExternalArtifactToken(testArtifactSigningKey, handler.adapterID, sdistTarget)
	if err != nil {
		t.Fatal(err)
	}
	sdistMetadata, external, err := handler.resolveExternalArtifact("/_external/" + sdistToken + "/demo-1.0.tar.gz.metadata")
	if err != nil || !external || sdistMetadata.url != sdistTarget+".metadata" {
		t.Fatalf("sdist metadata reference = %+v, external=%v, err=%v", sdistMetadata, external, err)
	}

	invalid := []string{
		"/_external/" + token + "/renamed-2.7.1-py3-none-any.whl",
		"/_external/" + token,
		"/_external/" + strings.Repeat("x", maxExternalArtifactTokenLen+1) + "/torch.whl",
	}
	for _, reference := range invalid {
		if _, external, err := handler.resolveExternalArtifact(reference); !external || err == nil {
			t.Errorf("invalid reference %q accepted: external=%v err=%v", reference, external, err)
		}
	}

	otherRoute := *handler
	otherRoute.adapterID = "extra:pytorch-cu129"
	if _, external, err := otherRoute.resolveExternalArtifact("/_external/" + token + "/torch-2.7.1+cu128-py3-none-any.whl"); !external || err == nil {
		t.Fatalf("cross-route token accepted: external=%v err=%v", external, err)
	}
}

func TestExternalArtifactReferenceRejectsSignedUnsafeURL(t *testing.T) {
	t.Parallel()
	handler := &Handler{adapterID: "extra:test", artifactSigningKey: testArtifactSigningKey}
	for _, target := range []string{
		"https://user:password@cdn.example/pkg-1.0-py3-none-any.whl",
		"https://cdn.example/pkg-1.0-py3-none-any.whl#sha256=abcd",
		"ftp://cdn.example/pkg-1.0-py3-none-any.whl",
		"https://cdn.example/a%2Fb-1.0-py3-none-any.whl",
	} {
		t.Run(target, func(t *testing.T) {
			token, err := encodeExternalArtifactToken(testArtifactSigningKey, handler.adapterID, target)
			if err != nil {
				t.Fatal(err)
			}
			if _, external, err := handler.resolveExternalArtifact("/_external/" + token + "/pkg-1.0-py3-none-any.whl"); !external || err == nil {
				t.Fatalf("unsafe signed target accepted: external=%v err=%v", external, err)
			}
		})
	}
}

func TestExtraIndexArtifactKeepsExtraPolicyIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	checker := &recordingQuarantineChecker{}
	handler := &Handler{
		pathPrefix:         "/pypi-torch-cu128",
		adapterID:          "extra:pytorch-cu128",
		artifactSigningKey: append([]byte(nil), testArtifactSigningKey...),
	}
	target := "https://cdn.example/torch-2.7.1-py3-none-any.whl"
	token, err := encodeExternalArtifactToken(testArtifactSigningKey, handler.adapterID, target)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router.Group(handler.pathPrefix))
	scoped := adapter.NewRequestScope(nil, nil, checker, nil).Wrap(router)
	recorder := httptest.NewRecorder()
	scoped.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet,
		handler.pathPrefix+"/files/_external/"+token+"/torch-2.7.1-py3-none-any.whl",
		nil,
	))
	if recorder.Code != http.StatusUnavailableForLegalReasons {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if checker.ecosystem != "extra:pytorch-cu128" {
		t.Fatalf("checker ecosystem = %q", checker.ecosystem)
	}
}
