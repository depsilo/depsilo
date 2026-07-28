package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

type mutationTestStorage struct {
	mu        sync.Mutex
	data      map[string][]byte
	puts      int
	deletes   int
	deleteErr error
}

type blockingMutationPutStorage struct {
	*mutationTestStorage
	putStarted chan struct{}
	releasePut chan struct{}
	startOnce  sync.Once
}

type failOnceExistsStorage struct {
	*mutationTestStorage
	existsMu sync.Mutex
	failNext bool
}

type blockingGetStorage struct {
	*mutationTestStorage
	blockKey string
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (s *blockingGetStorage) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if key == s.blockKey {
		s.once.Do(func() { close(s.started) })
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	}
	return s.mutationTestStorage.Get(ctx, key)
}

func (s *failOnceExistsStorage) Exists(ctx context.Context, key string) (bool, error) {
	s.existsMu.Lock()
	if s.failNext {
		s.failNext = false
		s.existsMu.Unlock()
		return false, errors.New("injected temporary exists failure")
	}
	s.existsMu.Unlock()
	return s.mutationTestStorage.Exists(ctx, key)
}

func (s *blockingMutationPutStorage) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	s.startOnce.Do(func() { close(s.putStarted) })
	select {
	case <-s.releasePut:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.mutationTestStorage.Put(ctx, key, reader, size, contentType)
}

func newMutationTestStorage() *mutationTestStorage {
	return &mutationTestStorage{data: make(map[string][]byte)}
}

func (s *mutationTestStorage) seed(key string, value []byte) {
	s.mu.Lock()
	s.data[key] = append([]byte(nil), value...)
	s.mu.Unlock()
}

func (s *mutationTestStorage) counts() (puts, deletes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts, s.deletes
}

func (s *mutationTestStorage) Exists(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[key]
	return ok, nil
}

func (s *mutationTestStorage) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.data[key]
	if !ok {
		return nil, 0, errors.New("object not found")
	}
	cloned := append([]byte(nil), value...)
	return io.NopCloser(bytes.NewReader(cloned)), int64(len(cloned)), nil
}

func (s *mutationTestStorage) Put(ctx context.Context, key string, reader io.Reader, _ int64, _ string) error {
	value, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.puts++
	s.data[key] = append([]byte(nil), value...)
	s.mu.Unlock()
	return nil
}

func (s *mutationTestStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes++
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.data, key)
	return nil
}

func (s *mutationTestStorage) Stat(ctx context.Context, key string) (*ObjectMeta, error) {
	reader, size, err := s.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	_ = reader.Close()
	return &ObjectMeta{Key: key, Size: size}, nil
}

func (s *mutationTestStorage) List(context.Context, string) ([]ObjectMeta, error) {
	return nil, nil
}

func (s *mutationTestStorage) TotalSize(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int64
	for _, value := range s.data {
		total += int64(len(value))
	}
	return total, nil
}

func invalidationStateCount(manager *Manager) int {
	manager.invalidations.mu.Lock()
	defer manager.invalidations.mu.Unlock()
	return len(manager.invalidations.states)
}

func forceTrackedRefresh(
	ctx context.Context,
	manager *Manager,
	key, adapterType string,
	ttl time.Duration,
	fetchFn FetchFunc,
) error {
	trackedCtx, tracker := WithTrackedForceRefresh(ctx)
	result, err := manager.Get(trackedCtx, key, adapterType, ttl, fetchFn)
	if err != nil {
		return err
	}
	if result == nil || result.Reader == nil {
		return errors.New("tracked refresh returned no reader")
	}
	_, drainErr := io.Copy(io.Discard, result.Reader)
	closeErr := result.Reader.Close()
	var commitErr error
	if tracker.Used() {
		commitErr = tracker.Wait(ctx)
	}
	return errors.Join(drainErr, closeErr, commitErr)
}

