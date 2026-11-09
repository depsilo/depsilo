package db

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// migrateVulnerabilityDetailRevision records which parser derived each stored
// advisory. Existing rows start at revision 0, whose severity parser misread
// OSV CVSS vectors and whose batch scans could replace advisory details with
// bare IDs. Every vulnerability check becomes due, so the next scan fetches
// those advisories again instead of waiting for each check to expire.
func migrateVulnerabilityDetailRevision(database *gorm.DB) error {
	if !database.Migrator().HasColumn(&Vulnerability{}, "DetailRevision") {
		if err := database.Migrator().AddColumn(&Vulnerability{}, "DetailRevision"); err != nil {
			return fmt.Errorf("add vulnerability detail revision column: %w", err)
		}
	}
	if err := database.Model(&VulnerabilityCheck{}).
		Where("1 = 1").
		UpdateColumn("next_fetch_at", time.Unix(0, 0).UTC()).Error; err != nil {
		return fmt.Errorf("schedule vulnerability checks for refresh: %w", err)
	}
	return nil
}
