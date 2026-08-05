package upstream

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/db"
)

type resolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (fn resolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return fn(ctx, host)
}

type closeTrackingConn struct {
	net.Conn
	closeCalls atomic.Int64
	closed     chan struct{}
}

func newCloseTrackingPipe() (*closeTrackingConn, net.Conn) {
	connection, peer := net.Pipe()
	return &closeTrackingConn{
		Conn:   connection,
		closed: make(chan struct{}),
	}, peer
}

func (c *closeTrackingConn) Close() error {
	if c.closeCalls.Add(1) == 1 {
		close(c.closed)
	}
	return c.Conn.Close()
}

func TestRequestReturnsOriginResponseWithoutFollowingRedirect(t *testing.T) {
	var redirected atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/resolve":
			if got := r.Method; got != http.MethodHead {
				t.Errorf("method = %q, want HEAD", got)
			}
			if got := r.Header.Values("X-Test"); len(got) != 2 || got[0] != "one" || got[1] != "two" {
				t.Errorf("X-Test = %q, want [one two]", got)
			}
			http.Redirect(w, r, "/artifact", http.StatusFound)
		case "/artifact":
			redirected.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	u := newTestUpstream(t, server.URL)
	resp, err := u.Request(context.Background(), "/resolve?token=secret", RequestOptions{
		Method:          http.MethodHead,
		Header:          http.Header{"X-Test": {"one", "two"}},
		FollowRedirects: true, // Origin exchanges are deliberately always single-hop.
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.StatusCode; got != http.StatusFound {
		t.Fatalf("status = %d, want 302", got)
	}
	if got := resp.Header.Get("Location"); got != "/artifact" {
		t.Fatalf("Location = %q, want /artifact", got)
	}
	if got := redirected.Load(); got != 0 {
		t.Fatalf("redirect target requests = %d, want 0", got)
	}
	u.mu.RLock()
	totalReqs := u.health.totalReqs
	u.mu.RUnlock()
	if totalReqs != 1 {
		t.Fatalf("origin health request count = %d, want 1", totalReqs)
	}
}

func TestCriticalLatchRejectsEveryOutboundMethod(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, "unexpected")
	}))
	defer server.Close()

	type operation func(*Upstream) error
	closeResponse := func(response *http.Response, err error) error {
		if response != nil {
			_ = response.Body.Close()
		}
		return err
	}
	closeFetch := func(result *FetchResult, err error) error {
		if result != nil && result.Body != nil {
			_ = result.Body.Close()
		}
		return err
	}
	operations := map[string]operation{
		"Request": func(u *Upstream) error {
			return closeResponse(u.Request(context.Background(), "/", RequestOptions{}))
		},
		"RequestOriginURL": func(u *Upstream) error {
			return closeResponse(u.RequestOriginURL(
				context.Background(),
				server.URL+"/",
				RequestOptions{},
			))
		},
		"RequestURL": func(u *Upstream) error {
			return closeResponse(u.RequestURL(
				context.Background(),
				server.URL+"/",
				RequestOptions{},
			))
		},
		"Fetch": func(u *Upstream) error {
			return closeFetch(u.Fetch(context.Background(), "/"))
		},
		"FetchURL": func(u *Upstream) error {
			return closeFetch(u.FetchURL(context.Background(), server.URL+"/"))
		},
		"FetchWithHeaders": func(u *Upstream) error {
			return closeFetch(u.FetchWithHeaders(context.Background(), "/", nil))
		},
	}
	for name, run := range operations {
		t.Run(name, func(t *testing.T) {
			upstream := newTestUpstream(t, server.URL)
			upstream.ReportCriticalFailure(time.Millisecond)
			before := requests.Load()
			err := run(upstream)
			if !errors.Is(err, errCriticalFailureLatched) {
				t.Fatalf("operation error = %v, want critical latch", err)
			}
			if got := requests.Load(); got != before {
				t.Fatalf("network requests changed from %d to %d", before, got)
			}
		})
	}
}

