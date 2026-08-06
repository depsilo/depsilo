package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type dataThenErrorBody struct {
	data []byte
	read bool
}

func (b *dataThenErrorBody) Read(p []byte) (int, error) {
	if !b.read {
		b.read = true
		return copy(p, b.data), nil
	}
	return 0, io.ErrUnexpectedEOF
}

func (*dataThenErrorBody) Close() error { return nil }

type countingResponseBody struct {
	reader io.Reader
	read   atomic.Int64
}

func (b *countingResponseBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read.Add(int64(n))
	return n, err
}

func (*countingResponseBody) Close() error { return nil }

type stalledResponseBody struct {
	closed  chan struct{}
	started chan struct{}
}

func (b *stalledResponseBody) Read([]byte) (int, error) {
	select {
	case <-b.started:
	default:
		close(b.started)
	}
	<-b.closed
	return 0, errors.New("response body closed")
}

func (b *stalledResponseBody) Close() error {
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}

type closeUnblocksEOFBody struct {
	closed       chan struct{}
	readFinished chan struct{}
}

func (b *closeUnblocksEOFBody) Read([]byte) (int, error) {
	<-b.closed
	return 0, io.EOF
}

func (b *closeUnblocksEOFBody) Close() error {
	close(b.closed)
	<-b.readFinished
	return nil
}

func TestFetchReportsBodyReadFailureAtBodyTerminalState(t *testing.T) {
	body := &dataThenErrorBody{data: []byte("partial")}
	u := &Upstream{
		Name:      "test",
		URL:       "https://origin.example",
		ProbeMode: "passive",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Header:        make(http.Header),
				Body:          body,
				ContentLength: 16,
				Request:       request,
			}, nil
		})},
		health:   healthState{healthy: false},
		recovery: passiveRecoveryState{reserved: true},
	}

	result, err := u.Fetch(context.Background(), "/artifact")
	if err != nil {
		t.Fatal(err)
	}

	u.mu.RLock()
	totalBeforeRead := u.health.totalReqs
	successesBeforeRead := u.health.successReqs
	recoveryBeforeRead := u.recovery.inFlight
	u.mu.RUnlock()
	if totalBeforeRead != 0 || successesBeforeRead != 0 {
		t.Fatalf("health reported before body terminal state: %d/%d", successesBeforeRead, totalBeforeRead)
	}
	if !recoveryBeforeRead {
		t.Fatal("passive recovery finished before body terminal state")
	}

	_, readErr := io.ReadAll(result.Body)
	if !errors.Is(readErr, io.ErrUnexpectedEOF) {
		t.Fatalf("body read error = %v, want unexpected EOF", readErr)
	}
	_ = result.Body.Close()

	u.mu.RLock()
	totalAfterRead := u.health.totalReqs
	successesAfterRead := u.health.successReqs
	recoveryAfterRead := u.recovery.inFlight
	u.mu.RUnlock()
	if totalAfterRead != 1 || successesAfterRead != 0 {
		t.Fatalf("body failure health totals = %d/%d, want 0/1 successes", successesAfterRead, totalAfterRead)
	}
	if recoveryAfterRead {
		t.Fatal("passive recovery remained in flight after body failure")
	}
}

func TestOriginExchangeReportsSuccessOnlyAfterCompleteBodyEOF(t *testing.T) {
	operations := map[string]func(*Upstream) (io.ReadCloser, error){
		"Request": func(u *Upstream) (io.ReadCloser, error) {
			response, err := u.Request(context.Background(), "/artifact", RequestOptions{})
			if err != nil {
				return nil, err
			}
			return response.Body, nil
		},
		"Fetch": func(u *Upstream) (io.ReadCloser, error) {
			result, err := u.Fetch(context.Background(), "/artifact")
			if err != nil {
				return nil, err
			}
			return result.Body, nil
		},
		"FetchWithHeaders": func(u *Upstream) (io.ReadCloser, error) {
			result, err := u.FetchWithHeaders(context.Background(), "/artifact", nil)
			if err != nil {
				return nil, err
			}
			return result.Body, nil
		},
	}

	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			const payload = "complete"
			u := responseBodyTestUpstream(io.NopCloser(strings.NewReader(payload)), int64(len(payload)))

			body, err := operation(u)
			if err != nil {
				t.Fatal(err)
			}
			u.mu.RLock()
			totalBeforeEOF := u.health.totalReqs
			u.mu.RUnlock()
			if totalBeforeEOF != 0 {
				t.Fatalf("health reports before EOF = %d, want 0", totalBeforeEOF)
			}

			contents, err := io.ReadAll(body)
			if err != nil {
				t.Fatal(err)
			}
			if string(contents) != payload {
				t.Fatalf("body = %q, want %q", contents, payload)
			}
			if err := body.Close(); err != nil {
				t.Fatal(err)
			}

			u.mu.RLock()
			totalReqs := u.health.totalReqs
			successReqs := u.health.successReqs
			u.mu.RUnlock()
			if totalReqs != 1 || successReqs != 1 {
				t.Fatalf("complete body health totals = %d/%d, want 1/1 successes", successReqs, totalReqs)
			}
		})
	}
}

