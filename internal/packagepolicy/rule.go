package packagepolicy

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const (
	// DialectRevision1 is the immutable schema-v3 package-rule contract.
	DialectRevision1 uint = 1

	// CurrentDialectRevision identifies the normalization semantics persisted in
	// package_rules. A future semantic change must add a database migration.
	CurrentDialectRevision uint = DialectRevision1

	maxEcosystemLength = 16
	maxPackageLength   = 256
	maxVersionLength   = 128
)

var (
	// ErrInvalidRule means a rule cannot be interpreted without guessing.
	ErrInvalidRule = errors.New("invalid package rule")
	// ErrRangesUnsupported means the selected dialect deliberately supports
	// package-wide and exact-version rules only.
	ErrRangesUnsupported = errors.New("version ranges are not supported for this ecosystem")
	// ErrVersionSelectorsUnsupported means real proxy requests do not expose a
	// complete version for this ecosystem, so only package-wide rules are safe.
	ErrVersionSelectorsUnsupported = errors.New("version selectors cannot be enforced for this ecosystem")
)

// RawRule is the operator-facing identity portion of a package rule.
type RawRule struct {
	Ecosystem   string
	PackageName string
	Version     string
}

// PreparedRule retains display values alongside the dialect-normalized values
// used by the request evaluator.
type PreparedRule struct {
	Ecosystem             string
	PackageName           string
	Version               string
	NormalizedPackageName string
	NormalizedVersion     string
	DialectRevision       uint
}

// PrepareRule validates and normalizes a complete rule identity atomically.
func PrepareRule(raw RawRule) (PreparedRule, error) {
	switch CurrentDialectRevision {
	case DialectRevision1:
		return PrepareRuleRevision1(raw)
	default:
		return PreparedRule{}, fmt.Errorf("%w: unsupported current dialect revision %d", ErrInvalidRule, CurrentDialectRevision)
	}
}

// PrepareRuleRevision1 is the frozen migration contract for dialect revision
// 1. Do not change its semantics after schema version 3 ships; add a new
// revision and numbered migration instead.
func PrepareRuleRevision1(raw RawRule) (PreparedRule, error) {
	prepared := PreparedRule{
		Ecosystem:       strings.ToLower(strings.TrimSpace(raw.Ecosystem)),
		PackageName:     strings.TrimSpace(raw.PackageName),
		Version:         strings.TrimSpace(raw.Version),
		DialectRevision: DialectRevision1,
	}
	if prepared.Version == "" {
		prepared.Version = "*"
	}
	if err := validateRawRuleBounds(prepared); err != nil {
		return PreparedRule{}, err
	}

	if prepared.Ecosystem == "*" {
		if prepared.PackageName != "*" || prepared.Version != "*" {
			return PreparedRule{}, fmt.Errorf(
				"%w: wildcard ecosystem is valid only for the */*/* rule across supported Package Rule ecosystems",
				ErrInvalidRule,
			)
		}
		prepared.NormalizedPackageName = "*"
		prepared.NormalizedVersion = "*"
		return prepared, nil
	}

	if !ruleEnforcementRevision1(prepared.Ecosystem) {
		return PreparedRule{}, fmt.Errorf("%w: unsupported package rule ecosystem %q", ErrInvalidRule, prepared.Ecosystem)
	}
	dialect, err := dialectForRevision1(prepared.Ecosystem)
	if err != nil {
		return PreparedRule{}, fmt.Errorf("%w: %v", ErrInvalidRule, err)
	}
	prepared.NormalizedPackageName, err = normalizePackageSelector(dialect, prepared.PackageName)
	if err != nil {
		return PreparedRule{}, fmt.Errorf("%w: normalize package selector: %v", ErrInvalidRule, err)
	}
	versionCapability, err := versionCapabilityForRevision1(prepared.Ecosystem)
	if err != nil {
		return PreparedRule{}, fmt.Errorf("%w: determine version capability: %v", ErrInvalidRule, err)
	}
	if versionCapability == PolicyVersionsPackageOnly && prepared.Version != "*" {
		return PreparedRule{}, fmt.Errorf("%w: %w", ErrInvalidRule, ErrVersionSelectorsUnsupported)
	}
	prepared.NormalizedVersion, err = normalizeVersionSelector(dialect, prepared.Version)
	if err != nil {
		return PreparedRule{}, err
	}
	return prepared, nil
}

func ruleEnforcementRevision1(ecosystemName string) bool {
	switch ecosystemName {
	case "pypi", "apt", "npm", "go", "cargo", "maven", "composer", "nuget", "conda", "cran", "alpine":
		return true
	default:
		return false
	}
}

