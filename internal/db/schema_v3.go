package db

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/packagepolicy"
	"gorm.io/gorm"
)

// migratePackageRuleDialects persists the exact normalized values evaluated by
// the rule engine. Legacy rows are prepared in one transaction with the schema
// change; an ambiguous rule leaves both schema version 2 and its data intact.
func migratePackageRuleDialects(database *gorm.DB) error {
	for _, field := range []string{"NormalizedPackageName", "NormalizedVersion", "DialectRevision"} {
		if database.Migrator().HasColumn(&PackageRule{}, field) {
			continue
		}
		if err := database.Migrator().AddColumn(&PackageRule{}, field); err != nil {
			return fmt.Errorf("add package rule %s column: %w", field, err)
		}
	}

	var rules []PackageRule
	if err := database.Order("id ASC").Find(&rules).Error; err != nil {
		return fmt.Errorf("read legacy package rules: %w", err)
	}
	type preparedRow struct {
		id          uint
		packageName string
		version     string
		revision    uint
	}
	preparedRows := make([]preparedRow, 0, len(rules))
	issues := make([]string, 0)
	for _, rule := range rules {
		prepared, err := packagepolicy.PrepareRuleRevision1(packagepolicy.RawRule{
			Ecosystem:   rule.Ecosystem,
			PackageName: rule.PackageName,
			Version:     rule.Version,
		})
		if err != nil {
			issues = append(issues, fmt.Sprintf("rule %d: %v", rule.ID, err))
			continue
		}
		// The legacy evaluator treated only the byte-exact value "allow" as
		// allowing. Normalizing an anomalous historical value such as "ALLOW"
		// would silently flip a deny decision into an allow during upgrade.
		if rule.Action != "allow" && rule.Action != "deny" {
			issues = append(issues, fmt.Sprintf(
				"rule %d: action must already be exactly allow or deny; recreate the rule instead of changing its legacy decision",
				rule.ID,
			))
			continue
		}
		// The legacy engine case-folded every package selector and exact
		// version, while its generic range comparator discarded ecosystem
		// qualifiers. A package- or version-specific row therefore has no
		// uniquely recoverable revision-1 meaning. Only ecosystem-wide */*
		// rules preserve the same match set and can be migrated automatically.
		if prepared.PackageName != "*" || prepared.Version != "*" {
			issues = append(issues, fmt.Sprintf(
				"rule %d: legacy package/version selector semantics are ambiguous; delete it before upgrade and recreate it under the ecosystem dialect",
				rule.ID,
			))
			continue
		}
		preparedRows = append(preparedRows, preparedRow{
			id:          rule.ID,
			packageName: prepared.NormalizedPackageName,
			version:     prepared.NormalizedVersion,
			revision:    prepared.DialectRevision,
		})
	}
	if len(issues) > 0 {
		return fmt.Errorf("prepare legacy package rules before upgrade: %s", strings.Join(issues, "; "))
	}
	for _, row := range preparedRows {
		if err := database.Model(&PackageRule{}).Where("id = ?", row.id).UpdateColumns(map[string]any{
			"normalized_package_name": row.packageName,
			"normalized_version":      row.version,
			"dialect_revision":        row.revision,
		}).Error; err != nil {
			return fmt.Errorf("backfill package rule %d dialect values: %w", row.id, err)
		}
	}
	if err := database.Model(&SecurityPolicy{}).
		Where("auto_block_enabled = ?", true).
		UpdateColumn("auto_block_enabled", false).Error; err != nil {
		return fmt.Errorf("disable legacy automatic vulnerability policies: %w", err)
	}
	if err := migrateLegacyBlocklistIdentities(database); err != nil {
		return err
	}
	if err := repairLegacyNPMScannerIdentity(database); err != nil {
		return err
	}
	if err := repairLegacyPyPIScannerIdentity(database); err != nil {
		return err
	}
	if err := repairLegacyAPTScannerIdentity(database); err != nil {
		return err
	}
	if err := repairLegacyGoScannerIdentity(database); err != nil {
		return err
	}
	if err := repairLegacyCargoScannerIdentity(database); err != nil {
		return err
	}
	if err := repairLegacyNuGetScannerIdentity(database); err != nil {
		return err
	}
	if err := repairLegacyCRANScannerIdentity(database); err != nil {
		return err
	}
	if err := repairLegacyMavenScannerIdentity(database); err != nil {
		return err
	}
	if err := repairLegacyRubyGemsScannerIdentity(database); err != nil {
		return err
	}
	if err := repairLegacyComposerScannerIdentity(database); err != nil {
		return err
	}
	if err := repairLegacyHuggingFaceCaseAliases(database); err != nil {
		return err
	}
	if err := canonicalizeLegacyQuarantineApprovals(database); err != nil {
		return err
	}
	return ensureSchemaV3Invariants(database)
}

