package security

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"depsilo/internal/db"
	"depsilo/internal/packagepolicy"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxAdvisoryImportBytes      = 32 << 20
	maxAdvisoryPackageNameBytes = 256
	storedAdvisoryReadBatchSize = 500
)

var (
	// ErrInvalidPackageScan means a scan result cannot safely be associated
	// with the package that was queried.
	ErrInvalidPackageScan = errors.New("invalid package scan")
	// ErrInvalidAdvisoryImport means an import is malformed or lacks a complete
	// advisory/package identity.
	ErrInvalidAdvisoryImport = errors.New("invalid advisory import")
	// ErrAdvisoryImportTooLarge means an import exceeds the bounded JSON input.
	ErrAdvisoryImportTooLarge = errors.New("advisory import exceeds 32 MiB")
)

// IngestReceipt reports committed work. Callers receive the zero value when
// an operation fails, even when the failure happens late in its transaction.
type IngestReceipt struct {
	Received     int `json:"received"`
	Advisories   int `json:"advisories"`
	Packages     int `json:"packages"`
	Duplicates   int `json:"duplicates"`
	Skipped      int `json:"skipped"`
	RulesCreated int `json:"rules_created"`
}

// AdvisoryCatalog is the transactional write boundary for vulnerability
// scans and operator imports. Its write gate is shared by both entry points so
// advisory/check reconciliation remains deterministic on SQLite's
// single-writer database.
type AdvisoryCatalog struct {
	db       *gorm.DB
	checkTTL time.Duration
	writes   chan struct{}
}

type advisoryIdentity struct {
	ecosystem string
	name      string
}

type normalizedAdvisory struct {
	model db.Vulnerability
}

type advisoryKey struct {
	osvID string
	advisoryIdentity
}

func (a normalizedAdvisory) identity() advisoryIdentity {
	return advisoryIdentity{ecosystem: a.model.Ecosystem, name: a.model.PackageName}
}

func (a normalizedAdvisory) key() advisoryKey {
	return advisoryKey{osvID: a.model.OSVID, advisoryIdentity: a.identity()}
}

// NewAdvisoryCatalog creates the shared vulnerability ingestion boundary.
func NewAdvisoryCatalog(database *gorm.DB, checkTTL time.Duration) (*AdvisoryCatalog, error) {
	if database == nil {
		return nil, errors.New("create advisory catalog: database is nil")
	}
	if checkTTL <= 0 {
		return nil, errors.New("create advisory catalog: check TTL must be positive")
	}

	writes := make(chan struct{}, 1)
	writes <- struct{}{}
	return &AdvisoryCatalog{
		db:       database,
		checkTTL: checkTTL,
		writes:   writes,
	}, nil
}

// RecordScan atomically stores one package's current OSV result and refreshes
// its check record. Advisory ingestion never mutates operator Package Rules.
func (c *AdvisoryCatalog) RecordScan(
	ctx context.Context,
	pkg PackageRef,
	advisories []OSVVulnerability,
) (IngestReceipt, error) {
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return IngestReceipt{}, err
	}

	identity, normalized, duplicates, err := normalizePackageScan(pkg, advisories)
	if err != nil {
		return IngestReceipt{}, err
	}
	receipt := IngestReceipt{
		Received:   len(advisories),
		Advisories: len(normalized),
		Packages:   1,
		Duplicates: duplicates,
	}

	err = c.writeTransaction(ctx, func(tx *gorm.DB) error {
		_, err := reconcileAdvisories(tx, normalized)
		if err != nil {
			return fmt.Errorf("store package scan advisories: %w", err)
		}

		now := time.Now().UTC()
		if err := upsertVulnerabilityCheck(tx, identity, len(normalized), now, c.checkTTL); err != nil {
			return fmt.Errorf("store package scan check: %w", err)
		}

		return nil
	})
	if err != nil {
		return IngestReceipt{}, err
	}

	return receipt, nil
}

