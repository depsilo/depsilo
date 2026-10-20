package audit

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

func newChainTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "audit-chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	return database
}

func sampleAuditRow(pkg string) db.AuditLog {
	return db.AuditLog{
		Ecosystem: "pypi", PackageName: pkg, Version: "1.0.0",
		Action: "download", CacheResult: "hit", StatusCode: 200,
		CreatedAt: time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC),
	}
}

func TestAppendAuditRowsChainsInOrder(t *testing.T) {
	database := newChainTestDB(t)
	rows := []db.AuditLog{sampleAuditRow("requests"), sampleAuditRow("numpy"), sampleAuditRow("scipy")}
	if err := AppendAuditRows(context.Background(), database, rows); err != nil {
		t.Fatal(err)
	}
	var stored []db.AuditLog
	if err := database.Order("id").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 {
		t.Fatalf("stored %d rows", len(stored))
	}
	prev := ""
	for _, row := range stored {
		if row.PrevHash == nil || *row.PrevHash != prev {
			t.Fatalf("row %d prev_hash = %v, want %q", row.ID, row.PrevHash, prev)
		}
		if expected := ComputeAuditHash(prev, row); row.Hash != expected {
			t.Fatalf("row %d hash = %q, want %q", row.ID, row.Hash, expected)
		}
		prev = row.Hash
	}
}

func TestVerifyChainReportsHealthyChainAndPreChainPrefix(t *testing.T) {
	database := newChainTestDB(t)
	// Legacy rows written before schema v8: no prev_hash, no hash.
	legacy := []db.AuditLog{sampleAuditRow("legacy-a"), sampleAuditRow("legacy-b")}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := AppendAuditRows(context.Background(), database, []db.AuditLog{sampleAuditRow("fresh-a"), sampleAuditRow("fresh-b")}); err != nil {
		t.Fatal(err)
	}

	report, err := VerifyChain(context.Background(), database, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.ChainedRows != 2 || report.UnchainedRows != 2 {
		t.Fatalf("report = %+v", report)
	}
	if report.FirstChainedID != legacy[1].ID+1 || report.HeadID != legacy[1].ID+2 || report.HeadHash == "" {
		t.Fatalf("report head/first = %+v", report)
	}
}

func TestVerifyChainDetectsContentTampering(t *testing.T) {
	database := newChainTestDB(t)
	if err := AppendAuditRows(context.Background(), database, []db.AuditLog{
		sampleAuditRow("alpha"), sampleAuditRow("beta"), sampleAuditRow("gamma"),
	}); err != nil {
		t.Fatal(err)
	}
	var rows []db.AuditLog
	if err := database.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&db.AuditLog{}).Where("id = ?", rows[1].ID).
		Update("package_name", "tampered").Error; err != nil {
		t.Fatal(err)
	}

	report, err := VerifyChain(context.Background(), database, 100)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.BrokenAtID != rows[1].ID || !strings.Contains(report.Reason, "content") {
		t.Fatalf("report = %+v, want content break at %d", report, rows[1].ID)
	}
}

func TestVerifyChainDetectsDeletionAndUnchainedRows(t *testing.T) {
	database := newChainTestDB(t)
	if err := AppendAuditRows(context.Background(), database, []db.AuditLog{
		sampleAuditRow("one"), sampleAuditRow("two"), sampleAuditRow("three"),
	}); err != nil {
		t.Fatal(err)
	}
	var rows []db.AuditLog
	if err := database.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Delete(&db.AuditLog{}, rows[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	report, err := VerifyChain(context.Background(), database, 100)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.BrokenAtID != rows[2].ID || !strings.Contains(report.Reason, "prev_hash") {
		t.Fatalf("deletion report = %+v, want prev_hash break at %d", report, rows[2].ID)
	}

	// An unchained row inserted after the chain started is a break too.
	fresh := newChainTestDB(t)
	if err := AppendAuditRows(context.Background(), fresh, []db.AuditLog{sampleAuditRow("one")}); err != nil {
		t.Fatal(err)
	}
	if err := fresh.Create(&db.AuditLog{Action: "download", CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	report, err = VerifyChain(context.Background(), fresh, 100)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || !strings.Contains(report.Reason, "not chained") {
		t.Fatalf("unchained report = %+v", report)
	}
}

func TestComputeAuditHashIsStableAcrossLocations(t *testing.T) {
	instant := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	row := sampleAuditRow("stable")
	row.CreatedAt = instant
	utcHash := ComputeAuditHash("prev", row)

	row.CreatedAt = instant.In(time.FixedZone("UTC+8", 8*3600))
	if got := ComputeAuditHash("prev", row); got != utcHash {
		t.Fatalf("hash depends on the timestamp location: %q vs %q", got, utcHash)
	}
	if other := ComputeAuditHash("other-prev", row); other == utcHash {
		t.Fatal("hash ignores prev_hash")
	}
}

func TestAppendAuditRowsRetriesStaleSQLiteSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.db")
	database, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, handle := range []*gorm.DB{database, writer} {
		sqlDB, err := handle.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := database.AutoMigrate(&db.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("CREATE TABLE competing_writes (value INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	// A commit after chainHead's read invalidates the deferred transaction's
	// WAL snapshot. This is the same pattern as concurrent cache/access writes.
	committed := false
	if err := database.Callback().Create().Before("gorm:create").Register("test:commit_competing_writer", func(tx *gorm.DB) {
		if committed {
			return
		}
		committed = true
		if err := writer.Exec("INSERT INTO competing_writes VALUES (1)").Error; err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendAuditRows(context.Background(), database, []db.AuditLog{sampleAuditRow("torch")}); err != nil {
		t.Fatalf("append after concurrent commit: %v", err)
	}
	var count int64
	if err := database.Model(&db.AuditLog{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("stored %d audit rows, want 1", count)
	}
	report, err := VerifyChain(context.Background(), database, 100)
	if err != nil || !report.OK || report.ChainedRows != 1 {
		t.Fatalf("chain report = %+v, error = %v", report, err)
	}
}
