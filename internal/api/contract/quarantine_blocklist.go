package contract

import "time"

// BlocklistStatus answers GET /admin/blocklist/status while the malicious
// blocklist is enabled. A disabled blocklist answers BlocklistDisabled
// instead. Running and SyncIntervalSeconds are present when a syncer runs;
// NextSyncAt also needs a previous sync.
type BlocklistStatus struct {
	Enabled             bool             `json:"enabled"`
	Mode                string           `json:"mode"`
	LastSyncAt          *time.Time       `json:"last_sync_at" tstype:"string | null,required"`
	LastSuccessAt       *time.Time       `json:"last_success_at" tstype:"string | null,required"`
	LastError           string           `json:"last_error"`
	DurationMs          int64            `json:"duration_ms"`
	EntryCount          int64            `json:"entry_count"`
	PerEcosystem        map[string]int64 `json:"per_ecosystem"`
	Ecosystems          []string         `json:"ecosystems"`
	Running             *bool            `json:"running,omitempty"`
	SyncIntervalSeconds *int64           `json:"sync_interval_seconds,omitempty"`
	NextSyncAt          *time.Time       `json:"next_sync_at,omitempty"`
}

// BlocklistDisabled answers the blocklist read endpoints when
// [supply_chain.blocklist] is disabled; Enabled is always false.
type BlocklistDisabled struct {
	Enabled bool `json:"enabled"`
}

// BlocklistSyncResponse answers POST /admin/blocklist/sync (202); the sync
// runs in the background and the status endpoint reports its outcome.
type BlocklistSyncResponse struct {
	Status string `json:"status" tstype:"'sync started'"`
}

// MalwareOverride is a time-limited exemption from the malicious blocklist.
// An empty Version covers every version of the package.
type MalwareOverride struct {
	ID        uint      `json:"id"`
	Ecosystem string    `json:"ecosystem"`
	Package   string    `json:"package"`
	Version   string    `json:"version"`
	Reason    string    `json:"reason"`
	ActorID   uint      `json:"actor_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// BlocklistOverrideListResponse answers GET /admin/blocklist/overrides with
// active and recently expired overrides and the server's clock, so the
// browser can count down expiry without trusting its own.
type BlocklistOverrideListResponse struct {
	Items []MalwareOverride `json:"items"`
	Now   time.Time         `json:"now"`
}

// CreatedMalwareOverride answers override creation (201). MatchesBlocklist
// reports whether the package has any blocklist entry, so a mistyped name is
// visible at once.
type CreatedMalwareOverride struct {
	MalwareOverride  `tstype:",extends"`
	MatchesBlocklist bool `json:"matches_blocklist"`
}

// BlocklistRevokeOverrideResponse answers DELETE
// /admin/blocklist/overrides/:id with the revoked override's id.
type BlocklistRevokeOverrideResponse struct {
	Revoked uint64 `json:"revoked"`
}
