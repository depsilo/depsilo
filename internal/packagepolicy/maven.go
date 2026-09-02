// Licensed to the Apache Software Foundation (ASF) under one or more
// contributor license agreements. See the NOTICE file distributed with this
// work for additional information regarding copyright ownership. The ASF
// licenses this file to You under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.
//
// Modified and ported to Go for Depsilo by Depsilo Contributors in 2026.

package packagepolicy

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf16"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// This implementation is a Go port of Apache Maven 3.9.16's
// org.apache.maven.artifact.versioning.ComparableVersion (Apache-2.0):
// https://maven.apache.org/ref/3.9.16/xref/org/apache/maven/artifact/versioning/ComparableVersion.html
// Its conformance corpus is Apache Maven's pinned ComparableVersionTest:
// https://github.com/apache/maven/blob/maven-3.9.16/maven-artifact/src/test/java/org/apache/maven/artifact/versioning/ComparableVersionTest.java
//
// It deliberately follows that pinned implementation, including its nested
// list representation around hyphens and digit/character transitions. Do not
// merge rules from a different Maven release into this parser: version-order
// changes are policy changes and require a dialect revision.
// See the repository NOTICE and LICENSES/Apache-2.0.txt files.

type mavenDialect struct{}

func (mavenDialect) NormalizePackageName(name string) (string, error) {
	if err := validateMavenPackageName(name); err != nil {
		return "", fmt.Errorf("Maven %w", err)
	}
	// Maven coordinates are case-sensitive repository path components.
	return name, nil
}

func validateMavenPackageName(name string) error {
	if err := validatePackageNameInput(name); err != nil {
		return err
	}
	if strings.Count(name, ":") != 1 {
		return fmt.Errorf("package coordinate must be exactly groupId:artifactId")
	}
	groupID, artifactID, _ := strings.Cut(name, ":")
	if groupID == "" || artifactID == "" {
		return fmt.Errorf("package coordinate must have non-empty groupId and artifactId")
	}
	for _, segment := range strings.Split(groupID, ".") {
		if segment == "" {
			return fmt.Errorf("groupId contains an empty dot-separated segment")
		}
		if err := validateMavenID(segment); err != nil {
			return fmt.Errorf("invalid groupId segment %q: %w", segment, err)
		}
	}
	if err := validateMavenID(artifactID); err != nil {
		return fmt.Errorf("invalid artifactId: %w", err)
	}
	return nil
}

func validateMavenID(value string) error {
	for index := range len(value) {
		character := value[index]
		if isASCIIAlphanumeric(character) || character == '_' || character == '-' || character == '.' {
			continue
		}
		return fmt.Errorf("contains invalid character %q", character)
	}
	return nil
}

func (mavenDialect) normalizePackagePrefix(prefix string) (string, error) {
	if err := validatePackageNameInput(prefix); err != nil {
		return "", err
	}
	if err := validateMavenPackageName(prefix); err == nil {
		return prefix, nil
	}
	if strings.Count(prefix, ":") > 1 {
		return "", fmt.Errorf("Maven package prefix contains more than one coordinate separator")
	}
	candidate := prefix + "x"
	if !strings.ContainsRune(prefix, ':') {
		candidate += ":x"
	}
	if err := validateMavenPackageName(candidate); err != nil {
		return "", fmt.Errorf("invalid Maven package prefix: %w", err)
	}
	return prefix, nil
}

func (mavenDialect) normalizePackageGlob(pattern string) (string, error) {
	if err := validatePackageGlobInput(pattern); err != nil {
		return "", fmt.Errorf("invalid Maven package glob: %w", err)
	}
	if err := validateGlobCharacters(pattern, "_.-:", true); err != nil {
		return "", fmt.Errorf("invalid Maven package glob: %w", err)
	}
	if strings.Count(pattern, ":") > 1 {
		return "", fmt.Errorf("Maven package glob contains more than one coordinate separator")
	}
	if !hasGlobMetacharacter(pattern) {
		return mavenDialect{}.NormalizePackageName(pattern)
	}
	return pattern, nil
}

