package docker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type tokenEntry struct {
	Token     string
	ExpiresAt time.Time
}

type AuthManager struct {
	mu     sync.RWMutex
	tokens map[string]*tokenEntry // key: "registryName:scope"
}

func NewAuthManager() *AuthManager {
	return &AuthManager{tokens: make(map[string]*tokenEntry)}
}

// GetToken returns a valid bearer token for the given registry and scope.
// It caches tokens and refreshes them when expired.
func (a *AuthManager) GetToken(client *http.Client, registryURL, registryName, username, password, scope string) (string, error) {
	cacheKey := registryName + ":" + scope

	a.mu.RLock()
	if entry, ok := a.tokens[cacheKey]; ok && time.Now().Before(entry.ExpiresAt) {
		a.mu.RUnlock()
		return entry.Token, nil
	}
	a.mu.RUnlock()

	// Discover token endpoint from /v2/ challenge
	realm, service, err := a.discoverAuth(client, registryURL)
	if err != nil {
		return "", fmt.Errorf("auth discovery failed: %w", err)
	}
	if realm == "" {
		// No auth required (e.g., insecure registry)
		return "", nil
	}

	token, expiresIn, err := a.fetchToken(client, realm, service, scope, username, password)
	if err != nil {
		return "", fmt.Errorf("token fetch failed: %w", err)
	}

	a.mu.Lock()
	a.tokens[cacheKey] = &tokenEntry{
		Token:     token,
		ExpiresAt: time.Now().Add(time.Duration(expiresIn-30) * time.Second), // refresh 30s early
	}
	a.mu.Unlock()

	return token, nil
}

// discoverAuth sends GET /v2/ and parses the WWW-Authenticate header.
func (a *AuthManager) discoverAuth(client *http.Client, registryURL string) (realm, service string, err error) {
	resp, err := client.Get(registryURL + "/v2/")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

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

	realm = ParseAuthParam(challenge, "realm")
	service = ParseAuthParam(challenge, "service")
	return realm, service, nil
}

// fetchToken requests a bearer token from the auth endpoint.
func (a *AuthManager) fetchToken(client *http.Client, realm, service, scope, username, password string) (string, int, error) {
	url := fmt.Sprintf("%s?service=%s&scope=%s", realm, service, scope)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", 0, err
	}
	if username != "" {
		req.SetBasicAuth(username, password)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
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
