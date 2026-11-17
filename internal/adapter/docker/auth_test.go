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

// Docker address literals follow the rules of package Upstreams, without
// declared private ranges: every special-purpose range and every documented
// unique-local range of proxy software is unsafe, and other unique-local
// addresses are private.
func TestLiteralNetworkScopeClassifiesLikePackageUpstreams(t *testing.T) {
	for host, want := range map[string]networkScope{
		"0.1.2.3":               networkUnsafe,
		"100.64.0.1":            networkUnsafe,
		"100.100.100.200":       networkUnsafe,
		"192.0.0.170":           networkUnsafe,
		"192.0.2.1":             networkUnsafe,
		"198.18.0.1":            networkUnsafe,
		"198.51.100.1":          networkUnsafe,
		"203.0.113.1":           networkUnsafe,
		"240.0.0.1":             networkUnsafe,
		"169.254.169.254":       networkUnsafe,
		"64:ff9b::a9fe:a9fe":    networkUnsafe,
		"64:ff9b:1::1":          networkUnsafe,
		"100::1":                networkUnsafe,
		"100:0:0:1::1":          networkUnsafe,
		"2001::1":               networkUnsafe,
		"2001:2::1":             networkUnsafe,
		"2001:db8::1":           networkUnsafe,
		"2002:7f00:1::":         networkUnsafe,
		"3fff::1":               networkUnsafe,
		"5f00::1":               networkUnsafe,
		"fe80::1":               networkUnsafe,
		"fdfe:dcba:9876::1":     networkUnsafe,
		"fd00:6152::2":          networkUnsafe,
		"fc00::1":               networkUnsafe,
		"10.0.0.1":              networkPrivate,
		"::ffff:192.168.1.10":   networkPrivate,
		"fd12:3456:789a::1":     networkPrivate,
		"fc00:f853:ccd:e793::1": networkPrivate, // kind's default network, outside fc00::/18
		"127.0.0.1":             networkLoopback,
		"::1":                   networkLoopback,
		"localhost":             networkLoopback,
		"93.184.216.34":         networkPublic,
		"2001:4860:4860::8888":  networkPublic,
	} {
		if got, known := literalNetworkScope(host); !known || got != want {
			t.Errorf("literalNetworkScope(%s) = %d, %v; want %d", host, got, known, want)
		}
	}
	if _, known := literalNetworkScope("registry.example"); known {
		t.Fatal("a host name was classified without DNS")
	}
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

// validateAuthRealm classifies only localhost, .localhost names and
// addresses. A private or loopback target among them is refused when the
// registry URL may not reach it. A registry configured by host name leaves an
// exact address literal to the origin pin of its transport, which the
// registry clients' tests show for every route, but localhost names and an
// address written with a trailing dot stay refused by their form, since a
// resolver may answer them with any address. The other checks hold for every
// registry.
func TestValidateAuthRealmLeavesPrivateLiteralsOfNamedRegistriesToTheTransport(t *testing.T) {
	for _, tt := range []struct {
		registry, realm string
		want            error
	}{
		{registry: "https://registry.example", realm: "https://10.0.0.30:9000/bucket/blob"},
		{registry: "https://registry.example", realm: "https://[fd12:3456:789a::5]/token"},
		{registry: "https://registry.example", realm: "https://[::ffff:10.0.0.30]/token"},
		{registry: "https://registry.example", realm: "https://127.0.0.1/token"},
		{registry: "https://registry.example", realm: "https://localhost/token", want: errLoopbackRealmTarget},
		{registry: "https://registry.example", realm: "https://admin.localhost:9090/token", want: errLoopbackRealmTarget},
		{registry: "https://registry.example", realm: "https://127.0.0.1./token", want: errLoopbackRealmTarget},
		{registry: "https://registry.example", realm: "https://10.0.0.30.:9000/bucket/blob", want: errPrivateRealmTarget},
		{registry: "https://registry.example", realm: "https://169.254.169.254./latest", want: errUnsafeRealmTarget},
		{registry: "https://registry.example", realm: "https://93.184.216.35/token"},
		{registry: "https://registry.example", realm: "https://auth.example/token"},
		{registry: "https://registry.example", realm: "https://169.254.169.254/latest", want: errUnsafeRealmTarget},
		{registry: "https://registry.example", realm: "https://100.100.100.200/latest", want: errUnsafeRealmTarget},
		{registry: "https://registry.example", realm: "https://198.18.0.12/token", want: errUnsafeRealmTarget},
		{registry: "https://registry.example", realm: "https://[fe80::1]/token", want: errUnsafeRealmTarget},
		{registry: "https://registry.example", realm: "https://[fdfe:dcba:9876::1]/token", want: errUnsafeRealmTarget},
		{registry: "https://registry.example", realm: "http://10.0.0.30/token", want: errRealmDowngrade},
		{registry: "https://registry.example", realm: "https://operator:secret@10.0.0.30/token", want: errInvalidRealmURL},
		{registry: "https://registry.example", realm: "ftp://10.0.0.30/token", want: errInvalidRealmURL},
		{registry: "https://registry.example", realm: "/token", want: errInvalidRealmURL},
		// A registry configured by address or localhost keeps the lexical
		// rules.
		{registry: "https://93.184.216.34", realm: "https://10.0.0.30/token", want: errPrivateRealmTarget},
		{registry: "https://93.184.216.34", realm: "https://127.0.0.1/token", want: errLoopbackRealmTarget},
		{registry: "https://93.184.216.34", realm: "https://localhost/token", want: errLoopbackRealmTarget},
		{registry: "https://93.184.216.34", realm: "https://auth.lan/token"},
		{registry: "https://10.0.0.5", realm: "https://10.0.0.30/token"},
		{registry: "https://10.0.0.5", realm: "https://127.0.0.1/token", want: errLoopbackRealmTarget},
		{registry: "https://10.0.0.5", realm: "https://169.254.169.254/latest", want: errUnsafeRealmTarget},
		{registry: "https://[fd12:3456:789a::5]", realm: "https://10.0.0.30/token"},
		{registry: "https://127.0.0.1:5000", realm: "https://10.0.0.30/token"},
		{registry: "https://localhost:5000", realm: "https://127.0.0.1/token"},
	} {
		if got := validateAuthRealm(tt.registry, tt.realm); got != tt.want {
			t.Errorf("validateAuthRealm(%s, %s) = %v, want %v", tt.registry, tt.realm, got, tt.want)
		}
	}
}
