package db

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateRequestDiagnostics adds bounded, optional facts to request-shaped
// records. Legacy rows remain explicitly unknown; no historical values are
// reconstructed from current rules or upstream configuration.
func migrateRequestDiagnostics(database *gorm.DB) error {
	for _, field := range []string{
		"RequestID", "CacheResult", "CacheReason", "PolicyDecision", "PolicyReason",
		"DeliveryResult", "DeliveryReason",
	} {
		if database.Migrator().HasColumn(&AccessLog{}, field) {
			continue
		}
		if err := database.Migrator().AddColumn(&AccessLog{}, field); err != nil {
			return fmt.Errorf("add access log diagnostic %s column: %w", field, err)
		}
	}
	if !database.Migrator().HasColumn(&AuditLog{}, "RequestID") {
		if err := database.Migrator().AddColumn(&AuditLog{}, "RequestID"); err != nil {
			return fmt.Errorf("add audit log request id column: %w", err)
		}
	}
	if !database.Migrator().HasIndex(&AccessLog{}, "idx_access_logs_request_id") {
		if err := database.Migrator().CreateIndex(&AccessLog{}, "RequestID"); err != nil {
			return fmt.Errorf("index access log request id: %w", err)
		}
	}
	if !database.Migrator().HasIndex(&AuditLog{}, "idx_audit_logs_request_id") {
		if err := database.Migrator().CreateIndex(&AuditLog{}, "RequestID"); err != nil {
			return fmt.Errorf("index audit log request id: %w", err)
		}
	}
	return ensureSchemaV4Invariants(database)
}

func ensureSchemaV4Invariants(database *gorm.DB) error {
	for _, field := range []string{
		"RequestID", "CacheResult", "CacheReason", "PolicyDecision", "PolicyReason",
		"DeliveryResult", "DeliveryReason",
	} {
		if !database.Migrator().HasColumn(&AccessLog{}, field) {
			return fmt.Errorf("access log diagnostic column %s is missing", field)
		}
	}
	if !database.Migrator().HasColumn(&AuditLog{}, "RequestID") {
		return fmt.Errorf("audit log request id column is missing")
	}
	if !database.Migrator().HasIndex(&AccessLog{}, "idx_access_logs_request_id") ||
		!database.Migrator().HasIndex(&AuditLog{}, "idx_audit_logs_request_id") {
		return fmt.Errorf("request diagnostics indexes are missing")
	}
	return nil
}
