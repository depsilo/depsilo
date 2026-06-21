package notify

import "time"

// Event types that can trigger a notification.
const (
	EventUpstreamDown    = "upstream_down"
	EventDiskHigh        = "disk_high"
	EventVulnCritical    = "vuln_critical"
	EventLicenseExpiring = "license_expiring"
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
