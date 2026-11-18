package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"depsilo/internal/config"
	"depsilo/internal/egress"
)

// withoutTUN stands in for a network that no TUN of proxy software carries
// fake-IP placeholders on: it refuses every placeholder at once, as a route
// that does not lead anywhere does, and hands every other address to dial.
func withoutTUN(dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if ip, err := netip.ParseAddr(host); err == nil {
			if _, placeholder := egress.FakeIPRange(ip); placeholder {
				return nil, fmt.Errorf("dial tcp %s: connect: no route to host", address)
			}
		}
		return dial(ctx, network, address)
	}
}

// fakeIPRegistryServer serves fakeIPRegistry.
func fakeIPRegistryServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(fakeIPRegistry(t))
	t.Cleanup(server.Close)
	return server
}

// fakeIPRegistry answers as registry.example, its Bearer realm auth.example,
// and the storage host cdn.example that a blob redirect names, telling them
// apart by the Host header, also as a forward proxy. Every answer closes its
// connection, so each request dials again.
func fakeIPRegistry(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Connection", "close")
		switch request.Host + request.URL.Path {
		case "registry.example/v2/":
			w.Header().Set("Www-Authenticate", `Bearer realm="http://auth.example/token",service="registry.example"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "auth.example/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"token":"fake-ip-token","expires_in":300}`)
		case "registry.example/v2/acme/widget/blobs/sha256:fixture":
			if got := request.Header.Get("Authorization"); got != "Bearer fake-ip-token" {
				t.Errorf("blob Authorization = %q, want the realm's token", got)
			}
			w.Header().Set("Location", "http://cdn.example/blob")
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "cdn.example/blob":
			_, _ = io.WriteString(w, "blob")
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}
}

