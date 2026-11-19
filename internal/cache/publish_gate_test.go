package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

// trickleBody sends head at once and holds tail until release closes, like an
// Upstream that answers promptly and then sends the body slowly.
type trickleBody struct {
	head, tail []byte
	release    chan struct{}
	closed     chan struct{}
	closeOnce  sync.Once
}

func newTrickleBody(head, tail string) *trickleBody {
	return &trickleBody{
		head:    []byte(head),
		tail:    []byte(tail),
		release: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (b *trickleBody) Read(p []byte) (int, error) {
	if len(b.head) > 0 {
		n := copy(p, b.head)
		b.head = b.head[n:]
		return n, nil
	}
	select {
	case <-b.release:
	case <-b.closed:
		return 0, errors.New("trickle body closed")
	}
	if len(b.tail) > 0 {
		n := copy(p, b.tail)
		b.tail = b.tail[n:]
		return n, nil
	}
	return 0, io.EOF
}

func (b *trickleBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

// observedLocalStorage is a LocalStorage that reports when a write has started
// consuming the new representation, so tests can act while it streams.
type observedLocalStorage struct {
	*LocalStorage
	streaming chan struct{}
	once      sync.Once
}

func newObservedLocalStorage(t *testing.T) *observedLocalStorage {
	t.Helper()
	local, err := NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &observedLocalStorage{LocalStorage: local, streaming: make(chan struct{})}
}

func (s *observedLocalStorage) observe(r io.Reader) io.Reader {
	return &firstReadSignal{reader: r, signal: func() { s.once.Do(func() { close(s.streaming) }) }}
}

func (s *observedLocalStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	return s.LocalStorage.Put(ctx, key, s.observe(r), size, contentType)
}

func (s *observedLocalStorage) PutStaged(ctx context.Context, key string, r io.Reader, size int64, contentType string, publish func() error) error {
	return s.LocalStorage.PutStaged(ctx, key, s.observe(r), size, contentType, publish)
}

func (s *observedLocalStorage) waitStreaming(t *testing.T) {
	t.Helper()
	select {
	case <-s.streaming:
	case <-time.After(2 * time.Second):
		t.Fatal("cache write never started streaming into storage")
	}
}

type firstReadSignal struct {
	reader io.Reader
	signal func()
}

func (r *firstReadSignal) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.signal()
	}
	return n, err
}

// seedLocalEntry stores body under key and writes its metadata row.
func seedLocalEntry(t *testing.T, storage Storage, database *gorm.DB, entry db.CacheEntry, body string) {
	t.Helper()
	if err := storage.Put(t.Context(), entry.Key, bytes.NewReader([]byte(body)), int64(len(body)), entry.ContentType); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
}

// promptGet reads key with a deadline well below the stalled writer, so a
// reader blocked behind the stream fails instead of hanging the test.
func promptGet(t *testing.T, m *Manager, key string, fetch FetchFunc) (*GetResult, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result, err := m.Get(ctx, key, "pypi", 5*time.Minute, fetch)
	if err != nil {
		return nil, nil, err
	}
	defer result.Reader.Close()
	body, err := io.ReadAll(result.Reader)
	return result, body, err
}

func unexpectedFetch(context.Context) (io.ReadCloser, string, int64, string, error) {
	return nil, "", 0, "", errors.New("cache hit unexpectedly contacted the Upstream")
}

func TestManagerSlowStreamingRefreshServesStaleCopyToFollower(t *testing.T) {
	const key = "pypi/simple/av/index.html"
	database := openStreamTestDB(t)
	storage := newObservedLocalStorage(t)
	seedLocalEntry(t, storage.LocalStorage, database, db.CacheEntry{
		Key: key, AdapterType: "pypi", StoragePath: key, ContentType: "text/old",
		ExpiresAt: time.Now().Add(-time.Minute),
	}, "versions: 17.1.0")
	m := NewManager(storage, database, NewEventBus(), 72*time.Hour)
	m.staleRefreshBudget = 20 * time.Millisecond
	t.Cleanup(func() { closeTestManager(t, m) })

	body := newTrickleBody("versions: 17.1.0, ", "18.0.0")
	defer body.Close()
	fetches := 0
	fetch := func(context.Context) (io.ReadCloser, string, int64, string, error) {
		fetches++
		return body, "text/new", -1, "mock", nil
	}

	// The request that starts the refresh receives the live, slow stream.
	leader, err := m.Get(t.Context(), key, "pypi", 5*time.Minute, fetch)
	if err != nil {
		t.Fatal(err)
	}
	leaderDone := make(chan []byte, 1)
	go func() {
		defer leader.Reader.Close()
		data, _ := io.ReadAll(leader.Reader)
		leaderDone <- data
	}()
	storage.waitStreaming(t)

	// A follower that outwaits the stale budget is served the committed stale
	// copy while the refresh is still streaming.
	result, got, err := promptGet(t, m, key, fetch)
	if err != nil {
		t.Fatalf("follower blocked behind the streaming refresh: %v", err)
	}
	if string(got) != "versions: 17.1.0" || result.ContentType != "text/old" {
		t.Fatalf("follower = body:%q type:%q, want the stale copy", got, result.ContentType)
	}

	close(body.release)
	select {
	case data := <-leaderDone:
		if string(data) != "versions: 17.1.0, 18.0.0" {
			t.Fatalf("leader stream = %q", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("leader stream did not finish after the Upstream sent the rest")
	}
	waitForNoInflight(t, m)
	result, got, err = promptGet(t, m, key, unexpectedFetch)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "versions: 17.1.0, 18.0.0" || result.ContentType != "text/new" {
		t.Fatalf("after commit = body:%q type:%q, want the refreshed index", got, result.ContentType)
	}
	if fetches != 1 {
		t.Fatalf("upstream fetches = %d, want 1", fetches)
	}
}

func TestManagerHitDuringStreamingRewriteSeesOneConsistentRepresentation(t *testing.T) {
	const key = "pypi/simple/rewrite/index.html"
	database := openStreamTestDB(t)
	storage := newObservedLocalStorage(t)
	seedLocalEntry(t, storage.LocalStorage, database, db.CacheEntry{
		Key: key, AdapterType: "pypi", StoragePath: key, CacheKind: db.CacheKindMetadata,
		ContentType: "text/old", ETag: `"old"`,
		ResponseHeaders: encodeResponseMetadata(http.Header{"ETag": {`"old"`}}),
		ExpiresAt:       time.Now().Add(time.Hour),
	}, "old-body")
	m := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })

	body := newTrickleBody("new-", "body")
	defer body.Close()
	refreshDone := make(chan struct{})
	go func() {
		defer close(refreshDone)
		m.backgroundRefresh(t.Context(), key, "pypi", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				return WithResponseMetadata(body, http.Header{"ETag": {`"new"`}}), "text/new", -1, "mock", nil
			})
	}()
	storage.waitStreaming(t)

	result, got, err := promptGet(t, m, key, unexpectedFetch)
	if err != nil {
		t.Fatalf("cache hit blocked behind the streaming rewrite: %v", err)
	}
	if string(got) != "old-body" || result.ContentType != "text/old" || result.Headers.Get("ETag") != `"old"` {
		t.Fatalf("hit during rewrite = body:%q type:%q etag:%q, want the committed old pair",
			got, result.ContentType, result.Headers.Get("ETag"))
	}

	close(body.release)
	select {
	case <-refreshDone:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not finish after the Upstream sent the rest")
	}
	result, got, err = promptGet(t, m, key, unexpectedFetch)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-body" || result.ContentType != "text/new" || result.Headers.Get("ETag") != `"new"` {
		t.Fatalf("hit after rewrite = body:%q type:%q etag:%q, want the committed new pair",
			got, result.ContentType, result.Headers.Get("ETag"))
	}
}

