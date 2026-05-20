package db

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(driver, dsn string) (*gorm.DB, error) {
	var dialector gorm.Dialector

	switch driver {
	case "sqlite":
		// Ensure parent directory exists
		if dir := filepath.Dir(dsn); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("create database directory: %w", err)
			}
		}
		dialector = sqlite.Open(dsn)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", driver)
	}

	// All timestamps go into the DB in UTC. The display layer (frontend
	// formatTime) converts to local time on render. This avoids SQLite's
	// lexicographic string comparison silently mis-ranking rows whose
	// stored zone-suffix differs from the query parameter's zone — the
	// root cause of the audit-log filter returning empty.
	db, err := gorm.Open(dialector, &gorm.Config{
		Logger:  logger.Default.LogMode(logger.Warn),
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for SQLite to avoid write lock contention
	if driver == "sqlite" {
		if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
			return nil, fmt.Errorf("set WAL mode: %w", err)
		}
	}

	return db, nil
}

func AutoMigrate(db *gorm.DB) error {
	zap.L().Info("running database auto-migration")
	return db.AutoMigrate(
		&CacheEntry{},
		&AccessLog{},
		&UpstreamRecord{},
		&User{},
		&APIToken{},
		&UpstreamLatencyLog{},
		&AuditLog{},
		&PackageRule{},
		&Vulnerability{},
		&VulnerabilityCheck{},
		&SecurityPolicy{},
		&DismissedVuln{},
		&Project{},
		&ProjectPackage{},
	)
}
