package upstream

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	pathpkg "path"
	"strings"
	"sync"
	"time"
)

// RequestOptions controls a raw upstream HTTP exchange.
//
// FollowRedirects applies only to RequestURL. Requests against the configured
// origin are always limited to one hop so redirects can be inspected safely
// without forwarding origin credentials to another host.
type RequestOptions struct {
	Method          string
	Header          http.Header
	FollowRedirects bool
	// SuppressHealth lets a multi-hop protocol account for the whole exchange
	// once instead of treating an intermediate redirect as a successful fetch.
	SuppressHealth bool
}

// Request performs one raw HTTP exchange against this upstream's configured
// origin. It returns all HTTP statuses unchanged, never follows redirects, and
// records the exchange in the origin's health statistics. The caller owns and
// must close the returned response body.
func (u *Upstream) Request(ctx context.Context, path string, opts RequestOptions) (*http.Response, error) {
	reqURL, err := originRequestURL(u.URL, path)
	if err != nil {
		return nil, err
	}
	return u.request(ctx, reqURL, opts, false, !opts.SuppressHealth)
}

// RequestOriginURL performs one exchange against an already-resolved absolute
// URL on the configured origin. It exists for protocol adapters that inspect
// redirects themselves: a root-relative Location must not be joined to the
// configured base path a second time.
func (u *Upstream) RequestOriginURL(
	ctx context.Context,
	reqURL string,
	opts RequestOptions,
) (*http.Response, error) {
	if !validHTTPURL(reqURL) {
		return nil, fmt.Errorf("create request for %s: invalid HTTP URL", safeURLOrigin(reqURL))
	}
	target, _ := url.Parse(reqURL)
	origin, originErr := url.Parse(u.URL)
	if originErr != nil || !sameHTTPOrigin(origin, target) ||
		(target.User != nil && !sameURLUserInfo(origin.User, target.User)) {
		return nil, fmt.Errorf("request %s: target escaped configured origin", safeURLOrigin(reqURL))
	}
	if target.User == nil && origin.User != nil {
		target.User = origin.User
		reqURL = target.String()
	}
	return u.request(ctx, reqURL, opts, false, !opts.SuppressHealth)
}

// RequestURL performs a raw HTTP exchange against an absolute external URL
// using this upstream's client, including its configured proxy and transport.
// External exchanges do not affect origin health statistics. The caller owns
// and must close the returned response body.
func (u *Upstream) RequestURL(ctx context.Context, reqURL string, opts RequestOptions) (*http.Response, error) {
	if !validHTTPURL(reqURL) {
		return nil, fmt.Errorf("create request for %s: invalid HTTP URL", safeURLOrigin(reqURL))
	}
	target, _ := url.Parse(reqURL)
	origin, _ := url.Parse(u.URL)
	if err := validateExternalTarget(origin, target, u.directNetworkGuard); err != nil {
		return nil, fmt.Errorf("request %s: external target rejected", safeURLOrigin(reqURL))
	}
	return u.request(ctx, reqURL, opts, opts.FollowRedirects, false)
}

func (u *Upstream) request(
	ctx context.Context,
	reqURL string,
	opts RequestOptions,
	followRedirects bool,
	reportHealth bool,
) (*http.Response, error) {
	method := opts.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: invalid request", safeURLOrigin(reqURL))
	}
	req.Header = opts.Header.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "depsilo/0.1")
	}

	if u.client == nil {
		return nil, fmt.Errorf("request %s: upstream client unavailable", safeURLOrigin(reqURL))
	}
	client := *u.client
	if !followRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else {
		client.CheckRedirect = u.secureRedirectCheck(client.CheckRedirect)
	}
	recovery, err := u.admitExchange()
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", safeURLOrigin(reqURL), err)
	}
	if recovery {
		defer u.finishPassiveRecovery()
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		if reportHealth {
			u.Report(latency, false)
		}
		return nil, fmt.Errorf("request %s: %w", safeURLOrigin(reqURL), redactedTransportError{cause: err})
	}
	if reportHealth {
		u.Report(latency, resp.StatusCode < http.StatusInternalServerError)
	}
	return resp, nil
}

