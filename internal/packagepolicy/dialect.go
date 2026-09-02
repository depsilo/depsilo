// Package packagepolicy owns ecosystem-specific package identity and version
// semantics. Callers select a dialect before normalizing or comparing values;
// there is intentionally no cross-ecosystem fallback comparator.
package packagepolicy

import (
	"fmt"
	"path"
	"strings"
	"unicode"

	"depsilo/internal/gomoduleidentity"
)

// PackagePolicyDialect defines the package identity and version semantics for
// one ecosystem.
type PackagePolicyDialect interface {
	NormalizePackageName(name string) (string, error)
	ValidateVersion(version string) error
	CompareVersions(a, b string) (int, error)
	SupportsRanges() bool
}

type semverDialect struct {
	name            string
	coreMax         string
	normalizeName   func(string) string
	validateName    func(string) error
	normalizePrefix func(string) (string, error)
	normalizeGlob   func(string) (string, error)
}

// DialectFor returns the policy dialect for a concrete ecosystem.
func DialectFor(ecosystem string) (PackagePolicyDialect, error) {
	return dialectForRevision1(ecosystem)
}

// dialectForRevision1 is the frozen type registry persisted by schema v3.
// Future dialect revisions must add a sibling registry instead of editing the
// dispatch used by PrepareRuleRevision1.
func dialectForRevision1(ecosystem string) (PackagePolicyDialect, error) {
	switch strings.ToLower(strings.TrimSpace(ecosystem)) {
	case "npm":
		return semverDialect{
			name: "npm", coreMax: "9007199254740991",
			normalizeName: identity, validateName: validateNPMPackageName,
			normalizePrefix: normalizeNPMPackagePrefix, normalizeGlob: normalizeNPMPackageGlob,
		}, nil
	case "cargo":
		return semverDialect{
			name: "cargo", coreMax: "18446744073709551615", normalizeName: identity,
			validateName: validateCargoPackageName, normalizeGlob: normalizeCargoPackageGlob,
		}, nil
	case "pypi":
		return pep440Dialect{}, nil
	case "maven":
		return mavenDialect{}, nil
	case "apt":
		return debianDialect{}, nil
	case "nuget":
		return nugetDialect{exactDialect: exactDialect{
			normalizeName:   lowerASCII,
			validateName:    validateNuGetPackageName,
			normalizePrefix: normalizeNuGetPackagePrefix,
			normalizeGlob:   normalizeNuGetPackageGlob,
		}}, nil
	case "composer":
		return exactDialect{
			normalizeName:   strings.ToLower,
			validateName:    validateComposerPackageName,
			normalizePrefix: normalizeComposerPackagePrefix,
			normalizeGlob:   normalizeComposerPackageGlob,
		}, nil
	case "conda":
		return exactDialect{
			normalizeName:   normalizeCondaPackageName,
			validateName:    validateCondaPackageName,
			normalizePrefix: normalizeCondaPackagePrefix,
			normalizeGlob:   normalizeCondaPackageGlob,
		}, nil
	case "alpine":
		return exactDialect{
			normalizeName:   identity,
			validateName:    validateAlpinePackageName,
			normalizePrefix: normalizeAlpinePackagePrefix,
			normalizeGlob:   normalizeAlpinePackageGlob,
		}, nil
	case "huggingface":
		return exactDialect{
			normalizeName:   strings.ToLower,
			validateName:    validateHuggingFacePackageName,
			normalizePrefix: normalizeHuggingFacePackagePrefix,
			normalizeGlob:   normalizeHuggingFacePackageGlob,
		}, nil
	case "docker":
		return exactDialect{
			normalizeName:   identity,
			validateName:    validateDockerRemoteName,
			normalizePrefix: normalizeDockerPackagePrefix,
			normalizeGlob:   normalizeDockerPackageGlob,
		}, nil
	case "go":
		return exactDialect{
			normalizeName:   identity,
			validateName:    gomoduleidentity.ValidatePath,
			normalizePrefix: normalizeGoModulePrefix,
			normalizeGlob:   normalizeGoModuleGlob,
		}, nil
	case "cran":
		return cranDialect{}, nil
	case "rubygems", "helm":
		return exactDialect{normalizeName: identity}, nil
	default:
		return nil, fmt.Errorf("unsupported package policy ecosystem %q", ecosystem)
	}
}

func (d semverDialect) NormalizePackageName(name string) (string, error) {
	if err := validatePackageNameInput(name); err != nil {
		return "", err
	}
	if d.validateName != nil {
		if err := d.validateName(name); err != nil {
			return "", err
		}
	}
	return d.normalizeName(name), nil
}

