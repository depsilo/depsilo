package contract

import "time"

// UpstreamProbeMode is how an Upstream's health is observed: active probes
// on an interval, or passive outcomes of real requests.
type UpstreamProbeMode string

const (
	UpstreamProbeModeActive  UpstreamProbeMode = "active"
	UpstreamProbeModePassive UpstreamProbeMode = "passive"
)

// UpstreamProxySource is where an Upstream's effective proxy comes from:
// empty for direct egress or a cascade parent, upstream for the Upstream's
// own proxy, default for egress.upstream_proxy.
type UpstreamProxySource string

const (
	UpstreamProxySourceDirect   UpstreamProxySource = ""
	UpstreamProxySourceUpstream UpstreamProxySource = "upstream"
	UpstreamProxySourceDefault  UpstreamProxySource = "default"
)

// UpstreamFailureCode classifies a failed Upstream check.
type UpstreamFailureCode string

const (
	UpstreamFailureCodeEgressFakeIP        UpstreamFailureCode = "egress_fake_ip"
	UpstreamFailureCodeEgressSharedAddress UpstreamFailureCode = "egress_shared_address"
	UpstreamFailureCodeEgressReserved      UpstreamFailureCode = "egress_reserved"
	UpstreamFailureCodeEgressPrivate       UpstreamFailureCode = "egress_private"
	UpstreamFailureCodeEgressProxy         UpstreamFailureCode = "egress_proxy"
	UpstreamFailureCodeDNS                 UpstreamFailureCode = "dns"
	UpstreamFailureCodeTimeout             UpstreamFailureCode = "timeout"
	UpstreamFailureCodeTLS                 UpstreamFailureCode = "tls"
	UpstreamFailureCodeConnectionRefused   UpstreamFailureCode = "connection_refused"
	UpstreamFailureCodeHTTPStatus          UpstreamFailureCode = "http_status"
	UpstreamFailureCodeNetwork             UpstreamFailureCode = "network"
)

// UpstreamFailure is a bounded explanation of a failed check. It carries no
// URL or credential, so every Admin role may see it.
type UpstreamFailure struct {
	Code    UpstreamFailureCode `json:"code"`
	Address string              `json:"address,omitempty"`
	Range   string              `json:"range,omitempty"`
	// Status is the HTTP status of http_status, or the egress proxy's CONNECT
	// answer for egress_proxy (absent when the proxy could not be reached).
	Status int `json:"status,omitempty"`
}

// AdminUpstream is one ordinary Upstream with its runtime health. URL and
// Proxy are credential-masked for roles that may not view credentials.
type AdminUpstream struct {
	ID            uint              `json:"id"`
	AdapterType   string            `json:"adapter_type"`
	Name          string            `json:"name"`
	URL           string            `json:"url"`
	Proxy         string            `json:"proxy"`
	Priority      int               `json:"priority"`
	ProbeMode     UpstreamProbeMode `json:"probe_mode"`
	ProbeInterval string            `json:"probe_interval"`
	// Via is the cascade peer name; empty means direct egress.
	Via          string  `json:"via"`
	Healthy      bool    `json:"healthy"`
	AvgLatencyMS int64   `json:"avg_latency_ms"`
	SuccessRate  float64 `json:"success_rate"`
	// LastCheckedAt is null before the first check.
	LastCheckedAt *time.Time `json:"last_checked_at" tstype:"string | null,required"`
	// HasObservations is observation evidence, independent of eligibility for
	// request selection.
	HasObservations bool `json:"has_observations"`
	// RuntimeSampleCount counts outcomes in this runtime object; it resets on
	// restart or reconfiguration.
	RuntimeSampleCount int64               `json:"runtime_sample_count"`
	WorkerRunning      bool                `json:"worker_running"`
	ProxySource        UpstreamProxySource `json:"proxy_source"`
	// LastFailure explains the newest failed check; null after a passing one.
	LastFailure *UpstreamFailure `json:"last_failure" tstype:"UpstreamFailure | null,required"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// AdminUpstreamListResponse answers GET /admin/upstreams.
type AdminUpstreamListResponse struct {
	Items []AdminUpstream `json:"items"`
	Total int             `json:"total"`
}

// DeleteUpstreamResponse answers DELETE /admin/upstreams/:id.
type DeleteUpstreamResponse struct {
	DeletedID   uint   `json:"deleted_id"`
	AdapterType string `json:"adapter_type"`
}

// UpstreamCheckResult is the outcome of one on-demand check.
type UpstreamCheckResult struct {
	Healthy   bool      `json:"healthy"`
	LatencyMS int64     `json:"latency_ms"`
	CheckedAt time.Time `json:"checked_at"`
	// Error is the raw failure text for credential viewers, a generic text for
	// other roles, and null after a passing check.
	Error   *string          `json:"error" tstype:"string | null,required"`
	Failure *UpstreamFailure `json:"failure" tstype:"UpstreamFailure | null,required"`
}

// CheckUpstreamResponse answers POST /admin/upstreams/:id/check.
type CheckUpstreamResponse struct {
	Upstream AdminUpstream       `json:"upstream"`
	Check    UpstreamCheckResult `json:"check"`
}