// Import atomically ingests one OSV object or an array of OSV objects. The
// complete file is decoded and validated before the write gate is acquired.
func (c *AdvisoryCatalog) Import(ctx context.Context, src io.Reader) (IngestReceipt, error) {
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return IngestReceipt{}, err
	}
	if src == nil {
		return IngestReceipt{}, fmt.Errorf("%w: reader is nil", ErrInvalidAdvisoryImport)
	}

	advisories, err := decodeAdvisoryImport(src)
	if err != nil {
		return IngestReceipt{}, err
	}
	normalized, duplicates, skipped, err := normalizeAdvisoryImport(advisories)
	if err != nil {
		return IngestReceipt{}, err
	}
	groups := groupAdvisories(normalized)
	receipt := IngestReceipt{
		Received:   len(advisories),
		Advisories: len(normalized),
		Packages:   len(groups),
		Duplicates: duplicates,
		Skipped:    skipped,
	}

	err = c.writeTransaction(ctx, func(tx *gorm.DB) error {
		effective, err := reconcileAdvisories(tx, normalized)
		if err != nil {
			return fmt.Errorf("store imported advisories: %w", err)
		}

		now := time.Now().UTC()
		for _, group := range groupAdvisories(effective) {
			var total int64
			if err := tx.Model(&db.Vulnerability{}).
				Where("ecosystem = ? AND package_name = ?", group.identity.ecosystem, group.identity.name).
				Count(&total).Error; err != nil {
				return fmt.Errorf("count imported package advisories: %w", err)
			}
			if total > int64(maxInt()) {
				return errors.New("count imported package advisories: count overflows int")
			}
			if err := upsertVulnerabilityCheck(tx, group.identity, int(total), now, c.checkTTL); err != nil {
				return fmt.Errorf("store imported package check: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return IngestReceipt{}, err
	}

	return receipt, nil
}

func (c *AdvisoryCatalog) writeTransaction(ctx context.Context, fn func(*gorm.DB) error) error {
	if c == nil || c.db == nil || c.writes == nil {
		return errors.New("advisory catalog is not initialized")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.writes:
	}
	defer func() { c.writes <- struct{}{} }()

	if err := ctx.Err(); err != nil {
		return err
	}
	return c.db.WithContext(ctx).Transaction(fn)
}

func normalizePackageScan(
	pkg PackageRef,
	input []OSVVulnerability,
) (advisoryIdentity, []normalizedAdvisory, int, error) {
	ecosystemName := strings.ToLower(strings.TrimSpace(pkg.Ecosystem))
	normalizedName, err := normalizeScannedPackageName(ecosystemName, pkg.Name)
	if err != nil {
		return advisoryIdentity{}, nil, 0, fmt.Errorf(
			"%w: normalize %s package identity: %v", ErrInvalidPackageScan, ecosystemName, err,
		)
	}
	identity := advisoryIdentity{
		ecosystem: ecosystemName,
		name:      normalizedName,
	}
	if identity.ecosystem == "" || identity.name == "" || OSVEcosystem(identity.ecosystem) == "" {
		return advisoryIdentity{}, nil, 0, fmt.Errorf(
			"%w: package ecosystem and name must identify an OSV-supported package",
			ErrInvalidPackageScan,
		)
	}

	byID := make(map[string]normalizedAdvisory, len(input))
	duplicates := 0
	for i, advisory := range input {
		advisory.ID = strings.TrimSpace(advisory.ID)
		if advisory.ID == "" {
			return advisoryIdentity{}, nil, 0, fmt.Errorf(
				"%w: advisory at index %d has no ID", ErrInvalidPackageScan, i,
			)
		}
		projected, err := projectScannedAdvisory(advisory, identity)
		if err != nil {
			return advisoryIdentity{}, nil, 0, err
		}

		model := ParseVulnerability(projected, identity.ecosystem)
		model.OSVID = advisory.ID
		model.Ecosystem = identity.ecosystem
		model.PackageName = identity.name
		incoming := normalizedAdvisory{model: *model}
		if existing, exists := byID[advisory.ID]; exists {
			duplicates++
			if shouldApplyAdvisory(existing.model.ModifiedAt, incoming.model.ModifiedAt) {
				byID[advisory.ID] = incoming
			}
			continue
		}
		byID[advisory.ID] = incoming
	}

	normalized := make([]normalizedAdvisory, 0, len(byID))
	for _, advisory := range byID {
		normalized = append(normalized, advisory)
	}
	sortNormalizedAdvisories(normalized)
	return identity, normalized, duplicates, nil
}

func projectScannedAdvisory(
	advisory OSVVulnerability,
	expected advisoryIdentity,
) (OSVVulnerability, error) {
	matching := make([]osvAffected, 0, len(advisory.Affected))
	hasDeclaredIdentity := false
	for _, affected := range advisory.Affected {
		if affected.Package == nil {
			continue
		}
		name := strings.TrimSpace(affected.Package.Name)
		osvEcosystem := strings.TrimSpace(affected.Package.Ecosystem)
		if name == "" && osvEcosystem == "" {
			continue
		}
		if name == "" || osvEcosystem == "" {
			return OSVVulnerability{}, fmt.Errorf(
				"%w: advisory %q has an incomplete affected package",
				ErrInvalidPackageScan, advisory.ID,
			)
		}
		hasDeclaredIdentity = true
		depsiloEcosystem := reverseEcosystem(osvEcosystem)
		if depsiloEcosystem == "" {
			continue
		}
		normalizedName, err := normalizeScannedPackageName(depsiloEcosystem, name)
		if err != nil {
			return OSVVulnerability{}, fmt.Errorf(
				"%w: advisory %q has invalid %s package identity: %v",
				ErrInvalidPackageScan, advisory.ID, depsiloEcosystem, err,
			)
		}
		if depsiloEcosystem == expected.ecosystem && normalizedName == expected.name {
			matching = append(matching, affected)
		}
	}
	if len(matching) == 0 && hasDeclaredIdentity {
		return OSVVulnerability{}, fmt.Errorf(
			"%w: advisory %q does not affect queried package %s/%s",
			ErrInvalidPackageScan,
			advisory.ID,
			expected.ecosystem,
			expected.name,
		)
	}
	projected := advisory
	projected.Affected = matching
	return projected, nil
}

func normalizeScannedPackageName(ecosystemName, packageName string) (string, error) {
	packageName = strings.TrimSpace(packageName)
	if len(packageName) > maxAdvisoryPackageNameBytes {
		return "", fmt.Errorf("package name exceeds %d bytes", maxAdvisoryPackageNameBytes)
	}
	dialect, err := packagepolicy.DialectFor(ecosystemName)
	if err != nil {
		return "", err
	}
	normalized, err := dialect.NormalizePackageName(packageName)
	if err != nil {
		return "", err
	}
	if len(normalized) > maxAdvisoryPackageNameBytes {
		return "", fmt.Errorf("normalized package name exceeds %d bytes", maxAdvisoryPackageNameBytes)
	}
	return normalized, nil
}

func decodeAdvisoryImport(src io.Reader) ([]OSVVulnerability, error) {
	data, err := io.ReadAll(io.LimitReader(src, maxAdvisoryImportBytes+1))
	if len(data) > maxAdvisoryImportBytes {
		return nil, ErrAdvisoryImportTooLarge
	}
	if err != nil {
		return nil, fmt.Errorf("read advisory import JSON: %w", err)
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%w: JSON is empty", ErrInvalidAdvisoryImport)
	}

	switch trimmed[0] {
	case '[':
		var advisories []OSVVulnerability
		if err := json.Unmarshal(trimmed, &advisories); err != nil {
			return nil, fmt.Errorf("%w: decode JSON array: %v", ErrInvalidAdvisoryImport, err)
		}
		if advisories == nil {
			return nil, fmt.Errorf("%w: JSON array is null", ErrInvalidAdvisoryImport)
		}
		return advisories, nil
	case '{':
		var advisory OSVVulnerability
		if err := json.Unmarshal(trimmed, &advisory); err != nil {
			return nil, fmt.Errorf("%w: decode JSON object: %v", ErrInvalidAdvisoryImport, err)
		}
		return []OSVVulnerability{advisory}, nil
	default:
		return nil, fmt.Errorf("%w: expected one JSON object or array", ErrInvalidAdvisoryImport)
	}
}

func normalizeAdvisoryImport(
	input []OSVVulnerability,
) ([]normalizedAdvisory, int, int, error) {
	byKey := make(map[advisoryKey]normalizedAdvisory, len(input))
	duplicates := 0
	skipped := 0
	for i, advisory := range input {
		advisory.ID = strings.TrimSpace(advisory.ID)
		if advisory.ID == "" {
			return nil, 0, 0, fmt.Errorf(
				"%w: advisory at index %d has no ID", ErrInvalidAdvisoryImport, i,
			)
		}

		projections, unsupported, err := projectImportedAdvisory(advisory)
		if err != nil {
			return nil, 0, 0, err
		}
		skipped += unsupported
		for _, projection := range projections {
			key := projection.key()
			if existing, exists := byKey[key]; exists {
				duplicates++
				if shouldApplyAdvisory(existing.model.ModifiedAt, projection.model.ModifiedAt) {
					byKey[key] = projection
				}
				continue
			}
			byKey[key] = projection
		}
	}

	normalized := make([]normalizedAdvisory, 0, len(byKey))
	for _, advisory := range byKey {
		normalized = append(normalized, advisory)
	}
	sortNormalizedAdvisories(normalized)
	return normalized, duplicates, skipped, nil
}

func shouldApplyAdvisory(existing, incoming time.Time) bool {
	if existing.IsZero() {
		return true
	}
	if incoming.IsZero() {
		return false
	}
	return incoming.After(existing)
}

func projectImportedAdvisory(advisory OSVVulnerability) ([]normalizedAdvisory, int, error) {
	if len(advisory.Affected) == 0 {
		return nil, 0, fmt.Errorf(
			"%w: advisory %q has no affected package", ErrInvalidAdvisoryImport, advisory.ID,
		)
	}

	byIdentity := make(map[advisoryIdentity][]osvAffected)
	unsupported := make(map[advisoryIdentity]struct{})
	for i, affected := range advisory.Affected {
		if affected.Package == nil {
			return nil, 0, fmt.Errorf(
				"%w: advisory %q affected entry %d has no package",
				ErrInvalidAdvisoryImport, advisory.ID, i,
			)
		}
		candidate := advisoryIdentity{
			ecosystem: strings.TrimSpace(affected.Package.Ecosystem),
			name:      strings.TrimSpace(affected.Package.Name),
		}
		if candidate.ecosystem == "" || candidate.name == "" {
			return nil, 0, fmt.Errorf(
				"%w: advisory %q affected entry %d has an incomplete package",
				ErrInvalidAdvisoryImport, advisory.ID, i,
			)
		}
		depsiloEcosystem := reverseEcosystem(candidate.ecosystem)
		if depsiloEcosystem == "" {
			unsupported[candidate] = struct{}{}
			continue
		}
		normalizedName, err := normalizeScannedPackageName(depsiloEcosystem, candidate.name)
		if err != nil {
			return nil, 0, fmt.Errorf(
				"%w: advisory %q affected entry %d has invalid %s package identity: %v",
				ErrInvalidAdvisoryImport, advisory.ID, i, depsiloEcosystem, err,
			)
		}
		identity := advisoryIdentity{ecosystem: depsiloEcosystem, name: normalizedName}
		byIdentity[identity] = append(byIdentity[identity], affected)
	}

	projections := make([]normalizedAdvisory, 0, len(byIdentity))
	for identity, affected := range byIdentity {
		projected := advisory
		projected.Affected = append([]osvAffected(nil), affected...)
		model := ParseVulnerability(projected, identity.ecosystem)
		model.OSVID = advisory.ID
		model.Ecosystem = identity.ecosystem
		model.PackageName = identity.name
		projections = append(projections, normalizedAdvisory{model: *model})
	}
	sortNormalizedAdvisories(projections)
	return projections, len(unsupported), nil
}

func sortNormalizedAdvisories(advisories []normalizedAdvisory) {
	sort.Slice(advisories, func(i, j int) bool {
		left, right := advisories[i].model, advisories[j].model
		if left.Ecosystem != right.Ecosystem {
			return left.Ecosystem < right.Ecosystem
		}
		if left.PackageName != right.PackageName {
			return left.PackageName < right.PackageName
		}
		return left.OSVID < right.OSVID
	})
}

type advisoryGroup struct {
	identity   advisoryIdentity
	advisories []normalizedAdvisory
}

func groupAdvisories(advisories []normalizedAdvisory) []advisoryGroup {
	groups := make([]advisoryGroup, 0)
	for _, advisory := range advisories {
		identity := advisory.identity()
		if len(groups) == 0 || groups[len(groups)-1].identity != identity {
			groups = append(groups, advisoryGroup{identity: identity})
		}
		last := len(groups) - 1
		groups[last].advisories = append(groups[last].advisories, advisory)
	}
	return groups
}

func reconcileAdvisories(
	tx *gorm.DB,
	advisories []normalizedAdvisory,
) ([]normalizedAdvisory, error) {
	stored, err := loadStoredAdvisories(tx, advisories)
	if err != nil {
		return nil, err
	}
	effective := make([]normalizedAdvisory, 0, len(advisories))
	for i := range advisories {
		incoming := advisories[i]
		if existing, exists := stored[incoming.key()]; exists &&
			!shouldApplyAdvisory(existing.ModifiedAt, incoming.model.ModifiedAt) {
			effective = append(effective, normalizedAdvisory{model: existing})
			continue
		}
		model := incoming.model
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "osv_id"},
				{Name: "ecosystem"},
				{Name: "package_name"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"ecosystem",
				"package_name",
				"affected_ranges",
				"severity",
				"cvss_score",
				"summary",
				"details",
				"aliases",
				"references",
				"published_at",
				"modified_at",
				"updated_at",
			}),
		}).Create(&model).Error; err != nil {
			return nil, err
		}
		effective = append(effective, incoming)
	}
	return effective, nil
}

