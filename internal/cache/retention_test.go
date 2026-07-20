package cache

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

type retentionTestStorage struct {
	Storage

	mu             sync.Mutex
	deleteFailures map[string]error
	existsFailures map[string]error
	totalFailures  []error
}

func (storage *retentionTestStorage) Exists(ctx context.Context, key string) (bool, error) {
	storage.mu.Lock()
	err := storage.existsFailures[key]
	storage.mu.Unlock()
	if err != nil {
		return false, err
	}
	return storage.Storage.Exists(ctx, key)
}

func (storage *retentionTestStorage) Delete(ctx context.Context, key string) error {
	storage.mu.Lock()
	err := storage.deleteFailures[key]
	storage.mu.Unlock()
	if err != nil {
		return err
	}
	return storage.Storage.Delete(ctx, key)
}

func (storage *retentionTestStorage) failDelete(key string, err error) {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	if err == nil {
		delete(storage.deleteFailures, key)
		return
	}
	storage.deleteFailures[key] = err
}

func (storage *retentionTestStorage) TotalSize(ctx context.Context) (int64, error) {
	storage.mu.Lock()
	if len(storage.totalFailures) > 0 {
		err := storage.totalFailures[0]
		storage.totalFailures = storage.totalFailures[1:]
		storage.mu.Unlock()
		return 0, err
	}
	storage.mu.Unlock()
	return storage.Storage.TotalSize(ctx)
}

func (storage *retentionTestStorage) failNextTotalSize(err error) {
	storage.mu.Lock()
	storage.totalFailures = append(storage.totalFailures, err)
	storage.mu.Unlock()
}

type retentionFixture struct {
	retention *Retention
	manager   *Manager
	storage   *retentionTestStorage
	database  *gorm.DB
}

func newRetentionFixture(t *testing.T, policy RetentionPolicy) retentionFixture {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "retention.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	local, err := NewLocalStorage(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	storage := &retentionTestStorage{
		Storage:        local,
		deleteFailures: make(map[string]error),
		existsFailures: make(map[string]error),
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	retention, err := NewRetention(manager, policy)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := manager.Close(closeCtx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close retention database: %v", err)
		}
	})
	return retentionFixture{retention: retention, manager: manager, storage: storage, database: database}
}

func seedRetentionEntry(
	t *testing.T,
	fixture retentionFixture,
	key string,
	size int,
	expiresAt time.Time,
	lastAccessed time.Time,
) db.CacheEntry {
	t.Helper()
	payload := bytes.Repeat([]byte{'x'}, size)
	if err := fixture.storage.Put(context.Background(), key, bytes.NewReader(payload), int64(size), "application/octet-stream"); err != nil {
		t.Fatalf("seed storage %q: %v", key, err)
	}
	entry := db.CacheEntry{
		Key:          key,
		AdapterType:  "pypi",
		CacheKind:    db.CacheKindArtifact,
		StoragePath:  key,
		Size:         int64(size),
		ContentType:  "application/octet-stream",
		ExpiresAt:    expiresAt,
		LastAccessed: lastAccessed,
	}
	if err := fixture.database.Create(&entry).Error; err != nil {
		t.Fatalf("seed database %q: %v", key, err)
	}
	return entry
}

func assertRetentionEntryExists(t *testing.T, fixture retentionFixture, id uint, want bool) {
	t.Helper()
	var count int64
	if err := fixture.database.Model(&db.CacheEntry{}).Where("id = ?", id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if got := count == 1; got != want {
		t.Fatalf("cache entry %d exists = %v, want %v", id, got, want)
	}
}

func TestNewRetentionValidatesPolicy(t *testing.T) {
	validPolicy := RetentionPolicy{MaxBytes: 100, ThresholdPercent: 90, TargetPercent: 80}
	if _, err := NewRetention(nil, validPolicy); err == nil {
		t.Fatal("NewRetention accepted a nil manager")
	}

	fixture := newRetentionFixture(t, validPolicy)
	for name, policy := range map[string]RetentionPolicy{
		"zero capacity":       {MaxBytes: 0, ThresholdPercent: 90, TargetPercent: 80},
		"zero threshold":      {MaxBytes: 100, ThresholdPercent: 0, TargetPercent: 80},
		"threshold over 100":  {MaxBytes: 100, ThresholdPercent: 101, TargetPercent: 80},
		"target at threshold": {MaxBytes: 100, ThresholdPercent: 90, TargetPercent: 90},
		"target over threshold": {
			MaxBytes: 100, ThresholdPercent: 80, TargetPercent: 90,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewRetention(fixture.manager, policy); err == nil {
				t.Fatalf("NewRetention accepted policy %+v", policy)
			}
		})
	}
}

