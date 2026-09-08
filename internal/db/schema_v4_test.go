package db

import (
	"testing"
	"time"
)

func TestRequestDiagnosticsMigrationAddsOptionalColumns(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}
	if err := applySchemaMigration(database, schemaMigrations[3], time.Now().UTC()); err != nil {
		t.Fatalf("apply request diagnostics migration: %v", err)
	}
	for _, field := range []string{"RequestID", "CacheResult", "CacheReason", "PolicyDecision", "PolicyReason", "DeliveryResult", "DeliveryReason"} {
		if !database.Migrator().HasColumn(&AccessLog{}, field) {
			t.Errorf("access log column %s is missing", field)
		}
	}
	if !database.Migrator().HasColumn(&AuditLog{}, "RequestID") {
		t.Fatal("audit log request_id column is missing")
	}
}
