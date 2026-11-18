// Package egress holds the operator's outbound network declarations for
// package upstreams and cascade peers: the special-purpose ranges that are
// never dialed, the shared-address ranges an operator declares as private
// networks, and an optional default HTTP proxy, which Docker registries
// without their own proxy use too.
//
// It also names the ranges that proxy software in TUN/fake-IP mode answers DNS
// queries from. The benchmarking ranges 198.18.0.0/15 and 2001:2::/48
// (FakeIPRange) hold placeholders that the proxy software maps back to host
// names: a proxy that carries a request resolves the name itself, and the
// software's TUN carries a connection to a placeholder. They stay
// special-purpose: an address from them written in a URL is never dialed,
// and a host name answered with them only (FakeIPAnswer) is dialed only while
// [egress] allow_fake_ip is on (Policy.AllowsFakeIP) and the answer holds no
// address of this machine (LocalAddress). The documented unique-local ranges
// of proxy software (UniqueLocalProxyRange) are refused everywhere and never
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
	"time"
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
// proxy software in TUN/fake-IP mode answers public host names with: a proxy
// carries a request for such a name by resolving the name itself, and the
// software's TUN carries a connection to the placeholder. They remain
// special-purpose ranges, so an address literal from them is never dialed;
// see FakeIPAnswer for the DNS answers that may be.
func FakeIPRange(address netip.Addr) (netip.Prefix, bool) {
	return containingPrefix(fakeIPPrefixes, address.Unmap())
}

// FakeIPConnectTimeout bounds each connection attempt that Depsilo itself
// makes to a fake-IP placeholder; a request whose own deadline comes first
// stops waiting then. The TUN of proxy software completes such a connection
// locally and at once, so a placeholder that does not connect within this
// time is carried by nothing, for example when only the system DNS points at
// the proxy software.
const FakeIPConnectTimeout = 5 * time.Second

// DNSHostName reports whether only DNS can resolve host: it is neither
// localhost nor an address in a form that URL parsers and resolvers read as
// one. IPv6 literals contain a colon, and a host whose last label is a
// number, such as 198.18.0.5, 3323068421, 0xc6120005 or 198.18.5, is an IPv4
// address to them.
func DNSHostName(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.ContainsAny(host, ":%[]") {
		return false
	}
	labels := strings.Split(host, ".")
	last := labels[len(labels)-1]
	if hex, ok := strings.CutPrefix(last, "0x"); ok {
		return strings.Trim(hex, "0123456789abcdef") != ""
	}
	return strings.Trim(last, "0123456789") != ""
}

// FakeIPAnswer reports whether answer, the DNS answer for host, is the one
// that proxy software in TUN/fake-IP mode gives for a host name: host is a
// DNS host name (see DNSHostName), and every address of answer is a
// placeholder (see FakeIPRange), from 198.18.0.0/15, 2001:2::/48 or both and
// in any order. Such an answer says nothing about the real address. An
// address literal is no DNS answer, and an answer that mixes placeholders with
// any other address is no fake-IP answer either.
func FakeIPAnswer(host string, answer []net.IPAddr) bool {
	if !DNSHostName(host) || len(answer) == 0 {
		return false
	}
	for _, address := range answer {
		ip, ok := netip.AddrFromSlice(address.IP)
		if !ok {
			return false
		}
		if _, placeholder := FakeIPRange(ip); !placeholder {
			return false
		}
	}
	return true
}

// LocalAddress returns the first address of answer that one of this
// machine's network interfaces holds, as interfaceAddrs lists them
// (net.InterfaceAddrs when nil), or an invalid address when none does. The
// list is read on every call, because a TUN comes and goes with the proxy
// software. A fake-IP answer that holds such an address is no placeholder
// answer: it names this machine, for example the TUN address of the proxy
// software itself (198.18.0.1 for mihomo), and a connection to it could reach
// a service on this machine. Fake-IP pools never hand out the TUN address.
// When the list cannot be read, LocalAddress returns the first address of
// answer with the error, so an answer that cannot be checked counts as local.
func LocalAddress(answer []net.IPAddr, interfaceAddrs func() ([]net.Addr, error)) (netip.Addr, error) {
	if interfaceAddrs == nil {
		interfaceAddrs = net.InterfaceAddrs
	}
	held, err := interfaceAddrs()
	if err != nil {
		var first netip.Addr
		if len(answer) > 0 {
			first, _ = netip.AddrFromSlice(answer[0].IP)
		}
		return first.Unmap(), fmt.Errorf("list the addresses of this machine's network interfaces: %w", err)
	}
	local := make(map[netip.Addr]bool, len(held))
	for _, entry := range held {
		if address, ok := interfaceAddress(entry); ok {
			local[address] = true
		}
	}
	for _, address := range answer {
		ip, ok := netip.AddrFromSlice(address.IP)
		if ok && local[ip.Unmap()] {
			return ip.Unmap(), nil
		}
	}
	return netip.Addr{}, nil
}

