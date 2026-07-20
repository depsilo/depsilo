package security

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func newBlockedFetcher(t *testing.T) *Fetcher {
	t.Helper()
	fetcher := &Fetcher{
		client:  &http.Client{},
		baseURL: "http://127.0.0.1",
		limiter: time.NewTicker(time.Hour),
		closed:  make(chan struct{}),
	}
	t.Cleanup(fetcher.Close)
	return fetcher
}

func TestFetcherRateLimitWaitHonorsCancellation(t *testing.T) {
	tests := []struct {
		name string
		run  func(context.Context, *Fetcher) error
	}{
		{name: "single query", run: func(ctx context.Context, fetcher *Fetcher) error {
			_, err := fetcher.Query(ctx, "pypi", "requests")
			return err
		}},
		{name: "batch query", run: func(ctx context.Context, fetcher *Fetcher) error {
			_, err := fetcher.QueryBatch(ctx, []PackageRef{{Ecosystem: "pypi", Name: "requests"}})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fetcher := newBlockedFetcher(t)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			started := time.Now()
			err := test.run(ctx, fetcher)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
			if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
				t.Fatalf("canceled rate-limit wait took %v", elapsed)
			}
		})
	}
}

func TestFetcherCloseUnblocksRateLimitWait(t *testing.T) {
	fetcher := newBlockedFetcher(t)
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		_, err := fetcher.QueryBatch(context.Background(), []PackageRef{{Ecosystem: "pypi", Name: "requests"}})
		result <- err
	}()
	<-started
	fetcher.Close()

	select {
	case err := <-result:
		if !errors.Is(err, ErrFetcherClosed) {
			t.Fatalf("error = %v, want ErrFetcherClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Fetcher.Close did not unblock the rate-limit wait")
	}
}

type idleClosingTransport struct {
	closed atomic.Bool
}

func (*idleClosingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected request")
}

func (transport *idleClosingTransport) CloseIdleConnections() {
	transport.closed.Store(true)
}

func TestFetcherCloseReleasesIdleConnections(t *testing.T) {
	transport := &idleClosingTransport{}
	fetcher := &Fetcher{
		client:  &http.Client{Transport: transport},
		limiter: time.NewTicker(time.Hour),
		closed:  make(chan struct{}),
	}
	fetcher.Close()
	fetcher.Close()
	if !transport.closed.Load() {
		t.Fatal("Fetcher.Close did not close idle HTTP connections")
	}
}
