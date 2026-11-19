package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

var retentionCutoff = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func agedAuditRow(pkg string, createdAt time.Time) db.AuditLog {
	row := sampleAuditRow(pkg)
	row.CreatedAt = createdAt
	return row
}

func storedAuditRows(t *testing.T, database *gorm.DB) []db.AuditLog {
	t.Helper()
	var rows []db.AuditLog
	if err := database.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func appendRows(t *testing.T, database *gorm.DB, rows ...db.AuditLog) []db.AuditLog {
	t.Helper()
	if err := AppendAuditRows(context.Background(), database, rows); err != nil {
		t.Fatal(err)
	}
	return storedAuditRows(t, database)
}

func TestPruneDeletesOnlyTheExpiredPrefixAndRecordsTheBase(t *testing.T) {
	database := newChainTestDB(t)
	old := retentionCutoff.Add(-48 * time.Hour)
	fresh := retentionCutoff.Add(time.Hour)
	// The fourth row is old again, but it follows a retained row: pruning is
	// a prefix by id, so it stays.
	rows := appendRows(t, database,
		agedAuditRow("a", old), agedAuditRow("b", old), agedAuditRow("c", fresh), agedAuditRow("d", old))

	retention := NewRetention(database, 90)
	retention.batch = 1
	deleted, err := retention.PruneBefore(context.Background(), retentionCutoff)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted %d rows, want 2", deleted)
	}
	remaining := storedAuditRows(t, database)
	if len(remaining) != 2 || remaining[0].ID != rows[2].ID || remaining[1].ID != rows[3].ID {
		t.Fatalf("remaining rows = %+v", remaining)
	}
	base, ok, err := LoadChainBase(context.Background(), database)
	if err != nil || !ok {
		t.Fatalf("chain base ok=%v err=%v", ok, err)
	}
	if base.ID != rows[1].ID || base.Hash != rows[1].Hash || !base.CreatedAt.Equal(old) {
		t.Fatalf("chain base = %+v, want row %d", base, rows[1].ID)
	}

	report, err := VerifyChain(context.Background(), database, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.ChainedRows != 2 || report.BaseID != rows[1].ID || report.BaseHash != rows[1].Hash {
		t.Fatalf("report after prune = %+v", report)
	}
	if report.BaseCreatedAt == nil || !report.BaseCreatedAt.Equal(old) {
		t.Fatalf("report base time = %v", report.BaseCreatedAt)
	}
}

func TestAppendAfterFullPruneContinuesTheChain(t *testing.T) {
	database := newChainTestDB(t)
	old := retentionCutoff.Add(-time.Hour)
	rows := appendRows(t, database, agedAuditRow("a", old), agedAuditRow("b", old), agedAuditRow("c", old))

	retention := NewRetention(database, 90)
	retention.batch = 2
	if deleted, err := retention.PruneBefore(context.Background(), retentionCutoff); err != nil || deleted != 3 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
	if remaining := storedAuditRows(t, database); len(remaining) != 0 {
		t.Fatalf("remaining = %+v", remaining)
	}
	report, err := VerifyChain(context.Background(), database, 100)
	if err != nil || !report.OK || report.BaseID != rows[2].ID {
		t.Fatalf("empty-after-prune report = %+v err=%v", report, err)
	}

	after := appendRows(t, database, sampleAuditRow("next"), sampleAuditRow("after"))
	if len(after) != 2 {
		t.Fatalf("rows after append = %+v", after)
	}
	// The chain continues from the pruned head, and ids keep increasing so
	// exporter cursors and anchors never see a reused id.
	if after[0].PrevHash == nil || *after[0].PrevHash != rows[2].Hash {
		t.Fatalf("first row after prune prev_hash = %v, want %q", after[0].PrevHash, rows[2].Hash)
	}
	if after[0].ID != rows[2].ID+1 || after[1].ID != rows[2].ID+2 {
		t.Fatalf("ids after prune = %d, %d; want %d, %d", after[0].ID, after[1].ID, rows[2].ID+1, rows[2].ID+2)
	}
	report, err = VerifyChain(context.Background(), database, 100)
	if err != nil || !report.OK || report.ChainedRows != 2 || report.HeadID != after[1].ID {
		t.Fatalf("report after continued append = %+v err=%v", report, err)
	}
}

func TestVerifyChainChecksTheFirstRetainedRowAgainstTheBase(t *testing.T) {
	database := newChainTestDB(t)
	old := retentionCutoff.Add(-time.Hour)
	rows := appendRows(t, database, agedAuditRow("a", old), agedAuditRow("b", old), sampleAuditRow("c"))
	if _, err := NewRetention(database, 90).PruneBefore(context.Background(), retentionCutoff); err != nil {
		t.Fatal(err)
	}

	// A forged base no longer links to the first retained row.
	if err := database.Save(&db.ControlPlaneState{Key: chainBaseKey, Value: fmt.Sprintf(`{"id":%d,"hash":"forged"}`, rows[1].ID)}).Error; err != nil {
		t.Fatal(err)
	}
	report, err := VerifyChain(context.Background(), database, 100)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.BrokenAtID != rows[2].ID {
		t.Fatalf("forged base report = %+v", report)
	}

	// A missing base with a first row that does not start the chain fails.
	if err := database.Where("key = ?", chainBaseKey).Delete(&db.ControlPlaneState{}).Error; err != nil {
		t.Fatal(err)
	}
	report, err = VerifyChain(context.Background(), database, 100)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.BrokenAtID != rows[2].ID || !strings.Contains(report.Reason, "base") {
		t.Fatalf("missing base report = %+v", report)
	}
}

func TestPruneKeepsRowsAnEnabledExporterHasNotDelivered(t *testing.T) {
	database := newChainTestDB(t)
	old := retentionCutoff.Add(-time.Hour)
	rows := appendRows(t, database,
		agedAuditRow("a", old), agedAuditRow("b", old), agedAuditRow("c", old), agedAuditRow("d", old))
	exporters := []db.AuditExporter{
		{Name: "behind", Kind: db.AuditExporterKindNDJSON, URL: "https://siem.example/in", Enabled: true, Cursor: rows[1].ID},
		{Name: "current", Kind: db.AuditExporterKindNDJSON, URL: "https://siem.example/in", Enabled: true, Cursor: rows[3].ID},
		{Name: "paused", Kind: db.AuditExporterKindNDJSON, URL: "https://siem.example/in", Enabled: true},
	}
	if err := database.Create(&exporters).Error; err != nil {
		t.Fatal(err)
	}
	// A disabled exporter does not hold retention back.
	if err := database.Model(&exporters[2]).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}

	deleted, err := NewRetention(database, 90).PruneBefore(context.Background(), retentionCutoff)
	if err != nil || deleted != 2 {
		t.Fatalf("deleted=%d err=%v, want 2", deleted, err)
	}
	remaining := storedAuditRows(t, database)
	if len(remaining) != 2 || remaining[0].ID != rows[2].ID {
		t.Fatalf("remaining = %+v", remaining)
	}
}