func (d semverDialect) normalizePackagePrefix(prefix string) (string, error) {
	if d.normalizePrefix != nil {
		return d.normalizePrefix(prefix)
	}
	return d.NormalizePackageName(prefix)
}

func (d semverDialect) normalizePackageGlob(pattern string) (string, error) {
	if d.normalizeGlob != nil {
		return d.normalizeGlob(pattern)
	}
	if err := validatePackageGlobInput(pattern); err != nil {
		return "", err
	}
	return d.normalizeName(pattern), nil
}

func (d semverDialect) ValidateVersion(version string) error {
	_, err := d.parse(version)
	return err
}

func (d semverDialect) CompareVersions(a, b string) (int, error) {
	left, err := d.parse(a)
	if err != nil {
		return 0, fmt.Errorf("parse left version: %w", err)
	}
	right, err := d.parse(b)
	if err != nil {
		return 0, fmt.Errorf("parse right version: %w", err)
	}
	return left.Compare(right), nil
}

func (semverDialect) SupportsRanges() bool { return true }

func (d semverDialect) canonicalVersion(value string) (string, error) {
	version, err := d.parse(value)
	if err != nil {
		return "", err
	}
	return version.canonical(), nil
}

func (d semverDialect) parse(value string) (strictSemVer, error) {
	return parseStrictSemVer(value, d.name, d.coreMax)
}

func identity(value string) string { return value }

func normalizePythonName(value string) string {
	var normalized strings.Builder
	normalized.Grow(len(value))
	separator := false
	for _, character := range strings.ToLower(value) {
		if character == '-' || character == '_' || character == '.' {
			separator = true
			continue
		}
		if separator {
			normalized.WriteByte('-')
			separator = false
		}
		normalized.WriteRune(character)
	}
	if separator {
		normalized.WriteByte('-')
	}
	return normalized.String()
}

type exactDialect struct {
	normalizeName   func(string) string
	validateName    func(string) error
	normalizePrefix func(string) (string, error)
	normalizeGlob   func(string) (string, error)
}

func (d exactDialect) NormalizePackageName(name string) (string, error) {
	if err := validatePackageNameInput(name); err != nil {
		return "", err
	}
	if d.validateName != nil {
		if err := d.validateName(name); err != nil {
			return "", err
		}
	}
	return d.normalizeName(name), nil
}

func (d exactDialect) normalizePackagePrefix(prefix string) (string, error) {
	if d.normalizePrefix != nil {
		return d.normalizePrefix(prefix)
	}
	return d.NormalizePackageName(prefix)
}

func (d exactDialect) normalizePackageGlob(pattern string) (string, error) {
	if d.normalizeGlob != nil {
		return d.normalizeGlob(pattern)
	}
	if err := validatePackageGlobInput(pattern); err != nil {
		return "", err
	}
	return d.normalizeName(pattern), nil
}

func (exactDialect) ValidateVersion(version string) error {
	if version == "" || strings.TrimSpace(version) != version ||
		strings.ContainsFunc(version, unicode.IsControl) || strings.ContainsFunc(version, unicode.IsSpace) {
		return fmt.Errorf("exact version must be non-empty and contain no whitespace or control characters")
	}
	return nil
}

func (d exactDialect) CompareVersions(a, b string) (int, error) {
	if err := d.ValidateVersion(a); err != nil {
		return 0, fmt.Errorf("validate left version: %w", err)
	}
	if err := d.ValidateVersion(b); err != nil {
		return 0, fmt.Errorf("validate right version: %w", err)
	}
	return strings.Compare(a, b), nil
}

func (exactDialect) SupportsRanges() bool { return false }

func (d exactDialect) canonicalVersion(value string) (string, error) {
	if err := d.ValidateVersion(value); err != nil {
		return "", err
	}
	return value, nil
}

func validatePackageNameInput(name string) error {
	if name == "" || strings.TrimSpace(name) != name ||
		strings.ContainsFunc(name, unicode.IsControl) || strings.ContainsFunc(name, unicode.IsSpace) {
		return fmt.Errorf("package name must be non-empty and contain no whitespace or control characters")
	}
	return nil
}

func validateCargoPackageName(name string) error {
	for _, character := range name {
		if isCargoPackageNameCharacter(character) {
			continue
		}
		return fmt.Errorf("Cargo package name contains invalid character %q", character)
	}
	return nil
}

