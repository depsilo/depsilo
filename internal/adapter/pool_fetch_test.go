package adapter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

// poolMember is an origin that answers every request with status, or serves
// its own name when status is 200; a closed member refuses connections.
type poolMember struct {
	name   string
	status int
	closed bool
}

func newTestPool(t *testing.T, members ...poolMember) (*upstream.PassiveRecoverySelector, []*upstream.Upstream, []*atomic.Int32) {
	t.Helper()
	configs := make([]config.UpstreamConfig, 0, len(members))
	hits := make([]*atomic.Int32, len(members))
	for i, member := range members {
		hits[i] = &atomic.Int32{}
		counter, status, name := hits[i], member.status, member.name
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			counter.Add(1)
			if status != http.StatusOK {
				w.WriteHeader(status)
				return
			}
			_, _ = io.WriteString(w, name)
		}))
		if member.closed {
			server.Close()
		} else {
			t.Cleanup(server.Close)
		}
		configs = append(configs, config.UpstreamConfig{Name: member.name, URL: server.URL, Priority: i + 1, ProbeMode: "passive"})
	}
	pool, err := upstream.NewPool(configs)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := pool.Snapshot()
	ordered := make([]*upstream.Upstream, len(members))
	for _, member := range snapshot {
		for i, wanted := range members {
			if member.Name == wanted.name {
				ordered[i] = member
			}
		}
	}
	return upstream.NewPassiveRecoverySelector(pool), ordered, hits
}

func TestFetchFromPoolAsksTheNextMemberOnlyWhenOneCannotProvide(t *testing.T) {
	for _, tc := range []struct {
		name         string
		first        poolMember
		retryMissing bool
		served       string // empty when the request must fail
		secondAsked  bool
	}{
		{"unreachable member", poolMember{name: "mirror", closed: true}, false, "official", true},
		{"server error", poolMember{name: "mirror", status: http.StatusServiceUnavailable}, false, "official", true},
		{"rate limited", poolMember{name: "mirror", status: http.StatusTooManyRequests}, false, "official", true},
		{"missing immutable artifact", poolMember{name: "mirror", status: http.StatusNotFound}, true, "official", true},
		{"missing where 404 is final", poolMember{name: "mirror", status: http.StatusNotFound}, false, "", false},
		{"forbidden", poolMember{name: "mirror", status: http.StatusForbidden}, true, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selector, members, hits := newTestPool(t, tc.first, poolMember{name: "official", status: http.StatusOK})
			ctx := context.Background()
			result, served, err := FetchFromPool(ctx, selector, members[0], tc.retryMissing, func(member *upstream.Upstream) (*upstream.FetchResult, error) {
				return member.Fetch(ctx, "/file")
			})
			if tc.served == "" {
				if err == nil {
					t.Fatalf("served by %s; want the first member's error", served.Name)
				}
			} else {
				if err != nil {
					t.Fatalf("fetch: %v", err)
				}
				body, _ := io.ReadAll(result.Body)
				result.Body.Close()
				if served.Name != tc.served || string(body) != tc.served {
					t.Fatalf("served by %s with %q, want %s", served.Name, body, tc.served)
				}
			}
			if asked := hits[1].Load() > 0; asked != tc.secondAsked {
				t.Fatalf("second member asked = %v, want %v", asked, tc.secondAsked)
			}
		})
	}
}

func TestFetchFromPoolAsksEachMemberOnceAndStopsWhenTheCallerLeaves(t *testing.T) {
	selector, members, hits := newTestPool(t,
		poolMember{name: "mirror", status: http.StatusBadGateway},
		poolMember{name: "second", status: http.StatusBadGateway},
		poolMember{name: "official", status: http.StatusServiceUnavailable})
	ctx := context.Background()
	_, served, err := FetchFromPool(ctx, selector, members[0], true, func(member *upstream.Upstream) (*upstream.FetchResult, error) {
		return member.Fetch(ctx, "/file")
	})
	if err == nil || served.Name != "official" {
		t.Fatalf("served=%v err=%v; want the last member's error", served.Name, err)
	}
	for i, hit := range hits {
		if hit.Load() != 1 {
			t.Fatalf("member %d asked %d times, want once", i, hit.Load())
		}
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	selector, members, hits = newTestPool(t, poolMember{name: "mirror", status: http.StatusBadGateway}, poolMember{name: "official", status: http.StatusOK})
	_, _, _ = FetchFromPool(cancelled, selector, members[0], true, func(member *upstream.Upstream) (*upstream.FetchResult, error) {
		return member.Fetch(context.Background(), "/file")
	})
	if hits[1].Load() != 0 {
		t.Fatal("asked another member after the caller left")
	}
}
