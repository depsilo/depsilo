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
	"depsilo/internal/upstream"
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

// directTargetLeftPrivateNetwork is the 502 reason of a dial on the direct leg
// of a registry on the default-proxy route whose host no longer answers with
// private or loopback addresses only; see privateLegDialer.
const directTargetLeftPrivateNetwork = "Docker direct target left the private network"

// unresolvedTarget refuses a host that the guard could not resolve; the
// lookup error, if any, is kept for the server log.
func unresolvedTarget(text string, lookupErr error) error {
	return &refusal{text: text, cause: lookupErr}
}

// hostNetwork is the view of this machine's network that the registry
// clients check every target against: the DNS answer for a host, and the
// addresses of this machine's network interfaces, which a fake-IP answer may
// not hold (see classifyAnswer). systemNetwork is the machine's own; tests
// stand in for it.
type hostNetwork interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
	InterfaceAddrs() ([]net.Addr, error)
}

// systemNetwork is this machine's network: the system resolver and the
// addresses that net.InterfaceAddrs lists, read at every check.
type systemNetwork struct{ *net.Resolver }

func (systemNetwork) InterfaceAddrs() ([]net.Addr, error) { return net.InterfaceAddrs() }

// Egress routes of a registry, named in its startup log line.
const (
	egressRouteOwnProxy     = "own proxy"
	egressRouteDefaultProxy = "default proxy (private answers direct)"
	egressRouteDirect       = "direct"
)

// Remedies that the fake-IP refusals of a direct client name. With [egress]
// allow_fake_ip off, a refused answer needs a proxy, or, for a registry that
// [egress] keeps off its default proxy, real DNS answers. When the client
// dialed the placeholders and nothing carried them, the proxy software's TUN
// mode is the first remedy.
const (
	fakeIPRemedySetProxy = "set [egress] upstream_proxy or DEPSILO_EGRESS_UPSTREAM_PROXY to the proxy software's HTTP port and restart, " +
		"see docs/deployment.md#upstream-network-egress"
	fakeIPRemedyKeptDirect = "[egress] no_proxy or a loopback registry URL keeps this registry off the proxy, " +
		"so give its host names real DNS answers, for example with fake-ip-filter in the proxy software, " +
		"or remove the registry from no_proxy, see docs/deployment.md#upstream-network-egress"
	fakeIPRemedyTUN = "turn on the proxy software's TUN mode, " +
		"or set [egress] upstream_proxy or DEPSILO_EGRESS_UPSTREAM_PROXY to its HTTP port and restart, " +
		"see docs/deployment.md#upstream-network-egress"
	fakeIPRemedyTUNKeptDirect = "turn on the proxy software's TUN mode, " +
		"or, as [egress] no_proxy or a loopback URL keeps this registry off the proxy, give its host names real DNS answers, " +
		"for example with fake-ip-filter, or remove it from no_proxy, see docs/deployment.md#upstream-network-egress"
)

// registryEgress is a registry's route, chosen once per registry from its
// configuration: the proxy that carries the requests of its client,
// including the Bearer realm and redirects, or none.
type registryEgress struct {
	proxy  string // empty for a direct registry
	route  string // named in the startup log line
	fakeIP fakeIPRule
	// privateDirect routes each request by its own host's answer instead:
	// private and loopback answers are dialed directly, and only public and
	// fake-IP answers go through proxy. Only the [egress] default proxy sets
	// it; a registry's own proxy carries everything.
	privateDirect bool
}

// fakeIPRule is how a registry's direct client treats a host name whose DNS
// answer is a fake-IP answer (see egress.FakeIPAnswer). A proxied client
// leaves the name to its proxy instead.
type fakeIPRule struct {
	// dial is [egress] allow_fake_ip: the client dials the placeholders, as
	// other programs on the machine do, for the TUN of proxy software to
	// carry. Otherwise it refuses the answer before anything is dialed.
	dial bool
	// keptDirect marks a registry that no_proxy or a loopback URL keeps off a
	// configured upstream_proxy, which a remedy cannot suggest; its remedies
	// ask for real DNS answers instead.
	keptDirect bool
}