func TestDefaultRetentionPolicyKeepsUsefulHysteresis(t *testing.T) {
	for _, test := range []struct {
		threshold  int
		wantTarget int
	}{
		{threshold: 100, wantTarget: 80},
		{threshold: 90, wantTarget: 80},
		{threshold: 80, wantTarget: 70},
		{threshold: 50, wantTarget: 40},
		{threshold: 5, wantTarget: 0},
		{threshold: 1, wantTarget: 0},
	} {
		policy := DefaultRetentionPolicy(1024, test.threshold)
		if policy.MaxBytes != 1024 || policy.ThresholdPercent != test.threshold || policy.TargetPercent != test.wantTarget {
			t.Errorf("DefaultRetentionPolicy(1024, %d) = %+v, want target %d", test.threshold, policy, test.wantTarget)
		}
	}
}

func TestRetentionRemovePreservesRowWhenStorageDeleteFails(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 100, ThresholdPercent: 90, TargetPercent: 80})
	entry := seedRetentionEntry(t, fixture, "pypi/files/remove.whl", 10, time.Now().Add(time.Hour), time.Now())
	deleteErr := errors.New("object store unavailable")
	fixture.storage.failDelete(entry.StoragePath, deleteErr)

	removal, err := fixture.retention.Remove(context.Background(), entry.ID)
	if !errors.Is(err, deleteErr) {
		t.Fatalf("Remove error = %v, want storage error", err)
	}
	if removal.ObjectRemoved || removal.MetadataRemoved || removal.ReclaimedBytes != 0 {
		t.Fatalf("failed removal = %+v", removal)
	}
	assertRetentionEntryExists(t, fixture, entry.ID, true)
	exists, err := fixture.storage.Exists(context.Background(), entry.StoragePath)
	if err != nil || !exists {
		t.Fatalf("object exists after failed Remove = %v, %v; want true, nil", exists, err)
	}

	fixture.storage.failDelete(entry.StoragePath, nil)
	removal, err = fixture.retention.Remove(context.Background(), entry.ID)
	if err != nil {
		t.Fatalf("retry Remove: %v", err)
	}
	if !removal.ObjectRemoved || !removal.MetadataRemoved || removal.ReclaimedBytes != entry.Size {
		t.Fatalf("successful removal = %+v", removal)
	}
	assertRetentionEntryExists(t, fixture, entry.ID, false)
	if _, err := fixture.retention.Remove(context.Background(), entry.ID); !errors.Is(err, ErrCacheEntryNotFound) {
		t.Fatalf("second Remove error = %v, want ErrCacheEntryNotFound", err)
	}
}

func TestRetentionRemovePreservesRowWhenStorageExistenceCheckFails(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 100, ThresholdPercent: 90, TargetPercent: 80})
	entry := seedRetentionEntry(t, fixture, "pypi/files/exists-failure.whl", 10, time.Now().Add(time.Hour), time.Now())
	existsErr := errors.New("object existence unavailable")
	fixture.storage.mu.Lock()
	fixture.storage.existsFailures[entry.StoragePath] = existsErr
	fixture.storage.mu.Unlock()

	removal, err := fixture.retention.Remove(context.Background(), entry.ID)
	if !errors.Is(err, existsErr) {
		t.Fatalf("Remove error = %v, want existence error", err)
	}
	if removal.ObjectRemoved || removal.MetadataRemoved || removal.ReclaimedBytes != 0 {
		t.Fatalf("failed removal = %+v", removal)
	}
	assertRetentionEntryExists(t, fixture, entry.ID, true)
}

