package egress

import (
	"errors"
	"net"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

// answerOf is a DNS answer of the given addresses, in their order.
func answerOf(addresses ...string) []net.IPAddr {
	answer := make([]net.IPAddr, 0, len(addresses))
	for _, address := range addresses {
		answer = append(answer, net.IPAddr{IP: net.ParseIP(address)})
	}
	return answer
}

// interfacesHolding lists the given addresses as this machine's network
// interfaces would, with prefix lengths, as net.InterfaceAddrs does.
func interfacesHolding(prefixes ...string) func() ([]net.Addr, error) {
	return func() ([]net.Addr, error) {
		held := make([]net.Addr, 0, len(prefixes))
		for _, raw := range prefixes {
			ip, network, err := net.ParseCIDR(raw)
			if err != nil {
				return nil, err
			}
			held = append(held, &net.IPNet{IP: ip, Mask: network.Mask})
		}
		return held, nil
	}
}

func TestDNSHostNameRejectsLocalhostAndEveryAddressForm(t *testing.T) {
	for host, want := range map[string]bool{
		"registry.example":  true,
		"Registry.Example.": true,
		"registry":          true,
		"a1.example":        true,
		"cdn.example.0x1g":  true,
		"198.18.0.5":        false,
		"198.18.5":          false,
		"3323068421":        false,
		"0xc6120005":        false,
		"0x":                false,
		"2001:2::5":         false,
		"[2001:2::5]":       false,
		"fe80::1%en0":       false,
		"localhost":         false,
		"api.localhost.":    false,
		"":                  false,
	} {
		if got := DNSHostName(host); got != want {
			t.Errorf("DNSHostName(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestFakeIPAnswerNeedsAHostNameAndPlaceholdersOnly(t *testing.T) {
	for _, test := range []struct {
		name   string
		host   string
		answer []net.IPAddr
		want   bool
	}{
		{name: "IPv4 placeholders", host: "registry.example", answer: answerOf("198.18.0.10", "198.19.255.12"), want: true},
		{name: "IPv6 placeholders", host: "registry.example", answer: answerOf("2001:2::a"), want: true},
		{name: "both families, IPv6 first", host: "registry.example", answer: answerOf("2001:2::a", "198.18.0.10"), want: true},
		{name: "a placeholder and a public address", host: "registry.example", answer: answerOf("198.18.0.10", "93.184.216.34")},
		{name: "a placeholder and a private address", host: "registry.example", answer: answerOf("198.18.0.10", "10.0.0.5")},
		{name: "a placeholder and a unique-local proxy address", host: "registry.example", answer: answerOf("198.18.0.10", "fdfe:dcba:9876::a")},
		{name: "an address literal", host: "198.18.0.10", answer: answerOf("198.18.0.10")},
		{name: "a decimal address", host: "3323068426", answer: answerOf("198.18.0.10")},
		{name: "localhost", host: "localhost", answer: answerOf("198.18.0.10")},
		{name: "an empty answer", host: "registry.example"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := FakeIPAnswer(test.host, test.answer); got != test.want {
				t.Fatalf("FakeIPAnswer(%q, %v) = %v, want %v", test.host, test.answer, got, test.want)
			}
		})
	}
}

// The TUN address of proxy software lies in the fake-IP range, so an answer
// that holds it, or any other address of this machine, is no placeholder
// answer. The list is read on every call.
func TestLocalAddressFindsAnAddressOfThisMachine(t *testing.T) {
	interfaces := interfacesHolding("127.0.0.1/8", "192.168.1.20/24", "198.18.0.1/16", "2001:2::1/126")
	for _, test := range []struct {
		name   string
		answer []net.IPAddr
		want   string
	}{
		{name: "the TUN address", answer: answerOf("198.18.0.10", "198.18.0.1"), want: "198.18.0.1"},
		{name: "an IPv6 TUN address", answer: answerOf("2001:2::1"), want: "2001:2::1"},
		{name: "an address of another interface", answer: answerOf("192.168.1.20"), want: "192.168.1.20"},
		// An address in the TUN's subnet is no address of this machine.
		{name: "placeholders of the pool", answer: answerOf("198.18.0.4", "2001:2::2")},
	} {
		t.Run(test.name, func(t *testing.T) {
			local, err := LocalAddress(test.answer, interfaces)
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if local.IsValid() {
				got = local.String()
			}
			if got != test.want {
				t.Fatalf("LocalAddress(%v) = %q, want %q", test.answer, got, test.want)
			}
		})
	}

	// Entries other than *net.IPNet are read too.
	bare := func() ([]net.Addr, error) { return []net.Addr{&net.IPAddr{IP: net.ParseIP("198.18.0.1")}}, nil }
	if local, err := LocalAddress(answerOf("198.18.0.1"), bare); err != nil || local != netip.MustParseAddr("198.18.0.1") {
		t.Fatalf("LocalAddress with a bare interface address = %v, %v", local, err)
	}

	// An answer that cannot be checked counts as local.
	failing := func() ([]net.Addr, error) { return nil, errors.New("netlink refused") }
	local, err := LocalAddress(answerOf("198.18.0.10", "2001:2::a"), failing)
	if local != netip.MustParseAddr("198.18.0.10") || err == nil || !strings.Contains(err.Error(), "netlink refused") {
		t.Fatalf("LocalAddress with a failing interface list = %v, %v; want the first address and the error", local, err)
	}

	calls := 0
	counting := func() ([]net.Addr, error) { calls++; return nil, nil }
	for range 2 {
		if local, err := LocalAddress(answerOf("198.18.0.10"), counting); err != nil || local.IsValid() {
			t.Fatalf("LocalAddress without interface addresses = %v, %v", local, err)
		}
	}
	if calls != 2 {
		t.Fatalf("interface list read %d times for two answers, want 2", calls)
	}
}

func TestIPv4FirstKeepsEachFamilyInOrder(t *testing.T) {
	got := IPv4First(answerOf("2001:2::a", "198.18.0.10", "2001:2::b", "198.18.0.11"))
	want := answerOf("198.18.0.10", "198.18.0.11", "2001:2::a", "2001:2::b")
	if !slices.EqualFunc(got, want, func(left, right net.IPAddr) bool { return left.IP.Equal(right.IP) }) {
		t.Fatalf("IPv4First = %v, want %v", got, want)
	}
}

func TestPolicyAllowsFakeIPByDefault(t *testing.T) {
	if !(Policy{}).AllowsFakeIP() {
		t.Fatal("the zero policy refuses fake-IP answers, want the default allow_fake_ip on")
	}
	for _, test := range []struct {
		name    string
		options []PolicyOption
		want    bool
	}{
		{name: "no option", want: true},
		{name: "allowed", options: []PolicyOption{AllowFakeIP(true)}, want: true},
		{name: "refused", options: []PolicyOption{AllowFakeIP(false)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy, err := NewPolicy("http://proxy.example:3128", []string{"mirror.lan"}, nil, test.options...)
			if err != nil {
				t.Fatal(err)
			}
			if got := policy.AllowsFakeIP(); got != test.want {
				t.Fatalf("AllowsFakeIP = %v, want %v", got, test.want)
			}
			// The switch leaves the other settings alone.
			if policy.UpstreamProxyFor("https://pypi.org") != "http://proxy.example:3128" || policy.UpstreamProxyFor("http://mirror.lan") != "" {
				t.Fatal("the fake-IP switch changed the default proxy")
			}
		})
	}
}

func TestSpecialPurposePrefixNamesTheReservedRange(t *testing.T) {
	for address, want := range map[string]string{
		"198.18.0.65":       "198.18.0.0/15",
		"100.101.102.103":   "100.64.0.0/10",
		"192.0.2.1":         "192.0.2.0/24",
		"::ffff:198.19.1.1": "198.18.0.0/15",
		"2001:db8::1":       "2001:db8::/32",
		"2001:2::1":         "2001:2::/48", // RFC 5180 benchmarking, inside 2001::/23
		"2001::1":           "2001::/23",
	} {
		prefix, ok := SpecialPurposePrefix(netip.MustParseAddr(address))
		if !ok || prefix.String() != want {
			t.Errorf("SpecialPurposePrefix(%s) = %v, %v; want %s", address, prefix, ok, want)
		}
	}
	for _, address := range []string{"8.8.8.8", "100.63.255.255", "100.128.0.0", "192.168.1.10", "2001:4860:4860::8888"} {
		if prefix, ok := SpecialPurposePrefix(netip.MustParseAddr(address)); ok {
			t.Errorf("SpecialPurposePrefix(%s) = %v, want no reserved range", address, prefix)
		}
	}
}

func TestFakeIPRangeNamesOnlyTheBenchmarkingRanges(t *testing.T) {
	for address, want := range map[string]string{
		"198.18.0.1":        "198.18.0.0/15",
		"198.19.255.254":    "198.18.0.0/15",
		"::ffff:198.18.0.1": "198.18.0.0/15",
		"2001:2::1":         "2001:2::/48",
		"2001:2:0:ffff::1":  "2001:2::/48",
	} {
		prefix, ok := FakeIPRange(netip.MustParseAddr(address))
		if !ok || prefix.String() != want {
			t.Errorf("FakeIPRange(%s) = %v, %v; want %s", address, prefix, ok, want)
		}
	}
	// Unique-local fake-IP ranges are no placeholders: a proxy may not carry
	// them, because they cannot be told from a private network.
	for _, address := range []string{"198.17.255.255", "198.20.0.1", "2001::1", "2001:3::1", "fc00::1", "fdfe:dcba:9876::1"} {
		if prefix, ok := FakeIPRange(netip.MustParseAddr(address)); ok {
			t.Errorf("FakeIPRange(%s) = %v, want no fake-IP range", address, prefix)
		}
	}
}

func TestUniqueLocalProxyRangeNamesOnlyTheDocumentedRanges(t *testing.T) {
	for address, want := range map[string]string{
		"fdfe:dcba:9876::46": "fdfe:dcba:9876::/48", // mihomo's fake-ip-range6 example and TUN default
		"fd00:6152::46":      "fd00:6152::/32",      // Surge
		"fc00::46":           "fc00::/18",           // sing-box examples before 2026-10, v2rayN, Hiddify
		"fc00:3fff::1":       "fc00::/18",
	} {
		prefix, ok := UniqueLocalProxyRange(netip.MustParseAddr(address))
		if !ok || prefix.String() != want {
			t.Errorf("UniqueLocalProxyRange(%s) = %v, %v; want %s", address, prefix, ok, want)
		}
	}
	for _, address := range []string{
		"fc00:4000::1",
		"fc00:f853:ccd:e793::1", // kind's default network
		"fd12:3456:789a::1",
		"fd00::1",
		"2001:db8::1",
	} {
		if prefix, ok := UniqueLocalProxyRange(netip.MustParseAddr(address)); ok {
			t.Errorf("UniqueLocalProxyRange(%s) = %v, want no range of proxy software", address, prefix)
		}
	}
}

func TestPolicyDeclaresOnlySharedAddressSpaceAsPrivate(t *testing.T) {
	policy, err := NewPolicy("", nil, []string{"100.64.0.0/10"})
	if err != nil {
		t.Fatal(err)
	}
	for address, want := range map[string]bool{
		"100.101.102.103":        true,
		"::ffff:100.101.102.103": true,
		"100.100.100.200":        false, // Alibaba Cloud instance metadata stays blocked
		"198.18.0.65":            false,
		"192.168.1.10":           false, // already private without a declaration
		"8.8.8.8":                false,
	} {
		if got := policy.IsDeclaredPrivate(netip.MustParseAddr(address)); got != want {
			t.Errorf("IsDeclaredPrivate(%s) = %v, want %v", address, got, want)
		}
	}
	if (Policy{}).IsDeclaredPrivate(netip.MustParseAddr("100.101.102.103")) {
		t.Fatal("the zero policy declared a shared address private")
	}

	narrow, err := NewPolicy("", nil, []string{"100.101.102.103", " 100.100.0.0/16 "})
	if err != nil {
		t.Fatal(err)
	}
	if !narrow.IsDeclaredPrivate(netip.MustParseAddr("100.101.102.103")) || narrow.IsDeclaredPrivate(netip.MustParseAddr("100.101.102.104")) ||
		!narrow.IsDeclaredPrivate(netip.MustParseAddr("100.100.1.1")) {
		t.Fatal("single-address or narrow declarations were not honored exactly")
	}
}

func TestPolicyRejectsRangesOutsideSharedAddressSpace(t *testing.T) {
	const fakeIPHint = "fake-IP answers need no declaration: Depsilo connects to them for the TUN of proxy software " +
		"to carry while egress.allow_fake_ip is on, or set egress.upstream_proxy"
	for raw, wantText := range map[string]string{
		"198.18.0.0/15":  fakeIPHint,
		"198.18.0.65":    fakeIPHint,
		"100.0.0.0/8":    "not within 100.64.0.0/10",
		"10.0.0.0/8":     "not within 100.64.0.0/10",
		"169.254.0.0/16": "not within 100.64.0.0/10",
		"fd7a::/48":      "not within 100.64.0.0/10",
		"tailnet":        "not an IPv4 CIDR range or address",
		"":               "not an IPv4 CIDR range or address",
	} {
		_, err := NewPolicy("", nil, []string{raw})
		if err == nil || !strings.Contains(err.Error(), "egress.private_ranges[0]") || !strings.Contains(err.Error(), wantText) {
			t.Errorf("private range %q error = %v, want %q", raw, err, wantText)
		}
	}
}

func TestPolicyChoosesDefaultUpstreamProxyPerOrigin(t *testing.T) {
	policy, err := NewPolicy("http://127.0.0.1:7890", []string{"mirror.lan", ".corp.example", "192.168.0.0/16", "10.1.2.3"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for origin, want := range map[string]string{
		"https://pypi.org":                  "http://127.0.0.1:7890",
		"https://registry.npmmirror.com":    "http://127.0.0.1:7890",
		"http://mirror.lan:8080":            "",
		"http://pkg.mirror.lan":             "",
		"http://corp.example":               "http://127.0.0.1:7890", // ".corp.example" matches subdomains only
		"http://pkg.corp.example":           "",
		"http://192.168.1.10:23333":         "",
		"http://10.1.2.3":                   "",
		"http://10.1.2.4":                   "http://127.0.0.1:7890",
		"http://localhost:8080":             "",
		"http://127.0.0.1:8080":             "",
		"http://[::1]:8080":                 "",
		"https://notmirror.lan.example.com": "http://127.0.0.1:7890",
	} {
		if got := policy.UpstreamProxyFor(origin); got != want {
			t.Errorf("UpstreamProxyFor(%s) = %q, want %q", origin, got, want)
		}
	}
	if (Policy{}).UpstreamProxyFor("https://pypi.org") != "" || (Policy{}).HasUpstreamProxy() {
		t.Fatal("the zero policy proxied an upstream")
	}
}

func TestPolicyRejectsMalformedProxySettings(t *testing.T) {
	for _, proxy := range []string{"127.0.0.1:7890", "socks5://127.0.0.1:7891", "http://", "http://127.0.0.1:7890/path", "http://127.0.0.1:7890?x=1"} {
		if _, err := NewPolicy(proxy, nil, nil); err == nil || !strings.Contains(err.Error(), "egress.upstream_proxy") {
			t.Errorf("proxy %q error = %v", proxy, err)
		}
	}
	for _, rule := range []string{"", "http://mirror.lan", "mirror.lan:8080", "*", "bad_host", "-bad.lan"} {
		if _, err := NewPolicy("http://127.0.0.1:7890", []string{rule}, nil); err == nil || !strings.Contains(err.Error(), "egress.no_proxy[0]") {
			t.Errorf("no_proxy %q error = %v", rule, err)
		}
	}
	if _, err := NewPolicy(" https://user:secret@proxy.corp.example:3128/ ", []string{"*.lan"}, nil); err != nil {
		t.Fatalf("valid proxy settings rejected: %v", err)
	}
}
