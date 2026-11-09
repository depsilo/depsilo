package config

import "depsilo/internal/egress"

// EgressConfig is [egress]: the operator's outbound network declarations for
// package upstreams and cascade peers. Changes apply on restart.
type EgressConfig struct {
	// UpstreamProxy is the default HTTP(S) proxy for package upstreams that
	// have neither their own proxy nor a cascade parent. Docker registries,
	// the cascade relay, and vulnerability or blocklist sync keep their own
	// settings.
	UpstreamProxy string `mapstructure:"upstream_proxy"`
	// NoProxy lists upstream hosts, domains, IP addresses, or CIDR ranges
	// that keep direct egress while UpstreamProxy is set.
	NoProxy []string `mapstructure:"no_proxy"`
	// PrivateRanges declares parts of 100.64.0.0/10 private networks, for
	// overlay networks such as Tailscale, Headscale, or NetBird.
	PrivateRanges []string `mapstructure:"private_ranges"`
}

// Policy validates the section into the runtime egress policy.
func (c EgressConfig) Policy() (egress.Policy, error) {
	return egress.NewPolicy(c.UpstreamProxy, c.NoProxy, c.PrivateRanges)
}
