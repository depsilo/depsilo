package credentialurl

import (
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// SecureTransport permits direct TLS, the same-host HTTPS Origin supplied by
// a browser behind TLS termination, or loopback HTTP for local development.
// A non-browser caller without Origin needs direct TLS or a loopback address;
// untrusted forwarding headers are never evidence of a secure connection.
func SecureTransport(request *http.Request) bool {
	if site := request.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	var origin *url.URL
	if raw := request.Header.Get("Origin"); raw != "" {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Host, request.Host) || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(raw, "#") || parsed.Scheme != "http" && parsed.Scheme != "https" {
			return false
		}
		origin = parsed
	}
	if request.TLS != nil || origin != nil && origin.Scheme == "https" {
		return true
	}
	host, err := url.Parse("//" + request.Host)
	if err != nil || host.Host != request.Host || host.User != nil || host.Path != "" || host.RawQuery != "" || host.Fragment != "" {
		return false
	}
	if strings.EqualFold(host.Hostname(), "localhost") {
		return true
	}
	address, err := netip.ParseAddr(host.Hostname())
	return err == nil && !address.Is4In6() && address.IsLoopback()
}
