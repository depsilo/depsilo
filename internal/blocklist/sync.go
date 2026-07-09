package blocklist

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/db"
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
// then one per interval. Failures degrade: they're logged and recorded
// in the sync-state row, and blocking continues on the last good data.
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
	defer s.running.Store(false)
	start := s.now()
	count, err := s.SyncOnce(ctx)
	took := s.now().Sub(start)
	if recErr := s.store.RecordSync(ctx, err, count, took); recErr != nil {
		zap.L().Warn("blocklist: record sync state", zap.Error(recErr))
	}
	if err != nil {
		zap.L().Warn("blocklist: sync failed — continuing on last good dataset",
			zap.Error(err), zap.Duration("took", took))
		return
	}
	zap.L().Info("blocklist: sync complete",
		zap.Int64("entries", count), zap.Duration("took", took))
}

// TriggerSync starts one sync on a fresh goroutine and reports
// whether it actually started (false = one is already running). The
// admin "sync now" endpoint surfaces false as 409.
func (s *Syncer) TriggerSync(ctx context.Context) bool {
	if s.running.Load() {
		return false
	}
	go s.runOnce(ctx)
	return true
}

// Running reports whether a sync is currently in flight (admin status).
func (s *Syncer) Running() bool { return s.running.Load() }

// Interval exposes the configured refresh period (admin status).
func (s *Syncer) Interval() time.Duration { return s.interval }

// SyncOnce refreshes every covered ecosystem and returns the total
// imported row count. Two-phase on purpose: EVERY archive downloads
// and parses before ANY database write, so a mid-run network failure
// can't leave half the ecosystems on new data and half on old — the
// "stays on the last good dataset" guarantee holds atomically per run
// (v0.8.0 review finding).
func (s *Syncer) SyncOnce(ctx context.Context) (int64, error) {
	// Phase 1: fetch + parse everything.
	fetched := make(map[string][]db.MaliciousPackage, len(SyncedEcosystems()))
	for _, eco := range SyncedEcosystems() {
		rows, err := s.fetchEcosystem(ctx, eco)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", eco, err)
		}
		fetched[eco] = rows
	}

	// Zero-wipe guard: a 200 response with a valid zip that contains
	// no MAL advisories (misrouted mirror, generic file server) must
	// not silently erase an ecosystem that had entries — that would
	// turn malware blocking off with a green sync (v0.8.0 review
	// finding). Ecosystems that were already empty may stay empty.
	existing, _, err := s.store.EntryCounts(ctx)
	if err != nil {
		return 0, fmt.Errorf("entry counts: %w", err)
	}
	for _, eco := range SyncedEcosystems() {
		if len(fetched[eco]) == 0 && existing[eco] > 0 {
			return 0, fmt.Errorf("%s: archive contained zero MAL advisories but the store holds %d — refusing to wipe (check mirror_url)", eco, existing[eco])
		}
	}

	// Phase 2: commit.
	var total int64
	for _, eco := range SyncedEcosystems() {
		if err := s.store.ReplaceEcosystem(ctx, eco, fetched[eco]); err != nil {
			return 0, fmt.Errorf("%s: store: %w", eco, err)
		}
		total += int64(len(fetched[eco]))
	}
	return total, nil
}

// fetchEcosystem downloads {mirror}/{OSVName}/all.zip to a temp file
// and extracts the MAL-* entries. The zip member NAME carries the
// advisory id ("MAL-2026-1234.json"), so non-malicious advisories are
// skipped without parsing their JSON — the npm archive holds tens of
// thousands of GHSA entries we never open.
func (s *Syncer) fetchEcosystem(ctx context.Context, eco string) ([]db.MaliciousPackage, error) {
	osvName := osvEcosystem[eco]
	reqURL := fmt.Sprintf("%s/%s/all.zip", s.mirror, url.PathEscape(osvName))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "depsilo-blocklist-sync")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", reqURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", reqURL, resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "depsilo-osv-*.zip")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return nil, fmt.Errorf("download %s: %w", reqURL, err)
	}

	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	importedAt := s.now()
	var rows []db.MaliciousPackage
	for _, f := range zr.File {
		if !strings.HasPrefix(baseName(f.Name), "MAL-") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", f.Name, err)
		}
		entryRows, err := parseAdvisory(rc, eco, osvName, importedAt)
		rc.Close()
		if err != nil {
			// One malformed advisory shouldn't unprotect the rest of
			// the ecosystem — log and keep going.
			zap.L().Warn("blocklist: skipping malformed advisory",
				zap.String("file", f.Name), zap.Error(err))
			continue
		}
		rows = append(rows, entryRows...)
	}
	return rows, nil
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
		if affEco != osvName {
			continue
		}

		var versions string // empty = every version is malicious
		switch {
		case len(aff.Versions) > 0 && !coversAllVersions(aff.Ranges):
			b, err := json.Marshal(aff.Versions)
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

		rows = append(rows, db.MaliciousPackage{
			SourceID:   adv.ID,
			Ecosystem:  eco,
			Package:    NormalizeName(eco, aff.Package.Name),
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