// interfaceAddress is the address that an entry of net.InterfaceAddrs holds,
// without its zone: an address with its prefix length, or a bare address.
func interfaceAddress(entry net.Addr) (netip.Addr, bool) {
	var ip net.IP
	switch value := entry.(type) {
	case *net.IPNet:
		ip = value.IP
	case *net.IPAddr:
		ip = value.IP
	default:
		if prefix, err := netip.ParsePrefix(entry.String()); err == nil {
			return prefix.Addr().Unmap().WithZone(""), true
		}
		address, err := netip.ParseAddr(entry.String())
		return address.Unmap().WithZone(""), err == nil
	}
	address, ok := netip.AddrFromSlice(ip)
	return address.Unmap().WithZone(""), ok
}

// IPv4First returns the addresses of answer with the IPv4 ones first, each
// family in the order of answer. Depsilo dials the placeholders of a fake-IP
// answer in this order: the TUN of proxy software carries IPv4 whenever it
// runs, IPv6 only when IPv6 is on for it, and Go's resolver may list either
// family first.
func IPv4First(answer []net.IPAddr) []net.IPAddr {
	ordered := make([]net.IPAddr, 0, len(answer))
	for _, address := range answer {
		if address.IP.To4() != nil {
			ordered = append(ordered, address)
		}
	}
	for _, address := range answer {
		if address.IP.To4() == nil {
			ordered = append(ordered, address)
		}
	}
	return ordered
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

// Policy is the validated [egress] configuration. The zero value is the
// default configuration: no default proxy, no declared private ranges, and
// allow_fake_ip on.
type Policy struct {
	upstreamProxy string
	noProxy       []noProxyRule
	privateRanges []netip.Prefix
	// refuseFakeIP turns [egress] allow_fake_ip off, so that the zero value
	// keeps the default.
	refuseFakeIP bool
}

// PolicyOption sets an [egress] switch of the Policy that NewPolicy builds.
type PolicyOption func(*Policy)

// AllowFakeIP sets [egress] allow_fake_ip, which is on unless an option turns
// it off; see Policy.AllowsFakeIP.
func AllowFakeIP(allowed bool) PolicyOption {
	return func(policy *Policy) { policy.refuseFakeIP = !allowed }
}

// NewPolicy validates [egress] settings. Errors name the offending key.
func NewPolicy(upstreamProxy string, noProxy, privateRanges []string, options ...PolicyOption) (Policy, error) {
	var policy Policy
	for _, option := range options {
		option(&policy)
	}
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

// AllowsFakeIP reports whether [egress] allow_fake_ip is on, which it is by
// default. It concerns only the connections that Depsilo dials itself, for a
// package Upstream, extra index or cascade parent without a proxy and for a
// Docker registry that no proxy carries: a host name with a fake-IP answer
// (see FakeIPAnswer) that holds no address of this machine (see
// LocalAddress) then counts as public and is dialed at its placeholders, as
// other programs on the machine dial them, for the TUN of proxy software to
// carry. Turned off, such an answer is refused before anything is dialed.
// A proxy that carries a request resolves the host name itself, whatever the
// setting.
func (p Policy) AllowsFakeIP() bool { return !p.refuseFakeIP }

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
			// A private declaration would let every public upstream redirect
			// into the local network.
			hint = "; fake-IP answers need no declaration: Depsilo connects to them for the TUN of proxy software " +
				"to carry while egress.allow_fake_ip is on, or set egress.upstream_proxy"
		}
		return netip.Prefix{}, fmt.Errorf("%s is not within %s (shared address space)%s", prefix, SharedAddressSpace, hint)
	}
	return prefix, nil
}
