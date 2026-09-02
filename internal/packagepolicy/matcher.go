package packagepolicy

import (
	"fmt"
)

// VersionMatcher is a validated, ecosystem-specific version selector. Match
// parses only the request version; the rule expression is compiled once.
type VersionMatcher interface {
	Match(version string) (bool, error)
}

// CompileVersionMatcher validates and compiles a version selector for one
// concrete ecosystem.
func CompileVersionMatcher(ecosystem, selector string) (VersionMatcher, error) {
	dialect, err := DialectFor(ecosystem)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeVersionSelector(dialect, selector)
	if err != nil {
		return nil, err
	}
	if normalized == "*" {
		return anyVersionMatcher{}, nil
	}

	operator, target := splitComparison(normalized)
	if operator == "" {
		return exactVersionMatcher{dialect: dialect, target: normalized}, nil
	}
	if _, ok := dialect.(pep440Dialect); ok {
		parsedTarget, err := parsePEP440Version(target)
		if err != nil {
			return nil, fmt.Errorf("compile PyPI version comparison: %w", err)
		}
		return pep440ComparisonMatcher{operator: operator, target: parsedTarget}, nil
	}
	if semverDialect, ok := dialect.(semverDialect); ok {
		parsedTarget, err := semverDialect.parse(target)
		if err != nil {
			return nil, fmt.Errorf("compile %s version comparison: %w", ecosystem, err)
		}
		return semverComparisonMatcher{dialect: semverDialect, operator: operator, target: parsedTarget}, nil
	}
	return comparisonVersionMatcher{dialect: dialect, operator: operator, target: target}, nil
}

type anyVersionMatcher struct{}

func (anyVersionMatcher) Match(string) (bool, error) { return true, nil }

type exactVersionMatcher struct {
	dialect PackagePolicyDialect
	target  string
}

func (m exactVersionMatcher) Match(version string) (bool, error) {
	comparison, err := m.dialect.CompareVersions(version, m.target)
	return comparison == 0, err
}

type semverComparisonMatcher struct {
	dialect  semverDialect
	operator string
	target   strictSemVer
}

func (m semverComparisonMatcher) Match(version string) (bool, error) {
	parsed, err := m.dialect.parse(version)
	if err != nil {
		return false, err
	}
	// npm/node-semver and Cargo both exclude an unmentioned prerelease from
	// ordered requirements. A prerelease candidate is eligible only when the
	// comparator target is also a prerelease of the same core version.
	if parsed.hasPrerelease() && (!m.target.hasPrerelease() || parsed.compareCore(m.target) != 0) {
		return false, nil
	}
	comparison := parsed.Compare(m.target)
	switch m.operator {
	case "<":
		return comparison < 0, nil
	case "<=":
		return comparison <= 0, nil
	case ">":
		return comparison > 0, nil
	case ">=":
		return comparison >= 0, nil
	default:
		return false, fmt.Errorf("unsupported SemVer comparison operator %q", m.operator)
	}
}

type comparisonVersionMatcher struct {
	dialect  PackagePolicyDialect
	operator string
	target   string
}

type pep440ComparisonMatcher struct {
	operator string
	target   pep440Version
}

func (matcher pep440ComparisonMatcher) Match(value string) (bool, error) {
	version, err := parsePEP440Version(value)
	if err != nil {
		return false, err
	}
	comparisonVersion := version
	if matcher.operator == "<=" || matcher.operator == ">=" {
		// PEP 440 forbids local identifiers in ordered specifiers and removes
		// the candidate's local label for inclusive ordered comparisons.
		comparisonVersion = version.withoutLocal()
	}
	comparison := comparePEP440Versions(comparisonVersion, matcher.target)
	switch matcher.operator {
	case "<":
		if comparison >= 0 {
			return false, nil
		}
		// <V excludes a prerelease OF V unless V is itself a prerelease.
		// packaging 26.2 defines that relation by V's earliest prerelease,
		// rather than by sharing only epoch and release components.
		if !matcher.target.isPrerelease() && version.isPrerelease() {
			earliest := matcher.target.earliestPrerelease()
			if comparePEP440Versions(version, earliest) >= 0 {
				return false, nil
			}
		}
		return true, nil
	case "<=":
		return comparison <= 0, nil
	case ">":
		if comparison <= 0 {
			return false, nil
		}
		// >V excludes only a post-release OF exactly V when V is not itself
		// a post release. Prerelease components remain part of that identity.
		if !matcher.target.isPostrelease() && version.isPostrelease() &&
			comparePEP440Versions(version.postBase(), matcher.target) == 0 {
			return false, nil
		}
		// Likewise, a local version is excluded only when its complete public
		// version equals V; a shared release tuple is not sufficient.
		if len(version.local) != 0 &&
			comparePEP440Versions(version.withoutLocal(), matcher.target) == 0 {
			return false, nil
		}
		return true, nil
	case ">=":
		return comparison >= 0, nil
	default:
		return false, fmt.Errorf("unsupported PEP 440 comparison operator %q", matcher.operator)
	}
}

func (m comparisonVersionMatcher) Match(version string) (bool, error) {
	comparison, err := m.dialect.CompareVersions(version, m.target)
	if err != nil {
		return false, err
	}
	switch m.operator {
	case "<":
		return comparison < 0, nil
	case "<=":
		return comparison <= 0, nil
	case ">":
		return comparison > 0, nil
	case ">=":
		return comparison >= 0, nil
	default:
		return false, fmt.Errorf("unsupported comparison operator %q", m.operator)
	}
}
