package db

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Frozen declaration keeps later ledger changes out of the v15-to-v16 upgrade.
type schemaV16DistributionExport struct {
	ID         uint      `gorm:"primaryKey"`
	SetID      string    `gorm:"size:64;not null;index:idx_distribution_exports_set"`
	Bytes      int64     `gorm:"not null"`
	State      string    `gorm:"size:16;not null"`
	Metered    bool      `gorm:"not null;default:false"`
	Actor      string    `gorm:"size:128;not null"`
	StartedAt  time.Time `gorm:"not null;index:idx_distribution_exports_started"`
	FinishedAt *time.Time
}

func (schemaV16DistributionExport) TableName() string { return "distribution_exports" }

// migrateDistributionExportUsage adds the ledger that meters transfer-bundle
// exports against the free edition's size and monthly allowances. Earlier
// exports were not recorded, so every instance starts with no usage.
func migrateDistributionExportUsage(database *gorm.DB) error {
	if err := database.AutoMigrate(&schemaV16DistributionExport{}); err != nil {
		return fmt.Errorf("create distribution export ledger: %w", err)
	}
	return nil
}
