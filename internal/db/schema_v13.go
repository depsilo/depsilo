package db

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Frozen declarations keep later catalog changes out of the v12-to-v13 upgrade.
type schemaV13DistributionSet struct {
	ID         string    `gorm:"primaryKey;size:64;not null"`
	Name       string    `gorm:"size:128;not null;uniqueIndex:idx_distribution_set_name_revision,priority:1"`
	Revision   string    `gorm:"size:128;not null;uniqueIndex:idx_distribution_set_name_revision,priority:2"`
	Manifest   []byte    `gorm:"not null"`
	CreatedBy  string    `gorm:"size:128;not null"`
	CreatedAt  time.Time `gorm:"not null"`
	VerifiedAt *time.Time
	CheckedAt  *time.Time
}

func (schemaV13DistributionSet) TableName() string { return "distribution_sets" }

type schemaV13DistributionObject struct {
	Digest     string    `gorm:"primaryKey;size:64;not null"`
	Size       int64     `gorm:"not null;check:distribution_object_size_nonnegative,size >= 0"`
	StorageKey string    `gorm:"size:256;not null;uniqueIndex:idx_distribution_object_storage_key"`
	VerifiedAt time.Time `gorm:"not null"`
	CreatedAt  time.Time `gorm:"not null"`
}

func (schemaV13DistributionObject) TableName() string { return "distribution_objects" }

type schemaV13DistributionSetObject struct {
	SetID  string `gorm:"primaryKey;size:64;not null"`
	Digest string `gorm:"primaryKey;size:64;not null;index:idx_distribution_set_object_digest"`
	Size   int64  `gorm:"not null;check:distribution_set_object_size_nonnegative,size >= 0"`
}

func (schemaV13DistributionSetObject) TableName() string { return "distribution_set_objects" }

type schemaV13DistributionActiveSet struct {
	Name        string    `gorm:"primaryKey;size:128;not null"`
	SetID       string    `gorm:"size:64;not null;index:idx_distribution_active_set_id"`
	ActivatedAt time.Time `gorm:"not null"`
}

func (schemaV13DistributionActiveSet) TableName() string { return "distribution_active_sets" }

func migrateDistributionCatalog(database *gorm.DB) error {
	if err := database.AutoMigrate(
		&schemaV13DistributionSet{},
		&schemaV13DistributionObject{},
		&schemaV13DistributionSetObject{},
		&schemaV13DistributionActiveSet{},
	); err != nil {
		return fmt.Errorf("create distribution catalog: %w", err)
	}
	return nil
}