func TestRequestReturnsErrorStatusAndReportsOriginHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Upstream", "preserved")
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	u := newTestUpstream(t, server.URL)
	resp, err := u.Request(context.Background(), "/model?access_token=secret", RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.StatusCode; got != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", got)
	}
	if got := resp.Header.Get("X-Upstream"); got != "preserved" {
		t.Fatalf("X-Upstream = %q, want preserved", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "unavailable") {
		t.Fatalf("body = %q, want upstream body", body)
	}

	u.mu.RLock()
	totalReqs := u.health.totalReqs
	successReqs := u.health.successReqs
	u.mu.RUnlock()
	if totalReqs != 1 || successReqs != 0 {
		t.Fatalf("origin health totals = %d/%d, want 0/1 successes", successReqs, totalReqs)
	}
}

func TestRequestURLCanFollowRedirectsWithoutChangingOriginHealth(t *testing.T) {
	var finalRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/signed":
			http.Redirect(w, r, "/blob", http.StatusTemporaryRedirect)
		case "/blob":
			finalRequests.Add(1)
			if got := r.Header.Get("Range"); got != "bytes=4-" {
				t.Errorf("Range = %q, want bytes=4-", got)
			}
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "blob")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	u := newTestUpstream(t, server.URL)
	resp, err := u.RequestURL(context.Background(), server.URL+"/signed?signature=secret", RequestOptions{
		Header:          http.Header{"Range": {"bytes=4-"}},
		FollowRedirects: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.StatusCode; got != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", got)
	}
	if got := finalRequests.Load(); got != 1 {
		t.Fatalf("final requests = %d, want 1", got)
	}
	u.mu.RLock()
	totalReqs := u.health.totalReqs
	u.mu.RUnlock()
	if totalReqs != 0 {
		t.Fatalf("external request changed origin health count to %d", totalReqs)
	}
}

func TestRequestURLDoesNotFollowRedirectByDefault(t *testing.T) {
	var finalRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/signed" {
			http.Redirect(w, r, "/blob", http.StatusFound)
			return
		}
		finalRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	u := newTestUpstream(t, server.URL)
	resp, err := u.RequestURL(context.Background(), server.URL+"/signed", RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.StatusCode; got != http.StatusFound {
		t.Fatalf("status = %d, want 302", got)
	}
	if got := finalRequests.Load(); got != 0 {
		t.Fatalf("final requests = %d, want 0", got)
	}
}

func TestRequestURLUsesConfiguredUpstreamProxy(t *testing.T) {
	var proxyRequests atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests.Add(1)
		if got := r.URL.Host; got != "cdn.example.test" {
			t.Errorf("proxy target host = %q, want cdn.example.test", got)
		}
		if got := r.URL.Query().Get("signature"); got != "secret" {
			t.Errorf("proxy target signature = %q, want secret", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "proxied")
	}))
	defer proxy.Close()

	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID:          1,
		AdapterType: "huggingface",
		Name:        "proxied",
		URL:         "http://origin.example.test",
		Proxy:       proxy.URL,
		Priority:    1,
		Healthy:     true,
		SuccessRate: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	u := pool.Snapshot()[0]

	resp, err := u.RequestURL(
		context.Background(),
		"http://cdn.example.test/blob?signature=secret",
		RequestOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); got != "proxied" {
		t.Fatalf("body = %q, want proxied", got)
	}
	if got := proxyRequests.Load(); got != 1 {
		t.Fatalf("proxy requests = %d, want 1", got)
	}
	u.mu.RLock()
	totalReqs := u.health.totalReqs
	u.mu.RUnlock()
	if totalReqs != 0 {
		t.Fatalf("external proxy request changed origin health count to %d", totalReqs)
	}
}

func TestRequestRejectsNonHTTPURLsBeforeTransport(t *testing.T) {
	var calls atomic.Int64
	u := &Upstream{
		URL: "ftp://origin.example",
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, errors.New("transport should not run")
		})},
	}

	if _, err := u.Request(context.Background(), "/file?token=secret", RequestOptions{}); err == nil {
		t.Fatal("origin FTP request unexpectedly succeeded")
	}
	if _, err := u.RequestURL(context.Background(), "file:///tmp/private?token=secret", RequestOptions{}); err == nil {
		t.Fatal("external file request unexpectedly succeeded")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("transport calls = %d, want 0", got)
	}
}

