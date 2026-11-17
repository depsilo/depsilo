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

// Refusals of the registry clients' guards. Their fixed text may appear in a
// 502 body.
var (
	errInvalidRegistryURL      = &refusal{text: "invalid Docker registry URL"}
	errInvalidRegistryProxyURL = &refusal{text: "invalid Docker registry proxy URL"}
	errInvalidProxyTarget      = &refusal{text: "invalid Docker proxy target"}
	errUnsafeProxyTarget       = &refusal{text: "unsafe Docker proxy target"}
	errProxyTargetCrossedScope = &refusal{text: "Docker proxy target crossed network scope"}
	errRegistryChangedScope    = &refusal{text: "Docker registry changed network scope"}
	errRedirectLimit           = &refusal{text: "Docker redirect limit exceeded"}
	errUnsafeRedirectTarget    = &refusal{text: "unsafe Docker redirect target"}
	errInvalidDialTarget       = &refusal{text: "invalid Docker dial target"}
	errUnsafeDialTarget        = &refusal{text: "unsafe Docker dial target"}
	errDialTargetCrossedScope  = &refusal{text: "Docker dial target crossed network scope"}
	errUnresolvedRegistry      = &refusal{text: "resolve Docker registry"}
	errUnsafeRegistryScope     = &refusal{text: "unsafe Docker registry network scope"}
)

// unresolvedTarget refuses a host that the guard could not resolve; the
// lookup error, if any, is kept for the server log.
func unresolvedTarget(text string, lookupErr error) error {
	return &refusal{text: text, cause: lookupErr}
}

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
		fields = append(fields, zap.String("proxy", urlOrigin(proxy)))
	}
	zap.L().Info("docker registry egress", fields...)
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
		return nil, errInvalidRegistryURL
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
			return nil, errInvalidRegistryProxyURL
		}
		transport.Proxy = http.ProxyURL(proxyURL)
		// The token request shares this transport, so the hook covers it too.
		transport.OnProxyConnectResponse = refuseProxyConnect
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
		return errInvalidProxyTarget
	}
	host := request.URL.Hostname()
	addresses, err := transport.resolver.LookupIPAddr(request.Context(), host)
	if err != nil || len(addresses) == 0 {
		return unresolvedTarget("resolve Docker proxy target", err)
	}
	targetScope := proxiedScope(host, addresses)
	if targetScope == networkUnsafe {
		if refusal := uniqueLocalFakeIPError(host, addresses); refusal != nil {
			return refusal
		}
		return errUnsafeProxyTarget
	}

	if sameHTTPOrigin(transport.origin, request.URL) {
		if !transport.pinOriginScope(targetScope) {
			return errRegistryChangedScope
		}
		return nil
	}
	originScope, err := transport.scopeForOrigin(request.Context())
	if err != nil {
		return originRefusal(err, errProxyTargetCrossedScope)
	}
	if !scopeAllows(originScope, targetScope) {
		return errProxyTargetCrossedScope
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
		return networkUnsafe, errUnresolvedRegistry
	}
	scope := proxiedScope(host, addresses)
	if scope == networkUnsafe {
		if refusal := uniqueLocalFakeIPError(host, addresses); refusal != nil {
			return networkUnsafe, refusal
		}
		return networkUnsafe, errUnsafeRegistryScope
	}
	if !transport.pinOriginScope(scope) {
		return networkUnsafe, errUnsafeRegistryScope
	}
	return scope, nil
}

// originRefusal is the refusal of a target whose registry has no usable
// network scope: the registry's own unique-local fake-IP refusal, which says
// why, or fallback.
func originRefusal(originErr, fallback error) error {
	var uniqueLocal *uniqueLocalFakeIPRefusal
	if errors.As(originErr, &uniqueLocal) {
		return uniqueLocal
	}
	return fallback
}

// proxiedScope classifies a host that a proxy will carry. It keeps every rule
// of resolvedScope with one exception: a host name whose every DNS answer is a
// fake-IP placeholder (see allFakeIP), from 198.18.0.0/15, 2001:2::/48 or
// both and in any order, counts as public. Proxy software in TUN/fake-IP mode
// answers that way for public hosts; the placeholder says nothing about the
// real address, and the proxy resolves the name itself. An address literal is
// not a DNS answer, and an answer that mixes placeholders with any other
// address stays unsafe.
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
// only: addresses in a benchmarking range (egress.FakeIPRange), IPv4, IPv6 or
// both. Unique-local fake-IP addresses are no placeholders, since they cannot
// be told from a private network; see uniqueLocalFakeIPError.
func allFakeIP(addresses []net.IPAddr) bool {
	for _, address := range addresses {
		ip, ok := netip.AddrFromSlice(address.IP)
		if !ok {
			return false
		}
		if _, placeholder := egress.FakeIPRange(ip); !placeholder {
			return false
		}
	}
	return len(addresses) > 0
}

