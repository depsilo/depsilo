package db

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateUserCredentialVersion gives every existing operator a persistent
// JWT revocation generation. Tokens minted before this schema version have no
// matching claim and are therefore invalidated once during upgrade.
func migrateUserCredentialVersion(database *gorm.DB) error {
	if !database.Migrator().HasColumn(&User{}, "CredentialVersion") {
		if err := database.Migrator().AddColumn(&User{}, "CredentialVersion"); err != nil {
			return fmt.Errorf("add user credential version: %w", err)
		}
	}
	return ensureSchemaV2Invariants(database)
}

func ensureSchemaV2Invariants(database *gorm.DB) error {
	if !database.Migrator().HasColumn(&User{}, "CredentialVersion") {
		return fmt.Errorf("user credential version column is missing")
	}
	if err := database.Model(&User{}).
		Where("credential_version IS NULL OR credential_version = 0").
		Update("credential_version", InitialCredentialVersion).Error; err != nil {
		return fmt.Errorf("backfill user credential version: %w", err)
	}
	return nil
}
