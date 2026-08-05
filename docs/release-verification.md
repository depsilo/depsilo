# Release verification and immutable build inputs

Tagged releases stay in draft state until all archives and tray bundles are
available, the Linux archive executes, its checksum set verifies, the candidate
container starts, its database and storage pass `/ready`, and signing and
attestation have completed.

Container publication is a two-job transaction:

1. The candidate job pushes one unique
   `release-candidate-<run-id>-<attempt>` multi-platform manifest to Docker Hub
   and GHCR. It addresses that candidate by digest for smoke testing, signing,
   SBOM generation, and attestation. It never writes a semver or floating tag.
2. Only after both registry digests and all trust material verify does the
   promotion job attach the formal `X.Y.Z` tags. Stable releases additionally
   move `X.Y`, `X` (except `0.x`), and `latest`. Prereleases never move floating
   tags. The GitHub draft is published last.

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
| `sigstore/cosign-installer` | `v3.9.1` | `398d4b0eeef1380460a10c8013a76f728fb906ac` |

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
| Frontend | `node:22.22.0-alpine3.23` | `sha256:e4bf2a82ad0a4037d28035ae71529873c069b13eb0455466ae0bc13363826e34` |
| Backend | `golang:1.26.5-alpine3.23` | `sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc` |
| Runtime | `alpine:3.23.3` | `sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659` |

Resolve replacements from Docker Hub's official-image manifests:

```bash
docker buildx imagetools inspect node:22.22.0-alpine3.23 \
  --format '{{json .Manifest}}'
docker buildx imagetools inspect golang:1.26.5-alpine3.23 \
  --format '{{json .Manifest}}'
docker buildx imagetools inspect alpine:3.23.3 \
  --format '{{json .Manifest}}'
```

Review both the readable tag and returned multi-platform digest in the same
change. Updating only one defeats either reproducibility or patch visibility.