func TestRequestRejectsAbsoluteRequestTarget(t *testing.T) {
	var calls atomic.Int64
	u := &Upstream{
		URL: "https://origin.example",
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, errors.New("transport should not run")
		})},
	}

	for _, target := range []string{
		"https://evil.example/private?token=secret",
		"//evil.example/private?token=secret",
		"@evil.example/private?token=secret",
		"relative/path",
		"?target=evil",
	} {
		if _, err := u.Request(context.Background(), target, RequestOptions{}); err == nil {
			t.Fatalf("absolute request target %q unexpectedly succeeded", target)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("transport calls = %d, want 0", got)
	}
}

func TestRequestURLRedactsTransportURLAndPreservesCause(t *testing.T) {
	rawURL := "https://example.test/private/model?access_token=hidden#fragment"
	transportErr := errors.New("dial failed for " + rawURL)
	u := &Upstream{
		URL: "https://example.test",
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportErr
		})},
	}

	_, err := u.RequestURL(context.Background(), rawURL, RequestOptions{})
	if err == nil {
		t.Fatal("credential-bearing transport failure unexpectedly succeeded")
	}
	if !errors.Is(err, transportErr) {
		t.Fatalf("transport cause was not preserved: %v", err)
	}
	if !strings.Contains(err.Error(), "https://example.test") {
		t.Fatalf("safe origin missing from error: %v", err)
	}
	for _, secret := range []string{"/private/model", "access_token", "hidden", "fragment"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error disclosed %q: %v", secret, err)
		}
	}
}

func TestRequestURLRedirectDoesNotLeakSignedReferer(t *testing.T) {
	var referer string
	var authorization string
	var cookie string
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		referer = r.Header.Get("Referer")
		authorization = r.Header.Get("Authorization")
		cookie = r.Header.Get("Cookie")
		_, _ = io.WriteString(w, "blob")
	}))
	defer final.Close()

	var signed *httptest.Server
	signed = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/blob", http.StatusTemporaryRedirect)
	}))
	defer signed.Close()

	u := newTestUpstream(t, signed.URL)
	response, err := u.RequestURL(
		context.Background(),
		signed.URL+"/signed?signature=must-not-leak",
		RequestOptions{
			FollowRedirects: true,
			Header: http.Header{
				"Authorization": {"Bearer origin-secret"},
				"Cookie":        {"session=origin-secret"},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if referer != "" {
		t.Fatalf("signed URL leaked through Referer: %q", referer)
	}
	if authorization != "" || cookie != "" {
		t.Fatalf("origin credentials leaked cross-origin: auth=%q cookie=%q", authorization, cookie)
	}
}

func TestRequestURLPreservesAuthorizationAcrossSameOriginRedirect(t *testing.T) {
	var finalAuthorization string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/canonical" {
			finalAuthorization = r.Header.Get("Authorization")
			_, _ = io.WriteString(w, "ok")
			return
		}
		http.Redirect(w, r, "/canonical", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	u := newTestUpstream(t, server.URL)
	response, err := u.RequestURL(
		context.Background(),
		server.URL+"/old",
		RequestOptions{
			FollowRedirects: true,
			Header:          http.Header{"Authorization": {"Bearer origin-secret"}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if finalAuthorization != "Bearer origin-secret" {
		t.Fatalf("same-origin Authorization = %q, want preserved", finalAuthorization)
	}
}

func TestRequestURLRejectsPublicToPrivateTargetsAndHTTPSDowngrade(t *testing.T) {
	var calls atomic.Int64
	u := &Upstream{
		URL: "https://origin.example.test",
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, errors.New("transport should not run")
		})},
	}

	for _, target := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1/private",
		"http://localhost/private",
		"http://cdn.example.test/blob",
	} {
		if _, err := u.RequestURL(context.Background(), target, RequestOptions{}); err == nil {
			t.Fatalf("unsafe target %q unexpectedly accepted", target)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("transport calls = %d, want 0", got)
	}
}

func TestIPNetworkScopeRejectsSpecialPurposeAddresses(t *testing.T) {
	for _, raw := range []string{
		"0.1.2.3",
		"100.64.0.0",
		"100.100.100.200",
		"100.127.255.255",
		"192.0.0.170",
		"192.0.2.1",
		"198.18.0.1",
		"198.51.100.1",
		"203.0.113.1",
		"240.0.0.1",
		"64:ff9b::7f00:1",
		"64:ff9b:1::1",
		"100::1",
		"100:0:0:1::1",
		"2001::1",
		"2001:db8::1",
		"2002:7f00:1::",
		"3fff::1",
		"5f00::1",
	} {
		if got := ipNetworkScope(net.ParseIP(raw)); got != networkUnsafe {
			t.Errorf("scope(%s) = %d, want unsafe", raw, got)
		}
	}
	for _, raw := range []string{
		"8.8.8.8",
		"100.63.255.255",
		"100.128.0.0",
		"2001:4860:4860::8888",
	} {
		if got := ipNetworkScope(net.ParseIP(raw)); got != networkPublic {
			t.Errorf("scope(%s) = %d, want public", raw, got)
		}
	}
}

func TestGuardedDialerRejectsDNSRebindingToLoopback(t *testing.T) {
	origin, err := url.Parse("https://origin.example")
	if err != nil {
		t.Fatal(err)
	}
	var dialCalls atomic.Int64
	dialer := &guardedDialer{
		origin: origin,
		resolver: resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
			switch host {
			case "origin.example":
				return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
			case "cdn.example":
				return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
			default:
				return nil, errors.New("unexpected hostname")
			}
		}),
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			dialCalls.Add(1)
			return nil, errors.New("transport should not dial")
		},
	}

	if _, err := dialer.DialContext(context.Background(), "tcp", "cdn.example:443"); err == nil {
		t.Fatal("DNS-rebound loopback target unexpectedly accepted")
	}
	if got := dialCalls.Load(); got != 0 {
		t.Fatalf("underlying dial calls = %d, want 0", got)
	}
}

