package huggingface

import (
	"context"
	"io"
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
)

type blockingCacheReadStorage struct {
	cache.Storage
	block   atomic.Bool
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingCacheReadStorage) Get(
	ctx context.Context,
	key string,
) (io.ReadCloser, int64, error) {
	reader, size, err := s.Storage.Get(ctx, key)
	if err != nil || !s.block.Load() {
		return reader, size, err
	}
	return &blockingCacheReader{
		ReadCloser: reader,
		start: func() {
			s.once.Do(func() { close(s.started) })
			<-s.release
		},
	}, size, nil
}

type blockingCacheReader struct {
	io.ReadCloser
	once  sync.Once
	start func()
}

func (r *blockingCacheReader) Read(buffer []byte) (int, error) {
	r.once.Do(r.start)
	return r.ReadCloser.Read(buffer)
}

type orderedRevocationStore struct {
	mu           sync.Mutex
	calls        int
	marker       repositoryRevocationMarker
	firstStarted chan struct{}
	releaseFirst chan struct{}
}

func (s *orderedRevocationStore) Load(
	context.Context,
) ([]repositoryRevocationMarker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.marker.Repository == "" {
		return nil, nil
	}
	return []repositoryRevocationMarker{s.marker}, nil
}

func (s *orderedRevocationStore) Begin(
	_ context.Context,
	repository string,
	escapedRepo string,
) (string, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	if call == 1 {
		close(s.firstStarted)
		<-s.releaseFirst
	}
	token := strings.Repeat(string(rune('a'+call-1)), 32)
	s.mu.Lock()
	s.marker = repositoryRevocationMarker{
		Repository:  repository,
		EscapedRepo: escapedRepo,
		Token:       token,
	}
	s.mu.Unlock()
	return token, nil
}

func (s *orderedRevocationStore) MarkCleanupSafe(
	_ context.Context,
	repository string,
	token string,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.marker.Repository != repository || s.marker.Token != token {
		return false, nil
	}
	s.marker.CleanupSafe = true
	return true, nil
}

func (s *orderedRevocationStore) DeleteCleanupSafe(
	_ context.Context,
	repository string,
	token string,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.marker.Repository != repository ||
		s.marker.Token != token ||
		!s.marker.CleanupSafe {
		return false, nil
	}
	s.marker = repositoryRevocationMarker{}
	return true, nil
}

func TestOldAnonymousSuccessCannotRestoreNewRevocationGeneration(t *testing.T) {
	const target = "/acme/model/resolve/main/model.bin"
	parsed := ParseRequestPath(target)
	repository, ok := repositoryForParsed(parsed)
	if !ok {
		t.Fatal("failed to derive repository")
	}
	request := httptest.NewRequest(http.MethodGet, "/huggingface"+target, nil)
	success := &resolvedResponse{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
	}

	tests := []struct {
		name         string
		startRevoked bool
		revokeAgain  bool
	}{
		{
			name:         "request started before revocation",
			startRevoked: false,
			revokeAgain:  true,
		},
		{
			name:         "request belongs to an older revocation generation",
			startRevoked: true,
			revokeAgain:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := &Handler{revocations: newRepositoryRevocationGate()}
			if test.startRevoked {
				generation, _ := handler.revocations.begin(repository.packageName)
				handler.revocations.finish(
					repository.packageName,
					generation,
					"",
					true,
				)
			}
			ticket := handler.directRepositoryRevocationTicket(request, target)
			if test.revokeAgain {
				generation, _ := handler.revocations.begin(repository.packageName)
				handler.revocations.finish(
					repository.packageName,
					generation,
					"",
					true,
				)
			}

			handler.observeDirectRepositoryResponse(
				request,
				target,
				success,
				ticket,
			)
			if !handler.repositoryRevoked(repository) {
				t.Fatal("an old anonymous success restored a newer revocation")
			}
		})
	}
}

