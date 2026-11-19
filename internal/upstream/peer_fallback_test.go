package upstream

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/cascade"
	"depsilo/internal/db"
)

// fallbackFixture is an origin and a parent; the parent answers with
// respond and counts its exchanges.
type fallbackFixture struct {
	origin      *httptest.Server
	parent      *httptest.Server
	parentCalls atomic.Int32
	originCalls atomic.Int32
}

func newFallbackFixture(t *testing.T, respond func(http.ResponseWriter)) *fallbackFixture {
	t.Helper()
	fixture := &fallbackFixture{}
	fixture.origin = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fixture.originCalls.Add(1)
		_, _ = io.WriteString(writer, "from-origin")
	}))
	t.Cleanup(fixture.origin.Close)
	fixture.parent = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fixture.parentCalls.Add(1)
		respond(writer)
	}))
	t.Cleanup(fixture.parent.Close)
	return fixture
}

func (f *fallbackFixture) client(t *testing.T, fallback bool) (*http.Client, *PeerStatus) {
	t.Helper()
	parentURL, err := url.Parse(f.parent.URL)
	if err != nil {
		t.Fatal(err)
	}
	set := NewPeerSet([]PeerEgress{{
		Name: "home", URL: parentURL, Token: "shared-token", LocalInstanceID: "child-node",
		MaxHops: 4, DirectFallback: fallback,
	}})
	peer, _ := set.Resolve("home")
	client, err := buildPeerClient(db.UpstreamRecord{URL: f.origin.URL}, peer, PoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	status, _ := set.Status("home")
	return client, status
}

func fetchBody(t *testing.T, client *http.Client, target string) (int, string, error) {
	t.Helper()
	response, err := client.Get(target)
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body), nil
}

func TestPeerFallbackServesFromTheParentWhileItWorks(t *testing.T) {
	fixture := newFallbackFixture(t, func(writer http.ResponseWriter) {
		writer.Header().Set(cascade.HeaderInstance, "parent-node")
		_, _ = io.WriteString(writer, "from-parent")
	})
	client, status := fixture.client(t, true)
	if code, body, err := fetchBody(t, client, fixture.origin.URL+"/pkg"); err != nil || code != 200 || body != "from-parent" {
		t.Fatalf("fetch = %d %q %v", code, body, err)
	}
	if fixture.originCalls.Load() != 0 {
		t.Fatal("a working parent must keep the exchange off the origin")
	}
	if snapshot := status.Snapshot(); snapshot.LastSuccessAt.IsZero() || snapshot.FallbackCount != 0 {
		t.Fatalf("status = %+v", snapshot)
	}
}

func TestPeerFallbackGoesDirectWhenTheParentCannotServe(t *testing.T) {
	for name, respond := range map[string]func(http.ResponseWriter){
		"not a relay": func(writer http.ResponseWriter) {
			writer.WriteHeader(http.StatusNotFound)
		},
		"token rejected": func(writer http.ResponseWriter) {
			writer.Header().Set(cascade.HeaderInstance, "parent-node")
			writer.Header().Set(cascade.HeaderRelayError, "CASCADE_UNAUTHORIZED")
			writer.WriteHeader(http.StatusUnauthorized)
		},
		"parent cannot reach the origin": func(writer http.ResponseWriter) {
			writer.Header().Set(cascade.HeaderInstance, "parent-node")
			writer.Header().Set(cascade.HeaderRelayError, "RELAY_UPSTREAM_UNAVAILABLE")
			writer.WriteHeader(http.StatusBadGateway)
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newFallbackFixture(t, respond)
			client, status := fixture.client(t, true)
			for range 3 {
				if code, body, err := fetchBody(t, client, fixture.origin.URL+"/pkg"); err != nil || code != 200 || body != "from-origin" {
					t.Fatalf("fetch = %d %q %v", code, body, err)
				}
			}
			// After one failure the parent is skipped for a while instead of
			// being asked again on every exchange.
			if calls := fixture.parentCalls.Load(); calls != 1 {
				t.Fatalf("parent calls = %d, want 1", calls)
			}
			snapshot := status.Snapshot()
			if snapshot.FallbackCount != 3 || snapshot.LastFailure == "" || snapshot.LastFallbackAt.IsZero() {
				t.Fatalf("status = %+v", snapshot)
			}
		})
	}
}

func TestPeerFallbackRetriesTheParentAfterTheCooldown(t *testing.T) {
	var healthy atomic.Bool
	fixture := newFallbackFixture(t, func(writer http.ResponseWriter) {
		if !healthy.Load() {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.Header().Set(cascade.HeaderInstance, "parent-node")
		_, _ = io.WriteString(writer, "from-parent")
	})
	client, status := fixture.client(t, true)
	clock := time.Now()
	status.now = func() time.Time { return clock }
	if _, body, _ := fetchBody(t, client, fixture.origin.URL+"/pkg"); body != "from-origin" {
		t.Fatalf("body = %q", body)
	}
	healthy.Store(true)
	clock = clock.Add(peerRetryAfter + time.Second)
	if _, body, _ := fetchBody(t, client, fixture.origin.URL+"/pkg"); body != "from-parent" {
		t.Fatalf("after the cooldown body = %q, want the parent's", body)
	}
	if snapshot := status.Snapshot(); snapshot.LastFailure != "" {
		t.Fatalf("a success through the parent must clear the failure: %+v", snapshot)
	}
}

func TestPeerFallbackPassesOnTheOriginsOwnAnswer(t *testing.T) {
	fixture := newFallbackFixture(t, func(writer http.ResponseWriter) {
		writer.Header().Set(cascade.HeaderInstance, "parent-node")
		writer.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(writer, `{"code":"UPSTREAM_ERROR"}`)
	})
	client, _ := fixture.client(t, true)
	if code, _, err := fetchBody(t, client, fixture.origin.URL+"/missing"); err != nil || code != http.StatusNotFound {
		t.Fatalf("fetch = %d %v, want the relayed 404", code, err)
	}
	if fixture.originCalls.Load() != 0 {
		t.Fatal("an origin answer relayed by the parent must not trigger a direct retry")
	}
}

func TestPeerWithoutFallbackFailsWhenTheParentIsDown(t *testing.T) {
	fixture := newFallbackFixture(t, func(http.ResponseWriter) {})
	client, _ := fixture.client(t, false)
	fixture.parent.Close()
	if _, _, err := fetchBody(t, client, fixture.origin.URL+"/pkg"); err == nil {
		t.Fatal("without direct_fallback a down parent must fail the exchange")
	}
	if fixture.originCalls.Load() != 0 {
		t.Fatal("without direct_fallback the origin must not be contacted")
	}
}

func TestPeerFallbackSurvivesAParentThatIsDown(t *testing.T) {
	fixture := newFallbackFixture(t, func(http.ResponseWriter) {})
	client, status := fixture.client(t, true)
	fixture.parent.Close()
	if code, body, err := fetchBody(t, client, fixture.origin.URL+"/pkg"); err != nil || code != 200 || body != "from-origin" {
		t.Fatalf("fetch = %d %q %v", code, body, err)
	}
	if status.Snapshot().LastFailure == "" {
		t.Fatal("the failure was not recorded")
	}
}
