package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAddFileHonorsCancelledContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.db")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var archive bytes.Buffer
	err := addFile(ctx, tar.NewWriter(&archive), "depsilo.db", path, 0o600)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("addFile() error = %v, want context.Canceled", err)
	}
}

func TestJournaledInstallValidatesLeaseAtEveryDestructiveBoundary(t *testing.T) {
	root := t.TempDir()
	sourceConfig := filepath.Join(root, "new-config.toml")
	sourceDatabase := filepath.Join(root, "new.db")
	targetConfig := filepath.Join(root, "config.toml")
	targetDatabase := filepath.Join(root, "depsilo.db")
	mustWriteRecoveryFile(t, sourceConfig, "new config")
	mustWriteRecoveryDatabase(t, sourceDatabase, "new")
	mustWriteRecoveryFile(t, targetConfig, "old config")
	mustWriteRecoveryDatabase(t, targetDatabase, "old")
	mustWriteRecoveryFile(t, targetDatabase+"-wal", "stale wal")
	mustWriteRecoveryFile(t, targetDatabase+"-shm", "stale shm")
	configMetadata, _ := inspectFile("config.toml", sourceConfig)
	databaseMetadata, _ := inspectFile("depsilo.db", sourceDatabase)
	validations := 0

	_, err := installStateWith(context.Background(), map[string]extractedFile{
		"config.toml": {path: sourceConfig, size: configMetadata.Size, sha256: configMetadata.SHA256},
		"depsilo.db":  {path: sourceDatabase, size: databaseMetadata.Size, sha256: databaseMetadata.SHA256},
	}, Paths{Config: targetConfig, Database: targetDatabase}, fileOperations{
		publish: publishFile,
		validateLease: func() error {
			validations++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// config publication, both SQLite sidecar removals, database publication,
	// and journal completion each validate the still-open lease identity.
	if validations < 5 {
		t.Fatalf("lease validations = %d, want at least 5 destructive-boundary checks", validations)
	}
}
