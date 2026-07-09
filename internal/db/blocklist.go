package db

import "time"

// Models for the known-malicious blocklist (DIRECTION Task 2). Domain
// helpers (Store, Syncer) live in internal/blocklist; only the GORM
// types live here, matching the quarantine convention.

// MaliciousPackage is one (advisory, ecosystem, package) row imported
// from the OSV malicious-packages dataset (MAL-* entries; GitHub's
// GHSA-malware advisories are aliased into the same set). Package
// names are stored in their ecosystem-normalized form (npm lowercase,
// PyPI PEP 503) so request-time lookups are a single indexed equality.
type MaliciousPackage struct {
	ID        uint   `gorm:"primarykey" json:"id"`
	SourceID  string `gorm:"size:64;uniqueIndex:idx_mal_src_eco_pkg,priority:1" json:"source_id"` // e.g. MAL-2026-1234
	Ecosystem string `gorm:"size:32;uniqueIndex:idx_mal_src_eco_pkg,priority:2;index:idx_mal_eco_pkg,priority:1" json:"ecosystem"`
	Package   string `gorm:"size:256;uniqueIndex:idx_mal_src_eco_pkg,priority:3;index:idx_mal_eco_pkg,priority:2" json:"package"`

	// Versions is a JSON array of exact affected versions. Empty means
	// EVERY version is malicious — the dataset's overwhelmingly common
	// case (introduced: "0" with no fixed event).
	Versions string `gorm:"type:text" json:"versions"`

	Aliases    string    `gorm:"size:512" json:"aliases"` // comma-joined advisory aliases (GHSA-…)
	Summary    string    `gorm:"size:512" json:"summary"`
	Modified   time.Time `json:"modified"` // advisory's own modified stamp
	ImportedAt time.Time `gorm:"index" json:"imported_at"`
}

func (MaliciousPackage) TableName() string { return "malicious_package" }

// MalwareOverride is an operator's explicit, audited exemption for a
// false positive. Unlike quarantine's permanent ApprovedVersion, an
// override EXPIRES 24h after creation and cannot be extended — only
// re-created (each re-creation is a fresh audited decision). Malware
// exemptions are emergency valves, not standing configuration.
type MalwareOverride struct {
	ID        uint   `gorm:"primarykey" json:"id"`
	Ecosystem string `gorm:"size:32;index:idx_override_eco_pkg,priority:1" json:"ecosystem"`
	Package   string `gorm:"size:256;index:idx_override_eco_pkg,priority:2" json:"package"`

	// Version empty means the override covers every version of the
	// package (matching the dataset's all-versions advisories).
	Version string `gorm:"size:128" json:"version"`

	Reason    string    `gorm:"size:512" json:"reason"` // mandatory at the handler layer
	ActorID   uint      `gorm:"index" json:"actor_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `gorm:"index" json:"expires_at"`
}

func (MalwareOverride) TableName() string { return "malware_override" }

// BlocklistSyncState is a single-row record of the last sync attempt,
// powering the admin status card and the degrade-on-failure posture:
// a failed sync updates LastSyncAt/LastError but leaves the imported
// rows (and LastSuccessAt) untouched, so blocking continues on the
// last good dataset.
type BlocklistSyncState struct {
	ID            uint       `gorm:"primarykey" json:"id"` // always 1
	LastSyncAt    *time.Time `json:"last_sync_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     string     `gorm:"size:512" json:"last_error"`
	EntryCount    int64      `json:"entry_count"`
	DurationMs    int64      `json:"duration_ms"`
}

func (BlocklistSyncState) TableName() string { return "blocklist_sync_state" }
