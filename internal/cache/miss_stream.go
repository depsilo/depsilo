package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	missStreamChunkSize        = 32 * 1024
	missStorageQueueDepth      = 8
	missStorageBackpressureTTL = 2 * time.Second
)

var (
	errCacheStorageBackpressure = errors.New("cache storage sink remained backpressured")
	missStreamChunkPool         = sync.Pool{
		New: func() any {
			return make([]byte, missStreamChunkSize)
		},
	}
)

// cachePersistenceError distinguishes a cache backend/metadata failure from an
// upstream transfer failure. Ordinary downloads fail open on this error;
// Prefetch and tracked refresh operations still require a durable commit.
type cachePersistenceError struct {
	cause error
}

func (e *cachePersistenceError) Error() string {
	return fmt.Sprintf("cache persistence failed: %v", e.cause)
}

func (e *cachePersistenceError) Unwrap() error {
	return e.cause
}

func isCachePersistenceError(err error) bool {
	var target *cachePersistenceError
	return errors.As(err, &target)
}

// missStreamState coordinates the two consumers of an upstream miss stream.
// Cache persistence is detachable: a normal downstream remains useful after
// storage fails, and a healthy storage fill remains useful after the downstream
// disconnects. The upstream is cancelled only when no useful consumer remains,
// or when a persistence-required internal operation cannot commit.
type missStreamState struct {
	mu                      sync.Mutex
	downstreamOpen          bool
	storageOpen             bool
	abortOnStorageFailure   bool
	cancel                  context.CancelFunc
	upstreamTransferFailure error
	downstreamDone          chan struct{}
	downstreamOnce          sync.Once
}

func newMissStreamState(cancel context.CancelFunc, abortOnStorageFailure bool) *missStreamState {
	return &missStreamState{
		downstreamOpen:        true,
		storageOpen:           true,
		abortOnStorageFailure: abortOnStorageFailure,
		cancel:                cancel,
		downstreamDone:        make(chan struct{}),
	}
}

func (s *missStreamState) downstreamClosed() {
	s.mu.Lock()
	s.downstreamOpen = false
	shouldCancel := !s.storageOpen
	s.mu.Unlock()
	s.downstreamOnce.Do(func() {
		close(s.downstreamDone)
	})
	if shouldCancel {
		s.cancel()
	}
}

func (s *missStreamState) storageFailed() {
	s.mu.Lock()
	s.storageOpen = false
	shouldCancel := s.abortOnStorageFailure || !s.downstreamOpen
	s.mu.Unlock()
	if shouldCancel {
		s.cancel()
	}
}

func (s *missStreamState) recordUpstreamTransferFailure(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	if s.upstreamTransferFailure == nil {
		s.upstreamTransferFailure = err
	}
	s.mu.Unlock()
}

func (s *missStreamState) upstreamFailure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upstreamTransferFailure
}

func (s *missStreamState) storageBackpressureRequired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.abortOnStorageFailure || !s.downstreamOpen
}

func acquireMissStreamChunk() []byte {
	return missStreamChunkPool.Get().([]byte)[:missStreamChunkSize]
}

func releaseMissStreamChunk(chunk []byte) {
	if cap(chunk) < missStreamChunkSize {
		return
	}
	missStreamChunkPool.Put(chunk[:missStreamChunkSize])
}

// missStorageFanout is the bounded seam between the upstream/client pump and
// cache persistence. The client remains the primary synchronous sink. Storage
// gets a small independent queue so connection setup and brief write stalls do
// not delay the response. If that queue remains full for too long during an
// ordinary download, storage is detached and the client continues. When no
// downstream remains (or Prefetch requires persistence), queue pressure applies
// normal bounded backpressure instead of abandoning the only useful sink.
type missStorageFanout struct {
	chunks      chan []byte
	writer      *io.PipeWriter
	state       *missStreamState
	cancelStore context.CancelCauseFunc
	done        chan struct{}
	doneOnce    sync.Once
	inputOnce   sync.Once
	terminalErr error
}

func newMissStorageFanout(
	writer *io.PipeWriter,
	state *missStreamState,
	cancelStore context.CancelCauseFunc,
) *missStorageFanout {
	return &missStorageFanout{
		chunks:      make(chan []byte, missStorageQueueDepth),
		writer:      writer,
		state:       state,
		cancelStore: cancelStore,
		done:        make(chan struct{}),
	}
}

