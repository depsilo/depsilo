package resolvers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// lastModifiedResolver derives a publish time from an artifact's
// HTTP Last-Modified header. Used for ecosystems whose upstream
// either lacks a clean per-version metadata API (alpine APKINDEX has
// no per-package timestamps) or whose metadata is too expensive to
// fetch for a one-shot lookup (conda repodata is MB-scale).
//
// Accuracy: Last-Modified on an artifact equals the upload time at
// most registries. Even when a CDN intermediary lies and reports its
// own cache-fill time, the value is bounded to "no later than upload
// + cache propagation delay" which for a freshness-window measured
// in days is plenty precise.
type lastModifiedResolver struct {
	client    *http.Client
	ecosystem string
	urlFn     func(pkg, version string) (string, error)
}

func (r *lastModifiedResolver) Lookup(ctx context.Context, pkg, version string) (time.Time, error) {
	url, err := r.urlFn(pkg, version)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: build %s url: %v", ErrUnsupported, r.ecosystem, err)
	}
	lm, err := headLastModified(ctx, r.client, url)
	if err != nil {
		return time.Time{}, err
	}
	t, err := http.ParseTime(lm)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: parse %s Last-Modified %q: %v", ErrUpstreamUnavailable, r.ecosystem, lm, err)
	}
	return t.UTC(), nil
}

// ── per-ecosystem URL builders ─────────────────────────────────────
//
// Each returns the canonical public artifact URL for (pkg, version).
// Mirrors / vendor-internal registries are NOT consulted here — the
// quarantine decision must use the authoritative timestamp.
//
// When the package identifier doesn't carry enough info to construct
// a URL (e.g. an alpine package without an architecture hint), the
// builder returns an error wrapped with ErrUnsupported so the checker
// records "we couldn't determine" rather than misreading silence as
// a publish time.

// mavenArtifactURL: Maven Central layout is
// /maven2/<group-with-slashes>/<artifact>/<version>/<artifact>-<version>.jar
// — pkg comes in as "group:artifact" (the canonical Maven coord).
func mavenArtifactURL(pkg, version string) (string, error) {
	colon := strings.IndexByte(pkg, ':')
	if colon <= 0 || colon == len(pkg)-1 {
		return "", fmt.Errorf("maven pkg must be group:artifact, got %q", pkg)
	}
	group := strings.ReplaceAll(pkg[:colon], ".", "/")
	artifact := pkg[colon+1:]
	return fmt.Sprintf("https://repo.maven.apache.org/maven2/%s/%s/%s/%s-%s.jar",
		group, artifact, version, artifact, version), nil
}

// helmArtifactURL: charts live at <repo>/<name>-<version>.tgz. We
// don't know the operator's chart repo in this layer, so we point at
// the Helm community stable repo (charts.bitnami.com) — a chart whose
// quarantine check fails there can still be approved manually if it
// lives on a private repo. Trade-off documented as "best-effort for
// the common case."
func helmArtifactURL(pkg, version string) (string, error) {
	return fmt.Sprintf("https://charts.bitnami.com/bitnami/%s-%s.tgz",
		safePathSegment(pkg), safePathSegment(version)), nil
}

// condaArtifactURL: conda-forge artifacts at
// /<channel>/<arch>/<pkg>-<version>-<build>.conda. We don't carry
// the build string at this layer (it's downstream of resolution), so
// we use the latest-version metadata endpoint instead. Approximation:
// good enough for quarantine; a more precise resolver would parse
// repodata.json which is 100MB+ for popular channels.
//
// pkg format: "<channel>/<name>" e.g. "conda-forge/numpy".
func condaArtifactURL(pkg, version string) (string, error) {
	slash := strings.IndexByte(pkg, '/')
	if slash <= 0 || slash == len(pkg)-1 {
		// Default to conda-forge for bare names.
		return fmt.Sprintf("https://conda.anaconda.org/conda-forge/noarch/%s-%s-0.tar.bz2",
			safePathSegment(pkg), safePathSegment(version)), nil
	}
	channel := pkg[:slash]
	name := pkg[slash+1:]
	return fmt.Sprintf("https://conda.anaconda.org/%s/noarch/%s-%s-0.tar.bz2",
		safePathSegment(channel), safePathSegment(name), safePathSegment(version)), nil
}

// alpineArtifactURL: APK file naming convention is
// <name>-<version>.apk under /<branch>/<repo>/<arch>/. pkg here
// carries the form "<branch>/<repo>/<arch>/<name>" — e.g.
// "v3.19/main/x86_64/curl". Reasonable cap for what the adapter has
// already parsed before calling us.
func alpineArtifactURL(pkg, version string) (string, error) {
	parts := strings.Split(pkg, "/")
	if len(parts) < 4 {
		return "", fmt.Errorf("alpine pkg must be branch/repo/arch/name, got %q", pkg)
	}
	branch, repo, arch := parts[0], parts[1], parts[2]
	name := parts[3]
	return fmt.Sprintf("https://dl-cdn.alpinelinux.org/alpine/%s/%s/%s/%s-%s.apk",
		safePathSegment(branch), safePathSegment(repo), safePathSegment(arch),
		safePathSegment(name), safePathSegment(version)), nil
}

// dockerArtifactURL: best-effort. Full Docker Hub publishing-time
// retrieval requires a token + a manifest fetch + a blob fetch to
// read the config's `created` field — too much work for a Last-
// Modified approximation. Manifest endpoint returns Last-Modified
// in practice (set by the registry on push). Lives behind the
// authoritated token flow; if the token isn't pre-issued, the call
// 401s and the checker treats it as ErrUpstreamUnavailable (serve).
//
// pkg format: "library/<name>" for official images, "<user>/<name>"
// for user images. version is the tag.
func dockerArtifactURL(pkg, version string) (string, error) {
	if !strings.Contains(pkg, "/") {
		// Bare image name → official library prefix.
		pkg = "library/" + pkg
	}
	return fmt.Sprintf("https://registry-1.docker.io/v2/%s/manifests/%s",
		pkg, safePathSegment(version)), nil
}
