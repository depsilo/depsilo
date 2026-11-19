package upstream

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"depsilo/internal/db"
	"depsilo/internal/egress"
)

// noInterfaceAddresses stands in for a machine whose network interfaces hold
// none of the addresses that the tests' DNS answers name.
func noInterfaceAddresses() ([]net.Addr, error) { return nil, nil }

// interfacesHolding stands in for a machine whose network interfaces hold the
// given addresses.
func interfacesHolding(addresses ...string) func() ([]net.Addr, error) {
	return func() ([]net.Addr, error) {
		held := make([]net.Addr, 0, len(addresses))
		for _, address := range addresses {
			held = append(held, &net.IPAddr{IP: net.ParseIP(address)})
		}
		return held, nil
	}
}

// dnsAnswers resolves each host to its addresses, in their order.
func dnsAnswers(answers map[string][]string) ipResolver {
	return resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
		addresses, ok := answers[host]
		if !ok {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		answer := make([]net.IPAddr, 0, len(addresses))
		for _, address := range addresses {
			answer = append(answer, net.IPAddr{IP: net.ParseIP(address)})
		}
		return answer, nil
	})
}

// dialLog records every address dialed, from any goroutine.
type dialLog struct {
	mu     sync.Mutex
	dialed []string
}

func (log *dialLog) record(address string) {
	log.mu.Lock()
	log.dialed = append(log.dialed, address)
	log.mu.Unlock()
}

func (log *dialLog) addresses() []string {
	log.mu.Lock()
	defer log.mu.Unlock()
	return slices.Clone(log.dialed)
}

// connecting is a dialer that records each address and connects at once, as
// the TUN of proxy software does for a placeholder.
func (log *dialLog) connecting(_ context.Context, _, address string) (net.Conn, error) {
	log.record(address)
	connection, peer := net.Pipe()
	_ = peer.Close()
	return connection, nil
}

// failing is a dialer that records each address and fails at once, as a
// placeholder fails that nothing carries and that the network refuses.
func (log *dialLog) failing(_ context.Context, _, address string) (net.Conn, error) {
	log.record(address)
	return nil, errors.New("PLACEHOLDER-DIAL-CANARY: connect: network is unreachable")
}

// With allow_fake_ip on, the default, a host name answered only with fake-IP
// placeholders is dialed at those placeholders, IPv4 ones first, for the TUN
// of proxy software to carry.
func TestGuardedDialerDialsAFakeIPAnswerAtItsPlaceholders(t *testing.T) {
	for _, test := range []struct {
		name   string
		answer []string
		want   []string
	}{
		{name: "IPv4 placeholder", answer: []string{"198.18.0.65"}, want: []string{"198.18.0.65:443"}},
		{name: "IPv6 placeholder", answer: []string{"2001:2::46"}, want: []string{"[2001:2::46]:443"}},
		{name: "IPv6 placeholder listed first", answer: []string{"2001:2::46", "198.18.0.70"}, want: []string{"198.18.0.70:443"}},
		{name: "IPv4 placeholder listed first", answer: []string{"198.18.0.70", "2001:2::46"}, want: []string{"198.18.0.70:443"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var log dialLog
			dialer := &guardedDialer{
				origin:         mustURL(t, "https://pypi.org"),
				resolver:       dnsAnswers(map[string][]string{"pypi.org": test.answer}),
				dialContext:    log.connecting,
				interfaceAddrs: noInterfaceAddresses,
			}
			connection, err := dialer.DialContext(context.Background(), "tcp", "pypi.org:443")
			if err != nil {
				t.Fatalf("DialContext: %v", err)
			}
			_ = connection.Close()
			if got := log.addresses(); !slices.Equal(got, test.want) {
				t.Fatalf("dialed %v, want %v", got, test.want)
			}
		})
	}
}

