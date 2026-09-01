package docker

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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestGetTokenCancellationStopsAuthDiscovery(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewAuthManager().GetToken(
		ctx,
		client,
		"https://registry.example",
		"registry",
		"",
		"",
		"repository:acme/widget:pull",
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetToken error = %v, want context cancellation", err)
	}
}

func TestGetTokenCanceledFollowerDoesNotWaitForSharedDiscovery(t *testing.T) {
	discoveryStarted := make(chan struct{})
	releaseDiscovery := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		select {
		case <-discoveryStarted:
		default:
			close(discoveryStarted)
		}
		<-releaseDiscovery
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Request:    request,
		}, nil
	})}
	manager := NewAuthManager()
	leaderDone := make(chan error, 1)
	go func() {
		_, err := manager.GetToken(
			context.Background(), client, "https://registry.example", "registry", "", "",
			"repository:acme/widget:pull",
		)
		leaderDone <- err
	}()
	<-discoveryStarted

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	followerDone := make(chan error, 1)
	go func() {
		_, err := manager.GetToken(
			ctx, client, "https://registry.example", "registry", "", "",
			"repository:acme/widget:pull",
		)
		followerDone <- err
	}()

	select {
	case err := <-followerDone:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("canceled follower error = %v, want context cancellation", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("canceled follower remained blocked on shared discovery")
	}
	close(releaseDiscovery)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader GetToken error = %v", err)
	}
}

func TestGetTokenRejectsUnsafeRealmBeforeTransport(t *testing.T) {
	var unsafeRequests atomic.Int64
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Hostname() {
		case "registry.example":
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header: http.Header{
					"Www-Authenticate": {`Bearer realm="http://169.254.169.254/latest/meta-data",service="registry.example"`},
				},
				Body:    io.NopCloser(strings.NewReader(`{"errors":[{"code":"UNAUTHORIZED"}]}`)),
				Request: request,
			}, nil
		case "169.254.169.254":
			unsafeRequests.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"token":"stolen"}`)),
				Request:    request,
			}, nil
		default:
			return nil, errors.New("unexpected target")
		}
	})}

	_, err := NewAuthManager().GetToken(
		context.Background(),
		client,
		"https://registry.example",
		"registry",
		"operator",
		"secret",
		"repository:acme/widget:pull",
	)
	if err == nil {
		t.Fatal("GetToken accepted a link-local Bearer realm")
	}
	if got := unsafeRequests.Load(); got != 0 {
		t.Fatalf("unsafe realm transport requests = %d, want 0", got)
	}
}

func TestGetTokenRejectsSpecialPurposeMetadataRealm(t *testing.T) {
	var unsafeRequests atomic.Int64
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "registry.example" {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header: http.Header{
					"Www-Authenticate": {`Bearer realm="https://100.100.100.200/latest/meta-data",service="registry.example"`},
				},
				Body:    io.NopCloser(strings.NewReader("unauthorized")),
				Request: request,
			}, nil
		}
		unsafeRequests.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"token":"stolen"}`)),
			Request:    request,
		}, nil
	})}

	_, err := NewAuthManager().GetToken(
		context.Background(), client, "https://registry.example", "registry", "operator", "secret",
		"repository:acme/widget:pull",
	)
	if err == nil {
		t.Fatal("GetToken accepted a special-purpose metadata realm")
	}
	if got := unsafeRequests.Load(); got != 0 {
		t.Fatalf("unsafe realm transport requests = %d, want 0", got)
	}
}

func TestGetTokenAllowsCrossHostRealmAndPreservesTokenQuery(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Hostname() {
		case "registry.example":
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header: http.Header{
					"Www-Authenticate": {`Bearer realm="https://auth.example/token?account=operator",service="registry.example/a&b"`},
				},
				Body:    io.NopCloser(strings.NewReader(`{"errors":[{"code":"UNAUTHORIZED"}]}`)),
				Request: request,
			}, nil
		case "auth.example":
			username, password, ok := request.BasicAuth()
			if !ok || username != "operator" || password != "secret" {
				t.Errorf("token Basic auth = (%q, %q, %v), want configured credentials", username, password, ok)
			}
			query := request.URL.Query()
			if got := query.Get("account"); got != "operator" {
				t.Errorf("account query = %q, want operator", got)
			}
			if got := query.Get("service"); got != "registry.example/a&b" {
				t.Errorf("service query = %q, want registry.example/a&b", got)
			}
			if got := query.Get("scope"); got != "repository:acme/widget:pull,push" {
				t.Errorf("scope query = %q, want repository:acme/widget:pull,push", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"token":"cross-host-token","expires_in":300}`)),
				Request:    request,
			}, nil
		default:
			return nil, errors.New("unexpected target")
		}
	})}

	token, err := NewAuthManager().GetToken(
		context.Background(),
		client,
		"https://registry.example",
		"registry",
		"operator",
		"secret",
		"repository:acme/widget:pull,push",
	)
	if err != nil {
		t.Fatal(err)
	}
	if token != "cross-host-token" {
		t.Fatalf("token = %q, want cross-host-token", token)
	}
}

func TestGetTokenDoesNotForwardBasicAuthAcrossRealmRedirect(t *testing.T) {
	var redirectedAuthorization string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Hostname() {
		case "registry.example":
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header: http.Header{
					"Www-Authenticate": {`Bearer realm="https://auth.example/token",service="registry.example"`},
				},
				Body:    io.NopCloser(strings.NewReader(`{"errors":[{"code":"UNAUTHORIZED"}]}`)),
				Request: request,
			}, nil
		case "auth.example":
			return &http.Response{
				StatusCode: http.StatusTemporaryRedirect,
				Header:     http.Header{"Location": {"https://child.auth.example/token"}},
				Body:       io.NopCloser(strings.NewReader("redirect")),
				Request:    request,
			}, nil
		case "child.auth.example":
			redirectedAuthorization = request.Header.Get("Authorization")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"token":"redirected-token","expires_in":300}`)),
				Request:    request,
			}, nil
		default:
			return nil, errors.New("unexpected target")
		}
	})}

	token, err := NewAuthManager().GetToken(
		context.Background(),
		client,
		"https://registry.example",
		"registry",
		"operator",
		"secret",
		"repository:acme/widget:pull",
	)
	if err != nil {
		t.Fatal(err)
	}
	if token != "redirected-token" {
		t.Fatalf("token = %q, want redirected-token", token)
	}
	if redirectedAuthorization != "" {
		t.Fatalf("redirected Authorization = %q, want empty", redirectedAuthorization)
	}
}

