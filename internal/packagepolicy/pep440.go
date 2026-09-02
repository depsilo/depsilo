package packagepolicy

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// This parser and comparator follow PyPA packaging 26.2's PEP 440 version
// model. Decimal components are retained as normalized strings so epochs,
// release segments, and suffix numbers have Python's unbounded-integer
// behavior instead of overflowing a Go machine integer.
//
// Reference implementation:
// https://github.com/pypa/packaging/blob/26.2/src/packaging/version.py

type pep440Dialect struct{}

type pep440Version struct {
	epoch     string
	release   []string
	pre       *pep440Prerelease
	post      *string
	dev       *string
	local     []pep440LocalSegment
	canonical string
}

type pep440Prerelease struct {
	kind   string
	number string
}

type pep440LocalSegment struct {
	value   string
	numeric bool
}

var pep440VersionPattern = regexp.MustCompile(
	`(?i)^v?` +
		`(?:(?:(?P<epoch>[0-9]+)!)?` +
		`(?P<release>[0-9]+(?:\.[0-9]+)*)` +
		`(?P<pre>[._-]?(?P<pre_l>alpha|a|beta|b|preview|pre|c|rc)[._-]?(?P<pre_n>[0-9]+)?)?` +
		`(?P<post>(?:-(?P<post_n1>[0-9]+))|(?:[._-]?(?P<post_l>post|rev|r)[._-]?(?P<post_n2>[0-9]+)?))?` +
		`(?P<dev>[._-]?(?P<dev_l>dev)[._-]?(?P<dev_n>[0-9]+)?)?)` +
		`(?:\+(?P<local>[a-z0-9]+(?:[._-][a-z0-9]+)*))?$`,
)

func (pep440Dialect) NormalizePackageName(name string) (string, error) {
	if err := validatePyPIPackageName(name); err != nil {
		return "", err
	}
	return normalizePythonName(name), nil
}

func validatePyPIPackageName(name string) error {
	if err := validatePackageNameInput(name); err != nil {
		return err
	}
	for index := range len(name) {
		character := name[index]
		if isASCIIAlphanumeric(character) || character == '.' || character == '_' || character == '-' {
			continue
		}
		return fmt.Errorf("PyPI package name contains invalid character %q", character)
	}
	if !isASCIIAlphanumeric(name[0]) || !isASCIIAlphanumeric(name[len(name)-1]) {
		return fmt.Errorf("PyPI package name must start and end with an ASCII letter or digit")
	}
	return nil
}

func (pep440Dialect) ValidateVersion(version string) error {
	_, err := parsePEP440Version(version)
	return err
}

func (pep440Dialect) CompareVersions(left, right string) (int, error) {
	leftVersion, err := parsePEP440Version(left)
	if err != nil {
		return 0, fmt.Errorf("parse left version: %w", err)
	}
	rightVersion, err := parsePEP440Version(right)
	if err != nil {
		return 0, fmt.Errorf("parse right version: %w", err)
	}
	return comparePEP440Versions(leftVersion, rightVersion), nil
}

func (pep440Dialect) SupportsRanges() bool { return true }

func (pep440Dialect) canonicalVersion(value string) (string, error) {
	version, err := parsePEP440Version(value)
	if err != nil {
		return "", err
	}
	return version.canonical, nil
}

func (pep440Dialect) validateRangeTarget(value string) error {
	version, err := parsePEP440Version(value)
	if err != nil {
		return err
	}
	if len(version.local) != 0 {
		return fmt.Errorf("PEP 440 ordered comparisons do not permit local version targets")
	}
	return nil
}

func parsePEP440Version(value string) (pep440Version, error) {
	if value == "" || strings.TrimSpace(value) != value ||
		strings.ContainsFunc(value, unicode.IsControl) || strings.ContainsFunc(value, unicode.IsSpace) {
		return pep440Version{}, fmt.Errorf("PEP 440 version must be non-empty and contain no whitespace or control characters")
	}
	for index := range len(value) {
		if value[index] > unicode.MaxASCII {
			return pep440Version{}, fmt.Errorf("PEP 440 version must contain ASCII characters only")
		}
	}
	matches := pep440VersionPattern.FindStringSubmatch(value)
	if matches == nil {
		return pep440Version{}, fmt.Errorf("invalid PEP 440 version %q", value)
	}
	groups := make(map[string]string, len(matches))
	for index, name := range pep440VersionPattern.SubexpNames() {
		if index != 0 && name != "" {
			groups[name] = matches[index]
		}
	}

	version := pep440Version{epoch: normalizeDecimal(groups["epoch"])}
	for _, component := range strings.Split(groups["release"], ".") {
		version.release = append(version.release, normalizeDecimal(component))
	}
	if groups["pre"] != "" {
		kind := strings.ToLower(groups["pre_l"])
		switch kind {
		case "alpha":
			kind = "a"
		case "beta":
			kind = "b"
		case "c", "pre", "preview":
			kind = "rc"
		}
		version.pre = &pep440Prerelease{kind: kind, number: normalizeDecimal(groups["pre_n"])}
	}
	if groups["post"] != "" {
		number := groups["post_n1"]
		if number == "" {
			number = groups["post_n2"]
		}
		normalized := normalizeDecimal(number)
		version.post = &normalized
	}
	if groups["dev"] != "" {
		normalized := normalizeDecimal(groups["dev_n"])
		version.dev = &normalized
	}
	if groups["local"] != "" {
		for _, component := range strings.FieldsFunc(strings.ToLower(groups["local"]), func(character rune) bool {
			return character == '.' || character == '-' || character == '_'
		}) {
			numeric := allASCIIDigits(component)
			if numeric {
				component = normalizeDecimal(component)
			}
			version.local = append(version.local, pep440LocalSegment{value: component, numeric: numeric})
		}
	}
	version.canonical = version.buildCanonical()
	return version, nil
}

