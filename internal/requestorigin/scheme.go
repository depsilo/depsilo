// Package requestorigin decides how a client addresses this service, for the
// handlers that write absolute URLs of their own into a response.
package requestorigin

import (
	"net/http"
	"strings"
)

// Scheme returns the URL scheme the client used to reach this service: always
// "http" or "https", so the result can be written into a URL as it is.
//
// A TLS connection to this process is "https". Otherwise the first element of
// X-Forwarded-Proto decides when it is "http" or "https"; chained proxies
// append to the header, so the first element is the one the client's own
// connection produced. Any other value, and a missing header, is "http".
//
// The header is read from every peer. [server] trusted_proxies governs the
// client address only, so the scheme is a hint for building links and never
// evidence that a connection was secure.
func Scheme(request *http.Request) string {
	if request.TLS != nil {
		return "https"
	}
	first, _, _ := strings.Cut(request.Header.Get("X-Forwarded-Proto"), ",")
	if strings.EqualFold(strings.TrimSpace(first), "https") {
		return "https"
	}
	return "http"
}
