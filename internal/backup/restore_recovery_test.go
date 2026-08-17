package backup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/sqlite"
)

func TestInterruptedRestoreConvergesOnNextDatabaseHold(t *testing.T) {
	tests := []struct {
		checkpoint string
		wantNew    bool
	}{
		{checkpoint: "prepared", wantNew: false},
		{checkpoint: "config_published", wantNew: true},
		{checkpoint: "sidecars_detached", wantNew: true},
		{checkpoint: "database_published", wantNew: true},
	}
	for _, tt := range tests {
		t.Run(tt.checkpoint, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			sourceConfig := filepath.Join(root, "new-config.toml")
			sourceDatabase := filepath.Join(root, "new.db")
			targetConfig := filepath.Join(root, "config.toml")
			targetDatabase := filepath.Join(root, "depsilo.db")
			mustWriteRecoveryFile(t, sourceConfig, "new config")
			mustWriteRecoveryDatabase(t, sourceDatabase, "new")
			mustWriteRecoveryFile(t, targetConfig, "old config")
			mustWriteRecoveryDatabase(t, targetDatabase, "old")
			mustWriteRecoveryFile(t, targetDatabase+"-wal", "stale wal")

			configMetadata, err := inspectFile("config.toml", sourceConfig)
			if err != nil {
				t.Fatal(err)
			}
			databaseMetadata, err := inspectFile("depsilo.db", sourceDatabase)
			if err != nil {
				t.Fatal(err)
			}
			crash := errors.New("simulated process crash")
			_, err = installStateWith(context.Background(), map[string]extractedFile{
				"config.toml": {path: sourceConfig, size: configMetadata.Size, sha256: configMetadata.SHA256},
				"depsilo.db":  {path: sourceDatabase, size: databaseMetadata.Size, sha256: databaseMetadata.SHA256},
			}, Paths{Config: targetConfig, Database: targetDatabase}, fileOperations{
				publish: publishFile,
				checkpoint: func(name string) error {
					if name == tt.checkpoint {
						return crash
					}
					return nil
				},
			})
			if !errors.Is(err, crash) {
				t.Fatalf("installStateWith() error = %v, want injected crash", err)
			}

			lease, err := HoldDatabase(targetDatabase)
			if err != nil {
				t.Fatalf("HoldDatabase() recovery error = %v", err)
			}
			if err := lease.Close(); err != nil {
				t.Fatal(err)
			}
			// Recovery is reentrant and a second pass is a no-op.
			lease, err = HoldDatabase(targetDatabase)
			if err != nil {
				t.Fatalf("second HoldDatabase() error = %v", err)
			}
			_ = lease.Close()

			wantConfig, wantDatabase := "old config", "old"
			if tt.wantNew {
				wantConfig, wantDatabase = "new config", "new"
			}
			if got := string(mustReadRecoveryFile(t, targetConfig)); got != wantConfig {
				t.Fatalf("config = %q, want %q", got, wantConfig)
			}
			if got := readRecoveryDatabase(t, targetDatabase); got != wantDatabase {
				t.Fatalf("database value = %q, want %q", got, wantDatabase)
			}
			if _, err := os.Lstat(targetDatabase + restoreJournalSuffix); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("restore journal remains after recovery: %v", err)
			}
			if tt.wantNew {
				if _, err := os.Lstat(targetDatabase + "-wal"); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("stale WAL remains after roll-forward: %v", err)
				}
			}
		})
	}
}

func TestDatabasePublishFailureLeavesRecoverableNewPair(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceConfig := filepath.Join(root, "new-config.toml")
	sourceDatabase := filepath.Join(root, "new.db")
	targetConfig := filepath.Join(root, "config.toml")
	targetDatabase := filepath.Join(root, "depsilo.db")
	mustWriteRecoveryFile(t, sourceConfig, "new config")
	mustWriteRecoveryDatabase(t, sourceDatabase, "new")
	mustWriteRecoveryFile(t, targetConfig, "old config")
	mustWriteRecoveryDatabase(t, targetDatabase, "old")
	configMetadata, _ := inspectFile("config.toml", sourceConfig)
	databaseMetadata, _ := inspectFile("depsilo.db", sourceDatabase)
	publishFailure := errors.New("injected database publish failure")

	_, err := installStateWith(context.Background(), map[string]extractedFile{
		"config.toml": {path: sourceConfig, size: configMetadata.Size, sha256: configMetadata.SHA256},
		"depsilo.db":  {path: sourceDatabase, size: databaseMetadata.Size, sha256: databaseMetadata.SHA256},
	}, Paths{Config: targetConfig, Database: targetDatabase}, fileOperations{
		publish: func(source, target string) error {
			if target == targetDatabase {
				return publishFailure
			}
			return publishFile(source, target)
		},
	})
	if !errors.Is(err, publishFailure) {
		t.Fatalf("installStateWith() error = %v, want publish failure", err)
	}
	lease, err := HoldDatabase(targetDatabase)
	if err != nil {
		t.Fatalf("HoldDatabase() recovery error = %v", err)
	}
	_ = lease.Close()
	if got := string(mustReadRecoveryFile(t, targetConfig)); got != "new config" {
		t.Fatalf("config = %q, want recovered new config", got)
	}
	if got := readRecoveryDatabase(t, targetDatabase); got != "new" {
		t.Fatalf("database value = %q, want recovered new database", got)
	}
}

func mustWriteRecoveryFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustWriteRecoveryDatabase(t *testing.T, path, value string) {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("CREATE TABLE restore_recovery (value TEXT NOT NULL)"); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO restore_recovery(value) VALUES (?)", value); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
}

func readRecoveryDatabase(t *testing.T, path string) string {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var value string
	if err := database.QueryRow("SELECT value FROM restore_recovery").Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func mustReadRecoveryFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
