package upstream

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
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

func TestGuardExplainsFakeIPAndSharedAddressDenials(t *testing.T) {
	for _, test := range []struct {
		name, origin, address string
		policy                egress.Policy
		want                  Failure
	}{
		{"fake-IP answer for a public origin", "https://pypi.org", "198.18.0.65", egress.Policy{},
			Failure{Code: FailureFakeIP, Address: "198.18.0.65", Range: "198.18.0.0/15"}},
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
