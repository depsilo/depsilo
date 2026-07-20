package notify

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/asyncruntime"
	"depsilo/internal/db"
)

var errNilSubmitter = errors.New("notify: nil async submitter")

// Notifier loads webhook configs from DB and dispatches notifications.
type Notifier struct {
	db    *gorm.DB
	tasks asyncruntime.Submitter

	mu           sync.Mutex
	configs      []db.WebhookConfig
	reservations map[uint]cooldownReservation
	nextToken    uint64
}

// cooldownReservation closes the gap between checking a webhook's cooldown and
// the asynchronous request completing. Without an in-memory reservation,
// concurrent Dispatch calls can all observe the same stale LastSentAt value and
// send duplicate notifications.
type cooldownReservation struct {
	token          uint64
	reservedAt     time.Time
	previousSentAt *time.Time
}

type dispatchTarget struct {
	config db.WebhookConfig
	token  uint64
}

// New creates a Notifier whose deliveries are owned by tasks. A nil submitter
// is retained as a configuration error: Dispatch reports it instead of
// silently starting an unowned goroutine.
func New(database *gorm.DB, tasks asyncruntime.Submitter) *Notifier {
	return &Notifier{
		db:           database,
		tasks:        tasks,
		reservations: make(map[uint]cooldownReservation),
	}
}

// LoadConfigs reloads webhook configs from the database.
func (n *Notifier) LoadConfigs(ctx context.Context) error {
	var configs []db.WebhookConfig
	if err := n.db.WithContext(ctx).Where("enabled = ?", true).Find(&configs).Error; err != nil {
		return err
	}
	n.mu.Lock()
	// A reload may race with an in-flight delivery. Preserve the reservation in
	// the freshly loaded snapshot so it cannot reopen the cooldown window.
	for i := range configs {
		reservation, ok := n.reservations[configs[i].ID]
		if !ok {
			// The query can have started just before a successful delivery persisted
			// its timestamp. Keep the newer in-memory value instead of replacing it
			// with that stale query result. This also protects the cooldown for the
			// current process when persisting LastSentAt failed.
			if currentSentAt := lastSentAt(n.configs, configs[i].ID); currentSentAt != nil &&
				(configs[i].LastSentAt == nil || currentSentAt.After(*configs[i].LastSentAt)) {
				configs[i].LastSentAt = cloneTime(currentSentAt)
			}
			continue
		}
		if sentAt := configs[i].LastSentAt; sentAt != nil {
			if reservation.previousSentAt == nil || sentAt.After(*reservation.previousSentAt) {
				reservation.previousSentAt = cloneTime(sentAt)
				n.reservations[configs[i].ID] = reservation
			}
			if sentAt.After(reservation.reservedAt) {
				continue
			}
		}
		configs[i].LastSentAt = cloneTime(&reservation.reservedAt)
	}
	n.configs = configs
	n.mu.Unlock()
	zap.L().Debug("webhook configs reloaded", zap.Int("count", len(configs)))
	return nil
}

// Dispatch reserves every matching webhook and submits one parent task for the
// whole event. Admission is therefore all-or-nothing: a rejected submission
// rolls every reservation back synchronously, while an accepted parent joins
// its fan-out before returning to the owning runtime.
func (n *Notifier) Dispatch(event Event) error {
	reservedAt := time.Now()
	n.mu.Lock()
	targets := make([]dispatchTarget, 0, len(n.configs))
	if n.reservations == nil {
		n.reservations = make(map[uint]cooldownReservation)
	}
	for i := range n.configs {
		cfg := &n.configs[i]
		if !cfg.Enabled {
			continue
		}
		if !matchEvent(cfg.Events, event.Type) {
			continue
		}
		// A request already in flight owns the reservation, including when the
		// configured cooldown is zero. This prevents concurrent duplicate sends
		// while still allowing the next Dispatch after that request completes.
		if _, inFlight := n.reservations[cfg.ID]; inFlight {
			continue
		}
		if cfg.CooldownMinutes > 0 && cfg.LastSentAt != nil {
			if reservedAt.Sub(*cfg.LastSentAt) < time.Duration(cfg.CooldownMinutes)*time.Minute {
				continue
			}
		}

		n.nextToken++
		reservation := cooldownReservation{
			token:          n.nextToken,
			reservedAt:     reservedAt,
			previousSentAt: cloneTime(cfg.LastSentAt),
		}
		n.reservations[cfg.ID] = reservation
		cfg.LastSentAt = cloneTime(&reservedAt)
		targets = append(targets, dispatchTarget{config: *cfg, token: reservation.token})
	}
	n.mu.Unlock()

	if len(targets) == 0 {
		return nil
	}
	if n.tasks == nil {
		n.rollbackTargets(targets)
		return errNilSubmitter
	}

	err := n.tasks.Submit(func(ctx context.Context) {
		var wait sync.WaitGroup
		wait.Add(len(targets))
		for _, item := range targets {
			target := item
			go func() {
				defer wait.Done()
				n.dispatchReserved(ctx, target.config, event, target.token, reservedAt)
			}()
		}
		wait.Wait()
	})
	if err != nil {
		n.rollbackTargets(targets)
		return err
	}
	return nil
}