func TestManagerMissCompensatesForMetadataCommitFailure(t *testing.T) {
	tests := []struct {
		name          string
		cleanupErr    error
		wantObject    bool
		wantDeleteErr bool
	}{
		{name: "cleanup succeeds"},
		{name: "cleanup failure is reported", cleanupErr: errors.New("injected object delete failure"), wantObject: true, wantDeleteErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openStreamTestDB(t)
			storage := newMutationTestStorage()
			storage.deleteErr = test.cleanupErr
			manager := NewManager(storage, database, NewEventBus(), time.Hour)
			t.Cleanup(func() { closeTestManager(t, manager) })

			injected := errors.New("injected cache metadata create failure")
			if err := database.Callback().Create().Before("gorm:create").Register("test:fail-cache-metadata-create", func(tx *gorm.DB) {
				if tx.Statement.Table == "cache_entries" {
					tx.AddError(injected)
				}
			}); err != nil {
				t.Fatal(err)
			}

			key := "pypi/simple/compensation/index.html"
			err := manager.Prefetch(t.Context(), key, "pypi", time.Hour,
				func(context.Context) (io.ReadCloser, string, int64, string, error) {
					return io.NopCloser(strings.NewReader("new-index")), "text/html", -1, "mock", nil
				})
			if err == nil {
				t.Fatal("Prefetch succeeded despite metadata commit failure")
			}
			if test.wantDeleteErr && !errors.Is(err, test.cleanupErr) {
				t.Fatalf("Prefetch error = %v, want cleanup error", err)
			}

			exists, existsErr := storage.Exists(t.Context(), key)
			if existsErr != nil {
				t.Fatal(existsErr)
			}
			if exists != test.wantObject {
				t.Fatalf("object exists = %t, want %t", exists, test.wantObject)
			}
			puts, deletes := storage.counts()
			if puts != 1 || deletes != 1 {
				t.Fatalf("storage mutations = put:%d delete:%d, want 1 each", puts, deletes)
			}
			var rows int64
			if err := database.Model(&db.CacheEntry{}).Where("key = ?", key).Count(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if rows != 0 {
				t.Fatalf("metadata rows = %d, want 0", rows)
			}
		})
	}
}

func TestManagerHitWaitsForAtomicObjectAndMetadataPublication(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMutationTestStorage()
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	const key = "pypi/simple/atomic-publication/index.html"
	storage.seed(key, []byte("old-body"))
	if err := database.Create(&db.CacheEntry{
		Key: key, StoragePath: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		ContentType: "text/old", ETag: `"old"`,
		ResponseHeaders: encodeResponseMetadata(http.Header{"ETag": {`"old"`}}),
		ExpiresAt:       time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}

	metadataCommitStarted := make(chan struct{})
	releaseMetadataCommit := make(chan struct{})
	var callbackOnce sync.Once
	const callbackName = "test:block-cache-metadata-publication"
	if err := database.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "cache_entries" {
			return
		}
		callbackOnce.Do(func() { close(metadataCommitStarted) })
		<-releaseMetadataCommit
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.Callback().Update().Remove(callbackName)
	})

	refreshDone := make(chan struct{})
	go func() {
		defer close(refreshDone)
		manager.backgroundRefresh(
			t.Context(),
			key,
			"pypi",
			time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				body := WithResponseMetadata(
					io.NopCloser(strings.NewReader("new-body")),
					http.Header{"ETag": {`"new"`}},
				)
				return body, "text/new", -1, "mock", nil
			},
		)
	}()
	select {
	case <-metadataCommitStarted:
	case <-time.After(time.Second):
		t.Fatal("refresh did not reach metadata publication after Storage.Put")
	}

	type hitOutcome struct {
		result *GetResult
		err    error
	}
	hitDone := make(chan hitOutcome, 1)
	go func() {
		result, err := manager.Get(t.Context(), key, "pypi", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				return nil, "", 0, "", errors.New("fresh cache unexpectedly missed")
			})
		hitDone <- hitOutcome{result: result, err: err}
	}()
	select {
	case outcome := <-hitDone:
		if outcome.result != nil {
			_ = outcome.result.Reader.Close()
		}
		t.Fatalf("cache hit observed half-published representation: %+v, err=%v", outcome.result, outcome.err)
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseMetadataCommit)
	select {
	case <-refreshDone:
	case <-time.After(time.Second):
		t.Fatal("refresh did not finish after metadata publication was released")
	}
	outcome := <-hitDone
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	defer outcome.result.Reader.Close()
	body, err := io.ReadAll(outcome.result.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new-body" ||
		outcome.result.ContentType != "text/new" ||
		outcome.result.Headers.Get("ETag") != `"new"` {
		t.Fatalf(
			"published representation = body:%q type:%q etag:%q",
			body,
			outcome.result.ContentType,
			outcome.result.Headers.Get("ETag"),
		)
	}
}

