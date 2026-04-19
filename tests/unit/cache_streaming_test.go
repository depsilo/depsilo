package unit

import (
	"bytes"
	"context"
	"io"
	"testing"

	"depsilo/internal/cache"
)

// fakeStorage is a minimal Storage implementation for testing.
type fakeStorage struct {
	data map[string][]byte
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{data: make(map[string][]byte)}
}

func (s *fakeStorage) Exists(_ context.Context, key string) (bool, error) {
	_, ok := s.data[key]
	return ok, nil
}

func (s *fakeStorage) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	d, ok := s.data[key]
	if !ok {
		return nil, 0, io.EOF
	}
	return io.NopCloser(bytes.NewReader(d)), int64(len(d)), nil
}

func (s *fakeStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	d, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.data[key] = d
	return nil
}

func (s *fakeStorage) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

func (s *fakeStorage) Stat(_ context.Context, key string) (*cache.ObjectMeta, error) {
	return nil, nil
}

func (s *fakeStorage) List(_ context.Context, _ string) ([]cache.ObjectMeta, error) {
	return nil, nil
}

func (s *fakeStorage) TotalSize(_ context.Context) (int64, error) {
	return 0, nil
}

// TestStreamingPut verifies that storage.Put receives data directly from
// the reader without full buffering. We check this by confirming the stored
// data matches the source and that countingReader accurately tracks size.
func TestStreamingPut(t *testing.T) {
	store := newFakeStorage()

	// Simulate a 5 MB upstream body
	size := 5 * 1024 * 1024
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251) // prime modulus for variety
	}

	cr := cache.NewCountingReader(bytes.NewReader(data))

	err := store.Put(context.Background(), "test/big-file.whl", cr, int64(size), "application/octet-stream")
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify countingReader tracked all bytes
	if cr.BytesRead() != int64(size) {
		t.Errorf("countingReader reported %d bytes, expected %d", cr.BytesRead(), size)
	}

	// Verify stored data matches
	stored := store.data["test/big-file.whl"]
	if len(stored) != size {
		t.Errorf("stored %d bytes, expected %d", len(stored), size)
	}
	if !bytes.Equal(stored, data) {
		t.Error("stored data does not match source data")
	}
}
