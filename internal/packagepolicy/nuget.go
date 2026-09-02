package packagepolicy

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// nugetDialect keeps package selectors exact-only while applying NuGetVersion
// identity to exact versions. NuGet deliberately differs from strict SemVer:
// it accepts one to four release components, pads omitted components with zero,
// compares prerelease labels case-insensitively, and ignores build metadata.
type nugetDialect struct {
	exactDialect
}

type nugetVersion struct {
	release    [4]uint32
	prerelease []nugetReleaseLabel
}

type nugetReleaseLabel struct {
	value   string
	numeric bool
	number  uint32
}

func validateNuGetPackageName(name string) error {
	if err := validatePackageNameInput(name); err != nil {
		return err
	}
	if len(name) > 100 {
		return fmt.Errorf("NuGet package ID exceeds 100 characters")
	}
	if !isNuGetPackageWord(name[0]) {
		return fmt.Errorf("NuGet package ID must start with an ASCII letter, digit, or underscore")
	}
	for index := 0; index < len(name); {
		if isNuGetPackageWord(name[index]) {
			index++
			continue
		}
		if name[index] != '.' && name[index] != '-' {
			return fmt.Errorf("NuGet package ID contains invalid character %q", name[index])
		}
		index++
		if index == len(name) || !isNuGetPackageWord(name[index]) {
			return fmt.Errorf("NuGet package ID separator must be followed by an ASCII letter, digit, or underscore")
		}
	}
	return nil
}

func isNuGetPackageWord(character byte) bool {
	return isASCIIAlphanumeric(character) || character == '_'
}

func normalizeNuGetPackagePrefix(prefix string) (string, error) {
	if err := validatePackageNameInput(prefix); err != nil {
		return "", err
	}
	if err := validateNuGetPackageName(prefix); err != nil {
		if len(prefix) >= 100 {
			return "", err
		}
		if candidateErr := validateNuGetPackageName(prefix + "x"); candidateErr != nil {
			return "", candidateErr
		}
	}
	return lowerASCII(prefix), nil
}

func normalizeNuGetPackageGlob(pattern string) (string, error) {
	if err := validatePackageGlobInput(pattern); err != nil {
		return "", fmt.Errorf("invalid NuGet package glob: %w", err)
	}
	if len(pattern) > 100 {
		return "", fmt.Errorf("NuGet package glob exceeds 100 characters")
	}
	if err := validateGlobCharacters(pattern, "_.-", true); err != nil {
		return "", fmt.Errorf("invalid NuGet package glob: %w", err)
	}
	if !hasGlobMetacharacter(pattern) {
		if err := validateNuGetPackageName(pattern); err != nil {
			return "", err
		}
	}
	return lowerASCII(pattern), nil
}

func (nugetDialect) ValidateVersion(value string) error {
	_, err := parseNuGetVersion(value)
	return err
}

func (nugetDialect) CompareVersions(left, right string) (int, error) {
	a, err := parseNuGetVersion(left)
	if err != nil {
		return 0, fmt.Errorf("parse left NuGet version: %w", err)
	}
	b, err := parseNuGetVersion(right)
	if err != nil {
		return 0, fmt.Errorf("parse right NuGet version: %w", err)
	}
	return compareNuGetVersions(a, b), nil
}

func (nugetDialect) SupportsRanges() bool { return false }

func (nugetDialect) canonicalVersion(value string) (string, error) {
	version, err := parseNuGetVersion(value)
	if err != nil {
		return "", err
	}
	return version.canonical(), nil
}

func parseNuGetVersion(value string) (nugetVersion, error) {
	if value == "" || strings.TrimSpace(value) != value ||
		strings.ContainsFunc(value, func(character rune) bool {
			return unicode.IsControl(character) || unicode.IsSpace(character)
		}) {
		return nugetVersion{}, fmt.Errorf("NuGet version must be non-empty and contain no whitespace or control characters")
	}

	public, metadata, hasMetadata := strings.Cut(value, "+")
	if hasMetadata {
		if strings.Contains(metadata, "+") || !validNuGetIdentifiers(metadata, true) {
			return nugetVersion{}, fmt.Errorf("invalid NuGet build metadata")
		}
	}

	core, prerelease, hasPrerelease := strings.Cut(public, "-")
	if hasPrerelease && !validNuGetIdentifiers(prerelease, false) {
		return nugetVersion{}, fmt.Errorf("invalid NuGet prerelease labels")
	}

	parts := strings.Split(core, ".")
	if len(parts) < 1 || len(parts) > 4 {
		return nugetVersion{}, fmt.Errorf("NuGet release must contain one to four numeric components")
	}
	var parsed nugetVersion
	for index, part := range parts {
		number, err := parseNuGetReleaseNumber(part)
		if err != nil {
			return nugetVersion{}, fmt.Errorf("invalid NuGet release component %q: %w", part, err)
		}
		parsed.release[index] = number
	}

	if hasPrerelease {
		labels := strings.Split(prerelease, ".")
		parsed.prerelease = make([]nugetReleaseLabel, len(labels))
		for index, label := range labels {
			canonical := lowerASCII(label)
			number, numeric := parseNuGetPrereleaseNumber(canonical)
			parsed.prerelease[index] = nugetReleaseLabel{
				value: canonical, numeric: numeric, number: number,
			}
		}
	}
	return parsed, nil
}

