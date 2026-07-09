// Package blocklist implements the known-malicious package blocklist
// (DIRECTION Task 2): a locally-synced copy of the OSV
// malicious-packages dataset (MAL-* advisories) that the quarantine
// checker consults as its very first step. Matching a row means the
// request is refused with 451 MALICIOUS_BLOCKED — never served — and
// the only exemption is an explicit, audited, 24h-expiring operator
// override.
//
// Deliberately separate from internal/security (CVE scanning): a CVE
// means "vulnerable, warn and serve"; a MAL advisory means "this
// version was published to attack you." The two must never share a
// code path that could blur that line.
package blocklist

import (
	"strings"
	"time"
)

// OverrideTTL is how long an operator override stays valid. Locked-in
// design: overrides are emergency valves for false positives, not
// standing configuration — they expire and must be consciously
// re-created.
const OverrideTTL = 24 * time.Hour

// QuarantineEvent actions written by this package. The request-time
// actions (malware_blocked / malware_bypassed) live in the quarantine
// package next to its other decision actions — each package owns the
// actions it records.
const (
	ActionOverrideCreated = "override_created"
	ActionOverrideRevoked = "override_revoked"
)

// Config is the operator-facing shape under [supply_chain.blocklist].
type Config struct {
	// Enabled defaults to true — an empty config is protected, matching
	// the quarantine posture. Sync failures degrade (serve on last good
	// data; no data at all means no blocking) rather than break the proxy.
	Enabled *bool `mapstructure:"enabled"`

	// SyncInterval between dataset refreshes. Default 6h.
	SyncInterval string `mapstructure:"sync_interval"`

	// MirrorURL is the OSV bulk-data base. Default is the official
	// bucket; operators behind restricted egress point this at an
	// internal mirror.
	MirrorURL string `mapstructure:"mirror_url"`

	// Proxy is an optional HTTP(S) proxy for the sync fetches —
	// storage.googleapis.com is unreachable from mainland China
	// without one.
	Proxy string `mapstructure:"proxy"`
}

// IsEnabled applies the default-true semantics.
func (c Config) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

// DefaultMirrorURL is the official OSV bulk-data bucket.
const DefaultMirrorURL = "https://osv-vulnerabilities.storage.googleapis.com"

// DefaultSyncInterval between refreshes.
const DefaultSyncInterval = 6 * time.Hour

// osvEcosystem maps depsilo adapter names to the OSV dataset's
// ecosystem directory names. Ecosystems missing here have no
// malicious-packages coverage and are skipped by the sync (docker,
// huggingface, helm, apt, conda, cran, alpine).
var osvEcosystem = map[string]string{
	"npm":      "npm",
	"pypi":     "PyPI",
	"cargo":    "crates.io",
	"rubygems": "RubyGems",
	"composer": "Packagist",
	"nuget":    "NuGet",
	"go":       "Go",
	"maven":    "Maven",
}

// SyncedEcosystems returns the depsilo ecosystem names the dataset
// covers, in stable order (for status displays and tests).
func SyncedEcosystems() []string {
	return []string{"npm", "pypi", "cargo", "rubygems", "composer", "nuget", "go", "maven"}
}

// NormalizeName maps a package name to the canonical form both the
// importer and the request-time lookup use, so equality is exact.
//   - npm: registry names are lowercase; scoped names keep the slash.
//   - pypi: PEP 503 — lowercase, runs of [-_.] collapse to "-".
//   - everything else: case-preserving identity (Maven coordinates and
//     Go module paths are case-significant; OSV stores them verbatim).
func NormalizeName(ecosystem, name string) string {
	switch ecosystem {
	case "npm", "nuget":
		// npm registry names are lowercase; NuGet IDs are
		// case-insensitive and the flat-container protocol mandates
		// lowercase in request URLs — store lowercase so request-time
		// equality works (v0.8.0 review finding: ~95% of NuGet MAL
		// entries carry mixed-case names).
		return strings.ToLower(name)
	case "pypi":
		lower := strings.ToLower(name)
		var b strings.Builder
		b.Grow(len(lower))
		prevSep := false
		for _, r := range lower {
			if r == '-' || r == '_' || r == '.' {
				if !prevSep {
					b.WriteByte('-')
				}
				prevSep = true
				continue
			}
			prevSep = false
			b.WriteRune(r)
		}
		return b.String()
	default:
		return name
	}
}

// NormalizeVersion maps a request-side version string to the dataset's
// convention. Go advisories carry semver WITHOUT the "v" prefix while
// GOPROXY paths carry it — strip so exact matching works.
func NormalizeVersion(ecosystem, version string) string {
	if ecosystem == "go" {
		return strings.TrimPrefix(version, "v")
	}
	return version
}

// CanonicalEcosystem maps adapter-reported ecosystem names onto the
// blocklist's canonical set. Extra PyPI-compatible indexes report
// "extra:<name>" (their own quarantine identity) but speak the PyPI
// protocol — malware in the dataset's PyPI section applies to them
// too (v0.8.0 review finding: extra indexes bypassed the gate).
func CanonicalEcosystem(ecosystem string) string {
	if strings.HasPrefix(ecosystem, "extra:") {
		return "pypi"
	}
	return ecosystem
}

// IsSyncedEcosystem reports whether the dataset covers the ecosystem —
// used by the admin API to reject overrides that could never match.
func IsSyncedEcosystem(ecosystem string) bool {
	_, ok := osvEcosystem[ecosystem]
	return ok
}
