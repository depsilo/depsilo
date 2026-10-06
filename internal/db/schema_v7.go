package db

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateAuditExporters adds the SIEM audit-routing table. Existing installs
// start with no exporters; a new exporter begins at the current audit head so
// onboarding never replays months of history into a collector.
func migrateAuditExporters(database *gorm.DB) error {
	if err := database.AutoMigrate(&AuditExporter{}); err != nil {
		return fmt.Errorf("create audit exporter table: %w", err)
	}
	return nil
}