// refusedRemedy is the advice of an answer refused with allow_fake_ip off.
func (rule fakeIPRule) refusedRemedy() string {
	if rule.keptDirect {
		return fakeIPRemedyKeptDirect
	}
	return fakeIPRemedySetProxy
}

// notCarriedRemedy is the advice of placeholders that nothing carried.
func (rule fakeIPRule) notCarriedRemedy() string {
	if rule.keptDirect {
		return fakeIPRemedyTUNKeptDirect
	}
	return fakeIPRemedyTUN
}

// registryEgressFor chooses a registry's route. The registry's own proxy
// wins. Otherwise the [egress] default applies exactly as it does to a
// package Upstream, so loopback and no_proxy hosts stay direct, and a
// registry that the default proxy carries still dials the hosts whose
// answers are private or loopback directly. A direct registry follows
// [egress] allow_fake_ip.
func registryEgressFor(registry config.RegistryConfig, policy egress.Policy) registryEgress {
	if registry.Proxy != "" {
		return registryEgress{proxy: registry.Proxy, route: egressRouteOwnProxy}
	}
	if proxy := policy.UpstreamProxyFor(registry.URL); proxy != "" {
		return registryEgress{proxy: proxy, route: egressRouteDefaultProxy, privateDirect: true}
	}
	return registryEgress{route: egressRouteDirect, fakeIP: fakeIPRule{
		dial:       policy.AllowsFakeIP(),
		keptDirect: policy.HasUpstreamProxy(),
	}}
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

// newRegistryClient builds the client of a registry under the default
// [egress] policy, which has no default proxy: with proxy, the proxy carries
// every request, as a registry's own proxy does; without one, the client
// dials fake-IP answers itself, and its fake-IP refusals suggest a proxy.
func newRegistryClient(
	origin, proxy string,
	resolver hostNetwork,
	dialContext func(context.Context, string, string) (net.Conn, error),
) (*http.Client, error) {
	route := registryEgressFor(config.RegistryConfig{URL: origin, Proxy: proxy}, egress.Policy{})
	return buildRegistryClient("", origin, route, resolver, dialContext)
}

// buildRegistryClient builds the client of the registry name on route. A
// direct route dials through guardedDialer. A proxied route sends every
// request through the proxy behind the preflight; on the default-proxy route
// (route.privateDirect), private and loopback targets go to a direct leg
// instead, and the preflight and that leg share the registry's origin pin.
func buildRegistryClient(
	name, origin string,
	route registryEgress,
	resolver hostNetwork,
	dialContext func(context.Context, string, string) (net.Conn, error),
) (*http.Client, error) {
	originURL, err := url.Parse(origin)
	if err != nil || originURL == nil || !originURL.IsAbs() || originURL.Host == "" ||
		(originURL.Scheme != "http" && originURL.Scheme != "https") ||
		originURL.RawQuery != "" || originURL.Fragment != "" {
		return nil, errInvalidRegistryURL
	}
	client := func(transport http.RoundTripper) *http.Client {
		return &http.Client{Transport: transport, CheckRedirect: secureRegistryRedirectCheck(origin)}
	}
	if route.proxy == "" {
		guard := &guardedDialer{
			pin:         &originPin{origin: originURL, resolver: resolver, placeholders: route.fakeIP.dial, pinsOnConnect: true},
			resolver:    resolver,
			dialContext: dialContext,
			fakeIP:      route.fakeIP,
		}
		transport := newRegistryTransport()
		transport.DialContext = guard.DialContext
		return client(transport), nil
	}

	proxyURL, parseErr := url.Parse(route.proxy)
	if parseErr != nil || proxyURL == nil || proxyURL.Host == "" ||
		(proxyURL.Scheme != "http" && proxyURL.Scheme != "https") {
		return nil, errInvalidRegistryProxyURL
	}
	proxied := newRegistryTransport()
	proxied.Proxy = http.ProxyURL(proxyURL)
	// The token request shares this transport, so the hook covers it too.
	proxied.OnProxyConnectResponse = refuseProxyConnect
	proxied.DialContext = dialContext
	preflight := &scopePreflightRoundTripper{
		pin:       &originPin{origin: originURL, resolver: resolver, placeholders: true},
		resolver:  resolver,
		transport: proxied,
	}
	if !route.privateDirect {
		return client(preflight), nil
	}
	direct := newRegistryTransport()
	direct.DialContext = (&privateLegDialer{
		pin:         preflight.pin,
		resolver:    resolver,
		dialContext: dialContext,
		dials:       &directDialLog{registry: name},
	}).DialContext
	return client(&splitRoundTripper{preflight: preflight, direct: direct}), nil
}

// newRegistryTransport is the HTTP transport of one leg of a registry client.
// It has no whole-response timeout, so blobs stream.
func newRegistryTransport() *http.Transport {
	return &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: false},
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ForceAttemptHTTP2:     true,
	}
}

