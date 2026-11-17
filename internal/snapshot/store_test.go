package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"depsilo/internal/db"
)

func newSnapshotTestStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "snapshots.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	records := []db.TamperRecord{
		{Key: "pypi/files/requests-2.31.0.whl", Ecosystem: "pypi", Package: "requests", Version: "2.31.0", SHA256: "aa" + strings.Repeat("0", 62), Size: 100},
		{Key: "pypi/files/requests-2.31.0.tar.gz", Ecosystem: "pypi", Package: "requests", Version: "2.31.0", SHA256: "bb" + strings.Repeat("0", 62), Size: 50},
		{Key: "npm/files/left-pad-1.3.0.tgz", Ecosystem: "npm", Package: "left-pad", Version: "1.3.0", SHA256: "cc" + strings.Repeat("0", 62), Size: 25},
	}
	if err := database.Create(&records).Error; err != nil {
		t.Fatal(err)
	}
	return NewStore(database)
}

func TestSnapshotFormatIdentifierIsVersioned(t *testing.T) {
	t.Parallel()
	if !strings.HasPrefix(Format, "depsilo/") || !strings.HasSuffix(Format, "/v1") {
		t.Fatalf("snapshot format identifier %q lost its /vN suffix", Format)
	}
}

func TestCreateFromCachePinsTamperRecords(t *testing.T) {
	store := newSnapshotTestStore(t)
	ctx := context.Background()

	snapshot, err := store.CreateFromCache(ctx, "golden-1", "before release", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ArtifactCount != 3 || snapshot.TotalBytes != 175 {
		t.Fatalf("snapshot = %+v, want 3 artifacts / 175 bytes", snapshot)
	}
	if _, err := store.CreateFromCache(ctx, "golden-1", "", "admin"); !errors.Is(err, ErrSnapshotNameTaken) {
		t.Fatalf("duplicate name error = %v", err)
	}

	items, total, err := store.Items(ctx, snapshot.ID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("items total=%d len=%d", total, len(items))
	}
	if items[0].Ecosystem != "npm" || items[0].SHA256 == "" {
		t.Fatalf("first item = %+v", items[0])
	}
}

func TestCreateFromCacheRejectsEmptyCache(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(database).CreateFromCache(context.Background(), "empty", "", "admin"); !errors.Is(err, ErrEmptySnapshot) {
		t.Fatalf("empty cache error = %v", err)
	}
}

func TestSnapshotOnlyModeGatesMembership(t *testing.T) {
	store := newSnapshotTestStore(t)
	ctx := context.Background()
	snapshot, err := store.CreateFromCache(ctx, "golden-1", "", "admin")
	if err != nil {
		t.Fatal(err)
	}

	decision, err := store.Check(ctx, "pypi", "requests", "2.31.0")
	if err != nil || !decision.Allowed {
		t.Fatalf("disabled-mode check = %+v, %v", decision, err)
	}

	if _, err := store.Activate(ctx, snapshot.ID); err != nil {
		t.Fatal(err)
	}
	if id, name := store.Active(); id != snapshot.ID || name != "golden-1" {
		t.Fatalf("active = %d/%q", id, name)
	}
	inside, err := store.Check(ctx, "pypi", "requests", "2.31.0")
	if err != nil || !inside.Allowed {
		t.Fatalf("pinned check = %+v, %v", inside, err)
	}
	outside, err := store.Check(ctx, "pypi", "requests", "2.32.0")
	if err != nil {
		t.Fatal(err)
	}
	if outside.Allowed || outside.SnapshotName != "golden-1" {
		t.Fatalf("outside check = %+v", outside)
	}

	if err := store.Delete(ctx, snapshot.ID); !errors.Is(err, ErrSnapshotActive) {
		t.Fatalf("delete active error = %v", err)
	}

	if _, err := store.Activate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if id, _ := store.Active(); id != 0 {
		t.Fatalf("active after disable = %d", id)
	}
	if err := store.Delete(ctx, snapshot.ID); err != nil {
		t.Fatalf("delete inactive snapshot: %v", err)
	}
	if _, err := store.Get(ctx, snapshot.ID); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("get deleted snapshot error = %v", err)
	}
}

func TestSnapshotActiveStateSurvivesRestart(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "restart.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.TamperRecord{
		Key: "npm/files/left-pad-1.3.0.tgz", Ecosystem: "npm", Package: "left-pad",
		Version: "1.3.0", SHA256: strings.Repeat("a", 64), Size: 10,
	}).Error; err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	snapshot, err := store.CreateFromCache(context.Background(), "restart", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Activate(context.Background(), snapshot.ID); err != nil {
		t.Fatal(err)
	}

	reloaded := NewStore(database)
	if err := reloaded.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if id, name := reloaded.Active(); id != snapshot.ID || name != "restart" {
		t.Fatalf("reloaded active = %d/%q", id, name)
	}
}

