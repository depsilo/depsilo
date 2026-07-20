package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestKeyMutationGateSerializesOnlyMatchingKeys(t *testing.T) {
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	t.Cleanup(cancelLifecycle)
	gate := newKeyMutationGate(lifecycleCtx)

	unlockFirst, err := gate.lock(t.Context(), "pypi/pkg")
	if err != nil {
		t.Fatal(err)
	}

	sameKey := make(chan struct{})
	go func() {
		unlock, lockErr := gate.lock(context.Background(), "pypi/pkg")
		if lockErr == nil {
			unlock()
		}
		close(sameKey)
	}()

	differentKey := make(chan error, 1)
	go func() {
		unlock, lockErr := gate.lock(context.Background(), "npm/pkg")
		if lockErr == nil {
			unlock()
		}
		differentKey <- lockErr
	}()

	select {
	case err := <-differentKey:
		if err != nil {
			t.Fatalf("different key lock: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("different key was blocked")
	}
	select {
	case <-sameKey:
		t.Fatal("matching key acquired before its owner released")
	case <-time.After(25 * time.Millisecond):
	}

	unlockFirst()
	select {
	case <-sameKey:
	case <-time.After(time.Second):
		t.Fatal("matching key did not acquire after release")
	}

	gate.mu.Lock()
	entryCount := len(gate.entries)
	gate.mu.Unlock()
	if entryCount != 0 {
		t.Fatalf("gate retained %d unused entries", entryCount)
	}
}

func TestKeyMutationGateWaitHonorsOperationAndLifecycleCancellation(t *testing.T) {
	t.Run("operation", func(t *testing.T) {
		gate := newKeyMutationGate(context.Background())
		unlock, err := gate.lock(t.Context(), "pypi/pkg")
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := gate.lock(ctx, "pypi/pkg"); !errors.Is(err, context.Canceled) {
			t.Fatalf("lock error = %v, want context.Canceled", err)
		}
	})

	t.Run("lifecycle", func(t *testing.T) {
		lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
		gate := newKeyMutationGate(lifecycleCtx)
		unlock, err := gate.lock(t.Context(), "pypi/pkg")
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()

		result := make(chan error, 1)
		go func() {
			_, lockErr := gate.lock(context.Background(), "pypi/pkg")
			result <- lockErr
		}()
		cancelLifecycle()
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("lock error = %v, want context.Canceled", err)
			}
		case <-time.After(time.Second):
			t.Fatal("lifecycle cancellation did not release waiter")
		}
	})
}

func TestKeyMutationGateRejectsEmptyKeyAndDoubleUnlockIsSafe(t *testing.T) {
	gate := newKeyMutationGate(context.Background())
	if _, err := gate.lock(t.Context(), ""); !errors.Is(err, errEmptyMutationKey) {
		t.Fatalf("empty key error = %v", err)
	}
	unlock, err := gate.lock(t.Context(), "pypi/pkg")
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	unlock()
}
