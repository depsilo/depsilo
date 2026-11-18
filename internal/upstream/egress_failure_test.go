package upstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"depsilo/internal/db"
	"depsilo/internal/egress"
)

func staticResolver(answers map[string]string) ipResolver {
	return resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
		if address, ok := answers[host]; ok {
			return []net.IPAddr{{IP: net.ParseIP(address)}}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	})
}

func recordingDial(dialed *[]string) func(context.Context, string, string) (net.Conn, error) {
	return func(_ context.Context, _, address string) (net.Conn, error) {
		*dialed = append(*dialed, address)
		connection, peer := net.Pipe()
		_ = peer.Close()
		return connection, nil
	}
}

func declaredSharedAddressSpace(t *testing.T) egress.Policy {
	t.Helper()
	policy, err := egress.NewPolicy("", nil, []string{"100.64.0.0/10"})
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

// fakeIPRefused is the [egress] policy with allow_fake_ip off.
func fakeIPRefused(t *testing.T) egress.Policy {
	t.Helper()
	policy, err := egress.NewPolicy("", nil, nil, egress.AllowFakeIP(false))
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestGuardExplainsFakeIPAndSharedAddressDenials(t *testing.T) {
	for _, test := range []struct {
		name, origin, address string
		policy                egress.Policy
		want                  Failure
	}{
		{"fake-IP answer for a public origin with allow_fake_ip off", "https://pypi.org", "198.18.0.65", fakeIPRefused(t),
			Failure{Code: FailureFakeIP, Address: "198.18.0.65", Range: "198.18.0.0/15"}},
		{"IPv6 fake-IP answer for a public origin with allow_fake_ip off", "https://pypi.org", "2001:2::46", fakeIPRefused(t),
			Failure{Code: FailureFakeIP, Address: "2001:2::46", Range: "2001:2::/48"}},
		// A unique-local range of proxy software is no private network: it
		// used to pin the origin as private and was dialed.
		{"unique-local fake-IP answer for a public origin", "https://pypi.org", "fd00:6152::46", egress.Policy{},
			Failure{Code: FailureFakeIP, Address: "fd00:6152::46", Range: "fd00:6152::/32"}},
		{"another IETF protocol assignment", "https://docs.example", "2001:3::1", egress.Policy{},
			Failure{Code: FailureReserved, Address: "2001:3::1", Range: "2001::/23"}},
		{"undeclared overlay peer", "http://parent.tailnet.ts.net:23333", "100.101.102.103", egress.Policy{},
			Failure{Code: FailureSharedAddress, Address: "100.101.102.103", Range: "100.64.0.0/10"}},
		{"cloud metadata stays reserved after a declaration", "http://metadata.example", "100.100.100.200", declaredSharedAddressSpace(t),
			Failure{Code: FailureSharedAddress, Address: "100.100.100.200", Range: "100.64.0.0/10"}},
		{"documentation address", "https://docs.example", "192.0.2.10", egress.Policy{},
			Failure{Code: FailureReserved, Address: "192.0.2.10", Range: "192.0.2.0/24"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			origin, _ := url.Parse(test.origin)
			var dialed []string
			dialer := &guardedDialer{
				origin:      origin,
				resolver:    staticResolver(map[string]string{origin.Hostname(): test.address}),
				dialContext: recordingDial(&dialed),
				policy:      test.policy,
			}
			_, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort(origin.Hostname(), "443"))
			if err == nil || err.Error() != "unsafe dial target" || len(dialed) != 0 {
				t.Fatalf("DialContext = %v after dialing %v, want an unsafe denial without a dial", err, dialed)
			}
			// The explanation survives the HTTP client's error wrapping.
			wrapped := &url.Error{Op: "Head", URL: test.origin, Err: err}
			if got := ClassifyFailure(wrapped); got == nil || *got != test.want {
				t.Fatalf("ClassifyFailure = %+v, want %+v", got, test.want)
			}
		})
	}
}

// Go's resolver merges A and AAAA answers and sorts them by RFC 6724, so either
// family's placeholder can come first; with allow_fake_ip off, the refusal
// reads as fake-IP either way.
func TestGuardExplainsDualStackFakeIPAnswersInEitherOrder(t *testing.T) {
	for _, test := range []struct {
		answer []string
		want   Failure
	}{
		{[]string{"2001:2::46", "198.18.0.70"}, Failure{Code: FailureFakeIP, Address: "2001:2::46", Range: "2001:2::/48"}},
		{[]string{"198.18.0.70", "2001:2::46"}, Failure{Code: FailureFakeIP, Address: "198.18.0.70", Range: "198.18.0.0/15"}},
	} {
		origin := mustURL(t, "https://pypi.org")
		var dialed []string
		dialer := &guardedDialer{
			origin: origin,
			resolver: resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
				answer := make([]net.IPAddr, 0, len(test.answer))
				for _, address := range test.answer {
					answer = append(answer, net.IPAddr{IP: net.ParseIP(address)})
				}
				return answer, nil
			}),
			dialContext: recordingDial(&dialed),
			policy:      fakeIPRefused(t),
		}
		_, err := dialer.DialContext(context.Background(), "tcp", "pypi.org:443")
		if err == nil || len(dialed) != 0 {
			t.Fatalf("answer %v: DialContext = %v after dialing %v, want an unsafe denial without a dial", test.answer, err, dialed)
		}
		if got := ClassifyFailure(&url.Error{Op: "Head", URL: "https://pypi.org", Err: err}); got == nil || *got != test.want {
			t.Fatalf("answer %v: ClassifyFailure = %+v, want %+v", test.answer, got, test.want)
		}
	}
}

