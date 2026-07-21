package db

import "time"

const (
	CacheKindMetadata = "metadata"
	CacheKindArtifact = "artifact"
)

type CacheEntry struct {
	ID           uint      `gorm:"primarykey" json:"id"`
	Key          string    `gorm:"uniqueIndex;size:512" json:"key"`
	AdapterType  string    `gorm:"size:16;index" json:"adapter_type"`
	CacheKind    string    `gorm:"size:16;index" json:"cache_kind"` // CacheKindMetadata | CacheKindArtifact
	PackageName  string    `gorm:"size:256;index" json:"package_name"`
	StoragePath  string    `gorm:"size:512" json:"storage_path"`
	Size         int64     `json:"size"`
	HitCount     int64     `gorm:"default:0" json:"hit_count"`
	ContentType  string    `gorm:"size:128" json:"content_type"`
	ETag         string    `gorm:"column:etag;size:512" json:"etag"`
	LastModified string    `gorm:"size:128" json:"last_modified"`
	ExpiresAt    time.Time `gorm:"index" json:"expires_at"`
	LastAccessed time.Time `gorm:"index" json:"last_accessed"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UpstreamUpdateEvent records proactive metadata checks for cached packages.
// It is intentionally separate from access/audit logs: the actor is the
// scheduler, not an end user.
type UpstreamUpdateEvent struct {
	ID              uint      `gorm:"primarykey;index:idx_upstream_update_subject_order,priority:2,sort:desc;index:idx_upstream_update_order,priority:2,sort:desc" json:"id"`
	CacheEntryID    uint      `gorm:"index:idx_upstream_update_subject_order,priority:1" json:"cache_entry_id"`
	Ecosystem       string    `gorm:"size:32;index" json:"ecosystem"`
	Upstream        string    `gorm:"size:128;index" json:"upstream"`
	Package         string    `gorm:"size:256" json:"package"`
	Result          string    `gorm:"size:24;index" json:"result"`
	Detail          string    `gorm:"size:512" json:"detail"`
	LatencyMs       int64     `json:"latency_ms"`
	OccurrenceCount uint64    `gorm:"not null;default:1" json:"occurrence_count"`
	LastSeenAt      time.Time `json:"last_seen_at"`
	CreatedAt       time.Time `gorm:"index:idx_upstream_update_order,priority:1,sort:desc" json:"created_at"`
}

type AccessLog struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	AdapterType string    `gorm:"size:16;index" json:"adapter_type"`
	Method      string    `gorm:"size:8" json:"method"`
	CacheKey    string    `gorm:"size:512" json:"cache_key"`
	PackageName string    `gorm:"size:256;index" json:"package_name"`
	Hit         bool      `gorm:"index" json:"hit"`
	Upstream    string    `gorm:"size:128" json:"upstream"`
	LatencyMs   int64     `json:"latency_ms"`
	StatusCode  int       `json:"status_code"`
	ClientIP    string    `gorm:"size:64" json:"client_ip"`
	BytesSent   int64     `json:"bytes_sent"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
}

