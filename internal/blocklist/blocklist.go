// Package blocklist implements the known-malicious package blocklist
// (DIRECTION Task 2): a locally-synced copy of the OSV
// malicious-packages dataset (MAL-* advisories) that the quarantine
// checker consults as its very first step. Matching a row is refused
// with 451 MALICIOUS_BLOCKED in block mode; warn mode serves the
// request and records malware_warned instead. The only exemption from
// block mode is an explicit, audited, 24h-expiring operator override.
//
// Deliberately separate from internal/security (CVE scanning): a CVE
// means "vulnerable, warn and serve"; a MAL advisory means "this
// version was published to attack you." The two must never share a
// code path that could blur that line.
package blocklist

import (
	"strings"
	"time"

	ecosystemcatalog "depsilo/internal/ecosystem"
	"depsilo/internal/packagepolicy"
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
	// Enabled defaults to true. This harder known-malware gate is independent
	// of the default-off minimum-release-age policy. Sync failures degrade
	// (serve on last good data; no data at all means no blocking) rather than
	// break the proxy.
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

	// Mode controls whether a known-malicious match blocks (default)
	// or merely records a warning and serves the request.
	Mode string `mapstructure:"mode"`
}

// IsEnabled applies the default-true semantics.
func (c Config) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

// DefaultMirrorURL is the official OSV bulk-data bucket.
const DefaultMirrorURL = "https://osv-vulnerabilities.storage.googleapis.com"

// DefaultSyncInterval between refreshes.
const DefaultSyncInterval = 6 * time.Hour

// SyncedEcosystems returns the depsilo ecosystem names the dataset
// covers, in stable order (for status displays and tests).
func SyncedEcosystems() []string {
	definitions := ecosystemcatalog.MaliciousDatasetDefinitions()
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}

// NormalizeName maps a valid package name through the same ecosystem dialect
// used by Package Rules. Invalid names return ""; production paths call the
// strict helper below so malformed advisory/request identities are observable.
func NormalizeName(ecosystem, name string) string {
	normalized, _ := normalizeNameStrict(ecosystem, name)
	return normalized
}

func normalizeNameStrict(ecosystem, name string) (string, error) {
	dialect, err := packagepolicy.DialectFor(CanonicalEcosystem(ecosystem))
	if err != nil {
		return "", err
	}
	return dialect.NormalizePackageName(name)
}

// NormalizeVersion maps a valid request-side version to the dataset's
// dialect spelling. Invalid versions return ""; production paths use the
// strict helper so they never guess. Go advisories omit the GOPROXY "v"
// prefix, so it is removed before validation.
func NormalizeVersion(ecosystem, version string) string {
	normalized, _ := normalizeVersionStrict(ecosystem, version)
	return normalized
}

func normalizeVersionStrict(ecosystem, version string) (string, error) {
	if version == "" {
		return "", nil
	}
	ecosystem = CanonicalEcosystem(ecosystem)
	if ecosystem == "go" {
		version = strings.TrimPrefix(version, "v")
	}
	return packagepolicy.NormalizeVersion(ecosystem, version)
}

// CanonicalEcosystem maps adapter-reported ecosystem names onto the
// blocklist's canonical set. Extra PyPI-compatible indexes report
// "extra:<name>" (their own quarantine identity) but speak the PyPI
// protocol — malware in the dataset's PyPI section applies to them
// too (v0.8.0 review finding: extra indexes bypassed the gate).
func CanonicalEcosystem(ecosystem string) string {
	canonical := strings.ToLower(ecosystem)
	if strings.HasPrefix(canonical, "extra:") {
		return "pypi"
	}
	return canonical
}

// IsSyncedEcosystem reports whether the dataset covers the ecosystem —
// used by the admin API to reject overrides that could never match.
func IsSyncedEcosystem(ecosystem string) bool {
	definition, ok := ecosystemcatalog.Lookup(CanonicalEcosystem(ecosystem))
	return ok && definition.MaliciousDataset
}
