package backup_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"depsilo/internal/backup"
	"depsilo/internal/db"
)

func TestCreateCapturesCommittedWALDataAndRecordsChecksums(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	databasePath := filepath.Join(root, "depsilo.db")
	archivePath := filepath.Join(root, "backup.tar.gz")
	if err := os.WriteFile(configPath, []byte("[database]\ndriver = \"sqlite\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	database, err := db.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	if err := database.Exec("PRAGMA wal_autocheckpoint=0").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("CREATE TABLE backup_probe (value TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("INSERT INTO backup_probe(value) VALUES (?)", "committed-in-wal").Error; err != nil {
		t.Fatal(err)
	}
	serverLease, err := backup.HoldDatabase(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer serverLease.Close()

	if _, err := backup.Create(context.Background(), backup.Paths{
		Config:   configPath,
		Database: databasePath,
	}, archivePath); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	files := readArchive(t, archivePath)
	manifestData, ok := files["manifest.json"]
	if !ok {
		t.Fatal("archive has no manifest.json")
	}
	var manifest struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
		Files   []struct {
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Format != "depsilo-backup" || manifest.Version != 2 {
		t.Fatalf("manifest identity = %q/v%d, want depsilo-backup/v2", manifest.Format, manifest.Version)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("manifest files = %d, want 2", len(manifest.Files))
	}
	for _, entry := range manifest.Files {
		body, ok := files[entry.Name]
		if !ok {
			t.Fatalf("manifest entry %q is absent from archive", entry.Name)
		}
		digest := sha256.Sum256(body)
		if entry.Size != int64(len(body)) || entry.SHA256 != hex.EncodeToString(digest[:]) {
			t.Fatalf("manifest metadata for %q does not match body", entry.Name)
		}
	}

	snapshotPath := filepath.Join(root, "snapshot.db")
	if err := os.WriteFile(snapshotPath, files["depsilo.db"], 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := db.Open("sqlite", snapshotPath)
	if err != nil {
		t.Fatalf("open archived snapshot: %v", err)
	}
	snapshotSQL, err := snapshot.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotSQL.Close()
	var value string
	if err := snapshot.Raw("SELECT value FROM backup_probe").Scan(&value).Error; err != nil {
		t.Fatalf("read archived snapshot: %v", err)
	}
	if value != "committed-in-wal" {
		t.Fatalf("archived value = %q, want committed WAL transaction", value)
	}
}

func TestCreateRefusesToOverwriteItsSourceState(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	databasePath := filepath.Join(root, "depsilo.db")
	writeTestState(t, configPath, databasePath)
	wantConfig := mustReadFile(t, configPath)

	if _, err := backup.Create(context.Background(), backup.Paths{
		Config: configPath, Database: databasePath,
	}, configPath); err == nil {
		t.Fatal("Create() replaced its source config")
	}
	assertFileContent(t, configPath, wantConfig)
}

func TestRestoreRejectsChecksumMismatchBeforeChangingTargets(t *testing.T) {
	root := t.TempDir()
	sourceConfig := filepath.Join(root, "source.toml")
	sourceDatabase := filepath.Join(root, "source.db")
	archivePath := filepath.Join(root, "backup.tar.gz")
	writeTestState(t, sourceConfig, sourceDatabase)
	if _, err := backup.Create(context.Background(), backup.Paths{
		Config: sourceConfig, Database: "file:" + filepath.ToSlash(sourceDatabase) + "?mode=rwc",
	}, archivePath); err != nil {
		t.Fatal(err)
	}

	files := readArchive(t, archivePath)
	files["config.toml"][0] ^= 0xff
	writeArchive(t, archivePath, []string{"manifest.json", "config.toml", "depsilo.db"}, files)

	targetConfig := filepath.Join(root, "target.toml")
	targetDatabase := filepath.Join(root, "target.db")
	wantConfig := []byte("original config")
	wantDatabase := []byte("original database")
	if err := os.WriteFile(targetConfig, wantConfig, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetDatabase, wantDatabase, 0o640); err != nil {
		t.Fatal(err)
	}

	_, err := backup.Restore(context.Background(), archivePath, backup.Paths{
		Config: targetConfig, Database: targetDatabase,
	})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Restore() error = %v, want checksum rejection", err)
	}
	assertFileContent(t, targetConfig, wantConfig)
	assertFileContent(t, targetDatabase, wantDatabase)
}

func TestRestorePublishesValidatedStateWithPrivatePermissions(t *testing.T) {
	root := t.TempDir()
	sourceConfig := filepath.Join(root, "source.toml")
	sourceDatabase := filepath.Join(root, "source.db")
	archivePath := filepath.Join(root, "backup.tar.gz")
	writeTestState(t, sourceConfig, sourceDatabase)
	if _, err := backup.Create(context.Background(), backup.Paths{
		Config: sourceConfig, Database: sourceDatabase,
	}, archivePath); err != nil {
		t.Fatal(err)
	}

	targetConfig := filepath.Join(root, "state", "config.toml")
	targetDatabase := filepath.Join(root, "state", "data", "depsilo.db")
	if err := os.MkdirAll(filepath.Dir(targetConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(targetDatabase), 0o755); err != nil {
		t.Fatal(err)
	}
	oldConfig := []byte("old config")
	oldDatabase := []byte("old database")
	if err := os.WriteFile(targetConfig, oldConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetDatabase, oldDatabase, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetDatabase+"-wal", []byte("stale wal"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := backup.Restore(context.Background(), archivePath, backup.Paths{
		Config: targetConfig, Database: "file:" + filepath.ToSlash(targetDatabase) + "?mode=rwc",
	})
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if len(result.Restored) != 2 || len(result.Previous) != 3 {
		t.Fatalf("Restore() result = %#v, want two files and three preserved predecessors", result)
	}
	restoredConfig := string(mustReadFile(t, targetConfig))
	wantDatabaseDSN := "dsn = \"" + filepath.ToSlash(targetDatabase) + "\""
	if !strings.Contains(restoredConfig, wantDatabaseDSN) {
		t.Fatalf("restored config = %q, want canonical custom database target %q", restoredConfig, wantDatabaseDSN)
	}
	if strings.Contains(restoredConfig, "mode=rwc") {
		t.Fatalf("restored config retained source/CLI SQLite URI options: %q", restoredConfig)
	}
	for _, path := range []string{targetConfig, targetDatabase} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("restored %s mode = %o, want 600", path, info.Mode().Perm())
		}
	}
	if _, err := os.Stat(targetDatabase + "-wal"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale WAL still exists after restore: %v", err)
	}

	restored, err := db.Open("sqlite", targetDatabase)
	if err != nil {
		t.Fatalf("open restored database: %v", err)
	}
	var value string
	if err := restored.Raw("SELECT value FROM restore_probe").Scan(&value).Error; err != nil {
		t.Fatalf("read restored database: %v", err)
	}
	if value != "restored" {
		t.Fatalf("restored database value = %q", value)
	}
	restoredSQL, err := restored.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := restoredSQL.Close(); err != nil {
		t.Fatal(err)
	}

	preserved := make(map[string]bool)
	for _, path := range result.Previous {
		preserved[string(mustReadFile(t, path))] = true
	}
	for _, want := range []string{string(oldConfig), string(oldDatabase), "stale wal"} {
		if !preserved[want] {
			t.Fatalf("previous state does not retain %q: %#v", want, result.Previous)
		}
	}
}

func TestRestoreRefusesDatabaseHeldByRunningServer(t *testing.T) {
	root := t.TempDir()
	sourceConfig := filepath.Join(root, "source.toml")
	sourceDatabase := filepath.Join(root, "source.db")
	archivePath := filepath.Join(root, "backup.tar.gz")
	writeTestState(t, sourceConfig, sourceDatabase)
	if _, err := backup.Create(context.Background(), backup.Paths{
		Config: sourceConfig, Database: sourceDatabase,
	}, archivePath); err != nil {
		t.Fatal(err)
	}

	targetConfig := filepath.Join(root, "target.toml")
	targetDatabase := filepath.Join(root, "target.db")
	wantConfig := []byte("operator is still running")
	wantDatabase := []byte("database is still running")
	if err := os.WriteFile(targetConfig, wantConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetDatabase, wantDatabase, 0o600); err != nil {
		t.Fatal(err)
	}
	lease, err := backup.HoldDatabase(targetDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()

	_, err = backup.Restore(context.Background(), archivePath, backup.Paths{
		Config: targetConfig, Database: targetDatabase,
	})
	if !errors.Is(err, backup.ErrDatabaseInUse) {
		t.Fatalf("Restore() error = %v, want ErrDatabaseInUse", err)
	}
	assertFileContent(t, targetConfig, wantConfig)
	assertFileContent(t, targetDatabase, wantDatabase)
}

func TestRestoreValidatesAllStagedStateBeforeChangingTargets(t *testing.T) {
	tests := []struct {
		name        string
		entry       string
		body        []byte
		wantMessage string
	}{
		{name: "invalid config", entry: "config.toml", body: []byte("[broken\n"), wantMessage: "backup config is invalid"},
		{name: "invalid database", entry: "depsilo.db", body: []byte("not a sqlite database"), wantMessage: "backup database is invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			sourceConfig := filepath.Join(root, "source.toml")
			sourceDatabase := filepath.Join(root, "source.db")
			archivePath := filepath.Join(root, "backup.tar.gz")
			writeTestState(t, sourceConfig, sourceDatabase)
			if _, err := backup.Create(context.Background(), backup.Paths{
				Config: sourceConfig, Database: sourceDatabase,
			}, archivePath); err != nil {
				t.Fatal(err)
			}
			files := readArchive(t, archivePath)
			files[tt.entry] = tt.body
			refreshManifestEntry(t, files, tt.entry)
			writeArchive(t, archivePath, []string{"manifest.json", "config.toml", "depsilo.db"}, files)

			targetConfig := filepath.Join(root, "target.toml")
			targetDatabase := filepath.Join(root, "target.db")
			wantConfig := []byte("original config")
			wantDatabase := []byte("original database")
			if err := os.WriteFile(targetConfig, wantConfig, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(targetDatabase, wantDatabase, 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := backup.Restore(context.Background(), archivePath, backup.Paths{
				Config: targetConfig, Database: targetDatabase,
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("Restore() error = %v, want %q", err, tt.wantMessage)
			}
			assertFileContent(t, targetConfig, wantConfig)
			assertFileContent(t, targetDatabase, wantDatabase)
		})
	}
}

func readArchive(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()

	files := make(map[string][]byte)
	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return files
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = body
	}
}

func writeTestState(t *testing.T, configPath, databasePath string) {
	t.Helper()
	if err := os.WriteFile(configPath, []byte("[database]\ndriver = \"sqlite\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("CREATE TABLE restore_probe (value TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("INSERT INTO restore_probe(value) VALUES (?)", "restored").Error; err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDatabase.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeArchive(t *testing.T, path string, order []string, files map[string][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	for _, name := range order {
		body := files[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
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

func refreshManifestEntry(t *testing.T, files map[string][]byte, name string) {
	t.Helper()
	var document struct {
		Format    string `json:"format"`
		Version   int    `json:"version"`
		CreatedAt string `json:"created_at"`
		Files     []struct {
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(files["manifest.json"], &document); err != nil {
		t.Fatal(err)
	}
	for i := range document.Files {
		if document.Files[i].Name != name {
			continue
		}
		digest := sha256.Sum256(files[name])
		document.Files[i].Size = int64(len(files[name]))
		document.Files[i].SHA256 = hex.EncodeToString(digest[:])
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	files["manifest.json"] = data
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
