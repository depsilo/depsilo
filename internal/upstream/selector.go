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

func (s *PrioritySelector) Select(ctx context.Context) (*Upstream, error) {
	return s.selectHealthy(ctx, nil)
}

// SelectExcluding chooses the best healthy alternative to a source that just
// failed the current exchange. The exclusion is request-local: it does not
// mutate global health or penalize a source for unrelated traffic.
func (s *PrioritySelector) SelectExcluding(ctx context.Context, excluded *Upstream) (*Upstream, error) {
	return s.selectHealthy(ctx, excluded)
}

func (s *PrioritySelector) selectHealthy(_ context.Context, excluded *Upstream) (*Upstream, error) {
	ups := s.pool.Snapshot()

	sort.Slice(ups, func(i, j int) bool {
		return ups[i].Priority < ups[j].Priority
	})

	for _, u := range ups {
		if u != excluded && u.IsHealthy() {
			return u, nil
		}
	}
	if excluded != nil {
		return nil, fmt.Errorf("no healthy alternate upstream")
	}
	return nil, fmt.Errorf("all upstreams are unhealthy")
}
