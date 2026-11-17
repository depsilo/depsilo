package docker

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	"depsilo/internal/egress"
)

type tokenEntry struct {
	Token     string
	ExpiresAt time.Time
}

const maxDockerErrorBodyBytes = 64 << 10

// Refusals of a registry's authentication answers and of its Bearer realm.
// Their fixed text may appear in a 502 body.
var (
	errMissingChallenge      = &refusal{text: "no WWW-Authenticate header"}
	errUnsupportedChallenge  = &refusal{text: "unsupported authentication challenge"}
	errChallengeWithoutRealm = &refusal{text: "Bearer challenge missing realm"}
	errInvalidRealmURL       = &refusal{text: "invalid HTTP URL"}
	errRealmDowngrade        = &refusal{text: "HTTPS downgrade"}
	errUnsafeRealmTarget     = &refusal{text: "unsafe network target"}
	errLoopbackRealmTarget   = &refusal{text: "loopback network target"}
	errPrivateRealmTarget    = &refusal{text: "private network target"}
	errTokenRedirectLimit    = &refusal{text: "token redirect limit exceeded"}
	errUnsafeTokenRedirect   = &refusal{text: "unsafe token redirect"}
	errTokenResponseTooLarge = &refusal{text: "token endpoint response exceeds 64 KiB"}
)

type AuthManager struct {
	mu     sync.RWMutex
	tokens map[string]*tokenEntry // key: "registryName:scope"
	sf     singleflight.Group
}

func NewAuthManager() *AuthManager {
	return &AuthManager{tokens: make(map[string]*tokenEntry)}
}