func TestGuardedDialerAllowsPrivateCDNOnlyForPrivateOrigin(t *testing.T) {
	origin, err := url.Parse("https://origin.internal")
	if err != nil {
		t.Fatal(err)
	}
	var dialAddresses []string
	dialer := &guardedDialer{
		origin: origin,
		resolver: resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
			switch host {
			case "origin.internal":
				return []net.IPAddr{{IP: net.ParseIP("10.0.0.2")}}, nil
			case "cdn.internal":
				return []net.IPAddr{{IP: net.ParseIP("10.0.0.3")}}, nil
			default:
				return nil, errors.New("unexpected hostname")
			}
		}),
		dialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			dialAddresses = append(dialAddresses, address)
			connection, peer := net.Pipe()
			_ = peer.Close()
			return connection, nil
		},
	}

	originConnection, err := dialer.DialContext(
		context.Background(),
		"tcp",
		"origin.internal:443",
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = originConnection.Close()
	cdnConnection, err := dialer.DialContext(context.Background(), "tcp", "cdn.internal:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = cdnConnection.Close()
	want := []string{"10.0.0.2:443", "10.0.0.3:443"}
	if strings.Join(dialAddresses, ",") != strings.Join(want, ",") {
		t.Fatalf("dial addresses = %q, want %q", dialAddresses, want)
	}
}

func TestGuardedDialerAllowsPrivateCDNAsFirstDialForPrivateOrigin(t *testing.T) {
	origin, err := url.Parse("https://origin.internal")
	if err != nil {
		t.Fatal(err)
	}
	var (
		resolvedHosts []string
		dialAddresses []string
	)
	dialer := &guardedDialer{
		origin: origin,
		resolver: resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
			resolvedHosts = append(resolvedHosts, host)
			switch host {
			case "origin.internal":
				return []net.IPAddr{{IP: net.ParseIP("10.0.0.2")}}, nil
			case "cdn.internal":
				return []net.IPAddr{{IP: net.ParseIP("10.0.0.3")}}, nil
			default:
				return nil, errors.New("unexpected hostname")
			}
		}),
		dialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			dialAddresses = append(dialAddresses, address)
			connection, peer := net.Pipe()
			_ = peer.Close()
			return connection, nil
		},
	}

	connection, err := dialer.DialContext(context.Background(), "tcp", "cdn.internal:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if strings.Join(resolvedHosts, ",") != "cdn.internal,origin.internal" {
		t.Fatalf("resolved hosts = %q, want target then configured origin policy", resolvedHosts)
	}
	if strings.Join(dialAddresses, ",") != "10.0.0.3:443" {
		t.Fatalf("dial addresses = %q, want only private CDN", dialAddresses)
	}
}

