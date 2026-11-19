package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"depsilo/internal/config"
	"depsilo/internal/egress"
)

func TestResolverClientDoesNotApplyWholeResponseTimeout(t *testing.T) {
	resolver := NewResolver(config.DockerConfig{
		DefaultRegistry: "test",
		Registries: []config.RegistryConfig{{
			Name: "test",
			URL:  "https://registry.example",
		}},
	}, egress.Policy{})
	registry := resolver.Default()
	if registry == nil {
		t.Fatal("default registry is nil")
	}
	if registry.Client.Timeout != 0 {
		t.Fatalf("registry client whole-response timeout = %v, want 0 for streamed blobs", registry.Client.Timeout)
	}
}

// staticIPResolver stands in for this machine's network: it answers each
// host with its addresses, and the machine's network interfaces hold none
// of them.
type staticIPResolver map[string][]net.IPAddr

func (resolver staticIPResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	addresses, ok := resolver[host]
	if !ok {
		return nil, fmt.Errorf("unexpected DNS lookup for %s", host)
	}
	return addresses, nil
}

func (staticIPResolver) InterfaceAddrs() ([]net.Addr, error) { return nil, nil }

// ipResolverFunc stands in for this machine's network with a lookup
// function; the machine's network interfaces hold no address it returns.
type ipResolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (resolve ipResolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return resolve(ctx, host)
}

func (ipResolverFunc) InterfaceAddrs() ([]net.Addr, error) { return nil, nil }

// interfacesHolding stands in for this machine's network as answers does,
// except that its network interfaces hold the given addresses, or cannot be
// listed when listErr is set.
type interfacesHolding struct {
	hostNetwork
	addresses []string
	listErr   error
}

func (machine interfacesHolding) InterfaceAddrs() ([]net.Addr, error) {
	if machine.listErr != nil {
		return nil, machine.listErr
	}
	held := make([]net.Addr, 0, len(machine.addresses))
	for _, address := range machine.addresses {
		held = append(held, &net.IPAddr{IP: net.ParseIP(address)})
	}
	return held, nil
}

func TestRegistryClientWithProxyRejectsUnsafeRedirectResolution(t *testing.T) {
	tests := []struct {
		name      string
		addresses []net.IPAddr
	}{
		{
			name:      "private",
			addresses: []net.IPAddr{{IP: net.ParseIP("192.168.10.20")}},
		},
		{
			name: "mixed public and private",
			addresses: []net.IPAddr{
				{IP: net.ParseIP("93.184.216.35")},
				{IP: net.ParseIP("192.168.10.20")},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var registryRequests atomic.Int64
			var redirectedRequests atomic.Int64
			proxy := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				switch request.URL.Hostname() {
				case "registry.example":
					registryRequests.Add(1)
					response.Header().Set("Location", "http://cdn.example/blob")
					response.WriteHeader(http.StatusTemporaryRedirect)
				case "cdn.example":
					redirectedRequests.Add(1)
					response.WriteHeader(http.StatusOK)
				default:
					http.Error(response, "unexpected proxy target", http.StatusBadGateway)
				}
			}))
			t.Cleanup(proxy.Close)

			var dialer net.Dialer
			client, err := newRegistryClient(
				"http://registry.example",
				proxy.URL,
				staticIPResolver{
					"registry.example": {{IP: net.ParseIP("93.184.216.34")}},
					"cdn.example":      tt.addresses,
				},
				dialer.DialContext,
			)
			if err != nil {
				t.Fatal(err)
			}

			response, err := client.Get("http://registry.example/v2/acme/widget/blobs/sha256:fixture")
			if response != nil {
				_ = response.Body.Close()
			}
			if err == nil {
				t.Fatalf("GET accepted redirect with %s DNS resolution", tt.name)
			}
			if got := registryRequests.Load(); got != 1 {
				t.Fatalf("registry proxy requests = %d, want 1", got)
			}
			if got := redirectedRequests.Load(); got != 0 {
				t.Fatalf("unsafe redirect proxy requests = %d, want 0", got)
			}
		})
	}
}

