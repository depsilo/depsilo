package snapshot

import (
	"context"

	"depsilo/internal/quarantine"
)

// QuarantineBridge adapts the store to the checker's snapshot gate without
// making the quarantine package depend on snapshot storage.
func (s *Store) QuarantineBridge() quarantine.SnapshotGate {
	return snapshotGate{store: s}
}

type snapshotGate struct {
	store *Store
}

func (g snapshotGate) Check(ctx context.Context, ecosystem, pkg, version string) (quarantine.SnapshotMatch, error) {
	decision, err := g.store.Check(ctx, ecosystem, pkg, version)
	if err != nil {
		return quarantine.SnapshotMatch{}, err
	}
	return quarantine.SnapshotMatch{
		Allowed:      decision.Allowed,
		SnapshotID:   decision.SnapshotID,
		SnapshotName: decision.SnapshotName,
	}, nil
}
