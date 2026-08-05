package db

import (
	"fmt"
	"slices"
	"time"

	"gorm.io/gorm"
)

const (
	schemaV1CompileCacheProtocol = "ccache"
	schemaV1CacheKindMetadata    = "metadata"
	schemaV1CacheKindArtifact    = "artifact"
)

func migrateBaselineSchema(database *gorm.DB) error {
	if err := database.AutoMigrate(schemaV1Models()...); err != nil {
		return err
	}
	// Version 1 is not committed until its repairable invariants have also
	// succeeded. If an interrupted legacy index swap is invalid, the enclosing
	// transaction leaves the ledger at version zero so the full migration can
	// be retried after the operator fixes the conflicting index.
	return ensureSchemaV1Invariants(database)
}

func ensureSchemaV1Invariants(database *gorm.DB) error {
	if err := migrateSchemaV1CompileCacheIdentityIndex(database); err != nil {
		return err
	}
	if err := migrateSchemaV1VulnerabilityIdentityIndex(database); err != nil {
		return err
	}
	if err := backfillSchemaV1CacheKinds(database); err != nil {
		return err
	}
	return backfillSchemaV1HuggingFacePackageNames(database)
}

func migrateSchemaV1CompileCacheIdentityIndex(database *gorm.DB) error {
	const (
		legacyIndex      = "idx_compile_cache_namespace_key"
		replacementIndex = "idx_compile_cache_protocol_namespace_key"
	)
	wantColumns := []string{"protocol", "namespace", "key"}

	return database.Transaction(func(tx *gorm.DB) error {
		// SQLite applies the column default while adding protocol to the legacy
		// table. Keep this explicit backfill for databases left in a partial
		// migration state, without touching storage_path or timestamps.
		if err := tx.Exec(
			"UPDATE compile_cache_entries SET protocol = ? WHERE protocol IS NULL OR TRIM(protocol) = ''",
			schemaV1CompileCacheProtocol,
		).Error; err != nil {
			return fmt.Errorf("backfill compiler-cache protocol: %w", err)
		}

		indexes, err := tx.Migrator().GetIndexes("compile_cache_entries")
		if err != nil {
			return fmt.Errorf("read compiler-cache indexes: %w", err)
		}
		var replacement gorm.Index
		for _, index := range indexes {
			if index.Name() == replacementIndex {
				replacement = index
				break
			}
		}
		if replacement == nil {
			return fmt.Errorf("replacement compiler-cache index %q is missing", replacementIndex)
		}
		unique, known := replacement.Unique()
		if !known || !unique || !slices.Equal(replacement.Columns(), wantColumns) {
			return fmt.Errorf(
				"replacement compiler-cache index %q has unique=%v columns=%v, want unique=true columns=%v",
				replacementIndex,
				unique,
				replacement.Columns(),
				wantColumns,
			)
		}

		if !tx.Migrator().HasIndex("compile_cache_entries", legacyIndex) {
			return nil
		}
		if err := tx.Migrator().DropIndex("compile_cache_entries", legacyIndex); err != nil {
			return fmt.Errorf("drop legacy compiler-cache index %q: %w", legacyIndex, err)
		}
		return nil
	})
}

func migrateSchemaV1VulnerabilityIdentityIndex(database *gorm.DB) error {
	const (
		legacyIndex      = "idx_vulnerabilities_osv_id"
		replacementIndex = "idx_vuln_osv_eco_pkg"
	)
	wantColumns := []string{"osv_id", "ecosystem", "package_name"}

	return database.Transaction(func(tx *gorm.DB) error {
		indexes, err := tx.Migrator().GetIndexes("vulnerabilities")
		if err != nil {
			return fmt.Errorf("read vulnerability indexes: %w", err)
		}

		var replacement gorm.Index
		for _, index := range indexes {
			if index.Name() == replacementIndex {
				replacement = index
				break
			}
		}
		if replacement == nil {
			return fmt.Errorf("replacement vulnerability index %q is missing", replacementIndex)
		}
		unique, known := replacement.Unique()
		if !known || !unique || !slices.Equal(replacement.Columns(), wantColumns) {
			return fmt.Errorf(
				"replacement vulnerability index %q has unique=%v columns=%v, want unique=true columns=%v",
				replacementIndex,
				unique,
				replacement.Columns(),
				wantColumns,
			)
		}

		if !tx.Migrator().HasIndex("vulnerabilities", legacyIndex) {
			return nil
		}
		if err := tx.Migrator().DropIndex("vulnerabilities", legacyIndex); err != nil {
			return fmt.Errorf("drop legacy vulnerability index %q: %w", legacyIndex, err)
		}
		return nil
	})
}

