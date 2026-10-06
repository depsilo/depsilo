package audit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"depsilo/internal/db"
)

func TestAnchorWritesOneCheckpointPerHead(t *testing.T) {
	database := newChainTestDB(t)
	path := filepath.Join(t.TempDir(), "anchors", "audit-anchors.ndjson")
	anchor := NewAnchor(database, path, 0)
	if !anchor.Enabled() {
		t.Fatal("anchor with a path reported disabled")
	}
	// Nothing chained yet: writing is a no-op rather than an empty checkpoint.
	if wrote, err := anchor.WriteCheckpoint(context.Background()); err != nil || wrote {
		t.Fatalf("empty chain wrote=%v err=%v", wrote, err)
	}
	if err := AppendAuditRows(context.Background(), database, []db.AuditLog{sampleAuditRow("one")}); err != nil {
		t.Fatal(err)
	}
	if wrote, err := anchor.WriteCheckpoint(context.Background()); err != nil || !wrote {
		t.Fatalf("first checkpoint wrote=%v err=%v", wrote, err)
	}
	// Same head: no duplicate line.
	if wrote, err := anchor.WriteCheckpoint(context.Background()); err != nil || wrote {
		t.Fatalf("duplicate checkpoint wrote=%v err=%v", wrote, err)
	}
	if err := AppendAuditRows(context.Background(), database, []db.AuditLog{sampleAuditRow("two")}); err != nil {
		t.Fatal(err)
	}
	if wrote, err := anchor.WriteCheckpoint(context.Background()); err != nil || !wrote {
		t.Fatalf("second checkpoint wrote=%v err=%v", wrote, err)
	}

	checkpoints, invalid, err := LoadCheckpoints(path)
	if err != nil {
		t.Fatal(err)
	}
	if invalid != 0 || len(checkpoints) != 2 {
		t.Fatalf("checkpoints=%d invalid=%d", len(checkpoints), invalid)
	}
	if checkpoints[0].HeadID >= checkpoints[1].HeadID {
		t.Fatalf("checkpoints not in head order: %+v", checkpoints)
	}
	if checkpoints[1].Format != AnchorFormat || checkpoints[1].HeadHash == "" {
		t.Fatalf("checkpoint = %+v", checkpoints[1])
	}
}

func TestVerifyAnchorsDetectsHeadRewrites(t *testing.T) {
	database := newChainTestDB(t)
	path := filepath.Join(t.TempDir(), "audit-anchors.ndjson")
	if err := AppendAuditRows(context.Background(), database, []db.AuditLog{
		sampleAuditRow("one"), sampleAuditRow("two"),
	}); err != nil {
		t.Fatal(err)
	}
	anchor := NewAnchor(database, path, 0)
	if _, err := anchor.WriteCheckpoint(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Healthy: every checkpointed row still matches.
	report, err := VerifyAnchors(context.Background(), database, path)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Checkpoints != 1 || report.LatestHeadID == 0 {
		t.Fatalf("healthy anchor report = %+v", report)
	}

	// Truncating the head leaves an internally consistent chain, which the
	// chain walk alone cannot see; the anchor catches it.
	var rows []db.AuditLog
	if err := database.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Delete(&db.AuditLog{}, rows[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	report, err = VerifyAnchors(context.Background(), database, path)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.BrokenAtID != rows[1].ID || !strings.Contains(report.Reason, "missing") {
		t.Fatalf("truncation anchor report = %+v", report)
	}
}

func TestLoadCheckpointsSkipsMalformedLinesAndMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.ndjson")
	checkpoints, _, err := LoadCheckpoints(missing)
	if err != nil || checkpoints != nil {
		t.Fatalf("missing file checkpoints=%v err=%v", checkpoints, err)
	}
	report, err := VerifyAnchors(context.Background(), newChainTestDB(t), missing)
	if err != nil || !report.OK || report.Checkpoints != 0 {
		t.Fatalf("missing-file report = %+v err=%v", report, err)
	}

	path := filepath.Join(t.TempDir(), "mixed.ndjson")
	body := "{not json}\n" +
		`{"format":"other","checked_at":"2026-10-06T10:00:00Z","head_id":1,"head_hash":"aa"}` + "\n" +
		`{"format":"depsilo/audit-anchor/v1","checked_at":"2026-10-06T10:00:00Z","head_id":7,"head_hash":"abc"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	checkpoints, invalid, err := LoadCheckpoints(path)
	if err != nil {
		t.Fatal(err)
	}
	if invalid != 2 || len(checkpoints) != 1 || checkpoints[0].HeadID != 7 {
		t.Fatalf("checkpoints=%+v invalid=%d", checkpoints, invalid)
	}
}
