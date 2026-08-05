package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"depsilo/internal/config"
)

const (
	maxManifestBytes = 1 << 20
	maxConfigBytes   = 1 << 20
)

// RestoreResult reports the durable files replaced by Restore and the paths
// retaining their previous versions. Cache files are never included.
type RestoreResult struct {
	Restored []string
	Previous []string
}

type extractedFile struct {
	path   string
	size   int64
	sha256 string
}

// Restore fully stages and validates an archive before replacing either
// target. Publication is journaled so an interrupted process is completed on
// the next Restore or server start without ever removing both live files.
func Restore(ctx context.Context, archivePath string, targets Paths) (RestoreResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if archivePath == "" {
		return RestoreResult{}, errors.New("backup archive path is empty")
	}
	if targets.Config == "" || targets.Database == "" {
		return RestoreResult{}, errors.New("restore config and database paths must not be empty")
	}
	databaseTarget, err := sqliteFilePath(targets.Database)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore database target: %w", err)
	}
	configTarget, err := canonicalRestoreTarget(targets.Config, "config")
	if err != nil {
		return RestoreResult{}, err
	}
	databaseTarget, err = canonicalRestoreTarget(databaseTarget, "database")
	if err != nil {
		return RestoreResult{}, err
	}
	targets = Paths{Config: configTarget, Database: databaseTarget}
	if err := validateRestoreTargetPair(targets.Config, targets.Database); err != nil {
		return RestoreResult{}, err
	}
	archiveIdentity := archivePath
	if resolved, resolveErr := filepath.EvalSymlinks(archivePath); resolveErr == nil {
		archiveIdentity, _ = filepath.Abs(resolved)
	} else if absolute, absoluteErr := filepath.Abs(archivePath); absoluteErr == nil {
		archiveIdentity = absolute
	}
	if sameCanonicalPath(archiveIdentity, targets.Config) || sameCanonicalPath(archiveIdentity, targets.Database) {
		return RestoreResult{}, errors.New("restore archive must not be one of its target files")
	}
	if err := ctx.Err(); err != nil {
		return RestoreResult{}, err
	}

	document, manifestDigest, err := readArchiveManifest(ctx, archivePath)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := validateManifest(document); err != nil {
		return RestoreResult{}, err
	}
	if err := preflightRestoreSpace(document, targets); err != nil {
		return RestoreResult{}, err
	}

	workspace, err := os.MkdirTemp("", "depsilo-restore-")
	if err != nil {
		return RestoreResult{}, fmt.Errorf("create restore workspace: %w", err)
	}
	defer os.RemoveAll(workspace)

	files, err := extractArchive(ctx, archivePath, workspace, document, manifestDigest)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := validateExtracted(document, files); err != nil {
		return RestoreResult{}, err
	}
	configDocument, err := os.ReadFile(files["config.toml"].path)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("read staged config: %w", err)
	}
	if err := config.ValidateDocument(configDocument); err != nil {
		return RestoreResult{}, fmt.Errorf("backup config is invalid: %w", err)
	}
	configDocument, _, err = config.RetargetSQLiteDatabase(configDocument, targets.Database)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("retarget backup database config: %w", err)
	}
	if len(configDocument) > maxConfigBytes {
		return RestoreResult{}, fmt.Errorf("retargeted config.toml exceeds the %d-byte restore limit", maxConfigBytes)
	}
	if err := replaceStagedConfig(files["config.toml"].path, configDocument); err != nil {
		return RestoreResult{}, err
	}
	if err := config.ValidateDocument(configDocument); err != nil {
		return RestoreResult{}, fmt.Errorf("retargeted backup config is invalid: %w", err)
	}
	configMetadata, err := inspectFile("config.toml", files["config.toml"].path)
	if err != nil {
		return RestoreResult{}, err
	}
	files["config.toml"] = extractedFile{
		path: files["config.toml"].path, size: configMetadata.Size, sha256: configMetadata.SHA256,
	}
	if err := validateSQLite(ctx, files["depsilo.db"].path); err != nil {
		return RestoreResult{}, fmt.Errorf("backup database is invalid: %w", err)
	}

	lease, err := HoldDatabase(targets.Database)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore requires the Depsilo server to be stopped: %w", err)
	}
	operations := fileOperations{publish: publishFile}
	if concrete, ok := lease.(*databaseLease); ok {
		if err := concrete.validate(); err != nil {
			_ = lease.Close()
			return RestoreResult{}, fmt.Errorf("database restore lease became unstable: %w", err)
		}
		operations.validateLease = concrete.validate
	}
	result, installErr := installStateWith(ctx, files, targets, operations)
	if closeErr := lease.Close(); closeErr != nil {
		installErr = errors.Join(installErr, fmt.Errorf("release database restore lease: %w", closeErr))
	}
	return result, installErr
}

