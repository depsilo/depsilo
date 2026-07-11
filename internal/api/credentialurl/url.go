package credentialurl

import "net/url"

const redacted = "***"

// PublicOrigin returns only the network origin, dropping every URL component
// that can carry credentials.
func PublicOrigin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return redacted
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String()
}

// Mask returns a fail-closed URL suitable for readers without credential
// access. Paths, queries, and fragments are opaque because all can contain
// registry or webhook tokens.
func Mask(raw string) string {
	origin := PublicOrigin(raw)
	if origin == redacted {
		return redacted
	}
	return origin + "/***"
}
