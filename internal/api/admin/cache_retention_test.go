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
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
)

type adminRetentionTestStorage struct {
	cache.Storage
	deleteErrors map[string]error
	deleteCalls  atomic.Int32
	usageCalls   atomic.Int32
}

func (storage *adminRetentionTestStorage) Delete(ctx context.Context, key string) error {
	storage.deleteCalls.Add(1)
	if err := storage.deleteErrors[key]; err != nil {
		return err
	}
	return storage.Storage.Delete(ctx, key)
}

func (storage *adminRetentionTestStorage) TotalSize(ctx context.Context) (int64, error) {
	storage.usageCalls.Add(1)
	return storage.Storage.TotalSize(ctx)
}

type cacheMutationTestResponse struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	Outcome         string `json:"outcome"`
	Interrupted     bool   `json:"interrupted"`
	Deleted         int    `json:"deleted"`
	Failed          int    `json:"failed"`
	Skipped         int    `json:"skipped"`
	Planned         int    `json:"planned"`
	PlannedCount    int    `json:"planned_count"`
	PlannedBytes    int64  `json:"planned_bytes"`
	TotalCandidates int64  `json:"total_candidate_count"`
	NotAttempted    int    `json:"not_attempted"`
	Items           []struct {
		ID              uint   `json:"id"`
		Status          string `json:"status"`
		ObjectRemoved   bool   `json:"object_removed"`
		MetadataRemoved bool   `json:"metadata_removed"`
	} `json:"items"`
	ReclaimedBytes  int64 `json:"reclaimed_bytes"`
	Examined        int   `json:"examined"`
	ExpiredRemoved  int   `json:"expired_removed"`
	LRURemoved      int   `json:"lru_removed"`
	UsageBefore     int64 `json:"usage_before"`
	UsageAfter      int64 `json:"usage_after"`
	ObjectRemoved   bool  `json:"object_removed"`
	MetadataRemoved bool  `json:"metadata_removed"`
}

func newAdminRetentionTestHandler(
	t *testing.T,
	deleteErrors map[string]error,
) (*gorm.DB, cache.Storage, *gin.Engine) {
	database, storage, _, router := newAdminRetentionTestHandlerWithHandler(t, deleteErrors)
	return database, storage, router
}