func TestManagerFailedCommitCleanupStaysFailClosedUntilReplacement(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMutationTestStorage()
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	const key = "pypi/simple/uncommitted-overwrite/index.html"
	storage.seed(key, []byte("old-body"))
	if err := database.Create(&db.CacheEntry{
		Key: key, StoragePath: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		ContentType: "text/old", ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}

	persistFailure := errors.New("injected metadata upsert failure")
	deleteFailure := errors.New("injected metadata cleanup failure")
	storageFailure := errors.New("injected object cleanup failure")
	storage.deleteErr = storageFailure
	const (
		createCallback = "test:fail-uncommitted-cache-upsert"
		deleteCallback = "test:fail-uncommitted-cache-delete"
	)
	if err := database.Callback().Create().Before("gorm:create").Register(createCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "cache_entries" {
			tx.AddError(persistFailure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.Callback().Delete().Before("gorm:delete").Register(deleteCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "cache_entries" {
			tx.AddError(deleteFailure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.Callback().Create().Remove(createCallback)
		_ = database.Callback().Delete().Remove(deleteCallback)
	})

	err := forceTrackedRefresh(
		t.Context(),
		manager,
		key,
		"pypi",
		time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(strings.NewReader("uncommitted-body")), "text/new", -1, "mock", nil
		},
	)
	if !errors.Is(err, persistFailure) ||
		!errors.Is(err, deleteFailure) ||
		!errors.Is(err, storageFailure) {
		t.Fatalf("failed publication error = %v, want upsert and both cleanup failures", err)
	}
	if manager.invalidations.readable(key) {
		t.Fatal("double cleanup failure did not retain a fail-closed tombstone")
	}

	offline := errors.New("upstream offline")
	result, getErr := manager.Get(t.Context(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "mock", &testStalePolicyError{cause: offline, allow: true}
		})
	if result != nil {
		_ = result.Reader.Close()
	}
	if !errors.Is(getErr, offline) || result != nil {
		t.Fatalf("uncommitted bytes became readable: result=%+v err=%v", result, getErr)
	}

	if err := database.Callback().Create().Remove(createCallback); err != nil {
		t.Fatal(err)
	}
	if err := database.Callback().Delete().Remove(deleteCallback); err != nil {
		t.Fatal(err)
	}
	storage.deleteErr = nil
	if err := manager.Prefetch(t.Context(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(strings.NewReader("replacement")), "text/replacement", -1, "mock", nil
		}); err != nil {
		t.Fatalf("replacement write: %v", err)
	}
	if !manager.invalidations.readable(key) {
		t.Fatal("successful replacement did not clear fail-closed tombstone")
	}
	hit, err := manager.Get(t.Context(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "", errors.New("replacement unexpectedly missed")
		})
	if err != nil {
		t.Fatal(err)
	}
	defer hit.Reader.Close()
	replacement, err := io.ReadAll(hit.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(replacement) != "replacement" || hit.ContentType != "text/replacement" {
		t.Fatalf("replacement = body:%q type:%q", replacement, hit.ContentType)
	}
}

func TestRetentionWaitsForInflightCommitAndThenWins(t *testing.T) {
	database := openStreamTestDB(t)
	storage := &blockingMutationPutStorage{
		mutationTestStorage: newMutationTestStorage(),
		putStarted:          make(chan struct{}),
		releasePut:          make(chan struct{}),
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })
	retention, err := NewRetention(manager, RetentionPolicy{
		MaxBytes: 1024, ThresholdPercent: 90, TargetPercent: 80,
	})
	if err != nil {
		t.Fatal(err)
	}

	key := "pypi/simple/concurrent-delete/index.html"
	entry := db.CacheEntry{
		Key: key, StoragePath: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		ExpiresAt: time.Now().Add(-time.Hour), LastAccessed: time.Now().Add(-time.Hour),
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}

	prefetchDone := make(chan error, 1)
	go func() {
		prefetchDone <- manager.Prefetch(t.Context(), key, "pypi", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				return io.NopCloser(strings.NewReader("new-index")), "text/html", -1, "mock", nil
			})
	}()
	select {
	case <-storage.putStarted:
	case <-time.After(time.Second):
		t.Fatal("cache fill did not reach the guarded storage commit")
	}

	removeStarted := make(chan struct{})
	removeDone := make(chan error, 1)
	go func() {
		close(removeStarted)
		_, removeErr := retention.Remove(t.Context(), entry.ID)
		removeDone <- removeErr
	}()
	<-removeStarted
	select {
	case removeErr := <-removeDone:
		t.Fatalf("retention bypassed the in-flight commit gate: %v", removeErr)
	case <-time.After(25 * time.Millisecond):
	}

	close(storage.releasePut)
	select {
	case prefetchErr := <-prefetchDone:
		if prefetchErr != nil {
			t.Fatalf("Prefetch: %v", prefetchErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Prefetch did not finish after releasing storage Put")
	}
	select {
	case removeErr := <-removeDone:
		if removeErr != nil {
			t.Fatalf("Remove: %v", removeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Remove did not finish after the in-flight commit")
	}

	exists, err := storage.Exists(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("object remained after retention ran behind the completed fill")
	}
	var rows int64
	if err := database.Model(&db.CacheEntry{}).Where("key = ?", key).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("metadata rows = %d, want 0", rows)
	}
}

func TestManagerAuthoritativeInvalidationDoesNotDependOnExistsPreflight(t *testing.T) {
	database := openStreamTestDB(t)
	storage := &failOnceExistsStorage{
		mutationTestStorage: newMutationTestStorage(),
		failNext:            true,
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	const key = "huggingface/api/models/acme/revoked"
	storage.seed(key, []byte(`{"public":true}`))
	if err := database.Create(&db.CacheEntry{
		Key: key, StoragePath: key, AdapterType: "huggingface", CacheKind: db.CacheKindMetadata,
		ContentType: "application/json", ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}

	revoked := errors.New("repository became private")
	result, err := manager.Get(
		WithForceRefresh(t.Context()),
		key,
		"huggingface",
		time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "hub", &testStalePolicyError{cause: revoked, allow: false}
		},
	)
	if !errors.Is(err, revoked) || result != nil {
		t.Fatalf("authoritative refresh = %+v, err=%v", result, err)
	}

	offline := errors.New("upstream offline")
	result, err = manager.Get(
		t.Context(),
		key,
		"huggingface",
		time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "hub", &testStalePolicyError{cause: offline, allow: true}
		},
	)
	if !errors.Is(err, offline) || result != nil {
		t.Fatalf("revoked content revived after Exists recovered: result=%+v err=%v", result, err)
	}
	if states := invalidationStateCount(manager); states != 0 {
		t.Fatalf("successful invalidation retained %d registry states", states)
	}
}

func TestManagerAuthoritativeMissesDoNotRetainRegistryState(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMutationTestStorage(), database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	revoked := errors.New("not found upstream")
	for i := 0; i < 64; i++ {
		key := fmt.Sprintf("huggingface/api/models/missing/%d", i)
		result, err := manager.Get(WithForceRefresh(t.Context()), key, "huggingface", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				return nil, "", 0, "hub", &testStalePolicyError{cause: revoked, allow: false}
			})
		if !errors.Is(err, revoked) || result != nil {
			t.Fatalf("authoritative miss %d = %+v, err=%v", i, result, err)
		}
	}
	if states := invalidationStateCount(manager); states != 0 {
		t.Fatalf("successful authoritative misses retained %d registry states", states)
	}
	manager.invalidations.mu.Lock()
	failClosed := manager.invalidations.failClosed
	manager.invalidations.mu.Unlock()
	if failClosed {
		t.Fatal("successful authoritative misses incorrectly entered global fail-closed mode")
	}
}

func TestManagerAuthoritativeInvalidationSupersedesBlockedOldWriter(t *testing.T) {
	database := openStreamTestDB(t)
	storage := &blockingMutationPutStorage{
		mutationTestStorage: newMutationTestStorage(),
		putStarted:          make(chan struct{}),
		releasePut:          make(chan struct{}),
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	manager.invalidationTimeout = 25 * time.Millisecond
	t.Cleanup(func() { closeTestManager(t, manager) })

	const key = "pypi/files/revoked-1.0.0.whl"
	storage.seed(key, []byte("trusted-old"))
	if err := database.Create(&db.CacheEntry{
		Key: key, StoragePath: key, AdapterType: "pypi", CacheKind: db.CacheKindArtifact,
		ContentType: "application/octet-stream", ExpiresAt: time.Now().Add(-time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}

	backgroundDone := make(chan struct{})
	go func() {
		defer close(backgroundDone)
		manager.backgroundRefresh(t.Context(), key, "pypi", 2*time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				return io.NopCloser(strings.NewReader("obsolete-writer")), "application/octet-stream", -1, "old-upstream", nil
			})
	}()
	select {
	case <-storage.putStarted:
	case <-time.After(time.Second):
		t.Fatal("old background writer did not enter storage.Put")
	}

	revoked := errors.New("package removed upstream")
	invalidationDone := make(chan error, 1)
	go func() {
		result, getErr := manager.Get(
			WithForceRefresh(t.Context()),
			key,
			"pypi",
			2*time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				return nil, "", 0, "upstream", &testStalePolicyError{cause: revoked, allow: false}
			},
		)
		if result != nil {
			_ = result.Reader.Close()
			invalidationDone <- fmt.Errorf("authoritative refresh returned result: %+v", result)
			return
		}
		invalidationDone <- getErr
	}()
	select {
	case err := <-invalidationDone:
		if !errors.Is(err, revoked) {
			t.Fatalf("authoritative refresh error = %v, want %v", err, revoked)
		}
	case <-time.After(time.Second):
		t.Fatal("authoritative refresh did not return after bounded invalidation wait")
	}

	close(storage.releasePut)
	select {
	case <-backgroundDone:
	case <-time.After(time.Second):
		t.Fatal("old background writer did not finish after storage.Put was released")
	}

	newFetches := 0
	if err := manager.Prefetch(t.Context(), key, "pypi", 2*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			newFetches++
			return io.NopCloser(strings.NewReader("replacement")), "application/octet-stream", -1, "new-upstream", nil
		}); err != nil {
		t.Fatalf("new successful fetch: %v", err)
	}
	if newFetches != 1 {
		t.Fatalf("new upstream fetches = %d, want 1 (old writer revived cache)", newFetches)
	}

	hit, err := manager.Get(t.Context(), key, "pypi", 2*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "", errors.New("replacement cache unexpectedly missed")
		})
	if err != nil {
		t.Fatal(err)
	}
	defer hit.Reader.Close()
	body, err := io.ReadAll(hit.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "replacement" {
		t.Fatalf("cached body = %q, want replacement", body)
	}
	if states := invalidationStateCount(manager); states != 0 {
		t.Fatalf("successful replacement retained %d registry states", states)
	}
}

func TestManagerFailedAuthoritativeCleanupKeepsTombstoneUntilSuccessfulWrite(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMutationTestStorage()
	storage.deleteErr = errors.New("injected storage delete failure")
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	const key = "huggingface/api/models/acme/still-private"
	storage.seed(key, []byte(`{"public":true}`))
	if err := database.Create(&db.CacheEntry{
		Key: key, StoragePath: key, AdapterType: "huggingface", CacheKind: db.CacheKindMetadata,
		ContentType: "application/json", ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}

	deleteFailure := errors.New("injected metadata delete failure")
	const callbackName = "test:fail-authoritative-cache-delete"
	if err := database.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "cache_entries" {
			tx.AddError(deleteFailure)
		}
	}); err != nil {
		t.Fatal(err)
	}

	revoked := errors.New("repository became private")
	result, err := manager.Get(WithForceRefresh(t.Context()), key, "huggingface", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "hub", &testStalePolicyError{cause: revoked, allow: false}
		})
	if !errors.Is(err, revoked) || result != nil {
		t.Fatalf("authoritative refresh = %+v, err=%v", result, err)
	}

	offline := errors.New("upstream offline")
	result, err = manager.Get(t.Context(), key, "huggingface", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "hub", &testStalePolicyError{cause: offline, allow: true}
		})
	if !errors.Is(err, offline) || result != nil {
		t.Fatalf("failed cleanup exposed tombstoned content: result=%+v err=%v", result, err)
	}

	if err := database.Callback().Delete().Remove(callbackName); err != nil {
		t.Fatal(err)
	}
	storage.deleteErr = nil
	if err := manager.Prefetch(t.Context(), key, "huggingface", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(strings.NewReader(`{"public":false}`)), "application/json", -1, "hub", nil
		}); err != nil {
		t.Fatal(err)
	}
	if states := invalidationStateCount(manager); states != 0 {
		t.Fatalf("successful new write retained %d registry states", states)
	}
}

func TestManagerAuthoritativeFlightPublishesOnlyAfterTombstone(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMutationTestStorage()
	storage.deleteErr = errors.New("injected storage delete failure")
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	const key = "huggingface/api/models/acme/revoked-before-publication"
	deleteFailure := errors.New("injected metadata delete failure")
	const callbackName = "test:fail-authoritative-delete-before-flight-publication"
	if err := database.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "cache_entries" {
			tx.AddError(deleteFailure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.Callback().Delete().Remove(callbackName)
	})

	revoked := errors.New("repository became private")
	result, err := manager.fetchAndStore(
		t.Context(),
		key,
		"huggingface",
		time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "hub", &testStalePolicyError{cause: revoked, allow: false}
		},
	)
	if !errors.Is(err, revoked) || result != nil {
		t.Fatalf("authoritative flight = %+v, err=%v", result, err)
	}
	if manager.invalidations.readable(key) {
		t.Fatal("authoritative flight became observable before its failed cleanup tombstone")
	}
}

