package notify

import (
	"context"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/entitlement"
	"depsilo/internal/upstream"
)

// SchedulerConfig holds the polling knobs for the webhook scheduler.
type SchedulerConfig struct {
	CheckInterval   time.Duration              // how often to poll (default 60s)
	DiskThreshold   float64                    // 0.0–1.0; trigger when usage exceeds (reserved for future)
	LicenseWarnDays int                        // days before expiry to warn (default 7)
	Pools           map[string]*upstream.Pool
	Checker         *entitlement.Checker
}

// StartScheduler runs periodic checks and dispatches events when thresholds are crossed.
// It blocks until ctx is cancelled.
func StartScheduler(ctx context.Context, n *Notifier, cfg SchedulerConfig) {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 60 * time.Second
	}
	if cfg.DiskThreshold <= 0 {
		cfg.DiskThreshold = 0.85
	}
	if cfg.LicenseWarnDays <= 0 {
		cfg.LicenseWarnDays = 7
	}

	zap.L().Info("webhook scheduler started",
		zap.Duration("interval", cfg.CheckInterval),
	)

	// Wait for system to initialise, then run first check
	select {
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
	}

	// Immediate first run
	runChecks(ctx, n, cfg)

	ticker := time.NewTicker(cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runChecks(ctx, n, cfg)
		}
	}
}

func runChecks(ctx context.Context, n *Notifier, cfg SchedulerConfig) {
	n.LoadConfigs()
	checkUpstreamHealth(ctx, n, cfg.Pools)
	checkLicense(ctx, n, cfg.Checker, cfg.LicenseWarnDays)
}

// checkUpstreamHealth fires an event when ALL upstreams for any ecosystem are unhealthy.
func checkUpstreamHealth(ctx context.Context, n *Notifier, pools map[string]*upstream.Pool) {
	if pools == nil {
		return
	}
	for eco, pool := range pools {
		upstreams := pool.Upstreams()
		if len(upstreams) == 0 {
			continue
		}
		allDown := true
		for _, u := range upstreams {
			if u.IsHealthy() {
				allDown = false
				break
			}
		}
		if allDown {
			n.Dispatch(ctx, Event{
				Type:      EventUpstreamDown,
				Severity:  "critical",
				Title:     "All " + eco + " upstreams are down",
				Message:   "All upstream mirrors for " + eco + " are unreachable. Depsilo will serve stale cache if available.",
				Timestamp: time.Now(),
			})
			zap.L().Warn("webhook: all upstreams down", zap.String("ecosystem", eco))
		}
	}
}

// checkLicense warns when a Pro license or trial is expiring soon.
func checkLicense(ctx context.Context, n *Notifier, checker *entitlement.Checker, warnDays int) {
	if checker == nil {
		return
	}
	status := checker.Status()
	if status.Source != "paid" && status.Source != "trial" {
		return
	}
	if status.ExpiresAt == nil {
		return
	}
	daysLeft := int(time.Until(*status.ExpiresAt).Hours() / 24)
	if daysLeft <= 0 {
		n.Dispatch(ctx, Event{
			Type:      EventLicenseExpiring,
			Severity:  "critical",
			Title:     "Depsilo Pro license has expired",
			Message:   "Your Pro license expired. Pro features (audit logs, SBOM, security scanning, rules) are now locked.",
			Timestamp: time.Now(),
		})
	} else if daysLeft <= warnDays {
		n.Dispatch(ctx, Event{
			Type:      EventLicenseExpiring,
			Severity:  "warning",
			Title:     "Depsilo Pro license expiring soon",
			Message:   "Your Pro license expires in " + itoa(daysLeft) + " day(s). Renew to keep Pro features.",
			Detail:    "Expires: " + status.ExpiresAt.Format("2006-01-02"),
			Timestamp: time.Now(),
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
