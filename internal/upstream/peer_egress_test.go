package upstream

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"depsilo/internal/cascade"
	"depsilo/internal/db"
)

func newPeerForServer(t *testing.T, server *httptest.Server, mutate func(*PeerEgress)) (*http.Client, PeerEgress) {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	peer := PeerEgress{
		Name:            "home",
		URL:             parsed,
		Token:           "shared-token",
		LocalInstanceID: "child-node",
		MaxHops:         4,
	}
	if mutate != nil {
		mutate(&peer)
	}
	client, err := buildPeerClient("", peer, "https://registry.npmjs.org")
	if err != nil {
		t.Fatalf("buildPeerClient: %v", err)
	}
	return client, peer
}

func TestPeerTransportRewritesExchangeAndStripsCredentials(t *testing.T) {
	type capture struct {
		path    string
		headers http.Header
	}
	requests := make(chan capture, 4)
	peerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- capture{path: request.URL.Path, headers: request.Header.Clone()}
		writer.Header().Set("ETag", `"peer-etag"`)
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "payload")
	}))
	defer peerServer.Close()

	client, _ := newPeerForServer(t, peerServer, nil)
	request, err := http.NewRequest(http.MethodGet, "https://registry.npmjs.org/left-pad", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "application/vnd.npm.install-v1+json")
	request.Header.Set("Authorization", "Bearer upstream-credential")
	request.Header.Set("Cookie", "session=secret")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer response.Body.Close()
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatal(err)
	}
	if response.Request == nil || response.Request.URL.String() != "https://registry.npmjs.org/left-pad" {
		t.Fatalf("response.Request URL = %v, want the origin URL", response.Request)
	}

	got := <-requests
	if got.path != cascade.RelayPath {
		t.Fatalf("relay path = %q", got.path)
	}
	if got.headers.Get(cascade.HeaderTarget) != "https://registry.npmjs.org/left-pad" {
		t.Fatalf("target header = %q", got.headers.Get(cascade.HeaderTarget))
	}
	if got.headers.Get(cascade.HeaderSource) != "https://registry.npmjs.org" {
		t.Fatalf("source header = %q", got.headers.Get(cascade.HeaderSource))
	}
	if got.headers.Get(cascade.HeaderToken) != "shared-token" {
		t.Fatalf("token header = %q", got.headers.Get(cascade.HeaderToken))
	}
	if got.headers.Get(cascade.HeaderChain) != "child-node" {
		t.Fatalf("chain header = %q", got.headers.Get(cascade.HeaderChain))
	}
	if got.headers.Get("Accept") != "application/vnd.npm.install-v1+json" {
		t.Fatalf("Accept was not preserved: %q", got.headers.Get("Accept"))
	}
	if got.headers.Get("Authorization") != "" || got.headers.Get("Cookie") != "" {
		t.Fatal("credentials must not cross the peer by default")
	}
}

func TestPeerTransportPropagatesChainTTLAndKind(t *testing.T) {
	requests := make(chan http.Header, 4)
	peerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- request.Header.Clone()
		writer.WriteHeader(http.StatusOK)
	}))
	defer peerServer.Close()

	client, _ := newPeerForServer(t, peerServer, func(peer *PeerEgress) {
		peer.ForwardCredentials = true
	})
	ctx := cascade.WithChain(context.Background(), cascade.Chain{"grandparent", "parent"})
	ctx = cascade.WithFetchTTL(ctx, 2*time.Hour)
	ctx = cascade.WithFetchKind(ctx, cascade.KindIndex)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://registry.npmjs.org/pkg", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer upstream-credential")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	response.Body.Close()

	headers := <-requests
	if headers.Get(cascade.HeaderChain) != "grandparent,parent" {
		t.Fatalf("chain = %q", headers.Get(cascade.HeaderChain))
	}
	if headers.Get(cascade.HeaderTTL) != "7200" {
		t.Fatalf("ttl = %q", headers.Get(cascade.HeaderTTL))
	}
	if headers.Get(cascade.HeaderKind) != cascade.KindIndex {
		t.Fatalf("kind = %q", headers.Get(cascade.HeaderKind))
	}
	if headers.Get("Authorization") != "Bearer upstream-credential" {
		t.Fatal("forward_credentials peer must keep Authorization")
	}
}

func TestPeerTransportRejectsUnsupportedRequests(t *testing.T) {
	peerServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer peerServer.Close()

	client, _ := newPeerForServer(t, peerServer, nil)
	post, err := http.NewRequest(http.MethodPost, "https://registry.npmjs.org/pkg", strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(post); err == nil {
		t.Fatal("POST through a peer must fail")
	}

	credentialed, err := http.NewRequest(http.MethodGet, "https://user:pass@registry.npmjs.org/pkg", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(credentialed); err == nil {
		t.Fatal("credentialed upstream URLs must be rejected")
	}
}

func TestPeerTransportResolvesRedirectsAgainstOrigin(t *testing.T) {
	var (
		mu      sync.Mutex
		targets []string
	)
	peerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		target := request.Header.Get(cascade.HeaderTarget)
		mu.Lock()
		targets = append(targets, target)
		mu.Unlock()
		if target == "https://registry.npmjs.org/start" {
			writer.Header().Set("Location", "/final")
			writer.WriteHeader(http.StatusFound)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "final")
	}))
	defer peerServer.Close()

	client, _ := newPeerForServer(t, peerServer, nil)
	request, err := http.NewRequest(http.MethodGet, "https://registry.npmjs.org/start", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(targets) != 2 || targets[0] != "https://registry.npmjs.org/start" || targets[1] != "https://registry.npmjs.org/final" {
		t.Fatalf("redirect targets = %v", targets)
	}
}

