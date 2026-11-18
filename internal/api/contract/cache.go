package contract

import "time"

// CacheKind separates mutable package-manager indexes from immutable artifacts.
type CacheKind string

const (
	CacheKindArtifact CacheKind = "artifact"
	CacheKindMetadata CacheKind = "metadata"
)

// CacheEntry is one cached object as the package-cache list reports it.
type CacheEntry struct {
	ID           uint      `json:"id"`
	Key          string    `json:"key"`
	AdapterType  string    `json:"adapter_type"`
	CacheKind    CacheKind `json:"cache_kind"`
	PackageName  string    `json:"package_name"`
	StoragePath  string    `json:"storage_path"`
	Size         int64     `json:"size"`
	HitCount     int64     `json:"hit_count"`
	ContentType  string    `json:"content_type"`
	ETag         string    `json:"etag"`
	LastModified string    `json:"last_modified"`
	ExpiresAt    time.Time `json:"expires_at"`
	LastAccessed time.Time `json:"last_accessed"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CacheListResponse answers GET /admin/cache.
type CacheListResponse struct {
	Total      int64            `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	Items      []CacheEntry     `json:"items"`
	Ecosystems []EcosystemCount `json:"ecosystems"`
}

// CacheDeleteResponse answers DELETE /admin/cache/:id.
type CacheDeleteResponse struct {
	Message        string `json:"message"`
	Deleted        int    `json:"deleted"`
	ReclaimedBytes int64  `json:"reclaimed_bytes"`
}

// CacheReclaimResponse answers POST /admin/cache/cleanup without a body: a
// broad manual reclaim under the retention policy.
type CacheReclaimResponse struct {
	Message        string `json:"message"`
	Deleted        int    `json:"deleted"`
	Failed         int    `json:"failed"`
	ReclaimedBytes int64  `json:"reclaimed_bytes"`
	Examined       int    `json:"examined"`
	ExpiredRemoved int    `json:"expired_removed"`
	LRURemoved     int    `json:"lru_removed"`
	UsageBefore    int64  `json:"usage_before"`
	UsageAfter     int64  `json:"usage_after"`
}

// CacheCleanupOutcome summarises how much of a confirmed cleanup plan ran.
type CacheCleanupOutcome string

const (
	CacheCleanupOutcomeSucceeded CacheCleanupOutcome = "succeeded"
	CacheCleanupOutcomePartial   CacheCleanupOutcome = "partial"
	CacheCleanupOutcomeFailed    CacheCleanupOutcome = "failed"
)

// CacheCleanupItemStatus is what happened to one planned entry.
type CacheCleanupItemStatus string

const (
	CacheCleanupItemStatusDeleted CacheCleanupItemStatus = "deleted"
	CacheCleanupItemStatusSkipped CacheCleanupItemStatus = "skipped"
	CacheCleanupItemStatusFailed  CacheCleanupItemStatus = "failed"
)

// CacheCleanupItemResult reports one planned entry of a confirmed cleanup.
type CacheCleanupItemResult struct {
	ID              uint                   `json:"id"`
	Status          CacheCleanupItemStatus `json:"status"`
	ObjectRemoved   bool                   `json:"object_removed"`
	MetadataRemoved bool                   `json:"metadata_removed"`
}

// CacheCleanupResponse answers POST /admin/cache/cleanup with a plan_id: the
// removal of exactly the entries a cleanup preview planned.
type CacheCleanupResponse struct {
	Message             string                   `json:"message"`
	Outcome             CacheCleanupOutcome      `json:"outcome"`
	Interrupted         bool                     `json:"interrupted"`
	Deleted             int                      `json:"deleted"`
	Skipped             int                      `json:"skipped"`
	Failed              int                      `json:"failed"`
	ReclaimedBytes      int64                    `json:"reclaimed_bytes"`
	Planned             int                      `json:"planned"`
	PlannedCount        int                      `json:"planned_count"`
	PlannedBytes        int64                    `json:"planned_bytes"`
	TotalCandidateCount int64                    `json:"total_candidate_count"`
	NotAttempted        int                      `json:"not_attempted"`
	Items               []CacheCleanupItemResult `json:"items"`
}

// CacheCleanupReason is why the retention policy selected an entry.
type CacheCleanupReason string

const (
	CacheCleanupReasonExpired CacheCleanupReason = "expired"
	CacheCleanupReasonLRU     CacheCleanupReason = "lru"
)

// CacheCleanupPreviewItem is one entry a cleanup would remove.
type CacheCleanupPreviewItem struct {
	ID           uint               `json:"id"`
	Key          string             `json:"key"`
	AdapterType  string             `json:"adapter_type"`
	PackageName  string             `json:"package_name"`
	Size         int64              `json:"size"`
	HitCount     int64              `json:"hit_count"`
	LastAccessed time.Time          `json:"last_accessed"`
	ExpiresAt    time.Time          `json:"expires_at"`
	Reason       CacheCleanupReason `json:"reason"`
}