func parseNuGetReleaseNumber(value string) (uint32, error) {
	if value == "" {
		return 0, fmt.Errorf("component is empty")
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf("component must contain only ASCII digits")
		}
	}

	// NuGetVersion stores release components in System.Version, whose fields
	// are non-negative Int32 values. Strip zeroes first so a long spelling of a
	// small value retains NuGet's normalization behavior without overflowing.
	canonical := strings.TrimLeft(value, "0")
	if canonical == "" {
		return 0, nil
	}
	const maxInt32 = "2147483647"
	if len(canonical) > len(maxInt32) || len(canonical) == len(maxInt32) && canonical > maxInt32 {
		return 0, fmt.Errorf("component exceeds 2147483647")
	}
	number, err := strconv.ParseUint(canonical, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(number), nil
}

func validNuGetIdentifiers(value string, allowNumericLeadingZero bool) bool {
	if value == "" {
		return false
	}
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return false
		}
		allDigits := true
		for _, character := range identifier {
			if !isNuGetIdentifierCharacter(character) {
				return false
			}
			if character < '0' || character > '9' {
				allDigits = false
			}
		}
		if !allowNumericLeadingZero && allDigits && len(identifier) > 1 && identifier[0] == '0' {
			return false
		}
	}
	return true
}

func isNuGetIdentifierCharacter(character rune) bool {
	return character >= '0' && character <= '9' ||
		character >= 'A' && character <= 'Z' ||
		character >= 'a' && character <= 'z' ||
		character == '-'
}

func parseNuGetPrereleaseNumber(value string) (uint32, bool) {
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	// VersionComparer uses Int32.TryParse when deciding whether a prerelease
	// identifier is numeric. Larger all-digit identifiers therefore follow its
	// string-label path rather than the numeric path.
	number, err := strconv.ParseUint(value, 10, 31)
	if err != nil {
		return 0, false
	}
	return uint32(number), true
}

func compareNuGetVersions(left, right nugetVersion) int {
	for index := range left.release {
		if left.release[index] < right.release[index] {
			return -1
		}
		if left.release[index] > right.release[index] {
			return 1
		}
	}
	if len(left.prerelease) == 0 && len(right.prerelease) == 0 {
		return 0
	}
	if len(left.prerelease) == 0 {
		return 1
	}
	if len(right.prerelease) == 0 {
		return -1
	}

	count := len(left.prerelease)
	if len(right.prerelease) > count {
		count = len(right.prerelease)
	}
	for index := 0; index < count; index++ {
		if index == len(left.prerelease) {
			return -1
		}
		if index == len(right.prerelease) {
			return 1
		}
		comparison := compareNuGetReleaseLabels(left.prerelease[index], right.prerelease[index])
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}

func compareNuGetReleaseLabels(left, right nugetReleaseLabel) int {
	if left.numeric && right.numeric {
		if left.number < right.number {
			return -1
		}
		if left.number > right.number {
			return 1
		}
		return 0
	}
	if left.numeric {
		return -1
	}
	if right.numeric {
		return 1
	}
	return strings.Compare(left.value, right.value)
}

func (version nugetVersion) canonical() string {
	var canonical strings.Builder
	canonical.WriteString(strconv.FormatUint(uint64(version.release[0]), 10))
	canonical.WriteByte('.')
	canonical.WriteString(strconv.FormatUint(uint64(version.release[1]), 10))
	canonical.WriteByte('.')
	canonical.WriteString(strconv.FormatUint(uint64(version.release[2]), 10))
	if version.release[3] != 0 {
		canonical.WriteByte('.')
		canonical.WriteString(strconv.FormatUint(uint64(version.release[3]), 10))
	}
	if len(version.prerelease) != 0 {
		canonical.WriteByte('-')
		for index, label := range version.prerelease {
			if index != 0 {
				canonical.WriteByte('.')
			}
			canonical.WriteString(label.value)
		}
	}
	return canonical.String()
}

func lowerASCII(value string) string {
	var lowered strings.Builder
	lowered.Grow(len(value))
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		lowered.WriteByte(character)
	}
	return lowered.String()
}
