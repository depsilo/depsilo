# Release verification and immutable build inputs

Tagged releases do not start publishing assets until the offline verification
workflow and release qualification are green. Qualification exercises all 14
package-manager clients, Docker OCI, pinned ccache and sccache clients, real
MinIO S3 behavior, the long-range v0.9.0 source/Compose upgrade contracts, and
the direct-predecessor v0.9.1 source plus immutable image/state contract.
Releases then stay in draft state until all archives and tray bundles are
available, the Linux archive executes, its checksum set verifies, the candidate
container starts, its database and storage pass `/ready`, and signing and
attestation have completed.

The local equivalents are:

```bash
make verify
make security
make test-ui-production
make test-e2e
make test-docker-docker
make test-compiler-cache-qualified
make test-s3
make test-v090-upgrade
make test-v090-compose-upgrade
make test-v091-upgrade
make release-check
make release-dry-run
```

The network and privileged checks are intentionally outside `make verify` so
ordinary development stays deterministic. They are mandatory in the reusable
workflow when a release tag calls it. One upgrade contract rebuilds the
annotated v0.9.0 tag source with the current CI toolchain. The second pulls
`ghcr.io/depsilo/depsilo@sha256:fbc16cae946eccfdbb115ec6523047702bef5d1e3e15359f9c16dcd6a8e6e56e`,
extracts the exact tagged Compose file, and verifies its real bind layout
against the UID/GID `10001:10001` candidate, including an explicitly confirmed
rotation from a weak legacy JWT secret. Together they cover source-level
compatibility and the immutable artifact users actually ran across the old
bind-layout seam.

The direct-predecessor contract separately pins the peeled v0.9.1 tag to commit
`773b9ad673615d5df6a8281f7cb658e3df84527d`, pins its shipped `compose.yaml`
bytes to SHA-256
`5abaad918604e045a32eacb81a4db14019e6a3c3d33d2f82be61e3bbe1a2a3ae`, and
pulls
`ghcr.io/depsilo/depsilo@sha256:bd3a2aeb8f7f461ed91cd583edc16e8f8958f103ebeb4946c626fd2a0d60b8f6`.
The executable child manifests are separately pinned to
`sha256:c242b5dba39aa891cb49ba815d3232f06a59344682a97c0a6f2841bdeb0dd571`
for Linux amd64 and
`sha256:2bef80088d03255a02b512763196006132e7827688b6400c9cd98286e2ee889a`
for Linux arm64, and the contract verifies that the selected child is a member
of the pinned index before running it.
It first reopens source-created state with the candidate. It then creates real
administrator, JWT, API-token, entitlement, and safe ecosystem-wide Package
Rule state through that immutable published image and its named-volume layout,
before proving the candidate preserves the state and migrates the rule to
schema v3 dialect revision 1. Mutable tags are never used as qualification
inputs.

Container publication is a two-job transaction:

1. The candidate job pushes one unique
   `release-candidate-<run-id>-<attempt>` multi-platform manifest to Docker Hub
   and GHCR. It addresses that candidate by digest for smoke testing, signing,
   SBOM generation, and attestation. It never writes a semver or floating tag.
2. Only after both registry digests and all trust material verify does the
   promotion job attach the formal `X.Y.Z` tags. Stable releases additionally
   move `X.Y`, `X` (except `0.x`), and `latest`. Prereleases never move floating
   tags. The GitHub draft is published last.

Inside the repository-wide promotion lock, the workflow reads authoritative
Git refs from the GitHub API and requires the current release tag to resolve to
the commit SHA that triggered the workflow. Lightweight tags are checked
directly; annotated tags are recursively peeled with cycle detection and an
eight-object depth limit. The identity is checked while planning, immediately
before the first registry-tag mutation, and again before the GitHub draft is
published. Both later checks recompute the floating-tag plan and fail closed if
a newer stable ref changed that decision.

Cosign signatures and attestations bind to the digest, so every formal tag
points at already trusted content from the moment it appears. Promotion checks
both registries before changing anything and refuses to replace an existing
`X.Y.Z` tag with a different digest. A failed promotion can be rerun against the
successful candidate job output; repeated tag creation with the same digest is
idempotent. Candidate tags are deliberately retained as traceable, non-release
references.

The release workflow uses GitHub Actions OIDC with cosign keyless signing. The
signed `checksums.txt` authenticates every GoReleaser archive. The installer,
tray bundles, source SBOMs, and image SBOMs each have their own
`.sigstore.json` bundle. Docker Hub and GHCR image digests are signed directly;
their CycloneDX image SBOM is also attached as an attestation. BuildKit emits
maximal image provenance during the multi-platform build.

## Verify a downloaded archive

Install cosign 3.x, download the archive, `checksums.txt`, and
`checksums.txt.sigstore.json` from the same GitHub release, then run:

```bash
export RELEASE=vX.Y.Z
export OIDC_ISSUER=https://token.actions.githubusercontent.com
export CERT_IDENTITY="https://github.com/depsilo/depsilo/.github/workflows/release.yml@refs/tags/${RELEASE}"

cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity "$CERT_IDENTITY" \
  --certificate-oidc-issuer "$OIDC_ISSUER" \
  checksums.txt

sha256sum --ignore-missing --check checksums.txt
```

Use the same `cosign verify-blob` command with the corresponding bundle to
verify `install.sh`, a tray archive, or an SBOM attachment.

## Verify a container

Resolve the immutable digest first; never verify only a mutable tag:

