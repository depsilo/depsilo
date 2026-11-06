package db

import (
	"context"
	"path/filepath"
	"strings"
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

func TestDurableConnectionReleaseRollsBackFailedCommit(t *testing.T) {
	for _, mode := range []string{"file", "memory"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "durable.db")
			if mode == "memory" {
				path = ":memory:"
			}
			database, err := Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			sqlDatabase, err := database.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDatabase.Close() })
			// PRAGMA foreign_keys is connection-local. Reuse this connection for
			// the injected COMMIT failure and every subsequent operation.
			sqlDatabase.SetMaxOpenConns(1)
			for _, statement := range []string{
				"PRAGMA foreign_keys=ON",
				"CREATE TABLE durable_parent (id INTEGER PRIMARY KEY, value INTEGER)",
				"INSERT INTO durable_parent VALUES (1, 10)",
				"CREATE TABLE durable_child (parent_id INTEGER REFERENCES durable_parent(id) DEFERRABLE INITIALLY DEFERRED)",
			} {
				if err := database.Exec(statement).Error; err != nil {
					t.Fatal(err)
				}
			}
			tx, release, err := BeginDurableTransaction(context.Background(), database)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tx.Rollback(); _ = release() })
			if err := tx.Exec("UPDATE durable_parent SET value = 99 WHERE id = 1").Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Exec("INSERT INTO durable_child VALUES (2)").Error; err != nil {
				t.Fatalf("constraint failed before COMMIT: %v", err)
			}
			if err := tx.Commit().Error; err == nil || !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
				t.Fatalf("expected deferred constraint failure at COMMIT: %v", err)
			}
			_ = tx.Rollback() // Cannot undo a failed COMMIT through sql.Tx alone.
			if err := release(); err != nil {
				t.Fatalf("release after failed COMMIT: %v", err)
			}
			if err := release(); err != nil {
				t.Fatalf("release is not idempotent: %v", err)
			}
			var value, childCount, synchronous int
			if err := database.Raw("SELECT value FROM durable_parent WHERE id = 1").Scan(&value).Error; err != nil || value != 10 {
				t.Fatalf("failed COMMIT leaked updated value: %d err=%v", value, err)
			}
			if err := database.Raw("SELECT COUNT(*) FROM durable_child").Scan(&childCount).Error; err != nil || childCount != 0 {
				t.Fatalf("failed COMMIT leaked invalid reference: %d err=%v", childCount, err)
			}
			if err := database.Raw("PRAGMA synchronous").Scan(&synchronous).Error; err != nil || synchronous != 1 {
				t.Fatalf("connection settings not restored: %d err=%v", synchronous, err)
			}
			next, nextRelease, err := BeginDurableTransaction(context.Background(), database)
			if err != nil {
				t.Fatalf("connection unusable after failed COMMIT: %v", err)
			}
			t.Cleanup(func() { _ = next.Rollback(); _ = nextRelease() })
			if err := next.Exec("UPDATE durable_parent SET value = 20 WHERE id = 1").Error; err != nil {
				t.Fatal(err)
			}
			if err := next.Commit().Error; err != nil {
				t.Fatalf("subsequent COMMIT: %v", err)
			}
			if err := nextRelease(); err != nil {
				t.Fatal(err)
			}
			if mode == "file" {
				if err := sqlDatabase.Close(); err != nil {
					t.Fatal(err)
				}
				database, err = Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				reopened, _ := database.DB()
				t.Cleanup(func() { _ = reopened.Close() })
			}
			if err := database.Raw("SELECT value FROM durable_parent WHERE id = 1").Scan(&value).Error; err != nil || value != 20 {
				t.Fatalf("subsequent committed value not retained: %d err=%v", value, err)
			}
		})
	}
}
