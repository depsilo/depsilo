package compilecache

import (
	"context"
	"sync"
)

// keyLockGate serializes one exact namespace/key identity without making
// unrelated keys collide. Entries are reference counted so historical ccache
// keys do not leave an unbounded lock map behind.
type keyLockGate struct {
	mu      sync.Mutex
	entries map[string]*keyLockEntry
}

type keyLockEntry struct {
	token chan struct{}
	refs  int
}

func newKeyLockGate() *keyLockGate {
	return &keyLockGate{entries: make(map[string]*keyLockEntry)}
}

func (gate *keyLockGate) acquire(key string) *keyLockEntry {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	entry := gate.entries[key]
	if entry == nil {
		entry = &keyLockEntry{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		gate.entries[key] = entry
	}
	entry.refs++
	return entry
}

func (gate *keyLockGate) lock(ctx context.Context, key string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entry := gate.acquire(key)
	select {
	case <-entry.token:
		if err := ctx.Err(); err != nil {
			gate.release(key, entry, true)
			return nil, err
		}
	case <-ctx.Done():
		gate.release(key, entry, false)
		return nil, ctx.Err()
	}
	var once sync.Once
	return func() { once.Do(func() { gate.release(key, entry, true) }) }, nil
}

func (gate *keyLockGate) tryLock(key string) (func(), bool) {
	entry := gate.acquire(key)
	select {
	case <-entry.token:
		var once sync.Once
		return func() { once.Do(func() { gate.release(key, entry, true) }) }, true
	default:
		gate.release(key, entry, false)
		return nil, false
	}
}

func (gate *keyLockGate) release(key string, entry *keyLockEntry, ownsToken bool) {
	if ownsToken {
		entry.token <- struct{}{}
	}
	gate.mu.Lock()
	entry.refs--
	if entry.refs == 0 && gate.entries[key] == entry {
		delete(gate.entries, key)
	}
	gate.mu.Unlock()
}