// schemaV1Models is the immutable schema snapshot for migration 1.
//
// These deliberately duplicate the database-facing shape of the domain
// models. Do not replace them with aliases or add fields when a domain model
// changes: add a new numbered migration instead. Explicit TableName methods
// keep future Go renames and GORM naming changes from rewriting migration 1.
func schemaV1Models() []any {
	return []any{
		&schemaV1CacheEntry{},
		&schemaV1HuggingFaceRefPin{},
		&schemaV1HuggingFaceRepositoryRevocation{},
		&schemaV1CompileCacheEntry{},
		&schemaV1CompileCacheCredential{},
		&schemaV1CompileCacheDeletion{},
		&schemaV1UpstreamUpdateEvent{},
		&schemaV1AccessLog{},
		&schemaV1AccessLogFiveMinutely{},
		&schemaV1AccessLogHourly{},
		&schemaV1AccessLogDaily{},
		&schemaV1AccessLogPackageDaily{},
		&schemaV1UpstreamRecord{},
		&schemaV1ControlPlaneState{},
		&schemaV1User{},
		&schemaV1APIToken{},
		&schemaV1UpstreamLatencyLog{},
		&schemaV1AuditLog{},
		&schemaV1PackageRule{},
		&schemaV1Vulnerability{},
		&schemaV1VulnerabilityCheck{},
		&schemaV1SecurityPolicy{},
		&schemaV1DismissedVuln{},
		&schemaV1Project{},
		&schemaV1ProjectPackage{},
		&schemaV1TrialRecord{},
		&schemaV1LicenseStorage{},
		&schemaV1WebhookConfig{},
		&schemaV1PackageTimestamp{},
		&schemaV1ApprovedVersion{},
		&schemaV1QuarantineEvent{},
		&schemaV1MaliciousPackage{},
		&schemaV1MalwareOverride{},
		&schemaV1BlocklistSyncState{},
		&schemaV1TamperRecord{},
	}
}