func TestRelayEgressMatchesConfiguredSourceAndOrigin(t *testing.T) {
	database := bootstrapDB(t)
	record := db.UpstreamRecord{
		AdapterType: "pypi", Name: "mirror", URL: "https://mirror.example/simple",
		Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true,
	}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.RelayEgress("https://mirror.example/simple", "https://mirror.example/simple/requests/"); !ok {
		t.Fatal("matching source and origin should resolve an egress client")
	}
	if _, ok := registry.RelayEgress("https://mirror.example/simple", "https://cdn.example/requests.whl"); ok {
		t.Fatal("a different target origin must not borrow the configured upstream policy")
	}
	if _, ok := registry.RelayEgress("https://unconfigured.example", "https://unconfigured.example/pkg"); ok {
		t.Fatal("an unconfigured source must not resolve")
	}
}

func TestNewRelayClientRejectsPrivateTargets(t *testing.T) {
	private := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer private.Close()

	response, err := NewRelayClient().Get(private.URL)
	if err == nil {
		response.Body.Close()
		t.Fatal("relay client dialed a private address")
	}
	if !strings.Contains(err.Error(), "not a public address") {
		t.Fatalf("error = %v", err)
	}
}

func TestRegistryMutationValidatesAndPersistsVia(t *testing.T) {
	database := bootstrapDB(t)
	seed := db.UpstreamRecord{
		AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org",
		Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true,
	}
	if err := database.Create(&seed).Error; err != nil {
		t.Fatal(err)
	}
	peerURL, err := url.Parse("https://cache.example")
	if err != nil {
		t.Fatal(err)
	}
	peers := NewPeerSet([]PeerEgress{{
		Name: "home", URL: peerURL, Token: "shared-token", LocalInstanceID: "child-node", MaxHops: 3,
	}})
	registry, err := NewRegistryWithOptions(database, []string{"npm"}, PoolOptions{Peers: peers})
	if err != nil {
		t.Fatal(err)
	}

	created, err := registry.Create(context.Background(), MutationInput{
		AdapterType: "npm", Name: "peer", URL: "https://registry.npmjs.org",
		Priority: 2, ProbeMode: "passive", ProbeInterval: "30m", Via: "home",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Via != "home" {
		t.Fatalf("created via = %q", created.Via)
	}
	var peerUpstream *Upstream
	for _, candidate := range registry.Pools()["npm"].Snapshot() {
		if candidate.ID == created.ID {
			peerUpstream = candidate
		}
	}
	if peerUpstream == nil {
		t.Fatal("created upstream missing from the pool")
	}
	if _, ok := peerUpstream.client.Transport.(*peerTransport); !ok {
		t.Fatalf("transport = %T, want *peerTransport", peerUpstream.client.Transport)
	}

	if _, err := registry.Create(context.Background(), MutationInput{
		AdapterType: "npm", Name: "unknown-peer", URL: "https://registry.npmjs.org",
		Priority: 3, ProbeMode: "passive", ProbeInterval: "30m", Via: "missing",
	}); err == nil {
		t.Fatal("creating an upstream with an unknown peer must fail")
	}
	if _, err := registry.Create(context.Background(), MutationInput{
		AdapterType: "npm", Name: "bad-name", URL: "https://registry.npmjs.org",
		Priority: 3, ProbeMode: "passive", ProbeInterval: "30m", Via: "Not A Peer",
	}); err == nil {
		t.Fatal("creating an upstream with an invalid peer name must fail")
	}
}

func TestRegistryIgnoresViaWhenCascadeIsDisabled(t *testing.T) {
	database := bootstrapDB(t)
	record := db.UpstreamRecord{
		AdapterType: "npm", Name: "peer", URL: "https://registry.npmjs.org",
		Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true, Via: "home",
	}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(database, []string{"npm"})
	if err != nil {
		t.Fatalf("NewRegistry with a dormant via: %v", err)
	}
	upstreams := registry.Pools()["npm"].Snapshot()
	if len(upstreams) != 1 {
		t.Fatalf("upstreams = %d", len(upstreams))
	}
	if _, ok := upstreams[0].client.Transport.(*peerTransport); ok {
		t.Fatal("disabled cascade must keep direct egress")
	}
	if upstreams[0].Via != "home" {
		t.Fatalf("dormant via should still be reported: %q", upstreams[0].Via)
	}
}
