package upstream

import (
	"net/http"
	"net/url"
	"strings"
)

// RelayEgress resolves a child-declared upstream source URL to one of this
// instance's configured upstream clients, so a multi-level chain keeps using
// the parent's own egress policy (including its cascade peer).
//
// The target must share the matched upstream's origin. A peer that forges the
// source header therefore cannot borrow a private-origin network policy to
// reach an arbitrary private address.
func (r *Registry) RelayEgress(source, target string) (*http.Client, bool) {
	if r == nil {
		return nil, false
	}
	sourceURL, err := url.Parse(strings.TrimSpace(source))
	if err != nil || sourceURL.Host == "" {
		return nil, false
	}
	targetURL, err := url.Parse(strings.TrimSpace(target))
	if err != nil || targetURL.Host == "" {
		return nil, false
	}
	for _, ecosystem := range r.active {
		pool := r.pools[ecosystem]
		if pool == nil {
			continue
		}
		for _, candidate := range pool.Snapshot() {
			base, err := url.Parse(candidate.URL)
			if err != nil || !sameNormalizedSource(base, sourceURL) {
				continue
			}
			if !sameHTTPOrigin(base, targetURL) {
				continue
			}
			client := *candidate.client
			client.CheckRedirect = func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			}
			return &client, true
		}
	}
	return nil, false
}

func sameNormalizedSource(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Host, right.Host) &&
		strings.TrimRight(left.EscapedPath(), "/") == strings.TrimRight(right.EscapedPath(), "/")
}
