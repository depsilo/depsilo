package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// chainBaseKey names the control_plane_states row that records the last
// audit row removed by retention. Its hash is the prev_hash the first
// retained row must carry, so the chain stays verifiable after pruning.
const chainBaseKey = "audit_chain_base"

const (
	retentionBatchSize = 1000
	retentionInterval  = time.Hour
	// retentionBatchPause leaves the write lock free between batches so a
	// large first sweep after an upgrade does not starve audit appends.
	retentionBatchPause = 20 * time.Millisecond
	// retentionBatchAttempts bounds reruns of a batch that lost its read
	// snapshot to a concurrent audit append.
	retentionBatchAttempts = 4
)

// ChainBase is the last audit row deleted by retention. The chain resumes
// from Hash: the first retained row (or the next appended row, when every row
// was pruned) links to it, and new ids continue after ID.
type ChainBase struct {
	ID        uint      `json:"id"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"created_at"`
}

// LoadChainBase returns the recorded retention base. ok is false when no
// audit row has ever been pruned.
func LoadChainBase(ctx context.Context, database *gorm.DB) (ChainBase, bool, error) {
	database = database.WithContext(ctx)
	if !database.Migrator().HasTable(&db.ControlPlaneState{}) {
		// Only partial test schemas lack the table; nothing was ever pruned.
		return ChainBase{}, false, nil
	}
	var states []db.ControlPlaneState
	if err := database.Where("key = ?", chainBaseKey).Limit(1).Find(&states).Error; err != nil {
		return ChainBase{}, false, fmt.Errorf("read audit chain base: %w", err)
	}
	if len(states) == 0 {
		return ChainBase{}, false, nil
	}
	var base ChainBase
	if err := json.Unmarshal([]byte(states[0].Value), &base); err != nil {
		return ChainBase{}, false, fmt.Errorf("decode audit chain base: %w", err)
	}
	return base, true, nil
}

func saveChainBase(tx *gorm.DB, base ChainBase) error {
	encoded, err := json.Marshal(base)
	if err != nil {
		return err
	}
	return tx.Save(&db.ControlPlaneState{Key: chainBaseKey, Value: string(encoded), UpdatedAt: time.Now().UTC()}).Error
}

// Retention deletes the oldest audit rows once they pass the configured age.
// It removes only a prefix by id, never a row an enabled SIEM exporter has
// not delivered, and never past a row whose link it cannot verify, so
// pruning cannot hide tampering. Each deleted batch records the chain base in
// the same transaction.
type Retention struct {
	db       *gorm.DB
	days     int
	batch    int
	pause    time.Duration
	interval time.Duration
	now      func() time.Time
}

// NewRetention creates the sweeper. days <= 0 keeps audit rows forever.
func NewRetention(database *gorm.DB, days int) *Retention {
	return &Retention{
		db:       database,
		days:     days,
		batch:    retentionBatchSize,
		pause:    retentionBatchPause,
		interval: retentionInterval,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// Enabled reports whether the sweeper deletes anything.
func (r *Retention) Enabled() bool {
	return r != nil && r.days > 0
}

// Start sweeps once and then every interval until ctx is cancelled.
func (r *Retention) Start(ctx context.Context) {
	if !r.Enabled() {
		return
	}
	r.sweep(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sweep(ctx)
		}
	}
}

func (r *Retention) sweep(ctx context.Context) {
	deleted, err := r.RunOnce(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		zap.L().Warn("audit retention sweep failed", zap.Int64("deleted", deleted), zap.Error(err))
		return
	}
	if deleted > 0 {
		zap.L().Info("audit retention pruned audit_logs", zap.Int64("rows", deleted), zap.Int("retention_days", r.days))
	}
}

// RunOnce deletes every row older than the retention horizon that is safe to
// delete. It returns the number of rows deleted.
func (r *Retention) RunOnce(ctx context.Context) (int64, error) {
	if !r.Enabled() {
		return 0, nil
	}
	return r.PruneBefore(ctx, r.now().AddDate(0, 0, -r.days))
}

// PruneBefore deletes, in bounded batches, the longest prefix of audit rows
// (by id) created before cutoff, stopping at the first row an enabled
// exporter still has to deliver or whose chain link does not verify.
func (r *Retention) PruneBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	batch := r.batch
	if batch <= 0 {
		batch = retentionBatchSize
	}
	var total int64
	for {
		deleted, more, err := r.pruneBatchWithRetry(ctx, cutoff, batch)
		total += deleted
		if err != nil {
			return total, err
		}
		if !more {
			return total, nil
		}
		if err := sleepContext(ctx, r.pause); err != nil {
			return total, err
		}
	}
}

