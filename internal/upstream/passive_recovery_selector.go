package upstream

import (
	"context"
	"fmt"
	"time"
)

// DefaultPassiveRecoveryCooldown limits half-open attempts for an unhealthy
// passive upstream when no active health probe can restore it.
const DefaultPassiveRecoveryCooldown = time.Minute

// PassiveRecoverySelector preserves normal priority selection while allowing
// an unhealthy passive upstream to receive a recovery exchange when no healthy
// upstream remains.
type PassiveRecoverySelector struct {
	pool     *Pool
	cooldown time.Duration
	now      func() time.Time
}

// NewPassiveRecoverySelector creates a selector that gives passive upstreams
// a bounded request-time path back to healthy state.
func NewPassiveRecoverySelector(pool *Pool) *PassiveRecoverySelector {
	return newPassiveRecoverySelector(pool, DefaultPassiveRecoveryCooldown, time.Now)
}

func newPassiveRecoverySelector(pool *Pool, cooldown time.Duration, now func() time.Time) *PassiveRecoverySelector {
	return &PassiveRecoverySelector{
		pool:     pool,
		cooldown: cooldown,
		now:      now,
	}
}

// Select returns the highest-priority healthy upstream, or an unhealthy
// passive upstream when the pool has no healthy member.
func (s *PassiveRecoverySelector) Select(ctx context.Context) (*Upstream, error) {
	return s.selectCandidate(ctx, nil)
}

// ResolveProvenanceSource returns the exact source named by authenticated
// metadata instead of applying health-based failover again.
func (s *PassiveRecoverySelector) ResolveProvenanceSource(sourceID string) (*Upstream, error) {
	if s == nil {
		return nil, fmt.Errorf("artifact provenance source is unavailable")
	}
	return resolveProvenanceSource(s.pool, sourceID)
}

// SelectExcluding keeps request-local retries away from the source that just
// failed. Healthy alternatives remain preferred; an unhealthy passive
// alternative may enter half-open recovery only when no healthy one exists.
func (s *PassiveRecoverySelector) SelectExcluding(ctx context.Context, excluded *Upstream) (*Upstream, error) {
	return s.selectCandidate(ctx, excluded)
}

func (s *PassiveRecoverySelector) selectCandidate(_ context.Context, excluded *Upstream) (*Upstream, error) {
	upstreams := orderedUpstreams(s.pool)

	for _, candidate := range upstreams {
		if candidate != excluded && candidate.IsHealthy() {
			return candidate, nil
		}
	}
	for _, candidate := range upstreams {
		if candidate != excluded && candidate.reservePassiveRecovery(s.now(), s.cooldown) {
			return candidate, nil
		}
	}
	if excluded != nil {
		return nil, fmt.Errorf("no healthy alternate upstream")
	}
	return nil, fmt.Errorf("all upstreams are unhealthy")
}

func (u *Upstream) reservePassiveRecovery(now time.Time, cooldown time.Duration) bool {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.ProbeMode != "passive" || u.health.healthy || u.health.criticalFailureLatched || u.recovery.inFlight {
		return false
	}
	if now.Before(u.recovery.nextAttempt) {
		return false
	}
	u.recovery.reserved = true
	u.recovery.nextAttempt = now.Add(cooldown)
	return true
}
