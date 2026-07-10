package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

func TestCountingReader_SumHex(t *testing.T) {
	const payload = "the quick brown fox\n"
	cr := NewCountingReader(strings.NewReader(payload))
	if _, err := io.Copy(io.Discard, cr); err != nil {
		t.Fatalf("copy: %v", err)
	}
	want := sha256.Sum256([]byte(payload))
	if got := cr.SumHex(); got != hex.EncodeToString(want[:]) {
		t.Errorf("SumHex = %s, want %s", got, hex.EncodeToString(want[:]))
	}
	if cr.BytesRead() != int64(len(payload)) {
		t.Errorf("BytesRead = %d, want %d", cr.BytesRead(), len(payload))
	}
}

func TestCountingReader_SumHex_Empty(t *testing.T) {
	cr := NewCountingReader(strings.NewReader(""))
	_, _ = io.Copy(io.Discard, cr)
	// sha256 of empty input is the well-known e3b0c442... digest.
	if got := cr.SumHex(); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("empty SumHex = %s", got)
	}
}
