package contract

import "time"

// AuditLogCacheResult is an audit row's outcome: hit, miss, or error for a
// package request, blocked for a policy denial, and success or error for a
// management event.
type AuditLogCacheResult string

const (
	AuditLogCacheResultHit     AuditLogCacheResult = "hit"
	AuditLogCacheResultMiss    AuditLogCacheResult = "miss"
	AuditLogCacheResultSuccess AuditLogCacheResult = "success"
	AuditLogCacheResultError   AuditLogCacheResult = "error"
	AuditLogCacheResultBlocked AuditLogCacheResult = "blocked"
)

// AuditLog is one audit row. UpstreamURL is credential-masked for roles that
// may not view credentials.
type AuditLog struct {
	ID          uint                `json:"id"`
	Ecosystem   string              `json:"ecosystem"`
	PackageName string              `json:"package_name"`
	Version     string              `json:"version"`
	Action      string              `json:"action"`
	CacheResult AuditLogCacheResult `json:"cache_result"`
	ClientIP    string              `json:"client_ip"`
	UserAgent   string              `json:"user_agent"`
	UpstreamURL string              `json:"upstream_url"`
	LatencyMs   int64               `json:"latency_ms"`
	BytesSent   int64               `json:"bytes_sent"`
	StatusCode  int                 `json:"status_code"`
	CreatedAt   time.Time           `json:"created_at"`
}

// AuditLogListResponse answers GET /admin/audit-logs. Ecosystems counts the
// rows every other filter matches, per ecosystem.
type AuditLogListResponse struct {
	Items      []AuditLog       `json:"items"`
	Total      int64            `json:"total"`
	Page       int              `json:"page"`
	Ecosystems []EcosystemCount `json:"ecosystems"`
}

// AuditExporterKind is the wire format an audit exporter posts.
type AuditExporterKind string

const (
	AuditExporterKindNDJSON    AuditExporterKind = "ndjson"
	AuditExporterKindSplunkHEC AuditExporterKind = "splunk_hec"
)

// AuditExporter is one SIEM collector the audit stream is forwarded to. The
// token is write-only; TokenSet reports whether one is stored.
type AuditExporter struct {
	ID       uint              `json:"id"`
	Name     string            `json:"name"`
	Kind     AuditExporterKind `json:"kind"`
	URL      string            `json:"url"`
	TokenSet bool              `json:"token_set"`
	Events   string            `json:"events"`
	Enabled  bool              `json:"enabled"`
	Cursor   uint              `json:"cursor"`
	// Lag is how many audit rows are not yet delivered (audit head - cursor).
	Lag            int64      `json:"lag"`
	DeliveredCount int64      `json:"delivered_count"`
	LastError      string     `json:"last_error"`
	LastAttemptAt  *time.Time `json:"last_attempt_at" tstype:"string | null,required"`
	LastSuccessAt  *time.Time `json:"last_success_at" tstype:"string | null,required"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// AuditExporterListResponse answers GET /admin/audit/exporters.
type AuditExporterListResponse struct {
	AuditHead uint            `json:"audit_head"`
	Items     []AuditExporter `json:"items"`
}

// AuditExporterDeleteResponse answers DELETE /admin/audit/exporters/:id.
type AuditExporterDeleteResponse struct {
	Deleted uint64 `json:"deleted"`
}

// AuditExporterTestResponse answers a successful test delivery.
type AuditExporterTestResponse struct {
	Delivered bool `json:"delivered"`
}

// AuditChainReport is the result of recomputing the tamper-evident audit hash
// chain. Rows written before the chain existed are the unchained prefix.
// BaseID/BaseHash name the last row deleted by audit retention, which the
// first retained row links to; BaseCreatedAt is that row's time and is null
// when retention has never deleted a row.
type AuditChainReport struct {
	OK             bool       `json:"ok"`
	RowsScanned    int64      `json:"rows_scanned"`
	ChainedRows    int64      `json:"chained_rows"`
	UnchainedRows  int64      `json:"unchained_rows"`
	FirstChainedID uint       `json:"first_chained_id,omitempty"`
	BaseID         uint       `json:"base_id,omitempty"`
	BaseHash       string     `json:"base_hash,omitempty"`
	BaseCreatedAt  *time.Time `json:"base_created_at" tstype:"string | null,required"`
	HeadID         uint       `json:"head_id,omitempty"`
	HeadHash       string     `json:"head_hash,omitempty"`
	BrokenAtID     uint       `json:"broken_at_id,omitempty"`
	Reason         string     `json:"reason,omitempty"`
	ScannedAt      time.Time  `json:"scanned_at"`
}

// AuditAnchorReport cross-checks chain-head checkpoints against the database.
// LatestCheckedAt is null before the first checkpoint. Pruned counts
// checkpoints whose rows audit retention deleted.
type AuditAnchorReport struct {
	Configured      bool       `json:"configured"`
	Path            string     `json:"path,omitempty"`
	Checkpoints     int        `json:"checkpoints"`
	InvalidLines    int        `json:"invalid_lines,omitempty"`
	Pruned          int        `json:"pruned,omitempty"`
	LatestCheckedAt *time.Time `json:"latest_checked_at" tstype:"string | null,required"`
	LatestHeadID    uint       `json:"latest_head_id,omitempty"`
	LatestHeadHash  string     `json:"latest_head_hash,omitempty"`
	OK              bool       `json:"ok"`
	BrokenAtID      uint       `json:"broken_at_id,omitempty"`
	Reason          string     `json:"reason,omitempty"`
}

// AuditAnchorFeed reports the checkpoint sinks. A *LastSuccessAt is null until
// that sink first succeeds.
type AuditAnchorFeed struct {
	FileConfigured      bool       `json:"file_configured"`
	FileLastSuccessAt   *time.Time `json:"file_last_success_at" tstype:"string | null,required"`
	FileLastError       string     `json:"file_last_error,omitempty"`
	RemoteConfigured    bool       `json:"remote_configured"`
	RemoteLastSuccessAt *time.Time `json:"remote_last_success_at" tstype:"string | null,required"`
	RemoteHeadID        uint       `json:"remote_head_id,omitempty"`
	RemoteLastError     string     `json:"remote_last_error,omitempty"`
}

// AuditIntegrityResponse answers GET /admin/audit/integrity.
type AuditIntegrityResponse struct {
	AnchorFeed AuditAnchorFeed   `json:"anchor_feed"`
	Anchors    AuditAnchorReport `json:"anchors"`
	Integrity  AuditChainReport  `json:"integrity"`
}