func TestManagerServeStaleRejectsTombstonedKeyAtReadBoundary(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMutationTestStorage()
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	const key = "pypi/simple/tombstoned/index.html"
	storage.seed(key, []byte("old-index"))
	if err := database.Create(&db.CacheEntry{
		Key: key, StoragePath: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		ContentType: "text/html", ExpiresAt: time.Now().Add(-time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}

	generation := manager.invalidations.beginInvalidation(key)
	defer manager.invalidations.finishInvalidation(key, generation, true)
	result, err := manager.serveStale(t.Context(), key, "pypi")
	if err == nil || result != nil {
		if result != nil {
			_ = result.Reader.Close()
		}
		t.Fatalf("serveStale exposed tombstoned cache: result=%+v err=%v", result, err)
	}
	if result, err := manager.Head(t.Context(), key, "pypi"); !errors.Is(err, ErrCacheMiss) || result != nil {
		t.Fatalf("Head exposed tombstoned cache: result=%+v err=%v", result, err)
	}
}

func TestManagerSlowStorageReadDoesNotBlockOtherKeyInvalidation(t *testing.T) {
	database := openStreamTestDB(t)
	const (
		slowKey  = "pypi/files/slow-1.0.0.whl"
		otherKey = "pypi/files/revoked-1.0.0.whl"
	)
	storage := &blockingGetStorage{
		mutationTestStorage: newMutationTestStorage(),
		blockKey:            slowKey,
		started:             make(chan struct{}),
		release:             make(chan struct{}),
	}
	storage.seed(slowKey, []byte("slow"))
	storage.seed(otherKey, []byte("revoked"))
	for _, key := range []string{slowKey, otherKey} {
		if err := database.Create(&db.CacheEntry{
			Key: key, StoragePath: key, AdapterType: "pypi", CacheKind: db.CacheKindArtifact,
			ContentType: "application/octet-stream", ExpiresAt: time.Now().Add(time.Hour),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	readDone := make(chan error, 1)
	go func() {
		result, err := manager.Get(t.Context(), slowKey, "pypi", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				return nil, "", 0, "", errors.New("fresh cache unexpectedly missed")
			})
		if result != nil {
			_ = result.Reader.Close()
		}
		readDone <- err
	}()
	select {
	case <-storage.started:
	case <-time.After(time.Second):
		t.Fatal("slow cache read did not reach Storage.Get")
	}

	invalidationDone := make(chan error, 1)
	go func() {
		invalidationDone <- manager.invalidateCachedEntry(t.Context(), otherKey)
	}()
	select {
	case err := <-invalidationDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("different-key invalidation was blocked by slow Storage.Get")
	}

	close(storage.release)
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("slow cache read did not finish after release")
	}
}

func TestManagerBackgroundRefreshRechecksRowBeforeWriting(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMutationTestStorage()
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })
	key := "pypi/simple/removed/index.html"
	entry := db.CacheEntry{
		Key: key, StoragePath: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		ExpiresAt: time.Now().Add(-time.Hour), LastAccessed: time.Now().Add(-time.Hour),
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	storage.seed(key, []byte("old-index"))

	fetchStarted := make(chan struct{})
	releaseFetch := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.backgroundRefresh(context.Background(), key, "pypi", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				close(fetchStarted)
				<-releaseFetch
				return io.NopCloser(strings.NewReader("new-index")), "text/html", -1, "mock", nil
			})
	}()
	select {
	case <-fetchStarted:
	case <-time.After(time.Second):
		t.Fatal("background fetch did not start")
	}
	unlock, err := manager.lockMutation(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Delete(t.Context(), key); err != nil {
		unlock()
		t.Fatal(err)
	}
	if err := database.Delete(&entry).Error; err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()
	close(releaseFetch)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not finish")
	}

	puts, deletes := storage.counts()
	if puts != 0 || deletes != 1 {
		t.Fatalf("storage mutations = put:%d delete:%d, want delete only", puts, deletes)
	}
	exists, err := storage.Exists(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("background refresh recreated an entry removed through the mutation gate")
	}

	if err := manager.Prefetch(t.Context(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(strings.NewReader("new-request-index")), "text/html", -1, "mock", nil
		}); err != nil {
		t.Fatalf("new request did not refill deleted key: %v", err)
	}
	exists, err = storage.Exists(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("new request did not persist a replacement object")
	}
	var rows int64
	if err := database.Model(&db.CacheEntry{}).Where("key = ?", key).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("metadata rows after new request = %d, want 1", rows)
	}
}

