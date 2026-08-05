// Package backup owns Depsilo's durable state archive format.
//
// The archive contains only the operator configuration and a consistent
// SQLite snapshot. Package and compiler cache objects are deliberately outside
// this module: they are disposable acceleration data, not control-plane state.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "github.com/glebarez/sqlite"
)

const (
	archiveFormat            = "depsilo-backup"
	archiveVersion           = 2
	maxStateSnapshotAttempts = 3
)

// Paths identifies the durable state covered by a backup. Cache object paths
// are intentionally absent from this interface.
type Paths struct {
	Config   string
	Database string
}

// Archive describes a completed, fully closed backup archive.
type Archive struct {
	Path string
	Size int64
}

type manifest struct {
	Format    string         `json:"format"`
	Version   int            `json:"version"`
	CreatedAt time.Time      `json:"created_at"`
	Files     []manifestFile `json:"files"`
}

type manifestFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Create writes a versioned, checksummed archive to output. SQLite's VACUUM
// INTO statement supplies a transactionally consistent snapshot, including
// commits that still live in the WAL, while a running server continues to
// serve requests.
func Create(ctx context.Context, paths Paths, output string) (Archive, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := requireRegularFile(paths.Config, "config"); err != nil {
		return Archive{}, err
	}
	databasePath, err := sqliteFilePath(paths.Database)
	if err != nil {
		return Archive{}, fmt.Errorf("resolve backup database: %w", err)
	}
	if err := requireRegularFile(databasePath, "database"); err != nil {
		return Archive{}, err
	}
	if output == "" {
		return Archive{}, errors.New("backup output path is empty")
	}
	if samePath(output, paths.Config) || samePath(output, databasePath) {
		return Archive{}, errors.New("backup output must not replace the source config or database")
	}

	outputDir := filepath.Dir(output)
	workspace, err := os.MkdirTemp(outputDir, ".depsilo-backup-work-")
	if err != nil {
		return Archive{}, fmt.Errorf("create backup workspace: %w", err)
	}
	defer os.RemoveAll(workspace)

	configSnapshotPath, snapshotPath, err := snapshotStateWindow(ctx, paths, workspace)
	if err != nil {
		return Archive{}, err
	}

	entries := []struct {
		name string
		path string
	}{
		{name: "config.toml", path: configSnapshotPath},
		{name: "depsilo.db", path: snapshotPath},
	}
	metadata := make([]manifestFile, 0, len(entries))
	for _, entry := range entries {
		file, err := inspectFile(entry.name, entry.path)
		if err != nil {
			return Archive{}, err
		}
		metadata = append(metadata, file)
	}
	document, err := json.MarshalIndent(manifest{
		Format:    archiveFormat,
		Version:   archiveVersion,
		CreatedAt: time.Now().UTC(),
		Files:     metadata,
	}, "", "  ")
	if err != nil {
		return Archive{}, fmt.Errorf("encode backup manifest: %w", err)
	}

	partial, err := os.CreateTemp(outputDir, ".depsilo-backup-*.tar.gz")
	if err != nil {
		return Archive{}, fmt.Errorf("create backup archive: %w", err)
	}
	partialPath := partial.Name()
	completed := false
	defer func() {
		if !completed {
			_ = os.Remove(partialPath)
		}
	}()

	gw := gzip.NewWriter(partial)
	tw := tar.NewWriter(gw)
	writeErr := addBytes(tw, "manifest.json", document, 0o600)
	for _, entry := range entries {
		if writeErr != nil {
			break
		}
		writeErr = addFile(ctx, tw, entry.name, entry.path, 0o600)
	}
	writeErr = errors.Join(writeErr, tw.Close(), gw.Close(), partial.Sync(), partial.Close())
	if writeErr != nil {
		return Archive{}, fmt.Errorf("finalize backup archive: %w", writeErr)
	}
	if err := publishFile(partialPath, output); err != nil {
		return Archive{}, fmt.Errorf("publish backup archive: %w", err)
	}
	completed = true
	if err := syncDirectory(outputDir); err != nil {
		return Archive{}, fmt.Errorf("sync backup directory: %w", err)
	}
	info, err := os.Stat(output)
	if err != nil {
		return Archive{}, fmt.Errorf("stat completed backup: %w", err)
	}
	return Archive{Path: output, Size: info.Size()}, nil
}

