package db

import "time"

// CompileCacheProtocolCCache is the migration default for legacy entries.
const CompileCacheProtocolCCache = "ccache"

// CompileCacheEntry is metadata for one opaque compiler-cache value. Compiler
// output lives in a storage root/bucket that is deliberately separate from the
// package-proxy cache; this row only powers capacity, LRU and observability.
type CompileCacheEntry struct {
	ID           uint      `gorm:"primarykey" json:"id"`
	Protocol     string    `gorm:"size:16;not null;default:ccache;uniqueIndex:idx_compile_cache_protocol_namespace_key,priority:1" json:"protocol"`
	Namespace    string    `gorm:"size:64;not null;uniqueIndex:idx_compile_cache_protocol_namespace_key,priority:2;index" json:"namespace"`
	Key          string    `gorm:"size:64;not null;uniqueIndex:idx_compile_cache_protocol_namespace_key,priority:3" json:"key"` // ccache: 33/40 chars; sccache: 64
	StoragePath  string    `gorm:"size:512;not null;uniqueIndex" json:"storage_path"`
	Size         int64     `gorm:"not null" json:"size"`
	Checksum     string    `gorm:"size:64" json:"checksum"`
	HitCount     int64     `gorm:"not null;default:0" json:"hit_count"`
	LastAccessed time.Time `gorm:"index;not null" json:"last_accessed"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CompileCacheCredential grants machine access to exactly one compiler-cache
// namespace. It is intentionally not an APIToken: a build worker must never
// gain access to Admin APIs just because it can populate compiler artifacts.
type CompileCacheCredential struct {
	ID          uint       `gorm:"primarykey" json:"id"`
	Name        string     `gorm:"size:128;not null;uniqueIndex:idx_compile_cache_credential_name,priority:2" json:"name"`
	Namespace   string     `gorm:"size:64;not null;uniqueIndex:idx_compile_cache_credential_name,priority:1;index" json:"namespace"`
	TokenHash   string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	Permissions string     `gorm:"size:16;not null" json:"permissions"`
	ExpiresAt   *time.Time `json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedBy   uint       `gorm:"not null;default:0" json:"created_by"`
	RevokedAt   *time.Time `gorm:"index" json:"revoked_at"`
	RevokedBy   *uint      `json:"revoked_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// CompileCacheDeletion is a durable object-deletion outbox. Metadata is
// removed transactionally with insertion of this row, then object storage is
// retried independently. This closes the crash/failure window where an S3 or
// filesystem delete fails after the logical cache entry has disappeared.
type CompileCacheDeletion struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	StoragePath string    `gorm:"size:512;not null;uniqueIndex" json:"storage_path"`
	NotBefore   time.Time `gorm:"index;not null" json:"not_before"`
	Attempts    int       `gorm:"not null;default:0" json:"attempts"`
	LastError   string    `gorm:"size:1024" json:"last_error"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
