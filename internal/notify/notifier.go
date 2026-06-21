package notify

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// Notifier loads webhook configs from DB and dispatches notifications.
type Notifier struct {
	db      *gorm.DB
	mu      sync.RWMutex
	configs []db.WebhookConfig
}

// New creates a new Notifier.
func New(database *gorm.DB) *Notifier {
	return &Notifier{db: database}
}

// LoadConfigs reloads webhook configs from the database.
func (n *Notifier) LoadConfigs() error {
	var configs []db.WebhookConfig
	if err := n.db.Where("enabled = ?", true).Find(&configs).Error; err != nil {
		return err
	}
	n.mu.Lock()
	n.configs = configs
	n.mu.Unlock()
	zap.L().Debug("webhook configs reloaded", zap.Int("count", len(configs)))
	return nil
}

// Dispatch sends a notification to all matching webhooks.
// It respects per-webhook cooldowns and event filters.
func (n *Notifier) Dispatch(ctx context.Context, event Event) {
	n.mu.RLock()
	configs := make([]db.WebhookConfig, len(n.configs))
	copy(configs, n.configs)
	n.mu.RUnlock()

	for i := range configs {
		cfg := &configs[i]
		if !cfg.Enabled {
			continue
		}
		if !matchEvent(cfg.Events, event.Type) {
			continue
		}
		// Check cooldown
		if cfg.CooldownMinutes > 0 && cfg.LastSentAt != nil {
			if time.Since(*cfg.LastSentAt) < time.Duration(cfg.CooldownMinutes)*time.Minute {
				continue
			}
		}
		go func(c db.WebhookConfig) {
			if err := dispatch(ctx, c, event); err != nil {
				zap.L().Warn("webhook dispatch failed",
					zap.String("name", c.Name),
					zap.String("platform", c.Platform),
					zap.Error(err),
				)
				return
			}
			now := time.Now()
			n.db.Model(&c).Update("last_sent_at", now)
		}(*cfg)
	}
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
