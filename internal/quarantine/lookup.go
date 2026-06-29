package quarantine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	"depsilo/internal/quarantine/resolvers"
)

// Lookup combines the persistent PackageTimestamp cache (db) with the
// per-ecosystem resolvers (upstream registry API). Single source of
// truth for "when was (ecosystem, package, version) published?"
//
// Hot path: every adapter handler that participates in quarantine
// will call this once per fetch. The persistent cache covers all
// already-seen versions; only the first request for an unseen version
// hits the upstream registry. The singleflight wrapper coalesces
// concurrent first-request bursts so a parallel install of 200
// packages doesn't fire 200 simultaneous upstream metadata calls per
// process.
type Lookup struct {
	store     *Store
	resolvers resolvers.Registry
	sf        singleflight.Group

	// Concurrent in-flight cache (process-local, fastest tier).
	// Memoizes results for the lifetime of the process so two adapter
	// handlers asking about the same version within ~ms don't both
	// hit the DB. The DB cache is still authoritative across process
	// restarts; this is just RAM.
	memMu sync.RWMutex
	mem   map[string]memEntry
}

type memEntry struct {
	publishAt time.Time
	err       error // nil for found, ErrNotFound / ErrUnsupported sentinel for negative
}

// NewLookup wires a Store to a Registry. resolvers may be nil — that's
// the test-friendly path where the caller pre-populates the store
// directly and never wants real upstream calls.
func NewLookup(store *Store, reg resolvers.Registry) *Lookup {
	return &Lookup{
		store:     store,
		resolvers: reg,
		mem:       make(map[string]memEntry, 256),
	}
}

// Get returns the publish time, looking through the in-memory cache,
// then the DB cache, then finally the upstream resolver. Errors are
// returned wrapped so callers can errors.Is them against the
// resolvers package's sentinel set (ErrNotFound, ErrUnsupported,
// ErrUpstreamUnavailable).
//
// Successful resolutions are persisted in both caches before return.
// Negative resolutions (ErrNotFound) are memoized but NOT persisted
// — a 404 today could be a fresh-publish tomorrow, and the DB cache
// is built around immutable rows.
func (l *Lookup) Get(ctx context.Context, ecosystem, pkg, version string) (time.Time, error) {
	if l == nil {
		return time.Time{}, resolvers.ErrUnsupported
	}
	ecosystem = strings.ToLower(ecosystem)
	cacheKey := memKey(ecosystem, pkg, version)

	// L1: in-memory cache.
	l.memMu.RLock()
	if entry, ok := l.mem[cacheKey]; ok {
		l.memMu.RUnlock()
		return entry.publishAt, entry.err
	}
	l.memMu.RUnlock()

	// L2: DB cache.
	if l.store != nil {
		if t, ok, err := l.store.LookupTimestamp(ctx, ecosystem, pkg, version); err == nil && ok {
			l.memorize(cacheKey, t, nil)
			return t, nil
		}
	}

	// L3: upstream — coalesce concurrent first-request bursts.
	v, err, _ := l.sf.Do(cacheKey, func() (any, error) {
		return l.fetchUpstream(ctx, ecosystem, pkg, version)
	})
	if err != nil {
		// Negative results are remembered in memory only — DB cache
		// stays unpolluted by transient 404s.
		l.memorize(cacheKey, time.Time{}, err)
		return time.Time{}, err
	}
	t := v.(time.Time)
	l.memorize(cacheKey, t, nil)
	if l.store != nil {
		if err := l.store.SaveTimestamp(ctx, ecosystem, pkg, version, t); err != nil {
			zap.L().Warn("quarantine: save timestamp", zap.Error(err),
				zap.String("ecosystem", ecosystem), zap.String("package", pkg), zap.String("version", version))
		}
	}
	return t, nil
}

func (l *Lookup) fetchUpstream(ctx context.Context, ecosystem, pkg, version string) (time.Time, error) {
	if l.resolvers == nil {
		return time.Time{}, resolvers.ErrUnsupported
	}
	resolver, ok := l.resolvers[ecosystem]
	if !ok {
		return time.Time{}, resolvers.ErrUnsupported
	}
	t, err := resolver.Lookup(ctx, pkg, version)
	if err != nil {
		// Don't double-wrap if already a sentinel.
		if errors.Is(err, resolvers.ErrNotFound) ||
			errors.Is(err, resolvers.ErrUnsupported) ||
			errors.Is(err, resolvers.ErrUpstreamUnavailable) {
			return time.Time{}, err
		}
		return time.Time{}, err
	}
	return t, nil
}

func (l *Lookup) memorize(key string, t time.Time, err error) {
	l.memMu.Lock()
	defer l.memMu.Unlock()
	// Bound the in-memory cache to keep RAM honest on long-lived
	// processes with extremely diverse dependency graphs. 4096 is
	// arbitrary but large enough that any single CI run will fit;
	// when the limit is hit we drop the whole map (LRU is overkill
	// here — we just need an upper bound).
	if len(l.mem) >= 4096 {
		l.mem = make(map[string]memEntry, 256)
	}
	l.mem[key] = memEntry{publishAt: t, err: err}
}

func memKey(ecosystem, pkg, version string) string {
	return ecosystem + "\x00" + pkg + "\x00" + version
}
