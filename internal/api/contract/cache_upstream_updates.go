package contract

import "time"

// UpstreamUpdateResult is the outcome of one index refresh against an
// upstream.
type UpstreamUpdateResult string

const (
	UpstreamUpdateResultUpdated   UpstreamUpdateResult = "updated"
	UpstreamUpdateResultUnchanged UpstreamUpdateResult = "unchanged"
	UpstreamUpdateResultError     UpstreamUpdateResult = "error"
)

// AdminUpstreamUpdateEvent is one episode of index refreshes: consecutive
// identical observations coalesce, counted by OccurrenceCount.
type AdminUpstreamUpdateEvent struct {
	ID              uint                 `json:"id"`
	CacheEntryID    uint                 `json:"cache_entry_id"`
	Ecosystem       string               `json:"ecosystem"`
	Upstream        string               `json:"upstream"`
	Package         string               `json:"package"`
	Result          UpstreamUpdateResult `json:"result"`
	Detail          string               `json:"detail"`
	LatencyMs       int64                `json:"latency_ms"`
	OccurrenceCount uint64               `json:"occurrence_count"`
	FirstSeenAt     time.Time            `json:"first_seen_at"`
	LastSeenAt      time.Time            `json:"last_seen_at"`
	CreatedAt       time.Time            `json:"created_at"`
}

// AdminUpstreamUpdateListResponse answers GET /admin/upstream-updates.
// NextCursor is null on the last page.
type AdminUpstreamUpdateListResponse struct {
	Items      []AdminUpstreamUpdateEvent `json:"items"`
	Total      int64                      `json:"total"`
	NextCursor *string                    `json:"next_cursor" tstype:"string | null,required"`
	Ecosystems []EcosystemCount           `json:"ecosystems"`
}
