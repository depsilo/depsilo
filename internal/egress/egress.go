// Package egress holds the operator's outbound network declarations for
// package upstreams and cascade peers: the special-purpose ranges that are
// never dialed, the shared-address ranges an operator declares as private
// networks, and an optional default HTTP proxy, which Docker registries
// without their own proxy use too.
//
// It also names the ranges that proxy software in TUN/fake-IP mode answers DNS
// queries from. The benchmarking ranges 198.18.0.0/15 and 2001:2::/48
// (FakeIPRange) hold placeholders that only a proxy can resolve; they stay
// special-purpose and are never dialed. The documented unique-local ranges of
// proxy software (UniqueLocalProxyRange) are refused everywhere and never
// count as private networks.
//
// It never relaxes the guard for public origins. A declared range is treated
// as a private network, so a public upstream still cannot redirect into it.
package egress

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// SharedAddressSpace is the RFC 6598 carrier-grade NAT range. Overlay
// networks such as Tailscale, Headscale and NetBird assign node addresses
// from it, which is the only reason an operator may declare part of it
// private.
var SharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

// BenchmarkingRange is RFC 2544's benchmarking range. Local proxies in
// fake-IP mode (Clash, mihomo, Surge, sing-box) answer DNS queries for public
// hosts with addresses from it.
var BenchmarkingRange = netip.MustParsePrefix("198.18.0.0/15")

// BenchmarkingRange6 is RFC 5180's IPv6 benchmarking range. The RFC's text
// gives 2001:0200::/48; verified RFC Errata 1752 corrects it to 2001:2::/48,
// the range IANA registers. Proxy software uses it for IPv6 fake-IP answers:
// Clash Verge Rev fills in 2001:2::/64 in TUN fake-IP mode since 2.5.5,
// sing-box's examples use 2001:2::/48 since 2026-10, and OpenClash suggests
// it.
var BenchmarkingRange6 = netip.MustParsePrefix("2001:2::/48")

// uniqueLocalProxyPrefixes are the unique-local ranges that proxy software
// documents for its IPv6 fake-IP answers and its own TUN, VIF and DNS
// addresses:
//
//   - fdfe:dcba:9876::/48: mihomo's documented fake-ip-range6 example and
//     Clash Verge Rev's DNS-settings template, and the default TUN address of
//     mihomo, sing-box and GUI.for.SingBox.
//   - fd00:6152::/32: Surge's IPv6 fake, VIF and DNS prefix, which cannot be
//     changed.
//   - fc00::/18: the fake range of sing-box's examples before 2026-10, of
//     v2rayN, Hiddify and GUI.for.SingBox.
//
// None is a random RFC 4193 Global ID, and fc00::/8 is not even assigned for
// local use, so a compliant network is not numbered in them.
var uniqueLocalProxyPrefixes = []netip.Prefix{
	netip.MustParsePrefix("fdfe:dcba:9876::/48"),
	netip.MustParsePrefix("fd00:6152::/32"),
	netip.MustParsePrefix("fc00::/18"),
}

// alibabaCloudMetadata is the ECS instance-metadata endpoint inside the
// shared address space. It stays blocked even when its range is declared, so
// declaring 100.64.0.0/10 on Alibaba Cloud cannot expose instance credentials.
var alibabaCloudMetadata = netip.MustParseAddr("100.100.100.200")

var specialPurposeIPv4Prefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	SharedAddressSpace,
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.175.48.0/24"),
	BenchmarkingRange,
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

var specialPurposeIPv6Prefixes = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	BenchmarkingRange6, // inside 2001::/23, so it comes first
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
}

// SpecialPurposePrefix returns the reserved, documentation, translation or
// benchmarking range that contains address.
func SpecialPurposePrefix(address netip.Addr) (netip.Prefix, bool) {
	address = address.Unmap()
	prefixes := specialPurposeIPv6Prefixes
	if address.Is4() {
		prefixes = specialPurposeIPv4Prefixes
	}
	return containingPrefix(prefixes, address)
}

// fakeIPPrefixes are the benchmarking ranges, whose addresses FakeIPRange
// calls placeholders.
var fakeIPPrefixes = []netip.Prefix{BenchmarkingRange, BenchmarkingRange6}

// FakeIPRange returns the benchmarking range, BenchmarkingRange or
// BenchmarkingRange6, that contains address. These are the placeholders that
// proxy software in TUN/fake-IP mode answers public host names with and that
// only a proxy, which resolves the name itself, may carry. They remain
// special-purpose ranges: nothing dials them directly.
func FakeIPRange(address netip.Addr) (netip.Prefix, bool) {
	return containingPrefix(fakeIPPrefixes, address.Unmap())
}

// UniqueLocalProxyRange returns the documented unique-local range of proxy
// software that contains address. These ranges hold IPv6 fake-IP answers and
// the proxy's own TUN, VIF and DNS addresses. Unlike a benchmarking
// placeholder, no proxy may carry such an address, and unlike the rest of
// fc00::/7 it is no private network: a fake answer from these ranges would
// otherwise pin a public origin as private. Address classification refuses it
// before it checks for a private network.
func UniqueLocalProxyRange(address netip.Addr) (netip.Prefix, bool) {
	return containingPrefix(uniqueLocalProxyPrefixes, address.Unmap())
}

func containingPrefix(prefixes []netip.Prefix, address netip.Addr) (netip.Prefix, bool) {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return prefix, true
		}
	}
	return netip.Prefix{}, false
}

