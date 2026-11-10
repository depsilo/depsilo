package cache

import (
	"bytes"
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/db"
)

// staleMetadataManager seeds an expired package index and returns a manager
// whose stale-serving budget is short enough for tests.
func staleMetadataManager(t *testing.T, key string, old []byte) *Manager {
	t.Helper()
	database := openStreamTestDB(t)
	storage := newMemStorage()
	storage.data[key] = old
	if err := database.Create(&db.CacheEntry{Key: key, AdapterType: "pypi", StoragePath: key, ContentType: "text/html", ExpiresAt: time.Now().Add(-time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	m := NewManager(storage, database, NewEventBus(), 72*time.Hour)
	m.staleRefreshBudget = 100 * time.Millisecond
	t.Cleanup(func() { closeTestManager(t, m) })
	return m
}

// gatedFetch blocks every fetch until release is closed, like an Upstream that
// accepts the connection and does not answer.
func gatedFetch(body []byte, calls *atomic.Int32, started chan<- struct{}, release <-chan struct{}) FetchFunc {
	return func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		if calls.Add(1) == 1 && started != nil {
			close(started)
		}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, "", 0, "", ctx.Err()
		}
		return io.NopCloser(bytes.NewReader(body)), "text/html", int64(len(body)), "mock", nil
	}
}

// getBytes reads one Get of an index key to the end.
func getBytes(t *testing.T, m *Manager, key string, fetch FetchFunc) []byte {
	t.Helper()
	result, err := m.Get(context.Background(), key, "pypi", 5*time.Minute, fetch)
	return readResult(t, result, err)
}

func readResult(t *testing.T, result *GetResult, err error) []byte {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	defer result.Reader.Close()
	data, err := io.ReadAll(result.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestManager_SlowMetadataRefreshServesStaleCopyWithinBudget(t *testing.T) {
	key := "pypi/simple/av/index.html"
	old, fresh := []byte("versions: 17.1.0"), []byte("versions: 17.1.0, 18.0.0")
	m := staleMetadataManager(t, key, old)
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	fetch := gatedFetch(fresh, &calls, started, release)

	// The request that starts the refresh and one that joins it both receive
	// the stale copy once the budget expires.
	var wg sync.WaitGroup
	results := make([][]byte, 2)
	elapsed := make([]time.Duration, 2)
	for i := range 2 {
		if i == 1 {
			<-started
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			begin := time.Now()
			result, err := m.Get(context.Background(), key, "pypi", 5*time.Minute, fetch)
			elapsed[i] = time.Since(begin)
			results[i] = readResult(t, result, err)
		}()
	}
	wg.Wait()
	for i, got := range results {
		if !bytes.Equal(got, old) || elapsed[i] > 2*time.Second {
			t.Fatalf("request %d got %q after %v; want the stale copy within the budget", i+1, got, elapsed[i])
		}
	}

	// The refresh outlives those requests and still commits.
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := getBytes(t, m, key, fetch)
		if bytes.Equal(got, fresh) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("refresh never committed; still serving %q", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream fetches = %d, want the single background refresh", calls.Load())
	}
	waitForNoInflight(t, m)
}

func TestManager_SlowFirstFillIsNotCutShortByTheStaleBudget(t *testing.T) {
	database := openStreamTestDB(t)
	m := NewManager(newMemStorage(), database, NewEventBus(), 72*time.Hour)
	m.staleRefreshBudget = 20 * time.Millisecond
	t.Cleanup(func() { closeTestManager(t, m) })
	body := []byte("versions: 1.0.0")
	slow := func(context.Context) (io.ReadCloser, string, int64, string, error) {
		time.Sleep(200 * time.Millisecond)
		return io.NopCloser(bytes.NewReader(body)), "text/html", int64(len(body)), "mock", nil
	}
	if got := getBytes(t, m, "pypi/simple/new/index.html", slow); !bytes.Equal(got, body) {
		t.Fatalf("first fill = %q, want the upstream index", got)
	}
}

func TestManager_PrefetchWaitsForSlowMetadataRefresh(t *testing.T) {
	key := "pypi/simple/av/index.html"
	fresh := []byte("versions: 17.1.0, 18.0.0")
	m := staleMetadataManager(t, key, []byte("versions: 17.1.0"))
	var calls atomic.Int32
	release := make(chan struct{})
	time.AfterFunc(300*time.Millisecond, func() { close(release) })
	if err := m.Prefetch(context.Background(), key, "pypi", 5*time.Minute, gatedFetch(fresh, &calls, nil, release)); err != nil {
		t.Fatalf("prefetch of slow metadata: %v", err)
	}
	if got := getBytes(t, m, key, gatedFetch(fresh, &calls, nil, release)); !bytes.Equal(got, fresh) {
		t.Fatalf("after prefetch got %q, want the refreshed index", got)
	}
}