// repairLegacyNPMScannerIdentity invalidates scanner-owned rows written from
// the pre-v3 case-folded npm cache namespace. The original registry spelling
// cannot be reconstructed from those rows, so preserving a false-clean check,
// dismissal, or project coordinate would attach one package's decision to
// another package's exact identity. New npm-exact-v1 cache entries remain
// available as the source set for a fresh scan.
func repairLegacyNPMScannerIdentity(database *gorm.DB) error {
	return invalidateLegacyVulnerabilityIdentityRows(database, "npm")
}

// repairLegacyRubyGemsScannerIdentity removes scanner decisions whose package
// names were guessed from artifact filenames. Platform gems such as
// nokogiri-1.16.5-x86_64-linux.gem were persisted as
// nokogiri-1.16.5-x86_64, so even a zero-result OSV response cannot safely be
// retained as evidence that the requested package was clean.
func repairLegacyRubyGemsScannerIdentity(database *gorm.DB) error {
	return invalidateLegacyVulnerabilityIdentityRows(database, "rubygems")
}

// repairLegacyComposerScannerIdentity rebuilds Composer identities only from
// the strict per-package p2 metadata and reversible dist cache-key shapes.
// Older releases accepted arbitrary p2 paths and could persist a guessed
// package name, so every Composer scanner decision must be invalidated before
// the repaired cache set is scanned again.
func repairLegacyComposerScannerIdentity(database *gorm.DB) error {
	return repairLegacyExtractedScannerIdentity(database, "composer", "Composer")
}

// repairLegacyPyPIScannerIdentity rebuilds cache metadata from trusted key
// shapes and canonicalizes those identities with PEP 503's name rule. An
// underscore, dot, or case alias in an older release could have produced a
// duplicate advisory identity, while arbitrary /files/ archives were split at
// their first hyphen and could produce a false-clean query for another package.
// Ambiguous keys are cleared; scanner-derived rows are invalidated before a
// fresh background scan considers the remaining trusted cache identities.
func repairLegacyPyPIScannerIdentity(database *gorm.DB) error {
	return repairLegacyExtractedScannerIdentity(database, "pypi", "PyPI")
}

// repairLegacyAPTScannerIdentity clears binary-package guesses that cannot be
// proven to be the source-package identity used by vulnerability advisories.
// Cache rows still own their stored objects and are preserved, while all
// scanner-derived decisions are invalidated until source-package provenance is
// available from authenticated repository metadata.
func repairLegacyAPTScannerIdentity(database *gorm.DB) error {
	if err := database.Model(&CacheEntry{}).
		Where("lower(adapter_type) = ?", "apt").
		UpdateColumn("package_name", "").Error; err != nil {
		return fmt.Errorf("clear legacy APT cache package identities: %w", err)
	}
	return invalidateLegacyVulnerabilityIdentityRows(database, "apt")
}

// repairLegacyGoScannerIdentity decodes the canonical GOPROXY spelling from
// each cache key instead of inheriting an escaped or otherwise untrusted
// package_name. Rows whose key does not round-trip through the Go module path
// escape rules retain their cache objects but lose the unusable identity.
func repairLegacyGoScannerIdentity(database *gorm.DB) error {
	return repairLegacyExtractedScannerIdentity(database, "go", "Go")
}

// repairLegacyCargoScannerIdentity trusts only case-exact crate artifact keys.
// Sparse-index paths normalize names to lowercase and therefore cannot recover
// Cargo's case-sensitive identity; configuration and malformed keys are also
// cleared before scanner-derived rows are rebuilt.
func repairLegacyCargoScannerIdentity(database *gorm.DB) error {
	return repairLegacyExtractedScannerIdentity(database, "cargo", "Cargo")
}

// repairLegacyNuGetScannerIdentity clears lowercase transport identities whose
// canonical registry casing cannot be recovered from flat-container or
// registration keys. Cache objects remain owned and available for retention;
// automatic vulnerability scanning stays disabled until provenance exists.
func repairLegacyNuGetScannerIdentity(database *gorm.DB) error {
	if err := database.Model(&CacheEntry{}).
		Where("lower(adapter_type) = ?", "nuget").
		UpdateColumn("package_name", "").Error; err != nil {
		return fmt.Errorf("clear legacy NuGet cache package identities: %w", err)
	}
	return invalidateLegacyVulnerabilityIdentityRows(database, "nuget")
}

// repairLegacyCRANScannerIdentity retains only package identities recovered
// from strict source/archive/binary artifact key shapes. Metadata and
// ambiguous archive paths keep their cache objects but cannot seed scans.
func repairLegacyCRANScannerIdentity(database *gorm.DB) error {
	return repairLegacyExtractedScannerIdentity(database, "cran", "CRAN")
}