// originPin is a registry's network scope. The first answer for the
// registry's own host pins it for the client's lifetime: every later answer
// for that host must have the same scope, and any other host is reached only
// where scopeAllows lets the pinned scope reach it. On the default-proxy
// route, the preflight and the direct leg share one pin. On the direct route
// (pinsOnConnect), a fake-IP answer for the registry's own host pins its
// scope only once a connection to its placeholders succeeded (see
// guardedDialer), so real DNS answers given after nothing carried them take
// effect without a restart.
type originPin struct {
	origin   *url.URL
	resolver hostNetwork
	// placeholders counts a fake-IP answer for the registry's own host as
	// public (see classifyAnswer), as the client's targets count: on a
	// proxied route, because the proxy carries it, and on the direct route
	// with [egress] allow_fake_ip, because the TUN of proxy software does.
	placeholders bool
	// pinsOnConnect is set on the direct route, where only a connection that
	// the TUN carried pins a fake-IP answer for the registry's own host (see
	// guardedDialer): originScope then counts such an answer as public for
	// the check at hand without pinning it.
	pinsOnConnect bool

	mu    sync.Mutex
	scope networkScope
	set   bool
}

// pinScope pins scope on the first call and reports whether scope is the
// registry's pinned scope.
func (pin *originPin) pinScope(scope networkScope) bool {
	pin.mu.Lock()
	defer pin.mu.Unlock()
	if !pin.set {
		pin.scope, pin.set = scope, true
		return true
	}
	return pin.scope == scope
}

// admits reports whether scope is the registry's pinned scope, or whether
// nothing is pinned yet, without pinning it.
func (pin *originPin) admits(scope networkScope) bool {
	pin.mu.Lock()
	defer pin.mu.Unlock()
	return !pin.set || pin.scope == scope
}

// originScope returns the pinned scope. While nothing is pinned, it resolves
// the registry's host and pins the scope of that answer, except a fake-IP
// answer under pinsOnConnect, which counts as public without being pinned.
func (pin *originPin) originScope(ctx context.Context) (networkScope, error) {
	pin.mu.Lock()
	if pin.set {
		scope := pin.scope
		pin.mu.Unlock()
		return scope, nil
	}
	pin.mu.Unlock()

	host := pin.origin.Hostname()
	addresses, err := pin.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return networkUnsafe, errUnresolvedRegistry
	}
	scope, fakeIP, localCause := classifyAnswer(pin.resolver, host, addresses, pin.placeholders)
	if scope == networkUnsafe {
		if refusal := uniqueLocalFakeIPError(host, addresses); refusal != nil {
			return networkUnsafe, refusal
		}
		return networkUnsafe, withCause(errUnsafeRegistryScope, localCause)
	}
	if fakeIP && pin.pinsOnConnect {
		return scope, nil
	}
	if !pin.pinScope(scope) {
		return networkUnsafe, errUnsafeRegistryScope
	}
	return scope, nil
}

