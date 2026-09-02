package huggingface

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

const durableTestToken = "0123456789abcdef0123456789abcdef"

type handlerRevocationStore struct {
	mu           sync.Mutex
	marker       repositoryRevocationMarker
	beginReady   chan struct{}
	releaseBegin chan struct{}
	deleteErr    error
}

func (s *handlerRevocationStore) Load(context.Context) ([]repositoryRevocationMarker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.marker.Repository == "" {
		return nil, nil
	}
	return []repositoryRevocationMarker{s.marker}, nil
}
func (s *handlerRevocationStore) Begin(_ context.Context, repository, escaped string) (string, error) {
	s.mu.Lock()
	s.marker = repositoryRevocationMarker{Repository: repository, EscapedRepo: escaped, Token: durableTestToken}
	s.mu.Unlock()
	if s.beginReady != nil {
		close(s.beginReady)
		<-s.releaseBegin
	}
	return durableTestToken, nil
}
func (s *handlerRevocationStore) MarkCleanupSafe(_ context.Context, repository, token string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.marker.Repository != repository || s.marker.Token != token {
		return false, nil
	}
	s.marker.CleanupSafe = true
	return true, nil
}
func (s *handlerRevocationStore) DeleteCleanupSafe(_ context.Context, repository, token string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return false, s.deleteErr
	}
	if s.marker.Repository != repository || s.marker.Token != token || !s.marker.CleanupSafe {
		return false, nil
	}
	s.marker = repositoryRevocationMarker{}
	return true, nil
}

type failingDeleteStorage struct {
	cache.Storage
	fail atomic.Bool
}

func (s *failingDeleteStorage) Delete(ctx context.Context, key string) error {
	if s.fail.Load() {
		return errors.New("object delete unavailable")
	}
	return s.Storage.Delete(ctx, key)
}

