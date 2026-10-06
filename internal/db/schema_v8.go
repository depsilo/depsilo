package db

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateAuditChain adds the tamper-evident audit chain. Rows written before
// the migration keep a NULL prev_hash and an empty hash; verification reports
// them as a pre-chain prefix rather than pretending they are covered.
func migrateAuditChain(database *gorm.DB) error {
	for _, field := range []string{"PrevHash", "Hash"} {
		if database.Migrator().HasColumn(&AuditLog{}, field) {
			continue
		}
		if err := database.Migrator().AddColumn(&AuditLog{}, field); err != nil {
			return fmt.Errorf("add audit chain column %s: %w", field, err)
		}
	}
	if !database.Migrator().HasIndex(&AuditLog{}, "idx_audit_logs_prev_hash") {
		if err := database.Migrator().CreateIndex(&AuditLog{}, "PrevHash"); err != nil {
			return fmt.Errorf("index audit chain prev_hash: %w", err)
		}
	}
	return nil
}
