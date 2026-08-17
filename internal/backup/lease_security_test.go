//go:build linux || darwin

package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHoldDatabaseRejectsWritableSharedParent(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	_, err := HoldDatabase(filepath.Join(shared, "depsilo.db"))
	if err == nil || !strings.Contains(err.Error(), "group/world-writable") {
		t.Fatalf("HoldDatabase() error = %v, want writable-parent rejection", err)
	}
}

func TestDatabaseLeaseDetectsLockInodeReplacement(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(root, "depsilo.db")
	closer, err := HoldDatabase(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	lease := closer.(*databaseLease)
	defer lease.Close()
	if err := os.Remove(lease.lockPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lease.lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := lease.validate(); err == nil {
		t.Fatal("database lease accepted a replaced lock inode")
	}
	replacement, err := HoldDatabase(databasePath)
	if err == nil {
		_ = replacement.Close()
	} else if !errors.Is(err, ErrDatabaseInUse) {
		t.Fatalf("replacement lock acquisition returned unexpected error: %v", err)
	}
}