func newAdminRetentionTestHandlerWithHandler(
	t *testing.T,
	deleteErrors map[string]error,
) (*gorm.DB, cache.Storage, *CacheHandler, *gin.Engine) {
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
	router.Use(func(c *gin.Context) {
		principal := middleware.Principal{ID: 1, CanWrite: true}
		if rawID, err := strconv.ParseUint(c.GetHeader("X-Test-Principal-ID"), 10, 32); err == nil && rawID > 0 {
			principal.ID = uint(rawID)
		}
		if c.GetHeader("X-Test-Readonly") == "1" {
			principal.CanWrite = false
		}
		c.Set(middleware.ContextKeyPrincipal, principal)
		c.Next()
	})
	router.DELETE("/cache/:id", handler.Delete)
	router.POST("/cache/cleanup", handler.Cleanup)
	router.GET("/cache/cleanup/preview", handler.PreviewCleanup)
	return database, storage, handler, router
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

func performCacheBodyRequest(
	t *testing.T,
	router http.Handler,
	body string,
) (*httptest.ResponseRecorder, cacheMutationTestResponse) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/cache/cleanup", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
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

func TestCacheCleanupRejectsMalformedPlanBodiesWithoutMutation(t *testing.T) {
	database, storage, router := newAdminRetentionTestHandler(t, nil)
	entry := db.CacheEntry{
		Key: "malformed-plan", StoragePath: "malformed-plan-object", Size: 16,
		ExpiresAt: time.Now().Add(-time.Hour), LastAccessed: time.Now().Add(-time.Hour),
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	putAdminRetentionTestObjects(t, storage, entry)

	for _, body := range []string{
		"{",
		`{"plan_id":123}`,
		`{}`,
		`{"plan_id":""}`,
		`{"plan_id":"missing","extra":true}`,
		`{"plan_id":null}`, `null`, `[]`, ` `,
		`{"Plan_ID":"missing"}`,
		`{"plan_id":"one","plan_id":"two"}`,
		`{"plan_id":"one"} {"plan_id":"two"}`,
		`{"plan_id":"` + strings.Repeat("x", 2048) + `"}`,
	} {
		recorder, response := performCacheBodyRequest(t, router, body)
		if recorder.Code != http.StatusBadRequest || response.Code != "BAD_REQUEST" {
			t.Fatalf("body %q status = %d response = %+v", body, recorder.Code, response)
		}
	}
	assertRetentionEntryExistsForAdminTest(t, database, entry.ID, true)
	tracked := storage.(*adminRetentionTestStorage)
	if tracked.deleteCalls.Load() != 0 || tracked.usageCalls.Load() != 0 {
		t.Fatalf("invalid requests touched storage: deletes=%d usage=%d", tracked.deleteCalls.Load(), tracked.usageCalls.Load())
	}
}

func TestCacheCleanupPlanExecutesOnlyThePreviewPage(t *testing.T) {
	database, storage, router := newAdminRetentionTestHandler(t, nil)
	now := time.Now().UTC()
	entries := make([]db.CacheEntry, 20)
	for i := range entries {
		entries[i] = db.CacheEntry{
			Key: formatUint(uint(i + 1)), StoragePath: "planned-" + formatUint(uint(i+1)), Size: 1,
			ExpiresAt: now.Add(-time.Hour), LastAccessed: now.Add(-time.Hour),
		}
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}
	putAdminRetentionTestObjects(t, storage, entries...)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/cache/cleanup/preview?page=1&page_size=8", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var preview struct {
		PlanID         string `json:"plan_id"`
		CandidateCount int64  `json:"candidate_count"`
		PlannedCount   int    `json:"planned_count"`
		PlannedBytes   int64  `json:"planned_bytes"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.PlanID == "" || preview.CandidateCount != 20 || preview.PlannedCount != 8 || preview.PlannedBytes != 8 {
		t.Fatalf("preview summary = %+v", preview)
	}

	recorder, response := performCacheBodyRequest(t, router, `{"plan_id":"`+preview.PlanID+`"}`)
	if recorder.Code != http.StatusOK || response.Outcome != "succeeded" || response.Deleted != 8 || response.PlannedCount != 8 || response.TotalCandidates != 20 {
		t.Fatalf("cleanup status = %d response = %+v", recorder.Code, response)
	}
	second, _ := performCacheBodyRequest(t, router, `{"plan_id":"`+preview.PlanID+`"}`)
	if second.Code != http.StatusConflict {
		t.Fatalf("duplicate cleanup status = %d body = %s", second.Code, second.Body.String())
	}
	assertAdminCacheEntryCount(t, database, 12)
}

func TestCacheCleanupPlanSkipsChangedObjectsWithoutDeleting(t *testing.T) {
	database, storage, router := newAdminRetentionTestHandler(t, nil)
	now := time.Now().UTC()
	entry := db.CacheEntry{Key: "changed", StoragePath: "changed-object", Size: 1, ExpiresAt: now.Add(-time.Hour), LastAccessed: now}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	putAdminRetentionTestObjects(t, storage, entry)
	preview := httptest.NewRecorder()
	router.ServeHTTP(preview, httptest.NewRequest(http.MethodGet, "/cache/cleanup/preview?page=1&page_size=8", nil))
	var plan struct {
		PlanID string `json:"plan_id"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &plan); err != nil || plan.PlanID == "" {
		t.Fatalf("preview = %d %s err=%v", preview.Code, preview.Body.String(), err)
	}
	if err := database.Model(&db.CacheEntry{}).Where("id = ?", entry.ID).Update("size", 2).Error; err != nil {
		t.Fatal(err)
	}
	tracked := storage.(*adminRetentionTestStorage)
	recorder, response := performCacheBodyRequest(t, router, `{"plan_id":"`+plan.PlanID+`"}`)
	if recorder.Code != http.StatusOK || response.Skipped != 1 || response.Deleted != 0 || tracked.deleteCalls.Load() != 0 {
		t.Fatalf("changed object response=%+v status=%d deletes=%d", response, recorder.Code, tracked.deleteCalls.Load())
	}
}

func TestCacheCleanupPlanRejectsOtherOwnersAndExpiredPlans(t *testing.T) {
	database, storage, handler, router := newAdminRetentionTestHandlerWithHandler(t, nil)
	entry := db.CacheEntry{
		Key: "owned-plan", StoragePath: "owned-plan-object", Size: 1,
		ExpiresAt: time.Now().Add(-time.Hour), LastAccessed: time.Now().Add(-time.Hour),
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	putAdminRetentionTestObjects(t, storage, entry)

	previewRecorder := httptest.NewRecorder()
	router.ServeHTTP(previewRecorder, httptest.NewRequest(http.MethodGet, "/cache/cleanup/preview?page=1&page_size=8", nil))
	var preview struct {
		PlanID string `json:"plan_id"`
	}
	if err := json.Unmarshal(previewRecorder.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if previewRecorder.Code != http.StatusOK || preview.PlanID == "" {
		t.Fatalf("preview status = %d body = %s", previewRecorder.Code, previewRecorder.Body.String())
	}

	otherOwner := httptest.NewRecorder()
	otherRequest := httptest.NewRequest(http.MethodPost, "/cache/cleanup", strings.NewReader(`{"plan_id":"`+preview.PlanID+`"}`))
	otherRequest.Header.Set("Content-Type", "application/json")
	otherRequest.Header.Set("X-Test-Principal-ID", "2")
	router.ServeHTTP(otherOwner, otherRequest)
	if otherOwner.Code != http.StatusConflict {
		t.Fatalf("other owner status = %d body = %s", otherOwner.Code, otherOwner.Body.String())
	}
	assertRetentionEntryExistsForAdminTest(t, database, entry.ID, true)

	handler.plansMu.Lock()
	plan := handler.plans[preview.PlanID]
	plan.expiresAt = time.Now().Add(-time.Second)
	handler.plans[preview.PlanID] = plan
	handler.plansMu.Unlock()
	expired := httptest.NewRecorder()
	expiredRequest := httptest.NewRequest(http.MethodPost, "/cache/cleanup", strings.NewReader(`{"plan_id":"`+preview.PlanID+`"}`))
	expiredRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(expired, expiredRequest)
	if expired.Code != http.StatusConflict {
		t.Fatalf("expired plan status = %d body = %s", expired.Code, expired.Body.String())
	}
	assertRetentionEntryExistsForAdminTest(t, database, entry.ID, true)
}

func TestCacheCleanupPlanReportsPartialAndTotalFailure(t *testing.T) {
	tests := []struct {
		name       string
		deleteKeys map[string]error
		wantResult string
		wantDelete int
		wantFailed int
	}{
		{name: "all failed", deleteKeys: map[string]error{"failed-object": errors.New("delete failed")}, wantResult: "failed", wantDelete: 0, wantFailed: 1},
		{name: "partial", deleteKeys: map[string]error{"partial-failed": errors.New("delete failed")}, wantResult: "partial", wantDelete: 1, wantFailed: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, storage, router := newAdminRetentionTestHandler(t, test.deleteKeys)
			now := time.Now().UTC()
			firstPath := "failed-object"
			if test.name == "partial" {
				firstPath = "success-object"
			}
			entries := []db.CacheEntry{{Key: "first", StoragePath: firstPath, Size: 1, ExpiresAt: now.Add(-time.Hour), LastAccessed: now}}
			if test.name == "partial" {
				entries = append(entries, db.CacheEntry{Key: "second", StoragePath: "partial-failed", Size: 1, ExpiresAt: now.Add(-time.Hour), LastAccessed: now})
			}
			if err := database.Create(&entries).Error; err != nil {
				t.Fatal(err)
			}
			putAdminRetentionTestObjects(t, storage, entries...)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/cache/cleanup/preview?page=1&page_size=8", nil)
			router.ServeHTTP(recorder, request)
			var preview struct {
				PlanID string `json:"plan_id"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &preview); err != nil {
				t.Fatal(err)
			}
			recorder, response := performCacheBodyRequest(t, router, `{"plan_id":"`+preview.PlanID+`"}`)
			if recorder.Code != http.StatusOK || response.Outcome != test.wantResult || response.Deleted != test.wantDelete || response.Failed != test.wantFailed {
				t.Fatalf("cleanup status = %d response = %+v", recorder.Code, response)
			}
		})
	}
}

func assertRetentionEntryExistsForAdminTest(t *testing.T, database *gorm.DB, id uint, want bool) {
	t.Helper()
	var count int64
	if err := database.Model(&db.CacheEntry{}).Where("id = ?", id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if (count == 1) != want {
		t.Fatalf("cache entry %d exists = %v, want %v", id, count == 1, want)
	}
}

func assertAdminCacheEntryCount(t *testing.T, database *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := database.Model(&db.CacheEntry{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("cache entry count = %d, want %d", count, want)
	}
}

func formatUint(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
