package config

import "depsilo/internal/egress"

// EgressConfig is [egress]: the operator's outbound network declarations for
// package upstreams, Docker registries, and cascade peers. Changes apply on
// restart.
type EgressConfig struct {
	// UpstreamProxy is the default HTTP(S) proxy for package upstreams that
	// have neither their own proxy nor a cascade parent, and for Docker
	// registries without their own proxy. The cascade relay and vulnerability
	// or blocklist sync keep their own settings.
	UpstreamProxy string `mapstructure:"upstream_proxy"`
	// NoProxy lists upstream or registry hosts, domains, IP addresses, or CIDR
	// ranges that keep direct egress while UpstreamProxy is set.
	NoProxy []string `mapstructure:"no_proxy"`
	// PrivateRanges declares parts of 100.64.0.0/10 private networks, for
	// overlay networks such as Tailscale, Headscale, or NetBird.
	PrivateRanges []string `mapstructure:"private_ranges"`
	// AllowFakeIP lets Depsilo connect, where no proxy carries a request, to
	// a host name whose DNS answer consists of fake-IP placeholders
	// (198.18.0.0/15, 2001:2::/48), for the TUN of proxy software to carry,
	// as other programs on the machine do. It defaults to true; false refuses
	// such answers unless a proxy carries the request. See
	// egress.Policy.AllowsFakeIP.
	AllowFakeIP bool `mapstructure:"allow_fake_ip"`
}

// Policy validates the section into the runtime egress policy.
func (c EgressConfig) Policy() (egress.Policy, error) {
	return egress.NewPolicy(c.UpstreamProxy, c.NoProxy, c.PrivateRanges, egress.AllowFakeIP(c.AllowFakeIP))
}
