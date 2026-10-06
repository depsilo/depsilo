package upstream

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// NewRelayClient builds the default egress client used by the cascade relay
// endpoint. Relayed targets are arbitrary absolute URLs supplied by a
// token-bearing peer, so the guarded dialer allows public addresses only: a
// private target must match one of this instance's own configured upstream
// origins, which has its own client and network policy.
//
// Redirects are deliberately not followed. The child node follows them one hop
// at a time through the relay, preserving Hugging Face's explicit redirect
// handling and keeping short-lived signed redirects out of the cache.
func NewRelayClient() *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ForceAttemptHTTP2:     true,
		DialContext: (&relayDialer{
			resolver:    net.DefaultResolver,
			dialContext: dialer.DialContext,
		}).DialContext,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// relayDialer enforces a public-only egress boundary with the same
// DNS-pinning and mixed-answer rejection rules as an upstream's guarded
// dialer.
type relayDialer struct {
	resolver    ipResolver
	dialContext func(context.Context, string, string) (net.Conn, error)
}

func (d *relayDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return nil, errors.New("invalid relay dial target")
	}
	addresses, err := d.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve relay target: %w", err)
	}
	if scope := resolvedNetworkScope(addresses); scope != networkPublic {
		return nil, errors.New("relay target is not a public address")
	}
	inner := &guardedDialer{resolver: d.resolver, dialContext: d.dialContext}
	return inner.dialResolved(ctx, network, port, addresses)
}
