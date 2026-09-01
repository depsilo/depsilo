package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
)

type tokenEntry struct {
	Token     string
	ExpiresAt time.Time
}

const maxDockerErrorBodyBytes = 64 << 10

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
			return "", fmt.Errorf("auth discovery failed: %w", err)
		}
		if realm == "" {
			return "", nil
		}
		if err := validateAuthRealm(registryURL, realm); err != nil {
			return "", fmt.Errorf("unsafe bearer realm: %w", err)
		}

		token, expiresIn, err := a.fetchToken(ctx, client, registryURL, realm, service, scope, username, password)
		if err != nil {
			return "", fmt.Errorf("token fetch failed: %w", err)
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

func validateAuthRealm(registryURL, realm string) error {
	origin, originErr := url.Parse(registryURL)
	target, targetErr := url.ParseRequestURI(realm)
	if originErr != nil || targetErr != nil || target == nil || !target.IsAbs() ||
		(target.Scheme != "http" && target.Scheme != "https") || target.Host == "" ||
		target.User != nil || target.Fragment != "" {
		return errors.New("invalid HTTP URL")
	}
	if strings.EqualFold(origin.Scheme, "https") && strings.EqualFold(target.Scheme, "http") {
		return errors.New("HTTPS downgrade")
	}

	originScope, originKnown := literalNetworkScope(origin.Hostname())
	targetScope, targetKnown := literalNetworkScope(target.Hostname())
	if !targetKnown {
		return nil
	}
	switch targetScope {
	case networkUnsafe:
		return errors.New("unsafe network target")
	case networkLoopback:
		if !originKnown || originScope != networkLoopback {
			return errors.New("loopback network target")
		}
	case networkPrivate:
		if !originKnown || (originScope != networkPrivate && originScope != networkLoopback) {
			return errors.New("private network target")
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

func literalNetworkScope(host string) (networkScope, bool) {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return networkLoopback, true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return networkPublic, false
	}
	switch {
	case ip.IsLoopback():
		return networkLoopback, true
	case ip.IsPrivate():
		return networkPrivate, true
	case ip.IsUnspecified(), ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsMulticast(),
		isSpecialPurposeDockerAddress(ip):
		return networkUnsafe, true
	default:
		return networkPublic, true
	}
}

var specialPurposeDockerIPv4Prefixes = []netip.Prefix{
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

var specialPurposeDockerIPv6Prefixes = []netip.Prefix{
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

func isSpecialPurposeDockerAddress(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	address = address.Unmap()
	prefixes := specialPurposeDockerIPv6Prefixes
	if address.Is4() {
		prefixes = specialPurposeDockerIPv4Prefixes
	}
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// discoverAuth sends GET /v2/ and parses the WWW-Authenticate header.
func (a *AuthManager) discoverAuth(ctx context.Context, client *http.Client, registryURL string) (realm, service string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", registryURL+"/v2/", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "docker/27.0.0 depsilo")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDockerErrorBodyBytes+1))

	if resp.StatusCode == http.StatusOK {
		return "", "", nil // no auth needed
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return "", "", fmt.Errorf("unexpected status %d from /v2/", resp.StatusCode)
	}

	challenge := resp.Header.Get("WWW-Authenticate")
	if challenge == "" {
		return "", "", fmt.Errorf("no WWW-Authenticate header")
	}
	scheme, parameters, ok := strings.Cut(strings.TrimSpace(challenge), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", "", fmt.Errorf("unsupported authentication challenge")
	}

	realm = ParseAuthParam(parameters, "realm")
	service = ParseAuthParam(parameters, "service")
	if realm == "" {
		return "", "", fmt.Errorf("Bearer challenge missing realm")
	}
	return realm, service, nil
}

// fetchToken requests a bearer token from the auth endpoint.
func (a *AuthManager) fetchToken(ctx context.Context, client *http.Client, registryURL, realm, service, scope, username, password string) (string, int, error) {
	tokenURL, err := url.ParseRequestURI(realm)
	if err != nil {
		return "", 0, err
	}
	query := tokenURL.Query()
	query.Set("service", service)
	query.Set("scope", scope)
	tokenURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", tokenURL.String(), nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "docker/27.0.0 depsilo")
	if username != "" {
		req.SetBasicAuth(username, password)
	}

	tokenClient := *client
	previousCheck := tokenClient.CheckRedirect
	tokenClient.CheckRedirect = func(redirect *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("token redirect limit exceeded")
		}
		if err := validateAuthRealm(registryURL, redirect.URL.String()); err != nil {
			return errors.New("unsafe token redirect")
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
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, readBoundedErrorBody(resp.Body))
	}

	var tokenResp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	tokenBody, err := io.ReadAll(io.LimitReader(resp.Body, maxDockerErrorBodyBytes+1))
	if err != nil {
		return "", 0, err
	}
	if len(tokenBody) > maxDockerErrorBodyBytes {
		return "", 0, errors.New("token endpoint response exceeds 64 KiB")
	}
	if err := json.Unmarshal(tokenBody, &tokenResp); err != nil {
		return "", 0, err
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

func readBoundedErrorBody(body io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(body, maxDockerErrorBodyBytes+1))
	if len(data) > maxDockerErrorBodyBytes {
		return string(data[:maxDockerErrorBodyBytes]) + "…"
	}
	return string(data)
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
