#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORKFLOW="$ROOT/.github/workflows/release.yml"
DOCKERFILE="$ROOT/Dockerfile"

if [ "$(grep -c '^FROM --platform=\$BUILDPLATFORM .* AS \(frontend\|backend\)$' "$DOCKERFILE")" -ne 2 ] ||
    ! grep -qx 'ARG TARGETOS' "$DOCKERFILE" ||
    ! grep -qx 'ARG TARGETARCH' "$DOCKERFILE" ||
    ! grep -q 'CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build' "$DOCKERFILE"; then
    echo "Dockerfile builders must cross-compile on BUILDPLATFORM" >&2
    exit 1
fi
if grep -q '^FROM --platform=\$BUILDPLATFORM alpine:' "$DOCKERFILE"; then
    echo "Dockerfile runtime stage must remain on TARGETPLATFORM" >&2
    exit 1
fi

if ! sed -n '/^  container_candidate:/,/^  publish:/p' "$WORKFLOW" |
    grep -qx '    timeout-minutes: 45'; then
    echo "container candidate job must have a bounded timeout" >&2
    exit 1
fi
smoke_step=$(sed -n '/- name: Smoke-test image and readiness endpoint/,/- name: Install cosign/p' "$WORKFLOW")
if ! grep -q 'for platform in linux/amd64 linux/arm64; do' <<<"$smoke_step" ||
    ! grep -q 'docker buildx imagetools inspect' <<<"$smoke_step" ||
    ! grep -q '\.manifests\[\]' <<<"$smoke_step" ||
    ! grep -q '\.platform\.os' <<<"$smoke_step" ||
    ! grep -q '\.platform\.architecture' <<<"$smoke_step" ||
    ! grep -q 'docker run --rm --platform "$platform"' <<<"$smoke_step" ||
    ! grep -q '@${platform_digest}" version' <<<"$smoke_step"; then
    echo "release smoke must execute each platform child manifest" >&2
    exit 1
fi
if grep -q '@${IMAGE_DIGEST}" version' <<<"$smoke_step"; then
    echo "release smoke cannot reuse the multi-platform index digest" >&2
    exit 1
fi

if grep -q 'for sbom in depsilo-${RELEASE_TAG}-image\.\*\.json' "$WORKFLOW"; then
    echo "release SBOM loops must not match generated signature bundles" >&2
    exit 1
fi
sign_sbom_step=$(sed -n '/- name: Attest image SBOM and sign SBOM files/,/- name: Verify candidate trust gate/p' "$WORKFLOW")
verify_sbom_step=$(sed -n '/- name: Verify candidate trust gate/,/- name: Upload image SBOMs to release/p' "$WORKFLOW")
for format in cdx spdx; do
    sbom="depsilo-\${RELEASE_TAG}-image.${format}.json"
    if ! grep -q "$sbom" <<<"$sign_sbom_step" || ! grep -q "$sbom" <<<"$verify_sbom_step"; then
        echo "release must sign and verify the explicit $format SBOM" >&2
        exit 1
    fi
done

installers=()
while IFS= read -r installer; do
    installers[${#installers[@]}]=$installer
done < <(
    sed -nE \
        's/.*uses: sigstore\/cosign-installer@([0-9a-f]{40}) # v([0-9]+)\.[0-9]+\.[0-9]+/\1 \2/p' \
        "$WORKFLOW"
)

cosign_releases=()
while IFS= read -r cosign_release; do
    cosign_releases[${#cosign_releases[@]}]=$cosign_release
done < <(
    sed -nE 's/^[[:space:]]+cosign-release: v([0-9]+)\..*/\1/p' "$WORKFLOW"
)

if [ "${#installers[@]}" -ne 3 ]; then
    echo "release workflow must keep all three cosign installers pinned" >&2
    exit 1
fi
if [ "${#installers[@]}" -ne "${#cosign_releases[@]}" ]; then
    echo "each cosign installer must declare a cosign release" >&2
    exit 1
fi

expected_installer=${installers[0]}
for index in "${!installers[@]}"; do
    if [ "${installers[$index]}" != "$expected_installer" ]; then
        echo "release jobs must use the same pinned cosign installer" >&2
        exit 1
    fi

    installer_major=${installers[$index]##* }
    cosign_major=${cosign_releases[$index]}
    if [ "$cosign_major" -ge 3 ] && [ "$installer_major" -lt 4 ]; then
        echo "cosign v${cosign_major} requires cosign-installer v4 or newer" >&2
        exit 1
    fi
done

step_line() {
    local name=$1
    local line
    line=$(grep -n -m1 -- "- name: $name" "$WORKFLOW" | cut -d: -f1) || true
    if [ -z "$line" ]; then
        echo "release workflow is missing step: $name" >&2
        exit 1
    fi
    printf '%s\n' "$line"
}

goreleaser_line=$(step_line "Run GoReleaser")
stage_source_line=$(step_line "Stage source SBOMs")
download_tray_line=$(step_line "Download tray bundles")
if [ "$stage_source_line" -lt "$goreleaser_line" ] || [ "$download_tray_line" -lt "$goreleaser_line" ]; then
    echo "release assets must be staged after GoReleaser validates the clean worktree" >&2
    exit 1
fi

echo "release workflow tests passed"
