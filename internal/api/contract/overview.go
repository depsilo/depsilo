package contract

import "time"

// DashboardRange is an Overview period key. All covers everything the instance
// still knows: archived totals plus retained rollups.
type DashboardRange string

const (
	DashboardRange1h  DashboardRange = "1h"
	DashboardRange24h DashboardRange = "24h"
	DashboardRange7d  DashboardRange = "7d"
	DashboardRange30d DashboardRange = "30d"
	DashboardRangeAll DashboardRange = "all"
)

// DashboardRangeSpan is the resolved period a response covers, as RFC 3339
// instants. Archived is reported by GET /admin/dashboard for the all range
// only: it says whether the totals include expired days the trend omits.
type DashboardRangeSpan struct {
	Key      DashboardRange `json:"key"`
	Start    string         `json:"start"`
	End      string         `json:"end"`
	Archived *bool          `json:"archived,omitempty"`
}

// DashboardPeriod is one Overview period block. HitRate is 0 when there is no
// sample; a client treats TotalRequests == 0 as "no data", not 0%.
type DashboardPeriod struct {
	TotalRequests    int64   `json:"total_requests"`
	HitCount         int64   `json:"hit_count"`
	MissCount        int64   `json:"miss_count"`
	HitRequests      int64   `json:"hit_requests"`
	MissRequests     int64   `json:"miss_requests"`
	HitRate          float64 `json:"hit_rate"`
	BytesServed      int64   `json:"bytes_served"`
	HitBytes         int64   `json:"hit_bytes"`
	MissBytes        int64   `json:"miss_bytes"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	AvgHitLatencyMs  float64 `json:"avg_hit_latency_ms"`
	AvgMissLatencyMs float64 `json:"avg_miss_latency_ms"`
	// TimeSavedMs estimates the waiting avoided by serving from cache:
	// (avg miss latency - avg hit latency) x hit requests. Zero means no
	// comparable hit/miss pair in the period, not a measured zero.
	TimeSavedMs int64 `json:"time_saved_ms"`
	// UpstreamRequests and UpstreamBytes are the measured Depsilo-to-upstream
	// exchanges and bytes read in the period.
	UpstreamRequests int64 `json:"upstream_requests"`
	UpstreamBytes    int64 `json:"upstream_bytes"`
	Errors           int64 `json:"errors"`
}

// DashboardWindow is a rolling 24-hour headline block. HitCount is reported
// for the current 24 hours only, not for the comparison period.
type DashboardWindow struct {
	TotalRequests int64   `json:"total_requests"`
	HitCount      *int64  `json:"hit_count,omitempty"`
	HitRate       float64 `json:"hit_rate"`
	BytesServed   int64   `json:"bytes_served"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
}

// OriginCoverage says since when origin traffic has been measured and whether
// the selected period lies entirely inside that coverage.
type OriginCoverage struct {
	Measured       bool    `json:"measured"`
	Since          *string `json:"since" tstype:"string | null,required"`
	WindowComplete bool    `json:"window_complete"`
}

// DashboardUsageState is the lifetime artifact-usage state. NeverUsed requires
// full lifecycle coverage; legacy uncertainty is Unknown.
type DashboardUsageState string

const (
	DashboardUsageStateNeverUsed DashboardUsageState = "never-used"
	DashboardUsageStateUsed      DashboardUsageState = "used"
	DashboardUsageStateUnknown   DashboardUsageState = "unknown"
)

// DashboardUsage is completed artifact GET history, independent of statistics
// ranges and retention. FirstSuccessfulRequestAt may be null even for known
// usage when its original timestamp is unknown.
type DashboardUsage struct {
	State                    DashboardUsageState `json:"state"`
	FirstSuccessfulRequestAt *time.Time          `json:"first_successful_request_at" tstype:"string | null,required"`
}

// DashboardDailyStat is one (UTC day, ecosystem) request count.
type DashboardDailyStat struct {
	Date        string `json:"date"`
	AdapterType string `json:"adapter_type"`
	Count       int64  `json:"count"`
}

