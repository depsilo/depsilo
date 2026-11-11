package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"
)

// A commit by another connection between the usage transaction's read and its
// write invalidates the deferred transaction's WAL snapshot. SQLite reports
// that at once as SQLITE_BUSY, which busy_timeout cannot wait out; the access
// recorder hit it while flushing during shutdown.
func TestSaveDependencyUsageRetriesStaleSQLiteSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	database, err := Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, handle := range []*gorm.DB{database, writer} {
		sqlDB, err := handle.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("CREATE TABLE competing_writes (value INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	competing := 0
	if err := database.Callback().Create().Before("gorm:create").Register("test:commit_competing_writer", func(tx *gorm.DB) {
		if competing > 0 {
			return
		}
		competing++
		if err := writer.Exec("INSERT INTO competing_writes VALUES (1)").Error; err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)
	ctx := context.Background()
	if err := SaveDependencyUsage(ctx, database, DependencyUsage{State: DependencyUsed, FirstSuccessfulRequestAt: &at}, true); err != nil {
		t.Fatalf("save after a concurrent commit: %v", err)
	}
	usage, err := ReadDependencyUsage(ctx, database)
	if err != nil || usage.State != DependencyUsed || usage.FirstSuccessfulRequestAt == nil || !usage.FirstSuccessfulRequestAt.Equal(at) {
		t.Fatalf("stored usage = %+v, %v", usage, err)
	}
	if competing != 1 {
		t.Fatalf("competing commits = %d, want exactly one injected conflict", competing)
	}
}