// Policy is the validated [egress] configuration. The zero value keeps every
// built-in rule: no default proxy and no declared private ranges.
type Policy struct {
	upstreamProxy string
	noProxy       []noProxyRule
	privateRanges []netip.Prefix
}

// NewPolicy validates [egress] settings. Errors name the offending key.
func NewPolicy(upstreamProxy string, noProxy, privateRanges []string) (Policy, error) {
	var policy Policy
	upstreamProxy = strings.TrimSpace(upstreamProxy)
	if upstreamProxy != "" {
		if err := validateProxyURL(upstreamProxy); err != nil {
			return Policy{}, fmt.Errorf("egress.upstream_proxy: %w", err)
		}
		policy.upstreamProxy = upstreamProxy
	}
	for i, raw := range noProxy {
		rule, err := parseNoProxyRule(raw)
		if err != nil {
			return Policy{}, fmt.Errorf("egress.no_proxy[%d]: %w", i, err)
		}
		policy.noProxy = append(policy.noProxy, rule)
	}
	for i, raw := range privateRanges {
		prefix, err := parsePrivateRange(raw)
		if err != nil {
			return Policy{}, fmt.Errorf("egress.private_ranges[%d]: %w", i, err)
		}
		policy.privateRanges = append(policy.privateRanges, prefix)
	}
	return policy, nil
}

// IsDeclaredPrivate reports whether address lies in a range the operator
// declared as a private network.
func (p Policy) IsDeclaredPrivate(address netip.Addr) bool {
	address = address.Unmap()
	if address == alibabaCloudMetadata {
		return false
	}
	for _, prefix := range p.privateRanges {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// UpstreamProxyFor returns the default proxy for an upstream or Docker
// registry whose configured origin is origin, or "" when it keeps direct
// egress. The choice is made once per upstream or registry: like its own
// proxy, it then carries every request of that upstream, including
// redirects. A Docker registry's client still dials the hosts whose answers
// are private or loopback directly; see internal/adapter/docker.
func (p Policy) UpstreamProxyFor(origin string) string {
	if p.upstreamProxy == "" {
		return ""
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return ""
	}
	address, parseErr := netip.ParseAddr(host)
	literal := parseErr == nil
	if literal && address.Unmap().IsLoopback() {
		return ""
	}
	for _, rule := range p.noProxy {
		if rule.matches(host, address, literal) {
			return ""
		}
	}
	return p.upstreamProxy
}

// HasUpstreamProxy reports whether a default upstream proxy is configured.
func (p Policy) HasUpstreamProxy() bool { return p.upstreamProxy != "" }

func validateProxyURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.Hostname() == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("must be an http:// or https:// proxy URL with a host")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return errors.New("must not contain a path, query, or fragment")
	}
	return nil
}

// noProxyRule follows the common NO_PROXY conventions for upstream hosts: a
// domain matches itself and its subdomains, a leading "." (or "*.") matches
// subdomains only, and IP addresses or CIDR ranges match literal IP hosts.
type noProxyRule struct {
	domain         string
	subdomainsOnly bool
	prefix         netip.Prefix
}

func parseNoProxyRule(raw string) (noProxyRule, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return noProxyRule{}, errors.New("must not be empty")
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return noProxyRule{prefix: prefix.Masked()}, nil
	}
	if address, err := netip.ParseAddr(value); err == nil {
		address = address.Unmap()
		return noProxyRule{prefix: netip.PrefixFrom(address, address.BitLen())}, nil
	}
	rule := noProxyRule{}
	switch {
	case strings.HasPrefix(value, "*."):
		value, rule.subdomainsOnly = value[2:], true
	case strings.HasPrefix(value, "."):
		value, rule.subdomainsOnly = value[1:], true
	}
	value = strings.TrimSuffix(value, ".")
	if !validDomain(value) {
		return noProxyRule{}, fmt.Errorf("%q is not a host name, domain, IP address, or CIDR range", raw)
	}
	rule.domain = value
	return rule, nil
}

func (r noProxyRule) matches(host string, address netip.Addr, literal bool) bool {
	if r.prefix.IsValid() {
		return literal && r.prefix.Contains(address.Unmap())
	}
	if literal {
		return false
	}
	if host == r.domain {
		return !r.subdomainsOnly
	}
	return strings.HasSuffix(host, "."+r.domain)
}

func validDomain(value string) bool {
	if value == "" || len(value) > 253 || net.ParseIP(value) != nil {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

func parsePrivateRange(raw string) (netip.Prefix, error) {
	value := strings.TrimSpace(raw)
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		address, addressErr := netip.ParseAddr(value)
		if addressErr != nil {
			return netip.Prefix{}, fmt.Errorf("%q is not an IPv4 CIDR range or address", raw)
		}
		prefix = netip.PrefixFrom(address, address.BitLen())
	}
	prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()).Masked()
	if !prefix.Addr().Is4() || prefix.Bits() < SharedAddressSpace.Bits() || !SharedAddressSpace.Contains(prefix.Addr()) {
		hint := ""
		if prefix.Addr().Is4() && BenchmarkingRange.Overlaps(prefix) {
			hint = "; fake-IP DNS from a local proxy needs egress.upstream_proxy instead"
		}
		return netip.Prefix{}, fmt.Errorf("%s is not within %s (shared address space)%s", prefix, SharedAddressSpace, hint)
	}
	return prefix, nil
}