func readArchiveManifest(ctx context.Context, archivePath string) (manifest, string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return manifest{}, "", fmt.Errorf("open backup archive: %w", err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return manifest{}, "", fmt.Errorf("open backup gzip stream: %w", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	if err := ctx.Err(); err != nil {
		return manifest{}, "", err
	}
	header, err := tr.Next()
	if err == io.EOF {
		return manifest{}, "", errors.New("backup is empty; manifest.json must be the first entry")
	}
	if err != nil {
		return manifest{}, "", fmt.Errorf("read backup manifest header: %w", err)
	}
	if header.Name != "manifest.json" {
		return manifest{}, "", fmt.Errorf("backup manifest.json must be the first entry, got %q", header.Name)
	}
	if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
		return manifest{}, "", errors.New("backup manifest.json is not a regular file")
	}
	if header.Size < 0 || header.Size > maxManifestBytes {
		return manifest{}, "", fmt.Errorf("backup manifest exceeds the %d-byte limit", maxManifestBytes)
	}
	body, err := readContext(ctx, tr, header.Size)
	if err != nil {
		return manifest{}, "", fmt.Errorf("read backup manifest: %w", err)
	}
	document, err := decodeManifest(body)
	if err != nil {
		return manifest{}, "", err
	}
	digest := sha256.Sum256(body)
	return document, hex.EncodeToString(digest[:]), nil
}

func decodeManifest(data []byte) (manifest, error) {
	var document manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return manifest{}, fmt.Errorf("decode backup manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return manifest{}, errors.New("backup manifest contains trailing JSON")
	}
	return document, nil
}

func validateManifest(document manifest) error {
	if document.Format != archiveFormat || document.Version != archiveVersion {
		return fmt.Errorf("unsupported backup format %q version %d", document.Format, document.Version)
	}
	if len(document.Files) != 2 {
		return fmt.Errorf("backup manifest lists %d files, want 2", len(document.Files))
	}
	seen := make(map[string]bool, 2)
	for _, file := range document.Files {
		if file.Name != "config.toml" && file.Name != "depsilo.db" {
			return fmt.Errorf("backup manifest contains unexpected file %q", file.Name)
		}
		if seen[file.Name] {
			return fmt.Errorf("backup manifest lists %q more than once", file.Name)
		}
		seen[file.Name] = true
		if file.Size < 0 {
			return fmt.Errorf("backup manifest gives %q a negative size", file.Name)
		}
		if file.Name == "config.toml" && file.Size > maxConfigBytes {
			return fmt.Errorf("backup config.toml size %d exceeds the %d-byte restore limit", file.Size, maxConfigBytes)
		}
		digest, err := hex.DecodeString(file.SHA256)
		if err != nil || len(digest) != sha256.Size || strings.ToLower(file.SHA256) != file.SHA256 {
			return fmt.Errorf("backup manifest has an invalid SHA-256 for %q", file.Name)
		}
	}
	for _, name := range []string{"config.toml", "depsilo.db"} {
		if !seen[name] {
			return fmt.Errorf("backup manifest is missing %q", name)
		}
	}
	return nil
}

func extractArchive(ctx context.Context, archivePath, workspace string, expected manifest, expectedManifestDigest string) (_ map[string]extractedFile, err error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open backup archive: %w", err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open backup gzip stream: %w", err)
	}
	defer func() { err = errors.Join(err, gr.Close()) }()

	expectedFiles := make(map[string]manifestFile, 2)
	for _, file := range expected.Files {
		expectedFiles[file.Name] = file
	}
	seen := make(map[string]bool, 2)
	files := make(map[string]extractedFile, 2)
	tr := tar.NewReader(gr)
	manifestHeader, err := tr.Next()
	if err != nil {
		return nil, fmt.Errorf("read backup manifest header: %w", err)
	}
	if manifestHeader.Name != "manifest.json" ||
		(manifestHeader.Typeflag != tar.TypeReg && manifestHeader.Typeflag != tar.TypeRegA) ||
		manifestHeader.Size < 0 || manifestHeader.Size > maxManifestBytes {
		return nil, errors.New("backup changed while restoring: manifest.json is no longer the valid first entry")
	}
	manifestBody, err := readContext(ctx, tr, manifestHeader.Size)
	if err != nil {
		return nil, fmt.Errorf("read backup manifest: %w", err)
	}
	manifestHash := sha256.Sum256(manifestBody)
	if hex.EncodeToString(manifestHash[:]) != expectedManifestDigest {
		return nil, errors.New("backup changed while restoring: manifest.json checksum differs from preflight")
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, nextErr := tr.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nil, fmt.Errorf("read backup archive: %w", nextErr)
		}
		expectedFile, allowed := expectedFiles[header.Name]
		if !allowed {
			return nil, fmt.Errorf("backup contains unexpected entry %q", header.Name)
		}
		if seen[header.Name] {
			return nil, fmt.Errorf("backup contains duplicate entry %q", header.Name)
		}
		seen[header.Name] = true
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("backup entry %q is not a regular file", header.Name)
		}
		if header.Size != expectedFile.Size {
			return nil, fmt.Errorf("backup entry %q size %d differs from manifest size %d", header.Name, header.Size, expectedFile.Size)
		}

		stagedPath := filepath.Join(workspace, header.Name)
		out, err := os.OpenFile(stagedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create staged %s: %w", header.Name, err)
		}
		hash := sha256.New()
		written, copyErr := copyContext(ctx, io.MultiWriter(out, hash), tr)
		closeErr := errors.Join(out.Sync(), out.Close())
		if err := errors.Join(copyErr, closeErr); err != nil {
			return nil, fmt.Errorf("extract %s: %w", header.Name, err)
		}
		if written != header.Size {
			return nil, fmt.Errorf("extract %s: wrote %d bytes, expected %d", header.Name, written, header.Size)
		}
		files[header.Name] = extractedFile{
			path: stagedPath, size: written, sha256: hex.EncodeToString(hash.Sum(nil)),
		}
	}
	for _, name := range []string{"config.toml", "depsilo.db"} {
		if !seen[name] {
			return nil, fmt.Errorf("backup is missing %q", name)
		}
	}
	return files, nil
}

