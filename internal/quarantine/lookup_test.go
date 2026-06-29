package quarantine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/quarantine/resolvers"
)

func newLookupDB(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	sqlDB, err := d.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	// :memory: SQLite gives each pool connection its own db — pin
	// to one so the migrated schema is visible across goroutines.
	sqlDB.SetMaxOpenConns(1)
	if err := d.AutoMigrate(&db.PackageTimestamp{}, &db.ApprovedVersion{}, &db.QuarantineEvent{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return d
}

// fakeResolver counts calls and returns canned values. Lets us
// assert the caching tiers actually prevent upstream calls.
type fakeResolver struct {
	mu      sync.Mutex
	calls   int
	want    time.Time
	wantErr error
}

func (f *fakeResolver) Lookup(_ context.Context, _, _ string) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.wantErr != nil {
		return time.Time{}, f.wantErr
	}
	return f.want, nil
}

func (f *fakeResolver) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestLookup_MemoryCacheHit(t *testing.T) {
	store := NewStore(newLookupDB(t))
	fake := &fakeResolver{want: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)}
	l := NewLookup(store, resolvers.Registry{"npm": fake})

	// First call goes to fake.
	if _, err := l.Get(context.Background(), "npm", "lodash", "4.17.21"); err != nil {
		t.Fatal(err)
	}
	// Second call should hit the in-memory cache, NOT the resolver.
	if _, err := l.Get(context.Background(), "npm", "lodash", "4.17.21"); err != nil {
		t.Fatal(err)
	}
	if got := fake.callCount(); got != 1 {
		t.Errorf("resolver called %d times, want 1 (memory cache should have served the second)", got)
	}
}

func TestLookup_DBCacheHit(t *testing.T) {
	store := NewStore(newLookupDB(t))
	fake := &fakeResolver{want: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)}
	l := NewLookup(store, resolvers.Registry{"npm": fake})

	if _, err := l.Get(context.Background(), "npm", "lodash", "4.17.21"); err != nil {
		t.Fatal(err)
	}

	// Fresh Lookup instance, same Store — should hit the DB cache,
	// no upstream call.
	l2 := NewLookup(store, resolvers.Registry{"npm": fake})
	got, err := l2.Get(context.Background(), "npm", "lodash", "4.17.21")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(fake.want) {
		t.Errorf("DB cache returned %v, want %v", got, fake.want)
	}
	if got := fake.callCount(); got != 1 {
		t.Errorf("resolver called %d times, want 1 (DB cache should have served the second instance)", got)
	}
}

func TestLookup_UnsupportedEcosystem(t *testing.T) {
	store := NewStore(newLookupDB(t))
	l := NewLookup(store, resolvers.Registry{})
	_, err := l.Get(context.Background(), "exotic", "x", "1.0")
	if !errors.Is(err, resolvers.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestLookup_NotFoundNegativeMemoized(t *testing.T) {
	store := NewStore(newLookupDB(t))
	fake := &fakeResolver{wantErr: resolvers.ErrNotFound}
	l := NewLookup(store, resolvers.Registry{"npm": fake})

	for i := 0; i < 3; i++ {
		_, err := l.Get(context.Background(), "npm", "missing", "1.0")
		if !errors.Is(err, resolvers.ErrNotFound) {
			t.Fatalf("call %d: err = %v, want ErrNotFound", i, err)
		}
	}
	if got := fake.callCount(); got != 1 {
		t.Errorf("resolver called %d times, want 1 (negative result should be memoized)", got)
	}
}

func TestLookup_NotFoundNotPersisted(t *testing.T) {
	store := NewStore(newLookupDB(t))
	fake := &fakeResolver{wantErr: resolvers.ErrNotFound}
	l := NewLookup(store, resolvers.Registry{"npm": fake})

	_, _ = l.Get(context.Background(), "npm", "missing", "1.0")

	// New Lookup instance — in-memory cache is gone. If negative
	// results had been written to DB this would not call the
	// resolver again; we want it to retry.
	fake2 := &fakeResolver{wantErr: resolvers.ErrNotFound}
	l2 := NewLookup(store, resolvers.Registry{"npm": fake2})
	_, _ = l2.Get(context.Background(), "npm", "missing", "1.0")
	if got := fake2.callCount(); got != 1 {
		t.Errorf("fresh Lookup with same Store called resolver %d times, want 1 (negative results must NOT be DB-persisted)", got)
	}
}

func TestLookup_NilSafe(t *testing.T) {
	// Lookup{} with no store / no resolvers must report ErrUnsupported,
	// not panic. The adapter layer constructs Lookup before knowing
	// if quarantine is enabled, so the nil paths matter.
	var nilLookup *Lookup
	_, err := nilLookup.Get(context.Background(), "npm", "x", "1.0")
	if !errors.Is(err, resolvers.ErrUnsupported) {
		t.Errorf("nil receiver err = %v, want ErrUnsupported", err)
	}
}