func TestGuardedDialerRejectsMixedScopeDNSAnswers(t *testing.T) {
	origin, err := url.Parse("https://origin.example")
	if err != nil {
		t.Fatal(err)
	}
	var dialCalls atomic.Int64
	dialer := &guardedDialer{
		origin: origin,
		resolver: resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
			switch host {
			case "origin.example":
				return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
			case "cdn.example":
				return []net.IPAddr{
					{IP: net.ParseIP("8.8.4.4")},
					{IP: net.ParseIP("10.0.0.4")},
				}, nil
			default:
				return nil, errors.New("unexpected hostname")
			}
		}),
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			dialCalls.Add(1)
			return nil, errors.New("transport should not dial")
		},
	}

	if _, err := dialer.DialContext(context.Background(), "tcp", "cdn.example:443"); err == nil {
		t.Fatal("mixed-scope DNS target unexpectedly accepted")
	}
	if got := dialCalls.Load(); got != 0 {
		t.Fatalf("underlying dial calls = %d, want 0", got)
	}
}

func TestGuardedDialerRejectsConfiguredOriginDNSRebinding(t *testing.T) {
	origin, err := url.Parse("https://origin.example")
	if err != nil {
		t.Fatal(err)
	}
	var resolutions atomic.Int64
	var dialCalls atomic.Int64
	dialer := &guardedDialer{
		origin: origin,
		resolver: resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
			if host != "origin.example" {
				return nil, errors.New("unexpected hostname")
			}
			if resolutions.Add(1) == 1 {
				return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
			}
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}),
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			dialCalls.Add(1)
			connection, peer := net.Pipe()
			_ = peer.Close()
			return connection, nil
		},
	}

	first, err := dialer.DialContext(context.Background(), "tcp", "origin.example:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	if _, err := dialer.DialContext(
		context.Background(),
		"tcp",
		"origin.example:443",
	); err == nil {
		t.Fatal("configured origin changed from public to loopback without rejection")
	}
	if got := dialCalls.Load(); got != 1 {
		t.Fatalf("underlying dial calls = %d, want only the pinned public origin", got)
	}
}

func TestGuardedDialerPublicTargetDoesNotDependOnOriginDNS(t *testing.T) {
	origin, err := url.Parse("https://origin.example")
	if err != nil {
		t.Fatal(err)
	}
	var dialAddress string
	dialer := &guardedDialer{
		origin: origin,
		resolver: resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
			switch host {
			case "cdn.example":
				return []net.IPAddr{{IP: net.ParseIP("8.8.4.4")}}, nil
			case "origin.example":
				return nil, errors.New("origin DNS unavailable")
			default:
				return nil, errors.New("unexpected hostname")
			}
		}),
		dialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			dialAddress = address
			connection, peer := net.Pipe()
			_ = peer.Close()
			return connection, nil
		},
	}

	connection, err := dialer.DialContext(context.Background(), "tcp", "cdn.example:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if dialAddress != "8.8.4.4:443" {
		t.Fatalf("dial address = %q, want public CDN without origin DNS lookup", dialAddress)
	}
}