func validateExtracted(document manifest, files map[string]extractedFile) error {
	listed := make(map[string]bool, 2)
	for _, expected := range document.Files {
		if expected.Name != "config.toml" && expected.Name != "depsilo.db" {
			return fmt.Errorf("backup manifest contains unexpected file %q", expected.Name)
		}
		if listed[expected.Name] {
			return fmt.Errorf("backup manifest lists %q more than once", expected.Name)
		}
		listed[expected.Name] = true
		actual, ok := files[expected.Name]
		if !ok {
			return fmt.Errorf("backup is missing %q", expected.Name)
		}
		if actual.size != expected.Size {
			return fmt.Errorf("size mismatch for %q: got %d, want %d", expected.Name, actual.size, expected.Size)
		}
		if actual.sha256 != expected.SHA256 {
			return fmt.Errorf("checksum mismatch for %q", expected.Name)
		}
	}
	for _, name := range []string{"config.toml", "depsilo.db"} {
		if !listed[name] {
			return fmt.Errorf("backup manifest is missing %q", name)
		}
	}
	return nil
}

func readContext(ctx context.Context, reader io.Reader, size int64) ([]byte, error) {
	if size < 0 || size > int64(maxManifestBytes) {
		return nil, errors.New("bounded read size is invalid")
	}
	var buffer bytes.Buffer
	buffer.Grow(int(size))
	written, err := copyContext(ctx, &buffer, io.LimitReader(reader, size))
	if err != nil {
		return nil, err
	}
	if written != size {
		return nil, io.ErrUnexpectedEOF
	}
	return buffer.Bytes(), nil
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			written, writeErr := destination.Write(buffer[:read])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != read {
				return total, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func replaceStagedConfig(path string, document []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open staged config for retargeting: %w", err)
	}
	_, writeErr := file.Write(document)
	err = errors.Join(writeErr, file.Chmod(0o600), file.Sync(), file.Close())
	if err != nil {
		return fmt.Errorf("persist retargeted staged config: %w", err)
	}
	return nil
}

type fileOperations struct {
	publish       func(string, string) error
	checkpoint    func(string) error
	validateLease func() error
}

func installStateWith(ctx context.Context, files map[string]extractedFile, targets Paths, operations fileOperations) (RestoreResult, error) {
	return installJournaled(ctx, files, targets, operations)
}

func stageBesideTarget(source, target string) (string, error) {
	return stageBesideTargetContext(context.Background(), source, target)
}

func stageBesideTargetContext(ctx context.Context, source, target string) (string, error) {
	in, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open staged restore source: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".restore-*")
	if err != nil {
		_ = in.Close()
		return "", fmt.Errorf("create restore candidate for %s: %w", target, err)
	}
	path := tmp.Name()
	copyErr := func() error {
		_, copyErr := copyContext(ctx, tmp, in)
		return errors.Join(copyErr, in.Close(), tmp.Chmod(0o600), tmp.Sync(), tmp.Close())
	}()
	if copyErr != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("stage restore candidate for %s: %w", target, copyErr)
	}
	return path, nil
}
