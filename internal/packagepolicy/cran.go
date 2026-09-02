package packagepolicy

import (
	"fmt"
	"strings"
	"unicode"
)

// cranDialect follows R's package_version identity: version components are
// arbitrary non-negative decimal integers separated by '.' or '-', leading
// zeroes are insignificant, and missing trailing components compare as zero.
// Package Rules remain exact-only because Depsilo does not expose R's richer
// dependency-constraint language as a single ordered comparator.
type cranDialect struct{}

type cranVersion struct {
	components []string
}

func (cranDialect) NormalizePackageName(name string) (string, error) {
	if err := validateCRANPackageName(name); err != nil {
		return "", err
	}
	return name, nil
}

func validateCRANPackageName(name string) error {
	if err := validatePackageNameInput(name); err != nil {
		return err
	}
	if len(name) < 2 || !isASCIIAlpha(name[0]) {
		return fmt.Errorf("CRAN package name must contain at least two characters and start with an ASCII letter")
	}
	if !isASCIIAlphanumeric(name[len(name)-1]) {
		return fmt.Errorf("CRAN package name must end with an ASCII letter or digit")
	}
	for index := range len(name) {
		if isASCIIAlphanumeric(name[index]) || name[index] == '.' {
			continue
		}
		return fmt.Errorf("CRAN package name contains invalid character %q", name[index])
	}
	return nil
}

func isASCIIAlpha(character byte) bool {
	return character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
}

func (cranDialect) normalizePackagePrefix(prefix string) (string, error) {
	if err := validatePackageNameInput(prefix); err != nil {
		return "", err
	}
	if err := validateCRANPackageName(prefix); err != nil {
		if candidateErr := validateCRANPackageName(prefix + "x"); candidateErr != nil {
			return "", candidateErr
		}
	}
	return prefix, nil
}

func (cranDialect) normalizePackageGlob(pattern string) (string, error) {
	if err := validatePackageGlobInput(pattern); err != nil {
		return "", fmt.Errorf("invalid CRAN package glob: %w", err)
	}
	if err := validateGlobCharacters(pattern, ".", true); err != nil {
		return "", fmt.Errorf("invalid CRAN package glob: %w", err)
	}
	if !hasGlobMetacharacter(pattern) {
		if err := validateCRANPackageName(pattern); err != nil {
			return "", err
		}
	}
	return pattern, nil
}

func (cranDialect) ValidateVersion(version string) error {
	_, err := parseCRANVersion(version)
	return err
}

func (cranDialect) CompareVersions(left, right string) (int, error) {
	a, err := parseCRANVersion(left)
	if err != nil {
		return 0, fmt.Errorf("parse left CRAN version: %w", err)
	}
	b, err := parseCRANVersion(right)
	if err != nil {
		return 0, fmt.Errorf("parse right CRAN version: %w", err)
	}
	length := max(len(a.components), len(b.components))
	for index := 0; index < length; index++ {
		leftComponent, rightComponent := "0", "0"
		if index < len(a.components) {
			leftComponent = a.components[index]
		}
		if index < len(b.components) {
			rightComponent = b.components[index]
		}
		if comparison := compareDecimal(leftComponent, rightComponent); comparison != 0 {
			return comparison, nil
		}
	}
	return 0, nil
}

func (cranDialect) SupportsRanges() bool { return false }

func (cranDialect) canonicalVersion(value string) (string, error) {
	version, err := parseCRANVersion(value)
	if err != nil {
		return "", err
	}
	components := append([]string(nil), version.components...)
	for len(components) > 2 && components[len(components)-1] == "0" {
		components = components[:len(components)-1]
	}
	return strings.Join(components, "."), nil
}

func parseCRANVersion(value string) (cranVersion, error) {
	if value == "" || strings.TrimSpace(value) != value ||
		strings.ContainsFunc(value, func(character rune) bool {
			return unicode.IsControl(character) || unicode.IsSpace(character)
		}) {
		return cranVersion{}, fmt.Errorf("CRAN version must be non-empty and contain no whitespace or control characters")
	}
	components := make([]string, 0, strings.Count(value, ".")+strings.Count(value, "-")+1)
	start := 0
	for index := 0; index <= len(value); index++ {
		if index != len(value) && value[index] != '.' && value[index] != '-' {
			if !isASCIIDigit(value[index]) {
				return cranVersion{}, fmt.Errorf("CRAN version contains invalid character %q", value[index])
			}
			continue
		}
		if index == start {
			return cranVersion{}, fmt.Errorf("CRAN version contains an empty numeric component")
		}
		components = append(components, normalizeDecimal(value[start:index]))
		start = index + 1
	}
	if len(components) < 2 {
		return cranVersion{}, fmt.Errorf("CRAN version must contain at least two numeric components")
	}
	return cranVersion{components: components}, nil
}
