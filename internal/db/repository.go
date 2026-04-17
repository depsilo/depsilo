package db

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"github.com/glebarez/sqlite"
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

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
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