// A fake-IP answer is checked as public, as through a proxy: any origin may
// reach it, and an origin with such an answer reaches no private or loopback
// target, also when the origin's answer is looked up only for that check.
func TestGuardedDialerChecksAFakeIPAnswerAsPublic(t *testing.T) {
	answers := dnsAnswers(map[string][]string{
		"pypi.org":      {"198.18.0.65"},
		"files.example": {"198.18.0.66"},
		"cdn.lan":       {"10.0.0.5"},
		"loop.lan":      {"127.0.0.1"},
		"mirror.lan":    {"192.168.1.20"},
	})
	newDialer := func(origin string, log *dialLog) *guardedDialer {
		return &guardedDialer{
			origin:         mustURL(t, origin),
			resolver:       answers,
			dialContext:    log.connecting,
			interfaceAddrs: noInterfaceAddresses,
		}
	}

	for _, test := range []struct {
		name string
		// first is dialed before target, if set, and pins the origin.
		first, target string
		want          Failure
	}{
		{name: "private target after the origin", first: "pypi.org:443", target: "cdn.lan:443",
			want: Failure{Code: FailurePrivate, Address: "10.0.0.5"}},
		{name: "loopback target after the origin", first: "pypi.org:443", target: "loop.lan:443",
			want: Failure{Code: FailurePrivate, Address: "127.0.0.1"}},
		{name: "private target before the origin", target: "cdn.lan:443",
			want: Failure{Code: FailurePrivate, Address: "10.0.0.5"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var log dialLog
			dialer := newDialer("https://pypi.org", &log)
			if test.first != "" {
				connection, err := dialer.DialContext(context.Background(), "tcp", test.first)
				if err != nil {
					t.Fatalf("origin dial: %v", err)
				}
				_ = connection.Close()
			}
			_, err := dialer.DialContext(context.Background(), "tcp", test.target)
			if got := ClassifyFailure(err); err == nil || *got != test.want {
				t.Fatalf("DialContext(%s) = %v (%+v), want the refusal %+v", test.target, err, got, test.want)
			}
			if got := log.addresses(); len(got) > 1 {
				t.Fatalf("dialed %v, want at most the origin", got)
			}
		})
	}

	// A private origin, such as a LAN mirror, may reach a fake-IP answer,
	// for example a redirect to public storage.
	var log dialLog
	dialer := newDialer("http://mirror.lan", &log)
	for _, target := range []string{"mirror.lan:80", "files.example:443"} {
		connection, err := dialer.DialContext(context.Background(), "tcp", target)
		if err != nil {
			t.Fatalf("private origin dialing %s: %v", target, err)
		}
		_ = connection.Close()
	}
	if got, want := log.addresses(), []string{"192.168.1.20:80", "198.18.0.66:443"}; !slices.Equal(got, want) {
		t.Fatalf("dialed %v, want %v", got, want)
	}
}

// A fake-IP answer pins the origin's network scope only once a connection to
// its placeholders succeeded, also when the origin's answer is looked up only
// to check another target. After nothing carried them, a real private answer
// for the origin, such as fake-ip-filter gives a mirror on the local network,
// is accepted without a restart. After a carried connection, the origin is
// pinned public, and a changed answer is refused.
func TestGuardedDialerPinsAFakeIPOriginOnlyOnceItConnected(t *testing.T) {
	for _, test := range []struct {
		name string
		// tun is whether the placeholder connects.
		tun bool
		// wantErr is the error of the origin's dial with its real answer.
		wantErr string
		dialed  []string
	}{
		{name: "nothing carried", dialed: []string{"198.18.0.65:80", "192.168.1.20:80", "10.0.0.5:80"}},
		{name: "carried by the TUN", tun: true, wantErr: "configured origin changed network scope", dialed: []string{"198.18.0.65:80"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mu sync.Mutex
			originAnswer := "198.18.0.65"
			answers := resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
				mu.Lock()
				defer mu.Unlock()
				switch host {
				case "mirror.example":
					return []net.IPAddr{{IP: net.ParseIP(originAnswer)}}, nil
				case "cdn.lan":
					return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil
				}
				return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
			})
			var log dialLog
			dialer := &guardedDialer{
				origin:   mustURL(t, "http://mirror.example"),
				resolver: answers,
				dialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
					if strings.HasPrefix(address, "198.18.") && !test.tun {
						return log.failing(ctx, network, address)
					}
					return log.connecting(ctx, network, address)
				},
				interfaceAddrs: noInterfaceAddresses,
			}
			ctx := context.Background()

			// A private target checked before the origin's first dial is
			// refused: the fake-IP answer counts as public.
			if _, err := dialer.DialContext(ctx, "tcp", "cdn.lan:80"); err == nil || ClassifyFailure(err).Code != FailurePrivate {
				t.Fatalf("private target before the origin = %v, want %s", err, FailurePrivate)
			}
			connection, err := dialer.DialContext(ctx, "tcp", "mirror.example:80")
			if test.tun != (err == nil) {
				t.Fatalf("origin's placeholder dial = %v, want success %v", err, test.tun)
			}
			if connection != nil {
				_ = connection.Close()
			}

			mu.Lock()
			originAnswer = "192.168.1.20"
			mu.Unlock()
			connection, err = dialer.DialContext(ctx, "tcp", "mirror.example:80")
			if test.wantErr != "" {
				if err == nil || err.Error() != test.wantErr {
					t.Fatalf("origin with its real answer = %v, want %q", err, test.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("origin with its real answer: %v", err)
				}
				_ = connection.Close()
				// Pinned private now, the origin may reach its network.
				connection, err = dialer.DialContext(ctx, "tcp", "cdn.lan:80")
				if err != nil {
					t.Fatalf("private target after the private origin: %v", err)
				}
				_ = connection.Close()
			}
			if got := log.addresses(); !slices.Equal(got, test.dialed) {
				t.Fatalf("dialed %v, want %v", got, test.dialed)
			}
		})
	}
}

