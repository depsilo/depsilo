package docker

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"depsilo/internal/config"
	"depsilo/internal/egress"
)

// recordingProxy is a forward-proxy fixture. It answers the absolute-form
// requests a proxied client sends and records the host of each.
type recordingProxy struct {
	*httptest.Server
	mu    sync.Mutex
	hosts []string
}

func newRecordingProxy(t *testing.T, handle http.HandlerFunc) *recordingProxy {
	t.Helper()
	proxy := &recordingProxy{}
	proxy.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		proxy.mu.Lock()
		proxy.hosts = append(proxy.hosts, request.Host)
		proxy.mu.Unlock()
		if handle == nil {
			response.WriteHeader(http.StatusOK)
			return
		}
		handle(response, request)
	}))
	t.Cleanup(proxy.Close)
	return proxy
}

func (proxy *recordingProxy) address() string { return proxy.Listener.Addr().String() }

func (proxy *recordingProxy) requestedHosts() []string {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	return append([]string(nil), proxy.hosts...)
}

// routedDialer connects a proxy address to that proxy. Any other address can
// only come from a direct client: it is recorded and connected to direct, or
// refused when the test expects no direct dial.
type routedDialer struct {
	proxies map[string]bool
	direct  string

	mu    sync.Mutex
	dials []string
}

func (dialer *routedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var base net.Dialer
	if dialer.proxies[address] {
		return base.DialContext(ctx, network, address)
	}
	dialer.mu.Lock()
	dialer.dials = append(dialer.dials, address)
	dialer.mu.Unlock()
	if dialer.direct == "" {
		return nil, errors.New("unexpected direct dial")
	}
	return base.DialContext(ctx, network, dialer.direct)
}

func (dialer *routedDialer) directDials() []string {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	return append([]string(nil), dialer.dials...)
}