// admitDial checks a dial of address, whose host answered with scope,
// against the pin: the registry's own host keeps its pinned scope, and any
// other host needs a scope that the registry's scope may reach.
func (pin *originPin) admitDial(ctx context.Context, address string, scope networkScope) error {
	if sameDialAddress(address, pin.origin) {
		if !pin.pinScope(scope) {
			return errRegistryChangedScope
		}
		return nil
	}
	originScope, err := pin.originScope(ctx)
	if err != nil {
		return originRefusal(err, errDialTargetCrossedScope)
	}
	if !scopeAllows(originScope, scope) {
		return errDialTargetCrossedScope
	}
	return nil
}

// allowsPrivate reports whether the pinned scope lets the registry reach a
// private target.
func (pin *originPin) allowsPrivate() bool {
	pin.mu.Lock()
	defer pin.mu.Unlock()
	return pin.set && scopeAllows(pin.scope, networkPrivate)
}

// scopePreflightRoundTripper checks the host of every request before a proxy
// carries it: Depsilo resolves the host itself, refuses unsafe answers, and
// checks the answer against the registry's origin pin.
type scopePreflightRoundTripper struct {
	pin       *originPin
	resolver  hostNetwork
	transport http.RoundTripper
}

func (transport *scopePreflightRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := transport.validateTarget(request); err != nil {
		return nil, err
	}
	return transport.transport.RoundTrip(request)
}

func (transport *scopePreflightRoundTripper) validateTarget(request *http.Request) error {
	_, err := transport.checkTarget(request)
	return err
}

// targetCheck is what the preflight learned about a request's host: the
// network scope of its local answer, and whether that answer was a fake-IP
// answer, which counts as public (see classifyAnswer).
type targetCheck struct {
	scope  networkScope
	fakeIP bool
}

// checkTarget resolves the host of request and returns its network scope. It
// refuses a failed lookup, an unsafe or mixed answer, a fake-IP answer that
// holds an address of this machine's network interfaces, a change of the
// registry's own scope, and a target outside the scope that the registry's
// pinned scope may reach.
func (transport *scopePreflightRoundTripper) checkTarget(request *http.Request) (targetCheck, error) {
	if request == nil || request.URL == nil || request.URL.Hostname() == "" ||
		(request.URL.Scheme != "http" && request.URL.Scheme != "https") {
		return targetCheck{}, errInvalidProxyTarget
	}
	host := request.URL.Hostname()
	addresses, err := transport.resolver.LookupIPAddr(request.Context(), host)
	if err != nil || len(addresses) == 0 {
		return targetCheck{}, unresolvedTarget("resolve Docker proxy target", err)
	}
	var check targetCheck
	var localCause error
	check.scope, check.fakeIP, localCause = classifyAnswer(transport.resolver, host, addresses, true)
	if check.scope == networkUnsafe {
		if refusal := uniqueLocalFakeIPError(host, addresses); refusal != nil {
			return targetCheck{}, refusal
		}
		return targetCheck{}, withCause(errUnsafeProxyTarget, localCause)
	}

	if sameHTTPOrigin(transport.pin.origin, request.URL) {
		if !transport.pin.pinScope(check.scope) {
			return targetCheck{}, errRegistryChangedScope
		}
		return check, nil
	}
	originScope, err := transport.pin.originScope(request.Context())
	if err != nil {
		return targetCheck{}, originRefusal(err, errProxyTargetCrossedScope)
	}
	if !scopeAllows(originScope, check.scope) {
		return targetCheck{}, errProxyTargetCrossedScope
	}
	return check, nil
}

// splitRoundTripper is the transport of a registry on the default-proxy
// route. The preflight checks each request, the Bearer realm's and every
// redirect's included, and its answer for the request's own host routes the
// request: a private or loopback answer to the direct leg, which dials only
// private and loopback addresses, and a public or fake-IP answer through the
// proxy. Every other answer is refused before anything is dialed or sent to
// the proxy. No route is remembered per host, and the direct leg's dial is
// the only lookup the split adds.
type splitRoundTripper struct {
	preflight *scopePreflightRoundTripper
	direct    http.RoundTripper
}

