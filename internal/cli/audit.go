package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"depsilo/internal/audit"
	"depsilo/internal/config"
	"depsilo/internal/db"
)

// runAudit implements `depsilo audit verify [--json]` — an offline integrity
// check against the SQLite file, usable without a running server (for example
// before filing a regulator's evidence package or after restoring a backup).
func runAudit(args []string) int {
	jsonMode, rest := stripJSONFlag(args)
	if len(rest) == 0 || rest[0] != "verify" {
		fmt.Fprintln(os.Stderr, "Usage: depsilo audit verify [--json]")
		return 1
	}
	for _, arg := range rest[1:] {
		fmt.Fprintf(os.Stderr, "unknown audit argument %q\n", arg)
		return 1
	}

	cfg, err := config.Load()
	if err != nil {
		return printAuditError(jsonMode, fmt.Errorf("load config: %w", err))
	}
	if cfg.Database.Driver != "sqlite" {
		return printAuditError(jsonMode, fmt.Errorf("audit verify supports sqlite databases, got %q", cfg.Database.Driver))
	}
	dsn := cfg.Database.DSN
	if dsn == "" || dsn == ":memory:" {
		return printAuditError(jsonMode, errors.New("resolved database DSN is empty"))
	}
	if strings.HasPrefix(dsn, "file:") {
		return printAuditError(jsonMode, errors.New("audit verify needs a filesystem database path"))
	}
	if _, err := os.Stat(dsn); err != nil {
		return printAuditError(jsonMode, fmt.Errorf("open database: %w", err))
	}

	database, err := db.Open("sqlite", dsn)
	if err != nil {
		return printAuditError(jsonMode, fmt.Errorf("open database: %w", err))
	}
	report, err := audit.VerifyChain(context.Background(), database, 2000)
	if err != nil {
		return printAuditError(jsonMode, err)
	}
	anchors, err := audit.VerifyAnchors(context.Background(), database, cfg.Audit.CheckpointFile)
	if err != nil {
		return printAuditError(jsonMode, err)
	}
	if jsonMode {
		printJSON(map[string]any{
			"ok": report.OK && anchors.OK, "integrity": report, "anchors": anchors,
			"remote_anchor_configured": strings.TrimSpace(cfg.Audit.CheckpointURL) != "",
		})
		if !report.OK || !anchors.OK {
			return 1
		}
		return 0
	}
	if !anchors.OK {
		fmt.Printf("✗ audit anchor contradiction at row %d: %s\n", anchors.BrokenAtID, anchors.Reason)
		fmt.Printf("  checkpoints: %d (%s)\n", anchors.Checkpoints, anchors.Path)
		return 1
	}
	if !report.OK {
		fmt.Printf("✗ audit chain broken at row %d: %s\n", report.BrokenAtID, report.Reason)
		fmt.Printf("  verified %d chained rows, head %d\n", report.ChainedRows, report.HeadID)
		return 1
	}
	fmt.Println("✓ audit chain verified")
	fmt.Printf("  chained rows: %d (head %d, %s)\n", report.ChainedRows, report.HeadID, shortHash(report.HeadHash))
	if report.UnchainedRows > 0 {
		fmt.Printf("  pre-chain rows: %d (written before schema v8; not covered by the chain)\n", report.UnchainedRows)
	}
	if anchors.Configured {
		fmt.Printf("  anchor checkpoints: %d (latest head %d at %s)\n",
			anchors.Checkpoints, anchors.LatestHeadID, anchors.LatestCheckedAt.Format(time.RFC3339))
		if anchors.InvalidLines > 0 {
			fmt.Printf("  anchor lines skipped as malformed: %d\n", anchors.InvalidLines)
		}
	}
	if strings.TrimSpace(cfg.Audit.CheckpointURL) != "" {
		fmt.Println("  remote anchor configured: verify its checkpoints at the receiving end (the local database cannot prove them)")
	}
	return 0
}

func printAuditError(jsonMode bool, err error) int {
	if jsonMode {
		printJSON(map[string]any{"ok": false, "error": err.Error()})
		return 1
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
	return 1
}

func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12] + "…"
}