// Cargo's manifest grammar accepts Unicode alphanumeric characters plus '-'
// and '_' in every position. The stricter Rust-identifier and crates.io
// publication rules belong to cargo-new or a particular registry; applying
// either here would reject valid packages from private Cargo registries.
func isCargoPackageNameCharacter(character rune) bool {
	return unicode.IsLetter(character) || unicode.IsNumber(character) ||
		unicode.In(character, unicode.Other_Alphabetic) || character == '-' || character == '_'
}

// NormalizePackageGlob validates and normalizes a quarantine package glob
// without pretending that its metacharacters form a concrete package name.
// Requests are still passed through NormalizePackageName before matching, so
// a broad glob can never make an invalid concrete identity valid.
func NormalizePackageGlob(dialect PackagePolicyDialect, pattern string) (string, error) {
	if normalizer, ok := dialect.(packagePatternNormalizer); ok {
		return normalizer.normalizePackageGlob(pattern)
	}
	if err := validatePackageGlobInput(pattern); err != nil {
		return "", err
	}

	// PyPI concrete validation requires an alphanumeric final character, while
	// a glob commonly ends in '*' or in a normalized separator followed by '*'.
	if _, ok := dialect.(pep440Dialect); ok {
		if err := validateGlobCharacters(pattern, "._-", true); err != nil {
			return "", fmt.Errorf("invalid PyPI package glob: %w", err)
		}
		return normalizePythonName(pattern), nil
	}
	return dialect.NormalizePackageName(pattern)
}

type packagePatternNormalizer interface {
	normalizePackagePrefix(string) (string, error)
	normalizePackageGlob(string) (string, error)
}

func normalizeDialectPackagePrefix(dialect PackagePolicyDialect, prefix string) (string, error) {
	if err := validatePackageNameInput(prefix); err != nil {
		return "", err
	}
	if _, ok := dialect.(pep440Dialect); ok {
		return normalizePyPIPackagePrefix(prefix)
	}
	if _, ok := dialect.(debianDialect); ok {
		return normalizePackagePrefixWithFiller(dialect, prefix)
	}
	if normalizer, ok := dialect.(packagePatternNormalizer); ok {
		return normalizer.normalizePackagePrefix(prefix)
	}
	return dialect.NormalizePackageName(prefix)
}

func normalizePackagePrefixWithFiller(dialect PackagePolicyDialect, prefix string) (string, error) {
	if normalized, err := dialect.NormalizePackageName(prefix); err == nil {
		return normalized, nil
	}
	const filler = "x"
	normalized, err := dialect.NormalizePackageName(prefix + filler)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(normalized, filler) {
		return "", fmt.Errorf("package prefix normalization did not preserve its safe filler")
	}
	return strings.TrimSuffix(normalized, filler), nil
}

func normalizePyPIPackagePrefix(prefix string) (string, error) {
	if !isASCIIAlphanumeric(prefix[0]) {
		return "", fmt.Errorf("PyPI package prefix must start with an ASCII letter or digit")
	}
	for index := range len(prefix) {
		character := prefix[index]
		if isASCIIAlphanumeric(character) || strings.ContainsRune("._-", rune(character)) {
			continue
		}
		return "", fmt.Errorf("PyPI package prefix contains invalid character %q", character)
	}
	return normalizePythonName(prefix), nil
}

func normalizeCargoPackageGlob(pattern string) (string, error) {
	if err := validatePackageGlobInput(pattern); err != nil {
		return "", fmt.Errorf("invalid Cargo package glob: %w", err)
	}
	if strings.ContainsRune(pattern, '/') {
		return "", fmt.Errorf("invalid Cargo package glob %q: crate names cannot contain '/'", pattern)
	}
	for _, character := range pattern {
		if isCargoPackageNameCharacter(character) ||
			strings.ContainsRune("*?[]\\^", character) {
			continue
		}
		return "", fmt.Errorf("Cargo package glob contains invalid character %q", character)
	}
	return pattern, nil
}

func normalizeGoModulePrefix(prefix string) (string, error) {
	if err := validatePackageNameInput(prefix); err != nil {
		return "", err
	}
	if err := gomoduleidentity.ValidatePrefix(prefix); err != nil {
		return "", err
	}
	return prefix, nil
}

func normalizeGoModuleGlob(pattern string) (string, error) {
	if err := validatePackageGlobInput(pattern); err != nil {
		return "", fmt.Errorf("invalid Go module package glob: %w", err)
	}
	if err := gomoduleidentity.ValidateGlob(pattern); err != nil {
		return "", err
	}
	return pattern, nil
}