func TestNewResolverChoosesOneEgressRoutePerRegistry(t *testing.T) {
	ownProxy := newRecordingProxy(t, nil)
	wantAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte("operator:proxy-secret"))
	defaultProxy := newRecordingProxy(t, func(response http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Proxy-Authorization"); got != wantAuthorization {
			t.Errorf("default proxy authorization = %q, want the configured proxy credentials", got)
		}
		response.WriteHeader(http.StatusOK)
	})
	direct := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(direct.Close)

	policy, err := egress.NewPolicy("http://operator:proxy-secret@"+defaultProxy.address(), []string{"mirror.lan"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dialer := &routedDialer{
		proxies: map[string]bool{ownProxy.address(): true, defaultProxy.address(): true},
		direct:  direct.Listener.Addr().String(),
	}
	core, logs := observer.New(zap.InfoLevel)
	t.Cleanup(zap.ReplaceGlobals(zap.New(core)))

	resolver := newResolver(config.DockerConfig{Registries: []config.RegistryConfig{
		{Name: "own", URL: "http://own.example", Proxy: ownProxy.URL},
		{Name: "hub", URL: "http://hub.example"},
		{Name: "mirror", URL: "http://mirror.lan"},
		{Name: "local", URL: "http://localhost:5000"},
	}}, policy, staticIPResolver{
		"own.example": {{IP: net.ParseIP("93.184.216.34")}},
		"hub.example": {{IP: net.ParseIP("93.184.216.35")}},
		"mirror.lan":  {{IP: net.ParseIP("192.168.1.20")}},
		"localhost":   {{IP: net.ParseIP("127.0.0.1")}},
	}, dialer.DialContext)

	for _, domain := range []string{"own.example", "hub.example", "mirror.lan", "localhost:5000"} {
		registry, _, _ := resolver.Resolve(domain + "/acme/widget/manifests/latest")
		if registry == nil {
			t.Fatalf("no registry for %s", domain)
		}
		response, err := registry.Client.Get(registry.URL + "/v2/")
		if err != nil {
			t.Fatalf("registry %s: %v", registry.Name, err)
		}
		_ = response.Body.Close()
	}

	if got := ownProxy.requestedHosts(); !slices.Equal(got, []string{"own.example"}) {
		t.Errorf("own proxy carried %v, want only the registry that names it", got)
	}
	if got := defaultProxy.requestedHosts(); !slices.Equal(got, []string{"hub.example"}) {
		t.Errorf("default proxy carried %v, want only the registry without a proxy", got)
	}
	if got := dialer.directDials(); !slices.Equal(got, []string{"192.168.1.20:80", "127.0.0.1:5000"}) {
		t.Errorf("direct dials = %v, want the no_proxy and loopback registries", got)
	}

	wantRoutes := map[string]string{
		"own":    "own proxy " + ownProxy.URL,
		"hub":    "default proxy http://" + defaultProxy.address(),
		"mirror": "direct",
		"local":  "direct",
	}
	routes := map[string]string{}
	for _, entry := range logs.FilterMessage("docker registry egress").All() {
		fields := entry.ContextMap()
		if strings.Contains(fmt.Sprint(fields), "proxy-secret") || strings.Contains(fmt.Sprint(fields), "operator") {
			t.Errorf("egress log line carries proxy userinfo: %v", fields)
		}
		route := fmt.Sprint(fields["route"])
		if proxy, ok := fields["proxy"]; ok {
			route += " " + fmt.Sprint(proxy)
		}
		routes[fmt.Sprint(fields["registry"])] = route
	}
	if !maps.Equal(routes, wantRoutes) {
		t.Errorf("logged egress routes = %v, want %v", routes, wantRoutes)
	}
}

// dnsAnswer is a DNS answer of the given addresses, in their order.
func dnsAnswer(addresses ...string) []net.IPAddr {
	answer := make([]net.IPAddr, 0, len(addresses))
	for _, address := range addresses {
		answer = append(answer, net.IPAddr{IP: net.ParseIP(address)})
	}
	return answer
}

func TestRegistryBehindFakeIPDNSUsesTheDefaultProxyForEveryHost(t *testing.T) {
	for _, tt := range []struct {
		name    string
		answers staticIPResolver
	}{
		{
			name: "IPv4 placeholders",
			answers: staticIPResolver{
				"registry.example": dnsAnswer("198.18.0.10"),
				"auth.example":     dnsAnswer("198.18.0.11"),
				"cdn.example":      dnsAnswer("198.18.0.12", "198.19.255.12"),
			},
		},
		{
			// Go's resolver merges the A and AAAA answers and sorts them by
			// RFC 6724, so either family can come first.
			name: "placeholders of both families in either order, and a realm with IPv6 placeholders only",
			answers: staticIPResolver{
				"registry.example": dnsAnswer("198.18.0.10", "2001:2::a"),
				"auth.example":     dnsAnswer("2001:2::b"),
				"cdn.example":      dnsAnswer("2001:2::c", "198.18.0.12"),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			proxy := newRecordingProxy(t, func(response http.ResponseWriter, request *http.Request) {
				switch request.URL.Hostname() + request.URL.Path {
				case "registry.example/v2/":
					response.Header().Set("Www-Authenticate", `Bearer realm="http://auth.example/token",service="registry.example"`)
					response.WriteHeader(http.StatusUnauthorized)
				case "auth.example/token":
					response.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(response, `{"token":"fake-ip-token","expires_in":300}`)
				case "registry.example/v2/acme/widget/blobs/sha256:fixture":
					if got := request.Header.Get("Authorization"); got != "Bearer fake-ip-token" {
						t.Errorf("blob Authorization = %q, want the realm's token", got)
					}
					response.Header().Set("Location", "http://cdn.example/blob")
					response.WriteHeader(http.StatusTemporaryRedirect)
				case "cdn.example/blob":
					_, _ = io.WriteString(response, "blob")
				default:
					http.Error(response, "unexpected proxy target", http.StatusBadGateway)
				}
			})
			policy, err := egress.NewPolicy(proxy.URL, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			dialer := &routedDialer{proxies: map[string]bool{proxy.address(): true}}
			resolver := newResolver(config.DockerConfig{Registries: []config.RegistryConfig{
				{Name: "hub", URL: "http://registry.example"},
			}}, policy, tt.answers, dialer.DialContext)
			registry := resolver.Default()

			token, err := NewAuthManager().GetToken(
				context.Background(), registry.Client, registry.URL, registry.Name, "", "",
				"repository:acme/widget:pull",
			)
			if err != nil {
				t.Fatalf("GetToken: %v", err)
			}
			request, err := http.NewRequest(http.MethodGet, registry.URL+"/v2/acme/widget/blobs/sha256:fixture", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+token)
			response, err := registry.Client.Do(request)
			if err != nil {
				t.Fatalf("blob request: %v", err)
			}
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil || string(body) != "blob" {
				t.Fatalf("blob body = %q, %v", body, err)
			}

			want := []string{"registry.example", "auth.example", "registry.example", "cdn.example"}
			if got := proxy.requestedHosts(); !slices.Equal(got, want) {
				t.Fatalf("proxy carried %v, want %v", got, want)
			}
			if got := dialer.directDials(); len(got) != 0 {
				t.Fatalf("direct dials = %v, want none", got)
			}
		})
	}
}

// A request to another host can come before any request to the registry; the
// registry's own fake-IP answer then sets its network scope, by the same rule.
func TestFakeIPRegistryWithProxyClassifiesItsOriginByTheSameRule(t *testing.T) {
	proxy := newRecordingProxy(t, nil)
	var dialer net.Dialer
	client, err := newRegistryClient("http://registry.example", proxy.URL, staticIPResolver{
		"registry.example": {{IP: net.ParseIP("198.18.0.10")}},
		"cdn.example":      {{IP: net.ParseIP("93.184.216.35")}},
	}, dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get("http://cdn.example/blob")
	if err != nil {
		t.Fatalf("cross-origin request before the first registry request: %v", err)
	}
	_ = response.Body.Close()
	if got := proxy.requestedHosts(); !slices.Equal(got, []string{"cdn.example"}) {
		t.Fatalf("proxy carried %v, want cdn.example", got)
	}
}

func TestFakeIPRegistryWithProxyStillRefusesPrivateRealm(t *testing.T) {
	for _, registryAnswer := range []string{"198.18.0.10", "2001:2::a"} {
		for _, tt := range []struct{ name, address string }{
			{name: "private", address: "10.20.30.40"},
			{name: "loopback", address: "127.0.0.1"},
		} {
			t.Run(registryAnswer+"/"+tt.name, func(t *testing.T) {
				proxy := newRecordingProxy(t, func(response http.ResponseWriter, request *http.Request) {
					switch request.URL.Hostname() {
					case "registry.example":
						response.Header().Set("Www-Authenticate", `Bearer realm="http://auth.example/token",service="registry.example"`)
						response.WriteHeader(http.StatusUnauthorized)
					default:
						response.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(response, `{"token":"must-not-be-returned","expires_in":300}`)
					}
				})
				var dialer net.Dialer
				client, err := newRegistryClient("http://registry.example", proxy.URL, staticIPResolver{
					"registry.example": dnsAnswer(registryAnswer),
					"auth.example":     dnsAnswer(tt.address),
				}, dialer.DialContext)
				if err != nil {
					t.Fatal(err)
				}

				_, err = NewAuthManager().GetToken(
					context.Background(), client, "http://registry.example", "registry", "operator", "secret",
					"repository:acme/widget:pull",
				)
				if err == nil {
					t.Fatalf("GetToken accepted a realm that resolved to %s", tt.address)
				}
				if got := proxy.requestedHosts(); !slices.Equal(got, []string{"registry.example"}) {
					t.Fatalf("proxy carried %v, want the fake-IP registry and not the %s realm", got, tt.name)
				}
			})
		}
	}
}

// clientReasonOf is the text that a 502 body gives for err, without its
// request suffix.
func clientReasonOf(err error) string {
	return strings.TrimSuffix(clientErrorMessage(err, ""), " (details in the Depsilo server log)")
}

// A placeholder mixed with any other address stays unsafe, and an address
// literal is no DNS answer, so a redirect to such a target is refused through
// a proxy and without one, and nothing reaches it.
func TestFakeIPMixedAndLiteralTargetsAreRefusedWithAndWithoutProxy(t *testing.T) {
	const ipv6Placeholder = "2001:2::c"
	unsafeTarget := map[string]string{"proxied": "unsafe Docker proxy target", "direct": "unsafe Docker dial target"}
	for _, tt := range []struct {
		name     string
		location string
		host     string
		answer   []net.IPAddr
		// want is the refusal; empty means the route's refusal of an unsafe
		// target.
		want string
	}{
		{name: "fake-IP and public answers", location: "http://cdn.example/blob", host: "cdn.example", answer: dnsAnswer("198.18.0.12", "93.184.216.35")},
		{name: "fake-IP and private answers", location: "http://cdn.example/blob", host: "cdn.example", answer: dnsAnswer("198.18.0.12", "10.0.0.5")},
		{name: "IPv6 placeholder and public answers", location: "http://cdn.example/blob", host: "cdn.example", answer: dnsAnswer(ipv6Placeholder, "93.184.216.35")},
		{name: "IPv6 placeholder and private answers", location: "http://cdn.example/blob", host: "cdn.example", answer: dnsAnswer(ipv6Placeholder, "10.0.0.5")},
		{name: "IPv6 placeholder and loopback answers", location: "http://cdn.example/blob", host: "cdn.example", answer: dnsAnswer(ipv6Placeholder, "::1")},
		{name: "IPv6 placeholder and link-local answers", location: "http://cdn.example/blob", host: "cdn.example", answer: dnsAnswer(ipv6Placeholder, "fe80::1")},
		{
			// 64:ff9b::a9fe:a9fe embeds 169.254.169.254.
			name: "IPv6 placeholder and NAT64 answers", location: "http://cdn.example/blob", host: "cdn.example",
			answer: dnsAnswer(ipv6Placeholder, "64:ff9b::a9fe:a9fe"),
		},
		{
			name: "fake-IP and documented unique-local answers", location: "http://cdn.example/blob", host: "cdn.example",
			answer: dnsAnswer("198.18.0.12", "fdfe:dcba:9876::c"),
			want: "cdn.example resolved into fdfe:dcba:9876::/48, an IPv6 fake-IP range that Depsilo cannot tell from a private network; " +
				uniqueLocalFakeIPRemedy,
		},
		{
			name: "literal fake-IP address", location: "http://198.18.0.12/blob", host: "198.18.0.12", answer: dnsAnswer("198.18.0.12"),
			want: "unsafe Docker redirect target",
		},
		{
			name: "literal IPv6 placeholder", location: "http://[2001:2::c]/blob", host: ipv6Placeholder, answer: dnsAnswer(ipv6Placeholder),
			want: "unsafe Docker redirect target",
		},
		{name: "fake-IP address as a decimal number", location: "http://3323068428/blob", host: "3323068428", answer: dnsAnswer("198.18.0.12")},
		{name: "fake-IP address as a hexadecimal number", location: "http://0xc612000c/blob", host: "0xc612000c", answer: dnsAnswer("198.18.0.12")},
	} {
		for _, route := range []string{"proxied", "direct"} {
			t.Run(route+"/"+tt.name, func(t *testing.T) {
				redirect := func(response http.ResponseWriter, request *http.Request) {
					if request.Host != "registry.example" {
						_, _ = io.WriteString(response, "must-not-be-fetched")
						return
					}
					response.Header().Set("Location", tt.location)
					response.WriteHeader(http.StatusTemporaryRedirect)
				}
				// Through a proxy the registry itself has a fake-IP answer;
				// without one it must have a real one.
				answers := staticIPResolver{"registry.example": dnsAnswer("198.18.0.10"), tt.host: tt.answer}
				var proxy *recordingProxy
				dialer := &routedDialer{}
				proxyURL, wantDials := "", []string(nil)
				if route == "proxied" {
					proxy = newRecordingProxy(t, redirect)
					proxyURL, dialer.proxies = proxy.URL, map[string]bool{proxy.address(): true}
				} else {
					registry := httptest.NewServer(http.HandlerFunc(redirect))
					t.Cleanup(registry.Close)
					answers["registry.example"] = dnsAnswer("93.184.216.34")
					dialer.direct, wantDials = registry.Listener.Addr().String(), []string{"93.184.216.34:80"}
				}
				client, err := newRegistryClient("http://registry.example", proxyURL, answers, dialer.DialContext)
				if err != nil {
					t.Fatal(err)
				}

				response, err := client.Get("http://registry.example/v2/acme/widget/blobs/sha256:fixture")
				if response != nil {
					_ = response.Body.Close()
				}
				want := tt.want
				if want == "" {
					want = unsafeTarget[route]
				}
				if err == nil || clientReasonOf(err) != want {
					t.Fatalf("GET with a redirect to %s: error %v, want the refusal %q", tt.location, err, want)
				}
				if proxy != nil {
					if got := proxy.requestedHosts(); !slices.Equal(got, []string{"registry.example"}) {
						t.Fatalf("proxy carried %v, want the fake-IP registry and not the redirect target", got)
					}
				}
				if got := dialer.directDials(); !slices.Equal(got, wantDials) {
					t.Fatalf("direct dials = %v, want %v", got, wantDials)
				}
			})
		}
	}
}

// Neither an address literal nor localhost is a name that only the proxy can
// resolve, so a fake-IP answer for either keeps its refusal.
func TestFakeIPRegistryWithProxyRefusesAddressAndLocalhostOrigins(t *testing.T) {
	for _, origin := range []string{"http://198.18.0.10", "http://[2001:2::a]", "http://localhost:5000"} {
		proxy := newRecordingProxy(t, nil)
		var dialer net.Dialer
		client, err := newRegistryClient(origin, proxy.URL, staticIPResolver{
			"198.18.0.10": dnsAnswer("198.18.0.10"),
			"2001:2::a":   dnsAnswer("2001:2::a"),
			"localhost":   dnsAnswer("198.18.0.10"),
		}, dialer.DialContext)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Get(origin + "/v2/")
		if response != nil {
			_ = response.Body.Close()
		}
		if err == nil || clientReasonOf(err) != "unsafe Docker proxy target" || len(proxy.requestedHosts()) != 0 {
			t.Fatalf("registry %s with a fake-IP answer: error %v, proxy carried %v", origin, err, proxy.requestedHosts())
		}
	}
}

func TestDirectRegistryRefusesFakeIPAnswerWithTheRemedy(t *testing.T) {
	registry := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Www-Authenticate", `Bearer realm="http://auth.example/token",service="registry.example"`)
		response.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	const remedy = "a fake-IP address from proxy software in TUN/fake-IP mode; " +
		"set [egress] upstream_proxy or DEPSILO_EGRESS_UPSTREAM_PROXY to the proxy software's HTTP port and restart, " +
		"see docs/deployment.md#upstream-network-egress"
	fakeIP := net.IPAddr{IP: net.ParseIP("198.18.0.10")}
	for _, tt := range []struct {
		name     string
		origin   string
		resolver staticIPResolver
		// wantReason is the reason that a 502 body gives.
		wantReason string
		wantDials  []string
	}{
		{
			name:       "registry",
			resolver:   staticIPResolver{"registry.example": {fakeIP}},
			wantReason: "registry.example resolved to 198.18.0.10, " + remedy,
		},
		{
			name: "Bearer realm",
			resolver: staticIPResolver{
				"registry.example": {{IP: net.ParseIP("93.184.216.34")}},
				"auth.example":     {{IP: net.ParseIP("198.18.0.11")}},
			},
			wantReason: "auth.example resolved to 198.18.0.11, " + remedy,
			wantDials:  []string{"93.184.216.34:80"},
		},
		// The refusal names an IPv4 placeholder whenever the answer has one.
		{
			name:       "registry with placeholders of both families",
			resolver:   staticIPResolver{"registry.example": dnsAnswer("198.18.0.10", "2001:2::a")},
			wantReason: "registry.example resolved to 198.18.0.10, " + remedy,
		},
		{
			name:       "registry with the IPv6 placeholder first",
			resolver:   staticIPResolver{"registry.example": dnsAnswer("2001:2::a", "198.18.0.10")},
			wantReason: "registry.example resolved to 198.18.0.10, " + remedy,
		},
		{
			name:       "registry with IPv6 placeholders only",
			resolver:   staticIPResolver{"registry.example": dnsAnswer("2001:2::a", "2001:2::b")},
			wantReason: "registry.example resolved to 2001:2::a, " + remedy,
		},
		{
			name: "Bearer realm with IPv6 placeholders only",
			resolver: staticIPResolver{
				"registry.example": dnsAnswer("93.184.216.34"),
				"auth.example":     dnsAnswer("2001:2::b"),
			},
			wantReason: "auth.example resolved to 2001:2::b, " + remedy,
			wantDials:  []string{"93.184.216.34:80"},
		},
		// A proxy refuses each of the following answers too, so setting one
		// would not help and the refusal does not suggest it.
		{
			name: "link-local answer keeps its refusal",
			resolver: staticIPResolver{
				"registry.example": {{IP: net.ParseIP("169.254.169.254")}, fakeIP},
			},
			wantReason: "unsafe Docker dial target",
		},
		{
			name:       "fake-IP and public answers keep the refusal",
			resolver:   staticIPResolver{"registry.example": {fakeIP, {IP: net.ParseIP("93.184.216.34")}}},
			wantReason: "unsafe Docker dial target",
		},
		{
			name:       "private and fake-IP answers keep the refusal",
			resolver:   staticIPResolver{"registry.example": {{IP: net.ParseIP("10.0.0.5")}, fakeIP}},
			wantReason: "unsafe Docker dial target",
		},
		{
			// fc00::/18 is a documented IPv6 fake-IP range, so the refusal
			// names the proxy software's setting instead of a proxy.
			name:       "fake-IP and unique-local IPv6 answers get the IPv6 fake-IP remedy",
			resolver:   staticIPResolver{"registry.example": {fakeIP, {IP: net.ParseIP("fc00::a")}}},
			wantReason: "registry.example resolved into fc00::/18, an IPv6 fake-IP range that Depsilo cannot tell from a private network; " + uniqueLocalFakeIPRemedy,
		},
		{
			name:       "literal fake-IP registry keeps the refusal",
			origin:     "http://198.18.0.10",
			resolver:   staticIPResolver{"198.18.0.10": {fakeIP}},
			wantReason: "unsafe Docker dial target",
		},
		{
			name:       "literal IPv6 placeholder registry keeps the refusal",
			origin:     "http://[2001:2::a]",
			resolver:   staticIPResolver{"2001:2::a": dnsAnswer("2001:2::a")},
			wantReason: "unsafe Docker dial target",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			origin := tt.origin
			if origin == "" {
				origin = "http://registry.example"
			}
			dialer := &routedDialer{direct: registry.Listener.Addr().String()}
			client, err := newRegistryClient(origin, "", tt.resolver, dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}

			_, err = NewAuthManager().GetToken(
				context.Background(), client, origin, "registry", "operator", "secret",
				"repository:acme/widget:pull",
			)
			if err == nil || !strings.HasSuffix(clientReasonOf(err), ": "+tt.wantReason) {
				t.Fatalf("GetToken error = %v, want a 502 reason %q", err, tt.wantReason)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("GetToken error carries credentials: %v", err)
			}
			if got := dialer.directDials(); !slices.Equal(got, tt.wantDials) {
				t.Fatalf("direct dials = %v, want %v", got, tt.wantDials)
			}
		})
	}
}

// A registry that no_proxy or a loopback URL keeps off an existing default
// proxy cannot be helped by setting upstream_proxy, so its fake-IP refusal
// asks for real DNS answers instead.
func TestRegistryKeptDirectRefusesFakeIPAnswerWithTheDNSRemedy(t *testing.T) {
	proxy := newRecordingProxy(t, nil)
	registry := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Www-Authenticate", `Bearer realm="http://auth.example/token",service="registry.local"`)
		response.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	policy, err := egress.NewPolicy(proxy.URL, []string{"mirror.lan"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	const remedy = "a fake-IP address from proxy software in TUN/fake-IP mode; " +
		"[egress] no_proxy or a loopback registry URL keeps this registry off the proxy, " +
		"so give its host names real DNS answers, for example with fake-ip-filter in the proxy software, " +
		"or remove the registry from no_proxy, see docs/deployment.md#upstream-network-egress"
	for _, tt := range []struct {
		name      string
		url       string
		resolver  staticIPResolver
		wantError string
		wantDials []string
	}{
		{
			name:      "no_proxy registry",
			url:       "http://mirror.lan",
			resolver:  staticIPResolver{"mirror.lan": {{IP: net.ParseIP("198.18.0.10")}}},
			wantError: "mirror.lan resolved to 198.18.0.10, " + remedy,
		},
		{
			name:      "no_proxy registry with IPv6 placeholders",
			url:       "http://mirror.lan",
			resolver:  staticIPResolver{"mirror.lan": dnsAnswer("2001:2::a")},
			wantError: "mirror.lan resolved to 2001:2::a, " + remedy,
		},
		{
			name: "Bearer realm of a loopback registry",
			url:  "http://localhost:5000",
			resolver: staticIPResolver{
				"localhost":    {{IP: net.ParseIP("127.0.0.1")}},
				"auth.example": {{IP: net.ParseIP("198.18.0.11")}},
			},
			wantError: "auth.example resolved to 198.18.0.11, " + remedy,
			wantDials: []string{"127.0.0.1:5000"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dialer := &routedDialer{
				proxies: map[string]bool{proxy.address(): true},
				direct:  registry.Listener.Addr().String(),
			}
			resolver := newResolver(config.DockerConfig{Registries: []config.RegistryConfig{
				{Name: "kept-direct", URL: tt.url},
			}}, policy, tt.resolver, dialer.DialContext)
			reg := resolver.Default()

			_, err := NewAuthManager().GetToken(
				context.Background(), reg.Client, reg.URL, reg.Name, "", "",
				"repository:acme/widget:pull",
			)
			if err == nil || !strings.HasSuffix(err.Error(), ": "+tt.wantError) {
				t.Fatalf("GetToken error = %v, want it to end with %q", err, tt.wantError)
			}
			if strings.Contains(err.Error(), "set [egress] upstream_proxy") {
				t.Fatalf("GetToken error suggests the proxy that already exists: %v", err)
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

// A unique-local answer from a documented range of proxy software, or one
// beside fake-IP placeholders, is IPv6 fake-IP that Depsilo cannot tell from a
// private network. Such a registry answer used to pin a public registry as a
// private network, which let a private Bearer realm through, with a proxy and
// without one. It is now refused with the proxy software's setting before
// anything is dialed or sent to the proxy.
func TestUniqueLocalFakeIPAnswerIsRefusedWithAndWithoutProxy(t *testing.T) {
	const inRange = ", an IPv6 fake-IP range that Depsilo cannot tell from a private network; " + uniqueLocalFakeIPRemedy
	for _, tt := range []struct {
		name     string
		registry []net.IPAddr
		want     string
		// hidden stays out of the 502 reason; logged reaches the server log.
		hidden, logged string
	}{
		{
			name:     "Surge's IPv6 prefix only",
			registry: dnsAnswer("fd00:6152::46"),
			want:     "registry.example resolved into fd00:6152::/32" + inRange,
			hidden:   "fd00:6152::46",
			logged:   "(DNS answer: fd00:6152::46)",
		},
		{
			name:     "mihomo's IPv6 example range only",
			registry: dnsAnswer("fdfe:dcba:9876::46"),
			want:     "registry.example resolved into fdfe:dcba:9876::/48" + inRange,
			hidden:   "fdfe:dcba:9876::46",
			logged:   "(DNS answer: fdfe:dcba:9876::46)",
		},
		{
			name:     "a placeholder and a documented unique-local address",
			registry: dnsAnswer("198.18.0.70", "fdfe:dcba:9876::46"),
			want:     "registry.example resolved into fdfe:dcba:9876::/48" + inRange,
			hidden:   "fdfe:dcba:9876::46",
			logged:   "(DNS answer: 198.18.0.70, fdfe:dcba:9876::46)",
		},
		{
			// Any other unique-local prefix may be a real network's, so the
			// reason names none.
			name:     "a placeholder and another unique-local address",
			registry: dnsAnswer("198.18.0.70", "fd12:3456:789a::5"),
			want: "registry.example resolved to fake-IP addresses and a unique-local address, " +
				"which Depsilo cannot tell from a private network; " + uniqueLocalFakeIPRemedy,
			hidden: "fd12",
			logged: "(DNS answer: 198.18.0.70, fd12:3456:789a::5)",
		},
	} {
		for _, route := range []string{"proxied", "direct"} {
			t.Run(route+"/"+tt.name, func(t *testing.T) {
				// The registry challenges as a public registry would and names
				// a realm on a private network.
				handle := func(response http.ResponseWriter, request *http.Request) {
					if request.URL.Path == "/v2/" {
						response.Header().Set("Www-Authenticate", `Bearer realm="http://auth.example/token",service="registry.example"`)
						response.WriteHeader(http.StatusUnauthorized)
						return
					}
					response.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(response, `{"token":"must-not-be-returned","expires_in":300}`)
				}
				var proxy *recordingProxy
				policy, connect := egress.Policy{}, ""
				if route == "proxied" {
					proxy = newRecordingProxy(t, handle)
					var err error
					if policy, err = egress.NewPolicy(proxy.URL, nil, nil); err != nil {
						t.Fatal(err)
					}
					connect = proxy.address()
				} else {
					server := httptest.NewServer(http.HandlerFunc(handle))
					t.Cleanup(server.Close)
					connect = server.Listener.Addr().String()
				}
				// Every dial is recorded, the proxy's included.
				var mu sync.Mutex
				var dials []string
				dial := func(ctx context.Context, network, address string) (net.Conn, error) {
					mu.Lock()
					dials = append(dials, address)
					mu.Unlock()
					var dialer net.Dialer
					return dialer.DialContext(ctx, network, connect)
				}
				registry := newResolver(config.DockerConfig{Registries: []config.RegistryConfig{
					{Name: "hub", URL: "http://registry.example"},
				}}, policy, staticIPResolver{
					"registry.example": tt.registry,
					"auth.example":     dnsAnswer("10.20.30.40"),
				}, dial).Default()

				_, err := NewAuthManager().GetToken(
					context.Background(), registry.Client, registry.URL, registry.Name, "operator", "secret",
					"repository:acme/widget:pull",
				)
				if err == nil {
					t.Fatal("GetToken succeeded for a registry answered with unique-local fake-IP")
				}
				reason := clientReasonOf(err)
				if want := "auth discovery failed at http://registry.example: " + tt.want; reason != want {
					t.Fatalf("502 reason = %q, want %q", reason, want)
				}
				if strings.Contains(reason, tt.hidden) {
					t.Fatalf("502 reason %q names %q", reason, tt.hidden)
				}
				if !strings.Contains(err.Error(), tt.logged) {
					t.Fatalf("logged error %q lacks %q", err.Error(), tt.logged)
				}
				mu.Lock()
				defer mu.Unlock()
				if len(dials) != 0 {
					t.Fatalf("dialed %v, want nothing dialed", dials)
				}
				if proxy != nil && len(proxy.requestedHosts()) != 0 {
					t.Fatalf("proxy carried %v, want nothing", proxy.requestedHosts())
				}
			})
		}
	}
}

// A request to another host can come before any request to the registry.
// When the registry's own answer is unique-local fake-IP, that request is
// refused with the registry's refusal, which says why.
func TestUniqueLocalFakeIPRegistryExplainsTheRefusalOfAnEarlierRequest(t *testing.T) {
	for _, route := range []string{"proxied", "direct"} {
		t.Run(route, func(t *testing.T) {
			proxy := newRecordingProxy(t, nil)
			dialer := &routedDialer{proxies: map[string]bool{proxy.address(): true}}
			proxyURL := proxy.URL
			if route == "direct" {
				proxyURL = ""
			}
			client, err := newRegistryClient("http://registry.example", proxyURL, staticIPResolver{
				"registry.example": dnsAnswer("fdfe:dcba:9876::46"),
				"cdn.example":      dnsAnswer("93.184.216.35"),
			}, dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}

			response, err := client.Get("http://cdn.example/blob")
			if response != nil {
				_ = response.Body.Close()
			}
			want := "registry.example resolved into fdfe:dcba:9876::/48, an IPv6 fake-IP range that Depsilo cannot tell from a private network; " +
				uniqueLocalFakeIPRemedy
			if err == nil || clientReasonOf(err) != want {
				t.Fatalf("cross-origin request first: error %v, want the refusal %q", err, want)
			}
			if got := proxy.requestedHosts(); len(got) != 0 {
				t.Fatalf("proxy carried %v, want nothing", got)
			}
			if got := dialer.directDials(); len(got) != 0 {
				t.Fatalf("direct dials = %v, want none", got)
			}
		})
	}
}

// A unique-local answer outside the documented ranges of proxy software and
// without placeholders is a private network as before: such a registry works
// without a proxy and may name a Bearer realm on a private network.
func TestUniqueLocalRegistryOutsideTheProxyRangesStaysPrivate(t *testing.T) {
	for _, address := range []string{
		"fd12:3456:789a::1",
		"fc00:f853:ccd:e793::1", // kind's default network, outside fc00::/18
	} {
		t.Run(address, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/v2/" {
					response.Header().Set("Www-Authenticate", `Bearer realm="http://auth.example/token",service="registry.example"`)
					response.WriteHeader(http.StatusUnauthorized)
					return
				}
				response.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(response, `{"token":"private-realm-token","expires_in":300}`)
			}))
			t.Cleanup(server.Close)
			dialer := &routedDialer{direct: server.Listener.Addr().String()}
			client, err := newRegistryClient("http://registry.example", "", staticIPResolver{
				"registry.example": dnsAnswer(address),
				"auth.example":     dnsAnswer("10.20.30.40"),
			}, dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}

			token, err := NewAuthManager().GetToken(
				context.Background(), client, "http://registry.example", "registry", "operator", "secret",
				"repository:acme/widget:pull",
			)
			if err != nil || token != "private-realm-token" {
				t.Fatalf("GetToken = %q, %v; want the private realm's token", token, err)
			}
			if got, want := dialer.directDials(), []string{net.JoinHostPort(address, "80"), "10.20.30.40:80"}; !slices.Equal(got, want) {
				t.Fatalf("direct dials = %v, want %v", got, want)
			}
		})
	}
}

// uniqueLocalFakeIPError decides from the host and its answer alone. The
// registry clients ask it only about answers that classify as unsafe, so the
// tests through them cannot show that it leaves a private unique-local answer,
// a placeholder answer or an address literal alone.
func TestUniqueLocalFakeIPErrorRefusesOnlyUniqueLocalFakeIPAnswers(t *testing.T) {
	for _, tt := range []struct {
		name   string
		host   string // registry.example when empty
		answer []net.IPAddr
		// refused reports whether the answer gets the refusal; wantRange is
		// the documented range that the refusal names, empty for none.
		refused   bool
		wantRange string
	}{
		{name: "a unique-local network", answer: dnsAnswer("fd12:3456:789a::1")},
		{name: "two addresses of a unique-local network", answer: dnsAnswer("fd12:3456:789a::1", "fd12:3456:789a::2")},
		{name: "kind's default network", answer: dnsAnswer("fc00:f853:ccd:e793::1")},
		{name: "placeholders only", answer: dnsAnswer("198.18.0.70", "2001:2::46")},
		{name: "a placeholder and a private IPv4 address", answer: dnsAnswer("2001:2::1", "10.0.0.1")},
		{name: "a documented range and a public address", answer: dnsAnswer("fd00:6152::46", "93.184.216.34")},
		{name: "an address literal", host: "fd00:6152::46", answer: dnsAnswer("fd00:6152::46")},
		{name: "localhost", host: "localhost", answer: dnsAnswer("198.18.0.70", "fd12:3456:789a::5")},
		{name: "a placeholder and another unique-local address", answer: dnsAnswer("198.18.0.70", "fd12:3456:789a::5"), refused: true},
		{name: "a documented range only", answer: dnsAnswer("fd00:6152::46"), refused: true, wantRange: "fd00:6152::/32"},
		{
			name:   "another unique-local address before a documented range",
			answer: dnsAnswer("fd12:3456:789a::5", "fdfe:dcba:9876::46"), refused: true, wantRange: "fdfe:dcba:9876::/48",
		},
		{name: "an IPv6 placeholder and fc00::/18", answer: dnsAnswer("2001:2::1", "fc00::46"), refused: true, wantRange: "fc00::/18"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			host := tt.host
			if host == "" {
				host = "registry.example"
			}
			err := uniqueLocalFakeIPError(host, tt.answer)
			if !tt.refused {
				if err != nil {
					t.Fatalf("refusal %v, want none", err)
				}
				return
			}
			var refusal *uniqueLocalFakeIPRefusal
			if !errors.As(err, &refusal) {
				t.Fatalf("error %v, want the unique-local fake-IP refusal", err)
			}
			gotRange := ""
			if refusal.proxyRange.IsValid() {
				gotRange = refusal.proxyRange.String()
			}
			if gotRange != tt.wantRange {
				t.Fatalf("refusal names range %q, want %q", gotRange, tt.wantRange)
			}
		})
	}
}