func TestRetentionRemoveDatabaseFailureIsRetryable(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 100, ThresholdPercent: 90, TargetPercent: 80})
	entry := seedRetentionEntry(t, fixture, "pypi/files/db-failure.whl", 10, time.Now().Add(time.Hour), time.Now())
	if err := fixture.database.Exec(`
			CREATE TRIGGER fail_cache_entry_delete
			BEFORE DELETE ON cache_entries
			BEGIN
				SELECT RAISE(FAIL, 'delete blocked');
			END`).Error; err != nil {
		t.Fatal(err)
	}

	removal, err := fixture.retention.Remove(context.Background(), entry.ID)
	if err == nil {
		t.Fatal("Remove succeeded despite database delete trigger")
	}
	if !removal.ObjectRemoved || removal.MetadataRemoved || removal.ReclaimedBytes != entry.Size {
		t.Fatalf("partial removal = %+v", removal)
	}
	assertRetentionEntryExists(t, fixture, entry.ID, true)
	exists, err := fixture.storage.Exists(context.Background(), entry.StoragePath)
	if err != nil || exists {
		t.Fatalf("object exists after database failure = %v, %v; want false, nil", exists, err)
	}

	if err := fixture.database.Exec("DROP TRIGGER fail_cache_entry_delete").Error; err != nil {
		t.Fatal(err)
	}
	removal, err = fixture.retention.Remove(context.Background(), entry.ID)
	if err != nil {
		t.Fatalf("retry Remove after dropping trigger: %v", err)
	}
	if removal.ObjectRemoved || !removal.MetadataRemoved || removal.ReclaimedBytes != 0 {
		t.Fatalf("retry removal = %+v", removal)
	}
	assertRetentionEntryExists(t, fixture, entry.ID, false)
}

func TestRetentionManualReclaimRemovesExpiredThenStableLRUToTarget(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 100, ThresholdPercent: 90, TargetPercent: 80})
	now := time.Now().UTC()
	expired := seedRetentionEntry(t, fixture, "expired", 10, now.Add(-time.Hour), now.Add(-4*time.Hour))
	oldest := seedRetentionEntry(t, fixture, "oldest", 30, now.Add(time.Hour), now.Add(-3*time.Hour))
	sameTimeSecond := seedRetentionEntry(t, fixture, "same-time-second", 30, now.Add(time.Hour), now.Add(-3*time.Hour))
	newest := seedRetentionEntry(t, fixture, "newest", 50, now.Add(time.Hour), now.Add(-time.Hour))

	report, err := fixture.retention.Reclaim(context.Background(), ReclaimModeManual)
	if err != nil {
		t.Fatal(err)
	}
	if report.Removed != 2 || report.ExpiredRemoved != 1 || report.LRURemoved != 1 {
		t.Fatalf("report counts = %+v", report)
	}
	if report.ReclaimedBytes != 40 || report.UsageBefore != 120 || report.UsageAfter != 80 {
		t.Fatalf("report usage = %+v", report)
	}
	assertRetentionEntryExists(t, fixture, expired.ID, false)
	assertRetentionEntryExists(t, fixture, oldest.ID, false)
	assertRetentionEntryExists(t, fixture, sameTimeSecond.ID, true)
	assertRetentionEntryExists(t, fixture, newest.ID, true)
}

func TestRetentionCapacityModeDoesNothingBelowInitialThreshold(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 100, ThresholdPercent: 80, TargetPercent: 50})
	entry := seedRetentionEntry(t, fixture, "expired-below-threshold", 70, time.Now().Add(-time.Hour), time.Now())

	report, err := fixture.retention.Reclaim(context.Background(), ReclaimModeCapacity)
	if err != nil {
		t.Fatal(err)
	}
	if report.Examined != 0 || report.Removed != 0 || report.UsageBefore != 70 || report.UsageAfter != 70 {
		t.Fatalf("capacity report = %+v", report)
	}
	assertRetentionEntryExists(t, fixture, entry.ID, true)
}

