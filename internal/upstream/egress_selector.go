package upstream

import (
	"context"
	"fmt"
)

// EgressSelector chooses an upstream only as an outbound transport for an
// external artifact URL that the adapter has already authorized from trusted
// metadata. It deliberately ignores metadata-origin health and passive
// recovery state: FetchURL reuses the selected upstream's client/proxy without
// reporting the external host's result against that metadata origin.
type EgressSelector struct {
	pool *Pool
}

// NewEgressSelector creates a selector for already-authorized external
// artifact downloads.
func NewEgressSelector(pool *Pool) Selector {
	return &EgressSelector{pool: pool}
}

// Select returns the highest-priority upstream that has not been disabled by
// a critical protocol-integrity failure. Equal priorities preserve pool order.
func (s *EgressSelector) Select(_ context.Context) (*Upstream, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("no egress-capable upstream")
	}

	var selected *Upstream
	for _, candidate := range s.pool.Snapshot() {
		if candidate == nil || candidate.hasCriticalFailure() {
			continue
		}
		if selected == nil || candidate.Priority < selected.Priority {
			selected = candidate
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("no egress-capable upstream")
	}
	return selected, nil
}

func (u *Upstream) hasCriticalFailure() bool {
	if u == nil {
		return true
	}
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.health.criticalFailureLatched
}
