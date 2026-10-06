package db

import "time"

// Snapshot is one approved artifact set promoted from the local cache. Items
// are the tamper-detection records captured at creation time, so every item
// carries the first-seen SHA-256 of the artifact bytes.
type Snapshot struct {
	ID            uint      `gorm:"primarykey" json:"id"`
	Name          string    `gorm:"size:128;uniqueIndex" json:"name"`
	Note          string    `gorm:"size:512" json:"note"`
	CreatedBy     string    `gorm:"size:64" json:"created_by"`
	ArtifactCount int64     `json:"artifact_count"`
	TotalBytes    int64     `json:"total_bytes"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (Snapshot) TableName() string { return "snapshots" }

// SnapshotItem is one artifact pinned by a snapshot. The identity tuple is
// what snapshot-only mode matches; the hash and cache key keep the exported
// manifest verifiable against the bytes.
type SnapshotItem struct {
	ID         uint   `gorm:"primarykey" json:"id"`
	SnapshotID uint   `gorm:"not null;uniqueIndex:idx_snapshot_item" json:"snapshot_id"`
	Ecosystem  string `gorm:"size:32;not null;uniqueIndex:idx_snapshot_item" json:"ecosystem"`
	Package    string `gorm:"size:256;not null;uniqueIndex:idx_snapshot_item" json:"package"`
	Version    string `gorm:"size:128;not null;uniqueIndex:idx_snapshot_item" json:"version"`
	CacheKey   string `gorm:"size:512;not null;uniqueIndex:idx_snapshot_item" json:"cache_key"`
	SHA256     string `gorm:"size:64;not null" json:"sha256"`
	Size       int64  `json:"size"`
}

func (SnapshotItem) TableName() string { return "snapshot_items" }
