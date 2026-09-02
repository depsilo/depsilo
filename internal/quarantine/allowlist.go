package quarantine

import (
	"fmt"
	"path"
	"strings"

	"depsilo/internal/packagepolicy"
)

// AllowList matches package coordinates against the user-provided bypass
// rules. It supports three syntaxes:
//
//	"ecosystem:glob"           — glob match on package name
//	                              e.g. "npm:@scope/internal-*"
//	"ecosystem:name==version"  — exact pin on a single version
//	                              e.g. "pypi:requests==2.32.3"
//	"ecosystem:name<OP>ver"    — version-range comparator on the
//	                              package name; OP ∈ >= > <= < ==
//	                              e.g. "npm:react>=18.0.0"
//
// The "ecosystem:" prefix is required so the same rule file can carry
// entries for every adapter without collisions. Match is called on the hot
// fetch path; ecosystem validation, package-name normalization, and version
// selector compilation all happen at startup.
//
// A nil AllowList matches nothing — equivalent to "no bypass rules
// configured." Empty input strings are tolerated as a convenience for
// hand-edited TOML files.
type AllowList struct {
	entries []allowEntry
}

type allowEntry struct {
	ecosystem      string
	dialect        packagepolicy.PackagePolicyDialect
	packageName    string
	packageGlob    bool
	versionMatcher packagepolicy.VersionMatcher
}

type quarantineVersionCapability uint8

const (
	quarantineVersionsPackageOnly quarantineVersionCapability = iota
	quarantineVersionsExactOnly
	quarantineVersionsRanges
)

// ParseAllowList parses a list of "ecosystem:rule" strings into an AllowList.
// Returns an error on the first malformed entry so the operator sees the typo
// at startup rather than discovering at request time that a rule never fires.
func ParseAllowList(rules []string) (*AllowList, error) {
	allowList := &AllowList{}
	for index, raw := range rules {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		entry, err := parseAllowEntry(raw)
		if err != nil {
			return nil, fmt.Errorf("allow rule #%d %q: %w", index+1, raw, err)
		}
		allowList.entries = append(allowList.entries, entry)
	}
	return allowList, nil
}

func parseAllowEntry(raw string) (allowEntry, error) {
	colon := strings.IndexByte(raw, ':')
	if colon <= 0 || colon == len(raw)-1 {
		return allowEntry{}, fmt.Errorf("missing 'ecosystem:' prefix")
	}
	ecosystem := strings.ToLower(strings.TrimSpace(raw[:colon]))
	rule := strings.TrimSpace(raw[colon+1:])
	if ecosystem == "" || rule == "" {
		return allowEntry{}, fmt.Errorf("empty ecosystem or rule")
	}
	dialect, err := packagepolicy.DialectFor(ecosystem)
	if err != nil {
		return allowEntry{}, err
	}

	// Version selectors take precedence over glob detection. Two-character
	// operators must be checked first so ">=" is never parsed as ">".
	for _, operator := range []string{">=", "<=", "==", ">", "<"} {
		if index := strings.Index(rule, operator); index >= 0 {
			name := strings.TrimSpace(rule[:index])
			version := strings.TrimSpace(rule[index+len(operator):])
			if name == "" || version == "" {
				return allowEntry{}, fmt.Errorf("comparator %q with empty operand", operator)
			}
			if operator == "==" && version == "*" {
				return allowEntry{}, fmt.Errorf("exact version selector requires a concrete version")
			}
			capability := quarantineAllowVersionCapability(ecosystem, dialect)
			if capability == quarantineVersionsPackageOnly {
				return allowEntry{}, packagepolicy.ErrVersionSelectorsUnsupported
			}
			if operator != "==" && capability != quarantineVersionsRanges {
				return allowEntry{}, packagepolicy.ErrVersionSelectorsUnsupported
			}
			normalizedName, err := dialect.NormalizePackageName(name)
			if err != nil {
				return allowEntry{}, fmt.Errorf("normalize package name: %w", err)
			}

			selector := operator + version
			if operator == "==" {
				selector = version
			}
			matcher, err := packagepolicy.CompileVersionMatcher(ecosystem, selector)
			if err != nil {
				return allowEntry{}, fmt.Errorf("compile version selector %q: %w", operator+version, err)
			}
			return allowEntry{
				ecosystem:      ecosystem,
				dialect:        dialect,
				packageName:    normalizedName,
				versionMatcher: matcher,
			}, nil
		}
	}

	// A package-wide rule uses deterministic slash-separated glob syntax; both
	// the configured pattern and request package use the dialect's package
	// identity normalization.
	normalizedPattern, err := packagepolicy.NormalizePackageGlob(dialect, rule)
	if err != nil {
		return allowEntry{}, fmt.Errorf("normalize package glob: %w", err)
	}
	return allowEntry{
		ecosystem:   ecosystem,
		dialect:     dialect,
		packageName: normalizedPattern,
		packageGlob: true,
	}, nil
}

// quarantineAllowVersionCapability is intentionally independent from Package
// Rules request-path capability. In particular, npm's adapter authenticates a
// packument-derived version before calling QuarantineGate, while the earlier
// Package Rules middleware cannot verify that private token and remains
// package-only. Conversely, APT, Composer, RubyGems, and Helm must not acquire
// version bypasses merely because a dialect comparator exists.
func quarantineAllowVersionCapability(
	ecosystem string,
	dialect packagepolicy.PackagePolicyDialect,
) quarantineVersionCapability {
	switch ecosystem {
	case "apt", "composer", "rubygems", "helm":
		return quarantineVersionsPackageOnly
	case "npm":
		return quarantineVersionsRanges
	default:
		if dialect.SupportsRanges() {
			return quarantineVersionsRanges
		}
		return quarantineVersionsExactOnly
	}
}

// Match reports whether (ecosystem, packageName, version) is allowed by any
// rule. version may be empty when the caller has not resolved to a specific
// version yet; version rules cannot fire without it, while package globs only
// need the name. Invalid request identities fail closed for the bypass.
func (a *AllowList) Match(ecosystem, packageName, version string) bool {
	if a == nil || len(a.entries) == 0 {
		return false
	}
	ecosystem = strings.ToLower(strings.TrimSpace(ecosystem))
	var normalizedPackageName string
	packageNameNormalized := false

	for index := range a.entries {
		entry := &a.entries[index]
		if entry.ecosystem != ecosystem {
			continue
		}
		if !packageNameNormalized {
			var err error
			normalizedPackageName, err = entry.dialect.NormalizePackageName(packageName)
			if err != nil {
				return false
			}
			packageNameNormalized = true
		}
		if entry.packageGlob {
			if matched, _ := path.Match(entry.packageName, normalizedPackageName); matched {
				return true
			}
			continue
		}
		if entry.packageName != normalizedPackageName || version == "" {
			continue
		}
		matched, err := entry.versionMatcher.Match(version)
		if err == nil && matched {
			return true
		}
	}
	return false
}
