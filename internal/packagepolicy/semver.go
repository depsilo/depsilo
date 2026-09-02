package packagepolicy

import (
	"fmt"
	"strings"
)

// strictSemVer is the small, complete SemVer surface package rules need. The
// rule language accepts one ordered comparator, so using a general constraint
// solver only adds semantics and edge cases we do not expose. Decimal strings
// avoid overflow for numeric prerelease identifiers and Cargo's uint64 core.
type strictSemVer struct {
	raw        string
	core       [3]string
	prerelease []semverIdentifier
}

type semverIdentifier struct {
	value   string
	numeric bool
}

func parseStrictSemVer(value, ecosystem, coreMax string) (strictSemVer, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return strictSemVer{}, fmt.Errorf("SemVer must be non-empty and contain no surrounding whitespace")
	}

	public := value
	if plus := strings.IndexByte(public, '+'); plus >= 0 {
		if strings.ContainsRune(public[plus+1:], '+') {
			return strictSemVer{}, fmt.Errorf("invalid SemVer build metadata")
		}
		if _, err := parseSemVerIdentifiers(public[plus+1:], false); err != nil {
			return strictSemVer{}, fmt.Errorf("invalid SemVer build metadata: %w", err)
		}
		public = public[:plus]
	}

	coreText := public
	var prerelease []semverIdentifier
	if dash := strings.IndexByte(coreText, '-'); dash >= 0 {
		var err error
		prerelease, err = parseSemVerIdentifiers(coreText[dash+1:], true)
		if err != nil {
			return strictSemVer{}, fmt.Errorf("invalid SemVer prerelease: %w", err)
		}
		coreText = coreText[:dash]
	}

	parts := strings.Split(coreText, ".")
	if len(parts) != 3 {
		return strictSemVer{}, fmt.Errorf("SemVer release must contain major.minor.patch")
	}
	parsed := strictSemVer{raw: value, prerelease: prerelease}
	for index, part := range parts {
		if !allASCIIDigits(part) || len(part) > 1 && part[0] == '0' {
			return strictSemVer{}, fmt.Errorf("invalid SemVer release component %q", part)
		}
		if compareDecimal(part, coreMax) > 0 {
			return strictSemVer{}, fmt.Errorf("%s SemVer release component %q exceeds %s", ecosystem, part, coreMax)
		}
		parsed.core[index] = part
	}
	return parsed, nil
}

func parseSemVerIdentifiers(value string, rejectNumericLeadingZero bool) ([]semverIdentifier, error) {
	if value == "" {
		return nil, fmt.Errorf("identifier list is empty")
	}
	parts := strings.Split(value, ".")
	identifiers := make([]semverIdentifier, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("identifier is empty")
		}
		numeric := true
		for index := range len(part) {
			character := part[index]
			if !isASCIIAlphanumeric(character) && character != '-' {
				return nil, fmt.Errorf("identifier %q contains invalid character %q", part, character)
			}
			if !isASCIIDigit(character) {
				numeric = false
			}
		}
		if rejectNumericLeadingZero && numeric && len(part) > 1 && part[0] == '0' {
			return nil, fmt.Errorf("numeric identifier %q has a leading zero", part)
		}
		identifiers = append(identifiers, semverIdentifier{value: part, numeric: numeric})
	}
	return identifiers, nil
}

func (version strictSemVer) canonical() string { return version.raw }

func (version strictSemVer) Compare(other strictSemVer) int {
	if comparison := version.compareCore(other); comparison != 0 {
		return comparison
	}
	if len(version.prerelease) == 0 {
		if len(other.prerelease) == 0 {
			return 0
		}
		return 1
	}
	if len(other.prerelease) == 0 {
		return -1
	}
	common := min(len(version.prerelease), len(other.prerelease))
	for index := 0; index < common; index++ {
		left := version.prerelease[index]
		right := other.prerelease[index]
		var comparison int
		switch {
		case left.numeric && right.numeric:
			comparison = compareDecimal(left.value, right.value)
		case left.numeric:
			comparison = -1
		case right.numeric:
			comparison = 1
		default:
			comparison = signOf(strings.Compare(left.value, right.value))
		}
		if comparison != 0 {
			return comparison
		}
	}
	return signOf(len(version.prerelease) - len(other.prerelease))
}

func (version strictSemVer) compareCore(other strictSemVer) int {
	for index := range version.core {
		if comparison := compareDecimal(version.core[index], other.core[index]); comparison != 0 {
			return comparison
		}
	}
	return 0
}

func (version strictSemVer) hasPrerelease() bool { return len(version.prerelease) != 0 }
