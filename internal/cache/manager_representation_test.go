package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestManager_ResponseMetadataPersistsAcrossMissAndHit(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	const key = "huggingface/models/acme/model/resolve/main/model.bin"
	payload := []byte("model-weights")
	fetches := 0
	fetch := func(context.Context) (io.ReadCloser, string, int64, string, error) {
		fetches++
		body := WithResponseMetadata(io.NopCloser(bytes.NewReader(payload)), http.Header{
			"Accept-Ranges":     {"bytes"},
			"Etag":              {`"model-v1"`},
			"Last-Modified":     {"Mon, 27 Jul 2026 01:02:03 GMT"},
			"X-Linked-Etag":     {`"blob-v1"`},
			"X-Linked-Size":     {"13"},
			"X-Repo-Commit":     {"0123456789abcdef"},
			"Location":          {"https://signed.example/private"},
			"Authorization":     {"Bearer upstream-secret"},
			"Set-Cookie":        {"session=secret"},
			"Connection":        {"keep-alive"},
			"X-Not-Allowlisted": {"must-not-persist"},
		})
		return body, "application/octet-stream", int64(len(payload)), "hub", nil
	}

	miss, err := manager.Get(context.Background(), key, "huggingface", time.Hour, fetch)
	if err != nil {
		t.Fatal(err)
	}
	assertSafeRepresentationHeaders(t, miss.Headers)
	// GetResult owns its own clone: downstream mutation must not alter the
	// metadata that the storage writer commits.
	miss.Headers.Set("ETag", `"downstream-mutated"`)
	got, err := io.ReadAll(miss.Reader)
	_ = miss.Reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("miss body = %q, want %q", got, payload)
	}
	waitForNoInflight(t, manager)

	var entry db.CacheEntry
	if err := database.Where("key = ?", key).First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if entry.ETag != `"model-v1"` || entry.LastModified != "Mon, 27 Jul 2026 01:02:03 GMT" {
		t.Fatalf("validators = (%q, %q)", entry.ETag, entry.LastModified)
	}
	for _, secret := range []string{"signed.example", "upstream-secret", "session=secret", "keep-alive", "must-not-persist"} {
		if strings.Contains(entry.ResponseHeaders, secret) {
			t.Fatalf("persisted response headers contain forbidden value %q: %s", secret, entry.ResponseHeaders)
		}
	}

	hit, err := manager.Get(context.Background(), key, "huggingface", time.Hour, func(context.Context) (io.ReadCloser, string, int64, string, error) {
		t.Fatal("fresh hit contacted upstream")
		return nil, "", 0, "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer hit.Reader.Close()
	if !hit.Hit {
		t.Fatal("second request was not a cache hit")
	}
	assertSafeRepresentationHeaders(t, hit.Headers)
	if hit.Headers.Get("ETag") != `"model-v1"` {
		t.Fatalf("hit ETag = %q, want persisted value", hit.Headers.Get("ETag"))
	}
	if fetches != 1 {
		t.Fatalf("upstream fetches = %d, want 1", fetches)
	}
}

func TestManager_ResponseMetadataReachesInflightFollower(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	const key = "huggingface/models/acme/model/resolve/main/config.json"
	payload := []byte(`{"model_type":"demo"}`)
	fetch := func(context.Context) (io.ReadCloser, string, int64, string, error) {
		body := WithResponseMetadata(io.NopCloser(bytes.NewReader(payload)), http.Header{
			"ETag":          {`"config-v1"`},
			"X-Repo-Commit": {"abcdef"},
		})
		return body, "application/json", int64(len(payload)), "hub", nil
	}
	leader, err := manager.Get(context.Background(), key, "huggingface", time.Hour, fetch)
	if err != nil {
		t.Fatal(err)
	}

	followerDone := make(chan *GetResult, 1)
	followerErr := make(chan error, 1)
	go func() {
		result, getErr := manager.Get(context.Background(), key, "huggingface", time.Hour, fetch)
		if getErr != nil {
			followerErr <- getErr
			return
		}
		followerDone <- result
	}()

	got, err := io.ReadAll(leader.Reader)
	_ = leader.Reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("leader body = %q", got)
	}

	select {
	case err := <-followerErr:
		t.Fatal(err)
	case follower := <-followerDone:
		defer follower.Reader.Close()
		if follower.Headers.Get("ETag") != `"config-v1"` || follower.Headers.Get("X-Repo-Commit") != "abcdef" {
			t.Fatalf("follower headers = %#v", follower.Headers)
		}
	case <-time.After(time.Second):
		t.Fatal("follower did not resume after cache commit")
	}
}