func TestRetentionCapacityModeRemovesExpiredBeforeLRUToTarget(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 100, ThresholdPercent: 80, TargetPercent: 50})
	now := time.Now().UTC()
	expired := seedRetentionEntry(t, fixture, "capacity-expired", 20, now.Add(-time.Hour), now.Add(-3*time.Hour))
	oldestFresh := seedRetentionEntry(t, fixture, "capacity-oldest", 40, now.Add(time.Hour), now.Add(-2*time.Hour))
	newestFresh := seedRetentionEntry(t, fixture, "capacity-newest", 40, now.Add(time.Hour), now.Add(-time.Hour))

	report, err := fixture.retention.Reclaim(context.Background(), ReclaimModeCapacity)
	if err != nil {
		t.Fatal(err)
	}
	if report.ExpiredRemoved != 1 || report.LRURemoved != 1 || report.Removed != 2 {
		t.Fatalf("report counts = %+v", report)
	}
	if report.UsageBefore != 100 || report.UsageAfter != 40 || report.ReclaimedBytes != 60 {
		t.Fatalf("report usage = %+v", report)
	}
	assertRetentionEntryExists(t, fixture, expired.ID, false)
	assertRetentionEntryExists(t, fixture, oldestFresh.ID, false)
	assertRetentionEntryExists(t, fixture, newestFresh.ID, true)
}

func TestRetentionReportsUntrackedStorageThatPreventsTarget(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 100, ThresholdPercent: 80, TargetPercent: 50})
	if err := fixture.storage.Put(
		context.Background(),
		"legacy-orphan",
		bytes.NewReader(bytes.Repeat([]byte{'x'}, 100)),
		100,
		"application/octet-stream",
	); err != nil {
		t.Fatal(err)
	}

	report, err := fixture.retention.Reclaim(context.Background(), ReclaimModeCapacity)
	if !errors.Is(err, ErrReclaimTargetNotReached) {
		t.Fatalf("Reclaim error = %v, want ErrReclaimTargetNotReached", err)
	}
	if report.Removed != 0 || report.Failed != 0 || report.UsageBefore != 100 || report.UsageAfter != 100 {
		t.Fatalf("report = %+v", report)
	}
}

func TestRetentionRechecksExpiryInsideMutationGate(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 100, ThresholdPercent: 90, TargetPercent: 80})
	now := time.Now().UTC()
	candidate := seedRetentionEntry(t, fixture, "refreshed-candidate", 10, now.Add(-time.Hour), now)
	if err := fixture.database.Model(&db.CacheEntry{}).Where("id = ?", candidate.ID).
		Update("expires_at", now.Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}

	removal, attempted, err := fixture.retention.removeCandidate(context.Background(), candidate, func(current db.CacheEntry) bool {
		return current.ExpiresAt.Before(now)
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempted || removal.MetadataRemoved {
		t.Fatal("candidate refreshed before gate re-read was removed")
	}
	assertRetentionEntryExists(t, fixture, candidate.ID, true)
}

func TestRetentionReclaimContinuesAndJoinsCandidateFailures(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 1000, ThresholdPercent: 90, TargetPercent: 80})
	now := time.Now().UTC()
	failed := seedRetentionEntry(t, fixture, "failed-expired", 10, now.Add(-time.Hour), now)
	removed := seedRetentionEntry(t, fixture, "removed-expired", 10, now.Add(-time.Hour), now)
	deleteErr := errors.New("injected delete failure")
	fixture.storage.failDelete(failed.StoragePath, deleteErr)

	report, err := fixture.retention.Reclaim(context.Background(), ReclaimModeManual)
	if !errors.Is(err, deleteErr) {
		t.Fatalf("Reclaim error = %v, want injected failure", err)
	}
	if report.Examined != 2 || report.Removed != 1 || report.Failed != 1 || report.ExpiredRemoved != 1 {
		t.Fatalf("report = %+v", report)
	}
	assertRetentionEntryExists(t, fixture, failed.ID, true)
	assertRetentionEntryExists(t, fixture, removed.ID, false)
}