func (u *Upstream) secureRedirectCheck(
	previousCheck func(*http.Request, []*http.Request) error,
) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("external redirect limit exceeded")
		}
		var previousURL *url.URL
		if len(via) > 0 {
			previousURL = via[len(via)-1].URL
		}
		// Go derives Referer from the previous URL, whose query commonly
		// contains a short-lived CDN signature. Never disclose it. Origin
		// credentials may survive a same-origin canonical redirect, but they
		// must never cross into signed artifact storage.
		request.Header.Del("Referer")
		request.Header.Del("Proxy-Authorization")
		if previousURL == nil || !sameHTTPOrigin(previousURL, request.URL) {
			request.Header.Del("Authorization")
			request.Header.Del("Cookie")
		}
		configuredOrigin, _ := url.Parse(u.URL)
		if err := validateExternalTarget(
			configuredOrigin,
			request.URL,
			u.directNetworkGuard,
		); err != nil {
			return errors.New("external redirect target rejected")
		}
		if previousURL != nil &&
			strings.EqualFold(previousURL.Scheme, "https") &&
			strings.EqualFold(request.URL.Scheme, "http") {
			return errors.New("external redirect HTTPS downgrade rejected")
		}
		if previousCheck != nil {
			return previousCheck(request, via)
		}
		return nil
	}
}

func sameHTTPOrigin(left, right *url.URL) bool {
	if left == nil || right == nil ||
		!strings.EqualFold(left.Scheme, right.Scheme) ||
		!strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	return effectiveURLPort(left) == effectiveURLPort(right)
}

