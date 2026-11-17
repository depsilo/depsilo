package upstream

import (
	"context"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"depsilo/internal/db"
	"depsilo/internal/egress"
)

// refuseByGuard records a failed check that the outbound guard refused
// because the upstream's host resolved to address.
func refuseByGuard(t *testing.T, u *Upstream, address string) {
	t.Helper()
	addresses := ipAddrs([]net.IPAddr{{IP: net.ParseIP(address)}})
	// A private or loopback answer is refused for its scope, not as unsafe.
	denied := scopeDenial(addresses, "private dial target")
	if addrNetworkScope(egress.Policy{}, addresses[0]) == networkUnsafe {
		denied = unsafeDenial(egress.Policy{}, addresses, "unsafe dial target")
	}
	u.applyProbe(ProbeResult{
		Healthy: false, CheckedAt: time.Now(),
		Err: &url.Error{Op: "Head", URL: u.URL, Err: denied},
	})
}

func unhealthyPool(t *testing.T) *Pool {
	t.Helper()
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{
		{ID: 1, AdapterType: "pypi", Name: "official", URL: "https://pypi.example", Priority: 1, ProbeMode: "active", ProbeInterval: "30m"},
		{ID: 2, AdapterType: "pypi", Name: "mirror", URL: "https://mirror.example", Priority: 2, ProbeMode: "active", ProbeInterval: "30m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestAllUnhealthyErrorNamesTheSharedEgressReason(t *testing.T) {
	const plain = "all upstreams are unhealthy"
	const docs = "docs/deployment.md#upstream-network-egress"
	for _, test := range []struct {
		name      string
		addresses []string // one refused answer per upstream; "" fails without a guard refusal
		want      []string
	}{
		{"fake-IP answers for every upstream", []string{"198.18.0.65", "198.19.4.1"},
			[]string{plain + ": ", "(" + FailureFakeIP + ")", "set [egress] upstream_proxy in config.toml (in a container, the environment variable DEPSILO_EGRESS_UPSTREAM_PROXY) and restart", docs}},
		{"overlay addresses for every upstream", []string{"100.101.102.103", "100.101.102.104"},
			[]string{plain + ": ", "(" + FailureSharedAddress + ")", "[egress] private_ranges", docs}},
		{"reserved addresses for every upstream", []string{"192.0.2.10", "192.0.2.11"},
			[]string{plain + ": ", "(" + FailureReserved + ")", docs}},
		{"private addresses for every upstream", []string{"10.0.0.5", "10.0.0.6"},
			[]string{plain + ": ", "(" + FailurePrivate + ")", docs}},
		{"different guard refusals", []string{"198.18.0.65", "100.101.102.103"}, nil},
		{"one upstream failed for another reason", []string{"198.18.0.65", ""}, nil},
	} {
		for _, selector := range []struct {
			name  string
			build func(*Pool) Selector
		}{
			{"priority", func(pool *Pool) Selector { return NewPrioritySelector(pool) }},
			// An active upstream never enters passive recovery, so this pool
			// reaches that selector's final error as well.
			{"passive recovery", func(pool *Pool) Selector { return NewPassiveRecoverySelector(pool) }},
		} {
			t.Run(test.name+"/"+selector.name, func(t *testing.T) {
				pool := unhealthyPool(t)
				for index, u := range pool.Snapshot() {
					if test.addresses[index] == "" {
						u.applyProbe(ProbeResult{Healthy: false, CheckedAt: time.Now(), Err: &StatusError{Code: 503}})
						continue
					}
					refuseByGuard(t, u, test.addresses[index])
				}

				_, err := selector.build(pool).Select(context.Background())
				if err == nil {
					t.Fatal("selected an upstream from a pool with no healthy member")
				}
				message := err.Error()
				if test.want == nil {
					if message != plain {
						t.Fatalf("error = %q, want exactly %q", message, plain)
					}
					return
				}
				for _, part := range test.want {
					if !strings.Contains(message, part) {
						t.Fatalf("error = %q, want it to contain %q", message, part)
					}
				}
				// The reason is a bounded code: no refused address, range, or
				// upstream host may reach an anonymous package client.
				for _, private := range append([]string{"198.18", "198.19", "100.101", "100.64", "192.0.2", "10.0.0", "pypi.example", "mirror.example"}, test.addresses...) {
					if strings.Contains(message, private) {
						t.Fatalf("error %q leaks %q", message, private)
					}
				}
			})
		}
	}
}

// A proxy that is down, or that cannot reach the upstreams it carries, fails
// them all at once. The message names the proxy and its remedy, whichever
// way each check failed there, and never calls it a refusal by the outbound
// guard. [egress] no_proxy keeps hosts off upstream_proxy only, never off an
// upstream's own proxy, and the remedy says so. Like a guard refusal, the
// message carries the code only: no status, address or host name.
func TestAllUnhealthyErrorNamesAFailureAtTheEgressProxy(t *testing.T) {
	const want = "all upstreams are unhealthy: every upstream failed at the egress proxy (egress_proxy); " +
		"check that the proxy runs and that [egress] upstream_proxy or an upstream's own proxy points at it; " +
		"to keep hosts on your own network off upstream_proxy, list them in [egress] no_proxy and restart, " +
		"see docs/deployment.md#upstream-network-egress"
	failAtProxy := func(u *Upstream, cause error) {
		u.applyProbe(ProbeResult{Healthy: false, CheckedAt: time.Now(), Err: &url.Error{Op: "Head", URL: u.URL, Err: cause}})
	}
	unreachable := &net.OpError{Op: "proxyconnect", Net: "tcp", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}}
	for _, selector := range []struct {
		name  string
		build func(*Pool) Selector
	}{
		{"priority", func(pool *Pool) Selector { return NewPrioritySelector(pool) }},
		{"passive recovery", func(pool *Pool) Selector { return NewPassiveRecoverySelector(pool) }},
	} {
		t.Run(selector.name, func(t *testing.T) {
			pool := unhealthyPool(t)
			upstreams := pool.Snapshot()
			failAtProxy(upstreams[0], unreachable)
			failAtProxy(upstreams[1], &proxyConnectError{status: 502})
			_, err := selector.build(pool).Select(context.Background())
			if err == nil || err.Error() != want {
				t.Fatalf("error = %v, want %q", err, want)
			}
			if strings.Contains(err.Error(), "outbound guard") || strings.Contains(err.Error(), "502") {
				t.Fatalf("error %q blames the guard or repeats a status", err)
			}

			// A guard refusal of the other upstream is a different reason.
			refuseByGuard(t, upstreams[1], "198.18.0.65")
			if _, err := selector.build(pool).Select(context.Background()); err == nil || err.Error() != "all upstreams are unhealthy" {
				t.Fatalf("error with mixed egress reasons = %v, want the plain message", err)
			}
		})
	}
}

func TestAllUnhealthyErrorStaysPlainWithoutARecordedReason(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "pypi", Name: "official", URL: "https://pypi.example", Priority: 1,
		ProbeMode: "active", ProbeInterval: "30m", Healthy: false,
	}})
	if err != nil {
		t.Fatal(err)
	}
	u := pool.Snapshot()[0]
	u.mu.Lock()
	u.health.healthy = false
	u.mu.Unlock()
	if _, err := NewPrioritySelector(pool).Select(context.Background()); err == nil || err.Error() != "all upstreams are unhealthy" {
		t.Fatalf("error = %v, want the plain message", err)
	}

	// A passing check clears the reason even if later traffic fails again.
	refuseByGuard(t, u, "198.18.0.65")
	u.applyProbe(ProbeResult{Healthy: true, CheckedAt: time.Now().Add(time.Second), Latency: time.Millisecond})
	u.mu.Lock()
	u.health.healthy = false
	u.mu.Unlock()
	if _, err := NewPrioritySelector(pool).Select(context.Background()); err == nil || err.Error() != "all upstreams are unhealthy" {
		t.Fatalf("error after a passing check = %v, want the plain message", err)
	}

	// So does traffic that succeeded after the refused check: the failures
	// that follow have another cause, and the old reason would misdirect.
	for _, mode := range []string{"active", "passive"} {
		recovered, err := NewPoolFromRecords([]db.UpstreamRecord{{
			ID: 1, AdapterType: "pypi", Name: "official", URL: "https://pypi.example", Priority: 1,
			ProbeMode: mode, ProbeInterval: "30m",
		}})
		if err != nil {
			t.Fatal(err)
		}
		u := recovered.Snapshot()[0]
		refuseByGuard(t, u, "198.18.0.65")
		if _, err := NewPrioritySelector(recovered).Select(context.Background()); err == nil || !strings.Contains(err.Error(), FailureFakeIP) {
			t.Fatalf("%s: error after the refused check = %v, want the %s reason", mode, err, FailureFakeIP)
		}
		for range 5 {
			u.Report(time.Millisecond, true)
		}
		if !u.HealthSnapshot().Healthy {
			t.Fatalf("%s: successful traffic did not make the upstream healthy", mode)
		}
		for range 20 {
			u.Report(time.Millisecond, false)
		}
		if _, err := NewPrioritySelector(recovered).Select(context.Background()); err == nil || err.Error() != "all upstreams are unhealthy" {
			t.Fatalf("%s: error after traffic recovered and failed again = %v, want the plain message", mode, err)
		}
	}

	empty, err := NewPoolFromRecords(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPrioritySelector(empty).Select(context.Background()); err == nil || err.Error() != "all upstreams are unhealthy" {
		t.Fatalf("empty pool error = %v, want the plain message", err)
	}
}