// DashboardUpstream is one live Upstream's in-memory health.
type DashboardUpstream struct {
	ID           uint    `json:"id"`
	Name         string  `json:"name"`
	Adapter      string  `json:"adapter"`
	Healthy      bool    `json:"healthy"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
	SuccessRate  float64 `json:"success_rate"`
}

// DashboardTopPackage is one of the most requested packages in an ecosystem.
type DashboardTopPackage struct {
	Name     string `json:"name"`
	HitCount int64  `json:"hit_count"`
}

// DashboardResponse answers GET /admin/dashboard. Prev is absent for the all
// range, which has no previous period; CacheUsagePercent is absent when the
// cache has no size quota.
type DashboardResponse struct {
	Usage          DashboardUsage       `json:"usage"`
	Range          DashboardRangeSpan   `json:"range"`
	Window         DashboardPeriod      `json:"window"`
	Prev           *DashboardPeriod     `json:"prev,omitempty"`
	OriginCoverage OriginCoverage       `json:"origin_coverage"`
	Last24h        DashboardWindow      `json:"last_24h"`
	Prev24h        DashboardWindow      `json:"prev_24h"`
	DailyStats     []DashboardDailyStat `json:"daily_stats"`
	Upstreams      []DashboardUpstream  `json:"upstreams"`
	// TopPackages is keyed by ecosystem.
	TopPackages       map[string][]DashboardTopPackage `json:"top_packages"`
	CacheUsagePercent *float64                         `json:"cache_usage_percent,omitempty"`
}

// DashboardTrendPoint is one bucket of the Overview trend. Bucket is unix
// seconds at the bucket's UTC start; Date is a legacy UTC display label.
type DashboardTrendPoint struct {
	Bucket      int64   `json:"bucket"`
	Date        string  `json:"date"`
	Requests    int64   `json:"requests"`
	Hits        int64   `json:"hits"`
	Misses      int64   `json:"misses"`
	HitRate     float64 `json:"hit_rate"`
	BytesServed int64   `json:"bytes_served"`
	BytesHit    int64   `json:"bytes_hit"`
	BytesMiss   int64   `json:"bytes_miss"`
	// UpstreamRequests and UpstreamBytes are the measured Depsilo-to-upstream
	// side of the bucket, never derived from the client-facing byte series.
	UpstreamRequests int64   `json:"upstream_requests"`
	UpstreamBytes    int64   `json:"upstream_bytes"`
	SumLatencyMs     int64   `json:"sum_latency_ms"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	Errors           int64   `json:"errors"`
}

// DashboardTrendsResponse answers GET /admin/dashboard/trends: a fixed-size
// series whose final point is the current unfinished bucket.
type DashboardTrendsResponse struct {
	Points []DashboardTrendPoint `json:"points"`
}

// DashboardBreakdownEcosystem is one ecosystem's share of an Overview period.
// The rows add up to the period block.
type DashboardBreakdownEcosystem struct {
	Ecosystem        string  `json:"ecosystem"`
	HitRequests      int64   `json:"hit_requests"`
	MissRequests     int64   `json:"miss_requests"`
	HitBytes         int64   `json:"hit_bytes"`
	MissBytes        int64   `json:"miss_bytes"`
	AvgHitLatencyMs  float64 `json:"avg_hit_latency_ms"`
	AvgMissLatencyMs float64 `json:"avg_miss_latency_ms"`
}

// DashboardBreakdownUpstream is one Upstream's part in the period.
// UpstreamRequests and UpstreamBytes are measured origin exchanges; the miss
// fields describe the client requests answered through it. Upstream names are
// unique only within an ecosystem.
type DashboardBreakdownUpstream struct {
	Ecosystem        string  `json:"ecosystem"`
	Upstream         string  `json:"upstream"`
	UpstreamRequests int64   `json:"upstream_requests"`
	UpstreamBytes    int64   `json:"upstream_bytes"`
	MissRequests     int64   `json:"miss_requests"`
	MissBytes        int64   `json:"miss_bytes"`
	AvgMissLatencyMs float64 `json:"avg_miss_latency_ms"`
}

// DashboardBreakdownPackage is one of the largest packages in the period.
type DashboardBreakdownPackage struct {
	Ecosystem   string `json:"ecosystem"`
	PackageName string `json:"package_name"`
	Requests    int64  `json:"requests"`
	TotalBytes  int64  `json:"total_bytes"`
	HitBytes    int64  `json:"hit_bytes"`
}

