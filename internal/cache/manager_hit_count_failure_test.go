package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

func TestManagerHitCountWorkerBoundsPendingAndBacksOffAfterWriteFailure(t *testing.T) {
	const (
		pendingLimit = 1024
		overflow     = 64
	)

	database := openStreamTestDB(t)
	entries := make([]db.CacheEntry, 0, pendingLimit+overflow)
	for index := range pendingLimit + overflow {
		key := fmt.Sprintf("pypi/files/hit-failure-%04d.whl", index)
		entries = append(entries, db.CacheEntry{
			Key:          key,
			AdapterType:  "pypi",
			StoragePath:  key,
			ExpiresAt:    time.Now().UTC().Add(time.Hour),
			LastAccessed: time.Now().UTC(),
		})
	}
	if err := database.CreateInBatches(&entries, 100).Error; err != nil {
		t.Fatal(err)
	}

	writeUnavailable := errors.New("hit-count writes unavailable")
	var failWrites atomic.Bool
	failWrites.Store(true)
	attempted := make(chan struct{}, pendingLimit+overflow)
	var attemptMu sync.Mutex
	var attemptTimes []time.Time
	callbackName := "test:fail_hit_count_updates"
	if err := database.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if !failWrites.Load() {
			return
		}
		attemptMu.Lock()
		attemptTimes = append(attemptTimes, time.Now())
		attemptMu.Unlock()
		select {
		case attempted <- struct{}{}:
		default:
		}
		tx.AddError(writeUnavailable)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Callback().Update().Remove(callbackName); err != nil {
			t.Errorf("remove hit-count failure callback: %v", err)
		}
	})

	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	var closeOnce sync.Once
	closeManager := func() {
		closeOnce.Do(func() {
			failWrites.Store(false)
			closeTestManager(t, manager)
		})
	}
	t.Cleanup(closeManager)

	for _, entry := range entries {
		if !manager.recordHit(entry.ID, time.Now().UTC()) {
			t.Fatal("bounded input queue unexpectedly rejected test hit")
		}
	}
	// Existing IDs must keep merging even after the distinct-ID cap is full.
	for range 3 {
		if !manager.recordHit(entries[0].ID, time.Now().UTC()) {
			t.Fatal("bounded input queue unexpectedly rejected duplicate test hit")
		}
	}

	for attempt := 0; attempt < 3; attempt++ {
		select {
		case <-attempted:
		case <-time.After(time.Second):
			t.Fatalf("hit-count retry attempt %d did not run", attempt+1)
		}
	}
	attemptMu.Lock()
	firstRetryDelay := attemptTimes[1].Sub(attemptTimes[0])
	secondRetryDelay := attemptTimes[2].Sub(attemptTimes[1])
	attemptMu.Unlock()
	if firstRetryDelay < 75*time.Millisecond || secondRetryDelay < 150*time.Millisecond {
		t.Errorf(
			"failed hit-count writes retried too tightly: delays=%s,%s",
			firstRetryDelay,
			secondRetryDelay,
		)
	}

	failWrites.Store(false)
	closeManager()

	var updated int64
	if err := database.Model(&db.CacheEntry{}).Where("hit_count > 0").Count(&updated).Error; err != nil {
		t.Fatal(err)
	}
	if updated != pendingLimit {
		t.Fatalf("persisted distinct hit-count rows = %d, want bounded %d", updated, pendingLimit)
	}
	var first db.CacheEntry
	if err := database.First(&first, entries[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if first.HitCount != 4 {
		t.Fatalf("existing pending hit count = %d, want 4", first.HitCount)
	}
}

func TestFlushHitCountsLeavesPendingIntactOnFailure(t *testing.T) {
	database := openStreamTestDB(t)
	manager := &Manager{db: database}
	pending := map[uint]mergedHitCount{
		123: {count: 2, lastAccessed: time.Now().UTC()},
	}

	callbackName := "test:fail_direct_hit_count_update"
	if err := database.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		tx.AddError(context.DeadlineExceeded)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Update().Remove(callbackName) })

	if err := manager.flushHitCounts(context.Background(), pending); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("flush error = %v, want context deadline exceeded", err)
	}
	if got := pending[123].count; got != 2 {
		t.Fatalf("failed flush changed pending count = %d, want 2", got)
	}
}
