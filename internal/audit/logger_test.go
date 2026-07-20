package audit

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
)

func TestLoggerDrainsAcceptedEntriesOnImmediateCancellation(t *testing.T) {
	database := newAuditLoggerTestDB(t)
	auditLogger := NewLogger(database, nil)
	const entries = 250
	for index := range entries {
		auditLogger.Log(db.AuditLog{
			Ecosystem:   "pypi",
			PackageName: fmt.Sprintf("package-%03d", index),
			Action:      "download",
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	auditLogger.Start(ctx)

	var count int64
	if err := database.Model(&db.AuditLog{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != entries {
		t.Fatalf("audit rows after immediate cancellation = %d, want %d", count, entries)
	}
}

func TestLoggerFlushesPartialBatchOnCancellation(t *testing.T) {
	database := newAuditLoggerTestDB(t)
	auditLogger := NewLogger(database, nil)
	auditLogger.Log(db.AuditLog{Ecosystem: "npm", PackageName: "left-pad", Action: "download"})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		auditLogger.Start(ctx)
		close(done)
	}()
	cancel()
	<-done

	var count int64
	if err := database.Model(&db.AuditLog{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit rows = %d, want 1", count)
	}
}

func newAuditLoggerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "audit.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open audit database: %v", err)
	}
	if err := database.AutoMigrate(&db.AuditLog{}); err != nil {
		t.Fatalf("migrate audit logs: %v", err)
	}
	return database
}