// GetToken returns a valid bearer token for the given registry and scope.
// It caches tokens and refreshes them when expired.
func (a *AuthManager) GetToken(ctx context.Context, client *http.Client, registryURL, registryName, username, password, scope string) (string, error) {
	cacheKey := registryName + ":" + scope

	// Fast path: check cache
	a.mu.RLock()
	if entry, ok := a.tokens[cacheKey]; ok && time.Now().Before(entry.ExpiresAt) {
		a.mu.RUnlock()
		return entry.Token, nil
	}
	a.mu.RUnlock()

	// Deduplicated fetch via singleflight
	resultCh := a.sf.DoChan(cacheKey, func() (interface{}, error) {
		// Double-check after winning the singleflight
		a.mu.RLock()
		if entry, ok := a.tokens[cacheKey]; ok && time.Now().Before(entry.ExpiresAt) {
			a.mu.RUnlock()
			return entry.Token, nil
		}
		a.mu.RUnlock()

		realm, service, err := a.discoverAuth(ctx, client, registryURL)
		if err != nil {
			return "", err
		}
		if realm == "" {
			return "", nil
		}
		if err := validateAuthRealm(registryURL, realm); err != nil {
			// The refused realm is the hop of this failure; an unparsable
			// one has no origin to show.
			realmURL, _ := url.Parse(realm)
			return "", &registryFailure{stage: stageToken, hop: realmURL, cause: &refusal{text: "unsafe bearer realm", cause: err}}
		}

		token, expiresIn, err := a.fetchToken(ctx, client, registryURL, realm, service, scope, username, password)
		if err != nil {
			return "", err
		}

		a.mu.Lock()
		a.tokens[cacheKey] = &tokenEntry{
			Token:     token,
			ExpiresAt: time.Now().Add(time.Duration(expiresIn-30) * time.Second),
		}
		a.mu.Unlock()

		return token, nil
	})
	select {
	case result := <-resultCh:
		if result.Err != nil {
			return "", result.Err
		}
		return result.Val.(string), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// validateAuthRealm checks a Bearer realm, or the target of a registry
// redirect, before anything is sent to it: an absolute http or https URL
// without userinfo, no HTTPS downgrade from the registry URL, and no unsafe
// address written in it. It classifies only hosts that literalNetworkScope
// knows: localhost, .localhost names and addresses, also one written with a
// trailing dot. A private or loopback target among them is refused unless
// the registry URL's own host is known and lets it reach that scope.
//
// One exception: a registry URL with a host name leaves a target written as
// an exact address literal, one that netip.ParseAddr accepts, to the
// transport. Every registry client checks each target's answer against the
// scope that the registry's own answer pinned (the preflight where a proxy
// may carry the request, the guarded dialer on the direct route, and both on
// the default-proxy route), and for such a literal that answer is the
// literal itself, without DNS, so it is also the address that a proxy
// connects to. A registry pinned private may therefore follow a redirect to
// a private address, as package Upstreams on direct egress may, one pinned
// loopback also to a loopback address, and one pinned public to neither.
// localhost, a .localhost name and an address written with a trailing dot
// stay refused by their form: they go to a resolver, Depsilo's or the
// proxy's, which may answer them with any address.
func validateAuthRealm(registryURL, realm string) error {
	origin, originErr := url.Parse(registryURL)
	target, targetErr := url.ParseRequestURI(realm)
	if originErr != nil || targetErr != nil || target == nil || !target.IsAbs() ||
		(target.Scheme != "http" && target.Scheme != "https") || target.Host == "" ||
		target.User != nil || target.Fragment != "" {
		return errInvalidRealmURL
	}
	if strings.EqualFold(origin.Scheme, "https") && strings.EqualFold(target.Scheme, "http") {
		return errRealmDowngrade
	}

	originScope, originKnown := literalNetworkScope(origin.Hostname())
	targetScope, targetKnown := literalNetworkScope(target.Hostname())
	if !targetKnown {
		return nil
	}
	if _, err := netip.ParseAddr(target.Hostname()); err == nil && !originKnown && targetScope != networkUnsafe {
		return nil
	}
	switch targetScope {
	case networkUnsafe:
		return errUnsafeRealmTarget
	case networkLoopback:
		if !originKnown || originScope != networkLoopback {
			return errLoopbackRealmTarget
		}
	case networkPrivate:
		if !originKnown || (originScope != networkPrivate && originScope != networkLoopback) {
			return errPrivateRealmTarget
		}
	}
	return nil
}

type networkScope uint8

const (
	networkPublic networkScope = iota
	networkPrivate
	networkLoopback
	networkUnsafe
)

// literalNetworkScope classifies localhost and an address literal; any other
// host is unknown. It applies the rules of upstream.addrNetworkScope, the
// classifier of package Upstreams: a documented unique-local range of proxy
// software is unsafe although it lies in fc00::/7 (see
// egress.UniqueLocalProxyRange), and so is every range of
// egress.SpecialPurposePrefix. Unlike package Upstreams, Docker registries use
// no declared private ranges.
func literalNetworkScope(host string) (networkScope, bool) {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return networkLoopback, true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return networkPublic, false
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return networkUnsafe, true
	}
	_, proxySoftware := egress.UniqueLocalProxyRange(address)
	_, specialPurpose := egress.SpecialPurposePrefix(address)
	switch {
	case ip.IsLoopback():
		return networkLoopback, true
	case proxySoftware:
		return networkUnsafe, true
	case ip.IsPrivate():
		return networkPrivate, true
	case ip.IsUnspecified(), ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsMulticast(), specialPurpose:
		return networkUnsafe, true
	default:
		return networkPublic, true
	}
}

// discoverAuth sends GET /v2/ and parses the WWW-Authenticate header. Its
// errors are registryFailures of the auth discovery stage.
func (a *AuthManager) discoverAuth(ctx context.Context, client *http.Client, registryURL string) (realm, service string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", registryURL+"/v2/", nil)
	if err != nil {
		return "", "", &registryFailure{stage: stageAuthDiscovery, cause: &refusal{text: errInvalidRegistryURL.text, cause: err}}
	}
	req.Header.Set("User-Agent", "docker/27.0.0 depsilo")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", transportFailure(stageAuthDiscovery, req, resp, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		return "", "", statusFailure(stageAuthDiscovery, req, resp)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDockerErrorBodyBytes+1))
	if resp.StatusCode == http.StatusOK {
		return "", "", nil // no auth needed
	}

	challenge := resp.Header.Get("WWW-Authenticate")
	if challenge == "" {
		return "", "", responseFailure(stageAuthDiscovery, req, resp, errMissingChallenge)
	}
	scheme, parameters, ok := strings.Cut(strings.TrimSpace(challenge), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", "", responseFailure(stageAuthDiscovery, req, resp, errUnsupportedChallenge)
	}

	realm = ParseAuthParam(parameters, "realm")
	service = ParseAuthParam(parameters, "service")
	if realm == "" {
		return "", "", responseFailure(stageAuthDiscovery, req, resp, errChallengeWithoutRealm)
	}
	return realm, service, nil
}

// fetchToken requests a bearer token from the auth endpoint. Its errors are
// registryFailures of the token stage.
func (a *AuthManager) fetchToken(ctx context.Context, client *http.Client, registryURL, realm, service, scope, username, password string) (string, int, error) {
	tokenURL, err := url.ParseRequestURI(realm)
	if err != nil {
		return "", 0, &registryFailure{stage: stageToken, cause: &refusal{text: "unsafe bearer realm", cause: err}}
	}
	query := tokenURL.Query()
	query.Set("service", service)
	query.Set("scope", scope)
	tokenURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", tokenURL.String(), nil)
	if err != nil {
		return "", 0, &registryFailure{stage: stageToken, hop: tokenURL, cause: &refusal{text: "unsafe bearer realm", cause: err}}
	}
	req.Header.Set("User-Agent", "docker/27.0.0 depsilo")
	if username != "" {
		req.SetBasicAuth(username, password)
	}

	tokenClient := *client
	previousCheck := tokenClient.CheckRedirect
	tokenClient.CheckRedirect = func(redirect *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errTokenRedirectLimit
		}
		if err := validateAuthRealm(registryURL, redirect.URL.String()); err != nil {
			return errUnsafeTokenRedirect
		}
		redirect.Header.Del("Referer")
		redirect.Header.Del("Proxy-Authorization")
		if len(via) == 0 || !sameHTTPOrigin(via[len(via)-1].URL, redirect.URL) {
			redirect.Header.Del("Authorization")
			redirect.Header.Del("Cookie")
		}
		if previousCheck != nil {
			return previousCheck(redirect, via)
		}
		return nil
	}
	resp, err := tokenClient.Do(req)
	if err != nil {
		return "", 0, transportFailure(stageToken, req, resp, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, statusFailure(stageToken, req, resp)
	}

	var tokenResp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	tokenBody, err := io.ReadAll(io.LimitReader(resp.Body, maxDockerErrorBodyBytes+1))
	if err != nil {
		return "", 0, responseFailure(stageToken, req, resp, err)
	}
	if len(tokenBody) > maxDockerErrorBodyBytes {
		return "", 0, responseFailure(stageToken, req, resp, errTokenResponseTooLarge)
	}
	if err := json.Unmarshal(tokenBody, &tokenResp); err != nil {
		return "", 0, responseFailure(stageToken, req, resp, &refusal{text: "token endpoint returned invalid JSON", cause: err})
	}

	token := tokenResp.Token
	if token == "" {
		token = tokenResp.AccessToken
	}
	expiresIn := tokenResp.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 300 // default 5 min
	}

	zap.L().Debug("docker auth token acquired", zap.String("service", service), zap.String("scope", scope), zap.Int("expires_in", expiresIn))
	return token, expiresIn, nil
}

func sameHTTPOrigin(left, right *url.URL) bool {
	if left == nil || right == nil || !strings.EqualFold(left.Scheme, right.Scheme) ||
		!strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	return effectivePort(left) == effectivePort(right)
}

func effectivePort(target *url.URL) string {
	if port := target.Port(); port != "" {
		return port
	}
	if strings.EqualFold(target.Scheme, "http") {
		return "80"
	}
	if strings.EqualFold(target.Scheme, "https") {
		return "443"
	}
	return ""
}

// ParseAuthParam extracts a named parameter from a WWW-Authenticate header.
// Example: `Bearer realm="https://auth.docker.io/token",service="registry.docker.io"`
func ParseAuthParam(header, name string) string {
	search := name + "=\""
	idx := strings.Index(header, search)
	if idx < 0 {
		return ""
	}
	start := idx + len(search)
	end := strings.Index(header[start:], "\"")
	if end < 0 {
		return ""
	}
	return header[start : start+end]
}