type countingReadCloser struct {
	reader io.Reader
	read   atomic.Int64
}

func (body *countingReadCloser) Read(buffer []byte) (int, error) {
	n, err := body.reader.Read(buffer)
	body.read.Add(int64(n))
	return n, err
}

func (*countingReadCloser) Close() error { return nil }

func TestGetTokenBoundsTokenEndpointErrorBody(t *testing.T) {
	const maxExpectedRead = 64<<10 + 1
	errorBody := &countingReadCloser{reader: strings.NewReader(strings.Repeat("x", 256<<10) + "DO_NOT_INCLUDE")}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "registry.example" {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header: http.Header{
					"Www-Authenticate": {`Bearer realm="https://auth.example/token",service="registry.example"`},
				},
				Body:    io.NopCloser(strings.NewReader("unauthorized")),
				Request: request,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": {"text/plain"}},
			Body:       errorBody,
			Request:    request,
		}, nil
	})}

	_, err := NewAuthManager().GetToken(
		context.Background(),
		client,
		"https://registry.example",
		"registry",
		"",
		"",
		"repository:acme/widget:pull",
	)
	if err == nil {
		t.Fatal("GetToken unexpectedly accepted token endpoint error")
	}
	if strings.Contains(err.Error(), "DO_NOT_INCLUDE") {
		t.Fatal("GetToken error included bytes beyond the bounded diagnostic prefix")
	}
	if got := errorBody.read.Load(); got > maxExpectedRead {
		t.Fatalf("token endpoint error bytes read = %d, want <= %d", got, maxExpectedRead)
	}
}

func TestGetTokenBoundsSuccessfulTokenResponse(t *testing.T) {
	const maxExpectedRead = 64<<10 + 1
	tokenBody := &countingReadCloser{reader: strings.NewReader(`{"token":"` + strings.Repeat("x", 256<<10) + `"}`)}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "registry.example" {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header: http.Header{
					"Www-Authenticate": {`Bearer realm="https://auth.example/token",service="registry.example"`},
				},
				Body:    io.NopCloser(strings.NewReader("unauthorized")),
				Request: request,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       tokenBody,
			Request:    request,
		}, nil
	})}

	_, err := NewAuthManager().GetToken(
		context.Background(), client, "https://registry.example", "registry", "", "",
		"repository:acme/widget:pull",
	)
	if err == nil {
		t.Fatal("GetToken accepted an oversized successful token response")
	}
	if got := tokenBody.read.Load(); got > maxExpectedRead {
		t.Fatalf("token response bytes read = %d, want <= %d", got, maxExpectedRead)
	}
}

func TestGetTokenDoesNotTreatBasicChallengeAsTokenRealm(t *testing.T) {
	var authRequests atomic.Int64
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "registry.example" {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header: http.Header{
					"Www-Authenticate": {`Basic realm="https://auth.example/token"`},
				},
				Body:    io.NopCloser(strings.NewReader("unauthorized")),
				Request: request,
			}, nil
		}
		authRequests.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"token":"must-not-be-used"}`)),
			Request:    request,
		}, nil
	})}

	_, err := NewAuthManager().GetToken(
		context.Background(),
		client,
		"https://registry.example",
		"registry",
		"operator",
		"secret",
		"repository:acme/widget:pull",
	)
	if err == nil {
		t.Fatal("GetToken accepted a non-Bearer challenge")
	}
	if got := authRequests.Load(); got != 0 {
		t.Fatalf("non-Bearer auth endpoint requests = %d, want 0", got)
	}
}