// pullBlob fetches a token for acme/widget from registry and then the blob
// sha256:fixture through registry's client, and returns the blob.
func pullBlob(t *testing.T, registry *Registry) (string, error) {
	t.Helper()
	token, err := NewAuthManager().GetToken(
		context.Background(), registry.Client, registry.URL, registry.Name, "", "", "repository:acme/widget:pull",
	)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequest(http.MethodGet, registry.URL+"/v2/acme/widget/blobs/sha256:fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := registry.Client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return string(body), err
}

// With [egress] allow_fake_ip on, the default, a registry that no proxy
// carries dials a host name answered only with fake-IP placeholders at those
// placeholders, for the TUN of proxy software to carry, as other programs on
// the machine do: the registry, its Bearer realm and a blob redirect alike.
// An answer with placeholders of both families is dialed at its IPv4 one.
func TestDirectRegistryDialsFakeIPAnswersAtTheirPlaceholders(t *testing.T) {
	for _, tt := range []struct {
		name      string
		answers   staticIPResolver
		wantDials []string
	}{
		{
			name: "IPv4 placeholders",
			answers: staticIPResolver{
				"registry.example": dnsAnswer("198.18.0.10"),
				"auth.example":     dnsAnswer("198.18.0.11"),
				"cdn.example":      dnsAnswer("198.18.0.12", "198.19.255.12"),
			},
			wantDials: []string{"198.18.0.10:80", "198.18.0.11:80", "198.18.0.10:80", "198.18.0.12:80"},
		},
		{
			name: "IPv6 placeholders",
			answers: staticIPResolver{
				"registry.example": dnsAnswer("2001:2::a"),
				"auth.example":     dnsAnswer("2001:2::b"),
				"cdn.example":      dnsAnswer("2001:2::c"),
			},
			wantDials: []string{"[2001:2::a]:80", "[2001:2::b]:80", "[2001:2::a]:80", "[2001:2::c]:80"},
		},
		{
			// Go's resolver merges the A and AAAA answers and sorts them by
			// RFC 6724, so either family can come first.
			name: "placeholders of both families in either order",
			answers: staticIPResolver{
				"registry.example": dnsAnswer("198.18.0.10", "2001:2::a"),
				"auth.example":     dnsAnswer("2001:2::b"),
				"cdn.example":      dnsAnswer("2001:2::c", "198.18.0.12"),
			},
			wantDials: []string{"198.18.0.10:80", "[2001:2::b]:80", "198.18.0.10:80", "198.18.0.12:80"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := fakeIPRegistryServer(t)
			dialer := &routedDialer{direct: server.Listener.Addr().String()}
			core, logs := observer.New(zap.InfoLevel)
			t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
			registry := newResolver(config.DockerConfig{Registries: []config.RegistryConfig{
				{Name: "hub", URL: "http://registry.example"},
			}}, egress.Policy{}, tt.answers, dialer.DialContext).Default()

			if blob, err := pullBlob(t, registry); err != nil || blob != "blob" {
				t.Fatalf("blob = %q, %v; want the blob", blob, err)
			}
			if got := dialer.directDials(); !slices.Equal(got, tt.wantDials) {
				t.Fatalf("direct dials = %v, want %v", got, tt.wantDials)
			}
			routes := logs.FilterMessage("docker registry egress").All()
			if len(routes) != 1 || routes[0].ContextMap()["route"] != egressRouteDirect {
				t.Fatalf("egress lines = %v, want the direct route", routes)
			}
		})
	}
}

// A fake-IP answer counts as public, as through a proxy: a registry that such
// an answer pinned cannot reach a Bearer realm that answers privately or with
// loopback, and nothing is sent to the realm.
func TestFakeIPRegistryWithoutProxyStillRefusesPrivateRealm(t *testing.T) {
	for _, registryAnswer := range []string{"198.18.0.10", "2001:2::a"} {
		for _, realmAnswer := range []string{"10.20.30.40", "127.0.0.1"} {
			t.Run(registryAnswer+"/"+realmAnswer, func(t *testing.T) {
				server := fakeIPRegistryServer(t)
				dialer := &routedDialer{direct: server.Listener.Addr().String()}
				client, err := newRegistryClient("http://registry.example", "", staticIPResolver{
					"registry.example": dnsAnswer(registryAnswer),
					"auth.example":     dnsAnswer(realmAnswer),
				}, dialer.DialContext)
				if err != nil {
					t.Fatal(err)
				}

				_, err = NewAuthManager().GetToken(
					context.Background(), client, "http://registry.example", "registry", "operator", "secret",
					"repository:acme/widget:pull",
				)
				if want := "token fetch failed at http://auth.example: Docker dial target crossed network scope"; err == nil || clientReasonOf(err) != want {
					t.Fatalf("GetToken error = %v, want the 502 reason %q", err, want)
				}
				if got, want := dialer.directDials(), []string{net.JoinHostPort(registryAnswer, "80")}; !slices.Equal(got, want) {
					t.Fatalf("direct dials = %v, want only the registry's placeholder %v", got, want)
				}
			})
		}
	}
}

// While the client has not dialed the registry yet, the direct route looks
// the registry's own answer up to check another host. A fake-IP answer counts
// as public there too while allow_fake_ip is on, so a public Bearer realm is
// reached first. With allow_fake_ip off, the registry has no usable scope,
// and the realm is refused before anything is dialed.
func TestDirectRegistryLooksUpItsFakeIPAnswerToCheckAnotherHost(t *testing.T) {
	answers := staticIPResolver{
		"registry.example": dnsAnswer("198.18.0.10"),
		"auth.example":     dnsAnswer("93.184.216.34"),
	}
	server := fakeIPRegistryServer(t)
	for _, tt := range []struct {
		name      string
		policy    egress.Policy
		wantErr   string
		wantDials []string
	}{
		{name: "allow_fake_ip on", wantDials: []string{"93.184.216.34:80"}},
		{name: "allow_fake_ip off", policy: fakeIPOffPolicy(t, ""), wantErr: "Docker dial target crossed network scope"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dialer := &routedDialer{direct: server.Listener.Addr().String()}
			route := registryEgressFor(config.RegistryConfig{URL: "http://registry.example"}, tt.policy)
			client, err := buildRegistryClient("hub", "http://registry.example", route, answers, dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Get("http://auth.example/token")
			if response != nil {
				_ = response.Body.Close()
			}
			switch {
			case tt.wantErr == "" && (err != nil || response.StatusCode != http.StatusOK):
				t.Fatalf("realm request = %v, %v; want the realm's answer", response, err)
			case tt.wantErr != "" && (err == nil || clientReasonOf(err) != tt.wantErr):
				t.Fatalf("realm request error = %v, want the 502 reason %q", err, tt.wantErr)
			}
			if got := dialer.directDials(); !slices.Equal(got, tt.wantDials) {
				t.Fatalf("direct dials = %v, want %v", got, tt.wantDials)
			}
		})
	}
}

// A fake-IP answer for the registry's own host pins it public only once the
// TUN carried a connection to the placeholder, also when another host is
// checked against it first. When nothing carried it, the registry keeps no
// scope, so the remedy of the 502 for a registry that no_proxy keeps direct,
// a real DNS answer such as a private one for a registry on the local
// network, works at the next pull without a restart. After a carried
// connection, a changed answer is refused as for any registry.
func TestRegistryPinsItsFakeIPAnswerOnlyOnceAPlaceholderConnected(t *testing.T) {
	proxy := newRecordingProxy(t, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Connection", "close")
		switch request.Host + request.URL.Path {
		case "mirror.lan/v2/", "auth.example/token":
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	policy, err := egress.NewPolicy(proxy.URL, []string{"mirror.lan"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	notCarried := "auth discovery failed at http://mirror.lan: mirror.lan resolved to 198.18.0.10, " +
		"a fake-IP address from proxy software, but nothing carried the connection to it; " + fakeIPRemedyTUNKeptDirect
	for _, tt := range []struct {
		name string
		// tun is whether a TUN carries the first pull's placeholder.
		tun bool
		// realmFirst requests the public realm host before the first pull,
		// which checks it against the registry's fake-IP answer.
		realmFirst bool
		// firstErr and secondErr are the 502 reasons of the pulls, "" for none.
		firstErr, secondErr string
		// wantDials are the dials that reached the network; withoutTUN
		// refuses a placeholder before.
		wantDials []string
	}{
		{
			name:      "nothing carried, then a private answer",
			firstErr:  notCarried,
			wantDials: []string{"192.168.1.20:80"},
		},
		{
			name:       "another host checked first, nothing carried, then a private answer",
			realmFirst: true,
			firstErr:   notCarried,
			wantDials:  []string{"93.184.216.34:80", "192.168.1.20:80"},
		},
		{
			name:      "carried, then a private answer",
			tun:       true,
			secondErr: "auth discovery failed at http://mirror.lan: Docker registry changed network scope",
			wantDials: []string{"198.18.0.10:80"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			answer := dnsAnswer("198.18.0.10")
			answers := ipResolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
				switch host {
				case "auth.example":
					return dnsAnswer("93.184.216.34"), nil
				case "mirror.lan":
					mu.Lock()
					defer mu.Unlock()
					return answer, nil
				}
				return nil, fmt.Errorf("unexpected DNS lookup for %s", host)
			})
			dialer := &routedDialer{proxies: map[string]bool{proxy.address(): true}, direct: server.Listener.Addr().String()}
			dial := withoutTUN(dialer.DialContext)
			if tt.tun {
				dial = dialer.DialContext
			}
			registry := newResolver(config.DockerConfig{Registries: []config.RegistryConfig{
				{Name: "lan", URL: "http://mirror.lan"},
			}}, policy, answers, dial).Default()
			pull := func() string {
				_, err := NewAuthManager().GetToken(
					context.Background(), registry.Client, registry.URL, registry.Name, "", "", "repository:acme/widget:pull",
				)
				if err == nil {
					return ""
				}
				return clientReasonOf(err)
			}

			if tt.realmFirst {
				response, err := registry.Client.Get("http://auth.example/token")
				if err != nil {
					t.Fatalf("realm request: %v", err)
				}
				_ = response.Body.Close()
			}
			if got := pull(); got != tt.firstErr {
				t.Fatalf("first pull = %q, want %q", got, tt.firstErr)
			}
			// The operator gives the registry its real, private answer.
			mu.Lock()
			answer = dnsAnswer("192.168.1.20")
			mu.Unlock()
			if got := pull(); got != tt.secondErr {
				t.Fatalf("second pull = %q, want %q", got, tt.secondErr)
			}
			if got := dialer.directDials(); !slices.Equal(got, tt.wantDials) {
				t.Fatalf("direct dials = %v, want %v", got, tt.wantDials)
			}
			if got := proxy.requestedHosts(); len(got) != 0 {
				t.Fatalf("proxy carried %v, want nothing for a registry kept direct", got)
			}
		})
	}
}

// A registry that no_proxy or a loopback URL keeps off the [egress] default
// proxy dials fake-IP answers itself too, and the proxy carries nothing.
func TestRegistryKeptDirectDialsFakeIPAnswers(t *testing.T) {
	proxy := newRecordingProxy(t, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.Host + request.URL.Path {
		case "localhost:5000/v2/":
			w.Header().Set("Www-Authenticate", `Bearer realm="http://auth.example/token",service="registry.local"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "auth.example/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"token":"kept-direct-token","expires_in":300}`)
		case "mirror.lan/v2/":
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	policy, err := egress.NewPolicy(proxy.URL, []string{"mirror.lan"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name      string
		url       string
		answers   staticIPResolver
		wantToken string
		wantDials []string
	}{
		{
			name:      "no_proxy registry",
			url:       "http://mirror.lan",
			answers:   staticIPResolver{"mirror.lan": dnsAnswer("2001:2::a", "198.18.0.10")},
			wantDials: []string{"198.18.0.10:80"},
		},
		{
			name: "Bearer realm of a loopback registry",
			url:  "http://localhost:5000",
			answers: staticIPResolver{
				"localhost":    dnsAnswer("127.0.0.1"),
				"auth.example": dnsAnswer("198.18.0.11"),
			},
			wantToken: "kept-direct-token",
			wantDials: []string{"127.0.0.1:5000", "198.18.0.11:80"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dialer := &routedDialer{proxies: map[string]bool{proxy.address(): true}, direct: server.Listener.Addr().String()}
			registry := newResolver(config.DockerConfig{Registries: []config.RegistryConfig{
				{Name: "kept-direct", URL: tt.url},
			}}, policy, tt.answers, dialer.DialContext).Default()

			token, err := NewAuthManager().GetToken(
				context.Background(), registry.Client, registry.URL, registry.Name, "", "", "repository:acme/widget:pull",
			)
			if err != nil || token != tt.wantToken {
				t.Fatalf("GetToken = %q, %v; want %q", token, err, tt.wantToken)
			}
			if got := dialer.directDials(); !slices.Equal(got, tt.wantDials) {
				t.Fatalf("direct dials = %v, want %v", got, tt.wantDials)
			}
			if got := proxy.requestedHosts(); len(got) != 0 {
				t.Fatalf("proxy carried %v, want nothing for a registry kept direct", got)
			}
		})
	}
}

// Only a fake-IP answer for a host name is dialed. A registry written as an
// address from either range, and an answer that mixes placeholders with any
// other address, keep their refusal, and nothing is dialed. (Redirects to
// such targets: TestFakeIPMixedAndLiteralTargetsAreRefusedWithAndWithoutProxy.)
func TestDirectRegistryRefusesFakeIPLiteralsAndMixedAnswers(t *testing.T) {
	for _, tt := range []struct {
		name, origin string
		answers      staticIPResolver
	}{
		{name: "IPv4 literal", origin: "http://198.18.0.5", answers: staticIPResolver{"198.18.0.5": dnsAnswer("198.18.0.5")}},
		{name: "IPv6 literal", origin: "http://[2001:2::5]", answers: staticIPResolver{"2001:2::5": dnsAnswer("2001:2::5")}},
		{name: "decimal literal", origin: "http://3323068421", answers: staticIPResolver{"3323068421": dnsAnswer("198.18.0.5")}},
		{name: "placeholder and public address", origin: "http://registry.example",
			answers: staticIPResolver{"registry.example": dnsAnswer("198.18.0.10", "93.184.216.34")}},
		{name: "private address and placeholder", origin: "http://registry.example",
			answers: staticIPResolver{"registry.example": dnsAnswer("10.0.0.5", "2001:2::a")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dialer := &routedDialer{}
			client, err := newRegistryClient(tt.origin, "", tt.answers, dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}
			_, err = NewAuthManager().GetToken(context.Background(), client, tt.origin, "registry", "", "", "repository:acme/widget:pull")
			if err == nil || !strings.HasSuffix(clientReasonOf(err), ": unsafe Docker dial target") {
				t.Fatalf("GetToken error = %v, want the refusal of an unsafe dial target", err)
			}
			if got := dialer.directDials(); len(got) != 0 {
				t.Fatalf("direct dials = %v, want none", got)
			}
		})
	}
}

// A fake-IP answer that holds an address of this machine's network
// interfaces, such as the TUN address of the proxy software, names this
// machine and is no placeholder answer: it is refused as unsafe on the direct
// route and before a proxy carries it, and so is an answer that Depsilo
// cannot check because the interfaces cannot be listed. Nothing is dialed or
// sent to the proxy, and the server log says why.
func TestFakeIPAnswerHoldingAnAddressOfThisMachineIsRefused(t *testing.T) {
	for _, tt := range []struct {
		name string
		// realm is set when the realm, rather than the registry, has the
		// fake-IP answer.
		realm  bool
		answer []net.IPAddr
		// held are the addresses of this machine's network interfaces, which
		// cannot be listed when listErr is set.
		held    []string
		listErr error
		logged  string
	}{
		{
			name:   "TUN address of the proxy software",
			answer: dnsAnswer("198.18.0.1"),
			held:   []string{"127.0.0.1", "198.18.0.1"},
			logged: "registry.example resolved to 198.18.0.1, an address of this machine's network interfaces",
		},
		{
			name:   "IPv6 TUN address next to a placeholder, for the realm",
			realm:  true,
			answer: dnsAnswer("198.18.0.20", "2001:2::1"),
			held:   []string{"2001:2::1"},
			logged: "auth.example resolved to 2001:2::1, an address of this machine's network interfaces",
		},
		{
			name:    "interfaces that cannot be listed",
			answer:  dnsAnswer("198.18.0.10"),
			listErr: errors.New("netlink refused"),
			logged:  "registry.example resolved to fake-IP addresses that Depsilo could not check",
		},
	} {
		for _, route := range []string{egressRouteDirect, egressRouteOwnProxy, egressRouteDefaultProxy} {
			t.Run(route+"/"+tt.name, func(t *testing.T) {
				answers := staticIPResolver{"registry.example": tt.answer}
				stage, host := "auth discovery", "registry.example"
				if tt.realm {
					answers = staticIPResolver{"registry.example": dnsAnswer("93.184.216.34"), "auth.example": tt.answer}
					stage, host = "token fetch", "auth.example"
				}
				server := fakeIPRegistryServer(t)
				proxy := newRecordingProxy(t, fakeIPRegistry(t))
				dialer := &routedDialer{proxies: map[string]bool{proxy.address(): true}, direct: server.Listener.Addr().String()}
				machine := interfacesHolding{hostNetwork: answers, addresses: tt.held, listErr: tt.listErr}
				registry := registryOnRoute(t, route, "http://registry.example", proxy, machine, dialer.DialContext)

				_, err := NewAuthManager().GetToken(
					context.Background(), registry.Client, registry.URL, registry.Name, "", "", "repository:acme/widget:pull",
				)
				refusal := "unsafe Docker dial target"
				if route != egressRouteDirect {
					refusal = "unsafe Docker proxy target"
				}
				if want := stage + " failed at http://" + host + ": " + refusal; err == nil || clientReasonOf(err) != want {
					t.Fatalf("GetToken error = %v, want the 502 reason %q", err, want)
				}
				if !strings.Contains(err.Error(), tt.logged) {
					t.Fatalf("logged error %q lacks %q", err.Error(), tt.logged)
				}
				// Only the registry's own public answer is carried.
				wantProxied, wantDials := []string(nil), []string(nil)
				if tt.realm {
					if route == egressRouteDirect {
						wantDials = []string{"93.184.216.34:80"}
					} else {
						wantProxied = []string{"registry.example"}
					}
				}
				if got := proxy.requestedHosts(); !slices.Equal(got, wantProxied) {
					t.Fatalf("proxy carried %v, want %v", got, wantProxied)
				}
				if got := dialer.directDials(); !slices.Equal(got, wantDials) {
					t.Fatalf("direct dials = %v, want %v", got, wantDials)
				}
			})
		}
	}
}

// When no placeholder connects, nothing carries them, and the 502 says so: it
// names the host, the IPv4 placeholder and the remedy, the TUN mode first,
// and for a registry that no_proxy keeps off the default proxy, real DNS
// answers instead of the proxy. The dial errors reach the server log only.
func TestBadGatewayExplainsFakeIPPlaceholdersThatNothingCarried(t *testing.T) {
	proxy := newRecordingProxy(t, nil)
	keptDirect, err := egress.NewPolicy(proxy.URL, []string{"mirror.lan"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		url     string
		policy  egress.Policy
		answers staticIPResolver
		want    string
		// hidden stays out of the 502 message, and logged reaches the log.
		hidden []string
		logged string
	}{
		{
			name:    "registry without a proxy",
			url:     "http://registry.example",
			answers: staticIPResolver{"registry.example": dnsAnswer("2001:2::a", "198.18.0.10")},
			want:    "auth discovery failed at http://registry.example: registry.example resolved to 198.18.0.10" + fakeIPNotCarriedReason,
			hidden:  []string{"no route", "dial tcp", "2001:2::a"},
			logged: "(dial error: dial tcp 198.18.0.10:80: connect: no route to host; " +
				"dial tcp [2001:2::a]:80: connect: no route to host)",
		},
		{
			// The default proxy exists but may not carry this registry.
			name:    "registry that no_proxy keeps off the default proxy",
			url:     "http://mirror.lan",
			policy:  keptDirect,
			answers: staticIPResolver{"mirror.lan": dnsAnswer("198.18.0.10")},
			want: "auth discovery failed at http://mirror.lan: mirror.lan resolved to 198.18.0.10, a fake-IP address from proxy software, " +
				"but nothing carried the connection to it; turn on the proxy software's TUN mode, " +
				"or, as [egress] no_proxy or a loopback URL keeps this registry off the proxy, give its host names real DNS answers, " +
				"for example with fake-ip-filter, or remove it from no_proxy, see docs/deployment.md#upstream-network-egress",
			hidden: []string{"no route", "dial tcp", "set [egress] upstream_proxy"},
			logged: "(dial error: dial tcp 198.18.0.10:80: connect: no route to host)",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zap.ErrorLevel)
			t.Cleanup(zap.ReplaceGlobals(zap.New(core)))
			dialer := &routedDialer{proxies: map[string]bool{proxy.address(): true}}
			router, _ := newRoutedDockerRouter(t, config.RegistryConfig{Name: "hub", URL: tt.url}, tt.policy, tt.answers,
				withoutTUN(dialer.DialContext), false)

			response := serveWithRequestID(router, http.MethodGet, "/v2/library/widget/manifests/latest", testRequestID)
			code, message := badGatewayBody(t, response)
			if want := tt.want + testRequestSuffix; code != "UPSTREAM_UNAVAILABLE" || message != want {
				t.Fatalf("answer = %s %q, want UPSTREAM_UNAVAILABLE %q", code, message, want)
			}
			for _, hidden := range tt.hidden {
				if strings.Contains(message, hidden) {
					t.Fatalf("502 message %q contains %q", message, hidden)
				}
			}
			if len(strings.TrimSuffix(message, testRequestSuffix)) > maxClientMessageBytes {
				t.Fatalf("502 message of %d bytes exceeds the cap", len(message))
			}
			logged := loggedErrorsOf(logs, testRequestID)
			if len(logged) != 1 || !strings.Contains(logged[0], tt.logged) {
				t.Fatalf("errors logged under request %s = %q, want one containing %q", testRequestID, logged, tt.logged)
			}
			if got := proxy.requestedHosts(); len(got) != 0 {
				t.Fatalf("proxy carried %v, want nothing", got)
			}
		})
	}
}

// A TUN completes a connection to a placeholder at once, so a placeholder
// that hangs is cut after a short timeout, egress.FakeIPConnectTimeout, and
// every placeholder of the answer is tried, IPv4 first, before the refusal.
// net/http dials apart from the request, whose own deadline does not reach
// the dial; a request whose deadline ends first keeps that deadline's error.
func TestFakeIPPlaceholderDialsAreCutByTheShortTimeout(t *testing.T) {
	answers := staticIPResolver{"registry.example": dnsAnswer("2001:2::a", "198.18.0.10")}
	var mu sync.Mutex
	var attempts []string
	hanging := func(ctx context.Context, _, address string) (net.Conn, error) {
		mu.Lock()
		attempts = append(attempts, address)
		mu.Unlock()
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 50*time.Millisecond {
			t.Errorf("dial of %s has deadline %v (set %v), want one within the short timeout", address, deadline, ok)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	// The route leaves the timeout to upstream.DialFakeIP's default, which
	// the second part of this test checks.
	route := registryEgressFor(config.RegistryConfig{URL: "http://registry.example"}, egress.Policy{})
	if !route.fakeIP.dial || route.fakeIP.timeout != 0 {
		t.Fatalf("direct route dials fake-IP %v with timeout %v, want the default timeout", route.fakeIP.dial, route.fakeIP.timeout)
	}
	route.fakeIP.timeout = 50 * time.Millisecond
	client, err := buildRegistryClient("hub", "http://registry.example", route, answers, hanging)
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	_, err = NewAuthManager().GetToken(context.Background(), client, "http://registry.example", "hub", "", "", "repository:acme/widget:pull")
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the short timeout cut the dials after %v", elapsed)
	}
	if want := "auth discovery failed at http://registry.example: registry.example resolved to 198.18.0.10" + fakeIPNotCarriedReason; err == nil ||
		clientReasonOf(err) != want {
		t.Fatalf("GetToken error = %v, want the 502 reason %q", err, want)
	}
	if !strings.Contains(err.Error(), "(dial error: context deadline exceeded; context deadline exceeded)") {
		t.Fatalf("logged error %q lacks the dial errors", err.Error())
	}
	mu.Lock()
	got := slices.Clone(attempts)
	mu.Unlock()
	if !slices.Equal(got, []string{"198.18.0.10:80", "[2001:2::a]:80"}) {
		t.Fatalf("dial attempts = %v, want both placeholders, IPv4 first", got)
	}

	// Without a shorter timeout, each attempt gets FakeIPConnectTimeout.
	var deadline time.Time
	var bounded bool
	connecting := func(ctx context.Context, _, _ string) (net.Conn, error) {
		deadline, bounded = ctx.Deadline()
		return nil, errors.New("refused")
	}
	client, err = newRegistryClient("http://registry.example", "", answers, connecting)
	if err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	response, err := client.Get("http://registry.example/v2/")
	finished := time.Now()
	if response != nil {
		_ = response.Body.Close()
	}
	var notCarried *fakeIPNotCarried
	if !errors.As(err, &notCarried) || !bounded ||
		deadline.Before(started.Add(egress.FakeIPConnectTimeout)) || deadline.After(finished.Add(egress.FakeIPConnectTimeout)) {
		t.Fatalf("GET = %v; placeholder dial deadline %v after the start (set %v), want %v", err, deadline.Sub(started), bounded, egress.FakeIPConnectTimeout)
	}

	// A request whose own deadline is shorter keeps that deadline's error.
	// Its dial goes on without that deadline, until the test ends.
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	client, err = newRegistryClient("http://registry.example", "", answers, func(ctx context.Context, _, _ string) (net.Conn, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-stop:
			return nil, errors.New("test ended")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://registry.example/v2/", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = client.Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, context.DeadlineExceeded) || errors.As(err, &notCarried) {
		t.Fatalf("GET with a short request deadline = %v, want the deadline's error", err)
	}
}
