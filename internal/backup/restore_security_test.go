package backup_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"depsilo/internal/backup"
)

func TestRestoreRequiresManifestAsFirstEntry(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "source.toml")
	databasePath := filepath.Join(root, "source.db")
	archivePath := filepath.Join(root, "backup.tar.gz")
	writeTestState(t, configPath, databasePath)
	if _, err := backup.Create(context.Background(), backup.Paths{Config: configPath, Database: databasePath}, archivePath); err != nil {
		t.Fatal(err)
	}
	files := readArchive(t, archivePath)
	writeArchive(t, archivePath, []string{"config.toml", "manifest.json", "depsilo.db"}, files)

	_, err := backup.Restore(context.Background(), archivePath, backup.Paths{
		Config: filepath.Join(root, "target.toml"), Database: filepath.Join(root, "target.db"),
	})
	if err == nil || !strings.Contains(err.Error(), "manifest.json must be the first") {
		t.Fatalf("Restore() error = %v, want first-entry manifest rejection", err)
	}
}

func TestRestoreRejectsConfigAboveExplicitLimitBeforeExtraction(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "backup.tar.gz")
	writeManifestOnlyArchive(t, archivePath, testManifestFiles{
		{name: "config.toml", size: (1 << 20) + 1, digest: strings.Repeat("0", 64)},
		{name: "depsilo.db", size: 0, digest: strings.Repeat("0", 64)},
	})

	_, err := backup.Restore(context.Background(), archivePath, backup.Paths{
		Config: filepath.Join(root, "target.toml"), Database: filepath.Join(root, "target.db"),
	})
	if err == nil || !strings.Contains(err.Error(), "config.toml") || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("Restore() error = %v, want actionable config limit rejection", err)
	}
}

func TestRestorePreflightsHugeCompressedEntryBeforeReadingBody(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "backup.tar.gz")
	writeManifestOnlyArchive(t, archivePath, testManifestFiles{
		{name: "config.toml", size: 0, digest: hex.EncodeToString(sha256.New().Sum(nil))},
		{name: "depsilo.db", size: 1 << 60, digest: strings.Repeat("0", 64)},
	})

	_, err := backup.Restore(context.Background(), archivePath, backup.Paths{
		Config: filepath.Join(root, "target.toml"), Database: filepath.Join(root, "target.db"),
	})
	if err == nil || !strings.Contains(err.Error(), "free space") {
		t.Fatalf("Restore() error = %v, want free-space preflight rejection", err)
	}
}

