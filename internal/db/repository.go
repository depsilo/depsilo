package db

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	if err := db.AutoMigrate(
		&CacheEntry{},
		&HuggingFaceRefPin{},
		&HuggingFaceRepositoryRevocation{},
		&CompileCacheEntry{},
		&CompileCacheCredential{},
		&CompileCacheDeletion{},
		&UpstreamUpdateEvent{},
		&AccessLog{},
		&AccessLogFiveMinutely{},
		&AccessLogHourly{},
		&AccessLogDaily{},
		&AccessLogPackageDaily{},
		&UpstreamRecord{},
		&ControlPlaneState{},
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
	); err != nil {
		return err
	}
	if err := migrateCompileCacheIdentityIndex(db); err != nil {
		return err
	}
	if err := migrateVulnerabilityIdentityIndex(db); err != nil {
		return err
	}
	if err := backfillCacheKinds(db); err != nil {
		return err
	}
	return backfillHuggingFacePackageNames(db)
}

func migrateCompileCacheIdentityIndex(database *gorm.DB) error {
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
			CompileCacheProtocolCCache,
		).Error; err != nil {
			return fmt.Errorf("backfill compiler-cache protocol: %w", err)
		}

		indexes, err := tx.Migrator().GetIndexes(&CompileCacheEntry{})
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

		if !tx.Migrator().HasIndex(&CompileCacheEntry{}, legacyIndex) {
			return nil
		}
		if err := tx.Migrator().DropIndex(&CompileCacheEntry{}, legacyIndex); err != nil {
			return fmt.Errorf("drop legacy compiler-cache index %q: %w", legacyIndex, err)
		}
		return nil
	})
}

func migrateVulnerabilityIdentityIndex(database *gorm.DB) error {
	const (
		legacyIndex      = "idx_vulnerabilities_osv_id"
		replacementIndex = "idx_vuln_osv_eco_pkg"
	)
	wantColumns := []string{"osv_id", "ecosystem", "package_name"}

	return database.Transaction(func(tx *gorm.DB) error {
		indexes, err := tx.Migrator().GetIndexes(&Vulnerability{})
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

		if !tx.Migrator().HasIndex(&Vulnerability{}, legacyIndex) {
			return nil
		}
		if err := tx.Migrator().DropIndex(&Vulnerability{}, legacyIndex); err != nil {
			return fmt.Errorf("drop legacy vulnerability index %q: %w", legacyIndex, err)
		}
		return nil
	})
}
