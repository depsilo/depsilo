// Package gomoduleidentity owns the canonical mapping between Go module
// identities and their GOPROXY path representation.
package gomoduleidentity

import (
	"fmt"
	"path"
	"strings"

	"golang.org/x/mod/module"
)

// ValidatePath reports whether value is a canonical, unescaped Go module
// path. It includes the module-aware /vN and gopkg.in major-suffix rules.
func ValidatePath(value string) error {
	if err := module.CheckPath(value); err != nil {
		return fmt.Errorf("invalid Go module path %q: %w", value, err)
	}
	return nil
}

// DecodeProxyPath converts the canonical GOPROXY spelling to the module's
// case-sensitive identity. module.UnescapePath deliberately accepts only
// ! followed by a lowercase ASCII letter and validates the decoded path.
func DecodeProxyPath(escaped string) (string, error) {
	value, err := module.UnescapePath(escaped)
	if err != nil {
		return "", err
	}
	canonical, err := module.EscapePath(value)
	if err != nil {
		return "", err
	}
	if canonical != escaped {
		return "", fmt.Errorf("non-canonical escaped Go module path %q", escaped)
	}
	return value, nil
}

// DecodeProxyVersion converts the canonical GOPROXY filename spelling to the
// version identity used by module metadata and policy rules. Uppercase ASCII
// letters and literal exclamation marks must be escaped in proxy URLs; the
// round trip rejects alternate or malformed spellings instead of guessing.
func DecodeProxyVersion(escaped string) (string, error) {
	if escaped == "" {
		return "", fmt.Errorf("empty escaped Go module version")
	}
	value, err := module.UnescapeVersion(escaped)
	if err != nil {
		return "", err
	}
	canonical, err := module.EscapeVersion(value)
	if err != nil {
		return "", err
	}
	if canonical != escaped {
		return "", fmt.Errorf("non-canonical escaped Go module version %q", escaped)
	}
	return value, nil
}

// ValidatePrefix reports whether prefix can prefix at least one valid module
// identity. Concrete request identities are validated again before matching,
// so a partial final path segment cannot make an invalid identity valid.
func ValidatePrefix(prefix string) error {
	if err := ValidatePath(prefix); err == nil {
		return nil
	}
	if err := ValidatePath(prefix + "x"); err != nil {
		return fmt.Errorf("invalid Go module path prefix %q: %w", prefix, err)
	}
	return nil
}

// ValidateGlob validates path.Match syntax and the literal Go module path
// structure around its metacharacters. A valid concrete witness proves the
// pattern has the required module host and safe slash-separated shape;
// request identities still cross ValidatePath before matching.
func ValidateGlob(pattern string) error {
	if _, err := path.Match(pattern, ""); err != nil {
		return fmt.Errorf("invalid Go module path glob %q: %w", pattern, err)
	}
	if pattern == "" || strings.HasPrefix(pattern, "/") || strings.HasSuffix(pattern, "/") || strings.Contains(pattern, "//") {
		return fmt.Errorf("invalid Go module path glob %q: unsafe slash-separated shape", pattern)
	}

	witness, err := globWitness(pattern)
	if err != nil {
		return fmt.Errorf("invalid Go module path glob %q: %w", pattern, err)
	}
	if err := ValidatePath(witness); err != nil {
		return fmt.Errorf("invalid Go module path glob %q: %w", pattern, err)
	}
	matched, err := path.Match(pattern, witness)
	if err != nil || !matched {
		return fmt.Errorf("invalid Go module path glob %q: cannot construct a valid matching module path", pattern)
	}
	return nil
}

func globWitness(pattern string) (string, error) {
	var witness strings.Builder
	witness.Grow(len(pattern))
	for index := 0; index < len(pattern); index++ {
		character := pattern[index]
		switch character {
		case '*', '?':
			witness.WriteByte('x')
		case '[':
			closing := closingBracket(pattern, index)
			if closing == len(pattern) {
				return "", fmt.Errorf("unterminated character class")
			}
			candidate, ok := classWitness(pattern[index : closing+1])
			if !ok {
				return "", fmt.Errorf("character class contains no valid module path character")
			}
			witness.WriteByte(candidate)
			index = closing
		case '\\':
			index++
			if index == len(pattern) {
				return "", fmt.Errorf("trailing escape")
			}
			escaped := pattern[index]
			if !isModulePathByte(escaped) {
				return "", fmt.Errorf("escaped character %q is not valid in a module path", escaped)
			}
			witness.WriteByte(escaped)
		default:
			if !isModulePathByte(character) && character != '/' {
				return "", fmt.Errorf("character %q is not valid in a module path", character)
			}
			witness.WriteByte(character)
		}
	}
	return witness.String(), nil
}

func closingBracket(pattern string, opening int) int {
	for index := opening + 1; index < len(pattern); index++ {
		if pattern[index] == '\\' {
			index++
			continue
		}
		if pattern[index] == ']' {
			return index
		}
	}
	return len(pattern)
}

func classWitness(class string) (byte, bool) {
	// Prefer lowercase host-safe characters and legal semantic-import major
	// digits before candidates such as 0 and 1 that are invalid after "/v".
	const candidates = "abcdefghijklmnopqrstuvwxyz23456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_-~.01"
	for index := range len(candidates) {
		candidate := candidates[index]
		matched, err := path.Match(class, string(candidate))
		if err == nil && matched {
			return candidate, true
		}
	}
	return 0, false
}

func isModulePathByte(character byte) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' ||
		strings.ContainsRune("-._~", rune(character))
}
