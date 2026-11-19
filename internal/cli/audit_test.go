package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"depsilo/internal/audit"
	"depsilo/internal/db"
)

func TestRunAuditRequiresVerifySubcommand(t *testing.T) {
	if code := runAudit(nil); code != 1 {
		t.Fatalf("runAudit(nil) = %d, want 1", code)
	}
	if code := runAudit([]string{"status"}); code != 1 {
		t.Fatalf("runAudit(status) = %d, want 1", code)
	}
}

// newAuditVerifyInstance creates a database holding one chained audit row and
// a config.toml whose DSN is the database path followed by dsnSuffix, and
// points DEPSILO_CONFIG at it.
func newAuditVerifyInstance(t *testing.T, dsnSuffix string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	databasePath := filepath.Join(dir, "depsilo.db")
	database, err := db.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	if err := audit.AppendAuditRows(context.Background(), database, []db.AuditLog{
		{Action: "download", CacheResult: "hit", StatusCode: 200, CreatedAt: time.Now().UTC()},
	}); err != nil {
		t.Fatal(err)
	}
	if pool, err := database.DB(); err != nil {
		t.Fatal(err)
	} else if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	document := "config_version = 1\n" +
		"[server]\nhost = \"127.0.0.1\"\nport = 23333\n" +
		"[database]\ndriver = \"sqlite\"\ndsn = \"" + filepath.ToSlash(databasePath) + dsnSuffix + "\"\n" +
		"[auth]\njwt_secret = \"0123456789abcdef0123456789abcdef\"\n"
	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	return dir
}

func TestRunAuditVerifyReadsTheDatabase(t *testing.T) {
	newAuditVerifyInstance(t, "")

	output := captureStdout(t, func() {
		if code := runAudit([]string{"verify"}); code != 0 {
			t.Fatalf("runAudit(verify) = %d, want 0", code)
		}
	})
	if !strings.Contains(output, "audit chain verified") || !strings.Contains(output, "chained rows: 1") {
		t.Fatalf("output = %q", output)
	}
}

// The server accepts SQLite connection options after "?" in the DSN; the
// command must check the file in front of them and open the same database.
func TestRunAuditVerifyAcceptsADSNWithConnectionOptions(t *testing.T) {
	dir := newAuditVerifyInstance(t, "?_pragma=busy_timeout(5000)")

	output := captureStdout(t, func() {
		if code := runAudit([]string{"verify"}); code != 0 {
			t.Fatalf("runAudit(verify) = %d, want 0", code)
		}
	})
	if !strings.Contains(output, "audit chain verified") || !strings.Contains(output, "chained rows: 1") {
		t.Fatalf("output = %q", output)
	}

	// A missing file is still reported, by its path alone, and not created.
	missing := filepath.Join(dir, "missing.db")
	t.Setenv("DEPSILO_DATABASE_DSN", filepath.ToSlash(missing)+"?_pragma=busy_timeout(5000)")
	output = captureStdout(t, func() {
		if code := runAudit([]string{"verify", "--json"}); code != 1 {
			t.Fatalf("runAudit(verify) on a missing database = %d, want 1", code)
		}
	})
	if !strings.Contains(output, "open database: stat") || strings.Contains(output, "_pragma") {
		t.Fatalf("output = %q", output)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("the command created the missing database: %v", err)
	}
}

// Space around the DSN is not part of it: a DSN of nothing else is reported
// as empty, in the command's own words, and a padded path names the same
// database the file check looked at.
func TestRunAuditVerifyIgnoresSpaceAroundTheDSN(t *testing.T) {
	dir := newAuditVerifyInstance(t, "")

	for _, dsn := range []string{" ", " :memory: "} {
		t.Setenv("DEPSILO_DATABASE_DSN", dsn)
		output := captureStdout(t, func() {
			if code := runAudit([]string{"verify", "--json"}); code != 1 {
				t.Fatalf("runAudit(verify) with the DSN %q = %d, want 1", dsn, code)
			}
		})
		if !strings.Contains(output, "resolved database DSN is empty") || strings.Contains(output, "lease") {
			t.Fatalf("DSN %q: output = %q", dsn, output)
		}
	}

	t.Setenv("DEPSILO_DATABASE_DSN", " "+filepath.ToSlash(filepath.Join(dir, "depsilo.db"))+" ")
	output := captureStdout(t, func() {
		if code := runAudit([]string{"verify"}); code != 0 {
			t.Fatalf("runAudit(verify) with a padded DSN = %d, want 0", code)
		}
	})
	if !strings.Contains(output, "audit chain verified") {
		t.Fatalf("output = %q", output)
	}
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	done := make(chan string, 1)
	go func() {
		var builder strings.Builder
		buffer := make([]byte, 4096)
		for {
			n, err := reader.Read(buffer)
			if n > 0 {
				builder.Write(buffer[:n])
			}
			if err != nil {
				break
			}
		}
		done <- builder.String()
	}()
	run()
	_ = writer.Close()
	os.Stdout = original
	return <-done
}

