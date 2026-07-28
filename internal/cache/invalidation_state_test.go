package cache

import (
	"fmt"
	"testing"
)

func TestInvalidationRegistryFailsClosedAtRetainedTombstoneLimit(t *testing.T) {
	registry := newCacheInvalidationRegistry()
	for i := 0; i <= maxRetainedInvalidations; i++ {
		key := fmt.Sprintf("failed-delete-%d", i)
		generation := registry.beginInvalidation(key)
		registry.finishInvalidation(key, generation, false)
	}

	registry.mu.Lock()
	failClosed := registry.failClosed
	states := len(registry.states)
	registry.mu.Unlock()
	if !failClosed {
		t.Fatal("registry did not fail closed after reaching the retained tombstone limit")
	}
	if states != 0 {
		t.Fatalf("fail-closed registry retained %d idle per-key states", states)
	}
	if !registry.tombstoned("previously-unseen-key") {
		t.Fatal("fail-closed registry allowed a cache read for an untracked key")
	}
}
