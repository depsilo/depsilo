package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
