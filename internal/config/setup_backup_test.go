package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestBackupConfigBeforeSetupCopiesExistingFilePrivately(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	const document = "# hand-written\n[cache]\nmax_size_gb = 7\n"
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	// A non-UTC clock must still produce the documented UTC name.
	now := time.Date(2026, 11, 16, 18, 15, 0, 0, time.FixedZone("UTC+8", 8*60*60))

	backupPath, err := BackupConfigBeforeSetup(path, now)
	if err != nil {
		t.Fatalf("BackupConfigBeforeSetup: %v", err)
	}
	if want := filepath.Join(directory, "config.toml.pre-setup-20261116T101500Z.bak"); backupPath != want {
		t.Fatalf("backup path = %q, want %q", backupPath, want)
	}
	copied, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(copied) != document {
		t.Fatalf("backup content = %q, want the original document", copied)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(backupPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("backup mode = %o, want 600", info.Mode().Perm())
		}
	}
	original, err := os.ReadFile(path)
	if err != nil || string(original) != document {
		t.Fatalf("backup changed the existing config: %q, %v", original, err)
	}
}

func TestBackupConfigBeforeSetupSkipsFirstRun(t *testing.T) {
	directory := t.TempDir()
	backupPath, err := BackupConfigBeforeSetup(filepath.Join(directory, "config.toml"), time.Now())
	if err != nil || backupPath != "" {
		t.Fatalf("BackupConfigBeforeSetup without a config = (%q, %v), want no backup", backupPath, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("first run created %d files, want none", len(entries))
	}
}

func TestBackupConfigBeforeSetupNeverOverwritesAnEarlierBackup(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	if err := os.WriteFile(path, []byte("current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 16, 10, 15, 0, 0, time.UTC)
	earlier := filepath.Join(directory, "config.toml.pre-setup-20261116T101500Z.bak")
	if err := os.WriteFile(earlier, []byte("earlier\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if backupPath, err := BackupConfigBeforeSetup(path, now); err == nil {
		t.Fatalf("BackupConfigBeforeSetup replaced an earlier backup at %q", backupPath)
	}
	kept, err := os.ReadFile(earlier)
	if err != nil || string(kept) != "earlier\n" {
		t.Fatalf("earlier backup = %q, %v; want it untouched", kept, err)
	}
}

func TestSetupReplacesConfigOnlyForPendingSetupWithExistingFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	if SetupReplacesConfig(&Config{IsDefault: true, ConfigPath: path}) {
		t.Fatal("a first run without config.toml reported a file to replace")
	}
	if err := os.WriteFile(path, []byte("config_version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !SetupReplacesConfig(&Config{IsDefault: true, ConfigPath: path}) {
		t.Fatal("a reopened setup did not report the existing config.toml")
	}
	if SetupReplacesConfig(&Config{IsDefault: false, ConfigPath: path}) {
		t.Fatal("a configured instance reported a pending replacement")
	}
	if SetupReplacesConfig(nil) {
		t.Fatal("a nil config reported a pending replacement")
	}
}
