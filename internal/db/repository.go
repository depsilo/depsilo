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

	// SQLite tuning for write throughput.
	//
	//   journal_mode=WAL  → readers don't block writers; writers don't block readers
	//   synchronous=NORMAL → don't fsync on every commit (the WAL itself is durable
	//                        across crashes; only os-level crash within ~1s can lose
	//                        the most recent transactions). The default FULL adds a
	//                        5-50ms fsync to every UPDATE and shows up as massive
	//                        latency under concurrent cache-hit fanout (e.g. pip
	//                        installing a tree of 10+ packages in parallel hitting
	//                        the cache_entries hit_count UPDATE).
	//   busy_timeout=5000 → if a write still locks, wait up to 5s before failing
	//                        rather than instant ERR_BUSY (matters when AccessLog
	//                        goroutines and admin queries compete for the writer).
	if driver == "sqlite" {
		if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
			return nil, fmt.Errorf("set WAL mode: %w", err)
		}
		if err := db.Exec("PRAGMA synchronous=NORMAL").Error; err != nil {
			return nil, fmt.Errorf("set synchronous mode: %w", err)
		}
		if err := db.Exec("PRAGMA busy_timeout=5000").Error; err != nil {
			return nil, fmt.Errorf("set busy_timeout: %w", err)
		}
	}

	return db, nil
}

func AutoMigrate(db *gorm.DB) error {
	zap.L().Info("running database auto-migration")
	return db.AutoMigrate(
		&CacheEntry{},
		&AccessLog{},
		&AccessLogHourly{},
		&AccessLogDaily{},
		&AccessLogPackageDaily{},
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
		&TrialRecord{},
		&LicenseStorage{},
		&WebhookConfig{},
		// Quarantine (T1 Task 1 — minimum release age). Three tables:
		// PackageTimestamp caches upstream publish times, ApprovedVersion
		// records operator bypasses, QuarantineEvent is the audit log.
		// All defined in db/quarantine.go; helpers in internal/quarantine.
		&PackageTimestamp{},
		&ApprovedVersion{},
		&QuarantineEvent{},
		// Known-malicious blocklist (DIRECTION Task 2). Defined in
		// db/blocklist.go; helpers in internal/blocklist.
		&MaliciousPackage{},
		&MalwareOverride{},
		&BlocklistSyncState{},
		// Tamper detection (DIRECTION T1). Defined in db/tamper.go;
		// helper in internal/tamper.
		&TamperRecord{},
	)
}
