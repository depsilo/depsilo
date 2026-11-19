package blocklist

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"depsilo/internal/asyncruntime"
	"depsilo/internal/db"
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

func TestSyncMatchesPyPIAndRubyGemsRequestIdentities(t *testing.T) {
	archive := buildZip(t, map[string]string{
		"MAL-2026-PYPI.json": `{
			"id":"MAL-2026-PYPI",
			"summary":"malicious PyPI release",
			"affected":[{
				"package":{"ecosystem":"PyPI","name":"Evil_Package"},
				"versions":["1.0.0","2.0.0RC1"]
			}]
		}`,
		"MAL-2026-PYPI-ALL.json": `{
			"id":"MAL-2026-PYPI-ALL",
			"summary":"typosquat",
			"affected":[{
				"package":{"ecosystem":"PyPI","name":"ascii2text"},
				"ranges":[{"type":"ECOSYSTEM","events":[{"introduced":"0"}]}]
			}]
		}`,
		"MAL-2026-GEM.json": `{
			"id":"MAL-2026-GEM",
			"summary":"malicious gem release",
			"affected":[{
				"package":{"ecosystem":"RubyGems","name":"evil_gem"},
				"versions":["6.0.1","1.0.0.pre.1"]
			}]
		}`,
	})
	srv := newMockOSV(t, archive)
	defer srv.Close()

	store := testStore(t)
	syncer, err := NewSyncer(store, Config{MirrorURL: srv.URL, SyncInterval: "6h"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := syncer.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}

	// The adapter normalizes artifact identity before the check, so every
	// PEP 503 spelling of the dataset row must match.
	for _, spelling := range []string{"Evil_Package", "evil-package", "evil_package"} {
		if m, _, err := store.Check(ctx, "pypi", spelling, "1.0.0"); err != nil || m == nil {
			t.Fatalf("pypi check(%q) = %+v, %v; want match", spelling, m, err)
		}
	}
	// The PEP 440 dialect normalizes the dataset's version spelling too.
	if m, _, err := store.Check(ctx, "pypi", "evil-package", "2.0.0rc1"); err != nil || m == nil {
		t.Fatalf("pypi normalized prerelease check = %+v, %v; want match", m, err)
	}
	if m, _, _ := store.Check(ctx, "pypi", "evil-package", "2.0.0"); m != nil {
		t.Fatalf("pypi unrelated version matched: %+v", m)
	}
	// An advisory that marks every range as affected, the dataset's dominant
	// shape, blocks every version.
	if m, _, err := store.Check(ctx, "pypi", "ascii2text", "9.9.9"); err != nil || m == nil {
		t.Fatalf("pypi all-versions check = %+v, %v; want match", m, err)
	}

	// RubyGems names keep their exact case; the adapter resolves the base
	// version before checking, so platform artifacts match here too.
	// RubyGems versions keep their exact spelling, prerelease dots included.
	for _, version := range []string{"6.0.1", "1.0.0.pre.1"} {
		if m, _, err := store.Check(ctx, "rubygems", "evil_gem", version); err != nil || m == nil {
			t.Fatalf("rubygems check(%q) = %+v, %v; want match", version, m, err)
		}
	}
	if m, _, _ := store.Check(ctx, "rubygems", "Evil_Gem", "6.0.1"); m != nil {
		t.Fatalf("rubygems case-folded name matched: %+v", m)
	}
	if m, _, _ := store.Check(ctx, "rubygems", "evil_gem", "7.0.0"); m != nil {
		t.Fatalf("rubygems unrelated version matched: %+v", m)
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
	result, err := syncer.SyncOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The same archive is served for every ecosystem, but each import
	// keeps only its own ecosystem's affected sections — the two npm
	// advisories land exactly once, under npm.
	if count := result.Imported(); count != 2 || result.Refreshed["npm"] != 2 {
		t.Fatalf("imported %d rows (%v), want 2 under npm", count, result.Refreshed)
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
		// SyncOnce alone doesn't record state (runOnce does) — record
		// explicitly the way runOnce would, then read back.
		if err := store.RecordSync(ctx, result, 0); err != nil {
			t.Fatal(err)
		}
		st, err := store.SyncState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.LastSuccessAt == nil || st.EntryCount != 2 || st.LastError != "" {
			t.Errorf("unexpected state: %+v", st)
		}
		perEcosystem, err := store.EcosystemSyncStates(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, eco := range SyncedEcosystems() {
			if state := perEcosystem[eco]; state.LastSuccessAt == nil || state.LastError != "" {
				t.Errorf("%s state = %+v, want fresh", eco, state)
			}
		}
	})
}

func TestSyncOnceReportsMalformedAdvisoriesInOneLine(t *testing.T) {
	archive := buildZip(t, map[string]string{
		"MAL-2026-1111.json": malAllVersions,
		"MAL-2026-3333.json": `{not json`,
		"MAL-2026-4444.json": `{also not json`,
	})
	srv := newMockOSV(t, archive)
	defer srv.Close()

	syncer, err := NewSyncer(testStore(t), Config{MirrorURL: srv.URL, SyncInterval: "6h"})
	if err != nil {
		t.Fatal(err)
	}

	core, logs := observer.New(zap.DebugLevel)
	t.Cleanup(zap.ReplaceGlobals(zap.New(core)))

	if _, err := syncer.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	// The mock serves the same archive for every ecosystem, so both
	// malformed advisories are skipped once per ecosystem.
	want := 2 * len(SyncedEcosystems())
	warnings := logs.FilterLevelExact(zap.WarnLevel).All()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %+v, want one summary line", warnings)
	}
	if got := warnings[0].ContextMap()["count"]; got != int64(want) {
		t.Fatalf("summary count = %v, want %d", got, want)
	}
	if !strings.Contains(warnings[0].Message, "skipped malformed advisories") {
		t.Fatalf("summary message = %q", warnings[0].Message)
	}

	details := logs.FilterLevelExact(zap.DebugLevel).FilterMessage("blocklist: skipping malformed advisory").All()
	if len(details) != want {
		t.Fatalf("debug detail lines = %d, want %d", len(details), want)
	}
	files := make(map[string]bool)
	for _, entry := range details {
		files[entry.ContextMap()["file"].(string)] = true
	}
	if !files["MAL-2026-3333.json"] || !files["MAL-2026-4444.json"] || len(files) != 2 {
		t.Fatalf("debug lines name %v, want both malformed advisories", files)
	}
}

func TestSyncOnceStaysQuietWithoutMalformedAdvisories(t *testing.T) {
	srv := newMockOSV(t, buildZip(t, map[string]string{"MAL-2026-1111.json": malAllVersions}))
	defer srv.Close()

	syncer, err := NewSyncer(testStore(t), Config{MirrorURL: srv.URL, SyncInterval: "6h"})
	if err != nil {
		t.Fatal(err)
	}

	core, logs := observer.New(zap.WarnLevel)
	t.Cleanup(zap.ReplaceGlobals(zap.New(core)))

	if _, err := syncer.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if logs.Len() != 0 {
		t.Fatalf("warnings = %+v, want none", logs.All())
	}
}

// osvMirror serves a per-ecosystem archive by OSV name; an ecosystem with
// a status code set answers that status instead.
type osvMirror struct {
	mu       sync.Mutex
	archives map[string][]byte
	statuses map[string]int
}

func (m *osvMirror) set(osvName string, archive []byte, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.archives[osvName], m.statuses[osvName] = archive, status
}

func newOSVMirror(t *testing.T, fallback []byte) (*osvMirror, *httptest.Server) {
	t.Helper()
	mirror := &osvMirror{archives: map[string][]byte{}, statuses: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		osvName, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/all.zip")
		if !ok {
			http.NotFound(w, r)
			return
		}
		mirror.mu.Lock()
		archive, status := mirror.archives[osvName], mirror.statuses[osvName]
		mirror.mu.Unlock()
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		if archive == nil {
			archive = fallback
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	}))
	t.Cleanup(srv.Close)
	return mirror, srv
}