// fakeIPDialError refuses a direct dial to a host name answered only with
// fake-IP placeholders and names the remedy; see fakeIPRefusal. It names an
// IPv4 placeholder when the answer has one, and otherwise the first IPv6
// placeholder.
func fakeIPDialError(host string, addresses []net.IPAddr, remedy string) error {
	if remedy == "" {
		remedy = fakeIPRemedySetProxy
	}
	var named netip.Addr
	for _, address := range addresses {
		ip, _ := netip.AddrFromSlice(address.IP)
		if ip = ip.Unmap(); ip.Is4() {
			named = ip
			break
		}
		if !named.IsValid() {
			named = ip
		}
	}
	return &fakeIPRefusal{host: host, address: named, remedy: remedy}
}

// uniqueLocalRange is RFC 4193's unique-local IPv6 range.
var uniqueLocalRange = netip.MustParsePrefix("fc00::/7")

// uniqueLocalFakeIPError refuses an answer that is IPv6 fake-IP from a
// unique-local range, and returns nil for any other answer. Such an answer
// belongs to a DNS host name and consists of fake-IP placeholders and
// unique-local addresses only, with at least one unique-local address, and
// with a placeholder or an address in a documented unique-local range of
// proxy software (egress.UniqueLocalProxyRange). No proxy may carry it,
// because Depsilo cannot tell a unique-local address from a private network;
// moving the proxy software's IPv6 fake-IP range into 2001:2::/48 makes it a
// placeholder answer. Its callers ask only about answers that classify as
// unsafe, which keep their plain refusal when it returns nil, but the rule
// does not rely on that: a unique-local answer outside the documented ranges
// and without placeholders is a private network and gets nil.
func uniqueLocalFakeIPError(host string, addresses []net.IPAddr) error {
	if !dnsHostName(host) {
		return nil
	}
	refusal := &uniqueLocalFakeIPRefusal{host: host}
	placeholder, uniqueLocal := false, false
	for _, address := range addresses {
		ip, ok := netip.AddrFromSlice(address.IP)
		if !ok {
			return nil
		}
		ip = ip.Unmap()
		refusal.answer = append(refusal.answer, ip)
		if _, fake := egress.FakeIPRange(ip); fake {
			placeholder = true
			continue
		}
		if !uniqueLocalRange.Contains(ip) {
			return nil
		}
		uniqueLocal = true
		if proxyRange, documented := egress.UniqueLocalProxyRange(ip); documented && !refusal.proxyRange.IsValid() {
			refusal.proxyRange = proxyRange
		}
	}
	if !uniqueLocal || !placeholder && !refusal.proxyRange.IsValid() {
		return nil
	}
	return refusal
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
			return errRedirectLimit
		}
		if err := validateAuthRealm(origin, request.URL.String()); err != nil {
			return errUnsafeRedirectTarget
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
		return nil, errInvalidDialTarget
	}
	addresses, err := dialer.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, unresolvedTarget("resolve Docker dial target", err)
	}
	targetScope := resolvedScope(addresses)
	if targetScope == networkUnsafe {
		// Only the answer that proxiedScope accepts is worth the proxy
		// remedy. Mixed answers and address literals stay refused through
		// a proxy too, so they keep the plain refusal, except unique-local
		// fake-IP, whose refusal names the proxy software's setting.
		if dnsHostName(host) && allFakeIP(addresses) {
			return nil, fakeIPDialError(host, addresses, dialer.fakeIPRemedy)
		}
		if refusal := uniqueLocalFakeIPError(host, addresses); refusal != nil {
			return nil, refusal
		}
		return nil, errUnsafeDialTarget
	}
	if !sameDialAddress(address, dialer.origin) {
		originScope, scopeErr := dialer.scopeForOrigin(ctx)
		if scopeErr != nil {
			return nil, originRefusal(scopeErr, errDialTargetCrossedScope)
		}
		if !scopeAllows(originScope, targetScope) {
			return nil, errDialTargetCrossedScope
		}
	} else if !dialer.pinOriginScope(targetScope) {
		return nil, errRegistryChangedScope
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

	host := dialer.origin.Hostname()
	addresses, err := dialer.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return networkUnsafe, errUnresolvedRegistry
	}
	scope := resolvedScope(addresses)
	if scope == networkUnsafe {
		if refusal := uniqueLocalFakeIPError(host, addresses); refusal != nil {
			return networkUnsafe, refusal
		}
		return networkUnsafe, errUnsafeRegistryScope
	}
	if !dialer.pinOriginScope(scope) {
		return networkUnsafe, errUnsafeRegistryScope
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
