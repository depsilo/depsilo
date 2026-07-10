package db

import "time"

// TamperRecord is the first-seen content fingerprint of one immutable
// artifact (keyed by its cache key, 1:1 with the artifact). Tamper
// detection compares a re-fetched artifact's SHA-256 against SHA256
// here; a mismatch on an immutable key is a tamper alert. Domain
// helper lives in internal/tamper.
type TamperRecord struct {
	Key            string    `gorm:"size:512;primaryKey" json:"key"`
	Ecosystem      string    `gorm:"size:32;index" json:"ecosystem"`
	Package        string    `gorm:"size:256;index" json:"package"`
	Version        string    `gorm:"size:128" json:"version"`
	SHA256         string    `gorm:"size:64" json:"sha256"`
	Size           int64     `json:"size"`
	FirstSeenAt    time.Time `json:"first_seen_at"`
	LastVerifiedAt time.Time `json:"last_verified_at"`
	VerifyCount    int64     `json:"verify_count"`
}

func (TamperRecord) TableName() string { return "tamper_record" }