func TestManagerInvalidationDuringStreamingWinsOverTheWriter(t *testing.T) {
	const key = "pypi/simple/revoked/index.html"
	database := openStreamTestDB(t)
	storage := newObservedLocalStorage(t)
	seedLocalEntry(t, storage.LocalStorage, database, db.CacheEntry{
		Key: key, AdapterType: "pypi", StoragePath: key, CacheKind: db.CacheKindMetadata,
		ContentType: "text/html", ExpiresAt: time.Now().Add(-time.Minute),
	}, "old-index")
	m := NewManager(storage, database, NewEventBus(), 72*time.Hour)
	m.invalidationTimeout = time.Second
	t.Cleanup(func() { closeTestManager(t, m) })

	body := newTrickleBody("new-", "index")
	defer body.Close()
	prefetchDone := make(chan error, 1)
	go func() {
		prefetchDone <- m.Prefetch(t.Context(), key, "pypi", 5*time.Minute,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				return body, "text/html", -1, "mock", nil
			})
	}()
	storage.waitStreaming(t)

	invalidated := make(chan error, 1)
	go func() { invalidated <- m.Invalidate(t.Context(), key) }()
	select {
	case err := <-invalidated:
		if err != nil {
			t.Fatalf("Invalidate during streaming: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Invalidate waited for the streaming writer")
	}

	close(body.release)
	select {
	case err := <-prefetchDone:
		if !errors.Is(err, errCacheWriteSuperseded) {
			t.Fatalf("Prefetch error = %v, want the superseded write", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("superseded writer did not finish")
	}

	if exists, err := storage.Exists(t.Context(), key); err != nil || exists {
		t.Fatalf("object after invalidation: exists=%t err=%v, want absent", exists, err)
	}
	var rows int64
	if err := database.Model(&db.CacheEntry{}).Where("key = ?", key).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("metadata rows = %d, want 0", rows)
	}
	staging, err := storage.ListStaging(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(staging) != 0 {
		t.Fatalf("superseded writer left staging objects: %+v", staging)
	}
	offline := errors.New("upstream offline")
	result, err := m.Get(t.Context(), key, "pypi", 5*time.Minute,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "mock", &testStalePolicyError{cause: offline, allow: true}
		})
	if result != nil {
		_ = result.Reader.Close()
	}
	if !errors.Is(err, offline) {
		t.Fatalf("Get after invalidation = %+v, err=%v; want the Upstream failure", result, err)
	}
}

// unserializedStagedStorage stages each write in memory and switches it into
// the LocalStorage only after publish, without serializing writers of the same
// key. Like S3 multipart uploads, a newer write can stage, publish and commit
// while an older one is still streaming.
type unserializedStagedStorage struct {
	*observedLocalStorage
}

func (s *unserializedStagedStorage) PutStaged(ctx context.Context, key string, r io.Reader, size int64, contentType string, publish func() error) error {
	staged, err := io.ReadAll(s.observe(r))
	if err != nil {
		return err
	}
	if err := runPublish(publish); err != nil {
		return err
	}
	return s.LocalStorage.Put(ctx, key, bytes.NewReader(staged), int64(len(staged)), contentType)
}

func TestManagerSupersededRefreshKeepsANewerCommittedFill(t *testing.T) {
	const key = "pypi/simple/superseded/index.html"
	database := openStreamTestDB(t)
	storage := &unserializedStagedStorage{observedLocalStorage: newObservedLocalStorage(t)}
	seedLocalEntry(t, storage.LocalStorage, database, db.CacheEntry{
		Key: key, AdapterType: "pypi", StoragePath: key, CacheKind: db.CacheKindMetadata,
		ContentType: "text/old", ExpiresAt: time.Now().Add(-time.Minute),
	}, "old-index")
	m := NewManager(storage, database, NewEventBus(), 72*time.Hour)
	m.invalidationTimeout = time.Second
	t.Cleanup(func() { closeTestManager(t, m) })

	// (1) A background refresh streams its body slowly.
	slow := newTrickleBody("slow-", "refresh")
	defer slow.Close()
	refreshDone := make(chan struct{})
	go func() {
		defer close(refreshDone)
		m.backgroundRefresh(t.Context(), key, "pypi", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				return slow, "text/slow", -1, "mock", nil
			})
	}()
	storage.waitStreaming(t)

	// (2) An invalidation supersedes it and finishes at once.
	if err := m.Invalidate(t.Context(), key); err != nil {
		t.Fatalf("Invalidate during streaming: %v", err)
	}

	// (3) A new request misses and commits a fill from a fast Upstream.
	result, got, err := promptGet(t, m, key, func(context.Context) (io.ReadCloser, string, int64, string, error) {
		return io.NopCloser(bytes.NewReader([]byte("fresh-index"))), "text/fresh", int64(len("fresh-index")), "mock", nil
	})
	if err != nil {
		t.Fatalf("fill after invalidation: %v", err)
	}
	if string(got) != "fresh-index" || result.ContentType != "text/fresh" {
		t.Fatalf("fill = body:%q type:%q", got, result.ContentType)
	}
	waitForNoInflight(t, m)

	// (4) The superseded refresh reaches publish and is refused.
	close(slow.release)
	select {
	case <-refreshDone:
	case <-time.After(2 * time.Second):
		t.Fatal("superseded refresh did not finish")
	}

	result, got, err = promptGet(t, m, key, unexpectedFetch)
	if err != nil {
		t.Fatalf("newer committed fill lost after the superseded refresh: %v", err)
	}
	if string(got) != "fresh-index" || result.ContentType != "text/fresh" {
		t.Fatalf("hit after superseded refresh = body:%q type:%q, want the newer fill", got, result.ContentType)
	}
}