func validateNPMPackageName(name string) error {
	if len(name) > 214 {
		return fmt.Errorf("npm package name exceeds 214 characters")
	}
	if strings.HasPrefix(name, "@") {
		parts := strings.Split(name, "/")
		if len(parts) != 2 || len(parts[0]) == 1 || parts[1] == "" {
			return fmt.Errorf("scoped npm package name must be @scope/name")
		}
		if err := validateNPMURLSafeComponent(parts[0][1:]); err != nil {
			return fmt.Errorf("invalid npm scope: %w", err)
		}
		if parts[1][0] == '.' {
			return fmt.Errorf("scoped npm package name cannot start with '.'")
		}
		if err := validateNPMURLSafeComponent(parts[1]); err != nil {
			return fmt.Errorf("invalid npm package: %w", err)
		}
		return nil
	}
	if strings.ContainsRune(name, '/') {
		return fmt.Errorf("unscoped npm package name cannot contain '/'")
	}
	if strings.ContainsRune("._-", rune(name[0])) {
		return fmt.Errorf("unscoped npm package name cannot start with %q", name[0])
	}
	switch strings.ToLower(name) {
	case "node_modules", "favicon.ico":
		return fmt.Errorf("%q is not a valid npm package name", name)
	}
	return validateNPMURLSafeComponent(name)
}

func validateNPMURLSafeComponent(value string) error {
	if value == "" {
		return fmt.Errorf("must be non-empty")
	}
	for index := range len(value) {
		character := value[index]
		if isASCIIAlphanumeric(character) || strings.ContainsRune("._~!()*'-", rune(character)) {
			continue
		}
		return fmt.Errorf("contains non-URL-safe character %q", character)
	}
	return nil
}

func normalizeNPMPackagePrefix(prefix string) (string, error) {
	if err := validatePackageNameInput(prefix); err != nil {
		return "", err
	}
	if err := validateNPMPackageName(prefix); err == nil {
		return prefix, nil
	}
	candidate := prefix + "x"
	if strings.HasPrefix(prefix, "@") && !strings.ContainsRune(prefix, '/') {
		candidate += "/x"
	}
	if err := validateNPMPackageName(candidate); err != nil {
		return "", err
	}
	return prefix, nil
}

func normalizeNPMPackageGlob(pattern string) (string, error) {
	if err := validatePackageGlobInput(pattern); err != nil {
		return "", fmt.Errorf("invalid npm package glob: %w", err)
	}
	parts := strings.Split(pattern, "/")
	switch {
	case strings.HasPrefix(pattern, "@"):
		if len(parts) != 2 || len(parts[0]) == 1 || parts[1] == "" {
			return "", fmt.Errorf("scoped npm package glob must be @scope/pattern")
		}
		if parts[1][0] == '.' {
			return "", fmt.Errorf("scoped npm package glob cannot start its package pattern with '.'")
		}
		for _, part := range []string{parts[0][1:], parts[1]} {
			if err := validateGlobCharacters(part, "._~!()'-", true); err != nil {
				return "", fmt.Errorf("invalid scoped npm package glob: %w", err)
			}
		}
	case len(parts) != 1:
		return "", fmt.Errorf("unscoped npm package glob cannot contain '/'")
	default:
		if strings.ContainsRune("._-", rune(pattern[0])) {
			return "", fmt.Errorf("unscoped npm package glob cannot start with %q", pattern[0])
		}
		if err := validateGlobCharacters(pattern, "._~!()'-", true); err != nil {
			return "", fmt.Errorf("invalid npm package glob: %w", err)
		}
	}
	if !hasGlobMetacharacter(pattern) {
		if err := validateNPMPackageName(pattern); err != nil {
			return "", fmt.Errorf("invalid npm package glob: %w", err)
		}
	}
	return pattern, nil
}

func validateComposerPackageName(name string) error {
	parts, err := exactCoordinateParts(name, 2, "Composer package name")
	if err != nil {
		return err
	}
	if err := validateComposerNamePart(parts[0], 1); err != nil {
		return fmt.Errorf("invalid Composer vendor: %w", err)
	}
	if err := validateComposerNamePart(parts[1], 2); err != nil {
		return fmt.Errorf("invalid Composer package: %w", err)
	}
	for _, part := range parts {
		if isWindowsReservedName(part) {
			return fmt.Errorf("Composer package coordinate uses Windows reserved name %q", part)
		}
	}
	if strings.HasSuffix(strings.ToLower(parts[1]), ".json") {
		return fmt.Errorf("Composer package name cannot end in .json")
	}
	return nil
}

