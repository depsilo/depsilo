package adapter

import "net/http"

// UpstreamFailureStatus returns the status and error code a package route
// answers when it could not get content from its pool. A member that answered
// 404 or 410 said the package or file does not exist there, and package
// clients read that answer as "not found" (npm, Cargo, Composer, NuGet and pip
// stop resolving or move on to their next source, and the go command falls
// back to its next GOPROXY entry) where a 502 reads as an outage. The
// hand-rolled package routes and TransparentProxy.Serve share this mapping.
// Every other failure, including no selectable member, a connection, TLS or
// response-header failure, 401, 403, 429 and 5xx, is a gateway failure.
//
// It only names the answer: whether a stale cached copy is served in its
// place is decided by cache.Manager, which serves one for any error that does
// not refuse it through cache.StaleFallbackPolicy.
func UpstreamFailureStatus(err error) (status int, code string) {
	switch upstreamStatus(err) {
	case http.StatusNotFound:
		return http.StatusNotFound, "NOT_FOUND"
	case http.StatusGone:
		return http.StatusGone, "GONE"
	default:
		return http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"
	}
}