```bash
export RELEASE=vX.Y.Z
export VERSION=${RELEASE#v}
export IMAGE=ghcr.io/depsilo/depsilo:${VERSION}
export DIGEST=$(docker buildx imagetools inspect "$IMAGE" \
  --format '{{json .Manifest}}' | jq -r .digest)
export OIDC_ISSUER=https://token.actions.githubusercontent.com
export CERT_IDENTITY="https://github.com/depsilo/depsilo/.github/workflows/release.yml@refs/tags/${RELEASE}"

cosign verify \
  --certificate-identity "$CERT_IDENTITY" \
  --certificate-oidc-issuer "$OIDC_ISSUER" \
  "ghcr.io/depsilo/depsilo@${DIGEST}"

cosign verify-attestation \
  --type cyclonedx \
  --certificate-identity "$CERT_IDENTITY" \
  --certificate-oidc-issuer "$OIDC_ISSUER" \
  "ghcr.io/depsilo/depsilo@${DIGEST}"
```

The same digest is published and signed at `depsilo/depsilo` on Docker Hub.

## Maintainer release checklist

Before making or retrying a tagged release:

- Confirm the workflow's `verify`, archive/tray, and `container_candidate` jobs
  are green before expecting any formal image tag.
- Confirm the candidate logs report the same `sha256:` manifest digest for
  Docker Hub and GHCR and show successful signature and CycloneDX-attestation
  verification for both.
- Confirm `publish` promotes that exact digest to `X.Y.Z` in both registries;
  for a stable release, also check `X.Y`, eligible `X`, and `latest`.
- Confirm the post-promotion step resolves every formal tag back to that digest
  and verifies its cosign signature before the GitHub draft becomes public.
- If promotion fails, use **Re-run failed jobs**. Do not re-run all jobs merely
  to repair a partial promotion: the successful candidate job output is the
  immutable retry input.
- Treat an immutable-tag conflict as a release incident. Never delete or move
  the existing `X.Y.Z` tag to make the workflow pass; investigate why the tag
  already names a different digest.

## Pinned GitHub Actions

Workflow dependencies are referenced by immutable commit SHA. The readable tag
is retained as an inline comment in each workflow.

| Action | Tag | Commit |
|---|---:|---|
| `actions/checkout` | `v4.4.0` | `11d5960a326750d5838078e36cf38b85af677262` |
| `actions/setup-go` | `v5.6.0` | `40f1582b2485089dde7abd97c1529aa768e1baff` |
| `actions/setup-node` | `v4.4.0` | `49933ea5288caeca8642d1e84afbd3f7d6820020` |
| `actions/download-artifact` | `v4.3.0` | `d3f86a106a0bac45b974a628896c90dbdf5c8093` |
| `actions/upload-artifact` | `v4.6.2` | `ea165f8d65b6e75b540449e92b4886f43607fa02` |
| `anchore/sbom-action` | `v0.24.0` | `e22c389904149dbc22b58101806040fa8d37a610` |
| `goreleaser/goreleaser-action` | `v7.2.3` | `f06c13b6b1a9625abc9e6e439d9c05a8f2190e94` |
| `softprops/action-gh-release` | `v3.0.2` | `3d0d9888cb7fd7b750713d6e236d1fcb99157228` |
| `docker/setup-qemu-action` | `v3.7.0` | `c7c53464625b32c7a7e944ae62b3e17d2b600130` |
| `docker/setup-buildx-action` | `v3.12.0` | `8d2750c68a42422c14e847fe6c8ac0403b4cbd6f` |
| `docker/login-action` | `v3.7.0` | `c94ce9fb468520275223c153574b00df6fe4bcc9` |
| `docker/metadata-action` | `v5.10.0` | `c299e40c65443455700f0fdfc63efafe5b349051` |
| `docker/build-push-action` | `v5.4.0` | `ca052bb54ab0790a636c9b5f226502c73d547a25` |
| `sigstore/cosign-installer` | `v4.1.2` | `6f9f17788090df1f26f669e9d70d6ae9567deba6` |

The SHAs above were resolved only from each action's official GitHub repository:

```bash
git ls-remote https://github.com/actions/checkout.git \
  'refs/tags/v4' 'refs/tags/v4^{}'
git ls-remote --tags https://github.com/actions/checkout.git | \
  grep 11d5960a326750d5838078e36cf38b85af677262
```

Repeat those commands with the repository and major tag shown in the table when
refreshing a pin. For an annotated tag, use the peeled `^{}` commit.

## Pinned official images

| Stage | Official image tag | Multi-platform digest |
|---|---|---|
| Frontend | `node:22.23.2-alpine3.23` | `sha256:46825fbbd4e996a78b7a2cdc08d75e38a5a505bdab95dcda55605359bf124bc6` |
| Backend | `golang:1.26.7-alpine3.23` | `sha256:b17af760035fc2f338eed92d448a6c67f2d45438844fc6c60678fa5f99e44b57` |
| Runtime | `alpine:3.23.5` | `sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40` |

Resolve replacements from Docker Hub's official-image manifests:

```bash
docker buildx imagetools inspect node:22.23.2-alpine3.23 \
  --format '{{json .Manifest}}'
docker buildx imagetools inspect golang:1.26.7-alpine3.23 \
  --format '{{json .Manifest}}'
docker buildx imagetools inspect alpine:3.23.5 \
  --format '{{json .Manifest}}'
```

Review both the readable tag and returned multi-platform digest in the same
change. Updating only one defeats either reproducibility or patch visibility.
