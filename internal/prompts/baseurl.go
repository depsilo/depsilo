package prompts

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// ValidateBaseURL reports why raw must not be rendered into a prompt as the
// service address, or nil when it may be.
//
// Callers use it for values that arrive from outside the process (a query
// parameter, a command-line flag). The value lands verbatim in text an agent
// reads as instructions, so it has to be an origin and nothing else: http or
// https, a host name or IP literal, an optional port, and at most a trailing
// slash. Anything that could carry prose — whitespace, credentials, a path, a
// query, a fragment — is refused rather than escaped.
func ValidateBaseURL(raw string) error {
	if raw == "" {
		return errors.New("is empty")
	}
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return errors.New("must not contain whitespace or control characters")
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return errors.New("is not a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("must start with http:// or https://")
	}
	if parsed.Opaque != "" || parsed.Host == "" {
		return errors.New("must name a host")
	}
	if parsed.User != nil {
		return errors.New("must not contain credentials")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return errors.New("must not contain a path")
	}
	if strings.Contains(raw, "?") {
		return errors.New("must not contain a query")
	}
	if strings.Contains(raw, "#") {
		return errors.New("must not contain a fragment")
	}
	if !validPromptHost(parsed) {
		return errors.New("must name a plain host name or IP address, with an optional port")
	}
	return nil
}

// validPromptHost accepts ASCII host names, IPv4 literals and bracketed IPv6
// literals. net/url alone would let quotes, brackets and other punctuation
// through in the host.
func validPromptHost(parsed *url.URL) bool {
	if strings.HasSuffix(parsed.Host, ":") {
		return false
	}
	if port := parsed.Port(); port != "" {
		if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
			return false
		}
	}
	host := parsed.Hostname()
	if strings.HasPrefix(parsed.Host, "[") {
		address, err := netip.ParseAddr(host)
		return err == nil && address.Is6() && address.Zone() == ""
	}
	if host == "" {
		return false
	}
	for _, c := range host {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_') {
			return false
		}
	}
	return true
}