func TestManagerBackgroundRefreshRemovesObjectWhenMetadataUpdateDoesNotCommit(t *testing.T) {
	tests := []struct {
		name     string
		callback func(*gorm.DB)
	}{
		{
			name: "database error",
			callback: func(tx *gorm.DB) {
				tx.AddError(errors.New("injected metadata update failure"))
			},
		},
		{
			name: "zero affected rows",
			callback: func(tx *gorm.DB) {
				tx.RowsAffected = 0
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openStreamTestDB(t)
			storage := newMutationTestStorage()
			manager := NewManager(storage, database, NewEventBus(), time.Hour)
			t.Cleanup(func() { closeTestManager(t, manager) })
			key := fmt.Sprintf("pypi/simple/%s/index.html", strings.ReplaceAll(test.name, " ", "-"))
			entry := db.CacheEntry{
				Key: key, StoragePath: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
				ExpiresAt: time.Now().Add(-time.Hour), LastAccessed: time.Now().Add(-time.Hour),
			}
			if err := database.Create(&entry).Error; err != nil {
				t.Fatal(err)
			}
			storage.seed(key, []byte("old-index"))

			callbackName := "test:fail-background-cache-update"
			if err := database.Callback().Update().After("gorm:update").Register(callbackName, func(tx *gorm.DB) {
				if tx.Statement.Table == "cache_entries" {
					test.callback(tx)
				}
			}); err != nil {
				t.Fatal(err)
			}

			manager.backgroundRefresh(t.Context(), key, "pypi", time.Hour,
				func(context.Context) (io.ReadCloser, string, int64, string, error) {
					return io.NopCloser(strings.NewReader("new-index")), "text/html", -1, "mock", nil
				})

			exists, err := storage.Exists(t.Context(), key)
			if err != nil {
				t.Fatal(err)
			}
			if exists {
				t.Fatal("background refresh left an object without a confirmed metadata update")
			}
			puts, deletes := storage.counts()
			if puts != 1 || deletes != 1 {
				t.Fatalf("storage mutations = put:%d delete:%d, want 1 each", puts, deletes)
			}
		})
	}
}

