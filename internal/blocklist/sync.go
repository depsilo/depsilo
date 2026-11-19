package blocklist

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/asyncruntime"
	"depsilo/internal/db"
	"depsilo/internal/ecosystem"
)

// Syncer downloads the OSV bulk dataset per ecosystem, keeps only the
// MAL-* (malicious-packages) advisories, and swaps them into the local
// store. Built once at server startup; Start runs it on a schedule.
type Syncer struct {
	store    *Store
	client   *http.Client
	mirror   string
	interval time.Duration
	now      func() time.Time
	// running guards against overlapping syncs — the scheduler tick,
	// admin "sync now" clicks and slow archives must never interleave
	// ReplaceEcosystem transactions (v0.8.0 review finding).
	running atomic.Bool
}

var (
	ErrSyncInProgress  = errors.New("blocklist sync already running")
	ErrSyncUnavailable = errors.New("blocklist sync is unavailable")
)

func NewSyncer(store *Store, cfg Config) (*Syncer, error) {
	mirror := strings.TrimRight(cfg.MirrorURL, "/")
	if mirror == "" {
		mirror = DefaultMirrorURL
	}
	interval := DefaultSyncInterval
	if cfg.SyncInterval != "" {
		d, err := time.ParseDuration(cfg.SyncInterval)
		if err != nil {
			return nil, fmt.Errorf("blocklist: invalid sync_interval %q: %w", cfg.SyncInterval, err)
		}
		if d < time.Minute {
			return nil, fmt.Errorf("blocklist: sync_interval %q is below the 1m floor", cfg.SyncInterval)
		}
		interval = d
	}

	transport := &http.Transport{}
	if cfg.Proxy != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err != nil {
			return nil, fmt.Errorf("blocklist: parse proxy url: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	return &Syncer{
		store: store,
		client: &http.Client{
			Transport: transport,
			// Bulk zips are tens of MB; generous but bounded.
			Timeout: 10 * time.Minute,
		},
		mirror:   mirror,
		interval: interval,
		now:      func() time.Time { return time.Now().UTC() },
	}, nil
}

// Start blocks until ctx is done, running one sync immediately and
// then one per interval. Failures degrade per ecosystem: they're logged
// and recorded in the sync state, and each failed ecosystem keeps
// blocking on its last good data.
func (s *Syncer) Start(ctx context.Context) {
	s.runOnce(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

// runOnce wraps SyncOnce with logging + state recording so scheduled
// and manually-triggered syncs behave identically.
func (s *Syncer) runOnce(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		zap.L().Info("blocklist: sync already running, skipping")
		return
	}
	s.runReserved(ctx)
}

// runReserved executes a sync after the caller has atomically reserved the
// single in-flight slot.
func (s *Syncer) runReserved(ctx context.Context) {
	defer s.running.Store(false)
	start := s.now()
	result, err := s.SyncOnce(ctx)
	took := s.now().Sub(start)
	if recErr := s.store.RecordSync(ctx, result, took); recErr != nil {
		zap.L().Warn("blocklist: record sync state", zap.Error(recErr))
	}
	if err != nil {
		zap.L().Warn("blocklist: sync failed for some ecosystems — they continue on their last good data",
			zap.Strings("failed", sortedKeys(result.Failed)), zap.Strings("refreshed", sortedKeys(result.Refreshed)),
			zap.Error(err), zap.Duration("took", took))
		return
	}
	zap.L().Info("blocklist: sync complete",
		zap.Int64("entries", result.Imported()), zap.Duration("took", took))
}

// TryStartSync reserves the single in-flight slot before submitting the work.
// A rejected submission releases the reservation synchronously, so shutdown
// cannot leave the status permanently stuck at running.
func (s *Syncer) TryStartSync(tasks asyncruntime.Submitter) error {
	if !s.running.CompareAndSwap(false, true) {
		return ErrSyncInProgress
	}
	if tasks == nil {
		s.running.Store(false)
		return ErrSyncUnavailable
	}
	if err := tasks.Submit(func(ctx context.Context) { s.runReserved(ctx) }); err != nil {
		s.running.Store(false)
		return errors.Join(ErrSyncUnavailable, err)
	}
	return nil
}

// Running reports whether a sync is currently in flight (admin status).
func (s *Syncer) Running() bool { return s.running.Load() }

// Interval exposes the configured refresh period (admin status).
func (s *Syncer) Interval() time.Duration { return s.interval }

// SyncResult reports one sync run ecosystem by ecosystem. An ecosystem
// appears in exactly one of the two maps.
type SyncResult struct {
	// Refreshed holds the row count committed for each ecosystem whose
	// dataset was replaced.
	Refreshed map[string]int64
	// Failed holds why each remaining ecosystem kept its last good data.
	Failed map[string]error
}

// Imported is the number of rows committed by the refreshed ecosystems.
func (r SyncResult) Imported() int64 {
	var total int64
	for _, n := range r.Refreshed {
		total += n
	}
	return total
}

// Err joins the per-ecosystem failures in stable ecosystem order, or
// returns nil when every covered ecosystem refreshed.
func (r SyncResult) Err() error {
	if len(r.Failed) == 0 {
		return nil
	}
	errs := make([]error, 0, len(r.Failed))
	for _, eco := range sortedKeys(r.Failed) {
		errs = append(errs, fmt.Errorf("%s: %w", eco, r.Failed[eco]))
	}
	return errors.Join(errs...)
}

// SyncOnce refreshes every covered ecosystem independently. Each ecosystem
// downloads, parses and commits on its own: a failure (unreachable archive,
// a mirror that lacks one ecosystem, a store error, or the zero-wipe guard)
// leaves that ecosystem on its last good data while the others still
// refresh. The error is SyncResult.Err: nil only when every ecosystem
// refreshed.
func (s *Syncer) SyncOnce(ctx context.Context) (SyncResult, error) {
	ecosystems := SyncedEcosystems()
	result := SyncResult{
		Refreshed: make(map[string]int64, len(ecosystems)),
		Failed:    make(map[string]error),
	}
	// The dataset always carries some advisories the importer cannot use, so
	// a warning per advisory repeats on every sync and buries the rest of the
	// log. Report them once per sync; the individual advisories are logged at
	// debug level by fetchEcosystem.
	skipped := 0
	skippedByEcosystem := make(map[string]int)
	defer func() {
		if skipped > 0 {
			zap.L().Warn("blocklist: skipped malformed advisories; set server.log_level to debug to list them",
				zap.Int("count", skipped), zap.Any("by_ecosystem", skippedByEcosystem))
		}
	}()

	// The zero-wipe guard compares each archive with what the store held
	// before this run. Without that baseline no ecosystem can be guarded,
	// so none is replaced.
	existing, _, err := s.store.EntryCounts(ctx)
	if err != nil {
		for _, eco := range ecosystems {
			result.Failed[eco] = fmt.Errorf("entry counts: %w", err)
		}
		return result, result.Err()
	}

	for _, eco := range ecosystems {
		// Fetch and commit one ecosystem at a time, so only one parsed
		// archive is held in memory.
		rows, malformed, err := s.fetchEcosystem(ctx, eco)
		if malformed > 0 {
			skipped += malformed
			skippedByEcosystem[eco] = malformed
		}
		if err != nil {
			result.Failed[eco] = err
			continue
		}
		// Zero-wipe guard: a 200 response with a valid zip that contains
		// no MAL advisories (misrouted mirror, generic file server) must
		// not silently erase an ecosystem that had entries — that would
		// turn malware blocking off with a green sync (v0.8.0 review
		// finding). Ecosystems that were already empty may stay empty.
		if len(rows) == 0 && existing[eco] > 0 {
			result.Failed[eco] = fmt.Errorf("archive contained zero MAL advisories but the store holds %d — refusing to wipe (check mirror_url)", existing[eco])
			continue
		}
		if err := s.store.ReplaceEcosystem(ctx, eco, rows); err != nil {
			result.Failed[eco] = fmt.Errorf("store: %w", err)
			continue
		}
		result.Refreshed[eco] = int64(len(rows))
	}
	return result, result.Err()
}

// fetchEcosystem downloads {mirror}/{OSVName}/all.zip to a temp file
// and extracts the MAL-* entries. The zip member NAME carries the
// advisory id ("MAL-2026-1234.json"), so non-malicious advisories are
// skipped without parsing their JSON — the npm archive holds tens of
// thousands of GHSA entries we never open. The second result counts the
// MAL advisories that were skipped as malformed.
func (s *Syncer) fetchEcosystem(ctx context.Context, eco string) ([]db.MaliciousPackage, int, error) {
	osvName := ecosystem.OSVNameFor(eco)
	reqURL := fmt.Sprintf("%s/%s/all.zip", s.mirror, url.PathEscape(osvName))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "depsilo-blocklist-sync")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch %s: %w", reqURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("fetch %s: HTTP %d", reqURL, resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "depsilo-osv-*.zip")
	if err != nil {
		return nil, 0, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return nil, 0, fmt.Errorf("download %s: %w", reqURL, err)
	}

	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return nil, 0, fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	importedAt := s.now()
	var rows []db.MaliciousPackage
	malformed := 0
	for _, f := range zr.File {
		if !strings.HasPrefix(baseName(f.Name), "MAL-") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, malformed, fmt.Errorf("open %s: %w", f.Name, err)
		}
		entryRows, err := parseAdvisory(rc, eco, osvName, importedAt)
		rc.Close()
		if err != nil {
			// One malformed advisory shouldn't unprotect the rest of
			// the ecosystem — count it and keep going. SyncOnce reports
			// the total in one line.
			malformed++
			zap.L().Debug("blocklist: skipping malformed advisory",
				zap.String("ecosystem", eco), zap.String("file", f.Name), zap.Error(err))
			continue
		}
		rows = append(rows, entryRows...)
	}
	return rows, malformed, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func baseName(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// osvAdvisory is the narrow slice of the OSV schema the importer
// needs. https://ossf.github.io/osv-schema/
type osvAdvisory struct {
	ID        string     `json:"id"`
	Summary   string     `json:"summary"`
	Aliases   []string   `json:"aliases"`
	Modified  time.Time  `json:"modified"`
	Withdrawn *time.Time `json:"withdrawn"`
	Affected  []struct {
		Package struct {
			Ecosystem string `json:"ecosystem"`
			Name      string `json:"name"`
		} `json:"package"`
		Versions []string   `json:"versions"`
		Ranges   []osvRange `json:"ranges"`
	} `json:"affected"`
}

type osvRange struct {
	Type   string `json:"type"`
	Events []struct {
		Introduced   string `json:"introduced"`
		Fixed        string `json:"fixed"`
		LastAffected string `json:"last_affected"`
	} `json:"events"`
}

// parseAdvisory converts one MAL advisory into rows for the given
// ecosystem. One advisory can affect several packages (typosquat
// campaigns); each affected package becomes its own row.
func parseAdvisory(r io.Reader, eco, osvName string, importedAt time.Time) ([]db.MaliciousPackage, error) {
	var adv osvAdvisory
	if err := json.NewDecoder(r).Decode(&adv); err != nil {
		return nil, err
	}

	// A withdrawn advisory is the dataset itself saying "false
	// positive, disregard" — importing it as an active block would
	// hard-451 a vindicated package (v0.8.0 review finding: a
	// withdrawn fastapi advisory exists in the live dataset).
	if adv.Withdrawn != nil && !adv.Withdrawn.IsZero() {
		return nil, nil
	}

	summary := adv.Summary
	if len(summary) > 500 {
		summary = summary[:500]
	}
	aliases := strings.Join(adv.Aliases, ",")
	if len(aliases) > 500 {
		aliases = aliases[:500]
	}

	var rows []db.MaliciousPackage
	for _, aff := range adv.Affected {
		// The per-ecosystem archive should only contain its own
		// entries, but one advisory can span ecosystems — keep only
		// the sections for the archive we're importing. OSV ecosystem
		// values can carry suffixes ("Maven:https://…"), so match on
		// the base token.
		affEco := aff.Package.Ecosystem
		if i := strings.IndexByte(affEco, ':'); i >= 0 {
			affEco = affEco[:i]
		}
		if !strings.EqualFold(affEco, osvName) {
			continue
		}

		var versions string // empty = every version is malicious
		switch {
		case len(aff.Versions) > 0 && !coversAllVersions(aff.Ranges):
			normalizedVersions := make([]string, 0, len(aff.Versions))
			for _, version := range aff.Versions {
				normalized, err := normalizeVersionStrict(eco, version)
				if err != nil {
					return nil, fmt.Errorf("advisory %s package %q has invalid %s version %q: %w", adv.ID, aff.Package.Name, eco, version, err)
				}
				normalizedVersions = append(normalizedVersions, normalized)
			}
			b, err := json.Marshal(normalizedVersions)
			if err != nil {
				return nil, err
			}
			versions = string(b)
		case coversAllVersions(aff.Ranges):
			versions = ""
		default:
			// Bounded range (a fixed / last_affected event) with no
			// explicit version enumeration. Without semver-range
			// evaluation (out of scope) we cannot tell which versions
			// are affected — and treating the row as all-versions
			// hard-blocks packages that were compromised and FIXED
			// (v0.8.0 review found real casualties in the live npm
			// dataset: fsevents, @solana/web3.js). Skipping is the
			// safe direction; the compromised window is years past
			// the min-release-age horizon anyway.
			zap.L().Debug("blocklist: skipping bounded-range advisory without version list",
				zap.String("id", adv.ID), zap.String("package", aff.Package.Name))
			continue
		}

		packageName, err := normalizeNameStrict(eco, aff.Package.Name)
		if err != nil {
			return nil, fmt.Errorf("advisory %s has invalid %s package identity %q: %w", adv.ID, eco, aff.Package.Name, err)
		}
		rows = append(rows, db.MaliciousPackage{
			SourceID:   adv.ID,
			Ecosystem:  eco,
			Package:    packageName,
			Versions:   versions,
			Aliases:    aliases,
			Summary:    summary,
			Modified:   adv.Modified,
			ImportedAt: importedAt,
		})
	}
	return rows, nil
}

// coversAllVersions reports whether any range says "introduced at 0,
// never bounded" — the dataset's idiom for "the package itself is the
// attack, every version counts." A fixed OR last_affected event bounds
// the range (the advisory itself says later versions are clean), so
// bounded ranges never count as all-versions.
func coversAllVersions(ranges []osvRange) bool {
	for _, r := range ranges {
		introducedZero := false
		bounded := false
		for _, e := range r.Events {
			if e.Introduced == "0" {
				introducedZero = true
			}
			if e.Fixed != "" || e.LastAffected != "" {
				bounded = true
			}
		}
		if introducedZero && !bounded {
			return true
		}
	}
	return false
}