func TestSlowOldAnonymousResponseCannotRestoreNewRevocationGeneration(
	t *testing.T,
) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		body   = "public-again"
	)
	requestStarted := make(chan struct{})
	releaseResponse := make(chan struct{})
	var startOnce sync.Once
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseResponse) })
	})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		startOnce.Do(func() { close(requestStarted) })
		<-releaseResponse
		w.Header().Set("Content-Length", "12")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Repo-Commit", commit)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	parsed := ParseRequestPath("/acme/model/resolve/" + commit + "/model.bin")
	repository, ok := repositoryForParsed(parsed)
	if !ok {
		t.Fatal("failed to derive repository")
	}
	handler.revokeRepository(context.Background(), parsed)

	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	responseDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(
				http.MethodGet,
				"/huggingface/acme/model/resolve/"+commit+"/model.bin",
				nil,
			),
		)
		responseDone <- recorder
	}()
	select {
	case <-requestStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for old anonymous request")
	}

	handler.revokeRepository(context.Background(), parsed)
	currentTicket := handler.revocations.ticket(repository.packageName)
	var currentMarker db.HuggingFaceRepositoryRevocation
	if err := database.Where("repository = ?", repository.packageName).
		Take(&currentMarker).Error; err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(releaseResponse) })

	select {
	case response := <-responseDone:
		if response.Code != http.StatusOK || response.Body.String() != body {
			t.Fatalf(
				"old anonymous response = (%d, %q)",
				response.Code,
				response.Body.String(),
			)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for old anonymous response")
	}
	if !handler.repositoryRevoked(repository) {
		t.Fatal("old anonymous response restored a newer revocation")
	}
	afterTicket := handler.revocations.ticket(repository.packageName)
	if afterTicket.generation != currentTicket.generation {
		t.Fatalf(
			"revocation generation changed from %d to %d",
			currentTicket.generation,
			afterTicket.generation,
		)
	}
	var afterMarker db.HuggingFaceRepositoryRevocation
	if err := database.Where("repository = ?", repository.packageName).
		Take(&afterMarker).Error; err != nil {
		t.Fatal(err)
	}
	if afterMarker.Token != currentMarker.Token {
		t.Fatalf(
			"old anonymous response changed marker token from %q to %q",
			currentMarker.Token,
			afterMarker.Token,
		)
	}
}

func TestCacheMissFollowersDoNotBlockRepositoryRevocationCleanup(
	t *testing.T,
) {
	gin.SetMode(gin.TestMode)

	const commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	originStarted := make(chan struct{})
	releaseOrigin := make(chan struct{})
	var startOnce sync.Once
	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		startOnce.Do(func() { close(originStarted) })
		<-releaseOrigin
		http.Error(w, "repository became private", http.StatusForbidden)
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	path := "/huggingface/acme/model/resolve/" + commit + "/model.bin"

	responses := make(chan *httptest.ResponseRecorder, 2)
	send := func() {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(http.MethodGet, path, nil),
		)
		responses <- recorder
	}
	go send()
	select {
	case <-originStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for cache miss leader")
	}
	go send()

	repository, ok := repositoryForParsed(ParseRequestPath(
		"/acme/model/resolve/" + commit + "/model.bin",
	))
	if !ok {
		t.Fatal("failed to derive repository")
	}
	waitForRepositoryAdmissions(t, handler.revocations, repository.packageName, 2)
	close(releaseOrigin)

	deadline := time.After(3 * time.Second)
	for range 2 {
		select {
		case response := <-responses:
			body, _ := io.ReadAll(response.Result().Body)
			if response.Code != http.StatusForbidden ||
				!strings.Contains(string(body), "repository became private") {
				t.Fatalf(
					"revoked response = (%d, %q)",
					response.Code,
					string(body),
				)
			}
		case <-deadline:
			t.Fatal("cache miss follower and revocation cleanup blocked each other")
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("origin requests = %d, want one coalesced fetch", requests.Load())
	}
	if !handler.repositoryRevoked(repository) {
		t.Fatal("repository gate was not closed")
	}
	waitForRepositoryMarkerSafe(t, database, repository.packageName)
}

