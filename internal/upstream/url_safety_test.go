package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestFetchURLRedactsCredentialsFromTransportErrors(t *testing.T) {
	rawURL := "https://alice:secret@example.test/private/token?access_token=hidden#fragment"
	transportErr := errors.New("dial failed for " + rawURL)
	u := &Upstream{
		Name: "private",
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportErr
		})},
	}

	_, err := u.FetchURL(context.Background(), rawURL)
	if err == nil {
		t.Fatal("credential-bearing transport failure unexpectedly succeeded")
	}
	if !errors.Is(err, transportErr) {
		t.Fatalf("transport cause was not preserved: %v", err)
	}
	if !strings.Contains(err.Error(), "https://example.test") {
		t.Fatalf("safe origin missing from error: %v", err)
	}
	for _, secret := range []string{"alice", "secret", "/private/token", "access_token", "hidden", "fragment"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error disclosed %q: %v", secret, err)
		}
	}
}

func TestFetchURLRedactsCredentialsFromStatusErrors(t *testing.T) {
	rawURL := "https://alice:secret@example.test/private/token?access_token=hidden"
	u := &Upstream{
		Name: "private",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("unavailable")),
				Request:    request,
			}, nil
		})},
	}

	_, err := u.FetchURL(context.Background(), rawURL)
	if err == nil {
		t.Fatal("upstream status failure unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "https://example.test") {
		t.Fatalf("safe origin missing from error: %v", err)
	}
	for _, secret := range []string{"alice", "secret", "/private/token", "access_token", "hidden"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error disclosed %q: %v", secret, err)
		}
	}
}