func TestGuardedDialerRacesIPv4AfterIPv6FallbackDelay(t *testing.T) {
	origin, err := url.Parse("https://origin.example")
	if err != nil {
		t.Fatal(err)
	}
	ipv4Started := make(chan struct{})
	dialer := &guardedDialer{
		origin: origin,
		resolver: resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
			if host != "cdn.example" {
				return nil, errors.New("unexpected hostname")
			}
			return []net.IPAddr{
				{IP: net.ParseIP("2001:4860:4860::8888")},
				{IP: net.ParseIP("8.8.8.8")},
			}, nil
		}),
		dialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
			if strings.HasPrefix(address, "[") {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			close(ipv4Started)
			connection, peer := net.Pipe()
			_ = peer.Close()
			return connection, nil
		},
	}

	startedAt := time.Now()
	connection, err := dialer.DialContext(context.Background(), "tcp", "cdn.example:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	select {
	case <-ipv4Started:
	default:
		t.Fatal("IPv4 fallback dial never started")
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("IPv4 fallback took %v, want under one second", elapsed)
	}
}

func TestAwaitDialAttemptsClosesQueuedWinnerWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	state := &dialRaceState{}
	winner, peer := newCloseTrackingPipe()
	defer peer.Close()
	if !state.offer(winner) {
		t.Fatal("first successful connection was not accepted")
	}
	results := make(chan dialAttemptResult, 1)
	results <- dialAttemptResult{}

	// Both select cases are now ready. Cancellation must retain ownership of
	// the queued winner regardless of which case the runtime selects.
	cancel()
	connection, err := awaitDialAttempts(ctx, results, 1, state)
	if connection != nil {
		_ = connection.Close()
		t.Fatal("canceled dial returned a connection")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dial error = %v, want context canceled", err)
	}
	select {
	case <-winner.closed:
	default:
		t.Fatal("queued winner was not closed")
	}
	if got := winner.closeCalls.Load(); got != 1 {
		t.Fatalf("winner close calls = %d, want 1", got)
	}
}

func TestDialRaceStateClosesSuccessfulLoser(t *testing.T) {
	state := &dialRaceState{}
	winner, winnerPeer := newCloseTrackingPipe()
	defer winnerPeer.Close()
	loser, loserPeer := newCloseTrackingPipe()
	defer loserPeer.Close()

	if !state.offer(winner) {
		t.Fatal("first successful connection was not accepted")
	}
	if state.offer(loser) {
		t.Fatal("second successful connection was accepted")
	}
	select {
	case <-loser.closed:
	default:
		t.Fatal("successful loser was not closed")
	}
	if got := winner.closeCalls.Load(); got != 0 {
		t.Fatalf("winner close calls before transfer = %d, want 0", got)
	}

	results := make(chan dialAttemptResult, 1)
	results <- dialAttemptResult{}
	connection, err := awaitDialAttempts(context.Background(), results, 1, state)
	if err != nil {
		t.Fatal(err)
	}
	if connection != winner {
		_ = connection.Close()
		t.Fatal("dial returned a connection other than the first winner")
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if got := winner.closeCalls.Load(); got != 1 {
		t.Fatalf("winner close calls after caller close = %d, want 1", got)
	}
}

func TestDialResolvedDeadlineClosesLateConnection(t *testing.T) {
	late, peer := newCloseTrackingPipe()
	defer peer.Close()
	dialStarted := make(chan struct{})
	releaseDial := make(chan struct{})
	dialer := &guardedDialer{
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			close(dialStarted)
			<-releaseDial // Deliberately simulate a dialer that returns after cancellation.
			return late, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	type dialResult struct {
		connection net.Conn
		err        error
	}
	done := make(chan dialResult, 1)
	go func() {
		connection, err := dialer.dialResolved(
			ctx,
			"tcp",
			"443",
			[]net.IPAddr{{IP: net.ParseIP("8.8.8.8")}},
		)
		done <- dialResult{connection: connection, err: err}
	}()
	<-dialStarted

	result := <-done
	if result.connection != nil {
		_ = result.connection.Close()
		t.Fatal("deadline-exceeded dial returned a connection")
	}
	if !errors.Is(result.err, context.DeadlineExceeded) {
		t.Fatalf("dial error = %v, want deadline exceeded", result.err)
	}

	close(releaseDial)
	select {
	case <-late.closed:
	case <-time.After(time.Second):
		t.Fatal("connection returned after deadline was not closed")
	}
}

func TestFetchRejectsTargetThatEscapesConfiguredOrigin(t *testing.T) {
	var calls atomic.Int64
	u := &Upstream{
		URL: "https://alice:secret@origin.example/repository",
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, errors.New("transport should not run")
		})},
	}

	for _, target := range []string{
		"https://evil.example/private",
		"//evil.example/private",
		"@evil.example/private",
		"relative/path",
		"/../admin",
		"/packages/../../admin",
		"/packages/%2e%2e/admin",
		"/packages/%2E%2E/admin",
		"/packages/%2e%2E/admin",
		"/packages/%252e%252e/admin",
		"/packages%252F..%252Fadmin",
		"/packages\\..\\admin",
		"/packages/%5c..%5cadmin",
		"/packages/%255c..%255cadmin",
		"/packages//item",
		"/packages/%2Fitem",
	} {
		t.Run(target, func(t *testing.T) {
			if _, err := u.Request(context.Background(), target, RequestOptions{}); err == nil {
				t.Errorf("Request target %q unexpectedly accepted", target)
			}
			if _, err := u.Fetch(context.Background(), target); err == nil {
				t.Errorf("Fetch target %q unexpectedly accepted", target)
			}
			if _, err := u.FetchWithHeaders(context.Background(), target, nil); err == nil {
				t.Errorf("FetchWithHeaders target %q unexpectedly accepted", target)
			}
		})
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("transport calls = %d, want 0", got)
	}
}