func TestRepositoryRevocationClosesGateBeforeSlowErrorBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit      = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		siblingBody = "cached-public-sibling"
	)
	var private atomic.Bool
	headersSent := make(chan struct{})
	releaseBodies := make(chan struct{})
	var headersOnce sync.Once
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseBodies) })
	})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !private.Load() {
			w.Header().Set("Content-Length", "21")
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("X-Repo-Commit", commit)
			_, _ = io.WriteString(w, siblingBody)
			return
		}
		w.Header().Set("X-Error-Code", "RepositoryNotFound")
		w.WriteHeader(http.StatusForbidden)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		headersOnce.Do(func() { close(headersSent) })
		<-releaseBodies
		_, _ = io.WriteString(w, "repository became private")
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	siblingPath := "/huggingface/acme/model/resolve/" + commit + "/b.bin"
	warm := httptest.NewRecorder()
	router.ServeHTTP(
		warm,
		httptest.NewRequest(http.MethodGet, siblingPath, nil),
	)
	if warm.Code != http.StatusOK || warm.Body.String() != siblingBody {
		t.Fatalf("warm sibling = (%d, %q)", warm.Code, warm.Body.String())
	}
	waitForCacheEntry(
		t,
		database,
		"huggingface/acme/model/resolve/"+commit+"/b.bin",
	)

	private.Store(true)
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(
				http.MethodGet,
				"/huggingface/acme/model/resolve/"+commit+"/a.bin",
				nil,
			),
		)
		firstDone <- recorder
	}()
	select {
	case <-headersSent:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for private response headers")
	}

	repository, ok := repositoryForParsed(ParseRequestPath(
		"/acme/model/resolve/" + commit + "/a.bin",
	))
	if !ok {
		t.Fatal("failed to derive repository")
	}
	waitForRepositoryRevoked(t, handler, repository)
	waitForRepositoryMarker(t, database, repository.packageName)

	siblingDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(http.MethodGet, siblingPath, nil),
		)
		siblingDone <- recorder
	}()
	select {
	case response := <-siblingDone:
		t.Fatalf(
			"sibling cache remained readable while 403 body was blocked: (%d, %q)",
			response.Code,
			response.Body.String(),
		)
	case <-time.After(30 * time.Millisecond):
	}

	releaseOnce.Do(func() { close(releaseBodies) })
	for name, responses := range map[string]<-chan *httptest.ResponseRecorder{
		"first":   firstDone,
		"sibling": siblingDone,
	} {
		select {
		case response := <-responses:
			if response.Code != http.StatusForbidden ||
				response.Body.String() == siblingBody {
				t.Fatalf(
					"%s private response = (%d, %q)",
					name,
					response.Code,
					response.Body.String(),
				)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for %s private response", name)
		}
	}
}

func TestRepositoryRevocationWaitsForCachedResponseStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "cccccccccccccccccccccccccccccccccccccccc"
		body   = "cached-private-artifact"
	)
	database, err := db.Open(
		"sqlite",
		filepath.Join(t.TempDir(), "stream-revocation.db"),
	)
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
	storage := &blockingCacheReadStorage{
		Storage: local,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	manager := cache.NewManager(
		storage,
		database,
		cache.NewEventBus(),
		time.Hour,
	)
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	key := "huggingface/acme/model/resolve/" + commit + "/model.bin"
	if err := storage.Put(
		context.Background(),
		key,
		strings.NewReader(body),
		int64(len(body)),
		"application/octet-stream",
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.CacheEntry{
		Key:         key,
		AdapterType: "huggingface",
		CacheKind:   db.CacheKindArtifact,
		PackageName: "acme/model",
		StoragePath: key,
		Size:        int64(len(body)),
		ContentType: "application/octet-stream",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}

	handler := New(
		manager,
		nil,
		config.CacheConfig{TTLBlob: time.Hour},
		database,
	)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	storage.block.Store(true)
	responseDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(
				http.MethodGet,
				"/huggingface/acme/model/resolve/"+commit+"/model.bin",
				nil,
			),
		)
		responseDone <- recorder
	}()
	select {
	case <-storage.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for cached response stream")
	}

	revocationDone := make(chan struct{})
	go func() {
		handler.revokeRepository(
			context.Background(),
			ParseRequestPath("/acme/model/resolve/"+commit+"/model.bin"),
		)
		close(revocationDone)
	}()
	select {
	case <-revocationDone:
		t.Fatal("repository cleanup completed while cached bytes were still streaming")
	case <-time.After(30 * time.Millisecond):
	}

	close(storage.release)
	select {
	case recorder := <-responseDone:
		if recorder.Code != http.StatusOK || recorder.Body.String() != body {
			t.Fatalf(
				"cached response = (%d, %q)",
				recorder.Code,
				recorder.Body.String(),
			)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for cached response completion")
	}
	select {
	case <-revocationDone:
	case <-time.After(3 * time.Second):
		t.Fatal("repository cleanup did not resume after cached response completed")
	}
}