// DashboardBreakdownGrain is the package ranking's granularity. UTCDay
// rankings cover whole UTC days and can start before the range.
type DashboardBreakdownGrain string

const (
	DashboardBreakdownGrainExact  DashboardBreakdownGrain = "exact"
	DashboardBreakdownGrainUTCDay DashboardBreakdownGrain = "utc_day"
)

// DashboardBreakdownResponse answers GET /admin/dashboard/breakdown.
type DashboardBreakdownResponse struct {
	Range            DashboardRangeSpan            `json:"range"`
	ByEcosystem      []DashboardBreakdownEcosystem `json:"by_ecosystem"`
	ByUpstream       []DashboardBreakdownUpstream  `json:"by_upstream"`
	TopPackages      []DashboardBreakdownPackage   `json:"top_packages"`
	TopPackagesGrain DashboardBreakdownGrain       `json:"top_packages_grain"`
}

// RecentDownload is one recorded artifact download. It is narrower than an
// audit log: package activity, not client identity or upstream details.
type RecentDownload struct {
	ID          uint      `json:"id"`
	Ecosystem   string    `json:"ecosystem"`
	PackageName string    `json:"package_name"`
	Version     string    `json:"version"`
	CacheResult string    `json:"cache_result"`
	LatencyMs   int64     `json:"latency_ms"`
	BytesSent   int64     `json:"bytes_sent"`
	StatusCode  int       `json:"status_code"`
	CreatedAt   time.Time `json:"created_at"`
}

// RecentDownloadsResponse answers GET /admin/dashboard/recent-downloads,
// newest first.
type RecentDownloadsResponse struct {
	Items []RecentDownload `json:"items"`
}

// RuntimeCapability says whether one measurement is actually collected.
type RuntimeCapability struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

// RuntimeRSSBasis is whether RSSBytes is the current or the peak resident set.
type RuntimeRSSBasis string

const (
	RuntimeRSSBasisProcess RuntimeRSSBasis = "process"
	RuntimeRSSBasisPeak    RuntimeRSSBasis = "peak"
)

// RuntimeProcess is the sampled process (or container) resource block.
// CPUPercent is on a single-core basis: 100% is one full core.
type RuntimeProcess struct {
	CPU              RuntimeCapability `json:"cpu"`
	CPUPercent       *float64          `json:"cpu_percent,omitempty"`
	CPUCores         int               `json:"cpu_cores,omitempty"`
	Memory           RuntimeCapability `json:"memory"`
	RSSBytes         *int64            `json:"rss_bytes,omitempty"`
	RSSBasis         RuntimeRSSBasis   `json:"rss_basis,omitempty"`
	MemoryBasis      string            `json:"memory_basis,omitempty"`
	MemoryUsedBytes  *int64            `json:"memory_used_bytes,omitempty"`
	MemoryLimitBytes *int64            `json:"memory_limit_bytes,omitempty"`
}

// RuntimeDisk is the filesystem holding the local storage path.
type RuntimeDisk struct {
	RuntimeCapability `tstype:",extends"`
	Path              string `json:"path,omitempty"`
	TotalBytes        int64  `json:"total_bytes,omitempty"`
	FreeBytes         int64  `json:"free_bytes,omitempty"`
}

// RuntimeCacheStorage keeps the metadata and physical views of the cache
// distinct. Entries and Packages are null until an inventory aggregate has
// succeeded; PhysicalBytes is reserved for a real on-disk occupancy figure and
// is currently not collected.
type RuntimeCacheStorage struct {
	StorageType string `json:"storage_type"`
	StoragePath string `json:"storage_path"`
	// LogicalBytes is SUM(cache_entries.size).
	LogicalBytes  int64  `json:"logical_bytes"`
	Entries       *int64 `json:"entries" tstype:"number | null,required"`
	Packages      *int64 `json:"packages" tstype:"number | null,required"`
	QuotaBytes    *int64 `json:"quota_bytes" tstype:"number | null,required"`
	PhysicalBytes *int64 `json:"physical_bytes" tstype:"number | null,required"`
	PhysicalKnown bool   `json:"physical_known"`
}

