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
	rawURL := "https://example.test/private/token?access_token=hidden#fragment"
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
	for _, secret := range []string{"/private/token", "access_token", "hidden", "fragment"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error disclosed %q: %v", secret, err)
		}
	}
}

func TestFetchURLRedactsCredentialsFromStatusErrors(t *testing.T) {
	rawURL := "https://example.test/private/token?access_token=hidden"
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
	for _, secret := range []string{"/private/token", "access_token", "hidden"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error disclosed %q: %v", secret, err)
		}
	}
}

func TestFetchURLRejectsUnsafeTargetBeforeTransport(t *testing.T) {
	var calls int
	u := &Upstream{
		URL: "https://origin.example",
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("transport should not run")
		})},
	}

	for _, rawURL := range []string{
		"https://alice:secret@example.test/private",
		"https://127.0.0.1/private",
		"https://169.254.169.254/latest/meta-data",
		"https://100.100.100.200/latest/meta-data",
		"file:///tmp/private",
	} {
		if _, err := u.FetchURL(context.Background(), rawURL); err == nil {
			t.Errorf("unsafe FetchURL target %q unexpectedly accepted", safeURLOrigin(rawURL))
		}
	}
	if calls != 0 {
		t.Fatalf("transport calls = %d, want 0", calls)
	}
}

func TestBuildClientRejectsOriginQueryAndFragment(t *testing.T) {
	for name, origin := range map[string]string{
		"query":    "https://example.test/base?token=secret",
		"fragment": "https://example.test/base#fragment",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := buildClient("", origin); err == nil {
				t.Errorf("buildClient accepted unusable origin %q", safeURLOrigin(origin))
			}
		})
	}
	if _, err := buildClient("", "https://example.test/base"); err != nil {
		t.Fatalf("buildClient rejected base-path origin: %v", err)
	}
}
