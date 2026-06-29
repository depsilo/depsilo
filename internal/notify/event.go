package notify

import "time"

// Event types that can trigger a notification.
const (
	EventUpstreamDown    = "upstream_down"
	EventDiskHigh        = "disk_high"
	EventVulnCritical    = "vuln_critical"
	EventLicenseExpiring = "license_expiring"
	// EventQuarantineBlocked fires when the minimum-release-age policy
	// refuses to serve a package. The webhook payload includes the
	// (ecosystem, package, version) triple plus the observed age and
	// configured threshold so the receiving channel can route the
	// alert to whoever owns the affected build pipeline. See
	// docs/DIRECTION.md §Task 1 and ADR-0003.
	EventQuarantineBlocked = "quarantine_blocked"
)

// Event represents a notification-worthy occurrence in Depsilo.
type Event struct {
	Type      string    `json:"type"`
	Severity  string    `json:"severity"` // critical | warning | info
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Detail    string    `json:"detail,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