func (mavenDialect) ValidateVersion(version string) error {
	_, err := parseMavenVersion(version)
	return err
}

func (mavenDialect) CompareVersions(a, b string) (int, error) {
	left, err := parseMavenVersion(a)
	if err != nil {
		return 0, fmt.Errorf("parse left version: %w", err)
	}
	right, err := parseMavenVersion(b)
	if err != nil {
		return 0, fmt.Errorf("parse right version: %w", err)
	}
	return signOf(left.items.compareTo(right.items)), nil
}

// ComparableVersion is retained for exact-version equivalence and diagnostics,
// but Maven ranges are deliberately disabled. Apache's unrestricted ordering
// is non-transitive for real version triples (MNG-6568), so it cannot safely
// decide an allow/deny interval.
func (mavenDialect) SupportsRanges() bool { return false }

func (mavenDialect) canonicalVersion(value string) (string, error) {
	if _, err := parseMavenVersion(value); err != nil {
		return "", err
	}
	// ComparableVersion.getCanonical() renders its internal item tree for
	// diagnostics; Maven does not guarantee that reparsing that string compares
	// equal to the original (for example, 0alpha1 versus alpha-1). Retain the
	// validated tokenization and apply only Maven's own case folding.
	return lowerMavenVersion(value), nil
}

type mavenVersion struct {
	items *mavenListItem
}

type mavenItemKind uint8

const (
	mavenNumericItemKind mavenItemKind = iota
	mavenStringItemKind
	mavenListItemKind
)

type mavenItem interface {
	kind() mavenItemKind
	isNull() bool
	compareTo(mavenItem) int
	canonical() string
}

// mavenNumericItem stores the normalized decimal representation instead of a
// machine integer. ComparableVersion selects int, long, or BigInteger by
// length; comparing normalized digit count and then bytes produces the same
// ordering without an overflow boundary.
type mavenNumericItem struct {
	digits string
}

func (mavenNumericItem) kind() mavenItemKind { return mavenNumericItemKind }

func (item mavenNumericItem) isNull() bool { return item.digits == "0" }

func (item mavenNumericItem) compareTo(other mavenItem) int {
	if other == nil {
		if item.isNull() {
			return 0
		}
		return 1
	}

	switch other.kind() {
	case mavenNumericItemKind:
		return compareMavenDecimal(item.digits, other.(mavenNumericItem).digits)
	case mavenStringItemKind, mavenListItemKind:
		return 1
	default:
		panic("invalid Maven version item")
	}
}

func (item mavenNumericItem) canonical() string { return item.digits }

type mavenStringItem struct {
	value string
}

func newMavenStringItem(value string, followedByDigit bool) mavenStringItem {
	if followedByDigit && len(value) == 1 {
		switch value[0] {
		case 'a':
			value = "alpha"
		case 'b':
			value = "beta"
		case 'm':
			value = "milestone"
		}
	}

	switch value {
	case "ga", "final", "release":
		value = ""
	case "cr":
		value = "rc"
	}
	return mavenStringItem{value: value}
}

func (mavenStringItem) kind() mavenItemKind { return mavenStringItemKind }

func (item mavenStringItem) isNull() bool { return item.value == "" }

func (item mavenStringItem) compareTo(other mavenItem) int {
	if other == nil {
		return compareMavenQualifiers(item.value, "")
	}

	switch other.kind() {
	case mavenNumericItemKind:
		return -1
	case mavenStringItemKind:
		return compareMavenQualifiers(item.value, other.(mavenStringItem).value)
	case mavenListItemKind:
		return -1
	default:
		panic("invalid Maven version item")
	}
}

func (item mavenStringItem) canonical() string { return item.value }

type mavenListItem struct {
	items []mavenItem
}

func (*mavenListItem) kind() mavenItemKind { return mavenListItemKind }

