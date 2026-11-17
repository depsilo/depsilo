package docker

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/config"
	"depsilo/internal/egress"
)

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

// Egress routes of a registry, named in its startup log line.
const (
	egressRouteOwnProxy     = "own proxy"
	egressRouteDefaultProxy = "default proxy"
	egressRouteDirect       = "direct"
)

// Remedies that a direct fake-IP refusal names. A registry without any proxy
// needs one; a registry that [egress] keeps direct already has a default
// proxy it may not use, so it needs real DNS answers instead.
const (
	fakeIPRemedySetProxy = "set [egress] upstream_proxy or DEPSILO_EGRESS_UPSTREAM_PROXY to the proxy software's HTTP port and restart, " +
		"see docs/deployment.md#upstream-network-egress"
	fakeIPRemedyKeptDirect = "[egress] no_proxy or a loopback registry URL keeps this registry off the proxy, " +
		"so give its host names real DNS answers, for example with fake-ip-filter in the proxy software, " +
		"or remove the registry from no_proxy, see docs/deployment.md#upstream-network-egress"
)

// registryEgress is a registry's route, chosen once per registry: the proxy
// that carries every request of its client, including the Bearer realm and
// redirects, or none.
type registryEgress struct {
	proxy  string // empty for a direct registry
	route  string // named in the startup log line
	remedy string // advice of a direct client's fake-IP refusal
}

// registryEgressFor chooses a registry's route. The registry's own proxy
// wins. Otherwise the [egress] default applies exactly as it does to a
// package Upstream, so loopback and no_proxy hosts stay direct.
func registryEgressFor(registry config.RegistryConfig, policy egress.Policy) registryEgress {
	if registry.Proxy != "" {
		return registryEgress{proxy: registry.Proxy, route: egressRouteOwnProxy}
	}
	if proxy := policy.UpstreamProxyFor(registry.URL); proxy != "" {
		return registryEgress{proxy: proxy, route: egressRouteDefaultProxy}
	}
	if policy.HasUpstreamProxy() {
		return registryEgress{route: egressRouteDirect, remedy: fakeIPRemedyKeptDirect}
	}
	return registryEgress{route: egressRouteDirect, remedy: fakeIPRemedySetProxy}
}

// logRegistryEgress names a registry's route in the manner of the Upstream
// pool's "using proxy" line. The proxy appears as scheme and host only, so
// userinfo never reaches the log.
func logRegistryEgress(name, route, proxy string) {
	fields := []zap.Field{zap.String("registry", name), zap.String("route", route)}
	if proxy != "" {
		fields = append(fields, zap.String("proxy", proxyOrigin(proxy)))
	}
	zap.L().Info("docker registry egress", fields...)
}

func proxyOrigin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "***"
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String()
}

type guardedDialer struct {
	origin       *url.URL
	resolver     ipResolver
	dialContext  func(context.Context, string, string) (net.Conn, error)
	fakeIPRemedy string

	mu             sync.Mutex
	originScope    networkScope
	originScopeSet bool
}

// newRegistryClient builds the client of a registry that no [egress] policy
// keeps direct: without a proxy, its fake-IP refusal suggests setting one.
func newRegistryClient(
	origin, proxy string,
	resolver ipResolver,
	dialContext func(context.Context, string, string) (net.Conn, error),
) (*http.Client, error) {
	return buildRegistryClient(origin, proxy, fakeIPRemedySetProxy, resolver, dialContext)
}

// buildRegistryClient builds a registry's client. fakeIPRemedy is the advice
// of a direct client's refusal of a fake-IP answer; a proxied client has none.
func buildRegistryClient(
	origin, proxy, fakeIPRemedy string,
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
		guard := &guardedDialer{origin: originURL, resolver: resolver, dialContext: dialContext, fakeIPRemedy: fakeIPRemedy}
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
	host := request.URL.Hostname()
	addresses, err := transport.resolver.LookupIPAddr(request.Context(), host)
	if err != nil || len(addresses) == 0 {
		return errors.New("resolve Docker proxy target")
	}
	targetScope := proxiedScope(host, addresses)
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

	host := transport.origin.Hostname()
	addresses, err := transport.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return networkUnsafe, errors.New("resolve Docker registry")
	}
	scope := proxiedScope(host, addresses)
	if scope == networkUnsafe || !transport.pinOriginScope(scope) {
		return networkUnsafe, errors.New("unsafe Docker registry network scope")
	}
	return scope, nil
}

// proxiedScope classifies a host that a proxy will carry. It keeps every rule
// of resolvedScope with one exception: a host name whose every DNS answer lies
// in egress.BenchmarkingRange counts as public. Proxy software in TUN/fake-IP
// mode answers that way for public hosts; the placeholder says nothing about
// the real address, and the proxy resolves the name itself. An address
// literal is not a DNS answer, and an answer that mixes placeholders with
// other addresses stays unsafe.
func proxiedScope(host string, addresses []net.IPAddr) networkScope {
	if dnsHostName(host) && allFakeIP(addresses) {
		return networkPublic
	}
	return resolvedScope(addresses)
}

// dnsHostName reports whether only DNS can resolve host: it is neither
// localhost nor an address in a form that URL parsers and resolvers read as
// one. IPv6 literals contain a colon, and a host whose last label is a number,
// such as 198.18.0.5, 3323068421, 0xc6120005 or 198.18.5, is an IPv4 address
// to them.
func dnsHostName(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.ContainsAny(host, ":%[]") {
		return false
	}
	labels := strings.Split(host, ".")
	last := labels[len(labels)-1]
	if hex, ok := strings.CutPrefix(last, "0x"); ok {
		return strings.Trim(hex, "0123456789abcdef") != ""
	}
	return strings.Trim(last, "0123456789") != ""
}

// allFakeIP reports whether a DNS answer consists of fake-IP placeholders
// only.
func allFakeIP(addresses []net.IPAddr) bool {
	for _, address := range addresses {
		ip, ok := netip.AddrFromSlice(address.IP)
		if !ok || !egress.BenchmarkingRange.Contains(ip.Unmap()) {
			return false
		}
	}
	return len(addresses) > 0
}

// fakeIPDialError explains a direct dial refused for a host name answered
// only with fake-IP addresses, the one answer a proxy would carry, and names
// the remedy. A Docker client reads it in the 502 body, so it carries only
// the dialed host, its first address and a fixed remedy.
func fakeIPDialError(host string, addresses []net.IPAddr, remedy string) error {
	if remedy == "" {
		remedy = fakeIPRemedySetProxy
	}
	address, _ := netip.AddrFromSlice(addresses[0].IP)
	return fmt.Errorf("%s resolved to %s, a fake-IP address from proxy software in TUN/fake-IP mode; %s",
		host, address.Unmap(), remedy)
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
		// Only the answer that proxiedScope accepts is worth the proxy
		// remedy. Mixed answers and address literals stay refused through
		// a proxy too, so they keep the plain refusal.
		if dnsHostName(host) && allFakeIP(addresses) {
			return nil, fakeIPDialError(host, addresses, dialer.fakeIPRemedy)
		}
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
