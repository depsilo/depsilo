package cache

import (
	"context"
	"testing"
	"time"
)

func TestStartLRUCleanupSchedulesCapacityReclaimUntilCancellation(t *testing.T) {
	fixture := newRetentionFixture(t, RetentionPolicy{MaxBytes: 10, ThresholdPercent: 80, TargetPercent: 50})
	entry := seedRetentionEntry(t, fixture, "scheduled-expired", 10, time.Now().Add(-time.Hour), time.Now())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		StartLRUCleanup(ctx, fixture.retention, 5*time.Millisecond)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		var count int64
		if err := fixture.database.Model(&entry).Where("id = ?", entry.ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduled capacity reclaim did not remove expired entry")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StartLRUCleanup did not stop after cancellation")
	}
}

func TestStartLRUCleanupRejectsInvalidSchedulerInputs(t *testing.T) {
	done := make(chan struct{})
	go func() {
		StartLRUCleanup(context.Background(), nil, time.Second)
		StartLRUCleanup(context.Background(), &Retention{}, 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("invalid scheduler inputs did not return")
	}
}
