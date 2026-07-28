package cache

import (
	"context"
	"errors"
	"sync"
)

var errEmptyMutationKey = errors.New("cache mutation key must not be empty")

// keyMutationGate serializes storage and metadata mutations for one cache key
// within a Manager process. Different keys remain independent. Entries are
// reference counted so a cache with many historical keys does not leave one
// lock allocated per key forever. A future multi-instance runtime would need
// a durable fencing protocol rather than extending this in-memory gate.
//
// The gate is deliberately internal to the cache package. Writers hold it
// across object and metadata publication; readers briefly hold it while
// opening a stable storage snapshot that matches one metadata row.
type keyMutationGate struct {
	ctx context.Context

	mu      sync.Mutex
	entries map[string]*keyMutationEntry
}

type keyMutationEntry struct {
	token chan struct{}
	refs  int
}

func newKeyMutationGate(ctx context.Context) *keyMutationGate {
	if ctx == nil {
		ctx = context.Background()
	}
	return &keyMutationGate{
		ctx:     ctx,
		entries: make(map[string]*keyMutationEntry),
	}
}

// lock waits for exclusive mutation ownership of key. Waiting is bounded by
// both the operation context and the Manager lifecycle context supplied when
// the gate was created.
func (g *keyMutationGate) lock(ctx context.Context, key string) (func(), error) {
	return g.lockWithLifecycle(ctx, key, true)
}

// lockSnapshot participates in the same per-key exclusion as mutations but is
// not cancelled merely because Manager shutdown has begun. Existing durable
// cache entries remain readable after Close; only new manager-owned work is
// rejected.
func (g *keyMutationGate) lockSnapshot(ctx context.Context, key string) (func(), error) {
	return g.lockWithLifecycle(ctx, key, false)
}

func (g *keyMutationGate) lockWithLifecycle(
	ctx context.Context,
	key string,
	enforceLifecycle bool,
) (func(), error) {
	if key == "" {
		return nil, errEmptyMutationKey
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if enforceLifecycle {
		if err := g.ctx.Err(); err != nil {
			return nil, err
		}
	}

	g.mu.Lock()
	// Mutation admission is rechecked while holding mu so cancellation cannot
	// race creation of a new writer after the Manager lifecycle has ended.
	if enforceLifecycle {
		if err := g.ctx.Err(); err != nil {
			g.mu.Unlock()
			return nil, err
		}
	}
	entry := g.entries[key]
	if entry == nil {
		entry = &keyMutationEntry{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		g.entries[key] = entry
	}
	entry.refs++
	g.mu.Unlock()

	if !enforceLifecycle {
		select {
		case <-entry.token:
			if err := ctx.Err(); err != nil {
				g.release(key, entry, true)
				return nil, err
			}
		case <-ctx.Done():
			g.release(key, entry, false)
			return nil, ctx.Err()
		}

		var once sync.Once
		return func() {
			once.Do(func() {
				g.release(key, entry, true)
			})
		}, nil
	}

	select {
	case <-entry.token:
		// A ready token and cancellation can be selected simultaneously. Do not
		// admit a mutation after either cancellation boundary has closed.
		if err := ctx.Err(); err != nil {
			g.release(key, entry, true)
			return nil, err
		}
		if err := g.ctx.Err(); err != nil {
			g.release(key, entry, true)
			return nil, err
		}
	case <-ctx.Done():
		g.release(key, entry, false)
		return nil, ctx.Err()
	case <-g.ctx.Done():
		g.release(key, entry, false)
		return nil, g.ctx.Err()
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			g.release(key, entry, true)
		})
	}, nil
}

func (g *keyMutationGate) release(key string, entry *keyMutationEntry, ownsToken bool) {
	if ownsToken {
		entry.token <- struct{}{}
	}

	g.mu.Lock()
	entry.refs--
	if entry.refs == 0 && g.entries[key] == entry {
		delete(g.entries, key)
	}
	g.mu.Unlock()
}
