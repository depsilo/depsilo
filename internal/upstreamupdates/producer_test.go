package upstreamupdates

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

func producerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "upstream-updates.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestProducerCheckRecordsEveryMetadataOutcomeAndSkipsArtifacts(t *testing.T) {
	database := producerTestDB(t)
	entries := []db.CacheEntry{
		{Key: "pypi/simple/updated/index.html", AdapterType: "pypi", CacheKind: db.CacheKindMetadata, PackageName: "updated", ETag: `"updated-v1"`},
		{Key: "extra:mirror/simple/unchanged/index.html", AdapterType: "extra:mirror", CacheKind: db.CacheKindMetadata, PackageName: "unchanged", LastModified: "Fri, 17 Jul 2026 08:00:00 GMT"},
		{Key: "pypi/simple/error/index.html", AdapterType: "pypi", CacheKind: db.CacheKindMetadata, PackageName: "error", ETag: `"error-v1"`},
		{Key: "npm/unsupported/metadata.json", AdapterType: "npm", CacheKind: db.CacheKindMetadata, PackageName: "unsupported", ETag: `"unsupported-v1"`},
		{Key: "pypi/simple/unvalidated/index.html", AdapterType: "pypi", CacheKind: db.CacheKindMetadata, PackageName: "unvalidated"},
		{Key: "maven/org/example/app/1.0-SNAPSHOT/app-1.0-SNAPSHOT.jar", AdapterType: "maven", CacheKind: db.CacheKindMetadata, PackageName: "snapshot-artifact", ETag: `"snapshot-v1"`},
		{Key: "pypi/files/artifact.whl", AdapterType: "pypi", CacheKind: db.CacheKindArtifact, PackageName: "artifact", ETag: `"artifact-v1"`},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}

	var calls []string
	producer, err := New(database, time.Hour, func(_ context.Context, entry db.CacheEntry) (RefreshOutcome, error) {
		calls = append(calls, entry.PackageName)
		switch entry.PackageName {
		case "updated":
			return RefreshOutcome{Upstream: "primary", Changed: true}, nil
		case "unchanged":
			return RefreshOutcome{Upstream: "mirror", Detail: "validator matched"}, nil
		default:
			return RefreshOutcome{Upstream: "broken"}, errors.New("https://user:secret@example.test must never be persisted")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := producer.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"updated", "unchanged", "error"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("refresh calls = %v, want %v", calls, want)
	}

	var events []db.UpstreamUpdateEvent
	if err := database.Order("id ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Result != ResultUpdated || events[0].Upstream != "primary" {
		t.Fatalf("updated event = %+v", events[0])
	}
	if events[1].Result != ResultUnchanged || events[1].Detail != "validator matched" {
		t.Fatalf("unchanged event = %+v", events[1])
	}
	if events[2].Result != ResultError || events[2].Upstream != "broken" || events[2].Detail != "metadata refresh failed" {
		t.Fatalf("error event = %+v", events[2])
	}
	if strings.Contains(events[2].Detail, "secret") || strings.Contains(events[2].Detail, "example.test") {
		t.Fatalf("credential-bearing error persisted: %q", events[2].Detail)
	}
}

func TestProducerRunStopsWithItsContext(t *testing.T) {
	database := producerTestDB(t)
	entry := db.CacheEntry{Key: "pypi/simple/pillow/index.html", AdapterType: "pypi", CacheKind: db.CacheKindMetadata, PackageName: "pillow", ETag: `"pillow-v1"`}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	refreshed := make(chan struct{}, 1)
	producer, err := New(database, time.Millisecond, func(_ context.Context, _ db.CacheEntry) (RefreshOutcome, error) {
		select {
		case refreshed <- struct{}{}:
		default:
		}
		return RefreshOutcome{Upstream: "primary"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		producer.Run(ctx)
		close(done)
	}()
	select {
	case <-refreshed:
	case <-time.After(time.Second):
		t.Fatal("producer did not run a scheduled refresh")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("producer did not stop after cancellation")
	}
}

func TestProducerRunWithNilContextDoesNotStartUncancellableWork(t *testing.T) {
	database := producerTestDB(t)
	called := false
	producer, err := New(database, time.Millisecond, func(context.Context, db.CacheEntry) (RefreshOutcome, error) {
		called = true
		return RefreshOutcome{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		producer.Run(nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("nil context created an uncancellable producer")
	}
	if called {
		t.Fatal("nil-context producer invoked its refresher")
	}
}

func TestProducerRedactsCredentialURLsFromEventFields(t *testing.T) {
	database := producerTestDB(t)
	entry := db.CacheEntry{
		Key:         "pypi/simple/private/index.html",
		AdapterType: "pypi",
		CacheKind:   db.CacheKindMetadata,
		PackageName: "https://package-user:package-secret@packages.example/private?token=hidden",
		ETag:        `"private-v1"`,
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	producer, err := New(database, time.Hour, func(context.Context, db.CacheEntry) (RefreshOutcome, error) {
		return RefreshOutcome{
			Upstream: "https://upstream-user:upstream-secret@mirror.example/private?token=hidden",
			Changed:  true,
			Detail:   "https://detail-user:detail-secret@detail.example/private?token=hidden",
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := producer.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	var event db.UpstreamUpdateEvent
	if err := database.First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Package != "https://packages.example/***" ||
		event.Upstream != "https://mirror.example/***" ||
		event.Detail != "https://detail.example/***" {
		t.Fatalf("redacted event = %+v", event)
	}
	row := event.Ecosystem + event.Upstream + event.Package + event.Detail
	for _, secret := range []string{"package-user", "package-secret", "upstream-user", "upstream-secret", "detail-user", "detail-secret", "token", "hidden", "/private"} {
		if strings.Contains(row, secret) {
			t.Fatalf("event disclosed %q: %+v", secret, event)
		}
	}
}

func TestProducerRetriesTransientEventInsert(t *testing.T) {
	database := producerTestDB(t)
	entry := db.CacheEntry{
		Key: "pypi/simple/retry/index.html", AdapterType: "pypi",
		CacheKind: db.CacheKindMetadata, PackageName: "retry", ETag: `"retry-v1"`,
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	attempts := 0
	if err := database.Callback().Create().Before("gorm:create").Register("fail_upstream_event_twice", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "UpstreamUpdateEvent" {
			return
		}
		attempts++
		if attempts < eventWriteAttempts {
			tx.AddError(errors.New("temporary event insert failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	producer, err := New(database, time.Hour, func(context.Context, db.CacheEntry) (RefreshOutcome, error) {
		return RefreshOutcome{Upstream: "primary", Changed: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := producer.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts != eventWriteAttempts {
		t.Fatalf("event insert attempts = %d, want %d", attempts, eventWriteAttempts)
	}
	var count int64
	if err := database.Model(&db.UpstreamUpdateEvent{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("persisted events = %d, want 1", count)
	}
}

func TestProducerRequiresItsRealDependencies(t *testing.T) {
	database := producerTestDB(t)
	refresh := func(context.Context, db.CacheEntry) (RefreshOutcome, error) { return RefreshOutcome{}, nil }
	if _, err := New(nil, time.Second, refresh); err == nil {
		t.Fatal("nil database accepted")
	}
	if _, err := New(database, 0, refresh); err == nil {
		t.Fatal("non-positive interval accepted")
	}
	if _, err := New(database, time.Second, nil); err == nil {
		t.Fatal("nil refresher accepted")
	}
}
