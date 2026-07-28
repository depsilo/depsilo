package cache

import (
	"errors"
	"fmt"
	"io"
)

// ErrBodySizeMismatch identifies a response body whose actual byte count did
// not match its declared representation size. Callers can use errors.Is even
// though the returned error also includes the expected and observed counts.
var ErrBodySizeMismatch = errors.New("response body size mismatch")

type bodySizeMismatchError struct {
	expected int64
	actual   int64
	cause    error
}

func (e *bodySizeMismatchError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf(
			"%v: expected %d bytes, received %d: %v",
			ErrBodySizeMismatch,
			e.expected,
			e.actual,
			e.cause,
		)
	}
	return fmt.Sprintf(
		"%v: expected %d bytes, received %d",
		ErrBodySizeMismatch,
		e.expected,
		e.actual,
	)
}

func (e *bodySizeMismatchError) Unwrap() []error {
	if e.cause == nil {
		return []error{ErrBodySizeMismatch}
	}
	return []error{ErrBodySizeMismatch, e.cause}
}

type exactSizeReader struct {
	reader   io.Reader
	expected int64
	actual   int64
	terminal error
	sawEOF   bool
}

func newExactSizeReader(reader io.Reader, expected int64) *exactSizeReader {
	return &exactSizeReader{reader: reader, expected: expected}
}

func (r *exactSizeReader) mismatch(cause error) error {
	err := &bodySizeMismatchError{
		expected: r.expected,
		actual:   r.actual,
		cause:    cause,
	}
	r.terminal = err
	return err
}

func (r *exactSizeReader) Read(buffer []byte) (int, error) {
	if r.terminal != nil {
		return 0, r.terminal
	}
	if len(buffer) == 0 {
		return 0, nil
	}

	remaining := r.expected - r.actual
	if remaining <= 0 {
		// Do not release bytes beyond the declared boundary to either the
		// downstream or storage. A one-byte probe distinguishes an exact body
		// from a longer body without buffering the representation.
		var probe [1]byte
		n, err := r.reader.Read(probe[:])
		if n > 0 {
			r.actual += int64(n)
			return 0, r.mismatch(nil)
		}
		if err == io.EOF {
			r.sawEOF = true
			r.terminal = io.EOF
			return 0, io.EOF
		}
		return 0, err
	}

	if int64(len(buffer)) > remaining {
		buffer = buffer[:remaining]
	}
	n, err := r.reader.Read(buffer)
	if n > 0 {
		r.actual += int64(n)
	}
	if r.actual > r.expected {
		// A conforming io.Reader cannot return more than len(buffer), but retain
		// a fail-closed check for broken third-party Reader implementations.
		return n, r.mismatch(nil)
	}
	if err != nil && r.actual != r.expected {
		return n, r.mismatch(err)
	}
	if err == io.EOF {
		r.sawEOF = true
		r.terminal = io.EOF
	}
	return n, err
}

// validateConsumed verifies that a storage backend consumed exactly the
// declared representation and reached its end. Some backends stop reading
// after Content-Length bytes; the final one-byte probe also detects an
// overlong source in that case.
func (r *exactSizeReader) validateConsumed() error {
	if errors.Is(r.terminal, ErrBodySizeMismatch) {
		return r.terminal
	}
	if r.actual != r.expected {
		return r.mismatch(nil)
	}
	if r.sawEOF || r.terminal == io.EOF {
		return nil
	}
	var probe [1]byte
	_, err := r.Read(probe[:])
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return r.mismatch(nil)
	}
	return err
}

type exactSizeReadCloser struct {
	*exactSizeReader
	body io.ReadCloser
}

func (r *exactSizeReadCloser) Close() error {
	return r.body.Close()
}

// WithExpectedBodySize streams body through an exact byte-count check. Short
// bodies and bodies longer than expected return an error matching
// ErrBodySizeMismatch. A negative expected size means unknown and leaves body
// unchanged; zero is a real, enforced empty representation.
func WithExpectedBodySize(body io.ReadCloser, expected int64) io.ReadCloser {
	if body == nil || expected < 0 {
		return body
	}
	return &exactSizeReadCloser{
		exactSizeReader: newExactSizeReader(body, expected),
		body:            body,
	}
}
