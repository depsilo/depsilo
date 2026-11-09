package db

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Frozen declaration keeps later provenance changes out of the v13-to-v14 upgrade.
type schemaV14DistributionSetProvenance struct {
	SetID            string    `gorm:"primaryKey;size:64;not null"`
	Origin           string    `gorm:"size:32;not null"`
	SignerKeyID      string    `gorm:"size:64;not null;index:idx_distribution_provenance_signer"`
	TransferManifest []byte    `gorm:"not null"`
	Signature        []byte    `gorm:"not null"`
	ImportedBy       string    `gorm:"size:128;not null"`
	ImportedAt       time.Time `gorm:"not null"`
}

func (schemaV14DistributionSetProvenance) TableName() string {
	return "distribution_set_provenances"
}

func migrateDistributionProvenance(database *gorm.DB) error {
	if err := database.AutoMigrate(&schemaV14DistributionSetProvenance{}); err != nil {
		return fmt.Errorf("create distribution provenance: %w", err)
	}
	return nil
}