func normalizeDecimal(value string) string {
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "0"
	}
	return value
}

func (version pep440Version) buildCanonical() string {
	var canonical strings.Builder
	if version.epoch != "0" {
		canonical.WriteString(version.epoch)
		canonical.WriteByte('!')
	}
	canonical.WriteString(strings.Join(version.release, "."))
	if version.pre != nil {
		canonical.WriteString(version.pre.kind)
		canonical.WriteString(version.pre.number)
	}
	if version.post != nil {
		canonical.WriteString(".post")
		canonical.WriteString(*version.post)
	}
	if version.dev != nil {
		canonical.WriteString(".dev")
		canonical.WriteString(*version.dev)
	}
	if len(version.local) != 0 {
		canonical.WriteByte('+')
		for index, component := range version.local {
			if index != 0 {
				canonical.WriteByte('.')
			}
			canonical.WriteString(component.value)
		}
	}
	return canonical.String()
}

func comparePEP440Versions(left, right pep440Version) int {
	if comparison := compareDecimal(left.epoch, right.epoch); comparison != 0 {
		return comparison
	}
	if comparison := comparePEP440Release(left.release, right.release); comparison != 0 {
		return comparison
	}
	if comparison := comparePEP440Pre(left, right); comparison != 0 {
		return comparison
	}
	if comparison := compareOptionalDecimal(left.post, right.post, -1); comparison != 0 {
		return comparison
	}
	if comparison := compareOptionalDecimal(left.dev, right.dev, 1); comparison != 0 {
		return comparison
	}
	return comparePEP440Local(left.local, right.local)
}

func comparePEP440Release(left, right []string) int {
	length := max(len(left), len(right))
	for index := 0; index < length; index++ {
		leftComponent, rightComponent := "0", "0"
		if index < len(left) {
			leftComponent = left[index]
		}
		if index < len(right) {
			rightComponent = right[index]
		}
		if comparison := compareDecimal(leftComponent, rightComponent); comparison != 0 {
			return comparison
		}
	}
	return 0
}

func comparePEP440Pre(left, right pep440Version) int {
	leftRank := pep440PreSentinel(left)
	rightRank := pep440PreSentinel(right)
	if leftRank != rightRank {
		return signOf(leftRank - rightRank)
	}
	if leftRank != 0 {
		return 0
	}
	preOrder := map[string]int{"a": 0, "b": 1, "rc": 2}
	if leftOrder, rightOrder := preOrder[left.pre.kind], preOrder[right.pre.kind]; leftOrder != rightOrder {
		return signOf(leftOrder - rightOrder)
	}
	return compareDecimal(left.pre.number, right.pre.number)
}

// -1 is NegativeInfinity, 0 is a concrete prerelease, and 1 is Infinity.
func pep440PreSentinel(version pep440Version) int {
	if version.pre != nil {
		return 0
	}
	if version.post == nil && version.dev != nil {
		return -1
	}
	return 1
}

// missingOrder places a missing value before (-1) or after (+1) a concrete
// value. PEP 440 uses the former for post releases and the latter for dev.
func compareOptionalDecimal(left, right *string, missingOrder int) int {
	switch {
	case left == nil && right == nil:
		return 0
	case left == nil:
		return missingOrder
	case right == nil:
		return -missingOrder
	default:
		return compareDecimal(*left, *right)
	}
}

func comparePEP440Local(left, right []pep440LocalSegment) int {
	switch {
	case len(left) == 0 && len(right) == 0:
		return 0
	case len(left) == 0:
		return -1
	case len(right) == 0:
		return 1
	}
	length := min(len(left), len(right))
	for index := 0; index < length; index++ {
		leftComponent, rightComponent := left[index], right[index]
		if leftComponent.numeric != rightComponent.numeric {
			if leftComponent.numeric {
				return 1
			}
			return -1
		}
		var comparison int
		if leftComponent.numeric {
			comparison = compareDecimal(leftComponent.value, rightComponent.value)
		} else {
			comparison = signOf(strings.Compare(leftComponent.value, rightComponent.value))
		}
		if comparison != 0 {
			return comparison
		}
	}
	return signOf(len(left) - len(right))
}

func (version pep440Version) withoutLocal() pep440Version {
	version.local = nil
	return version
}

// postBase returns the exact public version that version is a post-release of.
// It mirrors packaging 26.2's _post_base: post, dev, and local are removed,
// while a prerelease segment remains significant.
func (version pep440Version) postBase() pep440Version {
	version.post = nil
	version.dev = nil
	version.local = nil
	return version
}

// earliestPrerelease returns packaging 26.2's lower bound for "a pre-release
// of V": V with dev set to zero and its local segment removed. A post segment
// remains significant, so 1.0.dev0 is not treated as a pre-release of
// 1.0.post1.
func (version pep440Version) earliestPrerelease() pep440Version {
	zero := "0"
	version.dev = &zero
	version.local = nil
	return version
}

func (version pep440Version) isPrerelease() bool {
	return version.pre != nil || version.dev != nil
}

func (version pep440Version) isPostrelease() bool { return version.post != nil }