func TestFetchCloseBeforeEOFReportsFailureOnce(t *testing.T) {
	u := responseBodyTestUpstream(io.NopCloser(strings.NewReader("complete")), 8)
	result, err := u.Fetch(context.Background(), "/artifact")
	if err != nil {
		t.Fatal(err)
	}

	buffer := make([]byte, 1)
	if _, err := result.Body.Read(buffer); err != nil {
		t.Fatal(err)
	}
	if err := result.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if err := result.Body.Close(); err != nil {
		t.Fatal(err)
	}

	u.mu.RLock()
	totalReqs := u.health.totalReqs
	successReqs := u.health.successReqs
	u.mu.RUnlock()
	if totalReqs != 1 || successReqs != 0 {
		t.Fatalf("early close health totals = %d/%d, want 0/1 successes", successReqs, totalReqs)
	}
}

func TestFetchCloseWinsAgainstEOFUnblockedByClose(t *testing.T) {
	body := &closeUnblocksEOFBody{
		closed:       make(chan struct{}),
		readFinished: make(chan struct{}),
	}
	u := responseBodyTestUpstream(body, -1)
	result, err := u.Fetch(context.Background(), "/artifact")
	if err != nil {
		t.Fatal(err)
	}

	readDone := make(chan error, 1)
	go func() {
		_, readErr := result.Body.Read(make([]byte, 1))
		readDone <- readErr
		close(body.readFinished)
	}()
	if err := result.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if readErr := <-readDone; readErr != io.EOF {
		t.Fatalf("body read error = %v, want EOF", readErr)
	}

	u.mu.RLock()
	totalReqs := u.health.totalReqs
	successReqs := u.health.successReqs
	u.mu.RUnlock()
	if totalReqs != 1 || successReqs != 0 {
		t.Fatalf("close-unblocked EOF health totals = %d/%d, want 0/1 successes", successReqs, totalReqs)
	}
}

func TestFetchRejectsEOFBeforeDeclaredContentLength(t *testing.T) {
	u := responseBodyTestUpstream(io.NopCloser(strings.NewReader("short")), 10)
	result, err := u.Fetch(context.Background(), "/artifact")
	if err != nil {
		t.Fatal(err)
	}

	_, readErr := io.ReadAll(result.Body)
	if !errors.Is(readErr, io.ErrUnexpectedEOF) {
		t.Fatalf("body read error = %v, want unexpected EOF", readErr)
	}
	_ = result.Body.Close()

	u.mu.RLock()
	totalReqs := u.health.totalReqs
	successReqs := u.health.successReqs
	u.mu.RUnlock()
	if totalReqs != 1 || successReqs != 0 {
		t.Fatalf("short body health totals = %d/%d, want 0/1 successes", successReqs, totalReqs)
	}
}

func TestFetchBoundsOversizedClientErrorDrain(t *testing.T) {
	payload := strings.Repeat("x", maxErrorResponseDrainBytes*2)
	body := &countingResponseBody{reader: strings.NewReader(payload)}
	u := responseBodyStatusTestUpstream(http.StatusNotFound, body, int64(len(payload)))

	if _, err := u.Fetch(context.Background(), "/missing"); err == nil {
		t.Fatal("Fetch unexpectedly accepted a 404")
	}
	if got := body.read.Load(); got > maxErrorResponseDrainBytes+1 {
		t.Fatalf("4xx drain read %d bytes, limit is %d", got, maxErrorResponseDrainBytes+1)
	}

	u.mu.RLock()
	totalReqs := u.health.totalReqs
	successReqs := u.health.successReqs
	u.mu.RUnlock()
	if totalReqs != 1 || successReqs != 0 {
		t.Fatalf("oversized 4xx health totals = %d/%d, want 0/1 successes", successReqs, totalReqs)
	}
}

func TestFetchTimesOutStalledClientErrorDrain(t *testing.T) {
	body := &stalledResponseBody{closed: make(chan struct{}), started: make(chan struct{})}
	u := responseBodyStatusTestUpstream(http.StatusNotFound, body, -1)

	startedAt := time.Now()
	if _, err := u.Fetch(context.Background(), "/missing"); err == nil {
		t.Fatal("Fetch unexpectedly accepted a 404")
	}
	if elapsed := time.Since(startedAt); elapsed > errorResponseDrainTimeout+time.Second {
		t.Fatalf("stalled 4xx drain took %s, timeout is %s", elapsed, errorResponseDrainTimeout)
	}
	select {
	case <-body.started:
	default:
		t.Fatal("stalled response body was never read")
	}

	u.mu.RLock()
	totalReqs := u.health.totalReqs
	successReqs := u.health.successReqs
	u.mu.RUnlock()
	if totalReqs != 1 || successReqs != 0 {
		t.Fatalf("stalled 4xx health totals = %d/%d, want 0/1 successes", successReqs, totalReqs)
	}
}

func responseBodyTestUpstream(body io.ReadCloser, contentLength int64) *Upstream {
	return responseBodyStatusTestUpstream(http.StatusOK, body, contentLength)
}

func responseBodyStatusTestUpstream(status int, body io.ReadCloser, contentLength int64) *Upstream {
	return &Upstream{
		Name: "test",
		URL:  "https://origin.example",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    status,
				Header:        make(http.Header),
				Body:          body,
				ContentLength: contentLength,
				Request:       request,
			}, nil
		})},
		health: healthState{healthy: false},
	}
}
