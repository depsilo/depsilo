package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestCoalescedFollowerRecordsHitAfterExistingRowRefresh(t *testing.T) {
	storage := newMutationTestStorage()
	manager, _, closeManager := newPackageInvalidationManager(t, storage)
	t.Cleanup(closeManager)

	const key = "pypi/simple/example/index.html"
	storage.seed(key, []byte("old"))
	existing := db.CacheEntry{
		Key:          key,
		AdapterType:  "pypi",
		CacheKind:    db.CacheKindMetadata,
		PackageName:  "example",
		StoragePath:  key,
		Size:         3,
		ContentType:  "text/html",
		ExpiresAt:    time.Now().UTC().Add(-time.Minute),
		LastAccessed: time.Now().UTC().Add(-time.Minute),
	}
	if err := manager.db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	if existing.ID == 0 {
		t.Fatal("existing cache row has no identity")
	}

	var fetchCalls atomic.Int64
	fetch := func(context.Context) (io.ReadCloser, string, int64, string, error) {
		fetchCalls.Add(1)
		body := []byte("new representation")
		return io.NopCloser(bytes.NewReader(body)),
			"text/html",
			int64(len(body)),
			"origin",
			nil
	}
	type outcome struct {
		hit  bool
		body string
		err  error
	}
	leaderReady := make(chan struct{})
	allowLeaderRead := make(chan struct{})
	leaderDone := make(chan outcome, 1)
	go func() {
		result, err := manager.Get(
			context.Background(),
			key,
			"pypi",
			5*time.Minute,
			fetch,
		)
		close(leaderReady)
		if err != nil {
			leaderDone <- outcome{err: err}
			return
		}
		<-allowLeaderRead
		body, readErr := io.ReadAll(result.Reader)
		closeErr := result.Reader.Close()
		leaderDone <- outcome{
			hit:  result.Hit,
			body: string(body),
			err:  errors.Join(readErr, closeErr),
		}
	}()
	select {
	case <-leaderReady:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for refresh leader")
	}

	followerCtx, tracker := WithTrackedForceRefresh(context.Background())
	followerDone := make(chan outcome, 1)
	go func() {
		result, err := manager.Get(
			followerCtx,
			key,
			"pypi",
			5*time.Minute,
			fetch,
		)
		if err != nil {
			followerDone <- outcome{err: err}
			return
		}
		body, readErr := io.ReadAll(result.Reader)
		closeErr := result.Reader.Close()
		followerDone <- outcome{
			hit:  result.Hit,
			body: string(body),
			err:  errors.Join(readErr, closeErr),
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for !tracker.Used() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !tracker.Used() {
		t.Fatal("second request did not join the inflight refresh")
	}
	close(allowLeaderRead)

	var leader, follower outcome
	for name, target := range map[string]*outcome{
		"leader":   &leader,
		"follower": &follower,
	} {
		var source <-chan outcome
		if name == "leader" {
			source = leaderDone
		} else {
			source = followerDone
		}
		select {
		case *target = <-source:
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for %s", name)
		}
		if target.err != nil {
			t.Fatalf("%s error: %v", name, target.err)
		}
		if target.body != "new representation" {
			t.Fatalf("%s body = %q", name, target.body)
		}
	}
	if leader.hit {
		t.Fatal("refresh leader was reported as a cache hit")
	}
	if !follower.hit {
		t.Fatal("coalesced follower was not reported as a cache hit")
	}
	if fetchCalls.Load() != 1 {
		t.Fatalf("fetch calls = %d, want 1", fetchCalls.Load())
	}

	closeManager()
	var persisted db.CacheEntry
	if err := manager.db.Where("key = ?", key).First(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.ID != existing.ID {
		t.Fatalf(
			"refreshed cache row ID = %d, want existing ID %d",
			persisted.ID,
			existing.ID,
		)
	}
	if persisted.HitCount != 1 {
		t.Fatalf(
			"coalesced follower hit count = %d, want 1",
			persisted.HitCount,
		)
	}
}
