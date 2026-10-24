// Package identity owns external login transactions and explicit account links.
package identity

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const maxProviderBody = 1 << 20

// Every request made by discovery, key rotation, token exchange and UserInfo
// uses this transport. Metadata cannot redirect bearer tokens to another host.
type providerTransport struct {
	base   http.RoundTripper
	origin string
}

func (t providerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme+"://"+req.URL.Host != t.origin || req.URL.User != nil {
		return nil, errors.New("OIDC endpoint must share issuer origin")
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > maxProviderBody {
		resp.Body.Close()
		return nil, errors.New("OIDC response too large")
	}
	resp.Body = &boundedBody{ReadCloser: resp.Body, remaining: maxProviderBody}
	return resp, nil
}

type boundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errors.New("OIDC response too large")
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}

type providerState struct {
	mu       sync.Mutex
	provider *oidc.Provider
	oauth    *oauth2.Config
}

func (m *Manager) provider(ctx context.Context) (*oidc.Provider, *oauth2.Config, error) {
	m.remote.mu.Lock()
	defer m.remote.mu.Unlock()
	if m.remote.provider != nil {
		return m.remote.provider, m.remote.oauth, nil
	}
	p, err := oidc.NewProvider(m.httpContext(ctx), m.cfg.Issuer)
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	var discovery struct {
		Authorization string `json:"authorization_endpoint"`
		Token         string `json:"token_endpoint"`
		Keys          string `json:"jwks_uri"`
		UserInfo      string `json:"userinfo_endpoint"`
	}
	if p.Claims(&discovery) != nil {
		return nil, nil, ErrUnavailable
	}
	issuer, _ := url.Parse(m.cfg.Issuer)
	for _, endpoint := range []string{discovery.Authorization, discovery.Token, discovery.Keys, discovery.UserInfo} {
		u, e := url.Parse(endpoint)
		if e != nil || u.Scheme != issuer.Scheme || u.Host != issuer.Host || u.User != nil || u.Fragment != "" {
			return nil, nil, ErrUnavailable
		}
	}
	oauth := &oauth2.Config{ClientID: m.cfg.ClientID, ClientSecret: m.cfg.ClientSecret, Endpoint: p.Endpoint(), RedirectURL: m.cfg.RedirectURL, Scopes: []string{oidc.ScopeOpenID, "profile", "email"}}
	m.remote.provider, m.remote.oauth = p, oauth
	return p, oauth, nil
}

func (m *Manager) httpContext(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, m.client)
}

type authenticatedIdentity struct {
	Subject, Name, Role string
	Verified            bool
}

func (m *Manager) authenticate(ctx context.Context, txn transaction, code string) (authenticatedIdentity, error) {
	p, oauth, err := m.provider(ctx)
	if err != nil {
		return authenticatedIdentity{}, err
	}
	token, err := oauth.Exchange(m.httpContext(ctx), code, oauth2.VerifierOption(txn.Verifier))
	if err != nil {
		return authenticatedIdentity{}, ErrUnavailable
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) > 64<<10 {
		return authenticatedIdentity{}, ErrUnavailable
	}
	id, err := p.Verifier(&oidc.Config{ClientID: m.cfg.ClientID}).Verify(m.httpContext(ctx), raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(txn.Nonce)) != 1 || id.Subject == "" || len(id.Subject) > 255 {
		return authenticatedIdentity{}, ErrUnavailable
	}
	var signed map[string]json.RawMessage
	if id.Claims(&signed) != nil {
		return authenticatedIdentity{}, ErrUnavailable
	}
	var authorizedParty string
	if rawParty, present := signed["azp"]; present {
		if json.Unmarshal(rawParty, &authorizedParty) != nil || authorizedParty != m.cfg.ClientID {
			return authenticatedIdentity{}, ErrUnavailable
		}
	} else if len(id.Audience) > 1 {
		return authenticatedIdentity{}, ErrUnavailable
	}
	if id.AccessTokenHash != "" && id.VerifyAccessToken(token.AccessToken) != nil {
		return authenticatedIdentity{}, ErrUnavailable
	}
	// UserInfo confirms the subject and returns current group membership. Its
	// sub MUST match the signed ID token (OIDC Core 5.3.2).
	info, err := p.UserInfo(m.httpContext(ctx), oauth2.StaticTokenSource(token))
	if err != nil || info.Subject != id.Subject {
		return authenticatedIdentity{}, ErrUnavailable
	}
	var claims map[string]json.RawMessage
	if info.Claims(&claims) != nil {
		return authenticatedIdentity{}, ErrUnavailable
	}
	var name string
	_ = json.Unmarshal(claims["name"], &name)
	if len(name) > 255 {
		name = ""
	}
	if strings.TrimSpace(name) == "" {
		name = m.cfg.DisplayName
	}
	authenticated := authenticatedIdentity{Subject: id.Subject, Name: name, Verified: true}
	rawGroups, ok := claims[m.cfg.GroupsClaim]
	if !ok {
		rawGroups = signed[m.cfg.GroupsClaim]
	}
	var groups []string
	if len(rawGroups) == 0 || string(rawGroups) == "null" || json.Unmarshal(rawGroups, &groups) != nil {
		return authenticated, ErrDenied
	}
	role := m.mapRole(groups)
	if role == "" {
		return authenticated, ErrDenied
	}
	authenticated.Role = role
	return authenticated, nil
}

func (m *Manager) mapRole(groups []string) string {
	contains := func(allowed []string) bool {
		for _, g := range groups {
			for _, a := range allowed {
				if g == a {
					return true
				}
			}
		}
		return false
	}
	if contains(m.cfg.AdminGroups) {
		return "admin"
	}
	if contains(m.cfg.ReadonlyGroups) {
		return "readonly"
	}
	return ""
}

func providerClient(issuer string) *http.Client {
	u, _ := url.Parse(issuer)
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = 5 * time.Second
	return &http.Client{Timeout: 10 * time.Second, Transport: providerTransport{base: t, origin: u.Scheme + "://" + u.Host}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("OIDC redirects forbidden") }}
}