// Only a fake-IP answer for a host name is dialed. An address written in the
// URL, an answer that mixes placeholders with other addresses, unique-local
// ones of proxy software included, and, with allow_fake_ip off, every
// fake-IP answer keep their refusal, egress_fake_ip.
// An answer that holds an address of this machine's network interfaces, such
// as the TUN address of the proxy software, or one that cannot be checked
// because the interfaces cannot be listed, is refused as egress_reserved.
// Nothing is dialed for any of them.
func TestGuardedDialerRefusesFakeIPLiteralsMixedAndLocalAnswers(t *testing.T) {
	cannotList := func() ([]net.Addr, error) { return nil, errors.New("netlink refused") }
	for _, test := range []struct {
		name, origin   string
		answer         []string
		policy         egress.Policy
		interfaceAddrs func() ([]net.Addr, error)
		want           Failure
	}{
		{name: "address literal", origin: "https://198.18.0.65",
			want: Failure{Code: FailureFakeIP, Address: "198.18.0.65", Range: "198.18.0.0/15"}},
		{name: "IPv6 address literal", origin: "https://[2001:2::46]",
			want: Failure{Code: FailureFakeIP, Address: "2001:2::46", Range: "2001:2::/48"}},
		{name: "placeholder and public address", answer: []string{"198.18.0.65", "151.101.0.223"},
			want: Failure{Code: FailureFakeIP, Address: "198.18.0.65", Range: "198.18.0.0/15"}},
		{name: "placeholder and private address", answer: []string{"10.0.0.5", "198.18.0.65"},
			want: Failure{Code: FailureFakeIP, Address: "198.18.0.65", Range: "198.18.0.0/15"}},
		// Proxy software with unique-local IPv6 fake-IP answers this way even
		// while its TUN runs. The failure names the unique-local address,
		// whose range the proxy software must move, in either order.
		{name: "placeholder and unique-local IPv6 fake-IP address", answer: []string{"198.18.0.20", "fdfe:dcba:9876::20"},
			want: Failure{Code: FailureFakeIP, Address: "fdfe:dcba:9876::20", Range: "fdfe:dcba:9876::/48"}},
		{name: "unique-local IPv6 fake-IP address and placeholder", answer: []string{"fdfe:dcba:9876::20", "198.18.0.20"},
			want: Failure{Code: FailureFakeIP, Address: "fdfe:dcba:9876::20", Range: "fdfe:dcba:9876::/48"}},
		{name: "allow_fake_ip off", answer: []string{"198.18.0.65"}, policy: fakeIPRefused(t),
			want: Failure{Code: FailureFakeIP, Address: "198.18.0.65", Range: "198.18.0.0/15"}},
		// Go's resolver merges A and AAAA answers and sorts them by RFC 6724,
		// so either family's placeholder can come first; with allow_fake_ip
		// off, the refusal reads as fake-IP either way.
		{name: "allow_fake_ip off, IPv6 placeholder first", answer: []string{"2001:2::46", "198.18.0.70"}, policy: fakeIPRefused(t),
			want: Failure{Code: FailureFakeIP, Address: "2001:2::46", Range: "2001:2::/48"}},
		{name: "allow_fake_ip off, IPv4 placeholder first", answer: []string{"198.18.0.70", "2001:2::46"}, policy: fakeIPRefused(t),
			want: Failure{Code: FailureFakeIP, Address: "198.18.0.70", Range: "198.18.0.0/15"}},
		{name: "TUN address of the proxy software", answer: []string{"198.18.0.1"}, interfaceAddrs: interfacesHolding("127.0.0.1", "198.18.0.1"),
			want: Failure{Code: FailureReserved, Address: "198.18.0.1"}},
		{name: "IPv6 TUN address next to a placeholder", answer: []string{"198.18.0.65", "2001:2::1"}, interfaceAddrs: interfacesHolding("2001:2::1"),
			want: Failure{Code: FailureReserved, Address: "2001:2::1"}},
		{name: "interfaces that cannot be listed", answer: []string{"198.18.0.65"}, interfaceAddrs: cannotList,
			want: Failure{Code: FailureReserved, Address: "198.18.0.65"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			origin := test.origin
			if origin == "" {
				origin = "https://pypi.org"
			}
			originURL := mustURL(t, origin)
			if test.interfaceAddrs == nil {
				test.interfaceAddrs = noInterfaceAddresses
			}
			var log dialLog
			dialer := &guardedDialer{
				origin:         originURL,
				resolver:       dnsAnswers(map[string][]string{originURL.Hostname(): test.answer}),
				dialContext:    log.connecting,
				policy:         test.policy,
				interfaceAddrs: test.interfaceAddrs,
			}
			_, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort(originURL.Hostname(), "443"))
			if err == nil || err.Error() != "unsafe dial target" {
				t.Fatalf("DialContext = %v, want the unsafe denial", err)
			}
			if got := ClassifyFailure(&url.Error{Op: "Head", URL: origin, Err: err}); *got != test.want {
				t.Fatalf("ClassifyFailure = %+v, want %+v", got, test.want)
			}
			if got := log.addresses(); len(got) != 0 {
				t.Fatalf("dialed %v, want nothing", got)
			}
		})
	}
}