// snapshotStateWindow prevents a backup from pairing a database snapshot with
// a config file that was being rewritten at the same time. SQLite supplies
// the database consistency; the metadata window detects config replacement or
// modification around VACUUM INTO and retries a bounded number of times.
func snapshotStateWindow(ctx context.Context, paths Paths, workspace string) (string, string, error) {
	for attempt := 1; attempt <= maxStateSnapshotAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		before, err := os.Stat(paths.Config)
		if err != nil {
			return "", "", fmt.Errorf("inspect config before state snapshot: %w", err)
		}
		configSnapshot, err := stageBesideTargetContext(ctx, paths.Config, filepath.Join(workspace, "config.toml"))
		if err != nil {
			return "", "", fmt.Errorf("snapshot config: %w", err)
		}
		databaseSnapshot, err := unusedSnapshotPath(workspace)
		if err != nil {
			_ = os.Remove(configSnapshot)
			return "", "", err
		}
		if err := snapshotSQLite(ctx, paths.Database, databaseSnapshot); err != nil {
			_ = os.Remove(configSnapshot)
			_ = os.Remove(databaseSnapshot)
			return "", "", err
		}
		after, err := os.Stat(paths.Config)
		if err != nil {
			_ = os.Remove(configSnapshot)
			_ = os.Remove(databaseSnapshot)
			return "", "", fmt.Errorf("inspect config after state snapshot: %w", err)
		}
		if os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && before.Mode() == after.Mode() {
			return configSnapshot, databaseSnapshot, nil
		}
		_ = os.Remove(configSnapshot)
		_ = os.Remove(databaseSnapshot)
	}
	return "", "", fmt.Errorf("config changed during %d consecutive database snapshot attempts; retry when config updates have stopped", maxStateSnapshotAttempts)
}

func unusedSnapshotPath(workspace string) (string, error) {
	placeholder, err := os.CreateTemp(workspace, ".depsilo-db-snapshot-*")
	if err != nil {
		return "", fmt.Errorf("reserve SQLite snapshot path: %w", err)
	}
	path := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func snapshotSQLite(ctx context.Context, sourcePath, snapshotPath string) (err error) {
	database, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		return fmt.Errorf("open SQLite database for backup: %w", err)
	}
	database.SetMaxOpenConns(1)
	defer func() {
		if closeErr := database.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close SQLite backup connection: %w", closeErr))
		}
	}()

	if _, err := database.ExecContext(ctx, "VACUUM INTO ?", snapshotPath); err != nil {
		return fmt.Errorf("create consistent SQLite snapshot: %w", err)
	}
	if err := validateSQLite(ctx, snapshotPath); err != nil {
		return fmt.Errorf("validate SQLite snapshot: %w", err)
	}
	return nil
}

func validateSQLite(ctx context.Context, path string) (err error) {
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, database.Close()) }()
	var result string
	if err := database.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("quick_check returned %q", result)
	}
	return nil
}

func requireRegularFile(path, label string) error {
	if path == "" {
		return fmt.Errorf("%s path is empty", label)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s %q: %w", label, path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s %q is not a regular file", label, path)
	}
	return nil
}

func inspectFile(name, path string) (manifestFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return manifestFile{}, fmt.Errorf("open %s: %w", name, err)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(hash, f)
	closeErr := f.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return manifestFile{}, fmt.Errorf("hash %s: %w", name, err)
	}
	return manifestFile{Name: name, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func addBytes(tw *tar.Writer, name string, data []byte, mode int64) error {
	header := &tar.Header{Name: name, Size: int64(len(data)), Mode: mode, ModTime: time.Now().UTC()}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write archive header %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write archive body %s: %w", name, err)
	}
	return nil
}

func addFile(ctx context.Context, tw *tar.Writer, name, path string, mode int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", name, err)
	}
	header := &tar.Header{Name: name, Size: info.Size(), Mode: mode, ModTime: info.ModTime()}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write archive header %s: %w", name, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	_, copyErr := copyContext(ctx, tw, f)
	closeErr := f.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return fmt.Errorf("write archive body %s: %w", name, err)
	}
	return nil
}