type AccessLogFiveMinutely struct {
	BucketStart  int64     `gorm:"primaryKey" json:"bucket_start"`
	AdapterType  string    `gorm:"size:16;primaryKey" json:"adapter_type"`
	Hit          bool      `gorm:"primaryKey" json:"hit"`
	Upstream     string    `gorm:"size:128;primaryKey;default:''" json:"upstream"`
	RequestCount int64     `json:"request_count"`
	TotalBytes   int64     `json:"total_bytes"`
	SumLatencyMs int64     `json:"sum_latency_ms"`
	ErrorCount   int64     `json:"error_count"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AccessLogHourly is the hourly rollup that powers today/recent dashboards.
// (date, hour, adapter_type, hit, upstream) is the composite PK so UPSERT
// can accumulate counters. PackageName is intentionally absent: combined
// with the other dimensions it would grow this table by 4-5 orders of
// magnitude. Package grain lives in AccessLogPackageDaily.
// All times are UTC, matching db.Open's NowFunc.
type AccessLogHourly struct {
	Date         string    `gorm:"size:10;primaryKey" json:"date"`
	Hour         int       `gorm:"primaryKey" json:"hour"`
	AdapterType  string    `gorm:"size:16;primaryKey" json:"adapter_type"`
	Hit          bool      `gorm:"primaryKey" json:"hit"`
	Upstream     string    `gorm:"size:128;primaryKey;default:''" json:"upstream"`
	RequestCount int64     `json:"request_count"`
	TotalBytes   int64     `json:"total_bytes"`
	SumLatencyMs int64     `json:"sum_latency_ms"`
	ErrorCount   int64     `json:"error_count"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AccessLogDaily is rolled up by the nightly compactor from
// AccessLogHourly. It powers 7d/30d/90d dashboards without scanning the
// hourly table's 24x rows per day.
type AccessLogDaily struct {
	Date         string    `gorm:"size:10;primaryKey" json:"date"`
	AdapterType  string    `gorm:"size:16;primaryKey" json:"adapter_type"`
	Hit          bool      `gorm:"primaryKey" json:"hit"`
	Upstream     string    `gorm:"size:128;primaryKey;default:''" json:"upstream"`
	RequestCount int64     `json:"request_count"`
	TotalBytes   int64     `json:"total_bytes"`
	SumLatencyMs int64     `json:"sum_latency_ms"`
	ErrorCount   int64     `json:"error_count"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AccessLogPackageDaily is the package-grain daily rollup, kept separate
// to bound row count when distinct package_name is large. Powers
// top_packages views.
type AccessLogPackageDaily struct {
	Date         string    `gorm:"size:10;primaryKey" json:"date"`
	AdapterType  string    `gorm:"size:16;primaryKey" json:"adapter_type"`
	PackageName  string    `gorm:"size:256;primaryKey" json:"package_name"`
	Hit          bool      `gorm:"primaryKey" json:"hit"`
	RequestCount int64     `json:"request_count"`
	TotalBytes   int64     `json:"total_bytes"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName overrides — GORM pluralizes by default and would turn these
// into access_log_hourlies / access_log_dailies / access_log_package_dailies,
// which break the raw SQL written against the singular form.
func (AccessLogFiveMinutely) TableName() string { return "access_log_five_minutely" }
func (AccessLogHourly) TableName() string       { return "access_log_hourly" }
func (AccessLogDaily) TableName() string        { return "access_log_daily" }
func (AccessLogPackageDaily) TableName() string { return "access_log_package_daily" }

type UpstreamRecord struct {
	ID            uint      `gorm:"primarykey" json:"id"`
	AdapterType   string    `gorm:"size:16;index;uniqueIndex:idx_upstream_name_type" json:"adapter_type"`
	Name          string    `gorm:"size:128;uniqueIndex:idx_upstream_name_type" json:"name"`
	URL           string    `gorm:"size:512" json:"url"`
	Proxy         string    `gorm:"size:256" json:"proxy"`
	Priority      int       `json:"priority"`
	ProbeMode     string    `gorm:"size:16;default:'active'" json:"probe_mode"`
	ProbeInterval string    `gorm:"size:16;default:'30m'" json:"probe_interval"`
	Healthy       bool      `gorm:"default:true" json:"healthy"`
	AvgLatencyMs  int64     `json:"avg_latency_ms"`
	SuccessRate   float64   `json:"success_rate"`
	LastCheckedAt time.Time `json:"last_checked_at"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type User struct {
	ID           uint       `gorm:"primarykey" json:"id"`
	Username     string     `gorm:"uniqueIndex;size:64" json:"username"`
	PasswordHash string     `gorm:"size:256" json:"-"`
	Role         string     `gorm:"size:16;default:'readonly'" json:"role"`
	Enabled      bool       `gorm:"default:true" json:"enabled"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type APIToken struct {
	ID          uint       `gorm:"primarykey" json:"id"`
	UserID      uint       `gorm:"index" json:"user_id"`
	Name        string     `gorm:"size:128" json:"name"`
	TokenHash   string     `gorm:"uniqueIndex;size:256" json:"-"`
	Permissions string     `gorm:"size:32" json:"permissions"`
	ExpiresAt   *time.Time `json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type UpstreamLatencyLog struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	UpstreamID uint      `gorm:"index:idx_upstream_latency_upstream_created,priority:1" json:"upstream_id"`
	Name       string    `gorm:"size:128;index" json:"name"`
	LatencyMs  int64     `json:"latency_ms"`
	Healthy    bool      `json:"healthy"`
	CreatedAt  time.Time `gorm:"index;index:idx_upstream_latency_upstream_created,priority:2" json:"created_at"`
}

type PackageRule struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	Ecosystem   string    `gorm:"size:16;index" json:"ecosystem"`
	PackageName string    `gorm:"size:256" json:"package_name"`
	Version     string    `gorm:"size:128" json:"version"`
	Action      string    `gorm:"size:8" json:"action"`
	Reason      string    `gorm:"size:512" json:"reason"`
	CreatedBy   string    `gorm:"size:64" json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AuditLog struct {
	ID          uint      `gorm:"primarykey;index:idx_audit_action_order,priority:2,sort:desc" json:"id"`
	Ecosystem   string    `gorm:"size:16;index" json:"ecosystem"`
	PackageName string    `gorm:"size:256;index" json:"package_name"`
	Version     string    `gorm:"size:128" json:"version"`
	Action      string    `gorm:"size:16;index:idx_audit_action_order,priority:1" json:"action"`
	CacheResult string    `gorm:"size:8" json:"cache_result"`
	ClientIP    string    `gorm:"size:64;index" json:"client_ip"`
	UserAgent   string    `gorm:"size:256" json:"user_agent"`
	UpstreamURL string    `gorm:"size:512" json:"upstream_url"`
	LatencyMs   int64     `json:"latency_ms"`
	BytesSent   int64     `json:"bytes_sent"`
	StatusCode  int       `json:"status_code"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
}

type Vulnerability struct {
	ID             uint      `gorm:"primarykey" json:"id"`
	OSVID          string    `gorm:"size:64;uniqueIndex:idx_vuln_osv_eco_pkg,priority:1" json:"osv_id"`
	Ecosystem      string    `gorm:"size:16;index;uniqueIndex:idx_vuln_osv_eco_pkg,priority:2" json:"ecosystem"`
	PackageName    string    `gorm:"size:256;index;uniqueIndex:idx_vuln_osv_eco_pkg,priority:3" json:"package_name"`
	AffectedRanges string    `gorm:"type:text" json:"affected_ranges"`
	Severity       string    `gorm:"size:16;index" json:"severity"`
	CVSSScore      float32   `gorm:"default:0" json:"cvss_score"`
	Summary        string    `gorm:"type:text" json:"summary"`
	Details        string    `gorm:"type:text" json:"details"`
	Aliases        string    `gorm:"size:512" json:"aliases"`
	References     string    `gorm:"type:text" json:"references"`
	PublishedAt    time.Time `gorm:"index" json:"published_at"`
	ModifiedAt     time.Time `json:"modified_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type VulnerabilityCheck struct {
	ID                 uint      `gorm:"primarykey" json:"id"`
	Ecosystem          string    `gorm:"size:16;uniqueIndex:idx_vuln_check_eco_pkg" json:"ecosystem"`
	PackageName        string    `gorm:"size:256;uniqueIndex:idx_vuln_check_eco_pkg" json:"package_name"`
	HasVulnerabilities bool      `gorm:"default:false" json:"has_vulnerabilities"`
	VulnerabilityCount int       `gorm:"default:0" json:"vulnerability_count"`
	LastFetchedAt      time.Time `gorm:"index" json:"last_fetched_at"`
	NextFetchAt        time.Time `gorm:"index" json:"next_fetch_at"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type SecurityPolicy struct {
	ID               uint      `gorm:"primarykey" json:"id"`
	Ecosystem        string    `gorm:"size:16;uniqueIndex" json:"ecosystem"`
	AutoBlockEnabled bool      `gorm:"default:false" json:"auto_block_enabled"`
	MinCVSSScore     float32   `gorm:"default:9.0" json:"min_cvss_score"`
	CreatedBy        string    `gorm:"size:64" json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type DismissedVuln struct {
	ID              uint      `gorm:"primarykey" json:"id"`
	VulnerabilityID uint      `gorm:"uniqueIndex:idx_dismissed" json:"vulnerability_id"`
	DismissedBy     string    `gorm:"size:64" json:"dismissed_by"`
	CreatedAt       time.Time `json:"created_at"`
}

type Project struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	Name        string    `gorm:"size:128;uniqueIndex" json:"name"`
	Slug        string    `gorm:"size:128;uniqueIndex" json:"slug"`
	Description string    `gorm:"size:512" json:"description"`
	TokenHash   string    `gorm:"size:256;uniqueIndex" json:"-"`
	CreatedBy   string    `gorm:"size:64" json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ProjectPackage struct {
	ID            uint      `gorm:"primarykey" json:"id"`
	ProjectID     uint      `gorm:"index;uniqueIndex:idx_proj_pkg" json:"project_id"`
	Ecosystem     string    `gorm:"size:16;uniqueIndex:idx_proj_pkg" json:"ecosystem"`
	PackageName   string    `gorm:"size:256;uniqueIndex:idx_proj_pkg" json:"package_name"`
	Version       string    `gorm:"size:128;uniqueIndex:idx_proj_pkg" json:"version"`
	FirstSeenAt   time.Time `json:"first_seen_at"`
	LastSeenAt    time.Time `gorm:"index" json:"last_seen_at"`
	DownloadCount int       `gorm:"default:1" json:"download_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// TrialRecord persists the local 14-day Pro trial state. Singleton (at most one
// row, ID = 1). Uniqueness is enforced by the manager-layer mutex + count check,
// not by a DB constraint, because Depsilo is single-instance today.
type TrialRecord struct {
	ID            uint      `gorm:"primarykey" json:"id"`
	ActivatedAt   time.Time `gorm:"not null" json:"activated_at"`
	ExpiresAt     time.Time `gorm:"not null" json:"expires_at"`
	ActivatedBy   uint      `gorm:"index" json:"activated_by"`     // FK to User.ID; admin who clicked
	ActivatedFrom string    `gorm:"size:64" json:"activated_from"` // client IP, reserved for future abuse analysis
	CreatedAt     time.Time `json:"created_at"`
}

// LicenseStorage persists a license key that was set via the admin UI.
// Takes precedence over the config.toml-loaded key when both exist.
// Singleton (ID = 1). License keys are identifiers, not credentials —
// stored as plaintext; masked only for display.
type LicenseStorage struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Key       string    `gorm:"size:256" json:"key"`
	UpdatedBy uint      `gorm:"index" json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WebhookConfig stores a webhook endpoint configuration.
// Each webhook subscribes to one or more event types and formats
// payloads for a specific platform (Slack, DingTalk, WeCom, Feishu, or generic).
type WebhookConfig struct {
	ID              uint       `gorm:"primarykey" json:"id"`
	Name            string     `gorm:"size:128" json:"name"`    // human label, e.g. "Ops DingTalk"
	Platform        string     `gorm:"size:16" json:"platform"` // slack | dingtalk | wecom | feishu | generic
	URL             string     `gorm:"size:512" json:"url"`     // incoming webhook URL
	Secret          string     `gorm:"size:256" json:"-"`       // optional HMAC secret (reserved)
	Enabled         bool       `gorm:"default:true" json:"enabled"`
	Events          string     `gorm:"size:256;default:'*'" json:"events"` // comma-separated or '*'
	CooldownMinutes int        `gorm:"default:30" json:"cooldown_minutes"`
	LastSentAt      *time.Time `json:"last_sent_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