func TestRegistryClientWithProxyDoesNotDelegateAfterTargetDNSFailure(t *testing.T) {
	var registryRequests atomic.Int64
	var realmRequests atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Hostname() {
		case "registry.example":
			registryRequests.Add(1)
			response.Header().Set("Www-Authenticate", `Bearer realm="http://unresolved.example/token",service="registry.example"`)
			response.WriteHeader(http.StatusUnauthorized)
		case "unresolved.example":
			realmRequests.Add(1)
			response.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(response, `{"token":"must-not-be-returned","expires_in":300}`)
		default:
			http.Error(response, "unexpected proxy target", http.StatusBadGateway)
		}
	}))
	t.Cleanup(proxy.Close)

	var dialer net.Dialer
	client, err := newRegistryClient(
		"http://registry.example",
		proxy.URL,
		staticIPResolver{
			"registry.example": {{IP: net.ParseIP("93.184.216.34")}},
		},
		dialer.DialContext,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = NewAuthManager().GetToken(
		context.Background(), client, "http://registry.example", "registry", "operator", "secret",
		"repository:acme/widget:pull",
	)
	if err == nil {
		t.Fatal("GetToken delegated a realm whose local DNS lookup failed")
	}
	if got := registryRequests.Load(); got != 1 {
		t.Fatalf("registry proxy requests = %d, want 1", got)
	}
	if got := realmRequests.Load(); got != 0 {
		t.Fatalf("unresolved realm proxy requests = %d, want 0", got)
	}
}

func TestRegistryClientWithProxyPinsRegistryNetworkScope(t *testing.T) {
	var proxyRequests atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		proxyRequests.Add(1)
		response.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(proxy.Close)

	var lookups atomic.Int64
	resolver := ipResolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != "registry.example" {
			return nil, fmt.Errorf("unexpected DNS lookup for %s", host)
		}
		if lookups.Add(1) == 1 {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("10.20.30.40")}}, nil
	})
	var dialer net.Dialer
	client, err := newRegistryClient("http://registry.example", proxy.URL, resolver, dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.Get("http://registry.example/v2/")
	if err != nil {
		t.Fatalf("first registry request: %v", err)
	}
	_ = response.Body.Close()
	response, err = client.Get("http://registry.example/v2/")
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("second registry request accepted a changed network scope")
	}
	if got := proxyRequests.Load(); got != 1 {
		t.Fatalf("proxy requests = %d, want only the first public request", got)
	}
}

func TestRegistryClientWithProxyDoesNotForwardCredentialsAcrossRealmRedirect(t *testing.T) {
	var redirectedAuthorization string
	proxy := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Hostname() {
		case "registry.example":
			response.Header().Set("Www-Authenticate", `Bearer realm="http://auth.example/token",service="registry.example"`)
			response.WriteHeader(http.StatusUnauthorized)
		case "auth.example":
			username, password, ok := request.BasicAuth()
			if !ok || username != "operator" || password != "secret" {
				t.Errorf("token Basic auth = (%q, %q, %v), want configured credentials", username, password, ok)
			}
			response.Header().Set("Location", "http://child.auth.example/token")
			response.WriteHeader(http.StatusTemporaryRedirect)
		case "child.auth.example":
			redirectedAuthorization = request.Header.Get("Authorization")
			response.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(response, `{"token":"redirected-token","expires_in":300}`)
		default:
			http.Error(response, "unexpected proxy target", http.StatusBadGateway)
		}
	}))
	t.Cleanup(proxy.Close)

	var dialer net.Dialer
	client, err := newRegistryClient(
		"http://registry.example",
		proxy.URL,
		staticIPResolver{
			"registry.example":   {{IP: net.ParseIP("93.184.216.34")}},
			"auth.example":       {{IP: net.ParseIP("93.184.216.35")}},
			"child.auth.example": {{IP: net.ParseIP("93.184.216.36")}},
		},
		dialer.DialContext,
	)
	if err != nil {
		t.Fatal(err)
	}
	token, err := NewAuthManager().GetToken(
		context.Background(), client, "http://registry.example", "registry", "operator", "secret",
		"repository:acme/widget:pull",
	)
	if err != nil {
		t.Fatal(err)
	}
	if token != "redirected-token" {
		t.Fatalf("token = %q, want redirected-token", token)
	}
	if redirectedAuthorization != "" {
		t.Fatalf("redirected Authorization = %q, want empty", redirectedAuthorization)
	}
}
