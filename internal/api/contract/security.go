package contract

import "time"

// SecuritySeverity is an advisory's severity band. Unknown is an advisory
// whose source carried no usable score.
type SecuritySeverity string

const (
	SecuritySeverityCritical SecuritySeverity = "critical"
	SecuritySeverityHigh     SecuritySeverity = "high"
	SecuritySeverityMedium   SecuritySeverity = "medium"
	SecuritySeverityLow      SecuritySeverity = "low"
	SecuritySeverityUnknown  SecuritySeverity = "unknown"
)

// SecurityDashboard answers GET /admin/security/dashboard. BySeverity always
// carries critical, high, medium, and low, plus any other stored band.
// LastScanAt is null until a scan has completed.
type SecurityDashboard struct {
	TotalVulnerabilities int64            `json:"total_vulnerabilities"`
	AffectedPackages     int64            `json:"affected_packages"`
	BySeverity           map[string]int64 `json:"by_severity" tstype:"{ [severity in SecuritySeverity]?: number }"`
	AutoBlockedCount     int64            `json:"auto_blocked_count"`
	LastScanAt           *string          `json:"last_scan_at" tstype:"string | null,required"`
	ScanInProgress       bool             `json:"scan_in_progress"`
}

// SecurityVulnerability is one stored advisory for one package.
type SecurityVulnerability struct {
	ID             uint             `json:"id"`
	OSVID          string           `json:"osv_id"`
	Ecosystem      string           `json:"ecosystem"`
	PackageName    string           `json:"package_name"`
	AffectedRanges string           `json:"affected_ranges"`
	Severity       SecuritySeverity `json:"severity"`
	CVSSScore      float32          `json:"cvss_score"`
	Summary        string           `json:"summary"`
	Details        string           `json:"details"`
	Aliases        string           `json:"aliases"`
	References     string           `json:"references"`
	PublishedAt    time.Time        `json:"published_at"`
	ModifiedAt     time.Time        `json:"modified_at"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

// SecurityVulnerabilityCheck is the advisory lookup state of one package.
type SecurityVulnerabilityCheck struct {
	ID                 uint      `json:"id"`
	Ecosystem          string    `json:"ecosystem"`
	PackageName        string    `json:"package_name"`
	HasVulnerabilities bool      `json:"has_vulnerabilities"`
	VulnerabilityCount int       `json:"vulnerability_count"`
	LastFetchedAt      time.Time `json:"last_fetched_at"`
	NextFetchAt        time.Time `json:"next_fetch_at"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// SecurityVulnerabilityPage answers GET /admin/security/vulnerabilities.
type SecurityVulnerabilityPage struct {
	Items []SecurityVulnerability `json:"items"`
	Total int64                   `json:"total"`
	Page  int                     `json:"page"`
}

// SecuritySuggestionPage answers GET /admin/security/suggestions: scored
// advisories an operator has not dismissed, highest score first.
type SecuritySuggestionPage struct {
	Items []SecurityVulnerability `json:"items"`
	Total int64                   `json:"total"`
	Page  int                     `json:"page"`
}

// SecurityPackagePage answers GET /admin/security/packages: packages with at
// least one known advisory.
type SecurityPackagePage struct {
	Items []SecurityVulnerabilityCheck `json:"items"`
	Total int64                        `json:"total"`
	Page  int                          `json:"page"`
}

// ApproveSuggestionResponse answers approving a suggestion: the deny Package
// Rule it created.
type ApproveSuggestionResponse struct {
	RuleID uint `json:"rule_id"`
}

// DismissSuggestionResponse answers dismissing a suggestion.
type DismissSuggestionResponse struct {
	Status string `json:"status" tstype:"'dismissed'"`
}

// SecurityScanResponse answers POST /admin/security/scan (202): the scan runs
// in the background and the dashboard reports its progress.
type SecurityScanResponse struct {
	Status string `json:"status" tstype:"'scan_started'"`
}

// SecurityImportResponse answers an offline advisory import.
type SecurityImportResponse struct {
	Imported   int `json:"imported"`
	Received   int `json:"received"`
	Packages   int `json:"packages"`
	Duplicates int `json:"duplicates"`
	Skipped    int `json:"skipped"`
}

// SecurityPolicy is one ecosystem's advisory policy. GET
// /admin/security/policies answers an array of these.
type SecurityPolicy struct {
	ID               uint      `json:"id"`
	Ecosystem        string    `json:"ecosystem"`
	AutoBlockEnabled bool      `json:"auto_block_enabled"`
	MinCVSSScore     float32   `json:"min_cvss_score"`
	CreatedBy        string    `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
