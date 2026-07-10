// Package tamper implements content-integrity tracking (DIRECTION T1
// tamper detection): the first-seen SHA-256 of each immutable artifact
// is the baseline, and a later re-fetch whose hash differs is a tamper
// alert. The cache Manager calls Record on first fetch and Verify on
// background refresh; this package owns the DB rows, the audit event,
// and the alert hook.
package tamper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/db"
	"depsilo/internal/quarantine"
)

// OnTamperFn is the optional alert hook, fired when Verify finds a
// mismatch. Wired to the webhook notifier in server.go. Must not block
// (fire-and-forget); a panic is recovered so a misbehaving channel
// can't break the refresh path.
type OnTamperFn func(ev db.QuarantineEvent)

type Recorder struct {
	db       *gorm.DB
	now      func() time.Time
	onTamper OnTamperFn
}

func NewRecorder(database *gorm.DB) *Recorder {
	return &Recorder{db: database, now: func() time.Time { return time.Now().UTC() }}
}

func (r *Recorder) SetOnTamper(fn OnTamperFn) { r.onTamper = fn }

// Record establishes the first-seen baseline. Idempotent: if a record
// already exists for the key, this is a no-op (the existing baseline is
// the trusted truth and must never be overwritten by a later fetch).
func (r *Recorder) Record(ctx context.Context, key, ecosystem, pkg, version, sha256 string, size int64) {
	var existing db.TamperRecord
	err := r.db.WithContext(ctx).First(&existing, "key = ?", key).Error
	if err == nil {
		return // baseline already set
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		zap.L().Warn("tamper: baseline lookup", zap.String("key", key), zap.Error(err))
		return
	}
	now := r.now()
	rec := db.TamperRecord{
		Key: key, Ecosystem: ecosystem, Package: pkg, Version: version,
		SHA256: sha256, Size: size, FirstSeenAt: now, LastVerifiedAt: now,
	}
	if createErr := r.db.WithContext(ctx).Create(&rec).Error; createErr != nil {
		zap.L().Warn("tamper: baseline create", zap.String("key", key), zap.Error(createErr))
	}
}

// Verify compares a re-fetched artifact's hash to the baseline.
//   - No baseline (pre-feature cache): adopt this hash as the baseline,
//     never alert. Returns KnownMismatch=false.
//   - Match: bump VerifyCount + LastVerifiedAt. Returns false.
//   - Mismatch: write a tamper_detected event, fire OnTamper, DO NOT
//     touch the baseline hash. Returns KnownMismatch=true so the
//     manager keeps the first-seen bytes.
func (r *Recorder) Verify(ctx context.Context, key, ecosystem, pkg, version, sha256 string, size int64, clientIP string) cache.TamperResult {
	var rec db.TamperRecord
	err := r.db.WithContext(ctx).First(&rec, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		r.Record(ctx, key, ecosystem, pkg, version, sha256, size)
		return cache.TamperResult{}
	}
	if err != nil {
		// A DB hiccup must not turn a clean fetch into a false alarm.
		zap.L().Warn("tamper: verify lookup", zap.String("key", key), zap.Error(err))
		return cache.TamperResult{}
	}

	if rec.SHA256 == sha256 {
		if updErr := r.db.WithContext(ctx).Model(&rec).Updates(map[string]interface{}{
			"last_verified_at": r.now(),
			"verify_count":     rec.VerifyCount + 1,
		}).Error; updErr != nil {
			zap.L().Warn("tamper: verify bump", zap.String("key", key), zap.Error(updErr))
		}
		return cache.TamperResult{}
	}

	reason := fmt.Sprintf(
		"immutable artifact %s@%s (%s) changed upstream: first-seen sha256 %s, now %s — keeping first-seen bytes, refusing to cache the new content",
		pkg, version, ecosystem, short(rec.SHA256), short(sha256),
	)
	ev := db.QuarantineEvent{
		Ecosystem: ecosystem, Package: pkg, Version: version,
		Action: quarantine.ActionTamperDetected, Reason: reason,
		ClientIP: clientIP, CreatedAt: r.now(),
	}
	if createErr := r.db.WithContext(ctx).Create(&ev).Error; createErr != nil {
		zap.L().Warn("tamper: event create", zap.Error(createErr))
	}
	if r.onTamper != nil {
		func() {
			defer func() {
				if p := recover(); p != nil {
					zap.L().Error("tamper: OnTamper hook panicked", zap.Any("recover", p))
				}
			}()
			r.onTamper(ev)
		}()
	}
	return cache.TamperResult{KnownMismatch: true}
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
