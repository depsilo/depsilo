package docker

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type guardedDialer struct {
	origin      *url.URL
	resolver    ipResolver
	dialContext func(context.Context, string, string) (net.Conn, error)

	mu             sync.Mutex
	originScope    networkScope
	originScopeSet bool
}

func newRegistryClient(
	origin, proxy string,
	resolver ipResolver,
	dialContext func(context.Context, string, string) (net.Conn, error),
) (*http.Client, error) {
	originURL, err := url.Parse(origin)
	if err != nil || originURL == nil || !originURL.IsAbs() || originURL.Host == "" ||
		(originURL.Scheme != "http" && originURL.Scheme != "https") ||
		originURL.RawQuery != "" || originURL.Fragment != "" {
		return nil, errors.New("invalid Docker registry URL")
	}
	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: false},
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	var clientTransport http.RoundTripper = transport
	if proxy != "" {
		proxyURL, parseErr := url.Parse(proxy)
		if parseErr != nil || proxyURL == nil || proxyURL.Host == "" ||
			(proxyURL.Scheme != "http" && proxyURL.Scheme != "https") {
			return nil, errors.New("invalid Docker registry proxy URL")
		}
		transport.Proxy = http.ProxyURL(proxyURL)
		transport.DialContext = dialContext
		clientTransport = &scopePreflightRoundTripper{
			origin:    originURL,
			resolver:  resolver,
			transport: transport,
		}
	} else {
		guard := &guardedDialer{origin: originURL, resolver: resolver, dialContext: dialContext}
		transport.DialContext = guard.DialContext
	}
	return &http.Client{
		Transport:     clientTransport,
		CheckRedirect: secureRegistryRedirectCheck(origin),
	}, nil
}

type scopePreflightRoundTripper struct {
	origin    *url.URL
	resolver  ipResolver
	transport http.RoundTripper

	mu             sync.Mutex
	originScope    networkScope
	originScopeSet bool
}

func (transport *scopePreflightRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := transport.validateTarget(request); err != nil {
		return nil, err
	}
	return transport.transport.RoundTrip(request)
}

func (transport *scopePreflightRoundTripper) validateTarget(request *http.Request) error {
	if request == nil || request.URL == nil || request.URL.Hostname() == "" ||
		(request.URL.Scheme != "http" && request.URL.Scheme != "https") {
		return errors.New("invalid Docker proxy target")
	}
	addresses, err := transport.resolver.LookupIPAddr(request.Context(), request.URL.Hostname())
	if err != nil || len(addresses) == 0 {
		return errors.New("resolve Docker proxy target")
	}
	targetScope := resolvedScope(addresses)
	if targetScope == networkUnsafe {
		return errors.New("unsafe Docker proxy target")
	}

	if sameHTTPOrigin(transport.origin, request.URL) {
		if !transport.pinOriginScope(targetScope) {
			return errors.New("Docker registry changed network scope")
		}
		return nil
	}
	originScope, err := transport.scopeForOrigin(request.Context())
	if err != nil || !scopeAllows(originScope, targetScope) {
		return errors.New("Docker proxy target crossed network scope")
	}
	return nil
}

func (transport *scopePreflightRoundTripper) scopeForOrigin(ctx context.Context) (networkScope, error) {
	transport.mu.Lock()
	if transport.originScopeSet {
		scope := transport.originScope
		transport.mu.Unlock()
		return scope, nil
	}
	transport.mu.Unlock()

	addresses, err := transport.resolver.LookupIPAddr(ctx, transport.origin.Hostname())
	if err != nil || len(addresses) == 0 {
		return networkUnsafe, errors.New("resolve Docker registry")
	}
	scope := resolvedScope(addresses)
	if scope == networkUnsafe || !transport.pinOriginScope(scope) {
		return networkUnsafe, errors.New("unsafe Docker registry network scope")
	}
	return scope, nil
}

func (transport *scopePreflightRoundTripper) pinOriginScope(scope networkScope) bool {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if !transport.originScopeSet {
		transport.originScope = scope
		transport.originScopeSet = true
		return true
	}
	return transport.originScope == scope
}

func secureRegistryRedirectCheck(origin string) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("Docker redirect limit exceeded")
		}
		if err := validateAuthRealm(origin, request.URL.String()); err != nil {
			return errors.New("unsafe Docker redirect target")
		}
		request.Header.Del("Referer")
		request.Header.Del("Proxy-Authorization")
		if len(via) == 0 || !sameHTTPOrigin(via[len(via)-1].URL, request.URL) {
			request.Header.Del("Authorization")
			request.Header.Del("Cookie")
		}
		return nil
	}
}

func (dialer *guardedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return nil, errors.New("invalid Docker dial target")
	}
	addresses, err := dialer.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("resolve Docker dial target")
	}
	targetScope := resolvedScope(addresses)
	if targetScope == networkUnsafe {
		return nil, errors.New("unsafe Docker dial target")
	}
	if !sameDialAddress(address, dialer.origin) {
		originScope, scopeErr := dialer.scopeForOrigin(ctx)
		if scopeErr != nil || !scopeAllows(originScope, targetScope) {
			return nil, errors.New("Docker dial target crossed network scope")
		}
	} else if !dialer.pinOriginScope(targetScope) {
		return nil, errors.New("Docker registry changed network scope")
	}

	var lastErr error
	for _, candidate := range addresses {
		connection, dialErr := dialer.dialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
	}
	return nil, fmt.Errorf("dial Docker target: %w", lastErr)
}

func (dialer *guardedDialer) scopeForOrigin(ctx context.Context) (networkScope, error) {
	dialer.mu.Lock()
	if dialer.originScopeSet {
		scope := dialer.originScope
		dialer.mu.Unlock()
		return scope, nil
	}
	dialer.mu.Unlock()

	addresses, err := dialer.resolver.LookupIPAddr(ctx, dialer.origin.Hostname())
	if err != nil || len(addresses) == 0 {
		return networkUnsafe, errors.New("resolve Docker registry")
	}
	scope := resolvedScope(addresses)
	if scope == networkUnsafe || !dialer.pinOriginScope(scope) {
		return networkUnsafe, errors.New("unsafe Docker registry network scope")
	}
	return scope, nil
}

func (dialer *guardedDialer) pinOriginScope(scope networkScope) bool {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if !dialer.originScopeSet {
		dialer.originScope = scope
		dialer.originScopeSet = true
		return true
	}
	return dialer.originScope == scope
}

func scopeAllows(origin, target networkScope) bool {
	switch target {
	case networkPublic:
		return true
	case networkPrivate:
		return origin == networkPrivate || origin == networkLoopback
	case networkLoopback:
		return origin == networkLoopback
	default:
		return false
	}
}

func resolvedScope(addresses []net.IPAddr) networkScope {
	var scope networkScope
	for index, address := range addresses {
		candidate, _ := literalNetworkScope(address.IP.String())
		if candidate == networkUnsafe || index > 0 && candidate != scope {
			return networkUnsafe
		}
		scope = candidate
	}
	if len(addresses) == 0 {
		return networkUnsafe
	}
	return scope
}

func sameDialAddress(address string, origin *url.URL) bool {
	if origin == nil {
		return false
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(origin.Hostname(), ".")) &&
		port == effectivePort(origin)
}

type errorRoundTripper struct{ err error }

func (transport errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, transport.err
}
