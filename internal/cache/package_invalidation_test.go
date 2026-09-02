package cache

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"depsilo/internal/db"
)

type blockingPackageDeleteStorage struct {
	*mutationTestStorage
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingPackageDeleteStorage) Delete(ctx context.Context, key string) error {
	s.once.Do(func() { close(s.started) })
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.mutationTestStorage.Delete(ctx, key)
}

func newPackageInvalidationManager(
	t *testing.T,
	storage Storage,
) (*Manager, *mutationTestStorage, func()) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "package.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	base, ok := storage.(*mutationTestStorage)
	if !ok {
		if blocking, blockingOK := storage.(*blockingPackageDeleteStorage); blockingOK {
			base = blocking.mutationTestStorage
		}
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	closeManager := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	}
	return manager, base, closeManager
}

func seedPackageInvalidationEntries(
	t *testing.T,
	manager *Manager,
	storage *mutationTestStorage,
	keys ...string,
) {
	t.Helper()
	for _, key := range keys {
		storage.seed(key, []byte("old"))
		if err := manager.db.Create(&db.CacheEntry{
			Key:         key,
			AdapterType: "huggingface",
			CacheKind:   db.CacheKindArtifact,
			PackageName: "acme/model",
			StoragePath: key,
			Size:        3,
			ExpiresAt:   time.Now().Add(time.Hour),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestInvalidatePackageTombstonesAllEntriesBeforeDeletingFirst(t *testing.T) {
	base := newMutationTestStorage()
	storage := &blockingPackageDeleteStorage{
		mutationTestStorage: base,
		started:             make(chan struct{}),
		release:             make(chan struct{}),
	}
	manager, _, closeManager := newPackageInvalidationManager(t, storage)
	t.Cleanup(closeManager)

	const (
		keyA = "huggingface/acme/model/resolve/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/a.bin"
		keyB = "huggingface/acme/model/resolve/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/b.bin"
	)
	seedPackageInvalidationEntries(t, manager, base, keyA, keyB)

	type outcome struct {
		result PackageInvalidationResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := manager.InvalidatePackage(
			context.Background(),
			"huggingface",
			"acme/model",
		)
		done <- outcome{result: result, err: err}
	}()
	select {
	case <-storage.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first package delete")
	}

	if _, err := manager.Head(context.Background(), keyB, "huggingface"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("sibling Head during first delete error = %v, want cache miss", err)
	}
	close(storage.release)

	select {
	case got := <-done:
		if got.err != nil || !got.result.SafeToRestore || got.result.Entries != 2 {
			t.Fatalf("package invalidation = (%+v, %v), want 2 safe entries", got.result, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for package invalidation")
	}
}

func TestInvalidatePackageRetainsTombstonesWhenBothDeletesFail(t *testing.T) {
	storage := newMutationTestStorage()
	manager, _, closeManager := newPackageInvalidationManager(t, storage)
	t.Cleanup(closeManager)

	const (
		keyA = "huggingface/acme/model/resolve/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/a.bin"
		keyB = "huggingface/acme/model/resolve/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/b.bin"
	)
	seedPackageInvalidationEntries(t, manager, storage, keyA, keyB)
	storage.deleteErr = errors.New("object store unavailable")
	if err := manager.db.Exec(`
		CREATE TRIGGER fail_package_cache_delete
		BEFORE DELETE ON cache_entries
		BEGIN
			SELECT RAISE(FAIL, 'cache database unavailable');
		END;
	`).Error; err != nil {
		t.Fatal(err)
	}

	result, err := manager.InvalidatePackage(
		context.Background(),
		"huggingface",
		"acme/model",
	)
	if err == nil || result.SafeToRestore || result.Entries != 2 {
		t.Fatalf("failed package invalidation = (%+v, %v), want 2 unsafe entries", result, err)
	}
	for _, key := range []string{keyA, keyB} {
		if _, headErr := manager.Head(context.Background(), key, "huggingface"); !errors.Is(headErr, ErrCacheMiss) {
			t.Fatalf("Head(%q) after double-delete failure = %v, want cache miss", key, headErr)
		}
	}

	if err := manager.db.Exec("DROP TRIGGER fail_package_cache_delete").Error; err != nil {
		t.Fatal(err)
	}
	storage.deleteErr = nil
	retry, retryErr := manager.InvalidatePackage(
		context.Background(),
		"huggingface",
		"acme/model",
	)
	if retryErr != nil || !retry.SafeToRestore || retry.Entries != 2 {
		t.Fatalf("retry package invalidation = (%+v, %v), want 2 safe entries", retry, retryErr)
	}
}

func TestInvalidatePackageMatchesLegacyHuggingFaceCaseAliases(t *testing.T) {
	storage := newMutationTestStorage()
	manager, _, closeManager := newPackageInvalidationManager(t, storage)
	t.Cleanup(closeManager)

	key := "huggingface/OpenAI/Whisper-Tiny/resolve/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/config.json"
	storage.seed(key, []byte("old"))
	if err := manager.db.Create(&db.CacheEntry{
		Key: key, AdapterType: "huggingface", CacheKind: db.CacheKindArtifact,
		PackageName: "OpenAI/Whisper-Tiny", StoragePath: key, Size: 3,
		ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}

	result, err := manager.InvalidatePackage(
		context.Background(), "huggingface", "openai/whisper-tiny",
	)
	if err != nil || !result.SafeToRestore || result.Entries != 1 {
		t.Fatalf("case-alias invalidation = (%+v, %v), want one safe entry", result, err)
	}
	if _, err := manager.Head(context.Background(), key, "huggingface"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("legacy alias remained readable: %v", err)
	}
}
