package accesslog

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// UsageReader reads the recorder's continuous lifetime observation, including
// facts captured before the best-effort raw-log queue can drop them.
type UsageReader interface {
	DependencyUsage(context.Context) (db.DependencyUsage, error)
}

// usageTracker keeps at most one first-download fact. It has no event queue or
// goroutine: the existing recorder worker retries persistence on every flush.
// Disk is marked dirty before serving; an interrupted run loses certainty of
// absence rather than silently recovering an obsolete never-used state.
type usageTracker struct {
	database *gorm.DB
	mu       sync.Mutex
	writeMu  sync.Mutex
	usage    db.DependencyUsage
	revision uint64
	pending  bool
	lastErr  error
}

func newUsageTracker(database *gorm.DB) *usageTracker {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	usage, err := db.BeginDependencyUsage(ctx, database)
	if err != nil {
		zap.L().Warn("dependency usage history unavailable", zap.Error(err))
	}
	return &usageTracker{database: database, usage: usage, pending: err != nil, lastErr: err}
}

func (t *usageTracker) observe(e Event) {
	if t == nil || !potentialArtifactDelivery(e) {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if SuccessfulArtifactDownload(e) {
		if t.usage.State == db.DependencyUsed {
			// Concurrent completions can arrive out of timestamp order. Keep the
			// earliest observed completion while lifetime coverage remains known.
			if t.usage.FirstSuccessfulRequestAt == nil || !e.At.Before(*t.usage.FirstSuccessfulRequestAt) {
				return
			}
		} else if t.usage.State != db.DependencyNeverUsed {
			// An upgrade or observation gap can prove use, but not its first time.
			t.usage.State = db.DependencyUsed
			t.usage.FirstSuccessfulRequestAt = nil
			t.pending = true
			t.revision++
			return
		}
		t.usage.State = db.DependencyUsed
		if !e.At.IsZero() {
			at := e.At.UTC()
			t.usage.FirstSuccessfulRequestAt = &at
		}
	} else if e.DeliveryResult == "unknown" || e.DeliveryResult == "" || (e.DependencyKind == "unknown" && e.DeliveryResult == "completed") {
		// Only an artifact-shaped response with unknown completion introduces
		// a gap. An out-of-order observation predating a known first success
		// also revokes its timestamp, while retaining positive use evidence.
		if t.usage.State == db.DependencyNeverUsed {
			t.usage.State = db.DependencyUnknown
		} else if t.usage.State == db.DependencyUsed && t.usage.FirstSuccessfulRequestAt != nil &&
			(e.At.IsZero() || !e.At.After(*t.usage.FirstSuccessfulRequestAt)) {
			t.usage.FirstSuccessfulRequestAt = nil
		} else {
			return
		}
	} else {
		return
	}
	t.pending = true
	t.revision++
}

func (t *usageTracker) snapshot(ctx context.Context) (db.DependencyUsage, error) {
	if err := ctx.Err(); err != nil {
		return db.DependencyUsage{State: db.DependencyUnknown}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	usage := t.usage
	if usage.FirstSuccessfulRequestAt != nil {
		at := *usage.FirstSuccessfulRequestAt
		usage.FirstSuccessfulRequestAt = &at
	}
	return usage, t.lastErr
}

func (t *usageTracker) flush(ctx context.Context, clean bool) error {
	if t == nil {
		return nil
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	t.mu.Lock()
	if !t.pending && !clean {
		t.mu.Unlock()
		return nil
	}
	usage, revision := t.usage, t.revision
	t.mu.Unlock()

	err := db.SaveDependencyUsage(ctx, t.database, usage, clean)
	t.mu.Lock()
	t.lastErr = err
	if err == nil && t.revision == revision {
		t.pending = false
	}
	t.mu.Unlock()
	if err != nil {
		zap.L().Warn("failed to persist dependency usage; will retry", zap.Error(err))
	}
	return err
}

func (t *usageTracker) err() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastErr
}

func (n *nullRecorder) DependencyUsage(ctx context.Context) (db.DependencyUsage, error) {
	return n.usage.snapshot(ctx)
}

func (r *batchedRecorder) DependencyUsage(ctx context.Context) (db.DependencyUsage, error) {
	return r.usage.snapshot(ctx)
}

// A failed final clean write is retryable after the worker has drained. Close
// can be called again by the server's resource lifecycle once storage recovers.
func (t *usageTracker) finishClose(ctx context.Context) error {
	if t.err() != nil {
		return t.flush(ctx, true)
	}
	return nil
}
