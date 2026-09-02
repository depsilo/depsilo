package cache_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"depsilo/internal/adapter/huggingface"
	"depsilo/internal/cache"
	"depsilo/internal/db"
)

func TestSchemaV3RetiresLegacyHuggingFaceObjectUntilStartupRetentionOwnsCleanup(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "depsilo.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("initialize current schema: %v", err)
	}
	if err := database.Exec("DELETE FROM schema_migrations WHERE version = ?", 3).Error; err != nil {
		t.Fatalf("rewind migration ledger to schema v2: %v", err)
	}

	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	const legacyKey = "huggingface/openai/whisper-tiny/resolve/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/config.json"
	payload := []byte("legacy Hugging Face object")
	if err := storage.Put(ctx, legacyKey, bytes.NewReader(payload), int64(len(payload)), "application/octet-stream"); err != nil {
		t.Fatalf("store v2 object: %v", err)
	}
	legacy := db.CacheEntry{
		Key:          legacyKey,
		AdapterType:  "huggingface",
		CacheKind:    db.CacheKindArtifact,
		PackageName:  "openai/whisper-tiny",
		StoragePath:  legacyKey,
		Size:         int64(len(payload)),
		ContentType:  "application/octet-stream",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		LastAccessed: time.Now().UTC(),
	}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatalf("seed v2 cache row: %v", err)
	}
	const unrelatedKey = "pypi/files/expired-but-unrelated.whl"
	unrelatedPayload := []byte("unrelated cache object")
	if err := storage.Put(ctx, unrelatedKey, bytes.NewReader(unrelatedPayload), int64(len(unrelatedPayload)), "application/octet-stream"); err != nil {
		t.Fatalf("store unrelated object: %v", err)
	}
	unrelated := db.CacheEntry{
		Key:          unrelatedKey,
		AdapterType:  "pypi",
		CacheKind:    db.CacheKindArtifact,
		PackageName:  "unrelated",
		StoragePath:  unrelatedKey,
		Size:         int64(len(unrelatedPayload)),
		ContentType:  "application/octet-stream",
		ExpiresAt:    time.Now().UTC().Add(-time.Hour),
		LastAccessed: time.Now().UTC(),
	}
	if err := database.Create(&unrelated).Error; err != nil {
		t.Fatalf("seed unrelated cache row: %v", err)
	}

	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("upgrade schema v2 to v3: %v", err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := manager.Close(closeCtx); err != nil {
			t.Errorf("close manager: %v", err)
		}
	})

	if _, err := manager.Head(ctx, legacyKey, "huggingface"); !errors.Is(err, cache.ErrCacheMiss) {
		t.Fatalf("legacy key remained readable: %v", err)
	}
	var retired db.CacheEntry
	if err := database.First(&retired, legacy.ID).Error; err != nil {
		t.Fatalf("read retired cache row: %v", err)
	}
	if retired.Key == legacyKey || !strings.HasPrefix(retired.Key, "huggingface/__retired-v3__/entry/") {
		t.Fatalf("retired key = %q", retired.Key)
	}
	if len(retired.Key) > 512 {
		t.Fatalf("retired key length = %d, exceeds cache key limit", len(retired.Key))
	}
	if parsed := huggingface.ParseRequestPath(strings.TrimPrefix(retired.Key, "huggingface/")); parsed.Kind != huggingface.PathUnknown || huggingface.CacheKey(parsed) != "" {
		t.Fatalf("retired key is reachable from a Hugging Face route: %q parsed as %+v", retired.Key, parsed)
	}
	if retired.AdapterType != "retired-v3" || retired.PackageName != "" {
		t.Fatalf("retired identity = (%q, %q), want (retired-v3, empty)", retired.AdapterType, retired.PackageName)
	}
	if !retired.ExpiresAt.Before(time.Now().UTC()) {
		t.Fatalf("retired expiry = %s, want past", retired.ExpiresAt)
	}
	if retired.StoragePath != legacy.StoragePath || retired.Size != legacy.Size {
		t.Fatalf("retired storage metadata = (%q, %d), want (%q, %d)", retired.StoragePath, retired.Size, legacy.StoragePath, legacy.Size)
	}
	if exists, err := storage.Exists(ctx, legacy.StoragePath); err != nil || !exists {
		t.Fatalf("retired object exists = (%v, %v), want (true, nil)", exists, err)
	}

	retention, err := cache.NewRetention(manager, cache.RetentionPolicy{MaxBytes: 1024, ThresholdPercent: 90, TargetPercent: 80})
	if err != nil {
		t.Fatal(err)
	}
	report, err := retention.ReclaimRetired(ctx, db.RetiredHuggingFaceAdapterType)
	if err != nil {
		t.Fatalf("startup retired-entry retention: %v", err)
	}
	if report.Removed != 1 {
		t.Fatalf("startup retired-entry retention report = %+v, want one removal", report)
	}
	if exists, err := storage.Exists(ctx, legacy.StoragePath); err != nil || exists {
		t.Fatalf("retired object after startup cleanup = (%v, %v), want (false, nil)", exists, err)
	}
	var remaining int64
	if err := database.Model(&db.CacheEntry{}).Where("id = ?", legacy.ID).Count(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("retired row count after startup cleanup = %d, want 0", remaining)
	}
	if err := database.First(&unrelated, unrelated.ID).Error; err != nil {
		t.Fatalf("startup cleanup removed unrelated cache row: %v", err)
	}
	if exists, err := storage.Exists(ctx, unrelated.StoragePath); err != nil || !exists {
		t.Fatalf("startup cleanup removed unrelated object: (%v, %v)", exists, err)
	}

	freshPayload := []byte("fresh Hugging Face object")
	if err := manager.Prefetch(ctx, legacyKey, "huggingface", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(bytes.NewReader(freshPayload)), "application/octet-stream", int64(len(freshPayload)), "test", nil
		}); err != nil {
		t.Fatalf("write fresh canonical cache entry: %v", err)
	}
	manualReport, err := retention.Reclaim(ctx, cache.ReclaimModeManual)
	if err != nil {
		t.Fatalf("normal manual retention after fresh write: %v", err)
	}
	if manualReport.ExpiredRemoved != 1 {
		t.Fatalf("normal manual retention report = %+v, want the unrelated expired entry removed", manualReport)
	}
	if err := database.Model(&db.CacheEntry{}).Where("id = ?", unrelated.ID).Count(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("normal manual retention retained %d unrelated expired rows", remaining)
	}
	reader, _, err := storage.Get(ctx, legacyKey)
	if err != nil {
		t.Fatalf("fresh object was removed by normal manual retention: %v", err)
	}
	freshBody, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read fresh object = (%v, %v)", readErr, closeErr)
	}
	if !bytes.Equal(freshBody, freshPayload) {
		t.Fatalf("fresh object = %q, want %q", freshBody, freshPayload)
	}
}