// repairLegacyExtractedScannerIdentity is the shared migration boundary for
// ecosystems whose current cache-key parser can prove a package identity.
// Legacy package_name is never consulted: an unrecognized key or a dialect
// validation failure deliberately writes an empty identity.
func repairLegacyExtractedScannerIdentity(database *gorm.DB, ecosystemName, displayName string) error {
	dialect, err := packagepolicy.DialectFor(ecosystemName)
	if err != nil {
		return fmt.Errorf("load %s package dialect: %w", displayName, err)
	}
	var rows []cacheBackfillRow
	if err := database.Table("cache_entries").
		Where("lower(adapter_type) = ?", ecosystemName).
		FindInBatches(&rows, 500, func(_ *gorm.DB, _ int) error {
			idsByPackage := make(map[string][]uint)
			for _, row := range rows {
				packageName := packagekey.ExtractName(ecosystemName, row.Key)
				normalized, err := dialect.NormalizePackageName(packageName)
				if err != nil {
					normalized = ""
				}
				idsByPackage[normalized] = append(idsByPackage[normalized], row.ID)
			}
			for packageName, ids := range idsByPackage {
				if err := database.Table("cache_entries").Where("id IN ?", ids).
					Update("package_name", packageName).Error; err != nil {
					return fmt.Errorf("backfill %s cache package identity: %w", displayName, err)
				}
			}
			return nil
		}).Error; err != nil {
		return fmt.Errorf("read legacy %s cache identities: %w", displayName, err)
	}

	return invalidateLegacyVulnerabilityIdentityRows(database, ecosystemName)
}

// invalidateLegacyVulnerabilityIdentityRows removes every identity-dependent
// row for one ecosystem. Legacy storage does not distinguish automatically
// fetched advisories from operator imports, so retaining either kind would risk
// attaching it (and its dismissals) to the wrong canonical package. The SQL
// subquery also avoids materializing advisory IDs into a Go slice: a real
// imported catalog can exceed SQLite's 32,766 bind-parameter limit.
func invalidateLegacyVulnerabilityIdentityRows(database *gorm.DB, ecosystemName string) error {
	vulnerabilityIDs := database.Model(&Vulnerability{}).
		Select("id").
		Where("lower(ecosystem) = ?", ecosystemName)
	if err := database.Where("vulnerability_id IN (?)", vulnerabilityIDs).
		Delete(&DismissedVuln{}).Error; err != nil {
		return fmt.Errorf("invalidate legacy %s vulnerability dismissals: %w", ecosystemName, err)
	}
	if err := database.Where("lower(ecosystem) = ?", ecosystemName).
		Delete(&Vulnerability{}).Error; err != nil {
		return fmt.Errorf("invalidate legacy %s vulnerabilities: %w", ecosystemName, err)
	}
	if err := database.Where("lower(ecosystem) = ?", ecosystemName).
		Delete(&VulnerabilityCheck{}).Error; err != nil {
		return fmt.Errorf("invalidate legacy %s vulnerability checks: %w", ecosystemName, err)
	}
	if err := database.Where("lower(ecosystem) = ?", ecosystemName).
		Delete(&ProjectPackage{}).Error; err != nil {
		return fmt.Errorf("invalidate legacy %s project packages: %w", ecosystemName, err)
	}
	return nil
}

type schemaV3BlocklistMigrationStats struct {
	discardedRows      int
	mergedRows         int
	failClosedRows     int
	discardedOverrides int
	mergedOverrides    int
}

type schemaV3BlocklistRowKey struct {
	sourceID  string
	ecosystem string
	packageID string
}

type schemaV3VersionSet struct {
	all    bool
	values []string
}

type schemaV3CanonicalBlocklistRow struct {
	row      MaliciousPackage
	versions schemaV3VersionSet
}

// migrateLegacyBlocklistIdentities moves imported data and operator overrides
// onto the same identity contract used by request-time checks. npm rows are
// always invalidated: older releases lowercased the package name, so their
// original case-sensitive identity cannot be recovered. Rows for ecosystems
// outside schema v3's six synchronized datasets are removed so a stale PyPI,
// RubyGems, or unknown record cannot survive after its source stopped syncing.
//
// Recoverable rows keep their protection. Alias duplicates for one advisory
// are merged by taking the union of affected versions; an all-version row wins
// that union. A valid package with corrupt or invalid version data remains an
// all-version block, matching the legacy fail-closed behavior while an
// authoritative resync is pending. Invalid overrides are discarded
// unconditionally because broadening a security exception is less safe than
// requiring the operator to recreate it.
func migrateLegacyBlocklistIdentities(database *gorm.DB) error {
	var stats schemaV3BlocklistMigrationStats
	if err := migrateLegacyMaliciousPackages(database, &stats); err != nil {
		return err
	}
	if err := migrateLegacyMalwareOverrides(database, &stats); err != nil {
		return err
	}

	var remaining int64
	if err := database.Model(&MaliciousPackage{}).Count(&remaining).Error; err != nil {
		return fmt.Errorf("count blocklist rows after identity migration: %w", err)
	}
	lastError := fmt.Sprintf(
		"schema v3 package identities changed; full blocklist resync required (retained %d rows; discarded %d rows and %d overrides; treated %d invalid-version rows as all-version; merged %d rows and %d equivalent overrides)",
		remaining,
		stats.discardedRows,
		stats.discardedOverrides,
		stats.failClosedRows,
		stats.mergedRows,
		stats.mergedOverrides,
	)
	if err := database.Model(&BlocklistSyncState{}).Where("id = ?", 1).UpdateColumns(map[string]any{
		"last_success_at": nil,
		"last_error":      lastError,
		"entry_count":     remaining,
	}).Error; err != nil {
		return fmt.Errorf("mark full blocklist identity resync required: %w", err)
	}
	return nil
}