func (split *splitRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	check, err := split.preflight.checkTarget(request)
	if err != nil {
		return nil, err
	}
	if check.scope == networkPrivate || check.scope == networkLoopback {
		return split.direct.RoundTrip(request)
	}
	hint := split.proxyHint(request.URL.Hostname(), check)
	response, err := split.preflight.transport.RoundTrip(request)
	if err != nil {
		return nil, hintProxyFailure(request, err, check.fakeIP, hint)
	}
	if request.URL.Scheme == "http" && proxyGatewayStatus(response.StatusCode) {
		return nil, newProxyStatusRefusal(response, hint)
	}
	return response, nil
}

// proxyHint is the advice that a failure on the proxied leg adds to its 502
// reason, or "" when no advice would help:
//
//   - A fake-IP answer says nothing about the host, which may be on the local
//     network where the proxy cannot reach it. A real private answer would
//     send it to the direct leg if the host is the registry's own, whose new
//     scope a restart pins, or if the registry's pinned scope may reach
//     private targets; a registry pinned public would refuse it instead.
//   - A public answer leaves only no_proxy, which takes effect for the
//     registry's own host alone: [egress] decides a registry's route from the
//     host in its URL.
func (split *splitRoundTripper) proxyHint(host string, check targetCheck) string {
	pin := split.preflight.pin
	registryHost := strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(pin.origin.Hostname(), "."))
	switch {
	case check.fakeIP && (registryHost || pin.allowsPrivate()):
		return fakeIPProxyHint(host, registryHost)
	case !check.fakeIP && registryHost:
		return noProxyHint(host)
	}
	return ""
}

// hintProxyFailure adds hint to the error of a request that the proxied leg
// carried and that got no answer. A refused CONNECT gets the hint whatever
// the answer was. Any other failed connection through the proxy, such as a
// tunnel that the proxy closes right after CONNECT or one that times out,
// gets it only for a fake-IP answer, the one that leaves the proxy alone to
// resolve the host. A TLS failure, whose reason reads tls, shows that the
// proxy reached a server, so it gets no hint; neither does a failed
// connection to the proxy itself or a request whose own context ended.
func hintProxyFailure(request *http.Request, err error, fakeIP bool, hint string) error {
	if hint == "" || request.Context().Err() != nil || isProxyConnectFailure(err) {
		return err
	}
	var refused *proxyConnectRefusal
	if errors.As(err, &refused) || (fakeIP && upstream.ClassifyFailure(err).Code != upstream.FailureTLS) {
		return &proxyHintedFailure{err: err, hint: hint}
	}
	return err
}

// proxyGatewayStatus reports whether status is one that a forward proxy
// answers when it cannot reach a host: 502, 503 or 504.
func proxyGatewayStatus(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// privateLegDialer dials for the direct leg of a registry on the default-proxy
// route. The preflight's answer chose the leg, so the dialer resolves the
// host again and dials only an answer that is still private or loopback and
// that the shared origin pin allows. Any other answer is refused, never
// dialed.
type privateLegDialer struct {
	pin         *originPin
	resolver    hostNetwork
	dialContext func(context.Context, string, string) (net.Conn, error)
	dials       *directDialLog
}

func (dialer *privateLegDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return nil, errInvalidDialTarget
	}
	addresses, err := dialer.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, unresolvedTarget("resolve Docker dial target", err)
	}
	scope := resolvedScope(addresses)
	if scope != networkPrivate && scope != networkLoopback {
		return nil, leftPrivateNetwork(host, addresses)
	}
	if err := dialer.pin.admitDial(ctx, address, scope); err != nil {
		return nil, err
	}
	dialer.dials.firstDial(host)
	return dialAddresses(ctx, dialer.dialContext, network, addresses, port)
}