func (item *mavenListItem) isNull() bool { return len(item.items) == 0 }

func (item *mavenListItem) normalize() {
	for index := len(item.items) - 1; index >= 0; index-- {
		last := item.items[index]
		if last.isNull() {
			item.items = append(item.items[:index], item.items[index+1:]...)
			continue
		}
		if _, nested := last.(*mavenListItem); !nested {
			break
		}
	}
}

func (item *mavenListItem) compareTo(other mavenItem) int {
	if other == nil {
		if item.isNull() {
			return 0
		}
		// MNG-6964: compare every nested item with null, not only the first.
		for _, child := range item.items {
			if comparison := child.compareTo(nil); comparison != 0 {
				return comparison
			}
		}
		return 0
	}

	switch other.kind() {
	case mavenNumericItemKind:
		return -1
	case mavenStringItemKind:
		return 1
	case mavenListItemKind:
		right := other.(*mavenListItem)
		count := len(item.items)
		if len(right.items) > count {
			count = len(right.items)
		}
		for index := 0; index < count; index++ {
			var comparison int
			switch {
			case index >= len(item.items):
				comparison = -right.items[index].compareTo(nil)
			case index >= len(right.items):
				comparison = item.items[index].compareTo(nil)
			default:
				comparison = item.items[index].compareTo(right.items[index])
			}
			if comparison != 0 {
				return comparison
			}
		}
		return 0
	default:
		panic("invalid Maven version item")
	}
}

func (item *mavenListItem) canonical() string {
	var canonical strings.Builder
	for _, child := range item.items {
		if canonical.Len() > 0 {
			if _, nested := child.(*mavenListItem); nested {
				canonical.WriteByte('-')
			} else {
				canonical.WriteByte('.')
			}
		}
		canonical.WriteString(child.canonical())
	}
	return canonical.String()
}

func parseMavenVersion(value string) (mavenVersion, error) {
	if value == "" || strings.TrimSpace(value) != value ||
		strings.ContainsFunc(value, unicode.IsControl) || strings.ContainsFunc(value, unicode.IsSpace) {
		return mavenVersion{}, fmt.Errorf("Maven version must be non-empty and contain no whitespace or control characters")
	}
	// ComparableVersion lower-cases once with Locale.ENGLISH before tokenizing.
	characters := []rune(lowerMavenVersion(value))
	root := &mavenListItem{}
	list := root
	stack := []*mavenListItem{root}
	isDigit := false
	startIndex := 0

	for index, character := range characters {
		switch {
		case character == '.':
			if index == startIndex {
				list.items = append(list.items, mavenNumericItem{digits: "0"})
			} else {
				list.items = append(list.items, parseMavenItem(isDigit, string(characters[startIndex:index])))
			}
			startIndex = index + 1

		case character == '-':
			if index == startIndex {
				list.items = append(list.items, mavenNumericItem{digits: "0"})
			} else {
				list.items = append(list.items, parseMavenItem(isDigit, string(characters[startIndex:index])))
			}
			startIndex = index + 1

			nested := &mavenListItem{}
			list.items = append(list.items, nested)
			list = nested
			stack = append(stack, list)

		case isMavenDigit(character):
			if !isDigit && index > startIndex {
				// MNG-7644: treat .X as -X for every string qualifier X.
				if len(list.items) > 0 {
					nested := &mavenListItem{}
					list.items = append(list.items, nested)
					list = nested
					stack = append(stack, list)
				}

				list.items = append(list.items, newMavenStringItem(string(characters[startIndex:index]), true))
				startIndex = index

				nested := &mavenListItem{}
				list.items = append(list.items, nested)
				list = nested
				stack = append(stack, list)
			}
			isDigit = true

		default:
			if isDigit && index > startIndex {
				list.items = append(list.items, parseMavenItem(true, string(characters[startIndex:index])))
				startIndex = index

				nested := &mavenListItem{}
				list.items = append(list.items, nested)
				list = nested
				stack = append(stack, list)
			}
			isDigit = false
		}
	}

	if len(characters) > startIndex {
		// MNG-7644: treat a final .X as -X for every string qualifier X.
		if !isDigit && len(list.items) > 0 {
			nested := &mavenListItem{}
			list.items = append(list.items, nested)
			list = nested
			stack = append(stack, list)
		}
		list.items = append(list.items, parseMavenItem(isDigit, string(characters[startIndex:])))
	}

	for index := len(stack) - 1; index >= 0; index-- {
		stack[index].normalize()
	}
	return mavenVersion{items: root}, nil
}

