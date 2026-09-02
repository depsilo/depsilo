// Package resolvers fetches the upstream publish time for a single
// (ecosystem, package, version) from a canonical public registry API.
// That timestamp is authoritative only for bytes selected from the same
// registry. These resolvers must not govern an arbitrary configured private or
// mirrored Upstream until the composition root binds both to one source
// identity; production thresholds therefore remain zero today.
//
// Calls happen at most once per (ecosystem, package, version) per
// install lifetime — the result is persisted in PackageTimestamp via
// quarantine.Store. Even with 100s of resolver calls during a cold
// cache fill, total network cost is bounded by "every dependency
// the team uses, once."
//
// Go and APT have no resolver. Other implementations remain dormant until the
// source-provenance seam above is complete.
package resolvers

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Resolver fetches a publish time. Implementations are stateless;
// concurrent calls are safe.
type Resolver interface {
	Lookup(ctx context.Context, pkg, version string) (time.Time, error)
}

// Sentinel errors that callers (notably quarantine.Lookup +
// quarantine.Check) inspect to decide whether to fail-closed or
// fail-open. errors.Is is the canonical comparison — implementations
// MUST wrap these with %w when adding context.
var (
	// ErrNotFound means the upstream registry returned 404 for the
	// (package, version) pair — the version genuinely does not exist
	// upstream. Quarantine treats this the same as a missing
	// timestamp: if FailClosed, block; otherwise serve and let the
	// downstream registry call surface the real 404 to the client.
	ErrNotFound = errors.New("resolver: package or version not found upstream")

	// ErrUnsupported means we don't have a resolver for this
	// ecosystem yet. Quarantine treats it identically to ErrNotFound
	// — the checker decides what to do based on FailClosed. Distinct
	// error type so the audit event can be honest about which
	// branch was hit.
	ErrUnsupported = errors.New("resolver: ecosystem not implemented")

	// ErrUpstreamUnavailable means the registry call failed for
	// transient reasons (network error, 5xx, parse failure, timeout).
	// Different from ErrNotFound because the right policy is
	// different: a transient error should usually not block a
	// build, even under FailClosed, because the operator can't
	// distinguish "package is malicious" from "registry is having
	// a bad day." The checker logs and serves anyway. (Documented
	// in checker.go.)
	ErrUpstreamUnavailable = errors.New("resolver: upstream registry unavailable")
)

// Registry maps ecosystem name → Resolver. Built once during server
// startup via NewRegistry and held in quarantine.Lookup. Ecosystem
// names match internal/adapter directory names ("pypi", "npm",
// "cargo", ...).
//
// Resolvers absent from the map cause Lookup to return ErrUnsupported. The
// implementations remain available for tests and future source-bound
// composition, but production minimum-release-age thresholds stay at zero:
// these resolvers query fixed public registries and therefore cannot yet
// govern artifacts selected from arbitrary configured Upstreams.
type Registry map[string]Resolver

// NewRegistry wires up one Resolver per ecosystem with a shared
// *http.Client. The client carries a depsilo User-Agent so
// registries can identify us (npm in particular has been known to
// rate-limit unknown User-Agents) and a generous-but-bounded timeout
// so a hung upstream can't pin a goroutine forever.
//
// The User-Agent identifies "Depsilo/<version>" honestly — per
// docs/adr/0003 the tool is a self-hosted supply-chain enforcement layer
// and that posture only works if we don't try to look like something
// else when we phone home for metadata.
func NewRegistry() Registry {
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	r := Registry{
		"npm":         &npmResolver{client: httpClient, base: defaultNpmBase},
		"pypi":        &pypiResolver{client: httpClient, base: defaultPypiBase},
		"cargo":       &cargoResolver{client: httpClient, base: defaultCargoBase},
		"rubygems":    &rubygemsResolver{client: httpClient, base: defaultRubygemsBase},
		"composer":    &composerResolver{client: httpClient, base: defaultComposerBase},
		"nuget":       &nugetResolver{client: httpClient, base: defaultNugetBase},
		"huggingface": &hfResolver{client: httpClient, base: defaultHFBase},
		"cran":        &cranResolver{client: httpClient, base: defaultCranBase},

		// For ecosystems where the upstream lacks a clean version-level
		// metadata API (or the metadata is megabytes of repodata), we
		// approximate publish time via the artifact URL's HTTP
		// Last-Modified header. Not as precise as a true API timestamp
		// but it captures the upload time and is fine for quarantine
		// windows measured in days.
		"maven":  &lastModifiedResolver{client: httpClient, ecosystem: "maven", urlFn: mavenArtifactURL},
		"helm":   &lastModifiedResolver{client: httpClient, ecosystem: "helm", urlFn: helmArtifactURL},
		"conda":  &lastModifiedResolver{client: httpClient, ecosystem: "conda", urlFn: condaArtifactURL},
		"alpine": &lastModifiedResolver{client: httpClient, ecosystem: "alpine", urlFn: alpineArtifactURL},

		// Docker needs a token + manifest dance. Stub for now via
		// Last-Modified on the manifest URL; full implementation is
		// a follow-up if the approximation proves insufficient.
		"docker": &lastModifiedResolver{client: httpClient, ecosystem: "docker", urlFn: dockerArtifactURL},
	}
	return r
}
