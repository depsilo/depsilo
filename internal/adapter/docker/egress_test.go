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

func TestRegistryBehindFakeIPDNSUsesTheDefaultProxyForEveryHost(t *testing.T) {
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
	}}, policy, staticIPResolver{
		"registry.example": {{IP: net.ParseIP("198.18.0.10")}},
		"auth.example":     {{IP: net.ParseIP("198.18.0.11")}},
		"cdn.example":      {{IP: net.ParseIP("198.18.0.12")}, {IP: net.ParseIP("198.19.255.12")}},
	}, dialer.DialContext)
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
	for _, tt := range []struct{ name, address string }{
		{name: "private", address: "10.20.30.40"},
		{name: "loopback", address: "127.0.0.1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
				"registry.example": {{IP: net.ParseIP("198.18.0.10")}},
				"auth.example":     {{IP: net.ParseIP(tt.address)}},
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

func TestFakeIPRegistryWithProxyRefusesMixedAndLiteralTargets(t *testing.T) {
	fakeIP := net.IPAddr{IP: net.ParseIP("198.18.0.12")}
	for _, tt := range []struct {
		name     string
		location string
		host     string
		answer   []net.IPAddr
	}{
		{
			name: "fake-IP and public answers", location: "http://cdn.example/blob", host: "cdn.example",
			answer: []net.IPAddr{fakeIP, {IP: net.ParseIP("93.184.216.35")}},
		},
		{
			name: "fake-IP and private answers", location: "http://cdn.example/blob", host: "cdn.example",
			answer: []net.IPAddr{fakeIP, {IP: net.ParseIP("10.0.0.5")}},
		},
		{name: "literal fake-IP address", location: "http://198.18.0.12/blob", host: "198.18.0.12", answer: []net.IPAddr{fakeIP}},
		{name: "fake-IP address as a decimal number", location: "http://3323068428/blob", host: "3323068428", answer: []net.IPAddr{fakeIP}},
		{name: "fake-IP address as a hexadecimal number", location: "http://0xc612000c/blob", host: "0xc612000c", answer: []net.IPAddr{fakeIP}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			proxy := newRecordingProxy(t, func(response http.ResponseWriter, request *http.Request) {
				if request.URL.Hostname() != "registry.example" {
					_, _ = io.WriteString(response, "must-not-be-fetched")
					return
				}
				response.Header().Set("Location", tt.location)
				response.WriteHeader(http.StatusTemporaryRedirect)
			})
			var dialer net.Dialer
			client, err := newRegistryClient("http://registry.example", proxy.URL, staticIPResolver{
				"registry.example": {{IP: net.ParseIP("198.18.0.10")}},
				tt.host:            tt.answer,
			}, dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}

			response, err := client.Get("http://registry.example/v2/acme/widget/blobs/sha256:fixture")
			if response != nil {
				_ = response.Body.Close()
			}
			if err == nil {
				t.Fatalf("GET followed a redirect to %s", tt.location)
			}
			if got := proxy.requestedHosts(); !slices.Equal(got, []string{"registry.example"}) {
				t.Fatalf("proxy carried %v, want the fake-IP registry and not the redirect target", got)
			}
		})
	}

	// Neither an address literal nor localhost is a name that only the proxy
	// can resolve, so a fake-IP answer for either keeps its refusal.
	for _, origin := range []string{"http://198.18.0.10", "http://localhost:5000"} {
		proxy := newRecordingProxy(t, nil)
		var dialer net.Dialer
		client, err := newRegistryClient(origin, proxy.URL, staticIPResolver{
			"198.18.0.10": {{IP: net.ParseIP("198.18.0.10")}},
			"localhost":   {{IP: net.ParseIP("198.18.0.10")}},
		}, dialer.DialContext)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Get(origin + "/v2/")
		if response != nil {
			_ = response.Body.Close()
		}
		if err == nil || len(proxy.requestedHosts()) != 0 {
			t.Fatalf("registry %s with a fake-IP answer reached the proxy (err %v)", origin, err)
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
	for _, tt := range []struct {
		name      string
		resolver  staticIPResolver
		wantError string
		wantDials []string
	}{
		{
			name:      "registry",
			resolver:  staticIPResolver{"registry.example": {{IP: net.ParseIP("198.18.0.10")}}},
			wantError: "registry.example resolved to 198.18.0.10, " + remedy,
		},
		{
			name: "Bearer realm",
			resolver: staticIPResolver{
				"registry.example": {{IP: net.ParseIP("93.184.216.34")}},
				"auth.example":     {{IP: net.ParseIP("198.18.0.11")}},
			},
			wantError: "auth.example resolved to 198.18.0.11, " + remedy,
			wantDials: []string{"93.184.216.34:80"},
		},
		{
			name: "link-local answer keeps its refusal",
			resolver: staticIPResolver{
				"registry.example": {{IP: net.ParseIP("169.254.169.254")}, {IP: net.ParseIP("198.18.0.10")}},
			},
			wantError: "unsafe Docker dial target",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dialer := &routedDialer{direct: registry.Listener.Addr().String()}
			client, err := newRegistryClient("http://registry.example", "", tt.resolver, dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}

			_, err = NewAuthManager().GetToken(
				context.Background(), client, "http://registry.example", "registry", "operator", "secret",
				"repository:acme/widget:pull",
			)
			if err == nil || !strings.HasSuffix(err.Error(), ": "+tt.wantError) {
				t.Fatalf("GetToken error = %v, want it to end with %q", err, tt.wantError)
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