func lowerMavenVersion(value string) string {
	// ComparableVersion uses String.toLowerCase(Locale.ENGLISH), whose full
	// Unicode mapping can expand one rune (for example, İ -> i + combining dot).
	return cases.Lower(language.English).String(value)
}

func parseMavenItem(isDigit bool, value string) mavenItem {
	if isDigit {
		return mavenNumericItem{digits: normalizeMavenDecimal(value)}
	}
	return newMavenStringItem(value, false)
}

func normalizeMavenDecimal(value string) string {
	var normalized strings.Builder
	leadingZero := true
	for _, character := range value {
		digit, ok := mavenDigitValue(character)
		if !ok {
			panic("non-digit in Maven numeric item")
		}
		if leadingZero && digit == 0 {
			continue
		}
		leadingZero = false
		normalized.WriteByte('0' + byte(digit))
	}
	if normalized.Len() == 0 {
		return "0"
	}
	return normalized.String()
}

func compareMavenDecimal(left, right string) int {
	if len(left) != len(right) {
		return signOf(len(left) - len(right))
	}
	return signOf(strings.Compare(left, right))
}

var mavenQualifierOrder = map[string]int{
	"alpha":     0,
	"beta":      1,
	"milestone": 2,
	"rc":        3,
	"snapshot":  4,
	"":          5,
	"sp":        6,
}

func compareMavenQualifiers(left, right string) int {
	leftOrder, leftKnown := mavenQualifierOrder[left]
	rightOrder, rightKnown := mavenQualifierOrder[right]
	switch {
	case leftKnown && rightKnown:
		return signOf(leftOrder - rightOrder)
	case leftKnown:
		return -1
	case rightKnown:
		return 1
	default:
		return compareMavenStrings(left, right)
	}
}

// ComparableVersion 3.9.16 tokenizes with Character.isDigit(char). The char
// overload sees BMP decimal digits but not supplementary-plane surrogate
// pairs; Integer/Long/BigInteger then canonicalize those digits to ASCII.
func isMavenDigit(character rune) bool {
	_, ok := mavenDigitValue(character)
	return ok
}

func mavenDigitValue(character rune) (int, bool) {
	if character > 0xffff {
		return 0, false
	}
	for _, digitRange := range unicode.Digit.R16 {
		value := uint16(character)
		if value < digitRange.Lo || value > digitRange.Hi {
			continue
		}
		offset := value - digitRange.Lo
		if offset%digitRange.Stride != 0 {
			return 0, false
		}
		return int(offset/digitRange.Stride) % 10, true
	}
	return 0, false
}

// java.lang.String.compareTo compares UTF-16 code units. Using UTF-16 here
// preserves its ordering for unknown qualifiers outside the BMP as well.
func compareMavenStrings(left, right string) int {
	leftUnits := utf16.Encode([]rune(left))
	rightUnits := utf16.Encode([]rune(right))
	limit := len(leftUnits)
	if len(rightUnits) < limit {
		limit = len(rightUnits)
	}
	for index := 0; index < limit; index++ {
		if leftUnits[index] < rightUnits[index] {
			return -1
		}
		if leftUnits[index] > rightUnits[index] {
			return 1
		}
	}
	return signOf(len(leftUnits) - len(rightUnits))
}