func isWindowsReservedName(value string) bool {
	base := strings.ToLower(value)
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch base {
	case "con", "prn", "aux", "nul":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) {
		return base[3] >= '1' && base[3] <= '9'
	}
	return false
}

func validateComposerNamePart(value string, maxHyphenRun int) error {
	if !isASCIIAlphanumeric(value[0]) || !isASCIIAlphanumeric(value[len(value)-1]) {
		return fmt.Errorf("must start and end with an ASCII letter or digit")
	}
	for index := 0; index < len(value); {
		if isASCIIAlphanumeric(value[index]) {
			index++
			continue
		}
		separator := value[index]
		if separator != '.' && separator != '_' && separator != '-' {
			return fmt.Errorf("contains invalid character %q", separator)
		}
		runStart := index
		for index < len(value) && value[index] == separator {
			index++
		}
		if separator != '-' && index-runStart > 1 || separator == '-' && index-runStart > maxHyphenRun {
			return fmt.Errorf("contains an invalid separator run")
		}
		if index == len(value) || !isASCIIAlphanumeric(value[index]) {
			return fmt.Errorf("separator must be followed by an ASCII letter or digit")
		}
	}
	return nil
}

func normalizeComposerPackagePrefix(prefix string) (string, error) {
	if err := validateCompletedCoordinatePrefix(prefix, 2, validateComposerPackageName); err != nil {
		return "", err
	}
	return strings.ToLower(prefix), nil
}

func normalizeComposerPackageGlob(pattern string) (string, error) {
	parts, err := validateStructuredPackageGlob(pattern, 2, "Composer", "._-", true)
	if err != nil {
		return "", err
	}
	for index, part := range parts {
		if hasGlobMetacharacter(part) {
			continue
		}
		maxHyphenRun := 1
		if index == 1 {
			maxHyphenRun = 2
		}
		if err := validateComposerNamePart(part, maxHyphenRun); err != nil {
			return "", fmt.Errorf("invalid Composer package glob segment %q: %w", part, err)
		}
		if isWindowsReservedName(part) {
			return "", fmt.Errorf("Composer package glob uses Windows reserved name %q", part)
		}
		if index == 1 && strings.HasSuffix(strings.ToLower(part), ".json") {
			return "", fmt.Errorf("Composer package glob cannot end in .json")
		}
	}
	return strings.ToLower(pattern), nil
}

func normalizeCondaPackageName(name string) string {
	slash := strings.LastIndexByte(name, '/')
	return name[:slash+1] + strings.ToLower(name[slash+1:])
}

func validateCondaPackageName(name string) error {
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return fmt.Errorf("Conda package name must contain a channel path and package name")
	}
	for _, channelPart := range parts[:len(parts)-1] {
		if err := validateSimpleASCIIName(channelPart, "._-", false, true); err != nil {
			return fmt.Errorf("invalid Conda channel segment %q: %w", channelPart, err)
		}
	}
	if err := validateCondaNamePart(parts[len(parts)-1]); err != nil {
		return fmt.Errorf("invalid Conda package: %w", err)
	}
	return nil
}

func validateCondaNamePart(value string) error {
	if value == "" || value == "." || value == ".." {
		return fmt.Errorf("must be a non-empty safe path segment")
	}
	if !isASCIIAlphanumeric(value[0]) && value[0] != '_' {
		return fmt.Errorf("must start with an ASCII letter, digit, or underscore")
	}
	if !isASCIIAlphanumeric(value[len(value)-1]) {
		return fmt.Errorf("must end with an ASCII letter or digit")
	}
	for index := range len(value) {
		character := value[index]
		if isASCIIAlphanumeric(character) || strings.ContainsRune("._-", rune(character)) {
			continue
		}
		return fmt.Errorf("contains invalid character %q", character)
	}
	return nil
}

func normalizeCondaPackagePrefix(prefix string) (string, error) {
	if err := validatePackageNameInput(prefix); err != nil {
		return "", err
	}
	if err := validateCondaPackageName(prefix); err == nil {
		return normalizeCondaPackageName(prefix), nil
	}
	slash := strings.LastIndexByte(prefix, '/')
	if slash < 0 {
		if err := validateSimpleASCIIName(prefix+"x", "._-", false, true); err != nil {
			return "", fmt.Errorf("invalid Conda channel prefix: %w", err)
		}
		return prefix, nil
	}
	for _, channelPart := range strings.Split(prefix[:slash], "/") {
		if err := validateSimpleASCIIName(channelPart, "._-", false, true); err != nil {
			return "", fmt.Errorf("invalid Conda channel segment %q: %w", channelPart, err)
		}
	}
	packagePrefix := prefix[slash+1:]
	if packagePrefix == "" {
		return prefix, nil
	}
	if err := validateCondaNamePart(packagePrefix + "x"); err != nil {
		return "", fmt.Errorf("invalid Conda package prefix: %w", err)
	}
	if slash >= 0 {
		return prefix[:slash+1] + strings.ToLower(prefix[slash+1:]), nil
	}
	return prefix, nil
}