// enqueue transfers ownership of chunk to the storage pump on success. When
// wait is false, a persistently lagging cache is failed open after a bounded
// grace period. If the downstream closes during that wait, storage becomes the
// required sink and the caller switches to ordinary bounded backpressure.
// When wait is true, the caller blocks with bounded memory until storage
// progresses or the operation is cancelled.
func (s *missStorageFanout) enqueue(
	ctx context.Context,
	chunk []byte,
	wait bool,
) bool {
	waitForStorage := func() bool {
		select {
		case s.chunks <- chunk:
			return true
		case <-s.done:
			return false
		case <-ctx.Done():
			return false
		}
	}
	if wait || s.state.storageBackpressureRequired() {
		return waitForStorage()
	}

	select {
	case s.chunks <- chunk:
		return true
	case <-s.done:
		return false
	default:
	}

	timer := time.NewTimer(missStorageBackpressureTTL)
	defer timer.Stop()
	select {
	case s.chunks <- chunk:
		return true
	case <-s.done:
		return false
	case <-ctx.Done():
		return false
	case <-s.state.downstreamDone:
		return waitForStorage()
	case <-timer.C:
		// downstreamDone is closed just after downstreamOpen changes under the
		// state lock. Recheck here so a timeout racing that notification still
		// preserves the now-only useful storage consumer.
		if s.state.storageBackpressureRequired() {
			return waitForStorage()
		}
		s.detach(fmt.Errorf(
			"%w for %s",
			errCacheStorageBackpressure,
			missStorageBackpressureTTL,
		))
		return false
	}
}

// closeInput is called exactly once by the upstream pump. A terminal upstream
// error is delivered only after already-queued bytes, matching io.Pipe's
// write-then-close behavior.
func (s *missStorageFanout) closeInput(err error) {
	s.inputOnce.Do(func() {
		s.terminalErr = err
		close(s.chunks)
	})
}

func (s *missStorageFanout) signalDone() {
	s.doneOnce.Do(func() {
		close(s.done)
	})
}

func (s *missStorageFanout) detach(err error) {
	if err == nil {
		err = errCacheStorageBackpressure
	}
	s.doneOnce.Do(func() {
		s.state.storageFailed()
		s.cancelStore(err)
		_ = s.writer.CloseWithError(err)
		close(s.done)
	})
}

func (s *missStorageFanout) run(ctx context.Context) {
	defer s.discardQueued()
	for {
		select {
		case <-ctx.Done():
			s.detach(context.Cause(ctx))
			return
		case chunk, ok := <-s.chunks:
			if !ok {
				if s.terminalErr != nil {
					_ = s.writer.CloseWithError(s.terminalErr)
				} else {
					_ = s.writer.Close()
				}
				s.signalDone()
				return
			}
			written, err := s.writer.Write(chunk)
			releaseMissStreamChunk(chunk)
			if err == nil && written != len(chunk) {
				err = io.ErrShortWrite
			}
			if err != nil {
				s.detach(err)
				return
			}
		}
	}
}

func (s *missStorageFanout) discardQueued() {
	// Keep draining until the sole producer closes input. A send that was
	// already selectable when done closed may still win Go's select tie; ranging
	// to close guarantees ownership of that final chunk is reclaimed too.
	for chunk := range s.chunks {
		releaseMissStreamChunk(chunk)
	}
}

// observedPipeReader reports when the downstream has consumed or abandoned its
// stream. Close notification matters when storage has already failed while the
// pump is blocked in an upstream Read: it lets the last consumer cancel that
// read immediately instead of leaving it alive until the fetch timeout.
type observedPipeReader struct {
	*io.PipeReader
	once   sync.Once
	onDone func()
}

func (r *observedPipeReader) notifyDone() {
	r.once.Do(r.onDone)
}

func (r *observedPipeReader) Read(p []byte) (int, error) {
	n, err := r.PipeReader.Read(p)
	if err != nil {
		r.notifyDone()
	}
	return n, err
}

func (r *observedPipeReader) Close() error {
	err := r.PipeReader.Close()
	r.notifyDone()
	return err
}

// cancelOnDoneReadCloser binds a direct pass-through retry to its caller. It is
// used only after another request's cache commit failed, so there is no cache
// fill that should outlive a downstream disconnect.
type cancelOnDoneReadCloser struct {
	io.ReadCloser
	once   sync.Once
	onDone func()
}

func (r *cancelOnDoneReadCloser) finish() {
	r.once.Do(r.onDone)
}

func (r *cancelOnDoneReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err != nil {
		r.finish()
	}
	return n, err
}

func (r *cancelOnDoneReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.finish()
	return err
}
