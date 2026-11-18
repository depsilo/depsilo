package contract

// NowStatus is the coarse live signal: Down when no Upstream is healthy,
// Degraded when some are not. An instance with no Upstreams is Healthy.
type NowStatus string

const (
	NowStatusHealthy  NowStatus = "healthy"
	NowStatusDegraded NowStatus = "degraded"
	NowStatusDown     NowStatus = "down"
)

// NowLastActivity is the most recent retained request, relative to now. It
// does not establish lifetime usage history.
type NowLastActivity struct {
	SecondsAgo  int64  `json:"seconds_ago"`
	AdapterType string `json:"adapter_type"`
	Hit         bool   `json:"hit"`
	PackageName string `json:"package_name,omitempty"`
}

// NowRate is the rolling-window rate block. Request rates follow
// client-to-Depsilo-to-upstream; byte rates follow the response direction.
// The two legs are never summed.
type NowRate struct {
	// RequestsPerMin is the rolling 60-second client request count.
	RequestsPerMin int64 `json:"requests_per_min"`
	// HasData is false when the window observed no requests.
	HasData bool `json:"has_data"`
	// Measured is true when the process meter supplied the per-second rates,
	// distinguishing idle from not collected.
	Measured              bool    `json:"measured"`
	WindowSeconds         int     `json:"window_seconds"`
	ServiceRequestsPerSec float64 `json:"service_requests_per_sec"`
	ServiceBytesPerSec    float64 `json:"service_bytes_per_sec"`
	OriginRequestsPerSec  float64 `json:"origin_requests_per_sec"`
	OriginBytesPerSec     float64 `json:"origin_bytes_per_sec"`
}

// NowUpstreams tallies healthy Upstreams across every wired pool.
type NowUpstreams struct {
	Total   int `json:"total"`
	Healthy int `json:"healthy"`
}

// NowSparklinePoint is one one-minute bucket; T is unix seconds at its start.
type NowSparklinePoint struct {
	T        int64 `json:"t"`
	Requests int64 `json:"requests"`
	Hits     int64 `json:"hits"`
}

// NowResponse answers GET /api/v1/now, the Overview's five-second live strip.
// Sparkline is 30 one-minute buckets ending at the current minute.
type NowResponse struct {
	Status        NowStatus           `json:"status"`
	UptimeSeconds int64               `json:"uptime_seconds"`
	NowUnix       int64               `json:"now_unix"`
	Version       string              `json:"version"`
	LastActivity  *NowLastActivity    `json:"last_activity,omitempty"`
	Rate          NowRate             `json:"rate"`
	Upstreams     NowUpstreams        `json:"upstreams"`
	Sparkline     []NowSparklinePoint `json:"sparkline"`
}

// StatsServiceStatus is whether Depsilo is serving. Degraded means some
// Upstream is unhealthy or slow; Failed means none is healthy.
type StatsServiceStatus string

const (
	StatsServiceStatusHealthy  StatsServiceStatus = "healthy"
	StatsServiceStatusDegraded StatsServiceStatus = "degraded"
	StatsServiceStatusFailed   StatsServiceStatus = "failed"
)

// StatsService identifies the running service.
type StatsService struct {
	Version       string             `json:"version"`
	UptimeSeconds int64              `json:"uptime_seconds"`
	Status        StatsServiceStatus `json:"status"`
}

// StatsToday covers the current UTC day.
type StatsToday struct {
	TotalRequests int64   `json:"total_requests"`
	HitCount      int64   `json:"hit_count"`
	MissCount     int64   `json:"miss_count"`
	HitRate       float64 `json:"hit_rate"`
	BytesServed   int64   `json:"bytes_served"`
	BytesSaved    int64   `json:"bytes_saved"`
}

// StatsWeek covers the six finished UTC days before today plus today.
type StatsWeek struct {
	TotalRequests int64   `json:"total_requests"`
	HitCount      int64   `json:"hit_count"`
	HitRate       float64 `json:"hit_rate"`
	BytesSaved    int64   `json:"bytes_saved"`
}

