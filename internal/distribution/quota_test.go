package distribution

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/db"
	"gorm.io/gorm"
)

func newQuotaTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	return database
}

func newTestQuota(t *testing.T, database *gorm.DB, pro *atomic.Bool, now time.Time) *ExportQuota {
	t.Helper()
	quota, err := NewExportQuota(database, pro.Load, FreeExportLimits())
	if err != nil {
		t.Fatal(err)
	}
	quota.now = func() time.Time { return now }
	return quota
}

func exportedBytes(t *testing.T, quota *ExportQuota) int64 {
	t.Helper()
	usage, err := quota.Usage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return usage.UsedBytes
}

func TestExportQuotaLimitsFreeBundleSizeAndMonthlyTotal(t *testing.T) {
	database := newQuotaTestDB(t)
	var pro atomic.Bool
	now := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	if _, err := NewExportQuota(database, pro.Load, ExportLimits{BundleBytes: 2, MonthlyBytes: 1}); err == nil {
		t.Fatal("accepted a monthly allowance smaller than one bundle")
	}
	quota := newTestQuota(t, database, &pro, now)
	ctx := context.Background()

	_, err := quota.Reserve(ctx, "set-a", FreeExportBundleBytes+1, "admin")
	var refusal *ExportLimitError
	if !errors.As(err, &refusal) || !errors.Is(err, ErrExportBundleLimit) || refusal.BundleBytes != FreeExportBundleBytes+1 ||
		!refusal.ResetsAt.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("oversized free export = %+v, %v", refusal, err)
	}

	// Full-size bundles fit the monthly allowance until less than one remains.
	for i := 0; i < int(FreeExportMonthlyBytes/FreeExportBundleBytes); i++ {
		reservation, err := quota.Reserve(ctx, "set-a", FreeExportBundleBytes, "admin")
		if err != nil {
			t.Fatalf("export %d within the allowance: %v", i+1, err)
		}
		if err := reservation.Complete(ctx); err != nil {
			t.Fatal(err)
		}
	}
	remaining := FreeExportMonthlyBytes - exportedBytes(t, quota)
	if _, err := quota.Reserve(ctx, "set-a", remaining+1, "admin"); !errors.As(err, &refusal) ||
		!errors.Is(err, ErrExportMonthlyLimit) || refusal.UsedBytes != FreeExportMonthlyBytes-remaining {
		t.Fatalf("export beyond the remaining allowance = %+v, %v", refusal, err)
	}
	last, err := quota.Reserve(ctx, "set-a", remaining, "admin")
	if err != nil {
		t.Fatalf("export of exactly the remaining allowance: %v", err)
	}
	if err := last.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := quota.Reserve(ctx, "set-a", 1, "admin"); !errors.Is(err, ErrExportMonthlyLimit) {
		t.Fatalf("export beyond the monthly allowance = %v", err)
	}
	usage, err := quota.Usage(ctx)
	if err != nil || usage.Unlimited || usage.UsedBytes != FreeExportMonthlyBytes ||
		usage.BundleLimit != FreeExportBundleBytes || usage.MonthlyLimit != FreeExportMonthlyBytes {
		t.Fatalf("usage = %+v, %v", usage, err)
	}

	// The allowance renews with the UTC calendar month.
	nextMonth := newTestQuota(t, database, &pro, time.Date(2026, 12, 1, 0, 0, 1, 0, time.UTC))
	if _, err := nextMonth.Reserve(ctx, "set-a", FreeExportBundleBytes, "admin"); err != nil {
		t.Fatalf("export in a new month: %v", err)
	}
}

func TestExportQuotaDoesNotMeterProExports(t *testing.T) {
	database := newQuotaTestDB(t)
	var pro atomic.Bool
	pro.Store(true)
	quota := newTestQuota(t, database, &pro, time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC))
	ctx := context.Background()

	reservation, err := quota.Reserve(ctx, "set-a", 3*FreeExportMonthlyBytes, "admin")
	if err != nil {
		t.Fatalf("Pro export: %v", err)
	}
	if err := reservation.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	usage, err := quota.Usage(ctx)
	if err != nil || !usage.Unlimited || usage.UsedBytes != 0 || usage.BundleLimit != 0 {
		t.Fatalf("Pro usage = %+v, %v", usage, err)
	}

	// Exports made under Pro do not consume the free allowance after expiry.
	pro.Store(false)
	if _, err := quota.Reserve(ctx, "set-a", FreeExportBundleBytes, "admin"); err != nil {
		t.Fatalf("free export after Pro expiry: %v", err)
	}
	var recorded int64
	if err := database.Model(&db.DistributionExport{}).Count(&recorded).Error; err != nil || recorded != 2 {
		t.Fatalf("ledger rows = %d, %v", recorded, err)
	}
}

func TestExportQuotaCountsOnlyFinishedOrInFlightExports(t *testing.T) {
	database := newQuotaTestDB(t)
	var pro atomic.Bool
	now := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	quota := newTestQuota(t, database, &pro, now)
	ctx := context.Background()

	failed, err := quota.Reserve(ctx, "set-a", FreeExportBundleBytes, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if exportedBytes(t, quota) != FreeExportBundleBytes {
		t.Fatal("an in-flight export did not hold its reservation")
	}
	if err := failed.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if used := exportedBytes(t, quota); used != 0 {
		t.Fatalf("a failed export still counts %d bytes", used)
	}

	// A reservation left by a stopped process is discarded on startup.
	if _, err := quota.Reserve(ctx, "set-a", FreeExportBundleBytes, "admin"); err != nil {
		t.Fatal(err)
	}
	restarted := newTestQuota(t, database, &pro, now)
	if used := exportedBytes(t, restarted); used != 0 {
		t.Fatalf("an interrupted export still counts %d bytes after restart", used)
	}
}

func TestExportQuotaSerializesConcurrentReservations(t *testing.T) {
	database := newQuotaTestDB(t)
	var pro atomic.Bool
	quota := newTestQuota(t, database, &pro, time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC))
	ctx := context.Background()

	var wg sync.WaitGroup
	var accepted atomic.Int32
	allowed := int(FreeExportMonthlyBytes / FreeExportBundleBytes)
	for i := 0; i < allowed+4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := quota.Reserve(ctx, "set-a", FreeExportBundleBytes, "admin"); err == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if got, want := accepted.Load(), int32(allowed); got != want {
		t.Fatalf("concurrent reservations accepted = %d, want %d", got, want)
	}
}
