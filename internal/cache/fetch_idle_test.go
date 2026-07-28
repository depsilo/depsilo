package cache

import (
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type closeObservedBody struct {
	io.Reader
	closed chan struct{}
	once   sync.Once
}

type countedReadCloser struct {
	reader io.Reader
	reads  atomic.Int64
}

func (b *countedReadCloser) Read(buffer []byte) (int, error) {
	b.reads.Add(1)
	return b.reader.Read(buffer)
}

func (b *countedReadCloser) Close() error { return nil }

func (b *closeObservedBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func TestBodyIdleTimeoutDoesNotChargeTimeBetweenReadsToUpstream(t *testing.T) {
	origin := &closeObservedBody{
		Reader: strings.NewReader("ab"),
		closed: make(chan struct{}),
	}
	body := WithBodyIdleTimeout(origin, 20*time.Millisecond, nil)

	buffer := make([]byte, 1)
	if n, err := body.Read(buffer); n != 1 || err != nil || string(buffer[:n]) != "a" {
		t.Fatalf("first read = (%d, %v, %q)", n, err, buffer[:n])
	}
	time.Sleep(60 * time.Millisecond)
	select {
	case <-origin.closed:
		t.Fatal("slow downstream processing was misclassified as an idle upstream")
	default:
	}
	if n, err := body.Read(buffer); n != 1 || err != nil || string(buffer[:n]) != "b" {
		t.Fatalf("second read = (%d, %v, %q)", n, err, buffer[:n])
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBodyIdleTimeoutNeverReadsUnderlyingBodyAfterTerminalState(t *testing.T) {
	origin := &countedReadCloser{reader: strings.NewReader("")}
	body := WithBodyIdleTimeout(origin, time.Second, nil)

	if n, err := body.Read(make([]byte, 1)); n != 0 || err != io.EOF {
		t.Fatalf("first read = (%d, %v), want EOF", n, err)
	}
	if n, err := body.Read(make([]byte, 1)); n != 0 || err != io.EOF {
		t.Fatalf("read after EOF = (%d, %v), want EOF", n, err)
	}
	if got := origin.reads.Load(); got != 1 {
		t.Fatalf("underlying reads after EOF = %d, want 1", got)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if n, err := body.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("read after Close = (%d, %v), want ErrClosedPipe", n, err)
	}
	if got := origin.reads.Load(); got != 1 {
		t.Fatalf("underlying reads after Close = %d, want 1", got)
	}
}

func TestBodyIdleTimeoutCloseBeforeReadDoesNotDelegate(t *testing.T) {
	origin := &countedReadCloser{reader: strings.NewReader("body")}
	body := WithBodyIdleTimeout(origin, time.Second, nil)
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := body.Read(make([]byte, 1)); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("read after Close error = %v, want ErrClosedPipe", err)
	}
	if got := origin.reads.Load(); got != 0 {
		t.Fatalf("underlying reads after Close = %d, want 0", got)
	}
}
