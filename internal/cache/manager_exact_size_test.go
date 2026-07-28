package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestManagerDeclaredSizeMismatchDoesNotCommitMiss(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		declared int64
	}{
		{name: "short", payload: "abc", declared: 4},
		{name: "overlong", payload: "abcd", declared: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openStreamTestDB(t)
			storage := newMemStorage()
			manager := NewManager(storage, database, NewEventBus(), time.Hour)
			t.Cleanup(func() { closeTestManager(t, manager) })

			const key = "pypi/files/declared-size.whl"
			ctx, tracker := WithTrackedForceRefresh(context.Background())
			result, err := manager.Get(
				ctx,
				key,
				"pypi",
				time.Hour,
				func(context.Context) (io.ReadCloser, string, int64, string, error) {
					return io.NopCloser(bytes.NewBufferString(test.payload)),
						"application/octet-stream",
						test.declared,
						"upstream",
						nil
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, result.Reader)
			_ = result.Reader.Close()

			if err := tracker.Wait(context.Background()); !errors.Is(err, ErrBodySizeMismatch) {
				t.Fatalf("persistence error = %v, want ErrBodySizeMismatch", err)
			}
			var rows int64
			if err := database.Model(&db.CacheEntry{}).
				Where("key = ?", key).
				Count(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if rows != 0 {
				t.Fatalf("declared-size mismatch committed %d metadata rows", rows)
			}
			if exists, err := storage.Exists(context.Background(), key); err != nil || exists {
				t.Fatalf("declared-size mismatch object = (exists=%v, err=%v)", exists, err)
			}
		})
	}
}

func TestManagerBackgroundSizeMismatchPreservesObjectAndMetadata(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		declared int64
	}{
		{name: "short", payload: "new", declared: 4},
		{name: "overlong", payload: "new!", declared: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openStreamTestDB(t)
			storage := newMemStorage()
			const key = "pypi/simple/declared-size/index.html"
			storage.data[key] = []byte("trusted-old")
			oldExpiry := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
			entry := db.CacheEntry{
				Key:             key,
				AdapterType:     "pypi",
				CacheKind:       db.CacheKindMetadata,
				StoragePath:     key,
				Size:            int64(len("trusted-old")),
				ContentType:     "text/plain",
				ETag:            `"trusted"`,
				ResponseHeaders: `{"Etag":["\"trusted\""]}`,
				ExpiresAt:       oldExpiry,
				LastAccessed:    oldExpiry,
			}
			if err := database.Create(&entry).Error; err != nil {
				t.Fatal(err)
			}

			manager := NewManager(storage, database, NewEventBus(), time.Hour)
			t.Cleanup(func() { closeTestManager(t, manager) })
			manager.backgroundRefresh(
				context.Background(),
				key,
				"pypi",
				time.Minute,
				func(context.Context) (io.ReadCloser, string, int64, string, error) {
					body := WithResponseMetadata(
						io.NopCloser(bytes.NewBufferString(test.payload)),
						http.Header{"ETag": {`"untrusted-new"`}},
					)
					return body, "application/octet-stream", test.declared, "upstream", nil
				},
			)

			reader, _, err := storage.Get(context.Background(), key)
			if err != nil {
				t.Fatal(err)
			}
			got, readErr := io.ReadAll(reader)
			_ = reader.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != "trusted-old" {
				t.Fatalf("cached body = %q, want trusted-old", got)
			}

			var after db.CacheEntry
			if err := database.Where("key = ?", key).First(&after).Error; err != nil {
				t.Fatal(err)
			}
			if after.Size != entry.Size ||
				after.ContentType != entry.ContentType ||
				after.ETag != entry.ETag ||
				after.ResponseHeaders != entry.ResponseHeaders ||
				!after.ExpiresAt.Equal(oldExpiry) {
				t.Fatalf("metadata changed after rejected refresh: before=%+v after=%+v", entry, after)
			}
		})
	}
}