func TestFetchRejectsNonCanonicalConfiguredBasePath(t *testing.T) {
	var calls atomic.Int64
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("transport should not run")
	})}

	for _, base := range []string{
		"https://origin.example/repository/../private",
		"https://origin.example/repository/./private",
		"https://origin.example/repository/%2e%2e/private",
		"https://origin.example/repository/%252e%252e/private",
		"https://origin.example/repository\\private",
		"https://origin.example/repository/%5cprivate",
		"https://origin.example/repository//private",
	} {
		t.Run(base, func(t *testing.T) {
			u := &Upstream{URL: base, client: client}
			if _, err := u.Fetch(context.Background(), "/packages/item"); err == nil {
				t.Errorf("configured base %q unexpectedly accepted", base)
			}
		})
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("transport calls = %d, want 0", got)
	}
}

func TestOriginPathValidationBoundsNestedEscaping(t *testing.T) {
	if !validCanonicalOriginPath("/packages/%25literal", false) {
		t.Fatal("a normally escaped literal percent was rejected")
	}

	deeplyEscaped := "/packages/%literal"
	for range maxOriginPathDecodeLayers + 1 {
		deeplyEscaped = strings.ReplaceAll(deeplyEscaped, "%", "%25")
	}
	if validCanonicalOriginPath(deeplyEscaped, false) {
		t.Fatal("path that exceeds the nested-escape limit was accepted")
	}
}

func TestFetchPreservesConfiguredOriginUserInfoAndBasePath(t *testing.T) {
	var gotURL string
	u := &Upstream{
		Name: "private",
		URL:  "https://alice:secret@origin.example/repository/",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			gotURL = request.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("ok")),
				Request:    request,
			}, nil
		})},
	}

	result, err := u.Fetch(
		context.Background(),
		"/acme/model/resolve/refs%2Fpr%2F1/pkg-1.2.3/.well-known/item?channel=stable&signature=a%2Fb",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	if gotURL != "https://alice:secret@origin.example/repository/acme/model/resolve/refs%2Fpr%2F1/pkg-1.2.3/.well-known/item?channel=stable&signature=a%2Fb" {
		t.Fatalf("Fetch URL = %q, want configured credentials and base path", gotURL)
	}
	if result.URL != "https://origin.example/repository/acme/model/resolve/refs%2Fpr%2F1/pkg-1.2.3/.well-known/item?channel=stable&signature=a%2Fb" {
		t.Fatalf("FetchResult.URL = %q, want final URL without configured credentials", result.URL)
	}
}

func newTestUpstream(t *testing.T, rawURL string) *Upstream {
	t.Helper()
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID:          1,
		AdapterType: "huggingface",
		Name:        "test",
		URL:         rawURL,
		Priority:    1,
		Healthy:     true,
		SuccessRate: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return pool.Snapshot()[0]
}
