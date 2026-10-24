package db

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Frozen schema-v12 declarations keep future model changes out of this upgrade.
type schemaV12User struct {
	PermissionSource string `gorm:"size:16;not null;default:'local'"`
}

func (schemaV12User) TableName() string { return "users" }

type schemaV12ExternalIdentity struct {
	ID          uint   `gorm:"primarykey"`
	UserID      uint   `gorm:"not null;index:idx_external_identities_user_id;uniqueIndex:idx_external_identity_user_provider,priority:1"`
	Provider    string `gorm:"size:32;not null;uniqueIndex:idx_external_identity_user_provider,priority:2"`
	Issuer      string `gorm:"size:1024;not null;uniqueIndex:idx_external_identity_issuer_subject,priority:1"`
	Subject     string `gorm:"size:255;not null;uniqueIndex:idx_external_identity_issuer_subject,priority:2"`
	DisplayName string `gorm:"size:255"`
	CreatedAt   time.Time
	LastLoginAt time.Time
}

func (schemaV12ExternalIdentity) TableName() string { return "external_identities" }

type schemaV12IdentitySession struct {
	ID                string `gorm:"primarykey;size:64"`
	UserID            uint   `gorm:"not null;index:idx_identity_sessions_user_id"`
	IdentityID        uint   `gorm:"not null;index:idx_identity_sessions_identity_id"`
	Role              string `gorm:"size:16;not null"`
	ConfigFingerprint string `gorm:"size:64;not null"`
	CreatedAt         time.Time
	ExpiresAt         time.Time `gorm:"not null;index:idx_identity_sessions_expires_at"`
	RevokedAt         *time.Time
}

func (schemaV12IdentitySession) TableName() string { return "identity_sessions" }

func migrateExternalIdentities(database *gorm.DB) error {
	if !database.Migrator().HasColumn(&schemaV12User{}, "PermissionSource") {
		if err := database.Migrator().AddColumn(&schemaV12User{}, "PermissionSource"); err != nil {
			return fmt.Errorf("add user permission source: %w", err)
		}
	}
	if err := database.AutoMigrate(&schemaV12ExternalIdentity{}, &schemaV12IdentitySession{}); err != nil {
		return fmt.Errorf("create external identities and sessions: %w", err)
	}
	return nil
}
