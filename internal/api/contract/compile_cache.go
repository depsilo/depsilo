package contract

import "time"

// CompileCachePermissions is what a compiler-cache credential may do.
type CompileCachePermissions string

const (
	CompileCachePermissionsReadOnly  CompileCachePermissions = "readonly"
	CompileCachePermissionsReadWrite CompileCachePermissions = "readwrite"
)

// CompileCacheEndpoints are the per-client base URLs for one namespace.
type CompileCacheEndpoints struct {
	CCache  string `json:"ccache"`
	SCCache string `json:"sccache"`
}

// CompileCacheStats is a point-in-time compiler-cache usage snapshot.
type CompileCacheStats struct {
	SizeBytes      int64 `json:"size_bytes"`
	MaxBytes       int64 `json:"max_bytes"`
	Entries        int64 `json:"entries"`
	MaxEntries     int64 `json:"max_entries"`
	Hits           int64 `json:"hits"`
	NamespaceCount int64 `json:"namespace_count"`
}

// CompileCacheStatus answers GET /admin/compile-cache/status. Endpoints and
// Stats are present exactly when Enabled is true.
type CompileCacheStatus struct {
	Enabled   bool                   `json:"enabled"`
	Endpoints *CompileCacheEndpoints `json:"endpoints,omitempty"`
	Stats     *CompileCacheStats     `json:"stats,omitempty"`
}

// CompileCacheCredential is a build credential without its token.
type CompileCacheCredential struct {
	ID          uint                    `json:"id"`
	Name        string                  `json:"name"`
	Namespace   string                  `json:"namespace"`
	Permissions CompileCachePermissions `json:"permissions"`
	ExpiresAt   *time.Time              `json:"expires_at" tstype:"string | null,required"`
	LastUsedAt  *time.Time              `json:"last_used_at" tstype:"string | null,required"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

// CompileCacheCredentialList answers GET /admin/compile-cache/credentials.
type CompileCacheCredentialList struct {
	Items []CompileCacheCredential `json:"items"`
	Total int                      `json:"total"`
}

// CreatedCompileCacheCredential answers credential creation. It is the only
// response that carries the token, already embedded in client configuration.
type CreatedCompileCacheCredential struct {
	ID                  uint                    `json:"id"`
	Name                string                  `json:"name"`
	Namespace           string                  `json:"namespace"`
	Permissions         CompileCachePermissions `json:"permissions"`
	ExpiresAt           *time.Time              `json:"expires_at" tstype:"string | null,required"`
	Token               string                  `json:"token"`
	Endpoints           CompileCacheEndpoints   `json:"endpoints"`
	CCacheRemoteStorage string                  `json:"ccache_remote_storage"`
	SCCacheConfig       string                  `json:"sccache_config"`
}

// CompileCacheCleanup answers a manual LRU reclamation.
type CompileCacheCleanup struct {
	RemovedEntries int   `json:"removed_entries"`
	ReclaimedBytes int64 `json:"reclaimed_bytes"`
	SizeBytes      int64 `json:"size_bytes"`
	Entries        int64 `json:"entries"`
}