func migrateLegacyMaliciousPackages(database *gorm.DB, stats *schemaV3BlocklistMigrationStats) error {
	var legacy []MaliciousPackage
	if err := database.Order("id ASC").Find(&legacy).Error; err != nil {
		return fmt.Errorf("read legacy blocklist rows: %w", err)
	}

	canonical := make([]schemaV3CanonicalBlocklistRow, 0, len(legacy))
	indexByKey := make(map[schemaV3BlocklistRowKey]int, len(legacy))
	for _, row := range legacy {
		ecosystemName, keep := schemaV3LegacyBlocklistEcosystem(row.Ecosystem)
		if !keep || ecosystemName == "npm" {
			stats.discardedRows++
			continue
		}
		dialect, err := packagepolicy.DialectFor(ecosystemName)
		if err != nil {
			return fmt.Errorf("load %s blocklist package dialect: %w", ecosystemName, err)
		}
		packageName, err := dialect.NormalizePackageName(row.Package)
		if err != nil {
			stats.discardedRows++
			continue
		}
		versions, err := schemaV3CanonicalVersionSet(ecosystemName, dialect, row.Versions)
		if err != nil {
			versions = schemaV3VersionSet{all: true}
			stats.failClosedRows++
		}

		row.Ecosystem = ecosystemName
		row.Package = packageName
		key := schemaV3BlocklistRowKey{
			sourceID: row.SourceID, ecosystem: ecosystemName, packageID: packageName,
		}
		if index, exists := indexByKey[key]; exists {
			merged, err := schemaV3MergeVersionSets(dialect, canonical[index].versions, versions)
			if err != nil {
				return fmt.Errorf("merge legacy blocklist row %d versions: %w", row.ID, err)
			}
			canonical[index].versions = merged
			schemaV3MergeBlocklistMetadata(&canonical[index].row, row)
			stats.mergedRows++
			continue
		}
		indexByKey[key] = len(canonical)
		canonical = append(canonical, schemaV3CanonicalBlocklistRow{row: row, versions: versions})
	}

	if len(legacy) > 0 {
		if err := database.Where("1 = 1").Delete(&MaliciousPackage{}).Error; err != nil {
			return fmt.Errorf("replace legacy blocklist rows: %w", err)
		}
	}
	if len(canonical) == 0 {
		return nil
	}

	rows := make([]MaliciousPackage, 0, len(canonical))
	for _, migrated := range canonical {
		encoded, err := schemaV3EncodeVersionSet(migrated.versions)
		if err != nil {
			return fmt.Errorf("encode canonical blocklist row %d versions: %w", migrated.row.ID, err)
		}
		migrated.row.Versions = encoded
		rows = append(rows, migrated.row)
	}
	if err := database.CreateInBatches(&rows, 200).Error; err != nil {
		return fmt.Errorf("write canonical blocklist rows: %w", err)
	}
	return nil
}

func migrateLegacyMalwareOverrides(database *gorm.DB, stats *schemaV3BlocklistMigrationStats) error {
	var legacy []MalwareOverride
	if err := database.Order("id ASC").Find(&legacy).Error; err != nil {
		return fmt.Errorf("read legacy malware overrides: %w", err)
	}

	canonical := make([]MalwareOverride, 0, len(legacy))
	for _, override := range legacy {
		ecosystemName, keep := schemaV3LegacyBlocklistEcosystem(override.Ecosystem)
		if !keep || ecosystemName == "npm" {
			stats.discardedOverrides++
			continue
		}
		dialect, err := packagepolicy.DialectFor(ecosystemName)
		if err != nil {
			return fmt.Errorf("load %s override package dialect: %w", ecosystemName, err)
		}
		packageName, err := dialect.NormalizePackageName(override.Package)
		if err != nil {
			stats.discardedOverrides++
			continue
		}
		version := ""
		if override.Version != "" {
			version, err = schemaV3CanonicalBlocklistVersion(ecosystemName, override.Version)
			if err != nil {
				stats.discardedOverrides++
				continue
			}
		}
		override.Ecosystem = ecosystemName
		override.Package = packageName
		override.Version = version

		duplicate := -1
		for index := range canonical {
			existing := canonical[index]
			if existing.Ecosystem != ecosystemName || existing.Package != packageName {
				continue
			}
			if existing.Version == "" || version == "" {
				if existing.Version == version {
					duplicate = index
					break
				}
				continue
			}
			comparison, err := dialect.CompareVersions(existing.Version, version)
			if err != nil {
				return fmt.Errorf("compare canonical malware override versions: %w", err)
			}
			if comparison == 0 {
				duplicate = index
				break
			}
		}
		if duplicate < 0 {
			canonical = append(canonical, override)
			continue
		}
		if schemaV3PreferOverride(override, canonical[duplicate]) {
			canonical[duplicate] = override
		}
		stats.mergedOverrides++
	}

	if len(legacy) > 0 {
		if err := database.Where("1 = 1").Delete(&MalwareOverride{}).Error; err != nil {
			return fmt.Errorf("replace legacy malware overrides: %w", err)
		}
	}
	if len(canonical) == 0 {
		return nil
	}
	if err := database.CreateInBatches(&canonical, 200).Error; err != nil {
		return fmt.Errorf("write canonical malware overrides: %w", err)
	}
	return nil
}