// StatsCache is the cache metadata inventory.
type StatsCache struct {
	TotalFiles     int64 `json:"total_files"`
	TotalSizeBytes int64 `json:"total_size_bytes"`
	PypiFiles      int64 `json:"pypi_files"`
	AptFiles       int64 `json:"apt_files"`
}

// StatsSeriesPoint is one bucket of the last hour. AvgLatencyMs averages
// cache misses only.
type StatsSeriesPoint struct {
	TimeUnix     int64   `json:"time_unix"`
	Requests     int64   `json:"requests"`
	Hits         int64   `json:"hits"`
	HitRate      float64 `json:"hit_rate"`
	BytesSaved   int64   `json:"bytes_saved"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}

// StatsSeries is the last hour in fixed buckets.
type StatsSeries struct {
	IntervalMinutes int                `json:"interval_minutes"`
	Points          []StatsSeriesPoint `json:"points"`
}

// StatsUpstream is one live Upstream; URL is its public origin only, without
// credentials or path.
type StatsUpstream struct {
	ID           uint    `json:"id"`
	Name         string  `json:"name"`
	Adapter      string  `json:"adapter"`
	URL          string  `json:"url"`
	Healthy      bool    `json:"healthy"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
	SuccessRate  float64 `json:"success_rate"`
}

// StatsExtraIndex is one configured extra package index route.
type StatsExtraIndex struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// StatsResponse answers the anonymous GET /api/v1/stats and is the MCP
// depsilo://stats resource.
type StatsResponse struct {
	Service      StatsService      `json:"service"`
	Today        StatsToday        `json:"today"`
	Week         StatsWeek         `json:"week"`
	Cache        StatsCache        `json:"cache"`
	Series       StatsSeries       `json:"series"`
	Upstreams    []StatsUpstream   `json:"upstreams"`
	ExtraIndexes []StatsExtraIndex `json:"extra_indexes"`
}

// StatsLatencyPoint is one bucket of an Upstream's probe history. Time is an
// RFC 3339 UTC instant; an empty bucket reports healthy with zero requests.
type StatsLatencyPoint struct {
	Time      string `json:"time"`
	LatencyMs int64  `json:"latency_ms"`
	Healthy   bool   `json:"healthy"`
	Requests  int64  `json:"requests"`
}

// StatsLatencySeries answers GET /api/v1/latency-series, keyed by Upstream ID,
// or legacy:<name> for probe rows recorded before Upstreams had IDs.
type StatsLatencySeries map[string][]StatsLatencyPoint

// ReadinessState is the service-level readiness verdict.
type ReadinessState string

const (
	ReadinessStateReady    ReadinessState = "ready"
	ReadinessStateNotReady ReadinessState = "not_ready"
)

// ReadinessCheck is one dependency's readiness.
type ReadinessCheck string

const (
	ReadinessCheckReady       ReadinessCheck = "ready"
	ReadinessCheckUnavailable ReadinessCheck = "unavailable"
)

// ReadinessChecks are the dependencies required to serve cached traffic;
// Upstreams are a separate axis.
type ReadinessChecks struct {
	Database ReadinessCheck `json:"database"`
	Storage  ReadinessCheck `json:"storage"`
}

// ReadinessPolicy is the public subset of the policy status. The snapshot
// fields are present once a snapshot has loaded; fallback mode and failures
// stay on the authenticated Admin route.
type ReadinessPolicy struct {
	Status             PolicyStatusKind `json:"status"`
	Degraded           bool             `json:"degraded"`
	UsingStaleSnapshot bool             `json:"using_stale_snapshot"`
	SnapshotLoadedAt   string           `json:"snapshot_loaded_at,omitempty"`
	SnapshotAgeSeconds *float64         `json:"snapshot_age_seconds,omitempty"`
}

// ReadinessResponse answers GET /ready with HTTP 200 when Status is ready and
// 503 otherwise. Policy is informative and never makes the service not ready.
type ReadinessResponse struct {
	Status  ReadinessState   `json:"status"`
	Version string           `json:"version"`
	Checks  ReadinessChecks  `json:"checks"`
	Policy  *ReadinessPolicy `json:"policy,omitempty"`
}