func normalizeCondaPackageGlob(pattern string) (string, error) {
	if err := validatePackageGlobInput(pattern); err != nil {
		return "", fmt.Errorf("invalid Conda package glob: %w", err)
	}
	parts := strings.Split(pattern, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("Conda package glob must contain a channel path and package pattern")
	}
	for _, channelPart := range parts[:len(parts)-1] {
		if err := validateGlobCharacters(channelPart, "._-", true); err != nil {
			return "", fmt.Errorf("invalid Conda channel glob segment %q: %w", channelPart, err)
		}
		if !hasGlobMetacharacter(channelPart) {
			if err := validateSimpleASCIIName(channelPart, "._-", false, true); err != nil {
				return "", fmt.Errorf("invalid Conda channel glob segment %q: %w", channelPart, err)
			}
		}
	}
	packagePattern := parts[len(parts)-1]
	if err := validateGlobCharacters(packagePattern, "._-", true); err != nil {
		return "", fmt.Errorf("invalid Conda package glob: %w", err)
	}
	if !hasGlobMetacharacter(packagePattern) {
		if err := validateCondaNamePart(packagePattern); err != nil {
			return "", fmt.Errorf("invalid Conda package glob: %w", err)
		}
	}
	parts[len(parts)-1] = strings.ToLower(packagePattern)
	return strings.Join(parts, "/"), nil
}

func validateAlpinePackageName(name string) error {
	parts, err := exactCoordinateParts(name, 4, "Alpine package name")
	if err != nil {
		return err
	}
	for _, part := range parts {
		if err := validateSimpleASCIIName(part, "._+-", true, true); err != nil {
			return fmt.Errorf("invalid Alpine package coordinate segment %q: %w", part, err)
		}
	}
	return nil
}

func normalizeAlpinePackagePrefix(prefix string) (string, error) {
	if err := validateCompletedCoordinatePrefix(prefix, 4, validateAlpinePackageName); err != nil {
		return "", err
	}
	return prefix, nil
}

func normalizeAlpinePackageGlob(pattern string) (string, error) {
	parts, err := validateStructuredPackageGlob(pattern, 4, "Alpine", "._+-", true)
	if err != nil {
		return "", err
	}
	for _, part := range parts {
		if hasGlobMetacharacter(part) {
			continue
		}
		if err := validateSimpleASCIIName(part, "._+-", true, true); err != nil {
			return "", fmt.Errorf("invalid Alpine package glob segment %q: %w", part, err)
		}
	}
	return pattern, nil
}

func validateHuggingFacePackageName(name string) error {
	parts := strings.Split(name, "/")
	start, err := huggingFaceRepoIDStart(parts, false)
	if err != nil {
		return err
	}
	repositoryID := strings.Join(parts[start:], "/")
	if len(repositoryID) > 96 {
		return fmt.Errorf("Hugging Face repo_id must not exceed 96 characters")
	}
	if strings.HasSuffix(strings.ToLower(parts[len(parts)-1]), ".git") {
		return fmt.Errorf("Hugging Face repository name cannot end in .git")
	}
	for _, part := range parts[start:] {
		if err := validateHuggingFaceNamePart(part); err != nil {
			return fmt.Errorf("invalid Hugging Face repository segment %q: %w", part, err)
		}
	}
	return nil
}

// huggingFaceRepoIDStart separates Depsilo's dataset transport namespace from
// the Hub repo_id validated beneath it. A bare "datasets" remains a valid
// one-segment model ID; only a following slash makes it the dataset namespace.
func huggingFaceRepoIDStart(parts []string, pattern bool) (int, error) {
	switch len(parts) {
	case 1:
		return 0, nil
	case 2:
		if strings.EqualFold(parts[0], "datasets") && (!pattern || !hasGlobMetacharacter(parts[0])) {
			return 1, nil
		}
		return 0, nil
	case 3:
		if !strings.EqualFold(parts[0], "datasets") || pattern && hasGlobMetacharacter(parts[0]) {
			return 0, fmt.Errorf("three-part Hugging Face package identity must start with datasets/")
		}
		return 1, nil
	default:
		return 0, fmt.Errorf("Hugging Face package identity must be repo, owner/repo, datasets/repo, or datasets/owner/repo")
	}
}

