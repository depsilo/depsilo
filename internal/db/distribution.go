package db

import "time"

// DistributionSet retains one immutable manifest revision. VerifiedAt records
// the most recent successful content check, not a promise that the objects can
// never be lost or corrupted. CheckedAt also advances after failed checks.
type DistributionSet struct {
	ID         string     `gorm:"primaryKey;size:64;not null" json:"id"`
	Name       string     `gorm:"size:128;not null;uniqueIndex:idx_distribution_set_name_revision,priority:1" json:"name"`
	Revision   string     `gorm:"size:128;not null;uniqueIndex:idx_distribution_set_name_revision,priority:2" json:"revision"`
	Manifest   []byte     `gorm:"not null" json:"-"`
	CreatedBy  string     `gorm:"size:128;not null" json:"created_by"`
	CreatedAt  time.Time  `gorm:"not null" json:"created_at"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
}

func (DistributionSet) TableName() string { return "distribution_sets" }

// DistributionObject describes content owned by distribution, independently of
// ordinary cache_entries and their eviction lifecycle.
type DistributionObject struct {
	Digest     string    `gorm:"primaryKey;size:64;not null" json:"digest"`
	Size       int64     `gorm:"not null;check:distribution_object_size_nonnegative,size >= 0" json:"size"`
	StorageKey string    `gorm:"size:256;not null;uniqueIndex:idx_distribution_object_storage_key" json:"-"`
	VerifiedAt time.Time `gorm:"not null" json:"verified_at"`
	CreatedAt  time.Time `gorm:"not null" json:"created_at"`
}

func (DistributionObject) TableName() string { return "distribution_objects" }

// DistributionSetObject is the content requirement declared by a manifest.
// It can exist before the corresponding object arrives. The distribution
// module checks all references before verification or activation.
type DistributionSetObject struct {
	SetID  string `gorm:"primaryKey;size:64;not null" json:"set_id"`
	Digest string `gorm:"primaryKey;size:64;not null;index:idx_distribution_set_object_digest" json:"digest"`
	Size   int64  `gorm:"not null;check:distribution_set_object_size_nonnegative,size >= 0" json:"size"`
}

func (DistributionSetObject) TableName() string { return "distribution_set_objects" }

// DistributionActiveSet names one selected immutable revision. Switching this
// pointer never changes the manifest or releases the previous revision's data.
type DistributionActiveSet struct {
	Name        string    `gorm:"primaryKey;size:128;not null" json:"name"`
	SetID       string    `gorm:"size:64;not null;index:idx_distribution_active_set_id" json:"set_id"`
	ActivatedAt time.Time `gorm:"not null" json:"activated_at"`
}

func (DistributionActiveSet) TableName() string { return "distribution_active_sets" }

// DistributionSetProvenance records the signed transfer manifest through which
// a set arrived. It attests that a trusted preparer declared these bytes; it is
// not an upstream-native signature, a release timestamp, or install evidence.
type DistributionSetProvenance struct {
	SetID            string    `gorm:"primaryKey;size:64;not null" json:"set_id"`
	Origin           string    `gorm:"size:32;not null" json:"origin"`
	SignerKeyID      string    `gorm:"size:64;not null;index:idx_distribution_provenance_signer" json:"signer_key_id"`
	TransferManifest []byte    `gorm:"not null" json:"-"`
	Signature        []byte    `gorm:"not null" json:"-"`
	ImportedBy       string    `gorm:"size:128;not null" json:"imported_by"`
	ImportedAt       time.Time `gorm:"not null" json:"imported_at"`
}

func (DistributionSetProvenance) TableName() string { return "distribution_set_provenances" }
