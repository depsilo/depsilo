package cache

import (
	"context"
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

type ObjectMeta struct {
	Key          string
	Size         int64
	ContentType  string
	LastModified time.Time
}
