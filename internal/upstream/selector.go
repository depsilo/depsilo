package upstream

import (
	"context"
	"fmt"
	"sort"
)

// Selector picks an upstream from the pool.
type Selector interface {
	Select(ctx context.Context) (*Upstream, error)
}

// PrioritySelector selects the highest-priority healthy upstream.
type PrioritySelector struct {
	pool *Pool
}

func NewPrioritySelector(pool *Pool) *PrioritySelector {
	return &PrioritySelector{pool: pool}
}

func (s *PrioritySelector) Select(_ context.Context) (*Upstream, error) {
	ups := s.pool.Snapshot()

	sort.Slice(ups, func(i, j int) bool {
		return ups[i].Priority < ups[j].Priority
	})

	for _, u := range ups {
		if u.IsHealthy() {
			return u, nil
		}
	}
	return nil, fmt.Errorf("all upstreams are unhealthy")
}
