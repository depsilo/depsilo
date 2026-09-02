package db

import (
	"context"
	"path/filepath"
	"testing"

	"gorm.io/gorm"
)

func TestBeginDurableTransactionPinsFULLSynchronousConnection(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "durable.db")
	database, err := Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	if err := database.AutoMigrate(&User{}); err != nil {
		t.Fatal(err)
	}

	tx, release, err := BeginDurableTransaction(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	var synchronous int
	if err := tx.Raw("PRAGMA synchronous").Scan(&synchronous).Error; err != nil {
		t.Fatalf("read transaction synchronous mode: %v", err)
	}
	if synchronous < 2 {
		t.Fatalf("transaction synchronous mode = %d, want FULL (2) or stronger", synchronous)
	}
	if err := tx.Create(&User{Username: "durable", PasswordHash: "test", Role: "admin", Enabled: true}).Error; err != nil {
		t.Fatalf("write durable row: %v", err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatalf("release durable connection: %v", err)
	}

	// The connection is returned to the regular pool in its original NORMAL
	// mode, so the one-off setup durability does not silently penalize normal
	// cache traffic.
	var pooledSynchronous int
	if err := database.Raw("PRAGMA synchronous").Scan(&pooledSynchronous).Error; err != nil {
		t.Fatal(err)
	}
	if pooledSynchronous != 1 {
		t.Fatalf("pooled synchronous mode = %d, want NORMAL (1)", pooledSynchronous)
	}
	if err := sqlDatabase.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	reopenedSQL, err := reopened.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedSQL.Close() })
	var persisted User
	if err := reopened.Where("username = ?", "durable").First(&persisted).Error; err != nil {
		t.Fatalf("read committed row after reopen: %v", err)
	}
}

func TestBeginDurableTransactionRejectsNilDatabase(t *testing.T) {
	if _, _, err := BeginDurableTransaction(context.Background(), (*gorm.DB)(nil)); err == nil {
		t.Fatal("nil database unexpectedly accepted")
	}
}