func TestRepositoryRevocationBeginOrderKeepsGateAndMarkerConsistent(
	t *testing.T,
) {
	const target = "/acme/model/resolve/main/model.bin"
	store := &orderedRevocationStore{
		firstStarted: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	handler := &Handler{
		revocations:     newRepositoryRevocationGate(),
		revocationStore: store,
	}
	parsed := ParseRequestPath(target)
	repository, ok := repositoryForParsed(parsed)
	if !ok {
		t.Fatal("failed to derive repository")
	}

	done := make(chan struct{}, 2)
	go func() {
		handler.revokeRepository(context.Background(), parsed)
		done <- struct{}{}
	}()
	select {
	case <-store.firstStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for first durable Begin")
	}
	go func() {
		handler.revokeRepository(context.Background(), parsed)
		done <- struct{}{}
	}()
	time.Sleep(30 * time.Millisecond)
	handler.revocations.mu.Lock()
	generationWhileBlocked := handler.revocations.generation
	handler.revocations.mu.Unlock()
	store.mu.Lock()
	callsWhileBlocked := store.calls
	store.mu.Unlock()
	if generationWhileBlocked != 1 || callsWhileBlocked != 1 {
		t.Fatalf(
			"second Begin passed the repository order gate: generation=%d calls=%d",
			generationWhileBlocked,
			callsWhileBlocked,
		)
	}

	close(store.releaseFirst)
	for range 2 {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for ordered revocations")
		}
	}
	ticket := handler.revocations.ticket(repository.packageName)
	generation, token, safe := handler.revocations.restorationCandidate(
		repository.packageName,
		ticket.generation,
	)
	if !safe || generation != ticket.generation {
		t.Fatalf(
			"current gate is not cleanup-safe: generation=%d ticket=%+v",
			generation,
			ticket,
		)
	}
	store.mu.Lock()
	marker := store.marker
	store.mu.Unlock()
	if marker.Token != token || !marker.CleanupSafe {
		t.Fatalf(
			"gate and marker diverged: gate token=%q marker=%+v",
			token,
			marker,
		)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/huggingface"+target,
		nil,
	)
	handler.observeDirectRepositoryResponse(
		request,
		target,
		&resolvedResponse{StatusCode: http.StatusOK, Header: make(http.Header)},
		ticket,
	)
	if handler.repositoryRevoked(repository) {
		t.Fatal("consistent gate and durable marker did not restore")
	}
}

func TestPinnedHEADDirectForbiddenDoesNotWaitOnOwnAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const commit = "dddddddddddddddddddddddddddddddddddddddd"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "repository became private", http.StatusForbidden)
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	if err := database.Create(&db.HuggingFaceRefPin{
		Key:       "huggingface/acme/model/ref/main",
		Commit:    commit,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router.Group("/huggingface"))

	responseDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(
				http.MethodHead,
				"/huggingface/acme/model/resolve/main/model.bin",
				nil,
			),
		)
		responseDone <- recorder
	}()
	select {
	case response := <-responseDone:
		if response.Code != http.StatusForbidden {
			t.Fatalf(
				"pinned HEAD private response = (%d, %q)",
				response.Code,
				response.Body.String(),
			)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pinned HEAD direct fallback waited on its own cache admission")
	}

	repository, ok := repositoryForParsed(ParseRequestPath(
		"/acme/model/resolve/main/model.bin",
	))
	if !ok {
		t.Fatal("failed to derive repository")
	}
	if !handler.repositoryRevoked(repository) {
		t.Fatal("pinned HEAD 403 did not close repository gate")
	}
	waitForRepositoryMarkerSafe(t, database, repository.packageName)
}

func waitForRepositoryAdmissions(
	t *testing.T,
	gate *repositoryRevocationGate,
	repository string,
	want int,
) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		gate.mu.Lock()
		state := gate.states[repository]
		active := 0
		if state != nil {
			active = state.active
		}
		gate.mu.Unlock()
		if active >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("repository admissions did not reach %d", want)
}

func waitForRepositoryRevoked(
	t *testing.T,
	handler *Handler,
	repository huggingFaceRepository,
) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if handler.repositoryRevoked(repository) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("repository gate did not close")
}

func waitForRepositoryMarker(
	t *testing.T,
	database *gorm.DB,
	repository string,
) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		result := database.Where(
			"repository = ?",
			repository,
		).Model(&db.HuggingFaceRepositoryRevocation{}).Count(&count)
		if result.Error == nil && count == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("durable repository marker did not appear")
}

func waitForRepositoryMarkerSafe(
	t *testing.T,
	database *gorm.DB,
	repository string,
) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var marker db.HuggingFaceRepositoryRevocation
		result := database.Where("repository = ?", repository).Take(&marker)
		if result.Error == nil && marker.CleanupSafe {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("repository marker did not become cleanup-safe")
}
