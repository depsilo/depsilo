package db

import "time"

type CacheEntry struct {
	ID           uint      `gorm:"primarykey" json:"id"`
	Key          string    `gorm:"uniqueIndex;size:512" json:"key"`
	AdapterType  string    `gorm:"size:16;index" json:"adapter_type"`
	PackageName  string    `gorm:"size:256;index" json:"package_name"`
	StoragePath  string    `gorm:"size:512" json:"storage_path"`
	Size         int64     `json:"size"`
	HitCount     int64     `gorm:"default:0" json:"hit_count"`
	ContentType  string    `gorm:"size:128" json:"content_type"`
	ExpiresAt    time.Time `gorm:"index" json:"expires_at"`
	LastAccessed time.Time `gorm:"index" json:"last_accessed"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AccessLog struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	AdapterType string    `gorm:"size:16;index" json:"adapter_type"`
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

type UpstreamRecord struct {
	ID            uint      `gorm:"primarykey" json:"id"`
	AdapterType   string    `gorm:"size:16;index" json:"adapter_type"`
	Name          string    `gorm:"size:128;uniqueIndex" json:"name"`
	URL           string    `gorm:"size:512" json:"url"`
	Proxy         string    `gorm:"size:256" json:"proxy"`
	Priority      int       `json:"priority"`
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
	UpstreamID uint      `gorm:"index" json:"upstream_id"`
	Name       string    `gorm:"size:128;index" json:"name"`
	LatencyMs  int64     `json:"latency_ms"`
	Healthy    bool      `json:"healthy"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

type AuditLog struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	Ecosystem   string    `gorm:"size:16;index" json:"ecosystem"`
	PackageName string    `gorm:"size:256;index" json:"package_name"`
	Version     string    `gorm:"size:128" json:"version"`
	Action      string    `gorm:"size:16" json:"action"`
	CacheResult string    `gorm:"size:8" json:"cache_result"`
	ClientIP    string    `gorm:"size:64;index" json:"client_ip"`
	UserAgent   string    `gorm:"size:256" json:"user_agent"`
	UpstreamURL string    `gorm:"size:512" json:"upstream_url"`
	LatencyMs   int64     `json:"latency_ms"`
	BytesSent   int64     `json:"bytes_sent"`
	StatusCode  int       `json:"status_code"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
}
