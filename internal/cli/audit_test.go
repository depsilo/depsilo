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

func TestRunAuditVerifyReadsTheDatabase(t *testing.T) {
	dir := t.TempDir()
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
	configPath := filepath.Join(dir, "config.toml")
	document := "config_version = 1\n" +
		"[server]\nhost = \"127.0.0.1\"\nport = 23333\n" +
		"[database]\ndriver = \"sqlite\"\ndsn = \"" + databasePath + "\"\n" +
		"[auth]\njwt_secret = \"0123456789abcdef0123456789abcdef\"\n"
	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)

	output := captureStdout(t, func() {
		if code := runAudit([]string{"verify"}); code != 0 {
			t.Fatalf("runAudit(verify) = %d, want 0", code)
		}
	})
	if !strings.Contains(output, "audit chain verified") {
		t.Fatalf("output = %q", output)
	}
	if !strings.Contains(output, "chained rows: 1") {
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
