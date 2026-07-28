package db

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

type cacheEntryBeforeResponseHeaders struct {
	ID          uint   `gorm:"primarykey"`
	Key         string `gorm:"uniqueIndex;size:512"`
	AdapterType string `gorm:"size:16;index"`
	StoragePath string `gorm:"size:512"`
	ContentType string `gorm:"size:128"`
	ETag        string `gorm:"column:etag;size:512"`
}

func TestCacheResponseHeadersRemainInternalToPersistence(t *testing.T) {
	encoded, err := json.Marshal(CacheEntry{
		Key:             "huggingface/acme/model/resolve/main/file",
		ResponseHeaders: `{"X-Linked-Etag":["internal-metadata"]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "response_headers") ||
		strings.Contains(string(encoded), "internal-metadata") {
		t.Fatalf("CacheEntry JSON exposed persisted response metadata: %s", encoded)
	}
}

func (cacheEntryBeforeResponseHeaders) TableName() string { return "cache_entries" }

func TestAutoMigrateAddsCacheResponseHeadersWithoutLosingRows(t *testing.T) {
	database, err := Open("sqlite", filepath.Join(t.TempDir(), "depsilo.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&cacheEntryBeforeResponseHeaders{}); err != nil {
		t.Fatal(err)
	}
	legacy := cacheEntryBeforeResponseHeaders{
		Key:         "pypi/files/example.whl",
		AdapterType: "pypi",
		StoragePath: "pypi/files/example.whl",
		ContentType: "application/octet-stream",
		ETag:        `"legacy"`,
	}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	if !database.Migrator().HasColumn(&CacheEntry{}, "response_headers") {
		t.Fatal("cache_entries.response_headers was not added")
	}
	var migrated CacheEntry
	if err := database.Where("id = ?", legacy.ID).First(&migrated).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.Key != legacy.Key || migrated.ETag != legacy.ETag {
		t.Fatalf("legacy row changed during migration: %+v", migrated)
	}
	if migrated.ResponseHeaders != "" {
		t.Fatalf("legacy response_headers = %q, want empty", migrated.ResponseHeaders)
	}
}