func TestManager_ResponseMetadataSurvivesStaleFallback(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	const key = "pypi/simple/example/index.html"
	storage.data[key] = []byte("cached")
	if err := database.Create(&db.CacheEntry{
		Key:             key,
		AdapterType:     "pypi",
		CacheKind:       db.CacheKindMetadata,
		StoragePath:     key,
		ContentType:     "text/html",
		ResponseHeaders: `{"ETag":["\"cached-v1\""],"X-Repo-Commit":["cached-commit"],"Location":["must-not-escape"]}`,
		ExpiresAt:       time.Now().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	result, err := manager.Get(context.Background(), key, "pypi", time.Minute,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "upstream", errors.New("offline")
		})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Reader.Close()
	if result.Headers.Get("ETag") != `"cached-v1"` || result.Headers.Get("X-Repo-Commit") != "cached-commit" {
		t.Fatalf("stale headers = %#v", result.Headers)
	}
	if result.Headers.Get("Location") != "" {
		t.Fatalf("stale response exposed persisted non-allowlisted Location: %#v", result.Headers)
	}
}

type testStalePolicyError struct {
	cause error
	allow bool
}

func (e *testStalePolicyError) Error() string            { return e.cause.Error() }
func (e *testStalePolicyError) Unwrap() error            { return e.cause }
func (e *testStalePolicyError) AllowStaleFallback() bool { return e.allow }

func TestManager_RespectsAuthoritativeStaleFallbackPolicy(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "authoritative", true: "transient"}[allow], func(t *testing.T) {
			database := openStreamTestDB(t)
			storage := newMemStorage()
			const key = "huggingface/api/models/acme/model"
			storage.data[key] = []byte("cached")
			if err := database.Create(&db.CacheEntry{
				Key:         key,
				AdapterType: "huggingface",
				CacheKind:   db.CacheKindMetadata,
				StoragePath: key,
				ContentType: "application/json",
				ExpiresAt:   time.Now().Add(-time.Minute),
			}).Error; err != nil {
				t.Fatal(err)
			}
			manager := NewManager(storage, database, NewEventBus(), time.Hour)
			t.Cleanup(func() { closeTestManager(t, manager) })

			upstreamErr := errors.New("authoritative upstream result")
			result, err := manager.Get(
				context.Background(),
				key,
				"huggingface",
				time.Minute,
				func(context.Context) (io.ReadCloser, string, int64, string, error) {
					return nil, "", 0, "hub", &testStalePolicyError{
						cause: upstreamErr,
						allow: allow,
					}
				},
			)
			if !allow {
				if !errors.Is(err, upstreamErr) {
					t.Fatalf("error = %v, want authoritative upstream error", err)
				}
				if result != nil {
					t.Fatalf("authoritative error returned stale result: %+v", result)
				}
				var rows int64
				if countErr := database.Model(&db.CacheEntry{}).Where("key = ?", key).Count(&rows).Error; countErr != nil {
					t.Fatal(countErr)
				}
				if rows != 0 {
					t.Fatalf("authoritative error left %d stale metadata rows", rows)
				}
				if exists, existsErr := storage.Exists(context.Background(), key); existsErr != nil || exists {
					t.Fatalf("authoritative error left stale object (exists=%v, err=%v)", exists, existsErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer result.Reader.Close()
			body, readErr := io.ReadAll(result.Reader)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(body) != "cached" || !result.Hit {
				t.Fatalf("transient fallback = (%q, hit=%v)", body, result.Hit)
			}
		})
	}
}

func TestManager_BackgroundAuthoritativeErrorInvalidatesStaleArtifact(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	const key = "huggingface/acme/model/resolve/main/model.bin"
	storage.data[key] = []byte("old public bytes")
	if err := database.Create(&db.CacheEntry{
		Key:         key,
		AdapterType: "huggingface",
		CacheKind:   db.CacheKindArtifact,
		StoragePath: key,
		ExpiresAt:   time.Now().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	revoked := errors.New("repository is now private")
	result, err := manager.Get(
		context.Background(),
		key,
		"huggingface",
		time.Minute,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "hub", &testStalePolicyError{cause: revoked, allow: false}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = result.Reader.Close()

	deadline := time.Now().Add(time.Second)
	for {
		var rows int64
		if countErr := database.Model(&db.CacheEntry{}).Where("key = ?", key).Count(&rows).Error; countErr != nil {
			t.Fatal(countErr)
		}
		exists, existsErr := storage.Exists(context.Background(), key)
		if existsErr != nil {
			t.Fatal(existsErr)
		}
		if rows == 0 && !exists {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("authoritative background result did not invalidate stale artifact")
		}
		time.Sleep(5 * time.Millisecond)
	}

	offline := errors.New("offline")
	second, secondErr := manager.Get(
		context.Background(),
		key,
		"huggingface",
		time.Minute,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "hub", offline
		},
	)
	if !errors.Is(secondErr, offline) || second != nil {
		t.Fatalf("invalidated cache revived on transient error: result=%+v err=%v", second, secondErr)
	}
}

func TestManager_ForcedAuthoritativeErrorStillInvalidatesStaleEntry(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	const key = "huggingface/api/models/acme/private-model"
	storage.data[key] = []byte(`{"public":true}`)
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "huggingface", CacheKind: db.CacheKindMetadata,
		StoragePath: key, ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	revoked := errors.New("repository became private")
	result, err := manager.Get(
		WithForceRefresh(context.Background()),
		key,
		"huggingface",
		time.Minute,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "hub", &testStalePolicyError{cause: revoked, allow: false}
		},
	)
	if !errors.Is(err, revoked) || result != nil {
		t.Fatalf("forced authoritative result = %+v, err=%v", result, err)
	}
	var rows int64
	if countErr := database.Model(&db.CacheEntry{}).Where("key = ?", key).Count(&rows).Error; countErr != nil {
		t.Fatal(countErr)
	}
	exists, existsErr := storage.Exists(context.Background(), key)
	if existsErr != nil {
		t.Fatal(existsErr)
	}
	if rows != 0 || exists {
		t.Fatalf("forced authoritative refresh left stale state: rows=%d object=%v", rows, exists)
	}
}

func TestManager_TamperVerifyOnlyRefreshPreservesCachedRepresentation(t *testing.T) {
	manager, recorder := newTamperTestManager(t)
	storage := manager.storage.(*memStorage)

	const (
		key     = "pypi/files/example-1.0.0.whl"
		payload = "same artifact bytes"
	)
	storage.data[key] = []byte(payload)
	if err := manager.db.Create(&db.CacheEntry{
		Key:             key,
		AdapterType:     "pypi",
		CacheKind:       db.CacheKindArtifact,
		StoragePath:     key,
		ContentType:     "application/octet-stream",
		ETag:            `"old-etag"`,
		ResponseHeaders: `{"Etag":["\"old-etag\""],"X-Repo-Commit":["old-commit"]}`,
		ExpiresAt:       time.Now().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}

	refreshStarted := time.Now()
	stale, err := manager.Get(context.Background(), key, "pypi", 2*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			body := WithResponseMetadata(io.NopCloser(strings.NewReader(payload)), http.Header{
				"Content-Encoding": {"gzip"},
				"ETag":             {`"new-etag"`},
				"X-Repo-Commit":    {"new-commit"},
			})
			return body, "application/gzip", int64(len(payload)), "upstream", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	staleBody, err := io.ReadAll(stale.Reader)
	_ = stale.Reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(staleBody) != payload {
		t.Fatalf("stale body = %q, want %q", staleBody, payload)
	}

	waitFor(t, func() bool {
		if len(recorder.verifiedKeys()) != 1 {
			return false
		}
		var entry db.CacheEntry
		return manager.db.Where("key = ?", key).First(&entry).Error == nil &&
			entry.ExpiresAt.After(refreshStarted)
	})

	fresh, err := manager.Get(context.Background(), key, "pypi", 2*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "", errors.New("fresh cache unexpectedly contacted upstream")
		})
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Reader.Close()
	freshBody, err := io.ReadAll(fresh.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(freshBody) != payload {
		t.Fatalf("fresh body = %q, want cached %q", freshBody, payload)
	}
	if fresh.ContentType != "application/octet-stream" {
		t.Fatalf("content type = %q, want cached representation", fresh.ContentType)
	}
	if got := fresh.Headers.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q for uncompressed cached body", got)
	}
	if got := fresh.Headers.Get("ETag"); got != `"old-etag"` {
		t.Fatalf("ETag = %q, want cached representation validator", got)
	}
	if got := fresh.Headers.Get("X-Repo-Commit"); got != "old-commit" {
		t.Fatalf("X-Repo-Commit = %q, want cached representation metadata", got)
	}
}

func TestManager_BackgroundRefreshPersistsResponseMetadata(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	const key = "pypi/files/stale-metadata.whl"
	storage.data[key] = []byte("old")
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "pypi", CacheKind: db.CacheKindArtifact,
		StoragePath: key, ContentType: "application/octet-stream",
		ETag:            `"old"`,
		ResponseHeaders: `{"Etag":["\"old\""]}`,
		ExpiresAt:       time.Now().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	result, err := manager.Get(context.Background(), key, "pypi", 2*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			body := WithResponseMetadata(io.NopCloser(strings.NewReader("new")), http.Header{
				"ETag":          {`"new"`},
				"X-Repo-Commit": {"new-commit"},
			})
			return body, "application/octet-stream", 3, "upstream", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if result.Headers.Get("ETag") != `"old"` {
		t.Fatalf("stale response ETag = %q", result.Headers.Get("ETag"))
	}
	_ = result.Reader.Close()

	deadline := time.Now().Add(time.Second)
	for {
		var entry db.CacheEntry
		if err := database.Where("key = ?", key).First(&entry).Error; err != nil {
			t.Fatal(err)
		}
		if entry.ETag == `"new"` && strings.Contains(entry.ResponseHeaders, "new-commit") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("background response metadata was not persisted: %+v", entry)
		}
		time.Sleep(5 * time.Millisecond)
	}

	hit, err := manager.Get(context.Background(), key, "pypi", 2*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			t.Fatal("fresh hit contacted upstream")
			return nil, "", 0, "", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	defer hit.Reader.Close()
	if hit.Headers.Get("ETag") != `"new"` || hit.Headers.Get("X-Repo-Commit") != "new-commit" {
		t.Fatalf("refreshed hit headers = %#v", hit.Headers)
	}
}

func TestManager_PassthroughExposesSafeResponseMetadata(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	result, err := manager.fetchPassthrough(context.Background(),
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			body := WithResponseMetadata(io.NopCloser(strings.NewReader("body")), http.Header{
				"ETag":          {`"passthrough"`},
				"Set-Cookie":    {"secret=1"},
				"Authorization": {"Bearer secret"},
			})
			return body, "application/octet-stream", 4, "upstream", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Reader.Close()
	if result.Headers.Get("ETag") != `"passthrough"` {
		t.Fatalf("passthrough headers = %#v", result.Headers)
	}
	if result.Headers.Get("Set-Cookie") != "" || result.Headers.Get("Authorization") != "" {
		t.Fatalf("passthrough exposed unsafe headers: %#v", result.Headers)
	}
}

func TestManager_FetchTimeoutHintAppliesToMissAndPassthrough(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	blockingFetch := func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		<-ctx.Done()
		return nil, "", 0, "upstream", ctx.Err()
	}
	for name, call := range map[string]func(context.Context) error{
		"miss": func(ctx context.Context) error {
			_, err := manager.Get(ctx, "pypi/files/timeout.whl", "pypi", time.Hour, blockingFetch)
			return err
		},
		"passthrough": func(ctx context.Context) error {
			_, err := manager.fetchPassthrough(ctx, blockingFetch)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			err := call(WithFetchTimeout(context.Background(), 20*time.Millisecond))
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error = %v, want context deadline exceeded", err)
			}
			if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
				t.Fatalf("fetch timeout took %v", elapsed)
			}
		})
	}
}

func TestManager_FetchIdleTimeoutHintAppliesToPassthrough(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })
	body := newCloseBlockingBody()

	callerCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	ctx := WithFetchTimeout(callerCtx, 0)
	ctx = WithFetchIdleTimeout(ctx, 20*time.Millisecond)
	result, err := manager.fetchPassthrough(ctx,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return body, "application/octet-stream", -1, "upstream", nil
		})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, readErr := io.Copy(io.Discard, result.Reader)
	closeErr := result.Reader.Close()
	if err := errors.Join(readErr, closeErr); !errors.Is(err, ErrFetchIdleTimeout) {
		t.Fatalf("passthrough error = %v, want ErrFetchIdleTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("passthrough lost 20ms idle timeout hint; elapsed %v", elapsed)
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("passthrough idle timeout did not close upstream body")
	}
}

