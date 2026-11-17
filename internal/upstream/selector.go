package upstream

import (
	"context"
	"errors"
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
	return nil, allUnhealthyError(ups)
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

// egressDocs is where the deployment guide explains the outbound guard.
const egressDocs = "docs/deployment.md#upstream-network-egress"

// allUnhealthyError is what a client receives when no pool member can be
// selected. When every member's newest check was refused by the outbound guard
// for the same reason, it names that reason: otherwise a host whose DNS returns
// fake-IP addresses only ever says that everything is unhealthy. Only the
// bounded failure code is added, never an address or a host name.
func allUnhealthyError(upstreams []*Upstream) error {
	const message = "all upstreams are unhealthy"
	switch code := sharedEgressFailure(upstreams); code {
	case "":
		return errors.New(message)
	case FailureFakeIP:
		return fmt.Errorf("%s: the outbound guard refused every upstream (%s) because DNS returned fake-IP addresses; set [egress] upstream_proxy in config.toml (in a container, the environment variable DEPSILO_EGRESS_UPSTREAM_PROXY) and restart, see %s", message, code, egressDocs)
	case FailureSharedAddress:
		return fmt.Errorf("%s: the outbound guard refused every upstream (%s) because they resolve into the shared address space; declare your own range under [egress] private_ranges in config.toml and restart, see %s", message, code, egressDocs)
	default:
		return fmt.Errorf("%s: the outbound guard refused every upstream (%s); see %s", message, code, egressDocs)
	}
}

// sharedEgressFailure returns the outbound-guard failure code that every
// upstream's newest failed check carries, or "" when they do not all agree.
// A check that traffic has succeeded past explains nothing and counts as none.
func sharedEgressFailure(upstreams []*Upstream) string {
	shared := ""
	for _, u := range upstreams {
		code := u.standingFailureCode()
		if !isEgressFailure(code) || (shared != "" && code != shared) {
			return ""
		}
		shared = code
	}
	return shared
}

func isEgressFailure(code string) bool {
	switch code {
	case FailureFakeIP, FailureSharedAddress, FailureReserved, FailurePrivate:
		return true
	}
	return false
}
