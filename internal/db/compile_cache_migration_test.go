package db

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"gorm.io/gorm"
)

const (
	legacyCompileCacheIdentityIndex = "idx_compile_cache_namespace_key"
	wantCompileCacheIdentityIndex   = "idx_compile_cache_protocol_namespace_key"
)

type legacyCompileCacheEntry struct {
	ID           uint      `gorm:"primarykey"`
	Namespace    string    `gorm:"size:64;not null;uniqueIndex:idx_compile_cache_namespace_key,priority:1;index"`
	Key          string    `gorm:"size:40;not null;uniqueIndex:idx_compile_cache_namespace_key,priority:2"`
	StoragePath  string    `gorm:"size:512;not null;uniqueIndex"`
	Size         int64     `gorm:"not null"`
	Checksum     string    `gorm:"size:64"`
	HitCount     int64     `gorm:"not null;default:0"`
	LastAccessed time.Time `gorm:"index;not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (legacyCompileCacheEntry) TableName() string { return "compile_cache_entries" }

func TestAutoMigrateUpgradesLegacyCompileCacheIdentity(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&legacyCompileCacheEntry{}); err != nil {
		t.Fatalf("create legacy compiler-cache schema: %v", err)
	}
	const legacyStoragePath = "v1/team-a/objects/ab/cdef/legacy-generation"
	legacy := legacyCompileCacheEntry{
		ID: 17, Namespace: "team-a", Key: "0123456789abcdef0123456789abcdef01234567",
		StoragePath: legacyStoragePath, Size: 123, LastAccessed: time.Now().UTC(),
	}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy compiler-cache entry: %v", err)
	}
	if !database.Migrator().HasIndex(&legacyCompileCacheEntry{}, legacyCompileCacheIdentityIndex) {
		t.Fatalf("legacy schema is missing %s", legacyCompileCacheIdentityIndex)
	}

	if err := AutoMigrate(database); err != nil {
		t.Fatalf("AutoMigrate legacy database: %v", err)
	}
	assertCompileCacheIdentityIndex(t, database)

	var migrated CompileCacheEntry
	if err := database.First(&migrated, 17).Error; err != nil {
		t.Fatalf("read migrated compiler-cache entry: %v", err)
	}
	if migrated.Protocol != CompileCacheProtocolCCache {
		t.Errorf("migrated protocol = %q, want %q", migrated.Protocol, CompileCacheProtocolCCache)
	}
	if migrated.StoragePath != legacyStoragePath {
		t.Errorf("migrated storage_path = %q, want unchanged %q", migrated.StoragePath, legacyStoragePath)
	}

	sccache := CompileCacheEntry{
		Protocol: "sccache", Namespace: migrated.Namespace, Key: migrated.Key,
		StoragePath: "v2/sccache/team-a/objects/duplicate-key", Size: 456,
		LastAccessed: time.Now().UTC(),
	}
	if err := database.Create(&sccache).Error; err != nil {
		t.Fatalf("insert same namespace/key for different protocol: %v", err)
	}
	duplicate := CompileCacheEntry{
		Protocol: CompileCacheProtocolCCache, Namespace: migrated.Namespace, Key: migrated.Key,
		StoragePath: "v2/ccache/team-a/objects/exact-duplicate", Size: 789,
		LastAccessed: time.Now().UTC(),
	}
	if err := database.Create(&duplicate).Error; err == nil {
		t.Fatal("insert exact protocol/namespace/key duplicate succeeded")
	}

	if err := AutoMigrate(database); err != nil {
		t.Fatalf("repeat AutoMigrate: %v", err)
	}
	assertCompileCacheIdentityIndex(t, database)
}

func openCompileCacheMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := Open("sqlite", filepath.Join(t.TempDir(), "depsilo.db"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	return database
}

func assertCompileCacheIdentityIndex(t *testing.T, database *gorm.DB) {
	t.Helper()
	indexes, err := database.Migrator().GetIndexes(&CompileCacheEntry{})
	if err != nil {
		t.Fatalf("read compiler-cache indexes: %v", err)
	}
	if database.Migrator().HasIndex(&CompileCacheEntry{}, legacyCompileCacheIdentityIndex) {
		t.Errorf("legacy index %s still exists", legacyCompileCacheIdentityIndex)
	}
	for _, index := range indexes {
		if index.Name() != wantCompileCacheIdentityIndex {
			continue
		}
		unique, known := index.Unique()
		if !known || !unique {
			t.Errorf("replacement index unique = (%v, %v), want (true, true)", unique, known)
		}
		wantColumns := []string{"protocol", "namespace", "key"}
		if !slices.Equal(index.Columns(), wantColumns) {
			t.Errorf("replacement index columns = %v, want %v", index.Columns(), wantColumns)
		}
		return
	}
	t.Errorf("replacement index %s does not exist", wantCompileCacheIdentityIndex)
}