// With no configuration file and no database, the command says that it found
// no instance, where it looked, and how to point it at one, instead of
// reporting the signing key that a blank machine also lacks.
func TestRunAuditVerifySaysWhenItFindsNoInstance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEPSILO_CONFIG", "")
	t.Setenv("DEPSILO_DATABASE_DSN", "")
	workdir := t.TempDir()
	t.Chdir(workdir)
	databasePath := filepath.Join(home, ".depsilo", "data", "depsilo.db")

	stdout, stderr, code := captureStdoutStderr(t, func() int { return runAudit([]string{"verify"}) })
	if code != 1 || stdout != "" {
		t.Fatalf("runAudit(verify) = %d, stdout %q; want 1 and nothing", code, stdout)
	}
	for _, fragment := range []string{
		"no Depsilo instance found",
		"config.toml, /app/config.toml, " + filepath.Join(home, ".depsilo", "config.toml"),
		"no database at " + databasePath,
		"set DEPSILO_CONFIG to the instance's config.toml",
	} {
		if !strings.Contains(stderr, fragment) {
			t.Errorf("stderr %q missing %q", stderr, fragment)
		}
	}
	if strings.Contains(stderr, "signing key") {
		t.Errorf("stderr reports the signing key of an instance that does not exist: %q", stderr)
	}

	// A DEPSILO_CONFIG that names a missing file is the only place it looked.
	named := filepath.Join(workdir, "missing.toml")
	t.Setenv("DEPSILO_CONFIG", named)
	stdout, _, code = captureStdoutStderr(t, func() int { return runAudit([]string{"verify", "--json"}) })
	if code != 1 || !strings.Contains(stdout, "no config file at "+named+", and no database at ") {
		t.Fatalf("runAudit(verify --json) with a missing DEPSILO_CONFIG = %d, %q", code, stdout)
	}
	if _, err := os.Stat(filepath.Join(home, ".depsilo")); !os.IsNotExist(err) {
		t.Fatalf("looking for an instance created state: %v", err)
	}
}

// After retention pruned the oldest rows, the command verifies the retained
// chain from the recorded base and says how far history was pruned.
func TestRunAuditVerifyStartsFromTheRetentionBase(t *testing.T) {
	dir := newAuditVerifyInstance(t, "")
	database, err := db.Open("sqlite", filepath.Join(dir, "depsilo.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&db.AuditLog{}).Where("1 = 1").
		Update("created_at", time.Now().UTC().AddDate(0, 0, -200)).Error; err != nil {
		t.Fatal(err)
	}
	// The stored hash no longer matches the moved timestamp, so re-chain the
	// single row before pruning it.
	var row db.AuditLog
	if err := database.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&row).Update("hash", audit.ComputeAuditHash("", row)).Error; err != nil {
		t.Fatal(err)
	}
	if deleted, err := audit.NewRetention(database, 90).RunOnce(context.Background()); err != nil || deleted != 1 {
		t.Fatalf("retention deleted=%d err=%v", deleted, err)
	}
	if err := audit.AppendAuditRows(context.Background(), database, []db.AuditLog{
		{Action: "download", CacheResult: "hit", StatusCode: 200, CreatedAt: time.Now().UTC()},
	}); err != nil {
		t.Fatal(err)
	}
	if pool, err := database.DB(); err != nil {
		t.Fatal(err)
	} else if err := pool.Close(); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if code := runAudit([]string{"verify"}); code != 0 {
			t.Fatalf("runAudit(verify) = %d, want 0", code)
		}
	})
	if !strings.Contains(output, "audit chain verified") || !strings.Contains(output, "chained rows: 1 (head 2") ||
		!strings.Contains(output, "pruned by retention through row 1") {
		t.Fatalf("output = %q", output)
	}
}
