#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORKFLOW="$ROOT/.github/workflows/release.yml"

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