func TestManager_PrefetchPreservesFetchTimeoutHint(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	callerCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := manager.Prefetch(WithFetchTimeout(callerCtx, 20*time.Millisecond),
		"pypi/files/prefetch-timeout.whl", "pypi", time.Hour,
		func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
			<-ctx.Done()
			return nil, "", 0, "upstream", ctx.Err()
		})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("prefetch error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("prefetch lost 20ms fetch timeout hint; elapsed %v", elapsed)
	}
}

func TestManager_PrefetchPreservesFetchIdleTimeoutHint(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })
	body := newCloseBlockingBody()

	callerCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	ctx := WithFetchTimeout(callerCtx, 0)
	ctx = WithFetchIdleTimeout(ctx, 20*time.Millisecond)
	start := time.Now()
	err := manager.Prefetch(ctx,
		"huggingface/models/acme/large/resolve/0123456789abcdef/prefetch.bin",
		"huggingface",
		24*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return body, "application/octet-stream", -1, "upstream", nil
		})
	if !errors.Is(err, ErrFetchIdleTimeout) {
		t.Fatalf("prefetch error = %v, want ErrFetchIdleTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("prefetch lost 20ms idle timeout hint; elapsed %v", elapsed)
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("Prefetch idle timeout did not close upstream body")
	}
	waitForNoInflight(t, manager)
}

