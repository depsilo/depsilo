package upstream

import (
	"context"
	"fmt"
	"slices"
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
	return s.selectHealthy(ctx)
}

// ResolveProvenanceSource returns the exact source named by authenticated
// metadata instead of applying priority failover again.
func (s *PrioritySelector) ResolveProvenanceSource(sourceID string) (*Upstream, error) {
	if s == nil {
		return nil, fmt.Errorf("artifact provenance source is unavailable")
	}
	return resolveProvenanceSource(s.pool, sourceID)
}

// SelectExcluding chooses the best healthy alternative to a source that just
// failed the current exchange. The exclusion is request-local: it does not
// mutate global health or penalize a source for unrelated traffic.
func (s *PrioritySelector) SelectExcluding(ctx context.Context, excluded *Upstream) (*Upstream, error) {
	return s.selectHealthy(ctx, excluded)
}

// SelectExcludingAll is SelectExcluding for every source that already failed
// the current request, so one request tries each pool member at most once.
func (s *PrioritySelector) SelectExcludingAll(ctx context.Context, excluded ...*Upstream) (*Upstream, error) {
	return s.selectHealthy(ctx, excluded...)
}

func (s *PrioritySelector) selectHealthy(_ context.Context, excluded ...*Upstream) (*Upstream, error) {
	ups := orderedUpstreams(s.pool)

	for _, u := range ups {
		if !slices.Contains(excluded, u) && u.IsHealthy() {
			return u, nil
		}
	}
	if excludesAny(excluded) {
		return nil, fmt.Errorf("no healthy alternate upstream")
	}
	return nil, fmt.Errorf("all upstreams are unhealthy")
}

// excludesAny reports whether a request-local exclusion names a source.
func excludesAny(excluded []*Upstream) bool {
	return slices.ContainsFunc(excluded, func(u *Upstream) bool { return u != nil })
}

func orderedUpstreams(pool *Pool) []*Upstream {
	upstreams := pool.Snapshot()
	sort.SliceStable(upstreams, func(i, j int) bool {
		return upstreams[i].Priority < upstreams[j].Priority
	})
	return upstreams
}