// The TUN of proxy software completes a connection to a placeholder at once.
// When none of the placeholders connects, because the network refuses them
// or because they do not connect within the short timeout, nothing carries
// them, and the dial fails with egress_fake_ip, naming the IPv4 placeholder
// that was dialed first and its range. The error's text is fixed. A dial
// whose own context ends first keeps that context's error.
func TestGuardedDialerReportsAFakeIPAnswerThatNothingCarried(t *testing.T) {
	answers := dnsAnswers(map[string][]string{"pypi.org": {"2001:2::46", "198.18.0.70"}})
	want := Failure{Code: FailureFakeIP, Address: "198.18.0.70", Range: "198.18.0.0/15"}
	const fixed = "nothing carried the connection to the fake-IP dial target"

	// The families are not interleaved as for other answers: every IPv4
	// placeholder is dialed before an IPv6 one.
	t.Run("every IPv4 placeholder first", func(t *testing.T) {
		var log dialLog
		dialer := &guardedDialer{
			origin:      mustURL(t, "https://pypi.org"),
			resolver:    dnsAnswers(map[string][]string{"pypi.org": {"2001:2::1", "198.18.0.5", "198.18.0.6"}}),
			dialContext: log.failing, interfaceAddrs: noInterfaceAddresses,
		}
		_, err := dialer.DialContext(context.Background(), "tcp", "pypi.org:443")
		want := Failure{Code: FailureFakeIP, Address: "198.18.0.5", Range: "198.18.0.0/15"}
		if got := ClassifyFailure(err); err == nil || *got != want {
			t.Fatalf("DialContext = %v (%+v), want %+v", err, got, want)
		}
		if got, want := log.addresses(), []string{"198.18.0.5:443", "198.18.0.6:443", "[2001:2::1]:443"}; !slices.Equal(got, want) {
			t.Fatalf("dialed %v, want %v", got, want)
		}
	})

	t.Run("placeholders that never connect", func(t *testing.T) {
		var log dialLog
		hanging := func(ctx context.Context, _, address string) (net.Conn, error) {
			log.record(address)
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 50*time.Millisecond {
				t.Errorf("dial of %s has deadline %v (set %v), want one within the short timeout", address, deadline, ok)
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		dialer := &guardedDialer{
			origin: mustURL(t, "https://pypi.org"), resolver: answers, dialContext: hanging,
			interfaceAddrs: noInterfaceAddresses, fakeIPTimeout: 50 * time.Millisecond,
		}
		started := time.Now()
		_, err := dialer.DialContext(context.Background(), "tcp", "pypi.org:443")
		if err == nil || err.Error() != fixed {
			t.Fatalf("DialContext = %v, want %q", err, fixed)
		}
		if elapsed := time.Since(started); elapsed > 5*time.Second {
			t.Fatalf("the short timeout cut the dials after %v", elapsed)
		}
		if got := ClassifyFailure(err); *got != want {
			t.Fatalf("ClassifyFailure = %+v, want %+v; a timed-out placeholder is not the upstream's timeout", got, want)
		}
		if got := log.addresses(); len(got) != 2 {
			t.Fatalf("dialed %v, want both placeholders", got)
		}
	})

	t.Run("default timeout", func(t *testing.T) {
		var deadline time.Time
		var bounded bool
		dialer := &guardedDialer{
			origin: mustURL(t, "https://pypi.org"), resolver: dnsAnswers(map[string][]string{"pypi.org": {"198.18.0.70"}}),
			dialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				deadline, bounded = ctx.Deadline()
				return (&dialLog{}).connecting(ctx, network, address)
			},
			interfaceAddrs: noInterfaceAddresses,
		}
		started := time.Now()
		connection, err := dialer.DialContext(context.Background(), "tcp", "pypi.org:443")
		finished := time.Now()
		if err != nil {
			t.Fatal(err)
		}
		_ = connection.Close()
		if !bounded || deadline.Before(started.Add(egress.FakeIPConnectTimeout)) || deadline.After(finished.Add(egress.FakeIPConnectTimeout)) {
			t.Fatalf("placeholder dial deadline = %v after the start (set %v), want %v", deadline.Sub(started), bounded, egress.FakeIPConnectTimeout)
		}
	})

	t.Run("dial deadline shorter than the timeout", func(t *testing.T) {
		hanging := func(ctx context.Context, _, _ string) (net.Conn, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		dialer := &guardedDialer{
			origin: mustURL(t, "https://pypi.org"), resolver: answers, dialContext: hanging, interfaceAddrs: noInterfaceAddresses,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := dialer.DialContext(ctx, "tcp", "pypi.org:443")
		var notCarried *fakeIPNotCarriedError
		if !errors.Is(err, context.DeadlineExceeded) || errors.As(err, &notCarried) {
			t.Fatalf("DialContext = %v, want the request's own deadline", err)
		}
		if got := ClassifyFailure(err); got.Code != FailureTimeout {
			t.Fatalf("ClassifyFailure = %+v, want %s", got, FailureTimeout)
		}
	})
}

// checkDirectUpstream stores a direct Upstream at rawURL and runs the Admin
// check on it, as a health probe would, through the client that the pool
// builds with policy. Only the network under its guarded dialer is the
// test's: DNS gives answers, no network interface holds an address of them,
// and dial opens the connections.
func checkDirectUpstream(
	t *testing.T,
	rawURL string,
	policy egress.Policy,
	answers map[string][]string,
	dial func(context.Context, string, string) (net.Conn, error),
) (RuntimeUpstream, ProbeResult) {
	t.Helper()
	database := bootstrapDB(t)
	record := db.UpstreamRecord{
		AdapterType: "pypi", Name: "official", URL: rawURL, Priority: 1,
		ProbeMode: "passive", ProbeInterval: "30m", Healthy: true,
	}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistryWithOptions(database, []string{"pypi"}, PoolOptions{
		Egress:  policy,
		network: directNetwork{resolver: dnsAnswers(answers), dialContext: dial, interfaceAddrs: noInterfaceAddresses},
	})
	if err != nil {
		t.Fatal(err)
	}
	u, ok := registry.Pools()["pypi"].Find(record.ID)
	if !ok {
		t.Fatal("the stored upstream is not in the pool")
	}
	t.Cleanup(u.client.Transport.(*http.Transport).CloseIdleConnections)
	runtime, result, err := registry.Check(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, result
}

// checkBehindFakeIPDNS checks an Upstream at http://pypi.example, whose host
// DNS answers with the placeholder 198.18.0.65, under policy.
func checkBehindFakeIPDNS(
	t *testing.T,
	policy egress.Policy,
	dial func(context.Context, string, string) (net.Conn, error),
) (RuntimeUpstream, ProbeResult) {
	t.Helper()
	return checkDirectUpstream(t, "http://pypi.example", policy, map[string][]string{"pypi.example": {"198.18.0.65"}}, dial)
}

// The Admin check of an Upstream behind fake-IP DNS passes when the TUN
// carries the connection to the placeholder. When nothing carries it, the
// check records egress_fake_ip with the placeholder and its range, and its
// error, which Admin shows credential viewers, has the fixed text and not
// the dial error. With allow_fake_ip off, the check is refused before
// anything is dialed, as before.
func TestCheckOfAnUpstreamBehindFakeIPDNS(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Host != "pypi.example" {
			t.Errorf("request for host %q, want pypi.example", request.Host)
		}
	}))
	t.Cleanup(upstreamServer.Close)
	want := Failure{Code: FailureFakeIP, Address: "198.18.0.65", Range: "198.18.0.0/15"}

	t.Run("carried by the TUN", func(t *testing.T) {
		var log dialLog
		tun := func(ctx context.Context, network, address string) (net.Conn, error) {
			log.record(address)
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, upstreamServer.Listener.Addr().String())
		}
		runtime, result := checkBehindFakeIPDNS(t, egress.Policy{}, tun)
		if !result.Healthy || result.Err != nil || !runtime.Healthy || runtime.LastFailure != nil {
			t.Fatalf("check = %+v, upstream = %+v; want a passing check", result, runtime)
		}
		if got := log.addresses(); !slices.Equal(got, []string{"198.18.0.65:80"}) {
			t.Fatalf("dialed %v, want the placeholder", got)
		}
	})

	t.Run("nothing carried", func(t *testing.T) {
		var log dialLog
		runtime, result := checkBehindFakeIPDNS(t, egress.Policy{}, log.failing)
		if result.Healthy || runtime.LastFailure == nil || *runtime.LastFailure != want {
			t.Fatalf("recorded failure = %+v after %v, want %+v", runtime.LastFailure, result.Err, want)
		}
		text := result.Err.Error()
		if !strings.Contains(text, "nothing carried the connection to the fake-IP dial target") || strings.Contains(text, "CANARY") {
			t.Fatalf("check error = %q, want the fixed text without the dial error", text)
		}
		if got := log.addresses(); !slices.Equal(got, []string{"198.18.0.65:80"}) {
			t.Fatalf("dialed %v, want the placeholder", got)
		}
	})

	t.Run("allow_fake_ip off", func(t *testing.T) {
		var log dialLog
		runtime, result := checkBehindFakeIPDNS(t, fakeIPRefused(t), log.connecting)
		if result.Healthy || runtime.LastFailure == nil || *runtime.LastFailure != want {
			t.Fatalf("recorded failure = %+v after %v, want %+v", runtime.LastFailure, result.Err, want)
		}
		if !strings.Contains(result.Err.Error(), "unsafe dial target") {
			t.Fatalf("check error = %q, want the guard's refusal", result.Err)
		}
		if got := log.addresses(); len(got) != 0 {
			t.Fatalf("dialed %v, want nothing", got)
		}
	})
}

