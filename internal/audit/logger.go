package audit

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/entitlement"
)

// Logger batches and writes audit log entries asynchronously.
// Entries are only recorded when a Pro entitlement is active.
type Logger struct {
	database *gorm.DB
	checker  *entitlement.Checker
	queue    chan db.AuditLog
}

// NewLogger creates a new audit Logger.
func NewLogger(database *gorm.DB, checker *entitlement.Checker) *Logger {
	return &Logger{
		database: database,
		checker:  checker,
		queue:    make(chan db.AuditLog, 1000),
	}
}

// Start runs the background flush loop. It blocks until ctx is cancelled.
func (l *Logger) Start(ctx context.Context) {
	batch := make([]db.AuditLog, 0, 100)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Flush remaining
			if len(batch) > 0 {
				l.flush(batch)
			}
			return
		case entry := <-l.queue:
			batch = append(batch, entry)
			if len(batch) >= 100 {
				l.flush(batch)
				batch = make([]db.AuditLog, 0, 100)
			}
		case <-ticker.C:
			if len(batch) > 0 {
				l.flush(batch)
				batch = make([]db.AuditLog, 0, 100)
			}
		}
	}
}

func (l *Logger) flush(batch []db.AuditLog) {
	if err := l.database.Create(&batch).Error; err != nil {
		zap.L().Error("failed to write audit logs", zap.Int("count", len(batch)), zap.Error(err))
	}
}

// Log enqueues an audit log entry. As of the 2026-06-28 pricing reset
// audit logging is an open-source primitive — a self-hosted control
// point needs an audit trail from day one — so the previous
// `if !IsPro()` gate is gone. The checker field is retained for now
// to keep the constructor signature stable; future Pro-only audit
// extensions (e.g. long-retention storage) can re-introduce a
// granular check at that layer.
func (l *Logger) Log(entry db.AuditLog) {
	select {
	case l.queue <- entry:
	default:
		zap.L().Warn("audit log queue full, dropping entry")
	}
}