func TestManager_DefaultFetchTimeoutRemainsTenMinutes(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	result, err := manager.Get(context.Background(), "pypi/files/default-timeout.whl", "pypi", time.Hour,
		func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("default fetch context has no deadline")
			}
			remaining := time.Until(deadline)
			if remaining < 9*time.Minute || remaining > 10*time.Minute {
				t.Fatalf("default fetch deadline remaining = %v", remaining)
			}
			return io.NopCloser(strings.NewReader("body")), "application/octet-stream", 4, "upstream", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, result.Reader)
	_ = result.Reader.Close()
	waitForNoInflight(t, manager)
}

func TestManager_ZeroFetchTimeoutAddsNoDeadline(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	result, err := manager.Get(WithFetchTimeout(context.Background(), 0),
		"pypi/files/unbounded.whl", "pypi", time.Hour,
		func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
			if deadline, ok := ctx.Deadline(); ok {
				t.Fatalf("zero fetch timeout added deadline %v", deadline)
			}
			return io.NopCloser(strings.NewReader("body")), "application/octet-stream", 4, "upstream", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, result.Reader)
	_ = result.Reader.Close()
	waitForNoInflight(t, manager)
}

func TestManager_BackgroundRefreshInheritsFetchTimeoutHint(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	const key = "pypi/files/stale.whl"
	storage.data[key] = []byte("cached")
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "pypi", CacheKind: db.CacheKindArtifact,
		StoragePath: key, ContentType: "application/octet-stream",
		ExpiresAt: time.Now().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	refreshEnded := make(chan error, 1)
	ctx := WithFetchTimeout(context.Background(), 20*time.Millisecond)
	result, err := manager.Get(ctx, key, "pypi", 2*time.Hour,
		func(fetchCtx context.Context) (io.ReadCloser, string, int64, string, error) {
			<-fetchCtx.Done()
			refreshEnded <- fetchCtx.Err()
			return nil, "", 0, "upstream", fetchCtx.Err()
		})
	if err != nil {
		t.Fatal(err)
	}
	_ = result.Reader.Close()

	select {
	case err := <-refreshEnded:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("background refresh error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("background refresh did not inherit short fetch timeout")
	}
}

func TestManager_BackgroundRefreshInheritsFetchIdleTimeoutHint(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	const key = "huggingface/models/acme/large/resolve/0123456789abcdef/stale.bin"
	storage.data[key] = []byte("cached")
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "huggingface", CacheKind: db.CacheKindArtifact,
		StoragePath: key, ContentType: "application/octet-stream",
		ExpiresAt: time.Now().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })
	body := newCloseBlockingBody()

	ctx := WithFetchTimeout(context.Background(), 0)
	ctx = WithFetchIdleTimeout(ctx, 20*time.Millisecond)
	result, err := manager.Get(ctx, key, "huggingface", 24*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return body, "application/octet-stream", -1, "upstream", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	_ = result.Reader.Close()

	select {
	case <-body.started:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not start reading the upstream body")
	}
	select {
	case <-body.closed:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("background refresh lost the rolling idle timeout hint")
	}
}

func assertSafeRepresentationHeaders(t *testing.T, headers http.Header) {
	t.Helper()
	for key, want := range map[string]string{
		"Accept-Ranges": "bytes",
		"ETag":          `"model-v1"`,
		"Last-Modified": "Mon, 27 Jul 2026 01:02:03 GMT",
		"X-Linked-Etag": `"blob-v1"`,
		"X-Linked-Size": "13",
		"X-Repo-Commit": "0123456789abcdef",
	} {
		if got := headers.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	for _, key := range []string{"Location", "Authorization", "Set-Cookie", "Connection", "X-Not-Allowlisted"} {
		if got := headers.Values(key); len(got) != 0 {
			t.Errorf("forbidden %s persisted: %q", key, got)
		}
	}
}

func TestWithResponseMetadataRejectsConnectionNominatedHeaders(t *testing.T) {
	body := WithResponseMetadata(io.NopCloser(strings.NewReader("body")), http.Header{
		"Connection": {"ETag"},
		"ETag":       {`"must-not-survive"`},
	})
	headers := responseMetadataFrom(body)
	if headers.Get("ETag") != "" {
		t.Fatalf("connection-nominated ETag survived sanitization: %#v", headers)
	}
	_ = body.Close()
}