// steppingClock hands out strictly increasing times so a test can tell
// which run recorded a timestamp.
func steppingClock() func() time.Time {
	var mu sync.Mutex
	current := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		current = current.Add(time.Minute)
		return current
	}
}

const malMaven = `{
	"id": "MAL-2026-7777",
	"summary": "malicious maven artifact",
	"modified": "2026-07-01T00:00:00Z",
	"affected": [{
		"package": {"ecosystem": "Maven", "name": "com.evil:stealer"},
		"ranges": [{"type": "ECOSYSTEM", "events": [{"introduced": "0"}]}]
	}]
}`

const malNPMNew = `{
	"id": "MAL-2026-8888",
	"summary": "newly published npm malware",
	"modified": "2026-07-05T00:00:00Z",
	"affected": [{
		"package": {"ecosystem": "npm", "name": "fresh-evil"},
		"ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}]}]
	}]
}`

const malMavenNew = `{
	"id": "MAL-2026-9999",
	"summary": "newly published maven malware",
	"modified": "2026-07-05T00:00:00Z",
	"affected": [{
		"package": {"ecosystem": "Maven", "name": "com.evil:fresh"},
		"ranges": [{"type": "ECOSYSTEM", "events": [{"introduced": "0"}]}]
	}]
}`

func TestSyncOneFailingEcosystemDoesNotHoldBackTheOthers(t *testing.T) {
	first := buildZip(t, map[string]string{"MAL-2026-1111.json": malAllVersions, "MAL-2026-7777.json": malMaven})
	mirror, srv := newOSVMirror(t, first)

	store := testStore(t)
	store.now = steppingClock()
	syncer, err := NewSyncer(store, Config{MirrorURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	syncer.runOnce(ctx)
	firstRun, _ := store.SyncState(ctx)
	if firstRun.LastSuccessAt == nil || firstRun.LastError != "" {
		t.Fatalf("first run state = %+v, want success", firstRun)
	}

	// The mirror loses Maven; npm publishes a new advisory.
	mirror.set("Maven", nil, http.StatusInternalServerError)
	mirror.set("npm", buildZip(t, map[string]string{"MAL-2026-1111.json": malAllVersions, "MAL-2026-8888.json": malNPMNew}), http.StatusOK)

	result, err := syncer.SyncOnce(ctx)
	if err == nil || !strings.Contains(err.Error(), "maven: fetch") {
		t.Fatalf("SyncOnce error = %v, want the maven failure", err)
	}
	if len(result.Failed) != 1 || result.Failed["maven"] == nil {
		t.Fatalf("failed = %v, want only maven", result.Failed)
	}
	if result.Refreshed["npm"] != 2 || len(result.Refreshed) != len(SyncedEcosystems())-1 {
		t.Fatalf("refreshed = %v, want every other ecosystem", result.Refreshed)
	}
	if m, _, _ := store.Check(ctx, "npm", "fresh-evil", "1.0.0"); m == nil {
		t.Error("npm did not refresh because maven failed")
	}
	if m, _, _ := store.Check(ctx, "maven", "com.evil:stealer", "1.0.0"); m == nil {
		t.Error("maven lost its last good data")
	}

	if err := store.RecordSync(ctx, result, 0); err != nil {
		t.Fatal(err)
	}
	st, _ := store.SyncState(ctx)
	if !strings.HasPrefix(st.LastError, "maven: fetch ") || strings.Contains(st.LastError, "npm") {
		t.Errorf("run-level error = %q, want only the maven failure", st.LastError)
	}
	if st.LastSuccessAt == nil || !st.LastSuccessAt.Equal(*firstRun.LastSuccessAt) || !st.LastSyncAt.After(*firstRun.LastSyncAt) {
		t.Errorf("partial run state = %+v, want the attempt recorded and the full-success time kept", st)
	}
	perEcosystem, err := store.EcosystemSyncStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	maven, npm := perEcosystem["maven"], perEcosystem["npm"]
	if maven.LastSuccessAt == nil || !maven.LastSuccessAt.Equal(*firstRun.LastSuccessAt) || !strings.Contains(maven.LastError, "HTTP 500") {
		t.Errorf("maven state = %+v, want the first success kept and the failure recorded", maven)
	}
	if npm.LastSuccessAt == nil || !npm.LastSuccessAt.Equal(*st.LastSyncAt) || npm.LastError != "" {
		t.Errorf("npm state = %+v, want refreshed by the second run", npm)
	}

	// Maven recovers: the run-level success moves and every error clears.
	mirror.set("Maven", nil, http.StatusOK)
	syncer.runOnce(ctx)
	st, _ = store.SyncState(ctx)
	if st.LastError != "" || !st.LastSuccessAt.Equal(*st.LastSyncAt) {
		t.Errorf("recovered state = %+v, want a full success", st)
	}
	perEcosystem, _ = store.EcosystemSyncStates(ctx)
	if perEcosystem["maven"].LastError != "" {
		t.Errorf("maven error survived recovery: %+v", perEcosystem["maven"])
	}
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
	syncer.runOnce(ctx)
	good, _ := store.SyncState(ctx)
	if good.LastSuccessAt == nil || good.EntryCount != 1 || good.LastError != "" {
		t.Fatalf("first sync state = %+v", good)
	}

	// Mirror goes away — every ecosystem fails, data must survive.
	srv.Close()
	result, syncErr := syncer.SyncOnce(ctx)
	if syncErr == nil || len(result.Refreshed) != 0 || len(result.Failed) != len(SyncedEcosystems()) {
		t.Fatalf("dead mirror: refreshed=%v failed=%v err=%v, want every ecosystem failed", result.Refreshed, result.Failed, syncErr)
	}
	if err := store.RecordSync(ctx, result, 0); err != nil {
		t.Fatal(err)
	}

	if m, _, _ := store.Check(ctx, "npm", "Evil-Pkg", "1.0.0"); m == nil {
		t.Error("last good dataset lost after failed sync")
	}
	st, _ := store.SyncState(ctx)
	if st.LastSuccessAt == nil || st.EntryCount != 1 || st.LastError == "" {
		t.Errorf("success state clobbered by failure: %+v", st)
	}
	perEcosystem, _ := store.EcosystemSyncStates(ctx)
	for _, eco := range SyncedEcosystems() {
		if state := perEcosystem[eco]; state.LastSuccessAt == nil || state.LastError == "" || !strings.Contains(st.LastError, eco+": ") {
			t.Errorf("%s state = %+v (run error %q), want last success kept and failure recorded", eco, state, st.LastError)
		}
	}
}

func TestEcosystemSyncStatesFallBackToTheRunLevelRecord(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	// A database last synced by a release without per-ecosystem state.
	synced := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := store.db.Create(&db.BlocklistSyncState{ID: 1, LastSyncAt: &synced, LastSuccessAt: &synced, LastError: "maven: HTTP 404"}).Error; err != nil {
		t.Fatal(err)
	}
	perEcosystem, err := store.EcosystemSyncStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, eco := range SyncedEcosystems() {
		if state := perEcosystem[eco]; state.LastSuccessAt == nil || !state.LastSuccessAt.Equal(synced) || state.LastError != "maven: HTTP 404" {
			t.Errorf("%s state = %+v, want the run-level record", eco, state)
		}
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
	result, err := syncer.SyncOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Only the genuine all-versions advisory imports; withdrawn and
	// both bounded-range shapes are skipped.
	if count := result.Imported(); count != 1 {
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
	good := buildZip(t, map[string]string{"MAL-2026-1111.json": malAllVersions, "MAL-2026-7777.json": malMaven})
	empty := buildZip(t, map[string]string{"GHSA-not-mal.json": ghsaEntry})
	mirror, srv := newOSVMirror(t, good)

	store := testStore(t)
	syncer, err := NewSyncer(store, Config{MirrorURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := syncer.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}

	// A mirror that starts serving a MAL-free npm archive must not wipe
	// npm, and must not hold back an ecosystem whose archive is fine.
	mirror.set("npm", empty, http.StatusOK)
	mirror.set("Maven", buildZip(t, map[string]string{"MAL-2026-7777.json": malMaven, "MAL-2026-9999.json": malMavenNew}), http.StatusOK)
	result, err := syncer.SyncOnce(ctx)
	if err == nil || result.Failed["npm"] == nil || !strings.Contains(result.Failed["npm"].Error(), "refusing to wipe") {
		t.Fatalf("zero-entry npm archive over a populated store: failed=%v err=%v", result.Failed, err)
	}
	if len(result.Failed) != 1 || result.Refreshed["maven"] != 2 {
		t.Fatalf("refreshed=%v failed=%v, want only npm held back", result.Refreshed, result.Failed)
	}
	if m, _, _ := store.Check(ctx, "npm", "Evil-Pkg", "1.0.0"); m == nil {
		t.Error("populated ecosystem was wiped by an empty archive")
	}
	if m, _, _ := store.Check(ctx, "maven", "com.evil:fresh", "1.0.0"); m == nil {
		t.Error("maven did not refresh because npm hit the zero-wipe guard")
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
