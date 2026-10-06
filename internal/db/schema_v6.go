package db

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateSnapshots adds the freeze / golden-snapshot tables. Snapshots pin the
// artifact identities and hashes captured from tamper detection; existing
// installs start with no snapshots and snapshot-only mode disabled.
func migrateSnapshots(database *gorm.DB) error {
	if err := database.AutoMigrate(&Snapshot{}, &SnapshotItem{}); err != nil {
		return fmt.Errorf("create snapshot tables: %w", err)
	}
	return nil
}
