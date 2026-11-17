package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"depsilo/internal/audit"
	"depsilo/internal/backup"
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

	// On a machine with no instance, loading fails for the missing signing
	// key, which hides what is wrong. Look for the instance first, without
	// needing the key.
	if err := findAuditInstance(); err != nil {
		return printAuditError(jsonMode, err)
	}
	cfg, err := config.Load()
	if err != nil {
		return printAuditError(jsonMode, fmt.Errorf("load config: %w", err))
	}
	if cfg.Database.Driver != "sqlite" {
		return printAuditError(jsonMode, fmt.Errorf("audit verify supports sqlite databases, got %q", cfg.Database.Driver))
	}
	// The file rule below ignores surrounding space, so the checks here and
	// the open further down must do the same.
	dsn := strings.TrimSpace(cfg.Database.DSN)
	if dsn == "" || dsn == ":memory:" {
		return printAuditError(jsonMode, errors.New("resolved database DSN is empty"))
	}
	if strings.HasPrefix(dsn, "file:") {
		return printAuditError(jsonMode, errors.New("audit verify needs a filesystem database path"))
	}
	// Like the server, treat what follows "?" as SQLite connection options:
	// the file in front of them must exist, and the database is opened with
	// the whole DSN. Opening a missing file would create an empty database.
	databasePath, err := backup.DatabaseFilePath(dsn)
	if err != nil {
		return printAuditError(jsonMode, fmt.Errorf("open database: %w", err))
	}
	if _, err := os.Stat(databasePath); err != nil {
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

// findAuditInstance reports that no Depsilo instance exists where the
// command looked: neither a configuration file nor the database the
// configuration names. Anything else is left to the full load and checks.
func findAuditInstance() error {
	located, err := config.LoadForRestore()
	if err != nil || !located.IsDefault {
		return nil
	}
	databasePath, err := backup.DatabaseFilePath(strings.TrimSpace(located.Database.DSN))
	if err != nil {
		return nil
	}
	if _, err := os.Stat(databasePath); !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// The configuration loader's search order: DEPSILO_CONFIG alone when it
	// is set, otherwise ./config.toml, /app/config.toml, ~/.depsilo/config.toml.
	var searched []string
	if named := os.Getenv("DEPSILO_CONFIG"); named != "" {
		searched = append(searched, named)
	} else {
		if local, err := filepath.Abs("config.toml"); err == nil {
			searched = append(searched, local)
		} else {
			searched = append(searched, "./config.toml")
		}
		searched = append(searched, "/app/config.toml")
		if located.ConfigPath != "" {
			searched = append(searched, located.ConfigPath)
		}
	}
	return fmt.Errorf("no Depsilo instance found: no config file at %s, and no database at %s; "+
		"set DEPSILO_CONFIG to the instance's config.toml (and DEPSILO_DATABASE_DSN if the server sets it), "+
		"or run the command as the service user from the server's working directory",
		strings.Join(searched, ", "), databasePath)
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