func schemaV3LegacyBlocklistEcosystem(value string) (string, bool) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", false
	}
	canonical := strings.ToLower(value)
	// This switch freezes the dataset support contract at the point schema v3
	// was introduced. A later capability change needs its own numbered
	// migration instead of changing the interpretation of an applied one.
	switch canonical {
	case "npm", "cargo", "composer", "nuget", "go", "maven":
		return canonical, true
	default:
		return canonical, false
	}
}

func schemaV3CanonicalVersionSet(
	ecosystemName string,
	dialect packagepolicy.PackagePolicyDialect,
	raw string,
) (schemaV3VersionSet, error) {
	if raw == "" {
		return schemaV3VersionSet{all: true}, nil
	}
	var versions []string
	if err := json.Unmarshal([]byte(raw), &versions); err != nil {
		return schemaV3VersionSet{}, err
	}
	if versions == nil {
		return schemaV3VersionSet{}, fmt.Errorf("version list must be a JSON array")
	}
	if len(versions) == 0 {
		return schemaV3VersionSet{all: true}, nil
	}
	canonical := schemaV3VersionSet{values: make([]string, 0, len(versions))}
	for _, version := range versions {
		normalized, err := schemaV3CanonicalBlocklistVersion(ecosystemName, version)
		if err != nil {
			return schemaV3VersionSet{}, err
		}
		canonical.values, err = schemaV3AppendUniqueVersion(dialect, canonical.values, normalized)
		if err != nil {
			return schemaV3VersionSet{}, err
		}
	}
	sort.Strings(canonical.values)
	return canonical, nil
}

func schemaV3CanonicalBlocklistVersion(ecosystemName, version string) (string, error) {
	if ecosystemName == "go" {
		version = strings.TrimPrefix(version, "v")
	}
	return packagepolicy.NormalizeVersion(ecosystemName, version)
}

func schemaV3AppendUniqueVersion(
	dialect packagepolicy.PackagePolicyDialect,
	versions []string,
	candidate string,
) ([]string, error) {
	for _, version := range versions {
		comparison, err := dialect.CompareVersions(version, candidate)
		if err != nil {
			return nil, err
		}
		if comparison == 0 {
			return versions, nil
		}
	}
	return append(versions, candidate), nil
}

func schemaV3MergeVersionSets(
	dialect packagepolicy.PackagePolicyDialect,
	left, right schemaV3VersionSet,
) (schemaV3VersionSet, error) {
	if left.all || right.all {
		return schemaV3VersionSet{all: true}, nil
	}
	merged := schemaV3VersionSet{values: append([]string(nil), left.values...)}
	var err error
	for _, version := range right.values {
		merged.values, err = schemaV3AppendUniqueVersion(dialect, merged.values, version)
		if err != nil {
			return schemaV3VersionSet{}, err
		}
	}
	sort.Strings(merged.values)
	return merged, nil
}