func (r *Retention) pruneBatchWithRetry(ctx context.Context, cutoff time.Time, batch int) (int64, bool, error) {
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return 0, false, err
		}
		deleted, more, err := r.pruneBatch(ctx, cutoff, batch)
		if err == nil || !db.IsSQLiteBusy(err) || attempt == retentionBatchAttempts {
			return deleted, more, err
		}
		if err := sleepContext(ctx, time.Duration(attempt)*5*time.Millisecond); err != nil {
			return 0, false, err
		}
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// pruneBatch deletes at most one batch and reports whether the next batch may
// also be deletable. A link that does not verify stops pruning after the
// intact rows before it, so the break stays visible to VerifyChain.
func (r *Retention) pruneBatch(ctx context.Context, cutoff time.Time, batch int) (int64, bool, error) {
	var deleted int64
	more := false
	var broken error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		deleted, more, broken = 0, false, nil
		base, hasBase, err := LoadChainBase(ctx, tx)
		if err != nil {
			return err
		}
		var undelivered sql.NullInt64
		if err := tx.Model(&db.AuditExporter{}).Where("enabled = ?", true).
			Select("MIN(cursor)").Scan(&undelivered).Error; err != nil {
			return fmt.Errorf("read audit exporter cursors: %w", err)
		}
		// Scan the oldest rows themselves, not only expired ones: the first
		// row that must stay ends the prefix.
		var oldest []db.AuditLog
		if err := tx.Order("id").Limit(batch).Find(&oldest).Error; err != nil {
			return fmt.Errorf("read oldest audit rows: %w", err)
		}
		prev := ""
		started := false
		if hasBase {
			prev, started = base.Hash, base.Hash != ""
		}
		var last *db.AuditLog
		for index := range oldest {
			row := &oldest[index]
			if !row.CreatedAt.Before(cutoff) {
				break
			}
			if undelivered.Valid && int64(row.ID) > undelivered.Int64 {
				break
			}
			if hasBase && row.ID <= base.ID {
				broken = fmt.Errorf("audit retention stopped at row %d: row precedes the pruned chain base", row.ID)
				break
			}
			if reason := chainLinkFailure(prev, *row, started); reason != "" {
				broken = fmt.Errorf("audit retention stopped at row %d: %s", row.ID, reason)
				break
			}
			if row.Hash != "" {
				started = true
				prev = row.Hash
			}
			last = row
			deleted++
		}
		if last == nil {
			return nil
		}
		if err := tx.Where("id <= ?", last.ID).Delete(&db.AuditLog{}).Error; err != nil {
			return fmt.Errorf("delete expired audit rows: %w", err)
		}
		if err := saveChainBase(tx, ChainBase{ID: last.ID, Hash: last.Hash, CreatedAt: last.CreatedAt.UTC()}); err != nil {
			return fmt.Errorf("record audit chain base: %w", err)
		}
		more = broken == nil && len(oldest) == batch && last.ID == oldest[len(oldest)-1].ID
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	return deleted, more, broken
}

// chainLinkFailure reports why row does not continue the chain from prev, or
// "" when it does.
func chainLinkFailure(prev string, row db.AuditLog, started bool) string {
	if row.Hash == "" {
		if started {
			return "row is not chained after the chain started"
		}
		return ""
	}
	rowPrev := ""
	if row.PrevHash != nil {
		rowPrev = *row.PrevHash
	}
	if rowPrev != prev {
		return "prev_hash does not link to the previous row"
	}
	if ComputeAuditHash(prev, row) != row.Hash {
		return "hash does not match the row content"
	}
	return ""
}
