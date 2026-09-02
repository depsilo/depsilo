package middleware

import "testing"

func TestRedactedRequestPathHidesSignedArtifactTokens(t *testing.T) {
	tests := map[string]string{
		"/torch/files/_external/opaque-pypi-token":                                   "/torch/files/_external/<redacted>",
		"/npm/fixture/-/__depsilo_tarball_v1/opaque-npm-token/archive.tgz":           "/npm/fixture/-/__depsilo_tarball_v1/<redacted>",
		"/p/acme/npm/@scope/pkg/-/__depsilo_tarball_v1/opaque-npm-token/archive.tgz": "/p/acme/npm/@scope/pkg/-/__depsilo_tarball_v1/<redacted>",
		"/npm/fixture": "/npm/fixture",
	}
	for requestPath, want := range tests {
		if got := redactedRequestPath(requestPath); got != want {
			t.Errorf("redactedRequestPath(%q) = %q, want %q", requestPath, got, want)
		}
	}
}