func effectiveURLPort(target *url.URL) string {
	if target == nil {
		return ""
	}
	if port := target.Port(); port != "" {
		return port
	}
	switch strings.ToLower(target.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

type networkScope uint8

const (
	networkPublic networkScope = iota
	networkPrivate
	networkLoopback
	networkUnsafe
)

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type guardedDialer struct {
	origin      *url.URL
	resolver    ipResolver
	dialContext func(context.Context, string, string) (net.Conn, error)

	policyMu       sync.Mutex
	originScope    networkScope
	originScopeSet bool
}

// validateExternalTarget prevents a configured public origin from turning an
// artifact redirect into access to this process's private network. Private
// origins may use private CDNs; loopback is permitted only for a loopback
// origin (development/tests). Link-local, unspecified, and multicast targets
// are never valid artifact hosts.
func validateExternalTarget(origin, target *url.URL, directNetworkGuard bool) error {
	if target == nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" ||
		target.User != nil {
		return errors.New("invalid external target")
	}
	if origin != nil &&
		strings.EqualFold(origin.Scheme, "https") &&
		strings.EqualFold(target.Scheme, "http") {
		return errors.New("HTTPS downgrade")
	}

	// This fast preflight deliberately classifies only literals and localhost.
	// Direct clients repeat the policy against the IPs returned by the resolver
	// they actually dial, closing the DNS-check/DNS-use race. A configured HTTP
	// proxy owns hostname resolution, so its egress policy must enforce the same
	// boundary for non-literal targets.
	originScope, originKnown := literalHostNetworkScope(hostname(origin))
	targetScope, targetKnown := literalHostNetworkScope(target.Hostname())
	if !targetKnown {
		return nil
	}
	switch targetScope {
	case networkUnsafe:
		return errors.New("unsafe network target")
	case networkLoopback:
		if originKnown && originScope == networkLoopback {
			return nil
		}
		if !originKnown && directNetworkGuard {
			// The guarded dialer will compare the resolved target with the
			// scope pinned by the first successful configured-origin dial.
			return nil
		}
		if !originKnown || originScope != networkLoopback {
			return errors.New("loopback target")
		}
	case networkPrivate:
		if originKnown && (originScope == networkPrivate || originScope == networkLoopback) {
			return nil
		}
		if !originKnown && directNetworkGuard {
			return nil
		}
		if !originKnown || (originScope != networkPrivate && originScope != networkLoopback) {
			return errors.New("private network target")
		}
	}
	return nil
}

func hostname(target *url.URL) string {
	if target == nil {
		return ""
	}
	return target.Hostname()
}

func literalHostNetworkScope(host string) (networkScope, bool) {
	trimmed := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if trimmed == "" {
		return networkPublic, false
	}
	if trimmed == "localhost" || strings.HasSuffix(trimmed, ".localhost") {
		return networkLoopback, true
	}
	if ip := net.ParseIP(trimmed); ip != nil {
		return ipNetworkScope(ip), true
	}
	return networkPublic, false
}

func (d *guardedDialer) DialContext(
	ctx context.Context,
	network string,
	address string,
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return nil, errors.New("invalid dial target")
	}

	targetAddresses, err := d.resolve(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve dial target: %w", err)
	}
	targetScope := resolvedNetworkScope(targetAddresses)

	exactOrigin := sameDialAddress(address, d.origin)
	if targetScope == networkUnsafe {
		return nil, errors.New("unsafe dial target")
	}
	if exactOrigin {
		if originScope, known := d.pinnedOriginScope(); known && originScope != targetScope {
			return nil, errors.New("configured origin changed network scope")
		}
	} else {
		switch targetScope {
		case networkLoopback:
			originScope, policyErr := d.originScopeForPolicy(ctx)
			if policyErr != nil || originScope != networkLoopback {
				return nil, errors.New("loopback dial target rejected")
			}
		case networkPrivate:
			originScope, policyErr := d.originScopeForPolicy(ctx)
			if policyErr != nil || (originScope != networkPrivate && originScope != networkLoopback) {
				return nil, errors.New("private dial target rejected")
			}
		}
	}

	connection, err := d.dialResolved(ctx, network, port, targetAddresses)
	if err != nil {
		return nil, err
	}
	if exactOrigin && !d.pinOriginScope(targetScope) {
		_ = connection.Close()
		return nil, errors.New("configured origin changed network scope")
	}
	return connection, nil
}

func (d *guardedDialer) pinnedOriginScope() (networkScope, bool) {
	d.policyMu.Lock()
	defer d.policyMu.Unlock()
	return d.originScope, d.originScopeSet
}

func (d *guardedDialer) pinOriginScope(scope networkScope) bool {
	d.policyMu.Lock()
	defer d.policyMu.Unlock()
	if !d.originScopeSet {
		d.originScope = scope
		d.originScopeSet = true
		return true
	}
	return d.originScope == scope
}

func (d *guardedDialer) originScopeForPolicy(ctx context.Context) (networkScope, error) {
	if scope, known := d.pinnedOriginScope(); known {
		return scope, nil
	}
	if d.origin == nil || d.origin.Hostname() == "" {
		return networkUnsafe, errors.New("configured origin unavailable")
	}
	addresses, err := d.resolve(ctx, d.origin.Hostname())
	if err != nil {
		return networkUnsafe, fmt.Errorf("resolve configured origin: %w", err)
	}
	scope := resolvedNetworkScope(addresses)
	if scope == networkUnsafe {
		return networkUnsafe, errors.New("configured origin resolved to unsafe scope")
	}
	if !d.pinOriginScope(scope) {
		return networkUnsafe, errors.New("configured origin changed network scope")
	}
	return scope, nil
}

const happyEyeballsFallbackDelay = 250 * time.Millisecond

type dialAttemptResult struct {
	err error
}

// dialRaceState owns a successful connection until the caller accepts it.
// Keeping the connection out of the buffered result channel is important:
// cancellation may win the receive select after a success notification has
// been queued, and a connection stored in that channel would have no owner
// left to close it.
type dialRaceState struct {
	mu       sync.Mutex
	finished bool
	winner   net.Conn
}

func (s *dialRaceState) offer(connection net.Conn) bool {
	s.mu.Lock()
	accepted := !s.finished && s.winner == nil
	if accepted {
		s.winner = connection
	}
	s.mu.Unlock()
	if !accepted {
		_ = connection.Close()
	}
	return accepted
}

func (s *dialRaceState) takeWinner() net.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return nil
	}
	s.finished = true
	connection := s.winner
	s.winner = nil
	return connection
}

func (s *dialRaceState) abandon() {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.finished = true
	connection := s.winner
	s.winner = nil
	s.mu.Unlock()
	if connection != nil {
		_ = connection.Close()
	}
}

