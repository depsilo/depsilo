package upstream

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"testing"

	"depsilo/internal/cascade"
	"depsilo/internal/db"
)

func peerRegistry(t *testing.T, rows ...db.UpstreamRecord) *Registry {
	t.Helper()
	database := bootstrapDB(t)
	ecosystems := map[string]bool{}
	for _, row := range rows {
		row.ProbeMode, row.ProbeInterval, row.Healthy = "passive", "30m", true
		if err := database.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		ecosystems[row.AdapterType] = true
	}
	active := make([]string, 0, len(ecosystems))
	for name := range ecosystems {
		active = append(active, name)
	}
	registry, err := NewRegistry(database, active)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func testPeerSet(t *testing.T, name, rawURL string) *PeerSet {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return NewPeerSet([]PeerEgress{{Name: name, URL: parsed, Token: "shared-token-0123456789", LocalInstanceID: "child-node", MaxHops: 4}})
}

func transportOf(t *testing.T, registry *Registry, ecosystem, name string) http.RoundTripper {
	t.Helper()
	for _, item := range registry.Pools()[ecosystem].Snapshot() {
		if item.Name == name {
			return item.client.Transport
		}
	}
	t.Fatalf("%s/%s not found", ecosystem, name)
	return nil
}

func TestRegistrySetPeersRebuildsOnlyBoundUpstreams(t *testing.T) {
	registry := peerRegistry(t,
		db.UpstreamRecord{AdapterType: "npm", Name: "via-home", URL: "https://registry.npmjs.org", Priority: 1, Via: "home"},
		db.UpstreamRecord{AdapterType: "npm", Name: "direct", URL: "https://registry.npmjs.org", Priority: 2},
	)
	direct := registry.Pools()["npm"].Snapshot()[1]
	if _, ok := transportOf(t, registry, "npm", "via-home").(*peerTransport); ok {
		t.Fatal("without peers the bound upstream must egress directly")
	}

	if err := registry.SetPeers(testPeerSet(t, "home", "https://cache.example")); err != nil {
		t.Fatal(err)
	}
	if _, ok := transportOf(t, registry, "npm", "via-home").(*peerTransport); !ok {
		t.Fatal("the bound upstream did not switch to its parent")
	}
	if registry.Pools()["npm"].Snapshot()[1] != direct {
		t.Fatal("an unbound upstream must keep its runtime object")
	}

	if err := registry.SetPeers(nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := transportOf(t, registry, "npm", "via-home").(*peerTransport); ok {
		t.Fatal("clearing the peers must return the bound upstream to direct egress")
	}
}

func TestRegistryRouteAllViaBindsDirectPublicUpstreams(t *testing.T) {
	registry := peerRegistry(t,
		db.UpstreamRecord{AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1},
		db.UpstreamRecord{AdapterType: "npm", Name: "intranet", URL: "http://10.0.0.5:8080", Priority: 2},
		db.UpstreamRecord{AdapterType: "pypi", Name: "official", URL: "https://pypi.org", Priority: 1},
		db.UpstreamRecord{AdapterType: "pypi", Name: "other-parent", URL: "https://pypi.example", Priority: 2, Via: "other"},
	)
	peers := NewPeerSet([]PeerEgress{
		{Name: "home", URL: mustURL(t, "https://cache.example"), Token: "shared-token-0123456789", LocalInstanceID: "child-node", MaxHops: 4},
		{Name: "other", URL: mustURL(t, "https://other.example"), Token: "shared-token-0123456789", LocalInstanceID: "child-node", MaxHops: 4},
	})
	if err := registry.SetPeers(peers); err != nil {
		t.Fatal(err)
	}
	result, err := registry.RouteAllVia(context.Background(), "home")
	if err != nil {
		t.Fatal(err)
	}
	if result.Bound != 2 || !reflect.DeepEqual(result.Skipped, []string{"npm/intranet"}) {
		t.Fatalf("result = %+v", result)
	}
	got := map[string]string{}
	for _, item := range registry.List() {
		got[item.AdapterType+"/"+item.Name] = item.Via
	}
	want := map[string]string{"npm/npmjs": "home", "npm/intranet": "", "pypi/official": "home", "pypi/other-parent": "other"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bindings = %v, want %v", got, want)
	}
	if _, ok := transportOf(t, registry, "pypi", "official").(*peerTransport); !ok {
		t.Fatal("a routed upstream did not switch to its parent at once")
	}
	if _, err := registry.RouteAllVia(context.Background(), "missing"); !errors.Is(err, ErrInvalidUpstream) {
		t.Fatalf("unknown peer err = %v", err)
	}
}

func TestLocalOriginRecognizesHostsAParentCannotReach(t *testing.T) {
	var local []string
	for _, raw := range []string{
		"http://127.0.0.1:8080", "http://10.1.2.3", "http://192.168.1.5/simple", "http://[fd00::1]",
		"http://localhost:3000", "http://nexus", "https://nexus.corp", "https://mirror.lan",
		"https://pypi.org", "https://registry.npmjs.org", "https://8.8.8.8",
	} {
		if localOrigin(raw) {
			local = append(local, raw)
		}
	}
	sort.Strings(local)
	want := []string{"http://10.1.2.3", "http://127.0.0.1:8080", "http://192.168.1.5/simple", "http://[fd00::1]", "http://localhost:3000", "http://nexus", "https://mirror.lan", "https://nexus.corp"}
	if !reflect.DeepEqual(local, want) {
		t.Fatalf("local = %v, want %v", local, want)
	}
}

func TestProbePeerChecksTheRelayAndTheToken(t *testing.T) {
	parent := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path != cascade.RelayPath:
			http.NotFound(writer, request)
		case request.Header.Get(cascade.HeaderToken) != "right-token-0123456789":
			writer.Header().Set(cascade.HeaderInstance, "parent-node")
			writer.WriteHeader(http.StatusUnauthorized)
		case request.Header.Get(cascade.HeaderProbe) == "":
			t.Error("probe header missing")
		default:
			writer.Header().Set(cascade.HeaderInstance, "parent-node")
			writer.WriteHeader(http.StatusNoContent)
		}
	}))
	defer parent.Close()
	registry := peerRegistry(t, db.UpstreamRecord{AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1})
	probe := func(rawURL, token string) (string, error) {
		return registry.ProbePeer(context.Background(), PeerEgress{URL: mustURL(t, rawURL), Token: token})
	}
	if id, err := probe(parent.URL, "right-token-0123456789"); err != nil || id != "parent-node" {
		t.Fatalf("probe = %q, %v", id, err)
	}
	if _, err := probe(parent.URL, "wrong-token-0123456789"); !errors.Is(err, ErrPeerUnauthorized) {
		t.Fatalf("wrong token err = %v", err)
	}
	if _, err := probe(parent.URL+"/elsewhere", "right-token-0123456789"); !errors.Is(err, ErrPeerNotRelay) {
		t.Fatalf("not a relay err = %v", err)
	}
	parent.Close()
	if _, err := probe(parent.URL, "right-token-0123456789"); !errors.Is(err, ErrPeerUnreachable) {
		t.Fatalf("down parent err = %v", err)
	}
}