func TestDeclaredRangeIsPrivateNetworkForConfiguredOriginsOnly(t *testing.T) {
	policy := declaredSharedAddressSpace(t)
	answers := map[string]string{
		"parent.tailnet.ts.net": "100.101.102.103",
		"pkg.tailnet.ts.net":    "100.101.102.104",
		"pypi.org":              "151.101.0.223",
	}

	// A configured overlay origin is reachable, and may reach its own network.
	peerOrigin, _ := url.Parse("http://parent.tailnet.ts.net:23333")
	var dialed []string
	peer := &guardedDialer{origin: peerOrigin, resolver: staticResolver(answers), dialContext: recordingDial(&dialed), policy: policy}
	for _, target := range []string{"parent.tailnet.ts.net:23333", "pkg.tailnet.ts.net:443"} {
		connection, err := peer.DialContext(context.Background(), "tcp", target)
		if err != nil {
			t.Fatalf("declared overlay target %s rejected: %v", target, err)
		}
		_ = connection.Close()
	}

	// A public origin still cannot redirect into the declared network.
	publicOrigin, _ := url.Parse("https://pypi.org")
	public := &guardedDialer{origin: publicOrigin, resolver: staticResolver(answers), dialContext: recordingDial(&dialed), policy: policy}
	_, err := public.DialContext(context.Background(), "tcp", "pkg.tailnet.ts.net:443")
	if err == nil || err.Error() != "private dial target rejected" {
		t.Fatalf("public origin reached a declared private address: %v", err)
	}
	if got := ClassifyFailure(err); got == nil || got.Code != FailurePrivate || got.Address != "100.101.102.104" {
		t.Fatalf("ClassifyFailure = %+v, want egress_private for 100.101.102.104", got)
	}
	if err := validateExternalTarget(publicOrigin, mustURL(t, "http://100.101.102.104/pkg"), true, policy); err == nil {
		t.Fatal("public origin accepted a literal declared-private redirect target")
	}
	if err := validateExternalTarget(peerOrigin, mustURL(t, "http://100.101.102.104/pkg"), true, policy); err != nil {
		t.Fatalf("overlay origin rejected a literal target in its own network: %v", err)
	}
	if err := validateExternalTarget(peerOrigin, mustURL(t, "http://100.100.100.200/latest/meta-data"), true, policy); err == nil {
		t.Fatal("declared range exposed the cloud metadata endpoint")
	}
	if len(dialed) != 2 {
		t.Fatalf("dialed %v, want only the two overlay targets", dialed)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestClassifyFailureNamesCommonCheckFailures(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{&url.Error{Op: "Head", URL: "https://x", Err: &net.DNSError{Err: "no such host", Name: "x", IsNotFound: true}}, FailureDNS},
		{fmt.Errorf("resolve dial target: %w", &net.DNSError{Err: "i/o timeout", Name: "x", IsTimeout: true}), FailureDNS},
		{&url.Error{Op: "Head", URL: "https://x", Err: context.DeadlineExceeded}, FailureTimeout},
		{&url.Error{Op: "Head", URL: "https://x", Err: x509.UnknownAuthorityError{}}, FailureTLS},
		{&url.Error{Op: "Head", URL: "https://x", Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}, FailureRefused},
		{&StatusError{Code: http.StatusBadGateway}, FailureHTTPStatus},
		{errors.New("something else"), FailureNetwork},
	} {
		if got := ClassifyFailure(test.err); got == nil || got.Code != test.want {
			t.Errorf("ClassifyFailure(%v) = %+v, want %s", test.err, got, test.want)
		}
	}
	if got := ClassifyFailure(&StatusError{Code: 503}); got.Status != 503 || (&StatusError{Code: 503}).Error() != "upstream returned status 503" {
		t.Fatalf("status failure = %+v", got)
	}
	if ClassifyFailure(nil) != nil {
		t.Fatal("nil error classified as a failure")
	}
}

// Go wraps the error of a failed connection to the proxy in a "proxyconnect"
// OpError, whose cause would otherwise read as the upstream's own DNS,
// timeout, TLS or refused connection. A CONNECT refusal keeps its status
// through every wrapping that a check, a fetch or a cascade parent adds.
func TestClassifyFailureNamesEgressProxyFailures(t *testing.T) {
	proxyConnect := func(cause error) error {
		return &url.Error{Op: "Head", URL: "https://pypi.example/", Err: &net.OpError{Op: "proxyconnect", Net: "tcp", Err: cause}}
	}
	for _, test := range []struct {
		name string
		err  error
		want Failure
	}{
		{"CONNECT refused", &url.Error{Op: "Head", URL: "https://pypi.example/", Err: &proxyConnectError{status: http.StatusBadGateway}},
			Failure{Code: FailureEgressProxy, Status: http.StatusBadGateway}},
		{"CONNECT refused during a fetch",
			fmt.Errorf("fetch https://pypi.example: %w", redactedTransportError{cause: &url.Error{Op: "Get", URL: "https://pypi.example/simple/", Err: &proxyConnectError{status: http.StatusProxyAuthRequired}}}),
			Failure{Code: FailureEgressProxy, Status: http.StatusProxyAuthRequired}},
		{"CONNECT to a cascade parent refused",
			&url.Error{Op: "Head", URL: "https://pypi.example/", Err: fmt.Errorf("cascade peer home: %w", &proxyConnectError{status: http.StatusForbidden})},
			Failure{Code: FailureEgressProxy, Status: http.StatusForbidden}},
		{"proxy refused the connection", proxyConnect(&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}),
			Failure{Code: FailureEgressProxy}},
		{"proxy host name did not resolve", proxyConnect(&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "proxy.example", IsNotFound: true}}),
			Failure{Code: FailureEgressProxy}},
		{"dial to the proxy timed out", proxyConnect(&net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}),
			Failure{Code: FailureEgressProxy}},
		{"TLS handshake with an https proxy failed", proxyConnect(x509.UnknownAuthorityError{}),
			Failure{Code: FailureEgressProxy}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifyFailure(test.err); got == nil || *got != test.want {
				t.Fatalf("ClassifyFailure(%v) = %+v, want %+v", test.err, got, test.want)
			}
		})
	}
	if text := (&proxyConnectError{status: http.StatusBadGateway}).Error(); text != "egress proxy refused CONNECT (502)" {
		t.Fatalf("CONNECT refusal reads %q", text)
	}
}

