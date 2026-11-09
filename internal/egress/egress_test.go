package egress

import (
	"net/netip"
	"strings"
	"testing"
)

func TestSpecialPurposePrefixNamesTheReservedRange(t *testing.T) {
	for address, want := range map[string]string{
		"198.18.0.65":       "198.18.0.0/15",
		"100.101.102.103":   "100.64.0.0/10",
		"192.0.2.1":         "192.0.2.0/24",
		"::ffff:198.19.1.1": "198.18.0.0/15",
		"2001:db8::1":       "2001:db8::/32",
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
	for raw, wantText := range map[string]string{
		"198.18.0.0/15":  "egress.upstream_proxy instead",
		"198.18.0.65":    "egress.upstream_proxy instead",
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
