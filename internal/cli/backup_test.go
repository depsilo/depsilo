package cli

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"depsilo/internal/config"
	"depsilo/internal/db"
)

func TestBackupRestoreIntoFreshDefaultInstancePreservesSigningKey(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, key := range []string{
		"DEPSILO_CONFIG", "DEPSILO_DATABASE_DRIVER", "DEPSILO_DATABASE_DSN",
		"DEPSILO_STORAGE_PATH", "DEPSILO_COMPILE_CACHE_STORAGE_PATH",
		"DEPSILO_AUTH_JWT_SECRET", "DEPSILO_SERVER_HOST", "DEPSILO_SERVER_PORT",
		"DEPSILO_ADMIN_USERNAME", "DEPSILO_ADMIN_PASSWORD", "DEPSILO_BOOTSTRAP_TOKEN",
	} {
		t.Setenv(key, "")
	}
	sourceHome := filepath.Join(root, "source")
	t.Setenv("HOME", sourceHome)
	t.Setenv("USERPROFILE", sourceHome)
	source, err := config.LoadForServer()
	if err != nil {
		t.Fatalf("initialize source instance: %v", err)
	}
	originalKey := source.Auth.JWTSecret
	request := config.SetupRequest{}
	request.Database.Driver = source.Database.Driver
	request.Database.DSN = source.Database.DSN
	request.Auth.JWTSecret = originalKey
	request.Auth.Managed = true
	if err := config.WriteConfig(source.ConfigPath, request); err != nil {
		t.Fatalf("write source configuration: %v", err)
	}
	database, err := db.Open(source.Database.Driver, source.Database.DSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("CREATE TABLE restore_marker (value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("INSERT INTO restore_marker (value) VALUES (?)", "original-instance").Error; err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDatabase.Close(); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(root, "instance.tar.gz")
	runBackupRestoreCLI(t, "backup", "--out", archive, "--json")
	// The recovery machine has neither the source configuration nor its key.
	// Its default target paths must be sufficient; no manual target flags or
	// temporary key generation should be needed before restoring the archive.
	if err := os.Remove(source.ManagedJWTSecretPath); err != nil {
		t.Fatal(err)
	}
	freshHome := filepath.Join(root, "restored")
	t.Setenv("HOME", freshHome)
	t.Setenv("USERPROFILE", freshHome)
	freshConfigPath := filepath.Join(freshHome, ".depsilo", "config.toml")
	freshKeyPath, err := config.ManagedJWTSecretPath(freshConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	assertNoCLIManagedKey(t, freshKeyPath)
	runBackupRestoreCLI(t, "restore", archive, "--json")
	assertNoCLIManagedKey(t, freshKeyPath)

	restored, err := config.LoadForServer()
	if err != nil {
		t.Fatalf("load restored instance: %v", err)
	}
	if restored.Auth.JWTSecret != originalKey {
		t.Fatal("restored instance changed its signing key")
	}
	if restored.ConfigPath != freshConfigPath {
		t.Fatalf("restored config path = %q, want default path %q", restored.ConfigPath, freshConfigPath)
	}
	wantDatabase, err := filepath.EvalSymlinks(filepath.Join(freshHome, ".depsilo", "data", "depsilo.db"))
	if err != nil {
		t.Fatal(err)
	}
	if restored.Database.DSN != wantDatabase {
		t.Fatalf("restored database = %q, want fresh instance database %q", restored.Database.DSN, wantDatabase)
	}
	restoredDatabase, err := db.Open(restored.Database.Driver, restored.Database.DSN)
	if err != nil {
		t.Fatal(err)
	}
	restoredSQLDatabase, err := restoredDatabase.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restoredSQLDatabase.Close() })
	var value string
	if err := restoredDatabase.Raw("SELECT value FROM restore_marker").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	if value != "original-instance" {
		t.Fatal("restore did not preserve the original database contents")
	}
}

func runBackupRestoreCLI(t *testing.T, args ...string) {
	t.Helper()
	processArgs := append([]string{"-test.run=^TestCLIHelperProcess$", "--"}, args...)
	command := exec.Command(os.Args[0], processArgs...)
	command.Env = append(os.Environ(), cliHelperEnv+"=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI %s: %v; output=%s", args[0], err, output)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(output, &result); err != nil || !result.OK {
		t.Fatalf("CLI %s did not report success: %v; output=%s", args[0], err, output)
	}
}

func assertNoCLIManagedKey(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restore target unexpectedly has a generated signing key: %v", err)
	}
}