type schemaV1CacheEntry struct {
	ID              uint   `gorm:"primarykey"`
	Key             string `gorm:"uniqueIndex;size:512"`
	AdapterType     string `gorm:"size:16;index"`
	CacheKind       string `gorm:"size:16;index"`
	PackageName     string `gorm:"size:256;index"`
	StoragePath     string `gorm:"size:512"`
	Size            int64
	HitCount        int64     `gorm:"default:0"`
	ContentType     string    `gorm:"size:128"`
	ETag            string    `gorm:"column:etag;size:512"`
	LastModified    string    `gorm:"size:128"`
	ResponseHeaders string    `gorm:"type:text"`
	ExpiresAt       time.Time `gorm:"index"`
	LastAccessed    time.Time `gorm:"index"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (schemaV1CacheEntry) TableName() string { return "cache_entries" }

type schemaV1HuggingFaceRefPin struct {
	Key       string    `gorm:"primaryKey;size:512"`
	Commit    string    `gorm:"size:40;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (schemaV1HuggingFaceRefPin) TableName() string { return "hugging_face_ref_pins" }

type schemaV1HuggingFaceRepositoryRevocation struct {
	Repository  string `gorm:"primaryKey;size:256"`
	EscapedRepo string `gorm:"size:512;not null"`
	Token       string `gorm:"size:32;not null"`
	CleanupSafe bool   `gorm:"not null;default:false"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (schemaV1HuggingFaceRepositoryRevocation) TableName() string {
	return "hugging_face_repository_revocations"
}

type schemaV1CompileCacheEntry struct {
	ID           uint      `gorm:"primarykey"`
	Protocol     string    `gorm:"size:16;not null;default:ccache;uniqueIndex:idx_compile_cache_protocol_namespace_key,priority:1"`
	Namespace    string    `gorm:"size:64;not null;uniqueIndex:idx_compile_cache_protocol_namespace_key,priority:2;index"`
	Key          string    `gorm:"size:64;not null;uniqueIndex:idx_compile_cache_protocol_namespace_key,priority:3"`
	StoragePath  string    `gorm:"size:512;not null;uniqueIndex"`
	Size         int64     `gorm:"not null"`
	Checksum     string    `gorm:"size:64"`
	HitCount     int64     `gorm:"not null;default:0"`
	LastAccessed time.Time `gorm:"index;not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (schemaV1CompileCacheEntry) TableName() string { return "compile_cache_entries" }

type schemaV1CompileCacheCredential struct {
	ID          uint   `gorm:"primarykey"`
	Name        string `gorm:"size:128;not null;uniqueIndex:idx_compile_cache_credential_name,priority:2"`
	Namespace   string `gorm:"size:64;not null;uniqueIndex:idx_compile_cache_credential_name,priority:1;index"`
	TokenHash   string `gorm:"size:64;not null;uniqueIndex"`
	Permissions string `gorm:"size:16;not null"`
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	CreatedBy   uint       `gorm:"not null;default:0"`
	RevokedAt   *time.Time `gorm:"index"`
	RevokedBy   *uint
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (schemaV1CompileCacheCredential) TableName() string { return "compile_cache_credentials" }

type schemaV1CompileCacheDeletion struct {
	ID          uint      `gorm:"primarykey"`
	StoragePath string    `gorm:"size:512;not null;uniqueIndex"`
	NotBefore   time.Time `gorm:"index;not null"`
	Attempts    int       `gorm:"not null;default:0"`
	LastError   string    `gorm:"size:1024"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (schemaV1CompileCacheDeletion) TableName() string { return "compile_cache_deletions" }

type schemaV1UpstreamUpdateEvent struct {
	ID              uint   `gorm:"primarykey;index:idx_upstream_update_subject_order,priority:2,sort:desc;index:idx_upstream_update_order,priority:2,sort:desc"`
	CacheEntryID    uint   `gorm:"index:idx_upstream_update_subject_order,priority:1"`
	Ecosystem       string `gorm:"size:32;index"`
	Upstream        string `gorm:"size:128;index"`
	Package         string `gorm:"size:256"`
	Result          string `gorm:"size:24;index"`
	Detail          string `gorm:"size:512"`
	LatencyMs       int64
	OccurrenceCount uint64 `gorm:"not null;default:1"`
	LastSeenAt      time.Time
	CreatedAt       time.Time `gorm:"index:idx_upstream_update_order,priority:1,sort:desc"`
}

func (schemaV1UpstreamUpdateEvent) TableName() string { return "upstream_update_events" }

type schemaV1AccessLog struct {
	ID          uint   `gorm:"primarykey"`
	AdapterType string `gorm:"size:16;index"`
	Method      string `gorm:"size:8"`
	CacheKey    string `gorm:"size:512"`
	PackageName string `gorm:"size:256;index"`
	Hit         bool   `gorm:"index"`
	Upstream    string `gorm:"size:128"`
	LatencyMs   int64
	StatusCode  int
	ClientIP    string `gorm:"size:64"`
	BytesSent   int64
	CreatedAt   time.Time `gorm:"index"`
}

func (schemaV1AccessLog) TableName() string { return "access_logs" }

type schemaV1AccessLogFiveMinutely struct {
	BucketStart  int64  `gorm:"primaryKey"`
	AdapterType  string `gorm:"size:16;primaryKey"`
	Hit          bool   `gorm:"primaryKey"`
	Upstream     string `gorm:"size:128;primaryKey;default:''"`
	RequestCount int64
	TotalBytes   int64
	SumLatencyMs int64
	ErrorCount   int64
	UpdatedAt    time.Time
}

func (schemaV1AccessLogFiveMinutely) TableName() string { return "access_log_five_minutely" }

type schemaV1AccessLogHourly struct {
	Date         string `gorm:"size:10;primaryKey"`
	Hour         int    `gorm:"primaryKey"`
	AdapterType  string `gorm:"size:16;primaryKey"`
	Hit          bool   `gorm:"primaryKey"`
	Upstream     string `gorm:"size:128;primaryKey;default:''"`
	RequestCount int64
	TotalBytes   int64
	SumLatencyMs int64
	ErrorCount   int64
	UpdatedAt    time.Time
}

func (schemaV1AccessLogHourly) TableName() string { return "access_log_hourly" }

type schemaV1AccessLogDaily struct {
	Date         string `gorm:"size:10;primaryKey"`
	AdapterType  string `gorm:"size:16;primaryKey"`
	Hit          bool   `gorm:"primaryKey"`
	Upstream     string `gorm:"size:128;primaryKey;default:''"`
	RequestCount int64
	TotalBytes   int64
	SumLatencyMs int64
	ErrorCount   int64
	UpdatedAt    time.Time
}

func (schemaV1AccessLogDaily) TableName() string { return "access_log_daily" }

type schemaV1AccessLogPackageDaily struct {
	Date         string `gorm:"size:10;primaryKey"`
	AdapterType  string `gorm:"size:16;primaryKey"`
	PackageName  string `gorm:"size:256;primaryKey"`
	Hit          bool   `gorm:"primaryKey"`
	RequestCount int64
	TotalBytes   int64
	UpdatedAt    time.Time
}

func (schemaV1AccessLogPackageDaily) TableName() string { return "access_log_package_daily" }

type schemaV1UpstreamRecord struct {
	ID            uint   `gorm:"primarykey"`
	AdapterType   string `gorm:"size:16;index;uniqueIndex:idx_upstream_name_type"`
	Name          string `gorm:"size:128;uniqueIndex:idx_upstream_name_type"`
	URL           string `gorm:"size:512"`
	Proxy         string `gorm:"size:256"`
	Priority      int
	ProbeMode     string `gorm:"size:16;default:'active'"`
	ProbeInterval string `gorm:"size:16;default:'30m'"`
	Healthy       bool   `gorm:"default:true"`
	AvgLatencyMs  int64
	SuccessRate   float64
	LastCheckedAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (schemaV1UpstreamRecord) TableName() string { return "upstream_records" }

type schemaV1ControlPlaneState struct {
	Key       string `gorm:"primaryKey;size:128"`
	Value     string `gorm:"type:text;not null"`
	UpdatedAt time.Time
}

func (schemaV1ControlPlaneState) TableName() string { return "control_plane_states" }

type schemaV1User struct {
	ID           uint   `gorm:"primarykey"`
	Username     string `gorm:"uniqueIndex;size:64"`
	PasswordHash string `gorm:"size:256"`
	Role         string `gorm:"size:16;default:'readonly'"`
	Enabled      bool   `gorm:"default:true"`
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (schemaV1User) TableName() string { return "users" }

type schemaV1APIToken struct {
	ID          uint   `gorm:"primarykey"`
	UserID      uint   `gorm:"index"`
	Name        string `gorm:"size:128"`
	TokenHash   string `gorm:"uniqueIndex;size:256"`
	Permissions string `gorm:"size:32"`
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	CreatedAt   time.Time
}

func (schemaV1APIToken) TableName() string { return "api_tokens" }

type schemaV1UpstreamLatencyLog struct {
	ID         uint   `gorm:"primarykey"`
	UpstreamID uint   `gorm:"index:idx_upstream_latency_upstream_created,priority:1"`
	Name       string `gorm:"size:128;index"`
	LatencyMs  int64
	Healthy    bool
	CreatedAt  time.Time `gorm:"index;index:idx_upstream_latency_upstream_created,priority:2"`
}

func (schemaV1UpstreamLatencyLog) TableName() string { return "upstream_latency_logs" }

type schemaV1PackageRule struct {
	ID          uint   `gorm:"primarykey"`
	Ecosystem   string `gorm:"size:16;index"`
	PackageName string `gorm:"size:256"`
	Version     string `gorm:"size:128"`
	Action      string `gorm:"size:8"`
	Reason      string `gorm:"size:512"`
	CreatedBy   string `gorm:"size:64"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (schemaV1PackageRule) TableName() string { return "package_rules" }

type schemaV1AuditLog struct {
	ID          uint   `gorm:"primarykey;index:idx_audit_action_order,priority:2,sort:desc"`
	Ecosystem   string `gorm:"size:16;index"`
	PackageName string `gorm:"size:256;index"`
	Version     string `gorm:"size:128"`
	Action      string `gorm:"size:16;index:idx_audit_action_order,priority:1"`
	CacheResult string `gorm:"size:8"`
	ClientIP    string `gorm:"size:64;index"`
	UserAgent   string `gorm:"size:256"`
	UpstreamURL string `gorm:"size:512"`
	LatencyMs   int64
	BytesSent   int64
	StatusCode  int
	CreatedAt   time.Time `gorm:"index"`
}

func (schemaV1AuditLog) TableName() string { return "audit_logs" }

type schemaV1Vulnerability struct {
	ID             uint      `gorm:"primarykey"`
	OSVID          string    `gorm:"size:64;uniqueIndex:idx_vuln_osv_eco_pkg,priority:1"`
	Ecosystem      string    `gorm:"size:16;index;uniqueIndex:idx_vuln_osv_eco_pkg,priority:2"`
	PackageName    string    `gorm:"size:256;index;uniqueIndex:idx_vuln_osv_eco_pkg,priority:3"`
	AffectedRanges string    `gorm:"type:text"`
	Severity       string    `gorm:"size:16;index"`
	CVSSScore      float32   `gorm:"default:0"`
	Summary        string    `gorm:"type:text"`
	Details        string    `gorm:"type:text"`
	Aliases        string    `gorm:"size:512"`
	References     string    `gorm:"type:text"`
	PublishedAt    time.Time `gorm:"index"`
	ModifiedAt     time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (schemaV1Vulnerability) TableName() string { return "vulnerabilities" }

type schemaV1VulnerabilityCheck struct {
	ID                 uint      `gorm:"primarykey"`
	Ecosystem          string    `gorm:"size:16;uniqueIndex:idx_vuln_check_eco_pkg"`
	PackageName        string    `gorm:"size:256;uniqueIndex:idx_vuln_check_eco_pkg"`
	HasVulnerabilities bool      `gorm:"default:false"`
	VulnerabilityCount int       `gorm:"default:0"`
	LastFetchedAt      time.Time `gorm:"index"`
	NextFetchAt        time.Time `gorm:"index"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (schemaV1VulnerabilityCheck) TableName() string { return "vulnerability_checks" }

type schemaV1SecurityPolicy struct {
	ID               uint    `gorm:"primarykey"`
	Ecosystem        string  `gorm:"size:16;uniqueIndex"`
	AutoBlockEnabled bool    `gorm:"default:false"`
	MinCVSSScore     float32 `gorm:"default:9.0"`
	CreatedBy        string  `gorm:"size:64"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (schemaV1SecurityPolicy) TableName() string { return "security_policies" }

type schemaV1DismissedVuln struct {
	ID              uint   `gorm:"primarykey"`
	VulnerabilityID uint   `gorm:"uniqueIndex:idx_dismissed"`
	DismissedBy     string `gorm:"size:64"`
	CreatedAt       time.Time
}

func (schemaV1DismissedVuln) TableName() string { return "dismissed_vulns" }

type schemaV1Project struct {
	ID          uint   `gorm:"primarykey"`
	Name        string `gorm:"size:128;uniqueIndex"`
	Slug        string `gorm:"size:128;uniqueIndex"`
	Description string `gorm:"size:512"`
	TokenHash   string `gorm:"size:256;uniqueIndex"`
	CreatedBy   string `gorm:"size:64"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (schemaV1Project) TableName() string { return "projects" }

type schemaV1ProjectPackage struct {
	ID            uint   `gorm:"primarykey"`
	ProjectID     uint   `gorm:"index;uniqueIndex:idx_proj_pkg"`
	Ecosystem     string `gorm:"size:16;uniqueIndex:idx_proj_pkg"`
	PackageName   string `gorm:"size:256;uniqueIndex:idx_proj_pkg"`
	Version       string `gorm:"size:128;uniqueIndex:idx_proj_pkg"`
	FirstSeenAt   time.Time
	LastSeenAt    time.Time `gorm:"index"`
	DownloadCount int       `gorm:"default:1"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (schemaV1ProjectPackage) TableName() string { return "project_packages" }

type schemaV1TrialRecord struct {
	ID            uint      `gorm:"primarykey"`
	ActivatedAt   time.Time `gorm:"not null"`
	ExpiresAt     time.Time `gorm:"not null"`
	ActivatedBy   uint      `gorm:"index"`
	ActivatedFrom string    `gorm:"size:64"`
	CreatedAt     time.Time
}

func (schemaV1TrialRecord) TableName() string { return "trial_records" }

type schemaV1LicenseStorage struct {
	ID        uint   `gorm:"primarykey"`
	Key       string `gorm:"size:256"`
	UpdatedBy uint   `gorm:"index"`
	UpdatedAt time.Time
}

func (schemaV1LicenseStorage) TableName() string { return "license_storages" }

type schemaV1WebhookConfig struct {
	ID              uint   `gorm:"primarykey"`
	Name            string `gorm:"size:128"`
	Platform        string `gorm:"size:16"`
	URL             string `gorm:"size:512"`
	Secret          string `gorm:"size:256"`
	Enabled         bool   `gorm:"default:true"`
	Events          string `gorm:"size:256;default:'*'"`
	CooldownMinutes int    `gorm:"default:30"`
	LastSentAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (schemaV1WebhookConfig) TableName() string { return "webhook_configs" }

type schemaV1PackageTimestamp struct {
	Ecosystem string    `gorm:"size:32;primaryKey"`
	Package   string    `gorm:"size:256;primaryKey"`
	Version   string    `gorm:"size:128;primaryKey"`
	PublishAt time.Time `gorm:"index"`
	CreatedAt time.Time
}

func (schemaV1PackageTimestamp) TableName() string { return "package_timestamp" }

type schemaV1ApprovedVersion struct {
	ID         uint      `gorm:"primarykey"`
	Ecosystem  string    `gorm:"size:32;uniqueIndex:idx_approved_eco_pkg_ver,priority:1"`
	Package    string    `gorm:"size:256;uniqueIndex:idx_approved_eco_pkg_ver,priority:2"`
	Version    string    `gorm:"size:128;uniqueIndex:idx_approved_eco_pkg_ver,priority:3"`
	Reason     string    `gorm:"size:512"`
	ApprovedBy uint      `gorm:"index"`
	CreatedAt  time.Time `gorm:"index"`
}

func (schemaV1ApprovedVersion) TableName() string { return "approved_version" }

type schemaV1QuarantineEvent struct {
	ID        uint   `gorm:"primarykey"`
	Ecosystem string `gorm:"size:32;index:idx_qevent_eco_created,priority:1"`
	Package   string `gorm:"size:256;index"`
	Version   string `gorm:"size:128"`
	Action    string `gorm:"size:32;index"`
	Reason    string `gorm:"size:512"`
	Threshold int64
	AgeAtCall int64
	ActorID   uint      `gorm:"index"`
	ClientIP  string    `gorm:"size:64"`
	CreatedAt time.Time `gorm:"index:idx_qevent_eco_created,priority:2"`
}

func (schemaV1QuarantineEvent) TableName() string { return "quarantine_event" }

type schemaV1MaliciousPackage struct {
	ID         uint   `gorm:"primarykey"`
	SourceID   string `gorm:"size:64;uniqueIndex:idx_mal_src_eco_pkg,priority:1"`
	Ecosystem  string `gorm:"size:32;uniqueIndex:idx_mal_src_eco_pkg,priority:2;index:idx_mal_eco_pkg,priority:1"`
	Package    string `gorm:"size:256;uniqueIndex:idx_mal_src_eco_pkg,priority:3;index:idx_mal_eco_pkg,priority:2"`
	Versions   string `gorm:"type:text"`
	Aliases    string `gorm:"size:512"`
	Summary    string `gorm:"size:512"`
	Modified   time.Time
	ImportedAt time.Time `gorm:"index"`
}

func (schemaV1MaliciousPackage) TableName() string { return "malicious_package" }

type schemaV1MalwareOverride struct {
	ID        uint   `gorm:"primarykey"`
	Ecosystem string `gorm:"size:32;index:idx_override_eco_pkg,priority:1"`
	Package   string `gorm:"size:256;index:idx_override_eco_pkg,priority:2"`
	Version   string `gorm:"size:128"`
	Reason    string `gorm:"size:512"`
	ActorID   uint   `gorm:"index"`
	CreatedAt time.Time
	ExpiresAt time.Time `gorm:"index"`
}

func (schemaV1MalwareOverride) TableName() string { return "malware_override" }

type schemaV1BlocklistSyncState struct {
	ID            uint `gorm:"primarykey"`
	LastSyncAt    *time.Time
	LastSuccessAt *time.Time
	LastError     string `gorm:"size:512"`
	EntryCount    int64
	DurationMs    int64
}

func (schemaV1BlocklistSyncState) TableName() string { return "blocklist_sync_state" }

type schemaV1TamperRecord struct {
	Key            string `gorm:"size:512;primaryKey"`
	Ecosystem      string `gorm:"size:32;index"`
	Package        string `gorm:"size:256;index"`
	Version        string `gorm:"size:128"`
	SHA256         string `gorm:"size:64"`
	Size           int64
	FirstSeenAt    time.Time
	LastVerifiedAt time.Time
	VerifyCount    int64
}

func (schemaV1TamperRecord) TableName() string { return "tamper_record" }
