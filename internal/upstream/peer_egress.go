package upstream

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"depsilo/internal/cascade"
)

// PeerEgress is one resolved cascade peer a local upstream may egress through.
// URL is the peer's origin and may carry a reverse-proxy path prefix; the relay
// endpoint path is appended when the transport is built.
type PeerEgress struct {
	Name string
	// URL is the peer origin, for example https://cache.example or
	// http://192.168.1.10:23333.
	URL *url.URL
	// Token is the shared secret presented to this peer.
	Token string
	// LocalInstanceID is this node's identity. A request that does not already
	// carry an inbound chain starts one, so a cyclone always becomes visible on
	// the second visit to any node.
	LocalInstanceID string
	// MaxHops bounds how many relay nodes may handle one request.
	MaxHops int
	// ForwardCredentials permits Authorization/Cookie headers to cross the
	// peer. The default keeps upstream credentials local.
	ForwardCredentials bool
}

// PeerSet resolves configured peer names for pool construction and Admin
// validation.
type PeerSet struct {
	peers map[string]PeerEgress
}

// NewPeerSet builds an immutable lookup from resolved configuration.
func NewPeerSet(entries []PeerEgress) *PeerSet {
	set := &PeerSet{peers: make(map[string]PeerEgress, len(entries))}
	for _, entry := range entries {
		set.peers[entry.Name] = entry
	}
	return set
}

// Resolve returns a peer by its configured name.
func (s *PeerSet) Resolve(name string) (PeerEgress, bool) {
	if s == nil {
		return PeerEgress{}, false
	}
	peer, ok := s.peers[name]
	return peer, ok
}

// Names returns the configured peer names in deterministic order.
func (s *PeerSet) Names() []string {
	if s == nil {
		return nil
	}
	names := make([]string, 0, len(s.peers))
	for name := range s.peers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// peerTransport rewrites every request for one upstream into an authenticated
// relay exchange with the configured parent node. The original absolute URL
// travels in a header, so relative artifact paths, signed CDN URLs, and
// protocol redirects all keep their normal meaning.
type peerTransport struct {
	base   http.RoundTripper
	relay  *url.URL
	peer   PeerEgress
	source string
}

func newPeerTransport(base http.RoundTripper, peer PeerEgress, source string) (*peerTransport, error) {
	if base == nil {
		return nil, errors.New("cascade peer transport requires a base transport")
	}
	if peer.URL == nil || peer.URL.Host == "" ||
		(peer.URL.Scheme != "http" && peer.URL.Scheme != "https") {
		return nil, fmt.Errorf("cascade peer %q has an invalid URL", peer.Name)
	}
	if peer.Token == "" {
		return nil, fmt.Errorf("cascade peer %q has no token", peer.Name)
	}
	if !cascade.ValidInstanceID(peer.LocalInstanceID) {
		return nil, fmt.Errorf("cascade peer %q requires a valid local instance ID", peer.Name)
	}
	if peer.MaxHops <= 0 {
		peer.MaxHops = cascade.DefaultMaxHops
	}
	if peer.MaxHops > cascade.MaxChainEntries {
		return nil, fmt.Errorf("cascade peer %q max hops exceeds %d", peer.Name, cascade.MaxChainEntries)
	}
	sourceURL, err := url.Parse(strings.TrimSpace(source))
	if err != nil || sourceURL.Host == "" ||
		(sourceURL.Scheme != "http" && sourceURL.Scheme != "https") {
		return nil, fmt.Errorf("cascade peer %q requires a valid upstream source URL", peer.Name)
	}
	relay := *peer.URL
	relay.Path = strings.TrimRight(relay.Path, "/") + cascade.RelayPath
	relay.RawPath = ""
	relay.RawQuery = ""
	relay.ForceQuery = false
	relay.Fragment = ""
	return &peerTransport{base: base, relay: &relay, peer: peer, source: sourceURL.String()}, nil
}

// RoundTrip implements http.RoundTripper.
func (t *peerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, errors.New("cascade peer: nil request")
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return nil, fmt.Errorf("cascade peer %s: method %s is not supported", t.peer.Name, request.Method)
	}
	target := *request.URL
	if target.User != nil {
		return nil, fmt.Errorf("cascade peer %s: upstream URL must not embed credentials", t.peer.Name)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("cascade peer %s: upstream URL must use http or https", t.peer.Name)
	}
	if len(target.String()) > cascade.MaxTargetURLLength {
		return nil, fmt.Errorf("cascade peer %s: upstream URL exceeds the relay limit", t.peer.Name)
	}

	chain := cascade.ChainFrom(request.Context())
	if len(chain) == 0 {
		chain = cascade.Chain{t.peer.LocalInstanceID}
	}
	if len(chain) > t.peer.MaxHops {
		return nil, fmt.Errorf("cascade peer %s: cascade chain exceeds max_hops %d", t.peer.Name, t.peer.MaxHops)
	}

	outbound := request.Clone(request.Context())
	relay := *t.relay
	outbound.URL = &relay
	outbound.RequestURI = ""
	outbound.Host = ""
	outbound.Header = request.Header.Clone()
	if outbound.Header == nil {
		outbound.Header = make(http.Header)
	}
	stripHopByHopHeaders(outbound.Header)
	if !t.peer.ForwardCredentials {
		outbound.Header.Del("Authorization")
		outbound.Header.Del("Cookie")
		outbound.Header.Del("Proxy-Authorization")
	}
	outbound.Header.Set(cascade.HeaderTarget, target.String())
	outbound.Header.Set(cascade.HeaderSource, t.source)
	outbound.Header.Set(cascade.HeaderToken, t.peer.Token)
	outbound.Header.Set(cascade.HeaderChain, chain.String())
	if ttl, ok := cascade.FetchTTLFrom(request.Context()); ok {
		outbound.Header.Set(cascade.HeaderTTL, cascade.FormatTTLHeader(ttl))
	} else {
		outbound.Header.Del(cascade.HeaderTTL)
	}
	if kind, ok := cascade.FetchKindFrom(request.Context()); ok {
		outbound.Header.Set(cascade.HeaderKind, kind)
	} else {
		outbound.Header.Del(cascade.HeaderKind)
	}

	response, err := t.base.RoundTrip(outbound)
	if err != nil {
		return nil, fmt.Errorf("cascade peer %s: %w", t.peer.Name, err)
	}
	// The base transport reports the wire request (the peer URL). Adapters and
	// the HTTP client's redirect logic reason about the origin URL, so restore
	// the request the caller actually issued.
	response.Request = request
	return response, nil
}

var hopByHopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"TE",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func stripHopByHopHeaders(header http.Header) {
	if header == nil {
		return
	}
	for _, connectionHeader := range header.Values("Connection") {
		for _, name := range strings.Split(connectionHeader, ",") {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				header.Del(trimmed)
			}
		}
	}
	for _, name := range hopByHopHeaders {
		header.Del(name)
	}
}
