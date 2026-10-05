package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
)

type runtimeCacheBody struct {
	Cache struct {
		LogicalBytes int64  `json:"logical_bytes"`
		Entries      *int64 `json:"entries"`
		Packages     *int64 `json:"packages"`
	} `json:"cache"`
}

func newRuntimeTestHandler(t *testing.T) (*RuntimeHandler, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "runtime.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open runtime db: %v", err)
	}
	if err := database.AutoMigrate(&db.CacheEntry{}); err != nil {
		t.Fatalf("migrate runtime db: %v", err)
	}
	return NewRuntimeHandler(database, nil, "local", "./data/cache", 20), database
}

func readRuntimeCache(t *testing.T, handler *RuntimeHandler) runtimeCacheBody {
	t.Helper()
	router := gin.New()
	router.GET("/runtime", handler.Get)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/runtime", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("runtime status = %d, want 200", recorder.Code)
	}
	var body runtimeCacheBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode runtime: %v", err)
	}
	return body
}

func requireCount(t *testing.T, label string, got *int64, want int64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s missing from cache payload, want %d", label, want)
	}
	if *got != want {
		t.Fatalf("%s = %d, want %d", label, *got, want)
	}
}

func TestRuntimeReportsCacheInventory(t *testing.T) {
	handler, database := newRuntimeTestHandler(t)
	handler.now = func() time.Time { return time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC) }

	entries := []db.CacheEntry{
		{Key: "npm/lodash/-/lodash-4.17.21.tgz", AdapterType: "npm", CacheKind: "artifact", PackageName: "lodash", Size: 100},
		{Key: "npm/lodash", AdapterType: "npm", CacheKind: "metadata", PackageName: "lodash", Size: 10},
		{Key: "npm/react/-/react-18.2.0.tgz", AdapterType: "npm", CacheKind: "artifact", PackageName: "react", Size: 200},
		{Key: "pypi/lodash/1.0.0", AdapterType: "pypi", CacheKind: "artifact", PackageName: "lodash", Size: 300},
		{Key: "npm/-/index", AdapterType: "npm", CacheKind: "metadata", PackageName: "", Size: 7},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatalf("seed cache entries: %v", err)
	}

	body := readRuntimeCache(t, handler)
	// The same package's metadata and artifact rows are one package; the
	// identical name under another ecosystem is a different package.
	requireCount(t, "packages", body.Cache.Packages, 3)
	// Every row is a cache object, including metadata with no package name.
	requireCount(t, "entries", body.Cache.Entries, 5)
	if body.Cache.LogicalBytes != 617 {
		t.Fatalf("logical_bytes = %d, want 617", body.Cache.LogicalBytes)
	}
}

func TestRuntimeCacheInventoryIsMemoizedBetweenPolls(t *testing.T) {
	handler, database := newRuntimeTestHandler(t)
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	handler.now = func() time.Time { return now }

	if err := database.Create(&db.CacheEntry{
		Key: "npm/a", AdapterType: "npm", PackageName: "a", Size: 1,
	}).Error; err != nil {
		t.Fatalf("seed first entry: %v", err)
	}
	requireCount(t, "entries", readRuntimeCache(t, handler).Cache.Entries, 1)

	// A new arrival inside the memo window must not be recounted on every poll
	// from every open dashboard tab.
	if err := database.Create(&db.CacheEntry{
		Key: "npm/b", AdapterType: "npm", PackageName: "b", Size: 1,
	}).Error; err != nil {
		t.Fatalf("seed second entry: %v", err)
	}
	now = now.Add(cacheInventoryTTL - time.Second)
	requireCount(t, "entries within TTL", readRuntimeCache(t, handler).Cache.Entries, 1)

	// Past the TTL the next poll picks the new arrival up.
	now = now.Add(2 * time.Second)
	requireCount(t, "entries after TTL", readRuntimeCache(t, handler).Cache.Entries, 2)
}

func TestRuntimeCacheInventoryKeepsLastSampleWhenRefreshFails(t *testing.T) {
	handler, database := newRuntimeTestHandler(t)
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	handler.now = func() time.Time { return now }

	if err := database.Create(&db.CacheEntry{
		Key: "npm/a", AdapterType: "npm", PackageName: "a", Size: 4096,
	}).Error; err != nil {
		t.Fatalf("seed cache entry: %v", err)
	}
	good := readRuntimeCache(t, handler)

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("unwrap sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}
	now = now.Add(cacheInventoryTTL + time.Second)

	after := readRuntimeCache(t, handler)
	if after.Cache.Entries == nil || *after.Cache.Entries != *good.Cache.Entries {
		t.Fatalf("failed refresh changed entries: got %v, want %v", after.Cache.Entries, good.Cache.Entries)
	}
	if after.Cache.Packages == nil || *after.Cache.Packages != *good.Cache.Packages {
		t.Fatalf("failed refresh changed packages: got %v, want %v", after.Cache.Packages, good.Cache.Packages)
	}
	if after.Cache.LogicalBytes != good.Cache.LogicalBytes {
		t.Fatalf("failed refresh changed logical_bytes: got %d, want %d", after.Cache.LogicalBytes, good.Cache.LogicalBytes)
	}
}

func TestRuntimeCacheInventoryIsOmittedWithoutASuccessfulSample(t *testing.T) {
	handler, database := newRuntimeTestHandler(t)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("unwrap sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	body := readRuntimeCache(t, handler)
	// An unreadable inventory is unknown, not an empty cache.
	if body.Cache.Entries != nil || body.Cache.Packages != nil {
		t.Fatalf("reported inventory without a successful sample: %+v", body.Cache)
	}
}
