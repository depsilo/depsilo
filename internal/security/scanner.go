package security

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/db"
)

// Scanner coordinates vulnerability scanning.
type Scanner struct {
	db       *gorm.DB
	fetcher  *Fetcher
	checkTTL time.Duration
	scanning atomic.Bool
	lastScan time.Time
}

// NewScanner creates a new vulnerability scanner.
func NewScanner(database *gorm.DB, fetcher *Fetcher, checkTTL time.Duration) *Scanner {
	return &Scanner{
		db:       database,
		fetcher:  fetcher,
		checkTTL: checkTTL,
	}
}

// IsScanning returns true if a scan is currently in progress.
func (s *Scanner) IsScanning() bool {
	return s.scanning.Load()
}

// LastScanTime returns when the last full scan completed.
func (s *Scanner) LastScanTime() time.Time {
	return s.lastScan
}

// ScanAll scans all cached packages that need a vulnerability check refresh.
func (s *Scanner) ScanAll(ctx context.Context) error {
	if !s.scanning.CompareAndSwap(false, true) {
		return fmt.Errorf("scan already in progress")
	}
	defer s.scanning.Store(false)

	zap.L().Info("security scan started")
	start := time.Now()

	// Get distinct packages from cache
	type pkgRow struct {
		AdapterType string
		PackageName string
	}
	var packages []pkgRow
	if err := s.db.Model(&db.CacheEntry{}).
		Select("DISTINCT adapter_type, package_name").
		Where("package_name != ''").
		Find(&packages).Error; err != nil {
		return fmt.Errorf("query cached packages: %w", err)
	}

	// Filter: skip unsupported ecosystems and packages with fresh checks
	now := time.Now()
	var toScan []PackageRef
	for _, pkg := range packages {
		if OSVEcosystem(pkg.AdapterType) == "" {
			continue
		}
		var check db.VulnerabilityCheck
		err := s.db.Where("ecosystem = ? AND package_name = ?", pkg.AdapterType, pkg.PackageName).First(&check).Error
		if err == nil && check.NextFetchAt.After(now) {
			continue
		}
		toScan = append(toScan, PackageRef{Ecosystem: pkg.AdapterType, Name: pkg.PackageName})
	}

	if len(toScan) == 0 {
		zap.L().Info("security scan complete, all packages up to date")
		s.lastScan = time.Now()
		return nil
	}

	zap.L().Info("scanning packages", zap.Int("count", len(toScan)))

	// Group by ecosystem for batch queries
	byEcosystem := make(map[string][]PackageRef)
	for _, pkg := range toScan {
		byEcosystem[pkg.Ecosystem] = append(byEcosystem[pkg.Ecosystem], pkg)
	}

	totalVulns := 0
	for eco, pkgs := range byEcosystem {
		for i := 0; i < len(pkgs); i += 1000 {
			end := i + 1000
			if end > len(pkgs) {
				end = len(pkgs)
			}
			batch := pkgs[i:end]

			results, err := s.fetcher.QueryBatch(ctx, batch)
			if err != nil {
				zap.L().Error("batch query failed", zap.String("ecosystem", eco), zap.Error(err))
				continue
			}

			for j, vulns := range results {
				pkg := batch[j]
				count := s.processResults(pkg.Ecosystem, pkg.Name, vulns)
				totalVulns += count
			}
		}
	}

	s.lastScan = time.Now()
	zap.L().Info("security scan complete",
		zap.Int("packages", len(toScan)),
		zap.Int("vulnerabilities", totalVulns),
		zap.Duration("duration", time.Since(start)),
	)
	return nil
}