func validateHuggingFaceNamePart(value string) error {
	if value == "" || len(value) > 96 {
		return fmt.Errorf("must contain 1 to 96 characters")
	}
	if !isASCIIAlphanumeric(value[0]) && value[0] != '_' ||
		!isASCIIAlphanumeric(value[len(value)-1]) && value[len(value)-1] != '_' {
		return fmt.Errorf("must not start or end with '-' or '.'")
	}
	if strings.Contains(value, "--") || strings.Contains(value, "..") {
		return fmt.Errorf("must not contain '--' or '..'")
	}
	for index := range len(value) {
		character := value[index]
		if isASCIIAlphanumeric(character) || strings.ContainsRune("._-", rune(character)) {
			continue
		}
		return fmt.Errorf("contains invalid character %q", character)
	}
	return nil
}

func normalizeHuggingFacePackagePrefix(prefix string) (string, error) {
	if err := validatePackageNameInput(prefix); err != nil {
		return "", err
	}
	if err := validateHuggingFacePackageName(prefix); err != nil {
		if candidateErr := validateHuggingFacePackageName(prefix + "x"); candidateErr != nil {
			return "", candidateErr
		}
	}
	return strings.ToLower(prefix), nil
}

func normalizeHuggingFacePackageGlob(pattern string) (string, error) {
	if err := validatePackageGlobInput(pattern); err != nil {
		return "", fmt.Errorf("invalid Hugging Face package glob: %w", err)
	}
	parts := strings.Split(pattern, "/")
	start, err := huggingFaceRepoIDStart(parts, true)
	if err != nil {
		return "", err
	}
	if len(strings.Join(parts[start:], "/")) > 96 {
		return "", fmt.Errorf("Hugging Face repo_id glob must not exceed 96 characters")
	}
	repoParts := parts[start:]
	for index, part := range repoParts {
		if part == "" {
			return "", fmt.Errorf("Hugging Face package glob contains an empty path segment")
		}
		if err := validateHuggingFaceGlobPart(part, index == len(repoParts)-1); err != nil {
			return "", fmt.Errorf("invalid Hugging Face package glob segment %q: %w", part, err)
		}
	}
	return strings.ToLower(pattern), nil
}

func validateHuggingFaceGlobPart(part string, repositoryName bool) error {
	if err := validateGlobCharacters(part, "._-", true); err != nil {
		return err
	}
	if !hasGlobMetacharacter(part) {
		if repositoryName && strings.HasSuffix(strings.ToLower(part), ".git") {
			return fmt.Errorf("repository name cannot end in .git")
		}
		return validateHuggingFaceNamePart(part)
	}
	if part[0] == '-' || part[0] == '.' || part[len(part)-1] == '-' || part[len(part)-1] == '.' {
		return fmt.Errorf("literal segment boundary must not be '-' or '.'")
	}
	if strings.Contains(part, "--") || strings.Contains(part, "..") {
		return fmt.Errorf("must not contain literal '--' or '..'")
	}
	if repositoryName && strings.HasSuffix(strings.ToLower(part), ".git") {
		return fmt.Errorf("repository pattern cannot end in .git")
	}
	return nil
}

func validateDockerRemoteName(name string) error {
	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return fmt.Errorf("Docker remote name is empty")
	}
	for _, part := range parts {
		if err := validateDockerPathComponent(part); err != nil {
			return fmt.Errorf("invalid Docker remote-name component %q: %w", part, err)
		}
	}
	return nil
}

func validateDockerPathComponent(value string) error {
	if value == "" || !isASCIILowercaseAlphanumeric(value[0]) {
		return fmt.Errorf("must start with a lowercase ASCII letter or digit")
	}
	index := 0
	for index < len(value) && isASCIILowercaseAlphanumeric(value[index]) {
		index++
	}
	for index < len(value) {
		separator := value[index]
		switch separator {
		case '.', '_':
			index++
			if separator == '_' && index < len(value) && value[index] == '_' {
				index++
			}
		case '-':
			for index < len(value) && value[index] == '-' {
				index++
			}
		default:
			return fmt.Errorf("contains invalid or uppercase character %q", separator)
		}
		if index == len(value) || !isASCIILowercaseAlphanumeric(value[index]) {
			return fmt.Errorf("separator must be followed by a lowercase ASCII letter or digit")
		}
		for index < len(value) && isASCIILowercaseAlphanumeric(value[index]) {
			index++
		}
	}
	return nil
}

