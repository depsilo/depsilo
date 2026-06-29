package db

import "time"

// Models for the supply-chain quarantine subsystem (T1 Task 1 — minimum
// release age). Helpers (Store, Lookup, Approve, RecordEvent) live in
// internal/quarantine; only the GORM types live here, matching the
// existing convention (AuditLog / PackageRule / Vulnerability all sit
// in db with their domain helpers in internal/{audit,rules,security}).

// PackageTimestamp caches the upstream publish time of a single
// (ecosystem, package, version). Looking up timestamps on every
// request would hammer registry APIs (npm: ~100ms / pip: ~200ms /
// docker: a token + manifest roundtrip), so we persist the answer
// once and serve from the local DB forever after.
//
// Versions are immutable on every registry we proxy (Go modules
// being the immutability champion but npm / pip / cargo also enforce
// no-republish-under-same-version policies). That means a cached
// timestamp is good forever; we never need TTL or refresh logic.
type PackageTimestamp struct {
	Ecosystem string    `gorm:"size:32;primaryKey" json:"ecosystem"`
	Package   string    `gorm:"size:256;primaryKey" json:"package"`
	Version   string    `gorm:"size:128;primaryKey" json:"version"`
	PublishAt time.Time `gorm:"index" json:"publish_at"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName pins the singular form — GORM's default pluralizer would
// emit "package_timestamps" which is fine but the rest of the schema
// is singular (audit_log, cache_entry) so this stays in style.
func (PackageTimestamp) TableName() string { return "package_timestamp" }

// ApprovedVersion records an operator's manual decision to release
// a quarantined (ecosystem, package, version) early. Per the locked-in
// decisions (option A — "permanent approve") there is no expiry: once
// approved, the version flows forever. If the operator later regrets
// the approval, they revoke via the admin UI.
//
// Reason is mandatory at the admin-handler layer; the DB allows empty
// strings only so that legacy / programmatic inserts (config-based
// pre-seeding) work without a migration headache.
type ApprovedVersion struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	Ecosystem  string    `gorm:"size:32;uniqueIndex:idx_approved_eco_pkg_ver,priority:1" json:"ecosystem"`
	Package    string    `gorm:"size:256;uniqueIndex:idx_approved_eco_pkg_ver,priority:2" json:"package"`
	Version    string    `gorm:"size:128;uniqueIndex:idx_approved_eco_pkg_ver,priority:3" json:"version"`
	Reason     string    `gorm:"size:512" json:"reason"`
	ApprovedBy uint      `gorm:"index" json:"approved_by"` // db.User.ID
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

func (ApprovedVersion) TableName() string { return "approved_version" }

// QuarantineEvent is the auditable record of any quarantine decision
// that mattered: blocks, serves of last-eligible, approval bypasses,
// and approve/revoke admin actions. Powers the Monitor UI's
// "what just got blocked" surface and the webhook payloads.
type QuarantineEvent struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Ecosystem string    `gorm:"size:32;index:idx_qevent_eco_created,priority:1" json:"ecosystem"`
	Package   string    `gorm:"size:256;index" json:"package"`
	Version   string    `gorm:"size:128" json:"version"`
	Action    string    `gorm:"size:32;index" json:"action"`     // see quarantine.ActionXxx constants
	Reason    string    `gorm:"size:512" json:"reason"`           // human-readable detail
	Threshold int64     `json:"threshold_seconds"`                // policy threshold in effect at the time
	AgeAtCall int64     `json:"age_at_call_seconds"`              // observed age when the call was made
	ActorID   uint      `gorm:"index" json:"actor_id"`            // 0 for system events, db.User.ID for admin actions
	ClientIP  string    `gorm:"size:64" json:"client_ip"`         // requesting client's IP (system events only)
	CreatedAt time.Time `gorm:"index:idx_qevent_eco_created,priority:2" json:"created_at"`
}

func (QuarantineEvent) TableName() string { return "quarantine_event" }