// connectProxy answers every CONNECT request with status and reports the
// targets it was asked to connect to. A refusal carries a status text and a
// body of the proxy's own. After a 200, which proxy software such as mihomo
// sends before it connects to the target, the proxy hands the tunnel and the
// client's side of it to serve, and closes the tunnel when serve returns.
func connectProxy(t *testing.T, status int, serve func(tunnel net.Conn, client io.Reader)) (proxyURL string, targets func() []string) {
	t.Helper()
	const body = "PROXY-BODY-CANARY"
	var mu sync.Mutex
	var connects []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodConnect {
			t.Errorf("proxy received %s %s, want CONNECT", request.Method, request.URL)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		connects = append(connects, request.Host)
		mu.Unlock()
		connection, buffered, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack CONNECT: %v", err)
			return
		}
		defer connection.Close()
		if status != http.StatusOK {
			_, _ = fmt.Fprintf(buffered, "HTTP/1.1 %d PROXY-STATUS-CANARY\r\nContent-Length: %d\r\n\r\n%s", status, len(body), body)
			_ = buffered.Flush()
			return
		}
		_, _ = fmt.Fprint(buffered, "HTTP/1.1 200 Connection established\r\n\r\n")
		_ = buffered.Flush()
		serve(connection, buffered.Reader)
	}))
	t.Cleanup(proxy.Close)
	return proxy.URL, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), connects...)
	}
}