func (d *guardedDialer) dialResolved(
	ctx context.Context,
	network string,
	port string,
	addresses []net.IPAddr,
) (net.Conn, error) {
	ordered := interleaveIPFamilies(addresses)
	if len(ordered) == 0 {
		return nil, errors.New("dial target resolved without usable addresses")
	}

	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan dialAttemptResult, len(ordered))
	state := &dialRaceState{}
	for index, targetAddress := range ordered {
		delay := time.Duration(index) * happyEyeballsFallbackDelay
		literal := net.JoinHostPort(targetAddress.IP.String(), port)
		go func() {
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-raceCtx.Done():
					timer.Stop()
					results <- dialAttemptResult{err: raceCtx.Err()}
					return
				}
			}

			connection, dialErr := d.dialContext(raceCtx, network, literal)
			if dialErr == nil && connection == nil {
				dialErr = errors.New("dialer returned no connection")
			}
			if dialErr == nil && raceCtx.Err() != nil {
				_ = connection.Close()
				dialErr = raceCtx.Err()
				connection = nil
			}
			if dialErr == nil && !state.offer(connection) {
				dialErr = errors.New("another address connected first")
			}
			results <- dialAttemptResult{err: dialErr}
		}()
	}

	return awaitDialAttempts(ctx, results, len(ordered), state)
}

func awaitDialAttempts(
	ctx context.Context,
	results <-chan dialAttemptResult,
	attempts int,
	state *dialRaceState,
) (net.Conn, error) {
	dialErrors := make([]error, 0, attempts)
	for range attempts {
		select {
		case result := <-results:
			if result.err == nil {
				// Prefer a cancellation that was already observable when the
				// success notification was selected. abandon closes the
				// state-owned winner even when both select cases were ready.
				if err := ctx.Err(); err != nil {
					state.abandon()
					return nil, err
				}
				connection := state.takeWinner()
				if connection == nil {
					state.abandon()
					return nil, errors.New("dial succeeded without a winner")
				}
				return connection, nil
			}
			if result.err != nil {
				dialErrors = append(dialErrors, result.err)
			}
		case <-ctx.Done():
			state.abandon()
			return nil, ctx.Err()
		}
	}
	state.abandon()
	return nil, errors.Join(dialErrors...)
}

func interleaveIPFamilies(addresses []net.IPAddr) []net.IPAddr {
	if len(addresses) < 2 {
		return append([]net.IPAddr(nil), addresses...)
	}
	firstIsV4 := addresses[0].IP.To4() != nil
	primary := make([]net.IPAddr, 0, len(addresses))
	secondary := make([]net.IPAddr, 0, len(addresses))
	for _, address := range addresses {
		if address.IP == nil {
			continue
		}
		if (address.IP.To4() != nil) == firstIsV4 {
			primary = append(primary, address)
		} else {
			secondary = append(secondary, address)
		}
	}
	ordered := make([]net.IPAddr, 0, len(primary)+len(secondary))
	for len(primary) > 0 || len(secondary) > 0 {
		if len(primary) > 0 {
			ordered = append(ordered, primary[0])
			primary = primary[1:]
		}
		if len(secondary) > 0 {
			ordered = append(ordered, secondary[0])
			secondary = secondary[1:]
		}
	}
	return ordered
}

func (d *guardedDialer) resolve(ctx context.Context, host string) ([]net.IPAddr, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(host), ".")
	if ip := net.ParseIP(trimmed); ip != nil {
		return []net.IPAddr{{IP: ip}}, nil
	}
	if d.resolver == nil {
		return nil, errors.New("resolver unavailable")
	}
	addresses, err := d.resolver.LookupIPAddr(ctx, trimmed)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, errors.New("hostname resolved without addresses")
	}
	return addresses, nil
}

func sameDialAddress(address string, origin *url.URL) bool {
	if origin == nil {
		return false
	}
	originPort := origin.Port()
	if originPort == "" {
		switch strings.ToLower(origin.Scheme) {
		case "http":
			originPort = "80"
		case "https":
			originPort = "443"
		default:
			return false
		}
	}
	targetHost, targetPort, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	return strings.EqualFold(
		strings.TrimSuffix(targetHost, "."),
		strings.TrimSuffix(origin.Hostname(), "."),
	) && targetPort == originPort
}

