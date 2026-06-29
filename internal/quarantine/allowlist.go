package quarantine

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AllowList matches package coordinates against the user-provided
// bypass rules. Three syntaxes per the locked-in decisions:
//
//   "ecosystem:glob"           — glob match on package name
//                                 e.g. "npm:@scope/internal-*"
//   "ecosystem:name==version"  — exact pin on a single version
//                                 e.g. "pip:requests==2.32.3"
//   "ecosystem:name<OP>ver"    — version-range comparator on the
//                                 package name; OP ∈ >= > <= < ==
//                                 e.g. "npm:react>=18.0.0"
//
// The "ecosystem:" prefix is required so the same rule file can carry
// entries for every adapter without collisions. Match() is called on
// the hot fetch path; pre-parsing into typed entries lets it run
// without string analysis on every request.
//
// A nil AllowList matches nothing — equivalent to "no bypass rules
// configured." Empty input strings are tolerated as a convenience for
// hand-edited TOML files.
type AllowList struct {
	entries []allowEntry
}

type matchType int

const (
	matchGlob matchType = iota
	matchExact
	matchRange
)

type rangeOp int

const (
	opEQ rangeOp = iota // ==
	opGE                // >=
	opGT                // >
	opLE                // <=
	opLT                // <
)

type allowEntry struct {
	ecosystem string
	kind      matchType
	// For matchGlob: pattern is the glob pattern (filepath.Match
	// semantics). For matchExact: pattern is the package name,
	// version is the exact pinned version. For matchRange:
	// pattern is the package name, version is the comparator's
	// right-hand side, op is the comparator.
	pattern string
	version string
	op      rangeOp
}

// ParseAllowList parses a list of "ecosystem:rule" strings into an
// AllowList. Returns an error on the first malformed entry so the
// operator sees the typo at startup rather than discovering at
// request time that a rule never fires.
func ParseAllowList(rules []string) (*AllowList, error) {
	a := &AllowList{}
	for i, raw := range rules {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		entry, err := parseAllowEntry(raw)
		if err != nil {
			return nil, fmt.Errorf("allow rule #%d %q: %w", i+1, raw, err)
		}
		a.entries = append(a.entries, entry)
	}
	return a, nil
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

	// Range / exact comparators take precedence over glob detection.
	// Order matters: check the two-character ops (>=, <=, ==) before
	// the single-character ones (>, <) so "react>=18" doesn't get
	// parsed as opGT with pattern "react=" and version "18".
	for _, op := range []struct {
		token string
		kind  rangeOp
	}{
		{">=", opGE},
		{"<=", opLE},
		{"==", opEQ},
		{">", opGT},
		{"<", opLT},
	} {
		if idx := strings.Index(rule, op.token); idx > 0 {
			name := strings.TrimSpace(rule[:idx])
			version := strings.TrimSpace(rule[idx+len(op.token):])
			if name == "" || version == "" {
				return allowEntry{}, fmt.Errorf("comparator %q with empty operand", op.token)
			}
			kind := matchRange
			if op.kind == opEQ {
				kind = matchExact
			}
			return allowEntry{
				ecosystem: ecosystem,
				kind:      kind,
				pattern:   name,
				version:   version,
				op:        op.kind,
			}, nil
		}
	}

	// No comparator → glob match on the whole rule.
	return allowEntry{
		ecosystem: ecosystem,
		kind:      matchGlob,
		pattern:   rule,
	}, nil
}

// Match reports whether (ecosystem, packageName, version) is allowed
// by any rule. version may be empty when the caller hasn't resolved
// to a specific version yet (e.g. checking an index endpoint) —
// range and exact rules can't fire without a version, glob rules
// only need the name.
func (a *AllowList) Match(ecosystem, packageName, version string) bool {
	if a == nil || len(a.entries) == 0 {
		return false
	}
	ecosystem = strings.ToLower(ecosystem)
	for i := range a.entries {
		e := &a.entries[i]
		if e.ecosystem != ecosystem {
			continue
		}
		switch e.kind {
		case matchGlob:
			if matched, _ := filepath.Match(e.pattern, packageName); matched {
				return true
			}
		case matchExact:
			if e.pattern == packageName && version != "" && e.version == version {
				return true
			}
		case matchRange:
			if e.pattern != packageName || version == "" {
				continue
			}
			cmp := compareVersions(version, e.version)
			switch e.op {
			case opGE:
				if cmp >= 0 {
					return true
				}
			case opGT:
				if cmp > 0 {
					return true
				}
			case opLE:
				if cmp <= 0 {
					return true
				}
			case opLT:
				if cmp < 0 {
					return true
				}
			}
		}
	}
	return false
}

// compareVersions returns -1 / 0 / +1 like strings.Compare, using
// best-effort semver-shaped comparison. Numeric chunks compare
// numerically; non-numeric chunks fall back to lexical. Pre-release
// suffixes ("-rc.1", "-beta") sort before the bare version per
// semver convention — sufficient for allowlist range comparisons,
// not a full PEP-440 / npm-semver implementation. Operators who
// need stricter semantics can pin exact versions.
func compareVersions(a, b string) int {
	// Split on the first '-' so the build/pre-release tail compares
	// after the core triple.
	aCore, aTail := splitPre(a)
	bCore, bTail := splitPre(b)
	if c := compareCore(aCore, bCore); c != 0 {
		return c
	}
	// Per semver, a version with a pre-release suffix is LOWER than
	// the same version without one. Empty tail wins.
	switch {
	case aTail == "" && bTail == "":
		return 0
	case aTail == "":
		return 1
	case bTail == "":
		return -1
	}
	return strings.Compare(aTail, bTail)
}

func splitPre(v string) (core, tail string) {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '-'); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

func compareCore(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	n := len(aParts)
	if len(bParts) > n {
		n = len(bParts)
	}
	for i := 0; i < n; i++ {
		// Missing segments compare as numeric zero so "1.0" == "1.0.0"
		// — semver convention and the natural reading for allow-list
		// pins where operators routinely write the shorter form.
		ap := "0"
		bp := "0"
		if i < len(aParts) && aParts[i] != "" {
			ap = aParts[i]
		}
		if i < len(bParts) && bParts[i] != "" {
			bp = bParts[i]
		}
		// Try numeric first.
		var an, bn int
		_, aErr := fmt.Sscanf(ap, "%d", &an)
		_, bErr := fmt.Sscanf(bp, "%d", &bn)
		if aErr == nil && bErr == nil {
			switch {
			case an < bn:
				return -1
			case an > bn:
				return 1
			}
			continue
		}
		// Mixed or non-numeric → lexical compare on the segment.
		if ap < bp {
			return -1
		}
		if ap > bp {
			return 1
		}
	}
	return 0
}