func normalizeDockerPackagePrefix(prefix string) (string, error) {
	if err := validatePackageNameInput(prefix); err != nil {
		return "", err
	}
	candidate := prefix + "x"
	if err := validateDockerRemoteName(candidate); err != nil {
		return "", err
	}
	return prefix, nil
}

func normalizeDockerPackageGlob(pattern string) (string, error) {
	if err := validatePackageGlobInput(pattern); err != nil {
		return "", fmt.Errorf("invalid Docker package glob: %w", err)
	}
	parts := strings.Split(pattern, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("Docker package glob contains an unsafe path segment %q", part)
		}
		if err := validateGlobCharacters(part, "._-", false); err != nil {
			return "", fmt.Errorf("invalid Docker package glob segment %q: %w", part, err)
		}
		if !hasGlobMetacharacter(part) {
			if err := validateDockerPathComponent(part); err != nil {
				return "", fmt.Errorf("invalid Docker package glob segment %q: %w", part, err)
			}
		}
	}
	return pattern, nil
}

func exactCoordinateParts(value string, count int, label string) ([]string, error) {
	parts := strings.Split(value, "/")
	if len(parts) != count {
		return nil, fmt.Errorf("%s must contain exactly %d slash-separated segments", label, count)
	}
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("%s contains an empty path segment", label)
		}
	}
	return parts, nil
}

func validateCompletedCoordinatePrefix(prefix string, totalParts int, validate func(string) error) error {
	if err := validatePackageNameInput(prefix); err != nil {
		return err
	}
	slashes := strings.Count(prefix, "/")
	if slashes >= totalParts {
		return fmt.Errorf("package prefix contains too many path segments")
	}
	candidate := prefix + "x"
	for strings.Count(candidate, "/") < totalParts-1 {
		candidate += "/x"
	}
	return validate(candidate)
}

func validateSimpleASCIIName(value, punctuation string, allowTrailingPunctuation, allowUppercase bool) error {
	if value == "" || value == "." || value == ".." || !isASCIIAlphanumeric(value[0]) {
		return fmt.Errorf("must be a non-empty safe path segment beginning with an ASCII letter or digit")
	}
	if !allowTrailingPunctuation && !isASCIIAlphanumeric(value[len(value)-1]) {
		return fmt.Errorf("must end with an ASCII letter or digit")
	}
	for index := range len(value) {
		character := value[index]
		if isASCIIAlphanumeric(character) {
			if !allowUppercase && character >= 'A' && character <= 'Z' {
				return fmt.Errorf("contains uppercase character %q", character)
			}
			continue
		}
		if strings.ContainsRune(punctuation, rune(character)) {
			continue
		}
		return fmt.Errorf("contains invalid character %q", character)
	}
	return nil
}

func validateStructuredPackageGlob(pattern string, totalParts int, ecosystem, punctuation string, allowUppercase bool) ([]string, error) {
	if err := validatePackageGlobInput(pattern); err != nil {
		return nil, fmt.Errorf("invalid %s package glob: %w", ecosystem, err)
	}
	parts := strings.Split(pattern, "/")
	if len(parts) != totalParts {
		return nil, fmt.Errorf("%s package glob must contain exactly %d slash-separated segments", ecosystem, totalParts)
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, fmt.Errorf("%s package glob contains an unsafe path segment %q", ecosystem, part)
		}
		if err := validateGlobCharacters(part, punctuation, allowUppercase); err != nil {
			return nil, fmt.Errorf("invalid %s package glob segment %q: %w", ecosystem, part, err)
		}
	}
	return parts, nil
}

func validatePackageGlobInput(pattern string) error {
	if err := validatePackageNameInput(pattern); err != nil {
		return err
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return fmt.Errorf("invalid package glob %q: %w", pattern, err)
	}
	return nil
}

func validateGlobCharacters(value, punctuation string, allowUppercase bool) error {
	for index := range len(value) {
		character := value[index]
		if isASCIIAlphanumeric(character) {
			if !allowUppercase && character >= 'A' && character <= 'Z' {
				return fmt.Errorf("contains uppercase character %q", character)
			}
			continue
		}
		if strings.ContainsRune(punctuation+"*?[]\\^", rune(character)) {
			continue
		}
		return fmt.Errorf("contains invalid character %q", character)
	}
	return nil
}

func hasGlobMetacharacter(value string) bool {
	return strings.ContainsAny(value, "*?[\\")
}
