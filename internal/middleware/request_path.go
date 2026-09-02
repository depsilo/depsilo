package middleware

import "strings"

var signedArtifactPathMarkers = [...]string{
	"/files/_external/",
	"/-/__depsilo_tarball_v1/",
}

// redactedRequestPath prevents signed artifact bearer tokens from entering
// process logs. Their encrypted claims may authorize a complete upstream URL,
// including short-lived query credentials, even though the local request
// itself has no query string.
func redactedRequestPath(requestPath string) string {
	redactedAt := -1
	markerLength := 0
	for _, marker := range signedArtifactPathMarkers {
		index := strings.Index(requestPath, marker)
		if index < 0 || redactedAt >= 0 && index >= redactedAt {
			continue
		}
		redactedAt = index
		markerLength = len(marker)
	}
	if redactedAt < 0 {
		return requestPath
	}
	return requestPath[:redactedAt+markerLength] + "<redacted>"
}
