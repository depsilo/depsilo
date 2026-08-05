package db

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const sqliteMaxOpenConnections = 4

// sqliteConnectionDSN carries connection-scoped pragmas in the DSN so the
// driver applies them to every database/sql connection, not only the first
// connection used by an explicit PRAGMA statement.
func sqliteConnectionDSN(dsn string) string {
	pragmas := []string{
		"busy_timeout(5000)",
		"synchronous(NORMAL)",
	}
	separator := "?"
	if strings.HasSuffix(dsn, "?") || strings.HasSuffix(dsn, "&") {
		separator = ""
	} else if strings.Contains(dsn, "?") {
		separator = "&"
	}
	encoded := make([]string, 0, len(pragmas))
	for _, pragma := range pragmas {
		encoded = append(encoded, "_pragma="+url.QueryEscape(pragma))
	}
	return dsn + separator + strings.Join(encoded, "&")
}

func sqliteConnectionLimit(dsn string) int {
	normalized := strings.ToLower(dsn)
	if strings.HasPrefix(normalized, ":memory:") ||
		strings.HasPrefix(normalized, "file::memory:") {
		return 1
	}
	if queryStart := strings.IndexByte(dsn, '?'); queryStart >= 0 {
		query, err := url.ParseQuery(dsn[queryStart+1:])
		if err == nil && strings.EqualFold(query.Get("mode"), "memory") {
			return 1
		}
	}
	return sqliteMaxOpenConnections
}

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
		dialector = sqlite.Open(sqliteConnectionDSN(dsn))
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
		sqlDatabase, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("access sqlite connection pool: %w", err)
		}
		// Four connections preserve WAL read concurrency while bounding the
		// number of writers entering SQLite's busy handler. Separate SQLite
		// connections otherwise receive separate in-memory databases, so keep
		// tests and ephemeral callers on one shared view.
		maxConnections := sqliteConnectionLimit(dsn)
		sqlDatabase.SetMaxOpenConns(maxConnections)
		sqlDatabase.SetMaxIdleConns(maxConnections)

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

// CurrentSchemaVersion is the newest database schema understood by this
// binary. Every schema-changing release must add a numbered migration rather
// than relying on GORM to infer an upgrade from the latest model definitions.
const CurrentSchemaVersion = 1

type schemaMigrationRecord struct {
	Version   int       `gorm:"primaryKey;autoIncrement:false"`
	Name      string    `gorm:"size:128;not null"`
	AppliedAt time.Time `gorm:"not null"`
}

func (schemaMigrationRecord) TableName() string { return "schema_migrations" }

type schemaMigration struct {
	version int
	name    string
	apply   func(*gorm.DB) error
}

var schemaMigrations = []schemaMigration{
	{version: 1, name: "baseline", apply: migrateBaselineSchema},
}

// AutoMigrate applies each numbered schema migration exactly once. Databases
// created before the migration ledger are treated as version zero, so the
// baseline migration remains an idempotent upgrade path for existing installs.
// A database written by a newer binary is rejected to prevent a downgrade from
// silently mutating a schema it does not understand.
func AutoMigrate(database *gorm.DB) error {
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		return fmt.Errorf("create schema migration ledger: %w", err)
	}

	var current int
	if err := database.Model(&schemaMigrationRecord{}).
		Select("COALESCE(MAX(version), 0)").
		Scan(&current).Error; err != nil {
		return fmt.Errorf("read database schema version: %w", err)
	}
	if current > CurrentSchemaVersion {
		return fmt.Errorf(
			"database schema version %d is newer than this binary supports (%d); upgrade Depsilo instead of downgrading the database",
			current,
			CurrentSchemaVersion,
		)
	}

	for _, migration := range schemaMigrations {
		if migration.version <= current {
			continue
		}
		zap.L().Info("running database migration",
			zap.Int("from_version", current),
			zap.Int("to_version", migration.version),
			zap.String("name", migration.name),
		)
		if err := applySchemaMigration(database, migration, time.Now().UTC()); err != nil {
			return fmt.Errorf("apply database migration %d (%s): %w", migration.version, migration.name, err)
		}
		current = migration.version
	}

	// These checks are deliberately repeatable. Besides upgrading old schemas,
	// they repair interrupted index swaps and rows left partially backfilled by
	// older binaries. Keeping them outside the one-shot ledger means a later
	// startup can heal that drift without pretending a new schema version exists.
	return ensureCurrentSchemaInvariants(database)
}

func applySchemaMigration(database *gorm.DB, migration schemaMigration, appliedAt time.Time) error {
	return database.Transaction(func(tx *gorm.DB) error {
		if err := migration.apply(tx); err != nil {
			return err
		}
		record := schemaMigrationRecord{
			Version:   migration.version,
			Name:      migration.name,
			AppliedAt: appliedAt,
		}
		if err := tx.Create(&record).Error; err != nil {
			return fmt.Errorf("record database migration %d: %w", migration.version, err)
		}
		return nil
	})
}

func ensureCurrentSchemaInvariants(database *gorm.DB) error {
	// Version 1 is currently the newest schema, so its repairable invariants
	// are also the current invariants. A future version should append its own
	// invariant function here without changing ensureSchemaV1Invariants.
	return ensureSchemaV1Invariants(database)
}