func TestPruneStopsBeforeABrokenLink(t *testing.T) {
	database := newChainTestDB(t)
	old := retentionCutoff.Add(-time.Hour)
	rows := appendRows(t, database, agedAuditRow("a", old), agedAuditRow("b", old), agedAuditRow("c", old))
	if err := database.Model(&db.AuditLog{}).Where("id = ?", rows[1].ID).
		Update("package_name", "tampered").Error; err != nil {
		t.Fatal(err)
	}

	deleted, err := NewRetention(database, 90).PruneBefore(context.Background(), retentionCutoff)
	if err == nil {
		t.Fatal("pruning through a broken link reported success")
	}
	if deleted != 1 {
		t.Fatalf("deleted %d rows, want only the intact row before the break", deleted)
	}
	report, err := VerifyChain(context.Background(), database, 100)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.BrokenAtID != rows[1].ID {
		t.Fatalf("report = %+v, want the break kept visible at %d", report, rows[1].ID)
	}
}

func TestPruneCoversThePreChainPrefix(t *testing.T) {
	database := newChainTestDB(t)
	old := retentionCutoff.Add(-time.Hour)
	legacy := []db.AuditLog{agedAuditRow("legacy-a", old), agedAuditRow("legacy-b", old)}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	rows := appendRows(t, database, agedAuditRow("a", old), sampleAuditRow("b"))

	deleted, err := NewRetention(database, 90).PruneBefore(context.Background(), retentionCutoff)
	if err != nil || deleted != 3 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
	report, err := VerifyChain(context.Background(), database, 100)
	if err != nil || !report.OK || report.ChainedRows != 1 || report.BaseID != rows[2].ID {
		t.Fatalf("report = %+v err=%v", report, err)
	}
}

