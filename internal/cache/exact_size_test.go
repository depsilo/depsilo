package cache

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type unexpectedEOFReader struct {
	reader io.Reader
}

type fixedTerminalReader struct {
	err error
}

func (r *fixedTerminalReader) Read([]byte) (int, error) {
	return 0, r.err
}

func (r *unexpectedEOFReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	if err == io.EOF {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

func TestWithExpectedBodySize(t *testing.T) {
	tests := []struct {
		name     string
		body     io.ReadCloser
		expected int64
		want     string
		mismatch bool
	}{
		{
			name:     "exact",
			body:     io.NopCloser(strings.NewReader("abc")),
			expected: 3,
			want:     "abc",
		},
		{
			name:     "empty",
			body:     io.NopCloser(strings.NewReader("")),
			expected: 0,
		},
		{
			name:     "short clean EOF",
			body:     io.NopCloser(strings.NewReader("abc")),
			expected: 4,
			want:     "abc",
			mismatch: true,
		},
		{
			name: "short unexpected EOF",
			body: io.NopCloser(&unexpectedEOFReader{
				reader: strings.NewReader("abc"),
			}),
			expected: 4,
			want:     "abc",
			mismatch: true,
		},
		{
			name:     "overlong",
			body:     io.NopCloser(strings.NewReader("abcd")),
			expected: 3,
			want:     "abc",
			mismatch: true,
		},
		{
			name:     "nonempty declared empty",
			body:     io.NopCloser(strings.NewReader("a")),
			expected: 0,
			mismatch: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := WithExpectedBodySize(test.body, test.expected)
			defer body.Close()
			got, err := io.ReadAll(body)
			if got := string(got); got != test.want {
				t.Fatalf("body = %q, want %q", got, test.want)
			}
			if errors.Is(err, ErrBodySizeMismatch) != test.mismatch {
				t.Fatalf(
					"error = %v (mismatch=%v), want mismatch=%v",
					err,
					errors.Is(err, ErrBodySizeMismatch),
					test.mismatch,
				)
			}
		})
	}
}

func TestWithExpectedBodySizeLeavesUnknownBodyUnchanged(t *testing.T) {
	body := io.NopCloser(strings.NewReader("body"))
	if got := WithExpectedBodySize(body, -1); got != body {
		t.Fatal("unknown expected size unexpectedly wrapped the body")
	}
}

func TestWithExpectedBodySizePreservesShortReadCause(t *testing.T) {
	connectionReset := errors.New("connection reset")
	body := WithExpectedBodySize(
		io.NopCloser(io.MultiReader(
			strings.NewReader("ab"),
			&fixedTerminalReader{err: connectionReset},
		)),
		3,
	)
	defer body.Close()
	got, err := io.ReadAll(body)
	if string(got) != "ab" {
		t.Fatalf("body = %q, want ab", got)
	}
	if !errors.Is(err, ErrBodySizeMismatch) || !errors.Is(err, connectionReset) {
		t.Fatalf("error = %v, want size mismatch preserving connection reset", err)
	}
}

func TestExactSizeReaderValidateConsumedDetectsBackendThatStopsAtLength(t *testing.T) {
	reader := newExactSizeReader(strings.NewReader("abcd"), 3)
	buffer := make([]byte, 3)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		t.Fatal(err)
	}
	if err := reader.validateConsumed(); !errors.Is(err, ErrBodySizeMismatch) {
		t.Fatalf("validateConsumed error = %v, want ErrBodySizeMismatch", err)
	}
}
