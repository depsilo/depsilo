package blocklist

import (
	"context"

	"depsilo/internal/quarantine"
)

// QuarantineBridge adapts Store to the quarantine.Blocklist interface.
// The dependency is one-way on purpose: blocklist imports quarantine's
// interface types, quarantine never imports blocklist — mirroring how
// quarantine itself bridges into the adapter package.
type QuarantineBridge struct{ s *Store }

// QuarantineBridge returns a value usable directly as
// checker.SetBlocklist(store.QuarantineBridge()).
func (s *Store) QuarantineBridge() quarantine.Blocklist { return QuarantineBridge{s: s} }

func (b QuarantineBridge) Check(ctx context.Context, ecosystem, pkg, version string) (*quarantine.BlocklistMatch, bool, error) {
	m, overridden, err := b.s.Check(ctx, ecosystem, pkg, version)
	if m == nil {
		return nil, overridden, err
	}
	return &quarantine.BlocklistMatch{SourceID: m.SourceID, Summary: m.Summary}, overridden, err
}
