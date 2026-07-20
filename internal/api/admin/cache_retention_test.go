package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/db"
)

type adminRetentionTestStorage struct {
	cache.Storage
	deleteErrors map[string]error
}

func (storage *adminRetentionTestStorage) Delete(ctx context.Context, key string) error {
	if err := storage.deleteErrors[key]; err != nil {
		return err
	}
	return storage.Storage.Delete(ctx, key)
}

type cacheMutationTestResponse struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	Deleted         int    `json:"deleted"`
	Failed          int    `json:"failed"`
	ReclaimedBytes  int64  `json:"reclaimed_bytes"`
	Examined        int    `json:"examined"`
	ExpiredRemoved  int    `json:"expired_removed"`
	LRURemoved      int    `json:"lru_removed"`
	UsageBefore     int64  `json:"usage_before"`
	UsageAfter      int64  `json:"usage_after"`
	ObjectRemoved   bool   `json:"object_removed"`
	MetadataRemoved bool   `json:"metadata_removed"`
}

func newAdminRetentionTestHandler(
	t *testing.T,
	deleteErrors map[string]error,
) (*gorm.DB, cache.Storage, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "cache-retention.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CacheEntry{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close cache retention test DB: %v", err)
		}
	})

	localStorage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	storage := &adminRetentionTestStorage{Storage: localStorage, deleteErrors: deleteErrors}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})
	retention, err := cache.NewRetention(manager, cache.RetentionPolicy{
		MaxBytes: 1024, ThresholdPercent: 90, TargetPercent: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewCacheHandler(database, retention, 1)
	router := gin.New()
	router.DELETE("/cache/:id", handler.Delete)
	router.POST("/cache/cleanup", handler.Cleanup)
	return database, storage, router
}

func putAdminRetentionTestObjects(t *testing.T, storage cache.Storage, entries ...db.CacheEntry) {
	t.Helper()
	for _, entry := range entries {
		body := strings.Repeat("x", int(entry.Size))
		if err := storage.Put(context.Background(), entry.StoragePath, strings.NewReader(body), entry.Size, "application/octet-stream"); err != nil {
			t.Fatalf("put cache object %q: %v", entry.StoragePath, err)
		}
	}
}

func performCacheMutationRequest(
	t *testing.T,
	router http.Handler,
	method string,
	path string,
) (*httptest.ResponseRecorder, cacheMutationTestResponse) {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	var response cacheMutationTestResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return recorder, response
}

func TestCacheDeleteUsesRetentionAndReturnsHonestFailures(t *testing.T) {
	const sensitiveFailure = "s3 credentials rejected at private-bucket/cache-key"
	database, storage, router := newAdminRetentionTestHandler(t, map[string]error{
		"failed-object": errors.New(sensitiveFailure),
	})
	now := time.Now().UTC()
	entries := []db.CacheEntry{
		{Key: "removed", StoragePath: "removed-object", Size: 128, ExpiresAt: now.Add(time.Hour), LastAccessed: now},
		{Key: "retained", StoragePath: "failed-object", Size: 256, ExpiresAt: now.Add(time.Hour), LastAccessed: now},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}
	putAdminRetentionTestObjects(t, storage, entries...)

	recorder, response := performCacheMutationRequest(t, router, http.MethodDelete, "/cache/"+formatUint(entries[0].ID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("successful delete status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if response.Message != "deleted" || response.Deleted != 1 || response.ReclaimedBytes != 128 {
		t.Fatalf("successful delete response = %+v", response)
	}

	recorder, response = performCacheMutationRequest(t, router, http.MethodDelete, "/cache/"+formatUint(entries[1].ID))
	if recorder.Code != http.StatusInternalServerError || response.Code != "CACHE_REMOVE_INCOMPLETE" {
		t.Fatalf("failed delete status = %d, response = %+v", recorder.Code, response)
	}
	if response.Deleted != 0 || response.Failed != 1 || response.ReclaimedBytes != 0 || response.ObjectRemoved || response.MetadataRemoved {
		t.Fatalf("failed delete report = %+v", response)
	}
	if strings.Contains(recorder.Body.String(), sensitiveFailure) {
		t.Fatalf("failed delete leaked storage error: %s", recorder.Body.String())
	}
	var retained int64
	if err := database.Model(&db.CacheEntry{}).Where("id = ?", entries[1].ID).Count(&retained).Error; err != nil {
		t.Fatal(err)
	}
	if retained != 1 {
		t.Fatalf("failed delete retained rows = %d, want 1", retained)
	}
}

func TestCacheDeleteMapsInvalidAndMissingIDs(t *testing.T) {
	_, _, router := newAdminRetentionTestHandler(t, nil)

	for _, test := range []struct {
		path string
		want int
		code string
	}{
		{path: "/cache/not-a-number", want: http.StatusBadRequest, code: "BAD_REQUEST"},
		{path: "/cache/0", want: http.StatusBadRequest, code: "BAD_REQUEST"},
		{path: "/cache/999", want: http.StatusNotFound, code: "NOT_FOUND"},
	} {
		recorder, response := performCacheMutationRequest(t, router, http.MethodDelete, test.path)
		if recorder.Code != test.want || response.Code != test.code {
			t.Errorf("%s status = %d code = %q, want %d %q", test.path, recorder.Code, response.Code, test.want, test.code)
		}
	}
}

func TestCacheCleanupReturnsPartialReportWithoutLeakingErrors(t *testing.T) {
	const sensitiveFailure = "local path /secret/cache cannot be removed"
	database, storage, router := newAdminRetentionTestHandler(t, map[string]error{
		"failed-expired": errors.New(sensitiveFailure),
	})
	now := time.Now().UTC()
	entries := []db.CacheEntry{
		{Key: "expired-removed", StoragePath: "removed-expired", Size: 100, ExpiresAt: now.Add(-2 * time.Hour), LastAccessed: now.Add(-2 * time.Hour)},
		{Key: "expired-retained", StoragePath: "failed-expired", Size: 200, ExpiresAt: now.Add(-time.Hour), LastAccessed: now.Add(-time.Hour)},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}
	putAdminRetentionTestObjects(t, storage, entries...)

	recorder, response := performCacheMutationRequest(t, router, http.MethodPost, "/cache/cleanup")
	if recorder.Code != http.StatusInternalServerError || response.Code != "CACHE_CLEANUP_PARTIAL" {
		t.Fatalf("partial cleanup status = %d, response = %+v", recorder.Code, response)
	}
	if response.Deleted != 1 || response.Failed != 1 || response.ReclaimedBytes != 100 {
		t.Fatalf("partial cleanup response = %+v", response)
	}
	if strings.Contains(recorder.Body.String(), sensitiveFailure) {
		t.Fatalf("partial cleanup leaked storage error: %s", recorder.Body.String())
	}
	var remaining []db.CacheEntry
	if err := database.Order("id ASC").Find(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != entries[1].ID {
		t.Fatalf("remaining entries = %+v", remaining)
	}
}

func TestCacheCleanupPreservesLegacyFieldsAndAddsReport(t *testing.T) {
	database, storage, router := newAdminRetentionTestHandler(t, nil)
	now := time.Now().UTC()
	entry := db.CacheEntry{
		Key: "expired", StoragePath: "expired-object", Size: 96,
		ExpiresAt: now.Add(-time.Hour), LastAccessed: now.Add(-time.Hour),
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	putAdminRetentionTestObjects(t, storage, entry)

	recorder, response := performCacheMutationRequest(t, router, http.MethodPost, "/cache/cleanup")
	if recorder.Code != http.StatusOK {
		t.Fatalf("cleanup status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if response.Message != "cleanup completed" || response.Deleted != 1 || response.Failed != 0 {
		t.Fatalf("cleanup legacy response = %+v", response)
	}
	if response.ReclaimedBytes != 96 || response.Examined != 1 || response.ExpiredRemoved != 1 || response.LRURemoved != 0 {
		t.Fatalf("cleanup report = %+v", response)
	}
}

func TestCacheCleanupReportsUntrackedStorageAboveTarget(t *testing.T) {
	_, storage, router := newAdminRetentionTestHandler(t, nil)
	body := strings.Repeat("x", 1024)
	if err := storage.Put(
		context.Background(),
		"legacy-orphan",
		strings.NewReader(body),
		int64(len(body)),
		"application/octet-stream",
	); err != nil {
		t.Fatal(err)
	}

	recorder, response := performCacheMutationRequest(t, router, http.MethodPost, "/cache/cleanup")
	if recorder.Code != http.StatusInternalServerError || response.Code != "CACHE_RECLAIM_TARGET_NOT_REACHED" {
		t.Fatalf("cleanup status = %d, response = %+v", recorder.Code, response)
	}
	if response.Deleted != 0 || response.Failed != 0 || response.UsageBefore != 1024 || response.UsageAfter != 1024 {
		t.Fatalf("cleanup report = %+v", response)
	}
	if !strings.Contains(response.Message, "storage remains above target") {
		t.Fatalf("cleanup message = %q", response.Message)
	}
}

func formatUint(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
