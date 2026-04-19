package unit

import (
	"bytes"
	"io"
	"testing"

	"depsilo/internal/cache"
)

func TestCountingReader_CountsBytes(t *testing.T) {
	data := []byte("hello world") // 11 bytes
	cr := cache.NewCountingReader(bytes.NewReader(data))

	buf, err := io.ReadAll(cr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(buf) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(buf))
	}
	if cr.BytesRead() != 11 {
		t.Errorf("expected 11 bytes read, got %d", cr.BytesRead())
	}
}

func TestCountingReader_EmptyReader(t *testing.T) {
	cr := cache.NewCountingReader(bytes.NewReader(nil))

	buf, err := io.ReadAll(cr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buf) != 0 {
		t.Errorf("expected empty, got %d bytes", len(buf))
	}
	if cr.BytesRead() != 0 {
		t.Errorf("expected 0 bytes read, got %d", cr.BytesRead())
	}
}

func TestCountingReader_LargeData(t *testing.T) {
	data := make([]byte, 10*1024*1024) // 10 MB
	for i := range data {
		data[i] = byte(i % 256)
	}
	cr := cache.NewCountingReader(bytes.NewReader(data))

	written, err := io.Copy(io.Discard, cr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if written != int64(len(data)) {
		t.Errorf("io.Copy reported %d, expected %d", written, len(data))
	}
	if cr.BytesRead() != int64(len(data)) {
		t.Errorf("expected %d bytes read, got %d", len(data), cr.BytesRead())
	}
}