// ScanPackage scans a single package for vulnerabilities.
func (s *Scanner) ScanPackage(ctx context.Context, ecosystem, packageName string) error {
	if OSVEcosystem(ecosystem) == "" || packageName == "" {
		return nil
	}

	var check db.VulnerabilityCheck
	err := s.db.Where("ecosystem = ? AND package_name = ?", ecosystem, packageName).First(&check).Error
	if err == nil && check.NextFetchAt.After(time.Now()) {
		return nil // still fresh
	}

	vulns, err := s.fetcher.Query(ctx, ecosystem, packageName)
	if err != nil {
		return fmt.Errorf("query OSV for %s/%s: %w", ecosystem, packageName, err)
	}

	s.processResults(ecosystem, packageName, vulns)
	return nil
}

// processResults stores vulnerabilities and updates the check record.
func (s *Scanner) processResults(ecosystem, packageName string, vulns []OSVVulnerability) int {
	now := time.Now()

	for _, v := range vulns {
		parsed := ParseVulnerability(v, ecosystem)
		if parsed.PackageName == "" {
			parsed.PackageName = packageName
		}
		if parsed.Ecosystem == "" {
			parsed.Ecosystem = ecosystem
		}
		s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "osv_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"affected_ranges", "severity", "cvss_score", "summary", "details", "aliases", "references", "modified_at", "updated_at"}),
		}).Create(parsed)
	}

	s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ecosystem"}, {Name: "package_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"has_vulnerabilities", "vulnerability_count", "last_fetched_at", "next_fetch_at", "updated_at"}),
	}).Create(&db.VulnerabilityCheck{
		Ecosystem:          ecosystem,
		PackageName:        packageName,
		HasVulnerabilities: len(vulns) > 0,
		VulnerabilityCount: len(vulns),
		LastFetchedAt:      now,
		NextFetchAt:        now.Add(s.checkTTL),
	})

	if len(vulns) > 0 {
		s.checkAutoBlock(ecosystem, packageName, vulns)
	}

	return len(vulns)
}

// checkAutoBlock creates deny rules for vulnerabilities exceeding the CVSS threshold.
func (s *Scanner) checkAutoBlock(ecosystem, packageName string, vulns []OSVVulnerability) {
	var policy db.SecurityPolicy
	if err := s.db.Where("ecosystem = ? AND auto_block_enabled = ?", ecosystem, true).First(&policy).Error; err != nil {
		return
	}

	for _, v := range vulns {
		parsed := ParseVulnerability(v, ecosystem)
		if parsed.CVSSScore < policy.MinCVSSScore {
			continue
		}

		var count int64
		s.db.Model(&db.PackageRule{}).Where(
			"ecosystem = ? AND package_name = ? AND created_by = 'security-scanner' AND reason LIKE ?",
			ecosystem, packageName, "%"+v.ID+"%",
		).Count(&count)
		if count > 0 {
			continue
		}

		constraint := FormatVersionConstraint(parsed)
		rule := db.PackageRule{
			Ecosystem:   ecosystem,
			PackageName: packageName,
			Version:     constraint,
			Action:      "deny",
			Reason:      fmt.Sprintf("Auto-blocked: %s (CVSS %.1f)", v.ID, parsed.CVSSScore),
			CreatedBy:   "security-scanner",
		}
		if err := s.db.Create(&rule).Error; err != nil {
			zap.L().Warn("failed to create auto-block rule",
				zap.String("package", packageName), zap.String("vuln", v.ID), zap.Error(err))
		} else {
			zap.L().Info("auto-blocked vulnerable package",
				zap.String("ecosystem", ecosystem), zap.String("package", packageName),
				zap.String("vuln", v.ID), zap.Float32("cvss", parsed.CVSSScore))
		}
	}
}

// StartBackgroundScan runs periodic full scans.
func StartBackgroundScan(ctx context.Context, scanner *Scanner, interval time.Duration) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}

	if err := scanner.ScanAll(ctx); err != nil {
		zap.L().Error("initial security scan failed", zap.Error(err))
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := scanner.ScanAll(ctx); err != nil {
				zap.L().Error("periodic security scan failed", zap.Error(err))
			}
		}
	}
}
