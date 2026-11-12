package db

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// AccessLogArchiveFirstDateKey records the earliest UTC hour folded into the
// access log archive (RFC 3339; older values may be a bare YYYY-MM-DD).
// Together with the oldest retained rollup hour, it bounds the period an
// all-time total covers.
const AccessLogArchiveFirstDateKey = "access_log_archive_first_date"

// AccessLogArchiveThroughDateKey records the latest cutoff (YYYY-MM-DD)
// applied to the archive: every day before it has been folded in. Rebuilding
// rollups from raw logs must start at this date, or archived days would be
// counted twice.
const AccessLogArchiveThroughDateKey = "access_log_archive_through_date"

// Frozen declarations keep later model changes out of the v16-to-v17 upgrade.
type schemaV17AccessLogArchive struct {
	AdapterType      string `gorm:"size:16;primaryKey"`
	Hit              bool   `gorm:"primaryKey"`
	Upstream         string `gorm:"size:128;primaryKey;default:''"`
	RequestCount     int64  `gorm:"not null;default:0"`
	TotalBytes       int64  `gorm:"not null;default:0"`
	UpstreamRequests int64  `gorm:"not null;default:0"`
	UpstreamBytes    int64  `gorm:"not null;default:0"`
	SumLatencyMs     int64  `gorm:"not null;default:0"`
	ErrorCount       int64  `gorm:"not null;default:0"`
	UpdatedAt        time.Time
}

func (schemaV17AccessLogArchive) TableName() string { return "access_log_archive" }

type schemaV17AccessLogPackageArchive struct {
	AdapterType  string `gorm:"size:16;primaryKey"`
	PackageName  string `gorm:"size:256;primaryKey"`
	Hit          bool   `gorm:"primaryKey"`
	RequestCount int64  `gorm:"not null;default:0"`
	TotalBytes   int64  `gorm:"not null;default:0"`
	UpdatedAt    time.Time
}

func (schemaV17AccessLogPackageArchive) TableName() string { return "access_log_package_archive" }

// migrateAccessLogArchive adds the undated archive that rollup retention folds
// expiring days into, so all-time totals survive the rollup horizon. Days
// pruned before this version are gone; the archive starts empty and the
// all-time range reports the date its coverage begins.
func migrateAccessLogArchive(database *gorm.DB) error {
	if err := database.AutoMigrate(&schemaV17AccessLogArchive{}, &schemaV17AccessLogPackageArchive{}); err != nil {
		return fmt.Errorf("create access log archive: %w", err)
	}
	return nil
}
