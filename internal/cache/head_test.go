package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestManagerHeadReturnsDurableRepresentationMetadata(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	const key = "huggingface/acme/model/resolve/0123456789abcdef0123456789abcdef01234567/model.bin"
	storage.data[key] = []byte("weights")
	if err := database.Create(&db.CacheEntry{
		Key:             key,
		AdapterType:     "huggingface",
		CacheKind:       db.CacheKindArtifact,
		StoragePath:     key,
		Size:            7,
		ContentType:     "application/octet-stream",
		ResponseHeaders: `{"X-Linked-Etag":["sha256:model"],"X-Repo-Commit":["0123456789abcdef0123456789abcdef01234567"]}`,
		ExpiresAt:       time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	result, err := manager.Head(context.Background(), key, "huggingface")
	if err != nil {
		t.Fatal(err)
	}
	if result.Size != 7 || result.ContentType != "application/octet-stream" {
		t.Fatalf("head result = %+v", result)
	}
	if got := result.Headers.Get("X-Linked-Etag"); got != "sha256:model" {
		t.Fatalf("X-Linked-Etag = %q", got)
	}
	// Callers own a clone and cannot mutate the persisted row.
	result.Headers.Set("X-Linked-Etag", "changed")
	again, err := manager.Head(context.Background(), key, "huggingface")
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Headers.Get("X-Linked-Etag"); got != "sha256:model" {
		t.Fatalf("persisted metadata changed to %q", got)
	}
}

func TestManagerHeadRequiresBothMetadataAndObject(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	if _, err := manager.Head(context.Background(), "missing", "huggingface"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("missing Head error = %v", err)
	}
	const key = "huggingface/acme/model/resolve/main/model.bin"
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "huggingface", StoragePath: key,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Head(context.Background(), key, "huggingface"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("row-only Head error = %v", err)
	}
}

func TestManagerHeadWaitsForMatchingMutationAndObservesDeletion(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	const key = "huggingface/acme/model/resolve/main/model.bin"
	storage.data[key] = []byte("weights")
	entry := db.CacheEntry{
		Key:         key,
		AdapterType: "huggingface",
		StoragePath: key,
		Size:        7,
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}

	unlock, err := manager.lockMutation(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()

	started := make(chan struct{})
	headErr := make(chan error, 1)
	go func() {
		close(started)
		_, lookupErr := manager.Head(context.Background(), key, "huggingface")
		headErr <- lookupErr
	}()
	<-started

	select {
	case lookupErr := <-headErr:
		t.Fatalf("Head returned %v while matching mutation was still in progress", lookupErr)
	case <-time.After(25 * time.Millisecond):
	}

	if err := storage.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if err := database.Where("id = ? AND key = ?", entry.ID, key).Delete(&db.CacheEntry{}).Error; err != nil {
		t.Fatal(err)
	}
	unlock()
	locked = false

	select {
	case lookupErr := <-headErr:
		if !errors.Is(lookupErr, ErrCacheMiss) {
			t.Fatalf("Head error after deletion = %v, want ErrCacheMiss", lookupErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Head did not resume after matching mutation completed")
	}
}
