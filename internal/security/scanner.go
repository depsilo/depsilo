package security

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

var (
	ErrScanInProgress    = errors.New("scan already in progress")
	ErrScannerClosed     = errors.New("security scanner is closed")
	errNoAdvisoryCatalog = errors.New("security scanner has no advisory catalog")
)

// Scanner coordinates vulnerability scanning.
type Scanner struct {
	db       *gorm.DB
	fetcher  *Fetcher
	catalog  *AdvisoryCatalog
	scanning atomic.Bool
	lastScan atomic.Pointer[time.Time]

	lifecycleMu sync.Mutex
	lifecycleWG sync.WaitGroup
	closed      bool
	stopCtx     context.Context
	stop        context.CancelFunc
}

// NewScanner creates a new vulnerability scanner.
func NewScanner(database *gorm.DB, fetcher *Fetcher, catalog *AdvisoryCatalog) *Scanner {
	stopCtx, stop := context.WithCancel(context.Background())
	return &Scanner{
		db:      database,
		fetcher: fetcher,
		catalog: catalog,
		stopCtx: stopCtx,
		stop:    stop,
	}
}

// IsScanning returns true if a scan is currently in progress.
func (s *Scanner) IsScanning() bool {
	return s.scanning.Load()
}

// LastScanTime returns when the last full scan completed.
func (s *Scanner) LastScanTime() time.Time {
	lastScan := s.lastScan.Load()
	if lastScan == nil {
		return time.Time{}
	}
	return *lastScan
}

// ScanAll scans all cached packages that need a vulnerability check refresh.
func (s *Scanner) ScanAll(ctx context.Context) error {
	scanCtx, finish, err := s.beginScan(ctx, 0)
	if err != nil {
		return err
	}
	defer finish()
	return s.scanAll(scanCtx)
}

// StartScan atomically reserves the scanner before starting an asynchronous
// scan. The returned nil guarantees IsScanning is already true, so an HTTP
// handler can safely acknowledge the job without a check-then-start race.
func (s *Scanner) StartScan(ctx context.Context, timeout time.Duration) error {
	scanCtx, finish, err := s.beginScan(ctx, timeout)
	if err != nil {
		return err
	}
	go func() {
		defer finish()
		if err := s.scanAll(scanCtx); err != nil && !errors.Is(err, context.Canceled) {
			zap.L().Error("manual security scan failed", zap.Error(err))
		}
	}()
	return nil
}

func (s *Scanner) beginScan(parent context.Context, timeout time.Duration) (context.Context, func(), error) {
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}

	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.closed {
		return nil, nil, ErrScannerClosed
	}
	if !s.scanning.CompareAndSwap(false, true) {
		return nil, nil, ErrScanInProgress
	}

	var scanCtx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		scanCtx, cancel = context.WithTimeout(parent, timeout)
	} else {
		scanCtx, cancel = context.WithCancel(parent)
	}
	stopLink := context.AfterFunc(s.stopCtx, cancel)
	s.lifecycleWG.Add(1)

	var finishOnce sync.Once
	finish := func() {
		finishOnce.Do(func() {
			stopLink()
			cancel()
			s.scanning.Store(false)
			s.lifecycleWG.Done()
		})
	}
	return scanCtx, finish, nil
}

// Close prevents new scans, cancels active scans, and waits until they no
// longer use the database. It is safe to call more than once.
func (s *Scanner) Close(ctx context.Context) error {
	s.lifecycleMu.Lock()
	if !s.closed {
		s.closed = true
		s.stop()
	}
	s.lifecycleMu.Unlock()

	done := make(chan struct{})
	go func() {
		s.lifecycleWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Scanner) scanAll(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	zap.L().Info("security scan started")
	start := time.Now()

	// Get distinct packages from cache
	type pkgRow struct {
		AdapterType string
		PackageName string
	}
	var packages []pkgRow
	if err := s.db.WithContext(ctx).Model(&db.CacheEntry{}).
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
		err := s.db.WithContext(ctx).
			Where("ecosystem = ? AND package_name = ?", pkg.AdapterType, pkg.PackageName).
			First(&check).Error
		if err == nil && check.NextFetchAt.After(now) {
			continue
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("query vulnerability check for %s/%s: %w", pkg.AdapterType, pkg.PackageName, err)
		}
		toScan = append(toScan, PackageRef{Ecosystem: pkg.AdapterType, Name: pkg.PackageName})
	}

	if len(toScan) == 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		zap.L().Info("security scan complete, all packages up to date")
		s.storeLastScan(time.Now())
		return nil
	}
	if s.fetcher == nil {
		return fmt.Errorf("security scanner has no OSV fetcher")
	}

	zap.L().Info("scanning packages", zap.Int("count", len(toScan)))

	// Group by ecosystem for batch queries
	byEcosystem := make(map[string][]PackageRef)
	for _, pkg := range toScan {
		byEcosystem[pkg.Ecosystem] = append(byEcosystem[pkg.Ecosystem], pkg)
	}

	totalVulns := 0
	var scanErrors []error

