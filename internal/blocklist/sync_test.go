package blocklist

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"depsilo/internal/asyncruntime"
)

func TestParseAdvisoryNormalizesExplicitVersionsWithDialect(t *testing.T) {
	t.Parallel()

	document := `{
		"id":"MAL-NUGET-DIALECT",
		"summary":"test",
		"affected":[{
			"package":{"ecosystem":"NuGet","name":"Example.Client"},
			"versions":["01.0.0.0-Alpha+build.7"]
		}]
	}`
	rows, err := parseAdvisory(strings.NewReader(document), "nuget", "NuGet", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Package != "example.client" || rows[0].Versions != `["1.0.0-alpha"]` {
		t.Fatalf("normalized advisory rows = %#v", rows)
	}
}

func TestParseAdvisoryRejectsInvalidExplicitVersion(t *testing.T) {
	t.Parallel()

	document := `{
		"id":"MAL-NUGET-BAD",
		"summary":"test",
		"affected":[{
			"package":{"ecosystem":"NuGet","name":"Example.Client"},
			"versions":["1..0"]
		}]
	}`
	rows, err := parseAdvisory(strings.NewReader(document), "nuget", "NuGet", time.Unix(1, 0).UTC())
	if err == nil || len(rows) != 0 || !strings.Contains(err.Error(), "invalid nuget version") {
		t.Fatalf("invalid advisory = rows %#v err %v", rows, err)
	}
}

type submitterFunc func(asyncruntime.Task) error

func (submit submitterFunc) Submit(task asyncruntime.Task) error { return submit(task) }

// buildZip assembles an in-memory OSV-style archive: name → JSON body.
func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const malAllVersions = `{
	"id": "MAL-2026-1111",
	"summary": "evil-pkg is a credential stealer",
	"aliases": ["GHSA-aaaa-bbbb-cccc"],
	"modified": "2026-07-01T00:00:00Z",
	"affected": [{
		"package": {"ecosystem": "npm", "name": "Evil-Pkg"},
		"ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}]}]
	}]
}`

const malVersionList = `{
	"id": "MAL-2026-2222",
	"summary": "compromised release of sometimes-evil",
	"modified": "2026-07-02T00:00:00Z",
	"affected": [{
		"package": {"ecosystem": "npm", "name": "sometimes-evil"},
		"versions": ["1.2.3"],
		"ranges": [{"type": "ECOSYSTEM", "events": [{"introduced": "1.2.3"}, {"fixed": "1.2.4"}]}]
	}]
}`

const ghsaEntry = `{
	"id": "GHSA-zzzz-yyyy-xxxx",
	"summary": "an ordinary CVE that must NOT be imported",
	"modified": "2026-07-03T00:00:00Z",
	"affected": [{"package": {"ecosystem": "npm", "name": "left-pad"}, "versions": ["1.0.0"]}]
}`

// newMockOSV serves the same archive for every ecosystem path.
func newMockOSV(t *testing.T, archive []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/all.zip") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	}))
}

func TestSyncOnce(t *testing.T) {
	archive := buildZip(t, map[string]string{
		"MAL-2026-1111.json":       malAllVersions,
		"MAL-2026-2222.json":       malVersionList,
		"GHSA-zzzz-yyyy-xxxx.json": ghsaEntry,
		"MAL-2026-3333.json":       `{not json`, // malformed → skipped, not fatal
	})
	srv := newMockOSV(t, archive)
	defer srv.Close()

	store := testStore(t)
	syncer, err := NewSyncer(store, Config{MirrorURL: srv.URL, SyncInterval: "6h"})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	count, err := syncer.SyncOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The same archive is served for every ecosystem, but each import
	// keeps only its own ecosystem's affected sections — the two npm
	// advisories land exactly once, under npm.
	if count != 2 {
		t.Fatalf("imported %d rows, want 2", count)
	}

	t.Run("GHSA entries are never imported", func(t *testing.T) {
		if m, _, _ := store.Check(ctx, "npm", "left-pad", "1.0.0"); m != nil {
			t.Errorf("GHSA advisory imported: %+v", m)
		}
	})

	t.Run("all-versions advisory preserves name + matches exactly", func(t *testing.T) {
		m, _, err := store.Check(ctx, "npm", "Evil-Pkg", "42.0.0")
		if err != nil || m == nil {
			t.Fatalf("m=%v err=%v", m, err)
		}
		if m.SourceID != "MAL-2026-1111" {
			t.Errorf("SourceID = %s", m.SourceID)
		}
		if m, _, err := store.Check(ctx, "npm", "evil-pkg", "42.0.0"); err != nil || m != nil {
			t.Fatalf("case-distinct request matched Evil-Pkg: m=%v err=%v", m, err)
		}
	})

	t.Run("version-list advisory matches only listed versions", func(t *testing.T) {
		if m, _, _ := store.Check(ctx, "npm", "sometimes-evil", "1.2.3"); m == nil {
			t.Error("listed version should match")
		}
		if m, _, _ := store.Check(ctx, "npm", "sometimes-evil", "1.2.4"); m != nil {
			t.Error("fixed version must not match")
		}
	})

	t.Run("sync state records success", func(t *testing.T) {
		st, err := store.SyncState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		// SyncOnce alone doesn't record state (runOnce does) — record
		// explicitly the way runOnce would, then read back.
		if err := store.RecordSync(ctx, nil, count, 0); err != nil {
			t.Fatal(err)
		}
		st, _ = store.SyncState(ctx)
		if st.LastSuccessAt == nil || st.EntryCount != 2 || st.LastError != "" {
			t.Errorf("unexpected state: %+v", st)
		}
	})
}

func TestSyncFailureKeepsLastGoodData(t *testing.T) {
	archive := buildZip(t, map[string]string{"MAL-2026-1111.json": malAllVersions})
	srv := newMockOSV(t, archive)

	store := testStore(t)
	syncer, err := NewSyncer(store, Config{MirrorURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	count, err := syncer.SyncOnce(ctx)
	if err != nil || count != 1 {
		t.Fatalf("first sync: count=%d err=%v", count, err)
	}
	if err := store.RecordSync(ctx, nil, count, 0); err != nil {
		t.Fatal(err)
	}

	// Mirror goes away — the next sync fails, data must survive.
	srv.Close()
	_, syncErr := syncer.SyncOnce(ctx)
	if syncErr == nil {
		t.Fatal("sync against a dead mirror should fail")
	}
	if err := store.RecordSync(ctx, syncErr, 0, 0); err != nil {
		t.Fatal(err)
	}

	if m, _, _ := store.Check(ctx, "npm", "Evil-Pkg", "1.0.0"); m == nil {
		t.Error("last good dataset lost after failed sync")
	}
	st, _ := store.SyncState(ctx)
	if st.LastSuccessAt == nil || st.EntryCount != 1 {
		t.Errorf("success state clobbered by failure: %+v", st)
	}
}

func TestNewSyncerValidation(t *testing.T) {
	store := testStore(t)
	if _, err := NewSyncer(store, Config{SyncInterval: "nonsense"}); err == nil {
		t.Error("invalid interval accepted")
	}
	if _, err := NewSyncer(store, Config{SyncInterval: "5s"}); err == nil {
		t.Error("sub-minute interval accepted")
	}
	if _, err := NewSyncer(store, Config{Proxy: "://bad"}); err == nil {
		t.Error("invalid proxy accepted")
	}
}

// ── v0.8.0 review regression tests ─────────────────────────────────

const malWithdrawn = `{
	"id": "MAL-2026-4444",
	"summary": "retracted false positive against a legit package",
	"modified": "2026-07-01T00:00:00Z",
	"withdrawn": "2026-07-02T00:00:00Z",
	"affected": [{
		"package": {"ecosystem": "npm", "name": "vindicated-pkg"},
		"ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}]}]
	}]
}`

// The fsevents shape: compromised window bounded by a fixed event, no
// explicit version enumeration. Importing this as all-versions would
// 451 every clean release.
const malBoundedFixed = `{
	"id": "MAL-2026-5555",
	"summary": "fsevents-shaped advisory: compromised then fixed",
	"modified": "2026-07-01T00:00:00Z",
	"affected": [{
		"package": {"ecosystem": "npm", "name": "was-compromised"},
		"ranges": [{"type": "SEMVER", "events": [{"introduced": "1.0.0"}, {"fixed": "1.2.11"}]}]
	}]
}`

// The @ledgerhq shape: bounded by last_affected instead of fixed.
const malBoundedLastAffected = `{
	"id": "MAL-2026-6666",
	"summary": "connect-kit-shaped advisory bounded by last_affected",
	"modified": "2026-07-01T00:00:00Z",
	"affected": [{
		"package": {"ecosystem": "npm", "name": "briefly-compromised"},
		"ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}, {"last_affected": "1.1.7"}]}]
	}]
}`

func TestSync_ReviewRegressions(t *testing.T) {
	archive := buildZip(t, map[string]string{
		"MAL-2026-1111.json": malAllVersions,
		"MAL-2026-4444.json": malWithdrawn,
		"MAL-2026-5555.json": malBoundedFixed,
		"MAL-2026-6666.json": malBoundedLastAffected,
	})
	srv := newMockOSV(t, archive)
	defer srv.Close()

	store := testStore(t)
	syncer, err := NewSyncer(store, Config{MirrorURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	count, err := syncer.SyncOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Only the genuine all-versions advisory imports; withdrawn and
	// both bounded-range shapes are skipped.
	if count != 1 {
		t.Fatalf("imported %d rows, want 1", count)
	}
	if m, _, _ := store.Check(ctx, "npm", "vindicated-pkg", "1.0.0"); m != nil {
		t.Error("withdrawn advisory imported as an active block")
	}
	if m, _, _ := store.Check(ctx, "npm", "was-compromised", "2.3.3"); m != nil {
		t.Error("fixed-bounded advisory blocks clean versions")
	}
	if m, _, _ := store.Check(ctx, "npm", "briefly-compromised", "1.1.8"); m != nil {
		t.Error("last_affected-bounded advisory blocks clean versions")
	}
	if m, _, _ := store.Check(ctx, "npm", "Evil-Pkg", "1.0.0"); m == nil {
		t.Error("genuine all-versions advisory should still block")
	}
}

func TestSync_ZeroWipeGuard(t *testing.T) {
	good := buildZip(t, map[string]string{"MAL-2026-1111.json": malAllVersions})
	empty := buildZip(t, map[string]string{"GHSA-not-mal.json": ghsaEntry})

	current := good
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(current)
	}))
	defer srv.Close()

	store := testStore(t)
	syncer, err := NewSyncer(store, Config{MirrorURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := syncer.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}

	// A mirror that starts serving MAL-free archives must not wipe.
	current = empty
	if _, err := syncer.SyncOnce(ctx); err == nil {
		t.Fatal("zero-entry archive over a populated store must fail the sync")
	}
	if m, _, _ := store.Check(ctx, "npm", "Evil-Pkg", "1.0.0"); m == nil {
		t.Error("populated ecosystem was wiped by an empty archive")
	}
}

func TestSyncer_ConcurrencyGuard(t *testing.T) {
	store := testStore(t)
	syncer, err := NewSyncer(store, Config{MirrorURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	syncer.running.Store(true) // simulate an in-flight run
	if err := syncer.TryStartSync(submitterFunc(func(asyncruntime.Task) error {
		t.Fatal("running sync must not submit another task")
		return nil
	})); !errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("TryStartSync error = %v, want ErrSyncInProgress", err)
	}
	if !syncer.Running() {
		t.Error("Running() should reflect the in-flight state")
	}
}

func TestTryStartSyncReservesBeforeScheduling(t *testing.T) {
	store := testStore(t)
	syncer, err := NewSyncer(store, Config{MirrorURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var scheduled asyncruntime.Task
	submitter := submitterFunc(func(task asyncruntime.Task) error {
		scheduled = task
		return nil
	})
	if err := syncer.TryStartSync(submitter); err != nil {
		t.Fatalf("first sync was not accepted: %v", err)
	}
	if scheduled == nil || !syncer.Running() {
		t.Fatal("accepted sync was not reserved and scheduled")
	}
	if err := syncer.TryStartSync(submitterFunc(func(asyncruntime.Task) error {
		t.Fatal("second sync must not be scheduled")
		return nil
	})); !errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("second sync error = %v, want ErrSyncInProgress", err)
	}

	cancel()
	scheduled(ctx)
	if syncer.Running() {
		t.Fatal("sync reservation was not released after task exit")
	}
}

func TestTryStartSyncRollsBackWhenRuntimeRejects(t *testing.T) {
	store := testStore(t)
	syncer, err := NewSyncer(store, Config{MirrorURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	rejecting := submitterFunc(func(asyncruntime.Task) error { return asyncruntime.ErrClosed })
	err = syncer.TryStartSync(rejecting)
	if !errors.Is(err, ErrSyncUnavailable) || !errors.Is(err, asyncruntime.ErrClosed) {
		t.Fatalf("TryStartSync error = %v, want unavailable/closed", err)
	}
	if syncer.Running() {
		t.Fatal("rejected submission retained running reservation")
	}
}