func validateRawRuleBounds(rule PreparedRule) error {
	if rule.Ecosystem == "" || len(rule.Ecosystem) > maxEcosystemLength || containsControl(rule.Ecosystem) {
		return fmt.Errorf("%w: ecosystem must be non-empty, at most %d characters, and contain no control characters", ErrInvalidRule, maxEcosystemLength)
	}
	if rule.PackageName == "" || len(rule.PackageName) > maxPackageLength || containsControl(rule.PackageName) {
		return fmt.Errorf("%w: package name must be non-empty, at most %d characters, and contain no control characters", ErrInvalidRule, maxPackageLength)
	}
	if rule.Version == "" || len(rule.Version) > maxVersionLength || containsControl(rule.Version) {
		return fmt.Errorf("%w: version must be non-empty, at most %d characters, and contain no control characters", ErrInvalidRule, maxVersionLength)
	}
	return nil
}

func normalizePackageSelector(dialect PackagePolicyDialect, selector string) (string, error) {
	if selector == "*" {
		return selector, nil
	}
	if strings.Count(selector, "*") > 1 || strings.Contains(selector, "*") && !strings.HasSuffix(selector, "*") {
		return "", fmt.Errorf("package selector supports only an exact name, *, or one trailing prefix wildcard")
	}
	prefix := strings.HasSuffix(selector, "*")
	name := strings.TrimSuffix(selector, "*")
	if name == "" {
		return "", fmt.Errorf("package prefix is empty")
	}
	var normalized string
	var err error
	if prefix {
		normalized, err = normalizeDialectPackagePrefix(dialect, name)
	} else {
		normalized, err = dialect.NormalizePackageName(name)
	}
	if err != nil {
		return "", err
	}
	if len(normalized) > maxPackageLength {
		return "", fmt.Errorf("normalized package selector exceeds %d characters", maxPackageLength)
	}
	if prefix {
		if len(normalized) == maxPackageLength {
			return "", fmt.Errorf("normalized package prefix exceeds %d characters with wildcard", maxPackageLength)
		}
		normalized += "*"
	}
	return normalized, nil
}

func normalizeVersionSelector(dialect PackagePolicyDialect, selector string) (string, error) {
	if selector == "*" {
		return selector, nil
	}
	if strings.Contains(selector, "*") {
		return "", fmt.Errorf("%w: * is valid only as the complete version wildcard", ErrInvalidRule)
	}
	operator, target := splitComparison(selector)
	if operator == "" {
		if strings.HasPrefix(selector, "=") || strings.HasPrefix(selector, "!") || strings.HasPrefix(selector, "~") ||
			strings.ContainsAny(selector, "<>") || strings.ContainsFunc(selector, unicode.IsSpace) {
			return "", fmt.Errorf("%w: version must be an exact version or one of <, <=, >, >=", ErrInvalidRule)
		}
		canonical, err := canonicalVersion(dialect, selector)
		if err != nil {
			return "", fmt.Errorf("%w: invalid exact version %q: %v", ErrInvalidRule, selector, err)
		}
		return canonical, nil
	}
	if !dialect.SupportsRanges() {
		return "", fmt.Errorf("%w: %w", ErrInvalidRule, ErrRangesUnsupported)
	}
	if target == "" {
		return "", fmt.Errorf("%w: version comparison requires a target", ErrInvalidRule)
	}
	if strings.ContainsAny(target, "<>") || strings.ContainsFunc(target, unicode.IsSpace) {
		return "", fmt.Errorf("%w: version comparison must contain exactly one operator and one version", ErrInvalidRule)
	}
	if validator, ok := dialect.(rangeTargetValidator); ok {
		if err := validator.validateRangeTarget(target); err != nil {
			return "", fmt.Errorf("%w: invalid version comparison target %q: %v", ErrInvalidRule, target, err)
		}
	}
	canonical, err := canonicalVersion(dialect, target)
	if err != nil {
		return "", fmt.Errorf("%w: invalid version comparison target %q: %v", ErrInvalidRule, target, err)
	}
	return operator + " " + canonical, nil
}

func splitComparison(value string) (string, string) {
	for _, operator := range []string{"<=", ">=", "<", ">"} {
		if strings.HasPrefix(value, operator) {
			return operator, strings.TrimSpace(value[len(operator):])
		}
	}
	return "", value
}

type versionCanonicalizer interface {
	canonicalVersion(string) (string, error)
}

type rangeTargetValidator interface {
	validateRangeTarget(string) error
}

func canonicalVersion(dialect PackagePolicyDialect, value string) (string, error) {
	if canonicalizer, ok := dialect.(versionCanonicalizer); ok {
		return canonicalizer.canonicalVersion(value)
	}
	if err := dialect.ValidateVersion(value); err != nil {
		return "", err
	}
	return value, nil
}

// NormalizeVersion validates a concrete ecosystem version and returns the
// dialect's stable identity spelling. Equality decisions must still use
// CompareVersions: ecosystems such as SemVer deliberately ignore build
// metadata for precedence even though the metadata remains in this spelling.
func NormalizeVersion(ecosystem, value string) (string, error) {
	dialect, err := DialectFor(strings.ToLower(strings.TrimSpace(ecosystem)))
	if err != nil {
		return "", err
	}
	return canonicalVersion(dialect, value)
}

func containsControl(value string) bool {
	return strings.ContainsFunc(value, unicode.IsControl)
}
