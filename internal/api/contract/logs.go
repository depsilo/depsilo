package contract

import "time"

// EcosystemCount is one entry of a list's ecosystem facet: how many rows the
// current filters match in that ecosystem, ignoring the ecosystem filter
// itself. Access-log, audit-log, cache, and quarantine lists share it.
type EcosystemCount struct {
	ID    string `json:"id"`
	Count int64  `json:"count"`
}

// AccessLogCacheResult is an access log's cache outcome. Rows written before
// the outcome was recorded report hit from their legacy hit flag, else unknown.
type AccessLogCacheResult string

const (
	AccessLogCacheResultHit     AccessLogCacheResult = "hit"
	AccessLogCacheResultMiss    AccessLogCacheResult = "miss"
	AccessLogCacheResultUnknown AccessLogCacheResult = "unknown"
)

// AccessLog is one client request as the access-log list reports it.
type AccessLog struct {
	ID          uint                 `json:"id"`
	AdapterType string               `json:"adapter_type"`
	Method      string               `json:"method"`
	CacheKey    string               `json:"cache_key"`
	PackageName string               `json:"package_name"`
	Version     string               `json:"version"`
	Hit         bool                 `json:"hit"`
	CacheResult AccessLogCacheResult `json:"cache_result"`
	Upstream    string               `json:"upstream"`
	LatencyMs   int64                `json:"latency_ms"`
	StatusCode  int                  `json:"status_code"`
	ClientIP    string               `json:"client_ip"`
	BytesSent   int64                `json:"bytes_sent"`
	// UpstreamRequests and UpstreamBytes measure the Depsilo-to-Upstream
	// traffic attributed to this client request.
	UpstreamRequests int64     `json:"upstream_requests"`
	UpstreamBytes    int64     `json:"upstream_bytes"`
	CreatedAt        time.Time `json:"created_at"`
}

// AccessLogListResponse answers GET /admin/logs. Ecosystems counts the rows
// every other filter matches, per ecosystem; it is null when no row matches.
type AccessLogListResponse struct {
	Items      []AccessLog      `json:"items"`
	Total      int64            `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	Ecosystems []EcosystemCount `json:"ecosystems" tstype:"EcosystemCount[] | null"`
}

// AccessLogDetail answers GET /admin/logs/:id: the list row plus the request's
// cache, policy, and delivery decisions.
// CacheResult repeats the embedded field so the key keeps its position after
// request_id on the wire.
type AccessLogDetail struct {
	AccessLog      `tstype:",extends"`
	RequestID      string               `json:"request_id"`
	CacheResult    AccessLogCacheResult `json:"cache_result"`
	CacheReason    string               `json:"cache_reason"`
	PolicyDecision string               `json:"policy_decision"`
	PolicyReason   string               `json:"policy_reason"`
	DeliveryResult string               `json:"delivery_result"`
	DeliveryReason string               `json:"delivery_reason"`
}