func TestRestoreHonorsCancelledContextBeforeChangingTargets(t *testing.T) {
	root := t.TempDir()
	sourceConfig := filepath.Join(root, "source.toml")
	sourceDatabase := filepath.Join(root, "source.db")
	archivePath := filepath.Join(root, "backup.tar.gz")
	writeTestState(t, sourceConfig, sourceDatabase)
	if _, err := backup.Create(context.Background(), backup.Paths{Config: sourceConfig, Database: sourceDatabase}, archivePath); err != nil {
		t.Fatal(err)
	}
	targetConfig := filepath.Join(root, "target.toml")
	targetDatabase := filepath.Join(root, "target.db")
	if err := os.WriteFile(targetConfig, []byte("old config"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetDatabase, []byte("old database"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := backup.Restore(ctx, archivePath, backup.Paths{Config: targetConfig, Database: targetDatabase})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Restore() error = %v, want context.Canceled", err)
	}
	assertFileContent(t, targetConfig, []byte("old config"))
	assertFileContent(t, targetDatabase, []byte("old database"))
}

func TestRestoreRetargetsArchivedSQLiteURIToCanonicalPlainPath(t *testing.T) {
	root := t.TempDir()
	sourceConfig := filepath.Join(root, "source.toml")
	sourceDatabase := filepath.Join(root, "source.db")
	archivePath := filepath.Join(root, "backup.tar.gz")
	writeTestState(t, sourceConfig, sourceDatabase)
	archivedDSN := "file:" + filepath.ToSlash(sourceDatabase) + "?mode=rwc&cache=shared"
	if err := os.WriteFile(sourceConfig, []byte("[database]\ndriver = \"sqlite\"\ndsn = "+strconv.Quote(archivedDSN)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Create(context.Background(), backup.Paths{Config: sourceConfig, Database: sourceDatabase}, archivePath); err != nil {
		t.Fatal(err)
	}
	targetConfig := filepath.Join(root, "restored", "config.toml")
	targetDatabase := filepath.Join(root, "restored", "depsilo.db")
	if _, err := backup.Restore(context.Background(), archivePath, backup.Paths{Config: targetConfig, Database: targetDatabase}); err != nil {
		t.Fatal(err)
	}
	restored := string(mustReadFile(t, targetConfig))
	if !strings.Contains(restored, "dsn = "+strconv.Quote(targetDatabase)) {
		t.Fatalf("restored config = %q, want canonical plain target %q", restored, targetDatabase)
	}
	if strings.Contains(restored, "mode=rwc") || strings.Contains(restored, "cache=shared") || strings.Contains(restored, sourceDatabase) {
		t.Fatalf("restored config inherited archived SQLite URI options or source path: %q", restored)
	}
}

func TestRestoreResolvesParentSymlinksForNonexistentTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation normally requires an elevated Windows token")
	}
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	aliasParent := filepath.Join(root, "alias")
	if err := os.Symlink(realParent, aliasParent); err != nil {
		t.Fatal(err)
	}

	_, err := backup.Restore(context.Background(), filepath.Join(root, "missing.tar.gz"), backup.Paths{
		Config: filepath.Join(aliasParent, "state"), Database: filepath.Join(realParent, "state"),
	})
	if err == nil || !strings.Contains(err.Error(), "must be different") {
		t.Fatalf("Restore() error = %v, want aliased target rejection", err)
	}
}

func TestRestoreRejectsFinalTargetSymlinkAndSQLiteReservedCollision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation normally requires an elevated Windows token")
	}
	root := t.TempDir()
	realConfig := filepath.Join(root, "real.toml")
	if err := os.WriteFile(realConfig, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	configLink := filepath.Join(root, "config.toml")
	if err := os.Symlink(realConfig, configLink); err != nil {
		t.Fatal(err)
	}
	_, err := backup.Restore(context.Background(), filepath.Join(root, "missing.tar.gz"), backup.Paths{
		Config: configLink, Database: filepath.Join(root, "depsilo.db"),
	})
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Restore() error = %v, want final symlink rejection", err)
	}

	databasePath := filepath.Join(root, "other.db")
	_, err = backup.Restore(context.Background(), filepath.Join(root, "missing.tar.gz"), backup.Paths{
		Config: databasePath + "-wal", Database: databasePath,
	})
	if err == nil || !strings.Contains(err.Error(), "reserved SQLite") {
		t.Fatalf("Restore() error = %v, want reserved target rejection", err)
	}
}

func TestHoldDatabaseUsesStableIdentityAcrossSymlinkAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation normally requires an elevated Windows token")
	}
	root := t.TempDir()
	realPath := filepath.Join(root, "real.db")
	if err := os.WriteFile(realPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	aliasPath := filepath.Join(root, "alias.db")
	if err := os.Symlink(realPath, aliasPath); err != nil {
		t.Fatal(err)
	}
	lease, err := backup.HoldDatabase(realPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if _, err := backup.HoldDatabase(aliasPath); !errors.Is(err, backup.ErrDatabaseInUse) {
		t.Fatalf("HoldDatabase(alias) error = %v, want ErrDatabaseInUse", err)
	}
}

func TestHoldDatabaseRecognizesNamedMemoryURI(t *testing.T) {
	legacyPollutionPath, err := filepath.Abs("depsilo-shared.depsilo.lock")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(legacyPollutionPath)
	lease, err := backup.HoldDatabase("file:depsilo-shared?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("HoldDatabase(named memory) error = %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(legacyPollutionPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("named memory lease polluted the working directory: %v", err)
	}
}

type testManifestFile struct {
	name   string
	size   int64
	digest string
}

type testManifestFiles []testManifestFile

func writeManifestOnlyArchive(t *testing.T, path string, files testManifestFiles) {
	t.Helper()
	document := struct {
		Format    string    `json:"format"`
		Version   int       `json:"version"`
		CreatedAt time.Time `json:"created_at"`
		Files     []struct {
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}{Format: "depsilo-backup", Version: 2, CreatedAt: time.Now().UTC()}
	for _, file := range files {
		document.Files = append(document.Files, struct {
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		}{Name: file.name, Size: file.size, SHA256: file.digest})
	}
	body, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
