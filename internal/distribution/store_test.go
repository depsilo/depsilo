package distribution_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/cache"
	"depsilo/internal/db"
	"depsilo/internal/distribution"
	"gorm.io/gorm"
)

type catalogFixture struct {
	store   *distribution.Store
	db      *gorm.DB
	storage *cache.LocalStorage
	dbPath  string
	limits  distribution.Limits
}

func newCatalogFixture(t *testing.T, limits distribution.Limits) *catalogFixture {
	t.Helper()
	if limits == (distribution.Limits{}) {
		limits = distribution.Limits{MaxObjectBytes: 1024, MaxStorageBytes: 4096}
	}
	root := t.TempDir()
	f := &catalogFixture{dbPath: filepath.Join(root, "catalog.db"), limits: limits}
	f.openDatabase(t)
	var err error
	f.storage, err = cache.NewPrivateLocalStorage(filepath.Join(root, "content"))
	if err != nil {
		t.Fatal(err)
	}
	f.store = f.newStore(t, f.storage)
	return f
}

func (f *catalogFixture) openDatabase(t *testing.T) {
	t.Helper()
	database, err := db.Open("sqlite", f.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	f.db = database
}

func (f *catalogFixture) newStore(t *testing.T, storage cache.Storage) *distribution.Store {
	t.Helper()
	store, err := distribution.NewStore(f.db, storage, f.limits)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// These are deliberately content-only fixtures with wheel-like filenames.
// They prove byte integrity and catalog lifecycle, not wheel validity or pip installation.
func contentObject(data []byte, name string) distribution.Object {
	digest := sha256.Sum256(data)
	return distribution.Object{
		SHA256: hex.EncodeToString(digest[:]), Size: int64(len(data)), Kind: "wheel",
		Filename: name + "-1.0-py3-none-any.whl", Package: name, Version: "1.0",
	}
}

func manifestBytes(t *testing.T, name, revision string, objects ...distribution.Object) []byte {
	t.Helper()
	m := distribution.Manifest{
		Format: distribution.ManifestFormat, Name: name, Revision: revision, Ecosystem: "pypi",
		Target:  distribution.Target{OS: "linux", Architecture: "amd64", PythonVersion: "3.12", PythonABI: "cp312", PipVersion: "24.3", ClientImage: "python@sha256:" + strings.Repeat("1", 64)},
		Objects: objects,
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func stageSet(t *testing.T, f *catalogFixture, name, revision string, objects ...distribution.Object) *distribution.Set {
	t.Helper()
	set, err := f.store.Stage(context.Background(), manifestBytes(t, name, revision, objects...), "test-operator")
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func putContent(t *testing.T, f *catalogFixture, set *distribution.Set, object distribution.Object, data []byte) *distribution.UploadResult {
	t.Helper()
	result, err := f.store.Put(context.Background(), set.ID, object.SHA256, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func storageKey(digest string) string { return "objects/sha256/" + digest[:2] + "/" + digest }

func assertStoredContent(t *testing.T, f *catalogFixture, object distribution.Object, expected []byte) {
	t.Helper()
	r, size, err := f.storage.Get(context.Background(), storageKey(object.SHA256))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil || size != int64(len(expected)) || !bytes.Equal(got, expected) {
		t.Fatalf("stored content: size=%d bytes=%q err=%v", size, got, err)
	}
}

func overwriteContent(t *testing.T, f *catalogFixture, object distribution.Object, data []byte) {
	t.Helper()
	if err := f.storage.Put(context.Background(), storageKey(object.SHA256), bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
}

func TestStoreStagePreservesExactManifestAndConflictsWithoutMutation(t *testing.T) {
	f := newCatalogFixture(t, distribution.Limits{})
	o := contentObject([]byte("content-only example"), "example")
	raw := manifestBytes(t, "python", "v1", o)
	set, err := f.store.Stage(context.Background(), raw, "first-operator")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if set.ID != hex.EncodeToString(digest[:]) || set.ContentStatus != "staged" || set.VerifiedAt != nil || set.CheckedAt != nil || set.Serving || set.InstallValidation != "not_run" || set.ProvenanceStatus != "operator_declared" {
		t.Fatalf("unexpected staged declaration: %+v", set)
	}
	repeated, err := f.store.Stage(context.Background(), raw, "second-operator")
	if err != nil || repeated.ID != set.ID || !repeated.CreatedAt.Equal(set.CreatedAt) {
		t.Fatalf("identical Stage is not idempotent: %+v %v", repeated, err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Stage(context.Background(), compact.Bytes(), "another-operator"); !errors.Is(err, distribution.ErrConflict) {
		t.Fatalf("reformatted same revision: %v", err)
	}
	changed := o
	changed.Size++
	if _, err := f.store.Stage(context.Background(), manifestBytes(t, "python", "v2", changed), "operator"); !errors.Is(err, distribution.ErrConflict) {
		t.Fatalf("conflicting declared digest size: %v", err)
	}
	sets, total, err := f.store.List(context.Background(), 10, 0)
	if err != nil || total != 1 || len(sets) != 1 {
		t.Fatalf("failed staging changed catalog: total=%d rows=%d err=%v", total, len(sets), err)
	}
	var persisted db.DistributionSet
	if err := f.db.First(&persisted, "id = ?", set.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(persisted.Manifest, raw) || persisted.CreatedBy != "first-operator" {
		t.Fatal("idempotent or conflicting Stage replaced original bytes or actor")
	}
}

type cancelOnRead struct {
	reader io.Reader
	cancel context.CancelFunc
}

type cancelReadStorage struct {
	cache.Storage
	key    string
	cancel context.CancelFunc
}

func (s cancelReadStorage) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	r, size, err := s.Storage.Get(ctx, key)
	if err != nil || key != s.key {
		return r, size, err
	}
	return struct {
		io.Reader
		io.Closer
	}{Reader: cancelOnRead{reader: r, cancel: s.cancel}, Closer: r}, size, nil
}

func (r cancelOnRead) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.cancel()
	return n, err
}

func TestStorePutRejectsInvalidOrCanceledBytesWithoutPublishing(t *testing.T) {
	data := []byte("content-only package bytes")
	for _, name := range []string{"short", "long", "wrong-sha256", "cancel"} {
		t.Run(name, func(t *testing.T) {
			f := newCatalogFixture(t, distribution.Limits{})
			o := contentObject(data, "example")
			set := stageSet(t, f, "python", "v1", o)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var reader io.Reader
			want := distribution.ErrIntegrity
			switch name {
			case "short":
				reader = bytes.NewReader(data[:len(data)-1])
			case "long":
				reader = bytes.NewReader(append(append([]byte(nil), data...), 'x'))
			case "wrong-sha256":
				reader = bytes.NewReader(bytes.Repeat([]byte("x"), len(data)))
			case "cancel":
				reader = cancelOnRead{reader: bytes.NewReader(data), cancel: cancel}
				want = context.Canceled
			}
			if _, err := f.store.Put(ctx, set.ID, o.SHA256, reader); !errors.Is(err, want) {
				t.Fatalf("Put error=%v, want %v", err, want)
			}
			exists, err := f.storage.Exists(context.Background(), storageKey(o.SHA256))
			if err != nil || exists {
				t.Fatalf("invalid upload published: exists=%v err=%v", exists, err)
			}
			usage, err := f.store.Usage(context.Background())
			if err != nil || usage.Bytes != 0 || usage.Objects != 0 {
				t.Fatalf("invalid upload changed usage: %+v err=%v", usage, err)
			}
			if putContent(t, f, set, o, data).Reused {
				t.Fatal("valid retry falsely reused rejected object")
			}
			assertStoredContent(t, f, o, data)
		})
	}
}

func TestStorePutReusesVerifiedContentAndRepairsCorruption(t *testing.T) {
	f := newCatalogFixture(t, distribution.Limits{})
	data := []byte("content-only package bytes")
	o := contentObject(data, "example")
	first := stageSet(t, f, "python", "v1", o)
	second := stageSet(t, f, "python", "v2", o)
	if putContent(t, f, first, o, data).Reused || !putContent(t, f, second, o, data).Reused {
		t.Fatal("initial and cross-set reused upload states are wrong")
	}
	if _, err := f.store.Put(context.Background(), second.ID, o.SHA256, bytes.NewReader(bytes.Repeat([]byte("x"), len(data)))); !errors.Is(err, distribution.ErrIntegrity) {
		t.Fatalf("existing content bypassed incoming digest verification: %v", err)
	}
	assertStoredContent(t, f, o, data)
	for _, corrupted := range [][]byte{[]byte("truncated"), bytes.Repeat([]byte("x"), len(data))} {
		overwriteContent(t, f, o, corrupted)
		if putContent(t, f, second, o, data).Reused {
			t.Fatal("corrupted existing content was reported as reused")
		}
		assertStoredContent(t, f, o, data)
	}
	usage, err := f.store.Usage(context.Background())
	if err != nil || usage.Bytes != o.Size || usage.Objects != 1 || usage.Sets != 2 {
		t.Fatalf("deduplication/repair usage: %+v err=%v", usage, err)
	}
}

func TestStoreVerifyRechecksMissingAndCorruptContent(t *testing.T) {
	f := newCatalogFixture(t, distribution.Limits{})
	data := []byte("content-only package bytes")
	o := contentObject(data, "example")
	set := stageSet(t, f, "python", "v1", o)
	for _, code := range []string{"missing", "verified", "hash_mismatch", "size_mismatch", "missing-again"} {
		switch code {
		case "verified":
			putContent(t, f, set, o, data)
		case "hash_mismatch":
			overwriteContent(t, f, o, bytes.Repeat([]byte("x"), len(data)))
		case "size_mismatch":
			overwriteContent(t, f, o, []byte("short"))
		case "missing-again":
			if err := f.storage.Delete(context.Background(), storageKey(o.SHA256)); err != nil {
				t.Fatal(err)
			}
		}
		report, err := f.store.Verify(context.Background(), set.ID)
		if err != nil {
			t.Fatal(err)
		}
		current, err := f.store.Get(context.Background(), set.ID)
		if err != nil {
			t.Fatal(err)
		}
		if report.Serving || report.InstallValidation != "not_run" || report.ProvenanceStatus != "operator_declared" || current.CheckedAt == nil {
			t.Fatalf("content verification overclaimed or omitted check: %+v %+v", report, current)
		}
		if code == "verified" {
			if report.ContentStatus != "content_verified" || report.VerifiedObjects != 1 || len(report.Issues) != 0 || current.VerifiedAt == nil {
				t.Fatalf("valid bytes failed verification: %+v %+v", report, current)
			}
			continue
		}
		want := strings.TrimSuffix(code, "-again")
		if report.ContentStatus != "incomplete" || report.VerifiedObjects != 0 || len(report.Issues) != 1 || report.Issues[0].SHA256 != o.SHA256 || report.Issues[0].Code != want || current.ContentStatus != "incomplete" || current.VerifiedAt != nil {
			t.Fatalf("%s produced false readiness: %+v %+v", code, report, current)
		}
	}
}

func activePointer(t *testing.T, f *catalogFixture, name string) string {
	t.Helper()
	var active db.DistributionActiveSet
	if err := f.db.First(&active, "name = ?", name).Error; err != nil {
		t.Fatal(err)
	}
	return active.SetID
}

func TestStoreActivatePreservesPreviousPointerOnFailure(t *testing.T) {
	f := newCatalogFixture(t, distribution.Limits{})
	oldData, newData := []byte("old content-only bytes"), []byte("new content-only bytes")
	oldObject, newObject := contentObject(oldData, "old"), contentObject(newData, "new")
	oldSet := stageSet(t, f, "python", "v1", oldObject)
	newSet := stageSet(t, f, "python", "v2", newObject)
	putContent(t, f, oldSet, oldObject, oldData)
	if _, err := f.store.Activate(context.Background(), oldSet.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Activate(context.Background(), newSet.ID); !errors.Is(err, distribution.ErrIncomplete) || activePointer(t, f, oldSet.Name) != oldSet.ID {
		t.Fatalf("missing content changed active pointer: %v", err)
	}
	putContent(t, f, newSet, newObject, newData)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.store.Activate(ctx, newSet.ID); !errors.Is(err, context.Canceled) || activePointer(t, f, oldSet.Name) != oldSet.ID {
		t.Fatalf("canceled activation changed active pointer: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	cancelDuringCheck := f.newStore(t, cancelReadStorage{Storage: f.storage, key: storageKey(newObject.SHA256), cancel: cancel})
	if _, err := cancelDuringCheck.Activate(ctx, newSet.ID); !errors.Is(err, context.Canceled) || activePointer(t, f, oldSet.Name) != oldSet.ID {
		t.Fatalf("activation canceled during verification changed active pointer: %v", err)
	}

	// Defer constraint failure until COMMIT rather than failing the INSERT.
	// One connection ensures SQLite's connection-local foreign_keys setting
	// also applies to BeginDurableTransaction's pinned connection.
	sqlDB, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	for _, statement := range []string{
		"PRAGMA foreign_keys=ON",
		"CREATE TABLE activation_commit_parent (id INTEGER PRIMARY KEY)",
		"CREATE TABLE activation_commit_child (parent_id INTEGER REFERENCES activation_commit_parent(id) DEFERRABLE INITIALLY DEFERRED)",
		"CREATE TRIGGER fail_activation_commit AFTER UPDATE ON distribution_active_sets BEGIN INSERT INTO activation_commit_child(parent_id) VALUES (99); END",
	} {
		if err := f.db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.store.Activate(context.Background(), newSet.ID); err == nil || !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Fatalf("expected deferred COMMIT failure, got %v", err)
	}
	if activePointer(t, f, oldSet.Name) != oldSet.ID {
		t.Fatal("failed commit replaced previous active pointer")
	}
	if err := f.db.Exec("DROP TRIGGER fail_activation_commit").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Activate(context.Background(), newSet.ID); err != nil || activePointer(t, f, oldSet.Name) != newSet.ID {
		t.Fatalf("activation retry did not atomically switch: %v", err)
	}
}

func TestStoreReopenActiveRevalidatesContentAndFailsClosed(t *testing.T) {
	f := newCatalogFixture(t, distribution.Limits{})
	data := []byte("content-only package bytes")
	o := contentObject(data, "example")
	set := stageSet(t, f, "python", "v1", o)
	putContent(t, f, set, o, data)
	if _, err := f.store.Activate(context.Background(), set.ID); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := f.db.DB()
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	f.openDatabase(t)
	f.store = f.newStore(t, f.storage)
	if active, err := f.store.Active(context.Background(), set.Name); err != nil || active.ID != set.ID || active.Serving {
		t.Fatalf("reopened active state: %+v %v", active, err)
	}
	overwriteContent(t, f, o, bytes.Repeat([]byte("x"), len(data)))
	if active, err := f.store.Active(context.Background(), set.Name); active != nil || !errors.Is(err, distribution.ErrIncomplete) {
		t.Fatalf("corrupt active content did not fail closed: %+v %v", active, err)
	}
	if activePointer(t, f, set.Name) != set.ID {
		t.Fatal("content failure silently deselected the active revision")
	}
	putContent(t, f, set, o, data)
	if active, err := f.store.Active(context.Background(), set.Name); err != nil || active.ID != set.ID {
		t.Fatalf("repaired active revision unavailable: %+v %v", active, err)
	}
	if err := f.storage.Delete(context.Background(), storageKey(o.SHA256)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Active(context.Background(), set.Name); !errors.Is(err, distribution.ErrIncomplete) {
		t.Fatalf("restored catalog without bytes did not fail closed: %v", err)
	}
}

func TestStoreDeleteAndPruneRetainSharedAndActiveReferences(t *testing.T) {
	f := newCatalogFixture(t, distribution.Limits{})
	sharedData, oldData := []byte("shared content-only bytes"), []byte("old unique content-only bytes")
	shared, old := contentObject(sharedData, "shared"), contentObject(oldData, "old")
	first := stageSet(t, f, "python", "v1", shared, old)
	second := stageSet(t, f, "python", "v2", shared)
	putContent(t, f, first, shared, sharedData)
	putContent(t, f, first, old, oldData)
	if _, err := f.store.Activate(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(context.Background(), second.ID); !errors.Is(err, distribution.ErrActive) {
		t.Fatalf("active revision could be deleted: %v", err)
	}
	if err := f.store.Delete(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	orphanData := []byte("unrecorded orphan content")
	orphan := contentObject(orphanData, "orphan")
	overwriteContent(t, f, orphan, orphanData)
	removed, err := f.store.Prune(context.Background())
	if err != nil || removed != old.Size+orphan.Size {
		t.Fatalf("prune removed=%d want=%d err=%v", removed, old.Size+orphan.Size, err)
	}
	assertStoredContent(t, f, shared, sharedData)
	for _, o := range []distribution.Object{old, orphan} {
		if exists, err := f.storage.Exists(context.Background(), storageKey(o.SHA256)); err != nil || exists {
			t.Fatalf("unreferenced content retained: %s exists=%v err=%v", o.Package, exists, err)
		}
	}
	usage, err := f.store.Usage(context.Background())
	if err != nil || usage.Bytes != shared.Size || usage.Objects != 1 || usage.Sets != 1 {
		t.Fatalf("post-prune usage: %+v %v", usage, err)
	}
	if removed, err := f.store.Prune(context.Background()); err != nil || removed != 0 {
		t.Fatalf("repeated prune is not idempotent: %d %v", removed, err)
	}
}

func TestStoreQuotaCountsActualStoredBytesAndAllowsRepair(t *testing.T) {
	f := newCatalogFixture(t, distribution.Limits{MaxObjectBytes: 8, MaxStorageBytes: 10})
	aData, bData := []byte("aaaaaa"), []byte("bbbbbb")
	a, b := contentObject(aData, "a"), contentObject(bData, "b")
	if _, err := f.store.Stage(context.Background(), manifestBytes(t, "oversized", "v1", contentObject([]byte("123456789"), "large")), "operator"); !errors.Is(err, distribution.ErrQuota) {
		t.Fatalf("oversized object accepted: %v", err)
	}
	if _, err := f.store.Stage(context.Background(), manifestBytes(t, "oversized", "v2", a, b), "operator"); !errors.Is(err, distribution.ErrQuota) {
		t.Fatalf("set exceeding quota accepted: %v", err)
	}
	first := stageSet(t, f, "first", "v1", a)
	second := stageSet(t, f, "second", "v1", b)
	putContent(t, f, first, a, aData)
	if _, err := f.store.Put(context.Background(), second.ID, b.SHA256, bytes.NewReader(bData)); !errors.Is(err, distribution.ErrQuota) {
		t.Fatalf("aggregate storage quota ignored: %v", err)
	}
	if !putContent(t, f, first, a, aData).Reused {
		t.Fatal("existing valid object unnecessarily consumed quota")
	}
	overwriteContent(t, f, a, []byte("xxxxxx"))
	putContent(t, f, first, a, aData)
	if err := f.store.Delete(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	putContent(t, f, second, b, bData)
}

func TestStoreEnforcesReducedUploadLimitAfterReopen(t *testing.T) {
	f := newCatalogFixture(t, distribution.Limits{})
	data := []byte("retained bytes")
	object := contentObject(data, "limit")
	set := stageSet(t, f, "limits", "v1", object)
	putContent(t, f, set, object, data)
	if _, err := distribution.NewStore(f.db, f.storage, distribution.Limits{MaxObjectBytes: math.MaxInt64, MaxStorageBytes: math.MaxInt64}); !errors.Is(err, distribution.ErrInvalid) {
		t.Fatalf("overflowing reader limit accepted: %v", err)
	}
	f.limits.MaxObjectBytes = 1
	f.store = f.newStore(t, f.storage)
	if _, err := f.store.Put(context.Background(), set.ID, object.SHA256, bytes.NewReader(data)); !errors.Is(err, distribution.ErrQuota) {
		t.Fatalf("reopened store ignored reduced object limit: %v", err)
	}
	report, err := f.store.Verify(context.Background(), set.ID)
	if err != nil || report.ContentStatus != "content_verified" {
		t.Fatalf("existing retained content became unreadable: %v, %v", report, err)
	}
	assertStoredContent(t, f, object, data)
}

type pausedReader struct {
	reader  io.Reader
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *pausedReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.entered) })
	<-r.release
	return r.reader.Read(p)
}

type observedWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *observedWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

type observedStorage struct {
	cache.Storage
	putInProgress atomic.Bool
	listDuringPut atomic.Bool
	listCalls     atomic.Int32
}

func (s *observedStorage) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	s.putInProgress.Store(true)
	defer s.putInProgress.Store(false)
	return s.Storage.Put(ctx, key, reader, size, contentType)
}

func (s *observedStorage) List(ctx context.Context, prefix string) ([]cache.ObjectMeta, error) {
	s.listCalls.Add(1)
	if s.putInProgress.Load() {
		s.listDuringPut.Store(true)
	}
	return s.Storage.List(ctx, prefix)
}

func waitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not reach synchronization point")
	}
}

func waitError(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not finish")
		return nil
	}
}

func TestStoreUploadAndPruneAreSerializedAndWaitersCanCancel(t *testing.T) {
	f := newCatalogFixture(t, distribution.Limits{})
	observed := &observedStorage{Storage: f.storage}
	f.store = f.newStore(t, observed)
	data := []byte("content-only package bytes")
	o := contentObject(data, "example")
	set := stageSet(t, f, "python", "v1", o)
	reader := &pausedReader{reader: bytes.NewReader(data), entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(reader.release) }) })
	uploaded := make(chan error, 1)
	go func() {
		_, err := f.store.Put(context.Background(), set.ID, o.SHA256, reader)
		uploaded <- err
	}()
	waitSignal(t, reader.entered)
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	canceledWaiter := &observedWaitContext{Context: base, waiting: make(chan struct{})}
	canceled := make(chan error, 1)
	go func() { _, err := f.store.Prune(canceledWaiter); canceled <- err }()
	waitSignal(t, canceledWaiter.waiting)
	cancel()
	if err := waitError(t, canceled); !errors.Is(err, context.Canceled) || observed.listCalls.Load() != 0 {
		t.Fatalf("waiting GC did not cancel before storage access: %v calls=%d", err, observed.listCalls.Load())
	}
	queuedWaiter := &observedWaitContext{Context: context.Background(), waiting: make(chan struct{})}
	pruned := make(chan error, 1)
	go func() { _, err := f.store.Prune(queuedWaiter); pruned <- err }()
	waitSignal(t, queuedWaiter.waiting)
	release.Do(func() { close(reader.release) })
	if err := waitError(t, uploaded); err != nil {
		t.Fatal(err)
	}
	if err := waitError(t, pruned); err != nil {
		t.Fatal(err)
	}
	if observed.listDuringPut.Load() || observed.listCalls.Load() != 1 {
		t.Fatalf("GC overlapped upload: overlap=%v calls=%d", observed.listDuringPut.Load(), observed.listCalls.Load())
	}
	assertStoredContent(t, f, o, data)
}