// RuntimeGoMemory is the Go runtime's own memory view.
type RuntimeGoMemory struct {
	HeapAllocBytes int64 `json:"heap_alloc_bytes"`
	SysBytes       int64 `json:"sys_bytes"`
}

// RuntimeHost identifies the machine serving the response.
type RuntimeHost struct {
	Hostname string `json:"hostname"`
}

// RuntimeResponse answers GET /admin/runtime with the most recent shared
// resource sample plus cache storage facts.
type RuntimeResponse struct {
	SampledAt       time.Time           `json:"sampled_at"`
	IntervalSeconds int                 `json:"interval_seconds"`
	Process         RuntimeProcess      `json:"process"`
	GoRuntime       RuntimeGoMemory     `json:"go_runtime"`
	Cache           RuntimeCacheStorage `json:"cache"`
	Disk            RuntimeDisk         `json:"disk"`
	Host            RuntimeHost         `json:"host"`
}

// PolicyStatusKind is the package-rule snapshot's health. Unavailable means no
// usable snapshot has been observed yet; Unknown means no policy provider is
// wired.
type PolicyStatusKind string

const (
	PolicyStatusKindHealthy     PolicyStatusKind = "healthy"
	PolicyStatusKindDegraded    PolicyStatusKind = "degraded"
	PolicyStatusKindUnavailable PolicyStatusKind = "unavailable"
	PolicyStatusKindUnknown     PolicyStatusKind = "unknown"
)

// PolicyStatus answers GET /admin/policy/status: the freshness and fallback
// state of the package-rule snapshot used by the request path. A degraded
// status is still serving a known snapshot whose last refresh failed.
type PolicyStatus struct {
	Status             PolicyStatusKind `json:"status"`
	Degraded           bool             `json:"degraded"`
	UsingStaleSnapshot bool             `json:"using_stale_snapshot"`
	SnapshotLoadedAt   *string          `json:"snapshot_loaded_at" tstype:"string | null,required"`
	SnapshotAgeSeconds float64          `json:"snapshot_age_seconds"`
	RefreshFailures    uint64           `json:"refresh_failures"`
	OnLoadError        string           `json:"on_load_error"`
}

// CapabilitySupport is whether this build can provide a capability.
type CapabilitySupport string

const (
	CapabilitySupportSupported      CapabilitySupport = "supported"
	CapabilitySupportUnsupported    CapabilitySupport = "unsupported"
	CapabilitySupportSafetyDisabled CapabilitySupport = "safety_disabled"
)

// CapabilityDataStatus is the freshness of the data a capability depends on.
// SourceBound, Approximate, and Observed describe minimum-release-age
// provenance.
type CapabilityDataStatus string

const (
	CapabilityDataStatusNeverSynced CapabilityDataStatus = "never_synced"
	CapabilityDataStatusFresh       CapabilityDataStatus = "fresh"
	CapabilityDataStatusStale       CapabilityDataStatus = "stale"
	CapabilityDataStatusError       CapabilityDataStatus = "error"
	CapabilityDataStatusUnknown     CapabilityDataStatus = "unknown"
	CapabilityDataStatusSourceBound CapabilityDataStatus = "source_bound"
	CapabilityDataStatusApproximate CapabilityDataStatus = "approximate"
	CapabilityDataStatusObserved    CapabilityDataStatus = "observed"
)

// CapabilityFact is one runtime capability, per ecosystem where it applies
// (Ecosystem is empty for instance-wide facts). Mode is the configured
// enforcement: off, on, warn, block, or alert_only.
type CapabilityFact struct {
	Name          string               `json:"name"`
	Ecosystem     string               `json:"ecosystem"`
	Support       CapabilitySupport    `json:"support"`
	Mode          string               `json:"mode"`
	DataStatus    CapabilityDataStatus `json:"data_status"`
	LastSuccessAt *string              `json:"last_success_at,omitempty"`
	RecentFailure string               `json:"recent_failure,omitempty"`
}