func TestRetentionZeroDaysKeepsEverything(t *testing.T) {
	database := newChainTestDB(t)
	appendRows(t, database, agedAuditRow("ancient", time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)))

	retention := NewRetention(database, 0)
	if retention.Enabled() {
		t.Fatal("retention_days = 0 reported enabled")
	}
	deleted, err := retention.RunOnce(context.Background())
	if err != nil || deleted != 0 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
	if rows := storedAuditRows(t, database); len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}

	enabled := NewRetention(database, 90)
	if deleted, err := enabled.RunOnce(context.Background()); err != nil || deleted != 1 {
		t.Fatalf("enabled retention deleted=%d err=%v", deleted, err)
	}
}

func TestVerifyAnchorsReportsPrunedCheckpointsSeparately(t *testing.T) {
	database := newChainTestDB(t)
	path := filepath.Join(t.TempDir(), "audit-anchors.ndjson")
	old := retentionCutoff.Add(-time.Hour)
	rows := appendRows(t, database, agedAuditRow("a", old), agedAuditRow("b", old), sampleAuditRow("c"))
	writeCheckpoints(t, path, rows[0], rows[1], rows[2])

	if _, err := NewRetention(database, 90).PruneBefore(context.Background(), retentionCutoff); err != nil {
		t.Fatal(err)
	}
	report, err := VerifyAnchors(context.Background(), database, path)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Checkpoints != 3 || report.Pruned != 2 {
		t.Fatalf("anchor report after prune = %+v", report)
	}

	// Retained checkpoints are still verified.
	if err := database.Model(&db.AuditLog{}).Where("id = ?", rows[2].ID).Update("hash", "rewritten").Error; err != nil {
		t.Fatal(err)
	}
	report, err = VerifyAnchors(context.Background(), database, path)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.BrokenAtID != rows[2].ID {
		t.Fatalf("rewritten retained row report = %+v", report)
	}

	// A checkpoint at the base id must match the base hash.
	fresh := newChainTestDB(t)
	freshRows := appendRows(t, fresh, agedAuditRow("a", old), sampleAuditRow("b"))
	if _, err := NewRetention(fresh, 90).PruneBefore(context.Background(), retentionCutoff); err != nil {
		t.Fatal(err)
	}
	forgedPath := filepath.Join(t.TempDir(), "forged.ndjson")
	forged := freshRows[0]
	forged.Hash = "not-the-pruned-hash"
	writeCheckpoints(t, forgedPath, forged)
	report, err = VerifyAnchors(context.Background(), fresh, forgedPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.BrokenAtID != freshRows[0].ID {
		t.Fatalf("base contradiction report = %+v", report)
	}
}

// pruneAfterBaseRead commits one retention sweep right after the next read of
// the chain base, the window between a verifier's base read and its row scan.
// Each test database is fresh, so the callback is never removed.
func pruneAfterBaseRead(t *testing.T, database *gorm.DB) *atomic.Bool {
	t.Helper()
	var armed, fired atomic.Bool
	armed.Store(true)
	name := "test:prune_after_base_read"
	err := database.Callback().Query().After("gorm:query").Register(name, func(statement *gorm.DB) {
		if statement.Statement.Table != "control_plane_states" || !armed.CompareAndSwap(true, false) {
			return
		}
		if _, err := NewRetention(database, 90).PruneBefore(context.Background(), retentionCutoff); err != nil {
			t.Errorf("interleaved prune: %v", err)
			return
		}
		fired.Store(true)
	})
	if err != nil {
		t.Fatal(err)
	}
	return &fired
}

func TestVerifyReadsTheBaseAndRowsFromOneSnapshot(t *testing.T) {
	old := retentionCutoff.Add(-time.Hour)

	chainDB := newChainTestDB(t)
	appendRows(t, chainDB, agedAuditRow("a", old), agedAuditRow("b", old), sampleAuditRow("c"))
	fired := pruneAfterBaseRead(t, chainDB)
	report, err := VerifyChain(context.Background(), chainDB, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !fired.Load() {
		t.Fatal("the interleaved prune did not run")
	}
	// The database was consistent at every commit, so either snapshot verifies.
	if !report.OK {
		t.Fatalf("chain report with a prune between base and rows = %+v", report)
	}

	anchorDB := newChainTestDB(t)
	path := filepath.Join(t.TempDir(), "audit-anchors.ndjson")
	rows := appendRows(t, anchorDB, agedAuditRow("a", old), agedAuditRow("b", old), sampleAuditRow("c"))
	writeCheckpoints(t, path, rows[0], rows[1], rows[2])
	fired = pruneAfterBaseRead(t, anchorDB)
	anchors, err := VerifyAnchors(context.Background(), anchorDB, path)
	if err != nil {
		t.Fatal(err)
	}
	if !fired.Load() {
		t.Fatal("the interleaved prune did not run")
	}
	if !anchors.OK {
		t.Fatalf("anchor report with a prune between base and rows = %+v", anchors)
	}
}

func writeCheckpoints(t *testing.T, path string, rows ...db.AuditLog) {
	t.Helper()
	var lines []string
	for _, row := range rows {
		encoded, err := json.Marshal(ChainCheckpoint{
			Format: AnchorFormat, CheckedAt: time.Now().UTC(), HeadID: row.ID, HeadHash: row.Hash,
		})
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(encoded))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPruneConcurrentWithAppendsKeepsTheChainVerifiable(t *testing.T) {
	database := newChainTestDB(t)
	old := retentionCutoff.Add(-time.Hour)
	retention := NewRetention(database, 90)
	retention.batch = 7

	// One writer, like the audit logger, races the sweeper.
	var writers sync.WaitGroup
	errs := make(chan error, 1)
	writers.Add(1)
	go func() {
		defer writers.Done()
		for index := 0; index < 80; index++ {
			row := agedAuditRow(fmt.Sprintf("row-%d", index), old)
			if err := AppendAuditRows(context.Background(), database, []db.AuditLog{row}); err != nil {
				errs <- err
				return
			}
		}
	}()
	done := make(chan struct{})
	go func() {
		writers.Wait()
		close(done)
	}()
	for pruning := true; pruning; {
		select {
		case <-done:
			pruning = false
		default:
		}
		// A sweep that loses a write race fails and is retried next time.
		_, _ = retention.PruneBefore(context.Background(), retentionCutoff)
		time.Sleep(2 * time.Millisecond)
	}
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent append: %v", err)
	}
	if err := AppendAuditRows(context.Background(), database, []db.AuditLog{sampleAuditRow("tail")}); err != nil {
		t.Fatal(err)
	}
	report, err := VerifyChain(context.Background(), database, 10)
	if err != nil || !report.OK {
		t.Fatalf("report after concurrent pruning = %+v err=%v", report, err)
	}
	if report.BaseID == 0 || report.HeadID <= report.BaseID {
		t.Fatalf("base/head after concurrent pruning = %+v", report)
	}
}

func TestRetentionStartStopsWhenCancelled(t *testing.T) {
	database := newChainTestDB(t)
	retention := NewRetention(database, 90)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		retention.Start(ctx)
		close(stopped)
	}()
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("retention sweeper did not stop after cancellation")
	}
}