// DispatchTo submits one explicit delivery without consulting the Notifier's
// enabled configs, event filters, cooldowns, or reservations. It is intended
// for control-plane test deliveries: the supplied config is the only target
// and its LastSentAt is never persisted or changed in memory.
func (n *Notifier) DispatchTo(config db.WebhookConfig, event Event) error {
	if n.tasks == nil {
		return errNilSubmitter
	}
	return n.tasks.Submit(func(ctx context.Context) {
		if err := dispatch(ctx, config, event); err != nil && ctx.Err() == nil && !errors.Is(err, context.Canceled) {
			zap.L().Warn("direct webhook dispatch failed",
				zap.String("name", config.Name),
				zap.String("platform", config.Platform),
				zap.Error(err),
			)
		}
	})
}

func (n *Notifier) dispatchReserved(
	ctx context.Context,
	config db.WebhookConfig,
	event Event,
	token uint64,
	reservedAt time.Time,
) {
	delivered := false
	defer func() { n.finishReservation(config.ID, token, delivered) }()

	if err := dispatch(ctx, config, event); err != nil {
		if ctx.Err() == nil && !errors.Is(err, context.Canceled) {
			zap.L().Warn("webhook dispatch failed",
				zap.String("name", config.Name),
				zap.String("platform", config.Platform),
				zap.Error(err),
			)
		}
		return
	}
	delivered = true

	if err := n.db.WithContext(ctx).Model(&db.WebhookConfig{}).
		Where("id = ?", config.ID).
		Update("last_sent_at", reservedAt).Error; err != nil {
		// The notification was delivered, so retain the in-memory cooldown even
		// if persistence failed. Sending it again would create a duplicate.
		if ctx.Err() == nil && !errors.Is(err, context.Canceled) {
			zap.L().Warn("failed to persist webhook cooldown",
				zap.String("name", config.Name),
				zap.Error(err),
			)
		}
	}
}

func (n *Notifier) rollbackTargets(targets []dispatchTarget) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, target := range targets {
		n.finishReservationLocked(target.config.ID, target.token, false)
	}
}

func (n *Notifier) finishReservation(configID uint, token uint64, delivered bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.finishReservationLocked(configID, token, delivered)
}

func (n *Notifier) finishReservationLocked(configID uint, token uint64, delivered bool) {
	reservation, ok := n.reservations[configID]
	if !ok || reservation.token != token {
		return
	}
	delete(n.reservations, configID)

	for i := range n.configs {
		cfg := &n.configs[i]
		if cfg.ID != configID {
			continue
		}
		if delivered {
			if cfg.LastSentAt == nil || cfg.LastSentAt.Before(reservation.reservedAt) {
				cfg.LastSentAt = cloneTime(&reservation.reservedAt)
			}
			return
		}
		// Only undo this delivery's reservation. A newer value loaded from the
		// database must not be overwritten by a failed, older request.
		if cfg.LastSentAt != nil && cfg.LastSentAt.Equal(reservation.reservedAt) {
			cfg.LastSentAt = cloneTime(reservation.previousSentAt)
		}
		return
	}
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func lastSentAt(configs []db.WebhookConfig, configID uint) *time.Time {
	for i := range configs {
		if configs[i].ID == configID {
			return configs[i].LastSentAt
		}
	}
	return nil
}

// matchEvent returns true if the events filter includes the given event type.
func matchEvent(eventsFilter, eventType string) bool {
	if eventsFilter == "*" || eventsFilter == "" {
		return true
	}
	parts := splitAndTrim(eventsFilter, ",")
	for _, e := range parts {
		if e == eventType {
			return true
		}
	}
	return false
}

func splitAndTrim(s, sep string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			part := trimSpace(s[start:i])
			if part != "" {
				parts = append(parts, part)
			}
			start = i + 1
		}
	}
	part := trimSpace(s[start:])
	if part != "" {
		parts = append(parts, part)
	}
	return parts
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && s[start] == ' ' {
		start++
	}
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}
