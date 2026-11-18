package contract

import "time"

// Snapshot is a frozen set of cached artifacts.
type Snapshot struct {
	ID            uint      `json:"id"`
	Name          string    `json:"name"`
	Note          string    `json:"note"`
	CreatedBy     string    `json:"created_by"`
	ArtifactCount int64     `json:"artifact_count"`
	TotalBytes    int64     `json:"total_bytes"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// SnapshotItem is one artifact a snapshot pins.
type SnapshotItem struct {
	ID         uint   `json:"id"`
	SnapshotID uint   `json:"snapshot_id"`
	Ecosystem  string `json:"ecosystem"`
	Package    string `json:"package"`
	Version    string `json:"version"`
	CacheKey   string `json:"cache_key"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
}

// SnapshotListResponse answers GET /admin/snapshots. The active snapshot is
// ID 0 and name "" when snapshot-only mode is off.
type SnapshotListResponse struct {
	Items            []Snapshot `json:"items"`
	ActiveSnapshotID uint       `json:"active_snapshot_id"`
	ActiveSnapshot   string     `json:"active_snapshot"`
}

// SnapshotDetailResponse answers GET /admin/snapshots/:id with one page of
// its items.
type SnapshotDetailResponse struct {
	Snapshot Snapshot       `json:"snapshot"`
	Items    []SnapshotItem `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	Active   bool           `json:"active"`
}

// SnapshotActivateResponse answers PUT /admin/snapshots/active; ID 0 and
// name "" mean snapshot-only mode was switched off.
type SnapshotActivateResponse struct {
	ActiveSnapshotID uint   `json:"active_snapshot_id"`
	ActiveSnapshot   string `json:"active_snapshot"`
}

// SnapshotDeleteResponse answers DELETE /admin/snapshots/:id.
type SnapshotDeleteResponse struct {
	Deleted uint `json:"deleted"`
}