func loadStoredAdvisories(
	tx *gorm.DB,
	advisories []normalizedAdvisory,
) (map[advisoryKey]db.Vulnerability, error) {
	storedByKey := make(map[advisoryKey]db.Vulnerability, len(advisories))
	if len(advisories) == 0 {
		return storedByKey, nil
	}

	seenIDs := make(map[string]struct{}, len(advisories))
	ids := make([]string, 0, len(advisories))
	for _, advisory := range advisories {
		if _, exists := seenIDs[advisory.model.OSVID]; exists {
			continue
		}
		seenIDs[advisory.model.OSVID] = struct{}{}
		ids = append(ids, advisory.model.OSVID)
	}
	sort.Strings(ids)
	for start := 0; start < len(ids); start += storedAdvisoryReadBatchSize {
		end := min(start+storedAdvisoryReadBatchSize, len(ids))
		var stored []db.Vulnerability
		if err := tx.Where("osv_id IN ?", ids[start:end]).Find(&stored).Error; err != nil {
			return nil, fmt.Errorf("query stored advisories: %w", err)
		}
		for _, vulnerability := range stored {
			normalized := normalizedAdvisory{model: vulnerability}
			storedByKey[normalized.key()] = vulnerability
		}
	}
	return storedByKey, nil
}

func upsertVulnerabilityCheck(
	tx *gorm.DB,
	identity advisoryIdentity,
	count int,
	now time.Time,
	ttl time.Duration,
) error {
	check := db.VulnerabilityCheck{
		Ecosystem:          identity.ecosystem,
		PackageName:        identity.name,
		HasVulnerabilities: count > 0,
		VulnerabilityCount: count,
		LastFetchedAt:      now,
		NextFetchAt:        now.Add(ttl),
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "ecosystem"}, {Name: "package_name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"has_vulnerabilities",
			"vulnerability_count",
			"last_fetched_at",
			"next_fetch_at",
			"updated_at",
		}),
	}).Create(&check).Error
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
