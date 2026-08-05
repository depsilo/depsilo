package upstream

import (
	"context"
	"fmt"
	"sort"
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

// NewPassiveRecoverySelector creates a selector for pools whose passive
// upstreams have no background probe to restore their health.
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
func (s *PassiveRecoverySelector) Select(_ context.Context) (*Upstream, error) {
	upstreams := s.pool.Snapshot()
	sort.Slice(upstreams, func(i, j int) bool {
		return upstreams[i].Priority < upstreams[j].Priority
	})

	for _, candidate := range upstreams {
		if candidate.IsHealthy() {
			return candidate, nil
		}
	}
	for _, candidate := range upstreams {
		if candidate.reservePassiveRecovery(s.now(), s.cooldown) {
			return candidate, nil
		}
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
