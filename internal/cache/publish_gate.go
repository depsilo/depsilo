package cache

import (
	"context"
	"io"
)

// publishGate takes the key mutation gate when a write reaches its visibility
// switch and keeps it through the metadata commit that follows. Bytes stream
// into storage without the gate, so a slow Upstream never blocks readers of
// the committed representation, while a snapshot reader still sees either the
// previous object and row or the new pair.
type publishGate struct {
	manager    *Manager
	ctx        context.Context
	key        string
	generation uint64
	// recheck runs under the gate before publication; an error aborts it.
	recheck func() error

	unlock     func()
	lockErr    error
	superseded bool
	recheckErr error
}

func (m *Manager) newPublishGate(ctx context.Context, key string, generation uint64, recheck func() error) *publishGate {
	return &publishGate{manager: m, ctx: ctx, key: key, generation: generation, recheck: recheck}
}

// acquire is the publish hook handed to storage. A write that an invalidation
// superseded while it streamed is refused here, so it never becomes visible.
// The gate stays held, but a newer write may already have published and
// committed under key while this one streamed (S3 does not serialize staged
// writes of one key), so the caller must not treat the current row and object
// as its own.
func (g *publishGate) acquire() error {
	unlock, err := g.manager.lockMutation(g.ctx, g.key)
	if err != nil {
		g.lockErr = err
		return err
	}
	g.unlock = unlock
	if !g.manager.invalidations.writeCurrent(g.key, g.generation) {
		g.superseded = true
		return errCacheWriteSuperseded
	}
	if g.recheck != nil {
		if err := g.recheck(); err != nil {
			g.recheckErr = err
			return err
		}
	}
	return nil
}

func (g *publishGate) release() {
	if g.unlock != nil {
		g.unlock()
		g.unlock = nil
	}
}

// putPublished stores r under key and runs publish before the object becomes
// visible. A StagedPublisher runs it at the visibility switch; any other
// Storage runs it before Put, which keeps the gate across the whole write.
// On success publish has run and succeeded.
func (m *Manager) putPublished(ctx context.Context, key string, r io.Reader, size int64, contentType string, publish func() error) error {
	staged, ok := m.storage.(StagedPublisher)
	if !ok {
		if err := publish(); err != nil {
			return err
		}
		return m.storage.Put(ctx, key, r, size, contentType)
	}
	published := false
	err := staged.PutStaged(ctx, key, r, size, contentType, func() error {
		published = true
		return publish()
	})
	if err == nil && !published {
		// A conforming store never succeeds without publishing. Still take
		// the gate so the metadata commit stays guarded.
		return publish()
	}
	return err
}