func resolvedNetworkScope(addresses []net.IPAddr) networkScope {
	var (
		scope networkScope
		set   bool
	)
	for _, address := range addresses {
		candidate := ipNetworkScope(address.IP)
		if candidate == networkUnsafe {
			return networkUnsafe
		}
		if !set {
			scope = candidate
			set = true
			continue
		}
		// Mixed public/private/loopback answers are rejected instead of letting
		// address ordering decide which security boundary is crossed.
		if candidate != scope {
			return networkUnsafe
		}
	}
	if !set {
		return networkUnsafe
	}
	return scope
}

func ipNetworkScope(ip net.IP) networkScope {
	switch {
	case ip == nil:
		return networkUnsafe
	case ip.IsLoopback():
		return networkLoopback
	case ip.IsPrivate():
		return networkPrivate
	case ip.IsUnspecified(), ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsMulticast(),
		isSpecialPurposeAddress(ip):
		return networkUnsafe
	default:
		return networkPublic
	}
}

var specialPurposeIPv4Prefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

var specialPurposeIPv6Prefixes = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
}

func isSpecialPurposeAddress(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	address = address.Unmap()
	prefixes := specialPurposeIPv6Prefixes
	if address.Is4() {
		prefixes = specialPurposeIPv4Prefixes
	}
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func originRequestURL(base, path string) (string, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", fmt.Errorf("create request for %s: invalid relative URL", safeURLOrigin(base))
	}
	relative, err := url.ParseRequestURI(path)
	baseURL, baseErr := url.Parse(base)
	if err != nil || relative.IsAbs() || relative.Host != "" ||
		!validOriginRequestPath(relative) ||
		baseErr != nil || !validHTTPURL(base) ||
		!validConfiguredOriginPath(baseURL) ||
		baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return "", fmt.Errorf("create request for %s: invalid relative URL", safeURLOrigin(base))
	}
	reqURL := strings.TrimRight(base, "/") + path
	if !validHTTPURL(reqURL) {
		return "", fmt.Errorf("create request for %s: invalid HTTP URL", safeURLOrigin(base))
	}
	finalURL, err := url.Parse(reqURL)
	if err != nil ||
		!strings.EqualFold(finalURL.Scheme, baseURL.Scheme) ||
		!strings.EqualFold(finalURL.Host, baseURL.Host) ||
		!sameURLUserInfo(finalURL.User, baseURL.User) ||
		!urlPathWithinBase(baseURL.Path, finalURL.Path) {
		return "", fmt.Errorf("create request for %s: target escaped configured origin", safeURLOrigin(base))
	}
	return reqURL, nil
}

func validOriginRequestPath(target *url.URL) bool {
	if target == nil || target.Fragment != "" {
		return false
	}
	return validCanonicalOriginPath(target.EscapedPath(), false)
}

func validConfiguredOriginPath(target *url.URL) bool {
	if target == nil {
		return false
	}
	return validCanonicalOriginPath(target.EscapedPath(), true)
}

const maxOriginPathDecodeLayers = 8

// validCanonicalOriginPath validates every decoded representation of a URL
// path. Bounded PathUnescape catches traversal hidden behind multiple layers
// of escaping while retaining encoded slashes in otherwise safe paths. Paths
// that still decode after the bound fail closed so nested percent escapes
// cannot turn validation into work proportional to depth times request size.
func validCanonicalOriginPath(escapedPath string, allowEmpty bool) bool {
	if escapedPath == "" {
		return allowEmpty
	}
	candidate := escapedPath
	for range maxOriginPathDecodeLayers {
		if !validOriginPathValue(candidate) {
			return false
		}
		decoded, err := url.PathUnescape(candidate)
		if err != nil || decoded == candidate {
			return true
		}
		candidate = decoded
	}
	return false
}

func validOriginPathValue(value string) bool {
	if !strings.HasPrefix(value, "/") ||
		strings.Contains(value, `\`) ||
		strings.Contains(value, "//") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func urlPathWithinBase(basePath, targetPath string) bool {
	canonicalBase := pathpkg.Clean("/" + strings.TrimPrefix(basePath, "/"))
	canonicalTarget := pathpkg.Clean("/" + strings.TrimPrefix(targetPath, "/"))
	return canonicalBase == "/" ||
		canonicalTarget == canonicalBase ||
		strings.HasPrefix(canonicalTarget, canonicalBase+"/")
}

func sameURLUserInfo(left, right *url.Userinfo) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.String() == right.String()
}
