package cache

import (
	"sync"
)

const maxRetainedInvalidations = 4096

type cacheInvalidationState struct {
	generation    uint64
	writers       int
	invalidations int
	tombstoned    bool
	cleanupSafe   bool
	retained      bool
}

type cacheInvalidationRegistry struct {
	mu       sync.Mutex
	states   map[string]*cacheInvalidationState
	retained int
	// failClosed is entered only after too many unresolved double-delete
	// failures. It bounds memory without making any evicted tombstone readable;
	// cache reads remain disabled until the Manager is restarted.
	failClosed bool
}

func newCacheInvalidationRegistry() *cacheInvalidationRegistry {
	return &cacheInvalidationRegistry{states: make(map[string]*cacheInvalidationState)}
}

func (r *cacheInvalidationRegistry) beginWrite(key string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[key]
	if state == nil {
		state = &cacheInvalidationState{}
		r.states[key] = state
	}
	r.unretainLocked(state)
	state.writers++
	return state.generation
}

func (r *cacheInvalidationRegistry) writeCurrent(key string, generation uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[key]
	return state != nil && state.generation == generation
}

// commitWrite is the write linearization point. A concurrent invalidation
// either advances the generation first (and this commit is rejected), or runs
// afterwards and removes the newly committed representation.
func (r *cacheInvalidationRegistry) commitWrite(key string, generation uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[key]
	if state == nil || state.generation != generation {
		return false
	}
	state.tombstoned = false
	state.cleanupSafe = true
	state.writers--
	r.settleIdleLocked(key, state)
	return true
}

func (r *cacheInvalidationRegistry) abortWrite(key string, cleanupSafe ...bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[key]
	if state == nil {
		return
	}
	if len(cleanupSafe) > 0 && cleanupSafe[0] {
		state.cleanupSafe = true
	}
	state.writers--
	r.settleIdleLocked(key, state)
}

func (r *cacheInvalidationRegistry) markCleanupSafe(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[key]
	if state == nil {
		return
	}
	state.cleanupSafe = true
	r.settleIdleLocked(key, state)
}

func (r *cacheInvalidationRegistry) beginInvalidation(key string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[key]
	if state == nil {
		state = &cacheInvalidationState{}
		r.states[key] = state
	}
	r.unretainLocked(state)
	state.generation++
	state.invalidations++
	state.tombstoned = true
	state.cleanupSafe = false
	return state.generation
}

func (r *cacheInvalidationRegistry) invalidationPending(key string, generation uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[key]
	return state != nil && state.generation == generation && state.tombstoned
}

func (r *cacheInvalidationRegistry) finishInvalidation(key string, generation uint64, cleanupSafe bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[key]
	if state == nil {
		return
	}
	if state.generation == generation && state.tombstoned && cleanupSafe {
		state.cleanupSafe = true
	}
	state.invalidations--
	r.settleIdleLocked(key, state)
}

func (r *cacheInvalidationRegistry) tombstoned(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[key]
	return r.failClosed || state != nil && state.tombstoned
}

// readable is the read linearization point against authoritative invalidation.
// The lock is deliberately not held across storage I/O: a read admitted here
// precedes a later invalidation, while a tombstone already present rejects it.
func (r *cacheInvalidationRegistry) readable(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[key]
	return !r.failClosed && (state == nil || !state.tombstoned)
}

func (r *cacheInvalidationRegistry) settleIdleLocked(key string, state *cacheInvalidationState) {
	if state.writers != 0 || state.invalidations != 0 {
		return
	}
	if state.tombstoned && state.cleanupSafe {
		state.tombstoned = false
	}
	if !state.tombstoned {
		r.unretainLocked(state)
		delete(r.states, key)
		return
	}
	if r.failClosed {
		r.unretainLocked(state)
		delete(r.states, key)
		return
	}
	if !state.retained {
		state.retained = true
		r.retained++
	}
	r.capRetainedLocked()
}

func (r *cacheInvalidationRegistry) capRetainedLocked() {
	if r.retained <= maxRetainedInvalidations {
		return
	}
	r.failClosed = true
	for key, state := range r.states {
		if state.writers == 0 && state.invalidations == 0 {
			delete(r.states, key)
		}
	}
	r.retained = 0
}

func (r *cacheInvalidationRegistry) unretainLocked(state *cacheInvalidationState) {
	if !state.retained {
		return
	}
	state.retained = false
	r.retained--
}