// CapabilitySummary answers GET /admin/capabilities/summary: the build
// identity and every capability fact, with support, mode, and data freshness
// kept independent. Settings is the GET /admin/settings body, absent when the
// config store cannot provide it.
type CapabilitySummary struct {
	Version      string                 `json:"version"`
	Commit       string                 `json:"commit"`
	BuildDate    string                 `json:"build_date"`
	Capabilities []CapabilityFact       `json:"capabilities"`
	Settings     *AdminSettingsResponse `json:"settings,omitempty"`
}

// BandwidthRange is the report's inclusive UTC date range (YYYY-MM-DD).
type BandwidthRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// BandwidthSummary totals the report range. AvgHitLatency and AvgMissLatency
// are milliseconds.
type BandwidthSummary struct {
	TotalBytes     int64   `json:"total_bytes"`
	HitBytes       int64   `json:"hit_bytes"`
	MissBytes      int64   `json:"miss_bytes"`
	SavingsRate    float64 `json:"savings_rate"`
	TotalRequests  int64   `json:"total_requests"`
	HitRequests    int64   `json:"hit_requests"`
	MissRequests   int64   `json:"miss_requests"`
	TimeSavedMs    int64   `json:"time_saved_ms"`
	AvgHitLatency  float64 `json:"avg_hit_latency"`
	AvgMissLatency float64 `json:"avg_miss_latency"`
}

// BandwidthDaily is one UTC day of the report.
type BandwidthDaily struct {
	Date      string `json:"date"`
	HitBytes  int64  `json:"hit_bytes"`
	MissBytes int64  `json:"miss_bytes"`
	HitCount  int64  `json:"hit_count"`
	MissCount int64  `json:"miss_count"`
}

// BandwidthEcosystem is one ecosystem's share of the report.
type BandwidthEcosystem struct {
	Ecosystem        string  `json:"ecosystem"`
	HitBytes         int64   `json:"hit_bytes"`
	MissBytes        int64   `json:"miss_bytes"`
	HitCount         int64   `json:"hit_count"`
	MissCount        int64   `json:"miss_count"`
	AvgHitLatencyMs  float64 `json:"avg_hit_latency_ms"`
	AvgMissLatencyMs float64 `json:"avg_miss_latency_ms"`
}

// BandwidthPackage is one of the largest packages by bytes served.
type BandwidthPackage struct {
	PackageName  string `json:"package_name"`
	Ecosystem    string `json:"ecosystem"`
	TotalBytes   int64  `json:"total_bytes"`
	HitBytes     int64  `json:"hit_bytes"`
	RequestCount int64  `json:"request_count"`
}

// BandwidthUpstream is one Upstream's cache-miss traffic.
type BandwidthUpstream struct {
	Upstream     string  `json:"upstream"`
	MissBytes    int64   `json:"miss_bytes"`
	RequestCount int64   `json:"request_count"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}

// BandwidthReportResponse answers GET /admin/bandwidth.
type BandwidthReportResponse struct {
	Range       BandwidthRange       `json:"range"`
	Summary     BandwidthSummary     `json:"summary"`
	Daily       []BandwidthDaily     `json:"daily"`
	ByEcosystem []BandwidthEcosystem `json:"by_ecosystem"`
	TopPackages []BandwidthPackage   `json:"top_packages"`
	ByUpstream  []BandwidthUpstream  `json:"by_upstream"`
}

// AdminUpstreamLatencyPoint is one recorded Upstream health probe.
type AdminUpstreamLatencyPoint struct {
	Time      time.Time `json:"time"`
	LatencyMs int64     `json:"latency_ms"`
	Healthy   bool      `json:"healthy"`
}

// AdminUpstreamLatencySeries is one Upstream's recent probes, oldest first.
type AdminUpstreamLatencySeries struct {
	UpstreamID uint                        `json:"upstream_id"`
	Points     []AdminUpstreamLatencyPoint `json:"points"`
}

// AdminUpstreamLatenciesResponse answers GET /admin/upstreams/latency.
type AdminUpstreamLatenciesResponse struct {
	Series []AdminUpstreamLatencySeries `json:"series"`
}

// AdminUpstreamLatencyHistory answers GET /admin/upstreams/:id/latency.
type AdminUpstreamLatencyHistory struct {
	UpstreamName string                      `json:"upstream_name"`
	Points       []AdminUpstreamLatencyPoint `json:"points"`
}