// CacheCleanupPreviewResponse answers GET /admin/cache/cleanup/preview. The
// plan it registers lets one later cleanup remove exactly these items.
type CacheCleanupPreviewResponse struct {
	GeneratedAt          time.Time                 `json:"generated_at"`
	UsageBytes           int64                     `json:"usage_bytes"`
	ThresholdBytes       int64                     `json:"threshold_bytes"`
	TargetBytes          int64                     `json:"target_bytes"`
	LogicalBytes         int64                     `json:"logical_bytes"`
	CandidateCount       int64                     `json:"candidate_count"`
	PlannedCount         int                       `json:"planned_count"`
	PlannedBytes         int64                     `json:"planned_bytes"`
	PhysicalUsageKnown   bool                      `json:"physical_usage_known"`
	PhysicalUsageMessage string                    `json:"physical_usage_message,omitempty"`
	Items                []CacheCleanupPreviewItem `json:"items"`
	Page                 int                       `json:"page"`
	PageSize             int                       `json:"page_size"`
	PlanID               string                    `json:"plan_id"`
	PlanExpiresAt        time.Time                 `json:"plan_expires_at"`
}

// CacheTypeBreakdown is the cached bytes and files of one ecosystem.
type CacheTypeBreakdown struct {
	Type      string `json:"type"`
	Size      int64  `json:"size"`
	FileCount int64  `json:"file_count"`
}

// CachePackageSize is the cached bytes and hits of one package.
type CachePackageSize struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	HitCount int64  `json:"hit_count"`
}

// CacheDistributionResponse answers GET /admin/cache/distribution.
type CacheDistributionResponse struct {
	TotalSize    int64                `json:"total_size"`
	MaxSize      int64                `json:"max_size"`
	UsagePercent float64              `json:"usage_percent"`
	ByType       []CacheTypeBreakdown `json:"by_type"`
	TopPackages  []CachePackageSize   `json:"top_packages"`
}

// CacheTransferStatus is the stage of one live upstream download.
type CacheTransferStatus string

const (
	CacheTransferStatusConnecting  CacheTransferStatus = "connecting"
	CacheTransferStatusDownloading CacheTransferStatus = "downloading"
	CacheTransferStatusResuming    CacheTransferStatus = "resuming"
	CacheTransferStatusSaving      CacheTransferStatus = "saving"
	CacheTransferStatusCached      CacheTransferStatus = "cached"
	CacheTransferStatusFailed      CacheTransferStatus = "failed"
)

// CacheTransfer is the progress of one upstream download into the cache.
// TotalBytes is -1 until the upstream announces a length.
type CacheTransfer struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Ecosystem       string              `json:"ecosystem"`
	Upstream        string              `json:"upstream"`
	Status          CacheTransferStatus `json:"status"`
	DownloadedBytes int64               `json:"downloaded_bytes"`
	TotalBytes      int64               `json:"total_bytes"`
	ResumeAttempts  int                 `json:"resume_attempts"`
	ResumeOffset    int64               `json:"resume_offset"`
	BytesPerSecond  float64             `json:"bytes_per_second"`
	StartedAt       time.Time           `json:"started_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
	FinishedAt      *time.Time          `json:"finished_at,omitempty"`
}

// CacheTransfersResponse answers GET /admin/cache/transfers.
type CacheTransfersResponse struct {
	Items []CacheTransfer `json:"items"`
	Limit int             `json:"limit"`
}

// CacheIndexStatus says whether a cached index is still within its TTL.
type CacheIndexStatus string

const (
	CacheIndexStatusFresh CacheIndexStatus = "fresh"
	CacheIndexStatusStale CacheIndexStatus = "stale"
)

// CacheIndexEntry is one cached package-manager index.
type CacheIndexEntry struct {
	ID               uint             `json:"id"`
	Key              string           `json:"key"`
	AdapterType      string           `json:"adapter_type"`
	PackageName      string           `json:"package_name"`
	Size             int64            `json:"size"`
	HitCount         int64            `json:"hit_count"`
	ETag             string           `json:"etag"`
	LastModified     string           `json:"last_modified"`
	LastAccessed     time.Time        `json:"last_accessed"`
	ExpiresAt        time.Time        `json:"expires_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	Status           CacheIndexStatus `json:"status"`
	RefreshSupported bool             `json:"refresh_supported"`
}

// CacheIndexSummary counts the cached indexes of one ecosystem.
type CacheIndexSummary struct {
	AdapterType string  `json:"adapter_type"`
	Total       int64   `json:"total"`
	Fresh       int64   `json:"fresh"`
	Stale       int64   `json:"stale"`
	LastUpdated *string `json:"last_updated" tstype:"string | null,required"`
}

// CacheIndexListResponse answers GET /admin/cache/indexes.
type CacheIndexListResponse struct {
	Items      []CacheIndexEntry   `json:"items"`
	Total      int64               `json:"total"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"page_size"`
	Summary    []CacheIndexSummary `json:"summary"`
	Ecosystems []EcosystemCount    `json:"ecosystems"`
}

// CacheIndexRefreshResponse answers POST /admin/cache/indexes/:id/refresh.
type CacheIndexRefreshResponse struct {
	Message string `json:"message"`
	ID      uint   `json:"id"`
}