func TestManagerTamperTTLMutationWaitsForKeyGate(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMutationTestStorage()
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })
	recorder := &fakeRecorder{}
	manager.SetTamperRecorder(recorder)
	key := "pypi/files/gated-1.0.0.whl"
	entry := db.CacheEntry{
		Key: key, StoragePath: key, AdapterType: "pypi", CacheKind: db.CacheKindArtifact,
		ExpiresAt: time.Now().Add(-time.Hour), LastAccessed: time.Now().Add(-time.Hour),
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	storage.seed(key, []byte("artifact"))

	unlock, err := manager.lockMutation(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.backgroundRefresh(context.Background(), key, "pypi", 72*time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				return io.NopCloser(strings.NewReader("artifact")), "application/octet-stream", -1, "mock", nil
			})
	}()

	select {
	case <-done:
		unlock()
		t.Fatal("tamper refresh completed while its key mutation gate was held")
	case <-time.After(25 * time.Millisecond):
	}
	if got := recorder.verifiedKeys(); len(got) != 0 {
		unlock()
		t.Fatalf("Verify ran before key gate acquisition: %v", got)
	}
	unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("tamper refresh did not finish after key gate release")
	}
	if got := recorder.verifiedKeys(); len(got) != 1 || got[0] != key {
		t.Fatalf("verified keys = %v, want %q", got, key)
	}
}