// The pool's [egress] policy reaches the guarded dialer of a direct
// Upstream's client with its declared private ranges too: a declared range
// admits an overlay host that the default policy refuses before any dial.
func TestCheckOfADirectUpstreamFollowsDeclaredPrivateRanges(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(upstreamServer.Close)
	answers := map[string][]string{"mirror.tailnet.example": {"100.101.102.103"}}
	for _, test := range []struct {
		name   string
		policy egress.Policy
		want   *Failure
		dialed []string
	}{
		{name: "declared range", policy: declaredSharedAddressSpace(t), dialed: []string{"100.101.102.103:80"}},
		{name: "no declaration", want: &Failure{Code: FailureSharedAddress, Address: "100.101.102.103", Range: "100.64.0.0/10"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var log dialLog
			dial := func(ctx context.Context, network, address string) (net.Conn, error) {
				log.record(address)
				var dialer net.Dialer
				return dialer.DialContext(ctx, network, upstreamServer.Listener.Addr().String())
			}
			runtime, result := checkDirectUpstream(t, "http://mirror.tailnet.example", test.policy, answers, dial)
			switch {
			case test.want == nil && (!result.Healthy || runtime.LastFailure != nil):
				t.Fatalf("check = %+v, upstream = %+v; want a passing check", result, runtime)
			case test.want != nil && (result.Healthy || runtime.LastFailure == nil || *runtime.LastFailure != *test.want):
				t.Fatalf("recorded failure = %+v after %v, want %+v", runtime.LastFailure, result.Err, *test.want)
			}
			if got := log.addresses(); !slices.Equal(got, test.dialed) {
				t.Fatalf("dialed %v, want %v", got, test.dialed)
			}
		})
	}
}
