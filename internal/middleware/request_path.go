package middleware

import "strings"

const signedArtifactPathMarker = "/files/_external/"

// redactedRequestPath prevents signed external-artifact references from
// entering process logs. Their token authenticates a complete upstream URL,
// which may include short-lived query credentials even though the local
// request itself has no query string.
func redactedRequestPath(requestPath string) string {
	index := strings.Index(requestPath, signedArtifactPathMarker)
	if index < 0 {
		return requestPath
	}
	return requestPath[:index+len(signedArtifactPathMarker)] + "<redacted>"
}