scanBatches:
	for eco, pkgs := range byEcosystem {
		for i := 0; i < len(pkgs); i += 1000 {
			if err := ctx.Err(); err != nil {
				scanErrors = append(scanErrors, err)
				break scanBatches
			}
			end := i + 1000
			if end > len(pkgs) {
				end = len(pkgs)
			}
			batch := pkgs[i:end]

			results, err := s.fetcher.QueryBatch(ctx, batch)
			if err != nil {
				zap.L().Error("batch query failed", zap.String("ecosystem", eco), zap.Error(err))
				scanErrors = append(scanErrors, fmt.Errorf("query OSV batch for %s: %w", eco, err))
				if ctx.Err() != nil {
					break scanBatches
				}
				continue
			}

			if len(results) != len(batch) {
				scanErrors = append(scanErrors, fmt.Errorf(
					"query OSV batch for %s: result count %d does not match package count %d",
					eco, len(results), len(batch),
				))
			}
			resultCount := len(results)
			if resultCount > len(batch) {
				resultCount = len(batch)
			}
			for j := 0; j < resultCount; j++ {
				pkg := batch[j]
				count, err := s.recordScan(ctx, pkg, results[j])
				if err != nil {
					zap.L().Error("store scan results failed",
						zap.String("ecosystem", pkg.Ecosystem),
						zap.String("package", pkg.Name),
						zap.Error(err),
					)
					scanErrors = append(scanErrors, fmt.Errorf(
						"store scan results for %s/%s: %w", pkg.Ecosystem, pkg.Name, err,
					))
					continue
				}
				totalVulns += count
			}
		}
	}

	if len(scanErrors) > 0 {
		return errors.Join(scanErrors...)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.storeLastScan(time.Now())
	zap.L().Info("security scan complete",
		zap.Int("packages", len(toScan)),
		zap.Int("vulnerabilities", totalVulns),
		zap.Duration("duration", time.Since(start)),
	)
	return nil
}

func (s *Scanner) storeLastScan(scanTime time.Time) {
	s.lastScan.Store(&scanTime)
}

// ScanPackage scans a single package for vulnerabilities.
func (s *Scanner) ScanPackage(ctx context.Context, ecosystem, packageName string) error {
	if OSVEcosystem(ecosystem) == "" || packageName == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	var check db.VulnerabilityCheck
	err := s.db.WithContext(ctx).
		Where("ecosystem = ? AND package_name = ?", ecosystem, packageName).
		First(&check).Error
	if err == nil && check.NextFetchAt.After(time.Now()) {
		return nil // still fresh
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("query vulnerability check for %s/%s: %w", ecosystem, packageName, err)
	}
	if s.fetcher == nil {
		return fmt.Errorf("security scanner has no OSV fetcher")
	}

	vulns, err := s.fetcher.Query(ctx, ecosystem, packageName)
	if err != nil {
		return fmt.Errorf("query OSV for %s/%s: %w", ecosystem, packageName, err)
	}
	_, err = s.recordScan(ctx, PackageRef{Ecosystem: ecosystem, Name: packageName}, vulns)
	if err != nil {
		return fmt.Errorf("store scan results for %s/%s: %w", ecosystem, packageName, err)
	}
	return nil
}

func (s *Scanner) recordScan(ctx context.Context, pkg PackageRef, advisories []OSVVulnerability) (int, error) {
	if s.catalog == nil {
		return 0, errNoAdvisoryCatalog
	}
	receipt, err := s.catalog.RecordScan(ctx, pkg, advisories)
	return receipt.Advisories, err
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
