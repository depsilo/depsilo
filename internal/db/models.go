package db

import "time"

type CacheEntry struct {
	ID          uint      `gorm:"primarykey"`
	Key         string    `gorm:"uniqueIndex;size:512"`
	AdapterType string    `gorm:"size:16;index"`
	StoragePath string    `gorm:"size:512"`
	Size        int64
	HitCount    int64     `gorm:"default:0"`
	ContentType string    `gorm:"size:128"`
	ExpiresAt   time.Time `gorm:"index"`
	LastAccessed time.Time `gorm:"index"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type AccessLog struct {
	ID          uint      `gorm:"primarykey"`
	AdapterType string    `gorm:"size:16;index"`
	CacheKey    string    `gorm:"size:512"`
	PackageName string    `gorm:"size:256;index"`
	Hit         bool      `gorm:"index"`
	Upstream    string    `gorm:"size:128"`
	LatencyMs   int64
	StatusCode  int
	ClientIP    string    `gorm:"size:64"`
	BytesSent   int64
	CreatedAt   time.Time `gorm:"index"`
}

type UpstreamRecord struct {
	ID            uint      `gorm:"primarykey"`
	AdapterType   string    `gorm:"size:16;index"`
	Name          string    `gorm:"size:128;uniqueIndex"`
	URL           string    `gorm:"size:512"`
	Proxy         string    `gorm:"size:256"`
	Priority      int
	Healthy       bool      `gorm:"default:true"`
	AvgLatencyMs  int64
	SuccessRate   float64
	LastCheckedAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type User struct {
	ID           uint       `gorm:"primarykey"`
	Username     string     `gorm:"uniqueIndex;size:64"`
	PasswordHash string     `gorm:"size:256"`
	Role         string     `gorm:"size:16;default:'readonly'"`
	Enabled      bool       `gorm:"default:true"`
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type APIToken struct {
	ID          uint       `gorm:"primarykey"`
	UserID      uint       `gorm:"index"`
	Name        string     `gorm:"size:128"`
	TokenHash   string     `gorm:"uniqueIndex;size:256"`
	Permissions string     `gorm:"size:32"`
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	CreatedAt   time.Time
}