func TestHandlerPersistsMarkerBeforeRepositoryCleanup(t *testing.T) {
	database, manager, _ := newDurableHandlerFixture(t)
	key := "huggingface/acme/model/resolve/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/a.bin"
	if err := database.Create(&db.CacheEntry{Key: key, AdapterType: "huggingface", PackageName: "acme/model"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.HuggingFaceRefPin{Key: "huggingface/acme/model/ref/main", Commit: strings.Repeat("a", 40), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	store := &handlerRevocationStore{beginReady: make(chan struct{}), releaseBegin: make(chan struct{})}
	handler := &Handler{cacheMgr: manager, db: database, revocations: newRepositoryRevocationGate(), revocationStore: store}
	done := make(chan struct{})
	go func() {
		handler.revokeRepository(context.Background(), ParseRequestPath("/acme/model/resolve/main/a.bin"))
		close(done)
	}()
	<-store.beginReady
	markers, _ := store.Load(context.Background())
	if len(markers) != 1 {
		t.Fatal("durable marker was not present before cleanup")
	}
	for model, label := range map[any]string{&db.CacheEntry{}: "cache row", &db.HuggingFaceRefPin{}: "ref pin"} {
		var count int64
		if err := database.Model(model).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("%s changed before marker commit: count=%d err=%v", label, count, err)
		}
	}
	close(store.releaseBegin)
	<-done
}

func TestHandlerRevocationPurgesLegacyCaseAliasCacheAndRefPins(t *testing.T) {
	database, manager, _ := newDurableHandlerFixture(t)
	key := "huggingface/OpenAI/Whisper-Tiny/resolve/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/config.json"
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "huggingface", PackageName: "OpenAI/Whisper-Tiny", StoragePath: key,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.HuggingFaceRefPin{
		Key: "huggingface/OpenAI/Whisper-Tiny/ref/main", Commit: strings.Repeat("a", 40),
		ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	handler := New(manager, nil, config.CacheConfig{}, database)
	handler.revokeRepository(
		context.Background(),
		ParseRequestPath("/openai/whisper-tiny/resolve/main/config.json"),
	)

	for model, label := range map[any]string{&db.CacheEntry{}: "cache row", &db.HuggingFaceRefPin{}: "ref pin"} {
		var count int64
		if err := database.Model(model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("revocation retained %d legacy alias %s", count, label)
		}
	}
}

func TestHandlerRestartBypassesResidualCommitCacheAfterDoubleDeleteFailure(t *testing.T) {
	database, manager, storage := newDurableHandlerFixture(t)
	key := "huggingface/acme/model/resolve/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/a.bin"
	if err := storage.Put(context.Background(), key, strings.NewReader("stale"), 5, "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.CacheEntry{Key: key, AdapterType: "huggingface", PackageName: "acme/model", StoragePath: key, Size: 5, ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`CREATE TRIGGER fail_cache_delete BEFORE DELETE ON cache_entries BEGIN SELECT RAISE(FAIL, 'db delete unavailable'); END;`).Error; err != nil {
		t.Fatal(err)
	}
	storage.fail.Store(true)
	h1 := New(manager, nil, config.CacheConfig{}, database)
	h1.revokeRepository(context.Background(), ParseRequestPath("/acme/model/resolve/main/a.bin"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	_ = manager.Close(ctx)
	cancel()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private", http.StatusForbidden)
	}))
	defer origin.Close()
	manager2 := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() { _ = manager2.Close(context.Background()) })
	h2 := newDurableHTTPHandler(t, manager2, database, origin.URL)
	rec := httptest.NewRecorder()
	router := gin.New()
	h2.Register(router.Group("/huggingface"))
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/huggingface/acme/model/resolve/"+strings.Repeat("a", 40)+"/a.bin", nil))
	if rec.Code != http.StatusForbidden || rec.Body.String() == "stale" {
		t.Fatalf("restart served residual cache: (%d, %q)", rec.Code, rec.Body.String())
	}
}

func TestHandlerDeleteFailureKeepsGateClosedUntilMarkerRemovalRecovers(t *testing.T) {
	store := &handlerRevocationStore{marker: repositoryRevocationMarker{Repository: "acme/model", EscapedRepo: "acme/model", Token: durableTestToken, CleanupSafe: true}, deleteErr: errors.New("marker delete unavailable")}
	h := &Handler{revocations: newRepositoryRevocationGate(), revocationStore: store}
	h.loadRepositoryRevocations()
	request := httptest.NewRequest(http.MethodGet, "/huggingface/acme/model/resolve/main/a.bin", nil)
	success := &resolvedResponse{StatusCode: http.StatusOK, Header: make(http.Header)}
	target := "/acme/model/resolve/main/a.bin"
	h.observeDirectRepositoryResponse(
		request,
		target,
		success,
		h.directRepositoryRevocationTicket(request, target),
	)
	repository, _ := repositoryForParsed(ParseRequestPath("/acme/model/resolve/main/a.bin"))
	if !h.repositoryRevoked(repository) {
		t.Fatal("anonymous 2xx opened gate after durable marker delete failed")
	}
	store.mu.Lock()
	store.deleteErr = nil
	store.mu.Unlock()
	h.observeDirectRepositoryResponse(
		request,
		target,
		success,
		h.directRepositoryRevocationTicket(request, target),
	)
	if h.repositoryRevoked(repository) {
		t.Fatal("gate remained closed after durable marker removal recovered")
	}
}

func TestHandlerNewLoadFailureDisablesCacheGlobally(t *testing.T) {
	database, manager, storage := newDurableHandlerFixture(t)
	key := "huggingface/acme/model/resolve/" + strings.Repeat("a", 40) + "/a.bin"
	if err := storage.Put(context.Background(), key, strings.NewReader("cached-secret"), 13, "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.CacheEntry{
		Key:         key,
		AdapterType: "huggingface",
		PackageName: "acme/model",
		StoragePath: key,
		Size:        13,
		ExpiresAt:   time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrator().DropTable(&db.HuggingFaceRepositoryRevocation{}); err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "origin unavailable", http.StatusServiceUnavailable)
	}))
	defer origin.Close()
	h := newDurableHTTPHandler(t, manager, database, origin.URL)
	for _, path := range []string{"/acme/model/resolve/" + strings.Repeat("a", 40) + "/a.bin", "/other/repo/resolve/" + strings.Repeat("b", 40) + "/b.bin"} {
		repository, _ := repositoryForParsed(ParseRequestPath(path))
		if !h.repositoryRevoked(repository) {
			t.Fatalf("Load failure did not fail closed globally for %q", path)
		}
	}
	router := gin.New()
	h.Register(router.Group("/huggingface"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/huggingface/acme/model/resolve/"+strings.Repeat("a", 40)+"/a.bin", nil))
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() == "cached-secret" {
		t.Fatalf("Load failure served cache instead of origin response: (%d, %q)", rec.Code, rec.Body.String())
	}
}

func newDurableHandlerFixture(t *testing.T) (*gorm.DB, *cache.Manager, *failingDeleteStorage) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "durable-handler.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	local, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	storage := &failingDeleteStorage{Storage: local}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	return database, manager, storage
}

func newDurableHTTPHandler(t *testing.T, manager *cache.Manager, database *gorm.DB, origin string) *Handler {
	t.Helper()
	pool, err := upstream.NewPool([]config.UpstreamConfig{{Name: "origin", URL: origin, Priority: 1, ProbeMode: "passive"}})
	if err != nil {
		t.Fatal(err)
	}
	return New(manager, upstream.NewPrioritySelector(pool), config.CacheConfig{TTLBlob: time.Hour}, database)
}
