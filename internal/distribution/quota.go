package distribution

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"depsilo/internal/db"
	"gorm.io/gorm"
)

// The free edition can use every transfer feature at a bounded scale; Pro
// removes these export limits. Imports are never limited: an importing
// instance is often isolated and cannot be expected to validate a license.
const (
	// FreeExportBundleBytes is the largest transfer bundle the free edition exports.
	FreeExportBundleBytes int64 = 500 << 20
	// FreeExportMonthlyBytes caps the free edition's metered exports per UTC
	// calendar month.
	FreeExportMonthlyBytes int64 = 5 << 30

	exportReserved  = "reserved"
	exportCompleted = "completed"
)

// ExportLimits bounds metered exports.
type ExportLimits struct {
	// BundleBytes is the largest bundle one export may stream.
	BundleBytes int64
	// MonthlyBytes caps the metered exports of one UTC calendar month.
	MonthlyBytes int64
}

// FreeExportLimits returns the free edition's export limits.
func FreeExportLimits() ExportLimits {
	return ExportLimits{BundleBytes: FreeExportBundleBytes, MonthlyBytes: FreeExportMonthlyBytes}
}

var (
	ErrExportBundleLimit  = errors.New("transfer bundle exceeds the free edition's bundle size")
	ErrExportMonthlyLimit = errors.New("transfer bundle exceeds the free edition's monthly export allowance")
)

// ExportLimitError explains a refused export. Err is ErrExportBundleLimit or
// ErrExportMonthlyLimit.
type ExportLimitError struct {
	Err          error
	BundleBytes  int64
	BundleLimit  int64
	MonthlyLimit int64
	UsedBytes    int64
	ResetsAt     time.Time
}

func (e *ExportLimitError) Error() string { return e.Err.Error() }

func (e *ExportLimitError) Unwrap() error { return e.Err }

// ExportUsage reports the current UTC month's export allowance. UsedBytes
// counts metered exports only; limits are zero when exports are unlimited.
type ExportUsage struct {
	Unlimited    bool      `json:"unlimited"`
	BundleLimit  int64     `json:"bundle_limit_bytes,omitempty"`
	MonthlyLimit int64     `json:"monthly_limit_bytes,omitempty"`
	UsedBytes    int64     `json:"used_bytes"`
	ResetsAt     time.Time `json:"resets_at"`
}

// ExportQuota meters transfer-bundle exports in the export ledger. An export
// reserves its size before streaming, so concurrent exports cannot together
// exceed the monthly allowance, and only a completely written bundle stays
// recorded.
type ExportQuota struct {
	database  *gorm.DB
	unlimited func() bool
	limits    ExportLimits
	now       func() time.Time
	mu        sync.Mutex
}

// NewExportQuota meters exports against limits unless unlimited reports true
// (an active Pro license or trial). A reservation cannot outlive the process
// that streamed it, so reservations left by a previous process are discarded.
func NewExportQuota(database *gorm.DB, unlimited func() bool, limits ExportLimits) (*ExportQuota, error) {
	if database == nil {
		return nil, errors.New("export quota requires a database")
	}
	if limits.BundleBytes <= 0 || limits.MonthlyBytes < limits.BundleBytes {
		return nil, errors.New("export limits must allow at least one full bundle per month")
	}
	if err := database.Where("state = ?", exportReserved).Delete(&db.DistributionExport{}).Error; err != nil {
		return nil, fmt.Errorf("discard interrupted export reservations: %w", err)
	}
	return &ExportQuota{database: database, unlimited: unlimited, limits: limits, now: time.Now}, nil
}

func (q *ExportQuota) isUnlimited() bool { return q.unlimited != nil && q.unlimited() }

func exportMonth(now time.Time) (start, resets time.Time) {
	now = now.UTC()
	start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

func (q *ExportQuota) meteredSince(ctx context.Context, start time.Time) (int64, error) {
	var used int64
	err := q.database.WithContext(ctx).Model(&db.DistributionExport{}).
		Where("metered = ? AND started_at >= ?", true, start).
		Select("COALESCE(SUM(bytes), 0)").Scan(&used).Error
	return used, err
}

// Usage returns the current month's allowance.
func (q *ExportQuota) Usage(ctx context.Context) (ExportUsage, error) {
	start, resets := exportMonth(q.now())
	used, err := q.meteredSince(ctx, start)
	if err != nil {
		return ExportUsage{}, err
	}
	usage := ExportUsage{Unlimited: q.isUnlimited(), UsedBytes: used, ResetsAt: resets}
	if !usage.Unlimited {
		usage.BundleLimit, usage.MonthlyLimit = q.limits.BundleBytes, q.limits.MonthlyBytes
	}
	return usage, nil
}

// ExportReservation is one export's ledger row while its bundle streams.
type ExportReservation struct {
	quota *ExportQuota
	id    uint
}

// Reserve records an export of size bytes, or returns an *ExportLimitError
// when a metered export would exceed the bundle size or the monthly allowance.
func (q *ExportQuota) Reserve(ctx context.Context, setID string, size int64, actor string) (*ExportReservation, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().UTC()
	metered := !q.isUnlimited()
	if metered {
		start, resets := exportMonth(now)
		used, err := q.meteredSince(ctx, start)
		if err != nil {
			return nil, err
		}
		refusal := &ExportLimitError{BundleBytes: size, BundleLimit: q.limits.BundleBytes,
			MonthlyLimit: q.limits.MonthlyBytes, UsedBytes: used, ResetsAt: resets}
		switch {
		case size > q.limits.BundleBytes:
			refusal.Err = ErrExportBundleLimit
			return nil, refusal
		case used+size > q.limits.MonthlyBytes:
			refusal.Err = ErrExportMonthlyLimit
			return nil, refusal
		}
	}
	row := db.DistributionExport{SetID: setID, Bytes: size, State: exportReserved, Metered: metered, Actor: actor, StartedAt: now}
	if err := q.database.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, fmt.Errorf("record export reservation: %w", err)
	}
	return &ExportReservation{quota: q, id: row.ID}, nil
}

// Complete keeps the reservation as a finished export.
func (r *ExportReservation) Complete(ctx context.Context) error {
	finished := r.quota.now().UTC()
	return r.quota.database.WithContext(ctx).Model(&db.DistributionExport{}).
		Where("id = ? AND state = ?", r.id, exportReserved).
		Updates(map[string]any{"state": exportCompleted, "finished_at": finished}).Error
}

// Release drops the reservation of an export that did not finish; a short or
// failed stream is not a usable bundle and does not count.
func (r *ExportReservation) Release(ctx context.Context) error {
	return r.quota.database.WithContext(ctx).
		Where("id = ? AND state = ?", r.id, exportReserved).
		Delete(&db.DistributionExport{}).Error
}