func schemaV3EncodeVersionSet(versions schemaV3VersionSet) (string, error) {
	if versions.all {
		return "", nil
	}
	encoded, err := json.Marshal(versions.values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func schemaV3MergeBlocklistMetadata(current *MaliciousPackage, candidate MaliciousPackage) {
	if schemaV3PreferBlocklistMetadata(candidate, *current) {
		current.Summary = candidate.Summary
	}
	current.Aliases = schemaV3MergeAliases(current.Aliases, candidate.Aliases)
	if candidate.Modified.After(current.Modified) {
		current.Modified = candidate.Modified
	}
	if candidate.ImportedAt.After(current.ImportedAt) {
		current.ImportedAt = candidate.ImportedAt
	}
}

func schemaV3PreferBlocklistMetadata(candidate, current MaliciousPackage) bool {
	if !candidate.Modified.Equal(current.Modified) {
		return candidate.Modified.After(current.Modified)
	}
	if !candidate.ImportedAt.Equal(current.ImportedAt) {
		return candidate.ImportedAt.After(current.ImportedAt)
	}
	return candidate.ID > current.ID
}

func schemaV3MergeAliases(left, right string) string {
	unique := make(map[string]struct{})
	for _, aliases := range []string{left, right} {
		for _, alias := range strings.Split(aliases, ",") {
			alias = strings.TrimSpace(alias)
			if alias != "" {
				unique[alias] = struct{}{}
			}
		}
	}
	merged := make([]string, 0, len(unique))
	for alias := range unique {
		merged = append(merged, alias)
	}
	sort.Strings(merged)
	return strings.Join(merged, ",")
}

func schemaV3PreferOverride(candidate, current MalwareOverride) bool {
	if !candidate.ExpiresAt.Equal(current.ExpiresAt) {
		return candidate.ExpiresAt.After(current.ExpiresAt)
	}
	if !candidate.CreatedAt.Equal(current.CreatedAt) {
		return candidate.CreatedAt.After(current.CreatedAt)
	}
	return candidate.ID > current.ID
}

// repairLegacyMavenScannerIdentity replaces the artifactId-only identity used
// before schema v3 with Maven's OSV coordinate, groupId:artifactId. Metadata
// paths do not prove a versioned coordinate and are deliberately cleared so
// the background scanner cannot turn a filename guess into a false clean
// result. Scanner-derived rows are invalidated and rebuilt from repaired cache
// entries; project package rows cannot recover their missing groupId and are
// likewise allowed to repopulate from future requests.
func repairLegacyMavenScannerIdentity(database *gorm.DB) error {
	var rows []cacheBackfillRow
	if err := database.Table("cache_entries").
		Where("lower(adapter_type) = ?", "maven").
		FindInBatches(&rows, 500, func(_ *gorm.DB, _ int) error {
			idsByPackage := make(map[string][]uint)
			for _, row := range rows {
				packageName := packagekey.ExtractName("maven", row.Key)
				idsByPackage[packageName] = append(idsByPackage[packageName], row.ID)
			}
			for packageName, ids := range idsByPackage {
				if err := database.Table("cache_entries").Where("id IN ?", ids).
					Update("package_name", packageName).Error; err != nil {
					return fmt.Errorf("backfill Maven cache package identity: %w", err)
				}
			}
			return nil
		}).Error; err != nil {
		return fmt.Errorf("read legacy Maven cache identities: %w", err)
	}

	return invalidateLegacyVulnerabilityIdentityRows(database, "maven")
}

const (
	// RetiredHuggingFaceAdapterType marks cache metadata that schema v3 moved
	// out of the adapter-generated keyspace. Server startup must reclaim these
	// rows before accepting cache writes that could reuse their storage paths.
	RetiredHuggingFaceAdapterType         = "retired-v3"
	schemaV3RetiredHuggingFaceKeyPrefix   = "huggingface/__retired-v3__/entry/"
	schemaV3RetiredHuggingFaceAdapterType = RetiredHuggingFaceAdapterType
)

// repairLegacyHuggingFaceCaseAliases retires pre-v3 cache identities that may
// have split one Hub repository across case aliases. The row must remain until
// storage-owning retention deletes its object: dropping metadata here would
// leave the object permanently untracked. The retired namespace cannot be
// generated by a recognized Hub route, and the expired row is therefore both
// unreachable to adapters and eligible for ordinary retention. Ref pins and
// project packages are derived metadata and can be discarded. Durable
// revocation markers are collapsed to one lowercase key and marked
// cleanup-unsafe so the handler keeps its gate closed until a fresh cleanup
// generation succeeds.
func repairLegacyHuggingFaceCaseAliases(database *gorm.DB) error {
	if err := retireLegacyHuggingFaceCacheEntries(database); err != nil {
		return err
	}
	if err := database.Where("1 = 1").Delete(&HuggingFaceRefPin{}).Error; err != nil {
		return fmt.Errorf("invalidate legacy Hugging Face ref pins: %w", err)
	}
	if err := database.Where("lower(ecosystem) = ?", "huggingface").
		Delete(&ProjectPackage{}).Error; err != nil {
		return fmt.Errorf("invalidate legacy Hugging Face project packages: %w", err)
	}

	var records []HuggingFaceRepositoryRevocation
	if err := database.Order("updated_at ASC, repository ASC").Find(&records).Error; err != nil {
		return fmt.Errorf("read legacy Hugging Face revocation markers: %w", err)
	}
	canonical := make(map[string]HuggingFaceRepositoryRevocation, len(records))
	for _, record := range records {
		repository := strings.ToLower(record.Repository)
		record.Repository = repository
		record.EscapedRepo = strings.ToLower(record.EscapedRepo)
		record.CleanupSafe = false
		canonical[repository] = record
	}
	if len(records) == 0 {
		return nil
	}
	if err := database.Where("1 = 1").Delete(&HuggingFaceRepositoryRevocation{}).Error; err != nil {
		return fmt.Errorf("replace legacy Hugging Face revocation markers: %w", err)
	}
	markers := make([]HuggingFaceRepositoryRevocation, 0, len(canonical))
	for _, record := range canonical {
		markers = append(markers, record)
	}
	if err := database.CreateInBatches(&markers, 200).Error; err != nil {
		return fmt.Errorf("write canonical Hugging Face revocation markers: %w", err)
	}
	return nil
}

type schemaV3HuggingFaceCacheRetirementRow struct {
	ID uint `gorm:"primaryKey"`
}

// retireLegacyHuggingFaceCacheEntries processes a bounded row batch rather
// than materializing every legacy key or an ID IN (...) list. Large caches can
// exceed SQLite's bind-variable limit; per-row target allocation keeps the
// migration size-independent and preserves each object's storage ownership.
func retireLegacyHuggingFaceCacheEntries(database *gorm.DB) error {
	var rows []schemaV3HuggingFaceCacheRetirementRow
	return database.Table("cache_entries").
		Select("id").
		Where("lower(adapter_type) = ?", "huggingface").
		Order("id ASC").
		FindInBatches(&rows, 500, func(tx *gorm.DB, _ int) error {
			for _, row := range rows {
				key, err := allocateSchemaV3RetiredHuggingFaceKey(tx, row.ID)
				if err != nil {
					return err
				}
				updated := tx.Table("cache_entries").
					Where("id = ? AND lower(adapter_type) = ?", row.ID, "huggingface").
					UpdateColumns(map[string]any{
						"key":          key,
						"adapter_type": schemaV3RetiredHuggingFaceAdapterType,
						"package_name": "",
						"expires_at":   time.Unix(0, 0).UTC(),
					})
				if updated.Error != nil {
					return fmt.Errorf("retire legacy Hugging Face cache entry %d: %w", row.ID, updated.Error)
				}
				if updated.RowsAffected != 1 {
					return fmt.Errorf("retire legacy Hugging Face cache entry %d: expected one row, updated %d", row.ID, updated.RowsAffected)
				}
			}
			return nil
		}).Error
}

func allocateSchemaV3RetiredHuggingFaceKey(database *gorm.DB, id uint) (string, error) {
	base := schemaV3RetiredHuggingFaceKeyPrefix + strconv.FormatUint(uint64(id), 10)
	for suffix := uint(0); ; suffix++ {
		key := base
		if suffix != 0 {
			key += "-" + strconv.FormatUint(uint64(suffix), 10)
		}
		if len(key) > 512 {
			return "", fmt.Errorf("retired Hugging Face key for cache entry %d exceeds 512 bytes", id)
		}
		var conflicts int64
		if err := database.Table("cache_entries").
			Where("key = ? AND id <> ?", key, id).
			Count(&conflicts).Error; err != nil {
			return "", fmt.Errorf("check retired Hugging Face cache key %q: %w", key, err)
		}
		if conflicts == 0 {
			return key, nil
		}
	}
}

// canonicalizeLegacyQuarantineApprovals moves permanent approvals onto the
// same ecosystem identity contract used for request-time checks. Equivalent
// aliases collapse to the newest operator decision. An invalid legacy row is
// rejected instead of being interpreted heuristically: silently broadening a
// permanent security exception during upgrade would be unsafe.
func canonicalizeLegacyQuarantineApprovals(database *gorm.DB) error {
	var rows []ApprovedVersion
	if err := database.Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
		return fmt.Errorf("read legacy quarantine approvals: %w", err)
	}
	canonical, err := canonicalQuarantineApprovals(rows)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if err := database.Where("1 = 1").Delete(&ApprovedVersion{}).Error; err != nil {
		return fmt.Errorf("replace legacy quarantine approvals: %w", err)
	}
	if len(canonical) > 0 {
		if err := database.CreateInBatches(&canonical, 200).Error; err != nil {
			return fmt.Errorf("write canonical quarantine approvals: %w", err)
		}
	}
	return nil
}

func canonicalQuarantineApprovals(rows []ApprovedVersion) ([]ApprovedVersion, error) {
	canonical := make([]ApprovedVersion, 0, len(rows))
	type coordinate struct {
		ecosystem   string
		packageName string
	}
	indicesByCoordinate := make(map[coordinate][]int)
	for _, row := range rows {
		normalized, dialect, err := normalizeQuarantineApproval(row)
		if err != nil {
			return nil, fmt.Errorf(
				"quarantine approval %d (%q:%q@%q) cannot be migrated safely: %w; revoke it before upgrade",
				row.ID, row.Ecosystem, row.Package, row.Version, err,
			)
		}
		key := coordinate{ecosystem: normalized.Ecosystem, packageName: normalized.Package}
		replaced := false
		for _, index := range indicesByCoordinate[key] {
			candidate := canonical[index]
			comparison, err := dialect.CompareVersions(candidate.Version, normalized.Version)
			if err != nil {
				return nil, fmt.Errorf("compare quarantine approval %d: %w", row.ID, err)
			}
			if comparison == 0 {
				// Rows arrive oldest first, so replacement retains the newest
				// reason, actor, timestamp, and spelling when aliases collide.
				canonical[index] = normalized
				replaced = true
				break
			}
		}
		if !replaced {
			canonical = append(canonical, normalized)
			indicesByCoordinate[key] = append(indicesByCoordinate[key], len(canonical)-1)
		}
	}
	return canonical, nil
}

func normalizeQuarantineApproval(row ApprovedVersion) (ApprovedVersion, packagepolicy.PackagePolicyDialect, error) {
	ecosystemName := strings.ToLower(strings.TrimSpace(row.Ecosystem))
	dialect, err := packagepolicy.DialectFor(ecosystemName)
	if err != nil {
		return ApprovedVersion{}, nil, err
	}
	packageName, err := dialect.NormalizePackageName(row.Package)
	if err != nil {
		return ApprovedVersion{}, nil, fmt.Errorf("package: %w", err)
	}
	version, err := packagepolicy.NormalizeVersion(ecosystemName, row.Version)
	if err != nil {
		return ApprovedVersion{}, nil, fmt.Errorf("version: %w", err)
	}
	if len(ecosystemName) > 32 || len(packageName) > 256 || len(version) > 128 {
		return ApprovedVersion{}, nil, fmt.Errorf("normalized coordinate exceeds database limits")
	}
	row.Ecosystem = ecosystemName
	row.Package = packageName
	row.Version = version
	return row, dialect, nil
}

func ensureSchemaV3Invariants(database *gorm.DB) error {
	for _, field := range []string{"NormalizedPackageName", "NormalizedVersion", "DialectRevision"} {
		if !database.Migrator().HasColumn(&PackageRule{}, field) {
			return fmt.Errorf("package rule %s column is missing", field)
		}
	}
	var invalid int64
	if err := database.Model(&PackageRule{}).
		Where("normalized_package_name = '' OR normalized_version = '' OR dialect_revision <> ?", packagepolicy.DialectRevision1).
		Count(&invalid).Error; err != nil {
		return fmt.Errorf("verify prepared package rules: %w", err)
	}
	if invalid != 0 {
		return fmt.Errorf("package rule dialect invariant failed: %d unprepared rows", invalid)
	}
	var rules []PackageRule
	if err := database.Order("id ASC").Find(&rules).Error; err != nil {
		return fmt.Errorf("verify prepared package rule values: %w", err)
	}
	for _, rule := range rules {
		prepared, err := packagepolicy.PrepareRuleRevision1(packagepolicy.RawRule{
			Ecosystem: rule.Ecosystem, PackageName: rule.PackageName, Version: rule.Version,
		})
		if err != nil {
			return fmt.Errorf("package rule %d violates dialect revision 1: %w", rule.ID, err)
		}
		action, err := packagepolicy.NormalizeRuleAction(rule.Action)
		if err != nil {
			return fmt.Errorf("package rule %d violates dialect revision 1: %w", rule.ID, err)
		}
		if rule.Ecosystem != prepared.Ecosystem ||
			rule.PackageName != prepared.PackageName ||
			rule.Version != prepared.Version ||
			rule.NormalizedPackageName != prepared.NormalizedPackageName ||
			rule.NormalizedVersion != prepared.NormalizedVersion ||
			rule.DialectRevision != prepared.DialectRevision ||
			rule.Action != action {
			return fmt.Errorf("package rule %d raw and normalized values are inconsistent", rule.ID)
		}
	}
	return ensureQuarantineApprovalDialectInvariants(database)
}

func ensureQuarantineApprovalDialectInvariants(database *gorm.DB) error {
	var rows []ApprovedVersion
	if err := database.Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
		return fmt.Errorf("verify quarantine approvals: %w", err)
	}
	canonical, err := canonicalQuarantineApprovals(rows)
	if err != nil {
		return err
	}
	if len(canonical) != len(rows) {
		return fmt.Errorf("quarantine approval dialect invariant failed: semantic duplicate approvals exist")
	}
	for index := range rows {
		normalized, _, err := normalizeQuarantineApproval(rows[index])
		if err != nil {
			return fmt.Errorf("verify quarantine approval %d: %w", rows[index].ID, err)
		}
		if rows[index].Ecosystem != normalized.Ecosystem ||
			rows[index].Package != normalized.Package || rows[index].Version != normalized.Version {
			return fmt.Errorf("quarantine approval %d is not stored in canonical dialect form", rows[index].ID)
		}
	}
	return nil
}