func TestRetentionManualReclaimStillRemovesExpiredWhenInitialUsageFails(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 100, ThresholdPercent: 90, TargetPercent: 80})
	entry := seedRetentionEntry(t, fixture, "expired-after-usage-error", 10, time.Now().Add(-time.Hour), time.Now())
	usageErr := errors.New("usage unavailable")
	fixture.storage.failNextTotalSize(usageErr)

	report, err := fixture.retention.Reclaim(context.Background(), ReclaimModeManual)
	if !errors.Is(err, usageErr) {
		t.Fatalf("Reclaim error = %v, want usage error", err)
	}
	if report.Removed != 1 || report.ExpiredRemoved != 1 || report.ReclaimedBytes != 10 || report.UsageAfter != 0 {
		t.Fatalf("report = %+v", report)
	}
	assertRetentionEntryExists(t, fixture, entry.ID, false)
}

func TestRetentionBatchDatabaseFailureCountsBytesOnlyOnFirstAttempt(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 100, ThresholdPercent: 90, TargetPercent: 80})
	entry := seedRetentionEntry(t, fixture, "expired-db-failure", 10, time.Now().Add(-time.Hour), time.Now())
	if err := fixture.database.Exec(`
		CREATE TRIGGER fail_reclaim_delete
		BEFORE DELETE ON cache_entries
		BEGIN
			SELECT RAISE(FAIL, 'delete blocked');
		END`).Error; err != nil {
		t.Fatal(err)
	}

	first, err := fixture.retention.Reclaim(context.Background(), ReclaimModeManual)
	if err == nil {
		t.Fatal("first Reclaim succeeded despite database delete trigger")
	}
	if first.Removed != 0 || first.Failed != 1 || first.ReclaimedBytes != 10 || first.UsageAfter != 0 {
		t.Fatalf("first report = %+v", first)
	}
	assertRetentionEntryExists(t, fixture, entry.ID, true)

	if err := fixture.database.Exec("DROP TRIGGER fail_reclaim_delete").Error; err != nil {
		t.Fatal(err)
	}
	second, err := fixture.retention.Reclaim(context.Background(), ReclaimModeManual)
	if err != nil {
		t.Fatal(err)
	}
	if second.Removed != 1 || second.ExpiredRemoved != 1 || second.ReclaimedBytes != 0 {
		t.Fatalf("retry report = %+v", second)
	}
	assertRetentionEntryExists(t, fixture, entry.ID, false)
}

func TestRetentionRemoveHonorsMutationGateCancellation(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 100, ThresholdPercent: 90, TargetPercent: 80})
	entry := seedRetentionEntry(t, fixture, "locked-entry", 10, time.Now().Add(time.Hour), time.Now())
	unlock, err := fixture.manager.mutations.lock(context.Background(), entry.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := fixture.retention.Remove(ctx, entry.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Remove error = %v, want deadline exceeded", err)
	}
	assertRetentionEntryExists(t, fixture, entry.ID, true)
}

func TestRetentionReclaimStopsBeforeNextCandidateAfterCancellation(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 100, ThresholdPercent: 90, TargetPercent: 80})
	now := time.Now().UTC()
	first := seedRetentionEntry(t, fixture, "cancel-first", 10, now.Add(-2*time.Hour), now)
	second := seedRetentionEntry(t, fixture, "cancel-second", 10, now.Add(-time.Hour), now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var cancelOnce sync.Once
	const callbackName = "retention_test:cancel_after_delete"
	if err := fixture.database.Callback().Delete().After("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "cache_entries" {
			cancelOnce.Do(cancel)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fixture.database.Callback().Delete().Remove(callbackName) })

	report, err := fixture.retention.Reclaim(ctx, ReclaimModeManual)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Reclaim error = %v, want context cancellation", err)
	}
	if report.Examined != 1 || report.Removed != 1 || report.ExpiredRemoved != 1 {
		t.Fatalf("report = %+v", report)
	}
	assertRetentionEntryExists(t, fixture, first.ID, false)
	assertRetentionEntryExists(t, fixture, second.ID, true)
}