// unreachableProxyURL is the URL of a local address that refuses connections.
func unreachableProxyURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return "http://" + address
}

// checkUpstream stores record as the only pypi Upstream and runs the Admin
// check on it, which records its failure as a health probe does.
func checkUpstream(t *testing.T, record db.UpstreamRecord, options PoolOptions) (RuntimeUpstream, ProbeResult) {
	t.Helper()
	database := bootstrapDB(t)
	record.AdapterType, record.Priority, record.ProbeMode, record.ProbeInterval, record.Healthy = "pypi", 1, "passive", "30m", true
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistryWithOptions(database, []string{"pypi"}, options)
	if err != nil {
		t.Fatal(err)
	}
	runtime, result, err := registry.Check(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Healthy || runtime.Healthy || result.Err == nil {
		t.Fatalf("check through a failing proxy passed: runtime=%+v result=%+v", runtime, result)
	}
	return runtime, result
}

// A proxy that refuses CONNECT fails the check at the proxy, with the status
// it answered, on each proxied route: the Upstream's own proxy, the [egress]
// default proxy, and the own proxy that reaches a cascade parent. Every status
// other than 200 counts, such as a corporate proxy's 407 for credentials or
// 403 for a host its rules deny. Neither the recorded failure nor the check's
// error, which Admin shows credential viewers, quotes the proxy's status text
// or body.
func TestProxiedCheckRecordsTheStatusOfARefusedConnect(t *testing.T) {
	peerURL := mustURL(t, "https://cache.example")
	routes := []struct {
		name       string
		record     func(proxy string) db.UpstreamRecord
		options    func(t *testing.T, proxy string) PoolOptions
		wantTarget string
	}{
		{
			name: "own proxy",
			record: func(proxy string) db.UpstreamRecord {
				return db.UpstreamRecord{Name: "own", URL: "https://pypi.example", Proxy: proxy}
			},
			options:    func(*testing.T, string) PoolOptions { return PoolOptions{} },
			wantTarget: "pypi.example:443",
		},
		{
			name:   "default proxy",
			record: func(string) db.UpstreamRecord { return db.UpstreamRecord{Name: "default", URL: "https://pypi.example"} },
			options: func(t *testing.T, proxy string) PoolOptions {
				policy, err := egress.NewPolicy(proxy, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				return PoolOptions{Egress: policy}
			},
			wantTarget: "pypi.example:443",
		},
		{
			name: "cascade parent behind the upstream's own proxy",
			record: func(proxy string) db.UpstreamRecord {
				return db.UpstreamRecord{Name: "relayed", URL: "https://pypi.example", Proxy: proxy, Via: "home"}
			},
			options: func(*testing.T, string) PoolOptions {
				return PoolOptions{Peers: NewPeerSet([]PeerEgress{{
					Name: "home", URL: peerURL, Token: "shared-token", LocalInstanceID: "child-node", MaxHops: 4,
				}})}
			},
			wantTarget: "cache.example:443",
		},
	}
	for _, route := range routes {
		for _, status := range []int{http.StatusBadGateway, http.StatusProxyAuthRequired, http.StatusForbidden} {
			t.Run(fmt.Sprintf("%s/%d", route.name, status), func(t *testing.T) {
				proxy, targets := connectProxy(t, status, nil)
				runtime, result := checkUpstream(t, route.record(proxy), route.options(t, proxy))

				want := Failure{Code: FailureEgressProxy, Status: status}
				if runtime.LastFailure == nil || *runtime.LastFailure != want {
					t.Fatalf("recorded failure = %+v, want %+v", runtime.LastFailure, want)
				}
				if got := targets(); len(got) != 1 || got[0] != route.wantTarget {
					t.Fatalf("CONNECT targets = %v, want [%s]", got, route.wantTarget)
				}
				text := result.Err.Error()
				if refusal := fmt.Sprintf("egress proxy refused CONNECT (%d)", status); !strings.Contains(text, refusal) {
					t.Fatalf("check error = %q, want %q", text, refusal)
				}
				for _, hidden := range []string{"CANARY", http.StatusText(status)} {
					if strings.Contains(text, hidden) {
						t.Fatalf("check error %q quotes the proxy's answer (%q)", text, hidden)
					}
				}
			})
		}
	}
}

// A proxy that accepts CONNECT carries the check to an https upstream, so the
// hook refuses only the other answers.
func TestProxiedHTTPSCheckPassesThroughAnAcceptedConnect(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodHead || request.Host != "example.com" {
			t.Errorf("origin received %s for %s, want HEAD for example.com", request.Method, request.Host)
		}
	}))
	defer origin.Close()
	proxy, targets := connectProxy(t, http.StatusOK, func(tunnel net.Conn, client io.Reader) {
		target, err := net.Dial("tcp", origin.Listener.Addr().String())
		if err != nil {
			return
		}
		go func() {
			_, _ = io.Copy(target, client)
			_ = target.Close()
		}()
		_, _ = io.Copy(tunnel, target)
	})
	// The test certificate names example.com, wherever the proxy connects.
	u, err := newUpstreamFromRecord(db.UpstreamRecord{
		ID: 1, AdapterType: "pypi", Name: "tunnelled", URL: "https://example.com", Proxy: proxy,
		Priority: 1, ProbeMode: "passive", ProbeInterval: "30m",
	}, PoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	transport := u.client.Transport.(*http.Transport)
	roots := x509.NewCertPool()
	roots.AddCert(origin.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	defer transport.CloseIdleConnections()

	if result := probe(context.Background(), u); !result.Healthy || result.Err != nil {
		t.Fatalf("check through a proxy that accepted CONNECT = %+v, want healthy", result)
	}
	if got := targets(); len(got) != 1 || got[0] != "example.com:443" {
		t.Fatalf("CONNECT targets = %v, want [example.com:443]", got)
	}
}

// Proxy software such as mihomo answers CONNECT with 200 before it connects
// to the target, and closes the tunnel when it cannot reach it. Nothing then
// tells the proxy's failure from the upstream's own, so the check keeps the
// code of the closed connection, as the deployment guide explains.
func TestTunnelClosedAfterAnAcceptedConnectKeepsItsConnectionCode(t *testing.T) {
	proxy, _ := connectProxy(t, http.StatusOK, func(net.Conn, io.Reader) {})
	runtime, result := checkUpstream(t, db.UpstreamRecord{Name: "unreached", URL: "https://pypi.example", Proxy: proxy}, PoolOptions{})
	if runtime.LastFailure == nil || (runtime.LastFailure.Code != FailureRefused && runtime.LastFailure.Code != FailureNetwork) {
		t.Fatalf("recorded failure = %+v after %v, want %s or %s", runtime.LastFailure, result.Err, FailureRefused, FailureNetwork)
	}
}

// When Depsilo cannot connect to the proxy at all, no status exists, and the
// refused connection is the proxy's, not the upstream's.
func TestProxiedCheckRecordsAnUnreachableEgressProxy(t *testing.T) {
	for _, origin := range []string{"https://pypi.example", "http://pypi.example"} {
		t.Run(origin, func(t *testing.T) {
			runtime, result := checkUpstream(t, db.UpstreamRecord{Name: "down", URL: origin, Proxy: unreachableProxyURL(t)}, PoolOptions{})
			want := Failure{Code: FailureEgressProxy}
			if runtime.LastFailure == nil || *runtime.LastFailure != want {
				t.Fatalf("recorded failure = %+v after %v, want %+v", runtime.LastFailure, result.Err, want)
			}
		})
	}
}

// A plain-http request through a proxy needs no CONNECT: the proxy's 5xx
// answer arrives like the upstream's own and keeps the http_status code.
func TestPlainHTTPUpstreamKeepsTheStatusThatItsProxyAnswers(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodHead || request.URL.Host != "pypi.example" {
			t.Errorf("proxy received %s %s, want HEAD for pypi.example", request.Method, request.URL)
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	runtime, _ := checkUpstream(t, db.UpstreamRecord{Name: "plain", URL: "http://pypi.example", Proxy: proxy.URL}, PoolOptions{})
	want := Failure{Code: FailureHTTPStatus, Status: http.StatusBadGateway}
	if runtime.LastFailure == nil || *runtime.LastFailure != want {
		t.Fatalf("recorded failure = %+v, want %+v", runtime.LastFailure, want)
	}
}

func TestProbeFailureIsReportedUntilAPassingProbe(t *testing.T) {
	u := &Upstream{}
	denied := unsafeDenial(egress.Policy{}, ipAddrs([]net.IPAddr{{IP: net.ParseIP("198.18.0.65")}}), "unsafe dial target")
	u.applyProbe(ProbeResult{Healthy: false, CheckedAt: time.Now(), Err: &url.Error{Op: "Head", URL: "https://pypi.org/", Err: denied}})
	failure := u.HealthSnapshot().LastFailure
	if failure == nil || failure.Code != FailureFakeIP || failure.Address != "198.18.0.65" {
		t.Fatalf("LastFailure = %+v, want the fake-IP explanation", failure)
	}
	failure.Code = "mutated"
	if u.HealthSnapshot().LastFailure.Code != FailureFakeIP {
		t.Fatal("HealthSnapshot exposed mutable failure state")
	}
	u.applyProbe(ProbeResult{Healthy: true, CheckedAt: time.Now().Add(time.Second), Latency: time.Millisecond})
	if got := u.HealthSnapshot().LastFailure; got != nil {
		t.Fatalf("passing probe kept LastFailure %+v", got)
	}
}

func TestDefaultUpstreamProxyAppliesOnlyToDirectUpstreamsWithoutTheirOwn(t *testing.T) {
	policy, err := egress.NewPolicy("http://127.0.0.1:7890", []string{"mirror.lan"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	options := PoolOptions{Egress: policy}
	for _, test := range []struct {
		name         string
		record       db.UpstreamRecord
		wantProxy    string
		wantSource   string
		wantDirectGu bool
	}{
		{"public mirror", db.UpstreamRecord{Name: "official", URL: "https://pypi.org"}, "http://127.0.0.1:7890", ProxyFromDefault, false},
		{"own proxy wins", db.UpstreamRecord{Name: "corp", URL: "https://pypi.org", Proxy: "http://proxy.corp:3128"}, "http://proxy.corp:3128", ProxyFromUpstream, false},
		{"no_proxy host", db.UpstreamRecord{Name: "lan", URL: "http://mirror.lan:8080"}, "", "", true},
		{"loopback origin", db.UpstreamRecord{Name: "local", URL: "http://127.0.0.1:9000"}, "", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			u, err := newUpstreamFromRecord(test.record, options)
			if err != nil {
				t.Fatal(err)
			}
			if u.proxySource != test.wantSource || u.directNetworkGuard != test.wantDirectGu || u.Proxy != test.record.Proxy {
				t.Fatalf("upstream proxy source=%q guard=%v configured=%q", u.proxySource, u.directNetworkGuard, u.Proxy)
			}
			transport := u.client.Transport.(*http.Transport)
			var got string
			if transport.Proxy != nil {
				proxyURL, err := transport.Proxy(&http.Request{URL: mustURL(t, test.record.URL)})
				if err != nil {
					t.Fatal(err)
				}
				if proxyURL != nil {
					got = proxyURL.String()
				}
			}
			if got != test.wantProxy {
				t.Fatalf("effective proxy = %q, want %q", got, test.wantProxy)
			}
		})
	}
}
