package packagepolicy

import (
	"fmt"
	"strings"
)

type debianDialect struct{}

type debianVersion struct {
	epoch    string
	upstream string
	revision string
}

func (debianDialect) NormalizePackageName(name string) (string, error) {
	if err := validatePackageNameInput(name); err != nil {
		return "", err
	}
	if len(name) < 2 || !isASCIILowercaseAlphanumeric(name[0]) {
		return "", fmt.Errorf("Debian package name must contain at least two characters and start with a letter or digit")
	}
	for index := range len(name) {
		character := name[index]
		if isASCIILowercaseAlphanumeric(character) || character == '+' || character == '-' || character == '.' {
			continue
		}
		return "", fmt.Errorf("Debian package name contains invalid character %q", character)
	}
	return name, nil
}

func isASCIILowercaseAlphanumeric(character byte) bool {
	return isASCIIDigit(character) || character >= 'a' && character <= 'z'
}

func (debianDialect) ValidateVersion(version string) error {
	_, err := parseDebianVersion(version)
	return err
}

func (debianDialect) CompareVersions(a, b string) (int, error) {
	left, err := parseDebianVersion(a)
	if err != nil {
		return 0, fmt.Errorf("parse left version: %w", err)
	}
	right, err := parseDebianVersion(b)
	if err != nil {
		return 0, fmt.Errorf("parse right version: %w", err)
	}

	if comparison := compareDecimal(left.epoch, right.epoch); comparison != 0 {
		return comparison, nil
	}
	if comparison := compareDebianPart(left.upstream, right.upstream); comparison != 0 {
		return comparison, nil
	}
	return compareDebianPart(left.revision, right.revision), nil
}

func (debianDialect) SupportsRanges() bool { return true }

func (debianDialect) canonicalVersion(value string) (string, error) {
	parsed, err := parseDebianVersion(value)
	if err != nil {
		return "", err
	}
	epoch := strings.TrimLeft(parsed.epoch, "0")
	var normalized strings.Builder
	if epoch != "" {
		normalized.WriteString(epoch)
		normalized.WriteByte(':')
	}
	normalized.WriteString(parsed.upstream)
	if parsed.revision != "" {
		normalized.WriteByte('-')
		normalized.WriteString(parsed.revision)
	}
	return normalized.String(), nil
}

func parseDebianVersion(value string) (debianVersion, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return debianVersion{}, fmt.Errorf("Debian version must be non-empty and contain no surrounding whitespace")
	}

	parsed := debianVersion{epoch: "0"}
	remainder := value
	if colon := strings.IndexByte(remainder, ':'); colon >= 0 {
		if colon == 0 || !allASCIIDigits(remainder[:colon]) {
			return debianVersion{}, fmt.Errorf("Debian epoch must contain only decimal digits")
		}
		parsed.epoch = remainder[:colon]
		remainder = remainder[colon+1:]
		// A version has at most one epoch separator. Without this check a
		// second colon could be mistaken for part of upstream_version (for
		// example, 1:2:bad), causing an invalid version to participate in
		// ordered policy decisions.
		if strings.IndexByte(remainder, ':') >= 0 {
			return debianVersion{}, fmt.Errorf("Debian version contains more than one epoch separator")
		}
	}

	if dash := strings.LastIndexByte(remainder, '-'); dash >= 0 {
		parsed.upstream = remainder[:dash]
		parsed.revision = remainder[dash+1:]
		if parsed.revision == "" {
			return debianVersion{}, fmt.Errorf("Debian revision is empty")
		}
	} else {
		parsed.upstream = remainder
	}

	if parsed.upstream == "" || !isASCIIDigit(parsed.upstream[0]) {
		return debianVersion{}, fmt.Errorf("Debian upstream version must start with a decimal digit")
	}
	for index := 0; index < len(parsed.upstream); index++ {
		character := parsed.upstream[index]
		if !isASCIIAlphanumeric(character) && !strings.ContainsRune(".+-~", rune(character)) {
			return debianVersion{}, fmt.Errorf("Debian upstream version contains invalid character %q", character)
		}
	}
	for index := 0; index < len(parsed.revision); index++ {
		character := parsed.revision[index]
		if !isASCIIAlphanumeric(character) && !strings.ContainsRune("+.~", rune(character)) {
			return debianVersion{}, fmt.Errorf("Debian revision contains invalid character %q", character)
		}
	}
	return parsed, nil
}

func compareDecimal(left, right string) int {
	left = strings.TrimLeft(left, "0")
	right = strings.TrimLeft(right, "0")
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return signOf(strings.Compare(left, right))
}

// compareDebianPart implements Debian Policy's alternating non-digit/digit
// comparison without converting arbitrary-length numeric fields to integers.
func compareDebianPart(left, right string) int {
	for len(left) > 0 || len(right) > 0 {
		for (len(left) > 0 && !isASCIIDigit(left[0])) || (len(right) > 0 && !isASCIIDigit(right[0])) {
			var leftCharacter, rightCharacter byte
			if len(left) > 0 && !isASCIIDigit(left[0]) {
				leftCharacter = left[0]
				left = left[1:]
			}
			if len(right) > 0 && !isASCIIDigit(right[0]) {
				rightCharacter = right[0]
				right = right[1:]
			}
			if leftOrder, rightOrder := debianCharacterOrder(leftCharacter), debianCharacterOrder(rightCharacter); leftOrder != rightOrder {
				return signOf(leftOrder - rightOrder)
			}
		}

		leftDigits, leftRest := takeDigits(left)
		rightDigits, rightRest := takeDigits(right)
		if comparison := compareDecimal(leftDigits, rightDigits); comparison != 0 {
			return comparison
		}
		left, right = leftRest, rightRest
	}
	return 0
}

func takeDigits(value string) (string, string) {
	index := 0
	for index < len(value) && isASCIIDigit(value[index]) {
		index++
	}
	return value[:index], value[index:]
}

func debianCharacterOrder(character byte) int {
	switch {
	case character == '~':
		return -1
	case character == 0:
		return 0
	case character >= 'A' && character <= 'Z', character >= 'a' && character <= 'z':
		return int(character)
	default:
		return int(character) + 256
	}
}

func allASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for index := range len(value) {
		if !isASCIIDigit(value[index]) {
			return false
		}
	}
	return true
}

func isASCIIDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

func isASCIIAlphanumeric(character byte) bool {
	return isASCIIDigit(character) ||
		character >= 'A' && character <= 'Z' ||
		character >= 'a' && character <= 'z'
}

func signOf(value int) int {
	switch {
	case value < 0:
		return -1
	case value > 0:
		return 1
	default:
		return 0
	}
}
