package cache

import (
	"context"
	"fmt"
	"io"
	"time"
)

type Storage interface {
	Exists(ctx context.Context, key string) (bool, error)
	Get(ctx context.Context, key string) (io.ReadCloser, int64, error)
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (*ObjectMeta, error)
	List(ctx context.Context, prefix string) ([]ObjectMeta, error)
	TotalSize(ctx context.Context) (int64, error)
}

// StagedPublisher is the optional capability that keeps readers of a key's
// committed object unblocked while its replacement is still arriving.
// PutStaged consumes r like Put, but into a location that Get, Exists and Stat
// cannot observe. Once the whole body is stored there it calls publish exactly
// once, immediately before the atomic switch that makes the new object
// visible under key, so the caller can take its own lock for that switch.
// When publish fails, PutStaged makes nothing visible, discards what it
// staged, and returns an error wrapping the publish error. A Storage without
// this capability is driven conservatively: publish runs before Put.
type StagedPublisher interface {
	PutStaged(ctx context.Context, key string, r io.Reader, size int64, contentType string, publish func() error) error
}

// runPublish invokes a StagedPublisher publish hook; nil means an ordinary
// Put with nothing to coordinate.
func runPublish(publish func() error) error {
	if publish == nil {
		return nil
	}
	if err := publish(); err != nil {
		return fmt.Errorf("publish staged object: %w", err)
	}
	return nil
}

// StagingObjectStore is the optional local-storage capability used by
// retention to reconcile interrupted writes. ObjectMeta.Key is the original
// object key, never the implementation's reserved staging path. RemoveStaging
// returns false when the candidate disappeared or belongs to an active Put.
// Remote stores do not implement this interface because a bucket-wide listing
// on every retention pass would be disproportionately expensive.
type StagingObjectStore interface {
	ListStaging(ctx context.Context) ([]ObjectMeta, error)
	RemoveStaging(ctx context.Context, key string) (bool, error)
}

// ReadinessProber is the narrow capability used by the HTTP readiness check.
// It is intentionally separate from Storage: cache implementations and test
// doubles that are never exposed as server dependencies do not need to grow an
// operational-health API.
type ReadinessProber interface {
	CheckReady(ctx context.Context) error
}

type ObjectMeta struct {
	Key          string
	Size         int64
	ContentType  string
	LastModified time.Time
}