// leftPrivateNetwork refuses a dial of the direct leg whose host no longer
// answers with private or loopback addresses only. A 502 body gives only the
// fixed text; the server log keeps the answer.
func leftPrivateNetwork(host string, addresses []net.IPAddr) error {
	answer := make([]string, len(addresses))
	for index, address := range addresses {
		answer[index] = address.IP.String()
	}
	return &refusal{
		text:  directTargetLeftPrivateNetwork,
		cause: fmt.Errorf("%s resolved to %s", host, strings.Join(answer, ", ")),
	}
}

// maxLoggedDirectHosts bounds the hosts that a directDialLog remembers. A
// registry names its own realm and redirect hosts, so it could make the set
// grow without end; a host beyond the bound is logged at each dial instead.
const maxLoggedDirectHosts = 256

// directDialLog writes an info line the first time the direct leg of a
// registry dials a host: the registry's name and the host, no addresses.
type directDialLog struct {
	registry string

	mu    sync.Mutex
	hosts map[string]bool
}

func (dials *directDialLog) firstDial(host string) {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	dials.mu.Lock()
	logged := dials.hosts[host]
	if !logged && len(dials.hosts) < maxLoggedDirectHosts {
		if dials.hosts == nil {
			dials.hosts = make(map[string]bool)
		}
		dials.hosts[host] = true
	}
	dials.mu.Unlock()
	if !logged {
		zap.L().Info("docker registry host dialed directly", zap.String("registry", dials.registry), zap.String("host", host))
	}
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

// classifyAnswer returns the network scope of host's DNS answer. It keeps
// every rule of resolvedScope with one exception when placeholders is set: a
// fake-IP answer (see egress.FakeIPAnswer), from 198.18.0.0/15, 2001:2::/48
// or both and in any order, counts as public, and fakeIP reports it. Proxy
// software in TUN/fake-IP mode answers that way for host names; the
// placeholder says nothing about the real address, and the proxy that
// carries the request, or on the direct route the software's TUN, maps it
// back to the name. An address literal is no DNS answer, and an answer that
// mixes placeholders with any other address stays unsafe. So does a fake-IP
// answer that holds an address of this machine's network interfaces (see
// egress.LocalAddress), such as the TUN address of the proxy software;
// localCause then says why, for the server log only.
func classifyAnswer(network hostNetwork, host string, answer []net.IPAddr, placeholders bool) (scope networkScope, fakeIP bool, localCause error) {
	scope = resolvedScope(answer)
	if !placeholders || !egress.FakeIPAnswer(host, answer) {
		return scope, false, nil
	}
	local, err := egress.LocalAddress(answer, network.InterfaceAddrs)
	switch {
	case err != nil:
		return networkUnsafe, false, fmt.Errorf("%s resolved to fake-IP addresses that Depsilo could not check: %w", host, err)
	case local.IsValid():
		return networkUnsafe, false, fmt.Errorf("%s resolved to %s, an address of this machine's network interfaces", host, local)
	}
	return networkPublic, true, nil
}

// withCause is sentinel, a refusal whose fixed text a 502 body may state,
// with cause added for the server log, or sentinel itself without one.
func withCause(sentinel *refusal, cause error) error {
	if cause == nil {
		return sentinel
	}
	return &refusal{text: sentinel.text, cause: cause}
}

// firstPlaceholder is the placeholder of a fake-IP answer that a refusal
// names: the first one that a direct client dials, an IPv4 one when the
// answer has one.
func firstPlaceholder(answer []net.IPAddr) netip.Addr {
	ip, _ := netip.AddrFromSlice(egress.IPv4First(answer)[0].IP)
	return ip.Unmap()
}

// fakeIPDialError refuses a direct dial to a host name answered only with
// fake-IP placeholders while [egress] allow_fake_ip is off, and names the
// remedy; see fakeIPRefusal.
func fakeIPDialError(host string, addresses []net.IPAddr, remedy string) error {
	return &fakeIPRefusal{host: host, address: firstPlaceholder(addresses), remedy: remedy}
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
	if !egress.DNSHostName(host) {
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

// guardedDialer dials for a registry on the direct route. It resolves each
// host when it dials, refuses unsafe answers, and checks the answer against
// the registry's origin pin. A fake-IP answer follows fakeIP: with [egress]
// allow_fake_ip, it counts as public and is dialed at its placeholders for
// the TUN of proxy software to carry; otherwise it is refused with the
// rule's remedy. A fake-IP answer for the registry's own host is checked
// against the pin before the dial and pins public only after a connection
// succeeded, as a package Upstream's origin pins: when nothing carried the
// placeholders, the registry keeps no scope, and a real DNS answer that the
// refusal's remedy asks for, such as a private one for a registry on the
// local network, is then accepted without a restart.
type guardedDialer struct {
	pin         *originPin
	resolver    hostNetwork
	dialContext func(context.Context, string, string) (net.Conn, error)
	fakeIP      fakeIPRule
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
	targetScope, placeholders, localCause := classifyAnswer(dialer.resolver, host, addresses, dialer.fakeIP.dial)
	if targetScope == networkUnsafe {
		switch {
		case localCause != nil:
			return nil, withCause(errUnsafeDialTarget, localCause)
		case egress.FakeIPAnswer(host, addresses):
			// allow_fake_ip is off. Only the answer that a proxy, or with
			// the setting on a TUN, carries is worth these remedies. Mixed
			// answers and address literals stay refused on every route, so
			// they keep the plain refusal, except unique-local fake-IP,
			// whose refusal names the proxy software's setting.
			return nil, fakeIPDialError(host, addresses, dialer.fakeIP.refusedRemedy())
		}
		if refusal := uniqueLocalFakeIPError(host, addresses); refusal != nil {
			return nil, refusal
		}
		return nil, errUnsafeDialTarget
	}
	if placeholders && sameDialAddress(address, dialer.pin.origin) {
		if !dialer.pin.admits(networkPublic) {
			return nil, errRegistryChangedScope
		}
		connection, err := dialer.dialPlaceholders(ctx, network, host, addresses, port)
		if err != nil {
			return nil, err
		}
		if !dialer.pin.pinScope(networkPublic) {
			_ = connection.Close()
			return nil, errRegistryChangedScope
		}
		return connection, nil
	}
	if err := dialer.pin.admitDial(ctx, address, targetScope); err != nil {
		return nil, err
	}
	if placeholders {
		return dialer.dialPlaceholders(ctx, network, host, addresses, port)
	}
	return dialAddresses(ctx, dialer.dialContext, network, addresses, port)
}

// dialPlaceholders dials the placeholders of host's fake-IP answer through
// upstream.DialFakeIP, as a package Upstream's guarded dialer does, each
// attempt for at most egress.FakeIPConnectTimeout. The TUN of proxy software
// completes such a connection locally and at once, so when no attempt
// connects while ctx is live, nothing carries placeholders on this machine,
// for example because only the system DNS points at the proxy software. The
// refusal then names the host, the placeholder dialed first and the remedy;
// the dial errors reach the server log only.
func (dialer *guardedDialer) dialPlaceholders(ctx context.Context, network, host string, answer []net.IPAddr, port string) (net.Conn, error) {
	connection, err := upstream.DialFakeIP(ctx, dialer.dialContext, network, port, answer, 0)
	switch {
	case err == nil:
		return connection, nil
	case ctx.Err() != nil:
		return nil, fmt.Errorf("dial Docker target: %w", err)
	}
	return nil, &fakeIPNotCarried{
		host:    host,
		address: firstPlaceholder(answer),
		remedy:  dialer.fakeIP.notCarriedRemedy(),
		dialErr: err,
	}
}

// dialAddresses dials the checked addresses of a host, in their order, until
// one connects.
func dialAddresses(
	ctx context.Context,
	dial func(context.Context, string, string) (net.Conn, error),
	network string,
	addresses []net.IPAddr,
	port string,
) (net.Conn, error) {
	var lastErr error
	for _, candidate := range addresses {
		connection, err := dial(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("dial Docker target: %w", lastErr)
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