func TestSnapshotExportImportRoundTrip(t *testing.T) {
	store := newSnapshotTestStore(t)
	ctx := context.Background()
	snapshot, err := store.CreateFromCache(ctx, "golden-1", "release candidate", "admin")
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := store.Export(ctx, snapshot.ID, &buffer); err != nil {
		t.Fatal(err)
	}
	var header struct {
		Format string `json:"format"`
		Name   string `json:"name"`
		Items  []struct {
			Package string `json:"package"`
			SHA256  string `json:"sha256"`
		} `json:"items"`
	}
	if err := json.Unmarshal(buffer.Bytes(), &header); err != nil {
		t.Fatal(err)
	}
	if header.Format != Format || header.Name != "golden-1" || len(header.Items) != 3 {
		t.Fatalf("exported manifest = %+v", header)
	}

	imported, err := store.Import(ctx, bytes.NewReader(buffer.Bytes()), "golden-1-copy", "operator")
	if err != nil {
		t.Fatal(err)
	}
	if imported.Name != "golden-1-copy" || imported.ArtifactCount != 3 || imported.Note != "release candidate" {
		t.Fatalf("imported snapshot = %+v", imported)
	}
	items, total, err := store.Items(ctx, imported.ID, 10, 0)
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("imported items total=%d len=%d err=%v", total, len(items), err)
	}
	// Imported hashes become tamper baselines for artifacts this instance has
	// not seen; existing first-seen baselines are never overwritten.
	if err := store.db.Where("key = ?", "npm/files/left-pad-1.3.0.tgz").Delete(&db.TamperRecord{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.Import(ctx, bytes.NewReader(buffer.Bytes()), "golden-1-copy-2", "operator"); err != nil {
		t.Fatal(err)
	}
	var reseeded db.TamperRecord
	if err := store.db.First(&reseeded, "key = ?", "npm/files/left-pad-1.3.0.tgz").Error; err != nil {
		t.Fatal(err)
	}
	if reseeded.SHA256 != "cc"+strings.Repeat("0", 62) {
		t.Fatalf("reseeded baseline = %+v", reseeded)
	}
	var preserved db.TamperRecord
	if err := store.db.First(&preserved, "key = ?", "pypi/files/requests-2.31.0.whl").Error; err != nil {
		t.Fatal(err)
	}
	if preserved.SHA256 != "aa"+strings.Repeat("0", 62) {
		t.Fatalf("existing baseline overwritten: %+v", preserved)
	}

	if _, err := store.Import(ctx, strings.NewReader(`{"format":"nope","name":"x","items":[{"ecosystem":"pypi","package":"a","version":"1","cache_key":"k","sha256":"`+strings.Repeat("a", 64)+`"}]}`), "", "operator"); err == nil {
		t.Fatal("unsupported format was accepted")
	}
	if _, err := store.Import(ctx, strings.NewReader(`{"format":"`+Format+`","name":"y","items":[{"ecosystem":"pypi","package":"a","version":"1","cache_key":"k","sha256":"short"}]}`), "", "operator"); err == nil {
		t.Fatal("invalid hash was accepted")
	}
}

// A manifest Import refuses before writing anything is reported as
// ErrInvalidManifest with its own message, so a caller can answer it as the
// client's error. A failure while applying a valid manifest is not.
func TestImportMarksRejectedManifestsAsInvalid(t *testing.T) {
	const item = `{"ecosystem":"npm","package":"a","version":"1","cache_key":"npm/files/a-1.tgz","sha256":"` +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" + `","size":1}`
	withItem := func(entry string) string {
		return `{"format":"` + Format + `","name":"golden","items":[` + entry + `]}`
	}
	for _, test := range []struct {
		name, body, message string
	}{
		{"not JSON", "not a manifest", "decode snapshot manifest: invalid character 'o' in literal null (expecting 'u')"},
		{"another format", `{"format":"other","name":"golden","items":[` + item + `]}`, `unsupported snapshot manifest format "other"`},
		{"no name", `{"format":"` + Format + `","items":[` + item + `]}`, "snapshot name is required"},
		{"missing identity field", withItem(`{"ecosystem":"npm"}`), "snapshot item is missing an identity field"},
		{"short SHA-256", withItem(strings.Replace(item, strings.Repeat("b", 64), "bb", 1)), "snapshot item a@1 has an invalid SHA-256"},
		{"negative size", withItem(strings.Replace(item, `"size":1`, `"size":-1`, 1)), "snapshot item a@1 has a negative size"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := newSnapshotTestStore(t).Import(context.Background(), strings.NewReader(test.body), "", "operator")
			if !errors.Is(err, ErrInvalidManifest) || err.Error() != test.message {
				t.Fatalf("Import error = %v (invalid manifest: %t), want %q marked as ErrInvalidManifest",
					err, errors.Is(err, ErrInvalidManifest), test.message)
			}
		})
	}

	t.Run("empty item list keeps its own error", func(t *testing.T) {
		_, err := newSnapshotTestStore(t).Import(context.Background(),
			strings.NewReader(`{"format":"`+Format+`","name":"golden","items":[]}`), "", "operator")
		if !errors.Is(err, ErrEmptySnapshot) || errors.Is(err, ErrInvalidManifest) {
			t.Fatalf("Import error = %v, want ErrEmptySnapshot only", err)
		}
	})

	t.Run("a failed apply is not an invalid manifest", func(t *testing.T) {
		store := newSnapshotTestStore(t)
		if _, err := store.Import(context.Background(), strings.NewReader(withItem(item)), "", "operator"); err != nil {
			t.Fatal(err)
		}
		_, err := store.Import(context.Background(), strings.NewReader(withItem(item)), "", "operator")
		if !errors.Is(err, ErrSnapshotNameTaken) || errors.Is(err, ErrInvalidManifest) {
			t.Fatalf("second import with the same name: error = %v, want ErrSnapshotNameTaken only", err)
		}
	})
}
