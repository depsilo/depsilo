#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORKFLOW="$ROOT/.github/workflows/release.yml"
REAL_CLIENT_WORKFLOW="$ROOT/.github/workflows/real-client-e2e.yml"
VERIFY_WORKFLOW="$ROOT/.github/workflows/verify.yml"
DOCKERFILE="$ROOT/Dockerfile"

workflow_concurrency=$(sed -n '/^concurrency:/,/^jobs:/p' "$WORKFLOW")
if ! grep -Fqx '  group: release-${{ github.ref }}' <<<"$workflow_concurrency" ||
    ! grep -Fqx '  queue: max' <<<"$workflow_concurrency" ||
    ! grep -Fqx '  cancel-in-progress: false' <<<"$workflow_concurrency"; then
    echo "same-tag release runs must queue without replacing an in-progress or pending run" >&2
    exit 1
fi

if ! grep -Fqx 'FROM --platform=$BUILDPLATFORM node:22.23.2-alpine3.23@sha256:46825fbbd4e996a78b7a2cdc08d75e38a5a505bdab95dcda55605359bf124bc6 AS frontend' "$DOCKERFILE"; then
    echo "Docker frontend must use the reviewed Node 22.23.2 multi-platform image" >&2
    exit 1
fi
if ! grep -Fqx 'FROM alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40' "$DOCKERFILE"; then
    echo "Docker runtime must use the reviewed Alpine 3.23.5 multi-platform image" >&2
    exit 1
fi
if ! grep -Fqx 'USER 10001:10001' "$DOCKERFILE"; then
    echo "Docker runtime must use the fixed non-root uid/gid 10001" >&2
    exit 1
fi
if grep -Eq 'chown .*10001:10001 /root([[:space:]\\]|$)' "$DOCKERFILE" ||
    ! grep -Fq 'chmod 0710 /root' "$DOCKERFILE"; then
    echo "Docker runtime must expose only the state directories through the root home" >&2
    exit 1
fi
if grep -R -q "node-version: '22.22.0'" "$ROOT/.github/workflows"; then
    echo "release workflows still use the vulnerable Node 22.22.0 runtime" >&2
    exit 1
fi

if ! grep -q '^  workflow_call:' "$REAL_CLIENT_WORKFLOW" ||
    ! grep -q 'release_qualification:' "$REAL_CLIENT_WORKFLOW"; then
    echo "real-client workflow must expose release qualification through workflow_call" >&2
    exit 1
fi
client_contracts_job=$(sed -n '/^  client-contracts:/,/^  [a-zA-Z0-9_-]*:/p' "$WORKFLOW")
if ! grep -q 'uses: ./.github/workflows/real-client-e2e.yml' <<<"$client_contracts_job"; then
    echo "release workflow must call the real-client qualification workflow" >&2
    exit 1
fi
windows_cli_job=$(sed -n '/^  windows-cli:/,$p' "$VERIFY_WORKFLOW")
if ! grep -Fqx '    runs-on: windows-latest' <<<"$windows_cli_job" ||
    ! grep -q 'TestDaemonUsesServePortAndWritesPrivateStartupLog' <<<"$windows_cli_job" ||
    ! grep -q 'TestDaemonShutdownContextReceivesNamedEvent' <<<"$windows_cli_job" ||
    ! grep -q 'GOARCH.*arm64' <<<"$windows_cli_job"; then
    echo "verify workflow must run the detached named-event lifecycle on Windows and cross-build ARM64" >&2
    exit 1
fi
release_job=$(sed -n '/^  release:/,/^  [a-zA-Z0-9_-]*:/p' "$WORKFLOW")
if ! grep -q 'needs:.*client-contracts' <<<"$release_job"; then
    echo "release assets must wait for real-client qualification" >&2
    exit 1
fi
if ! grep -q -- '- name: Extract release notes' <<<"$release_job" ||
    ! grep -Fq 'bash scripts/extract-release-notes.sh "$GITHUB_REF_NAME" >"$RUNNER_TEMP/release-notes.md"' <<<"$release_job" ||
    ! grep -Fq -- '--release-notes ${{ runner.temp }}/release-notes.md' <<<"$release_job"; then
    echo "GoReleaser must publish the version-matched CHANGELOG section" >&2
    exit 1
fi
release_notes_line=$(grep -n -m1 -- '- name: Extract release notes' <<<"$release_job" | cut -d: -f1)
goreleaser_line=$(grep -n -m1 -- '- name: Run GoReleaser' <<<"$release_job" | cut -d: -f1)
if [ "$release_notes_line" -ge "$goreleaser_line" ]; then
    echo "release notes must be extracted before GoReleaser creates the draft" >&2
    exit 1
fi
tray_macos_job=$(sed -n '/^  tray-macos:/,/^  [a-zA-Z0-9_-]*:/p' "$WORKFLOW")
if ! grep -q 'TestDaemonUsesServePortAndWritesPrivateStartupLog' <<<"$tray_macos_job"; then
    echo "release qualification must run the detached daemon lifecycle natively on macOS" >&2
    exit 1
fi
for command in 'make test-e2e' 'make test-docker-docker' 'make test-compiler-cache-qualified' 'make test-s3' 'make test-v090-upgrade' 'make test-v090-compose-upgrade'; do
    if ! grep -q "$command" "$REAL_CLIENT_WORKFLOW"; then
        echo "release qualification is missing: $command" >&2
        exit 1
    fi
done

real_clients_job=$(sed -n '/^  real-clients:/,/^  docker-registry:/p' "$REAL_CLIENT_WORKFLOW")
upload_log_step=$(sed -n '/- name: Upload server log/,/- name: Clean up/p' <<<"$real_clients_job")
if ! grep -Fq 'path: .test-e2e-state/.dev.log' <<<"$upload_log_step"; then
    echo "real-client failures must upload the isolated E2E server log" >&2
    exit 1
fi
if ! grep -Fq 'include-hidden-files: true' <<<"$upload_log_step"; then
    echo "real-client log upload must include its hidden state path" >&2
    exit 1
fi
upload_log_line=$(grep -n -m1 -- '- name: Upload server log' <<<"$real_clients_job" | cut -d: -f1)
cleanup_line=$(grep -n -m1 -- '- name: Clean up' <<<"$real_clients_job" | cut -d: -f1)
if [ "$upload_log_line" -ge "$cleanup_line" ]; then
    echo "real-client server log must be uploaded before cleanup" >&2
    exit 1
fi
docker_registry_job=$(sed -n '/^  docker-registry:/,/^  compiler-cache:/p' "$REAL_CLIENT_WORKFLOW")
for service_job in "$real_clients_job" "$docker_registry_job"; do
    if ! grep -Fqx '        run: make test-clean' <<<"$service_job" || grep -Fq 'make stop' <<<"$service_job"; then
        echo "real-client cleanup must use only the isolated test-clean target" >&2
        exit 1
    fi
done

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

container_candidate_job=$(sed -n '/^  container_candidate:/,/^  publish:/p' "$WORKFLOW")
if ! grep -qx '    timeout-minutes: 45' <<<"$container_candidate_job"; then
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
if ! grep -q -- '--volume "$state_volume:/root/.depsilo"' <<<"$smoke_step" ||
    ! grep -q 'Bootstrap token:' <<<"$smoke_step" ||
    ! grep -q '/root/.depsilo/data/depsilo.db' <<<"$smoke_step" ||
    ! grep -q '/root/.depsilo/data/cache' <<<"$smoke_step"; then
    echo "release smoke must cover zero-config startup and the single state volume" >&2
    exit 1
fi
if ! grep -q "Config.User.*10001:10001" <<<"$smoke_step" ||
    ! grep -q 'docker exec "$container_id" id -u' <<<"$smoke_step" ||
    ! grep -q 'docker exec "$container_id" id -g' <<<"$smoke_step" ||
    ! grep -q '/proc/1/status' <<<"$smoke_step" ||
    ! grep -q "stat -c '%u:%g' /root/.depsilo" <<<"$smoke_step"; then
    echo "release smoke must prove the image and running CLI use uid/gid 10001" >&2
    exit 1
fi
if ! grep -q -- '--entrypoint /bin/chown' <<<"$smoke_step" ||
    ! grep -q -- '-R 0:0 /root/.depsilo' <<<"$smoke_step" ||
    ! grep -q -- '-R 10001:10001 /root/.depsilo' <<<"$smoke_step" ||
    ! grep -q '.v090-volume-marker' <<<"$smoke_step"; then
    echo "release smoke must migrate and reopen a root-owned v0.9 state volume" >&2
    exit 1
fi
if ! grep -q '/api/v1/setup/complete' <<<"$smoke_step" ||
    ! grep -q 'depsilo backup' <<<"$smoke_step" ||
    ! grep -q 'docker cp' <<<"$smoke_step" ||
    ! grep -q -- '--user "$(id -u):$(id -g)"' <<<"$smoke_step" ||
    ! grep -q 'restore /state/depsilo-release-backup.tar.gz' <<<"$smoke_step" ||
    ! grep -q '/api/v1/auth/login' <<<"$smoke_step"; then
    echo "release smoke must complete setup and prove container backup/restore" >&2
    exit 1
fi
if grep -q 'release-smoke.toml\|DEPSILO_CONFIG=/app/config.toml' <<<"$smoke_step"; then
    echo "release smoke must not bypass image defaults with a prepared config" >&2
    exit 1
fi

publish_job=$(sed -n '/^  publish:/,$p' "$WORKFLOW")
if ! grep -Fqx '      group: depsilo-release-promotion-${{ github.repository }}' <<<"$publish_job" ||
    ! grep -Fqx '      queue: max' <<<"$publish_job" ||
    ! grep -Fqx '      cancel-in-progress: false' <<<"$publish_job"; then
    echo "release promotion must use one repository-wide FIFO concurrency group" >&2
    exit 1
fi
if ! grep -q 'list-verified-release-tags.sh' <<<"$publish_job" ||
    ! grep -q '"$GITHUB_SHA"' <<<"$publish_job" ||
    ! grep -q 'plan-release-promotion.sh' <<<"$publish_job" ||
    ! grep -q 'steps.promotion_plan.outputs.promote_floating' <<<"$publish_job"; then
    echo "release promotion must bind authoritative remote tag refs to the triggering commit" >&2
    exit 1
fi
ref_verifier_count=$(grep -c 'list-verified-release-tags.sh' <<<"$publish_job" || true)
if [ "$ref_verifier_count" -ne 3 ]; then
    echo "release promotion must verify tag identity at planning, mutation, and publication" >&2
    exit 1
fi
promote_step=$(sed -n '/- name: Promote trusted digest to release tags/,/- name: Verify promoted release tags/p' <<<"$publish_job")
if ! grep -q 'list-verified-release-tags.sh' <<<"$promote_step" ||
    ! grep -q 'plan-release-promotion.sh' <<<"$promote_step" ||
    ! grep -q 'EXPECTED_PROMOTE_FLOATING' <<<"$promote_step"; then
    echo "release tag mutation must revalidate identity and the monotonic promotion decision" >&2
    exit 1
fi
mutation_recheck_line=$(grep -n -m1 'list-verified-release-tags.sh' <<<"$promote_step" | cut -d: -f1)
first_mutation_line=$(grep -n -m1 'docker buildx imagetools create' <<<"$promote_step" | cut -d: -f1)
if [ "$mutation_recheck_line" -ge "$first_mutation_line" ]; then
    echo "release tag identity must be revalidated before the first registry mutation" >&2
    exit 1
fi
release_publish_step=$(sed -n '/- name: Publish trusted GitHub release/,$p' <<<"$publish_job")
if ! grep -q 'list-verified-release-tags.sh' <<<"$release_publish_step" ||
    ! grep -q 'plan-release-promotion.sh' <<<"$release_publish_step" ||
    ! grep -q 'PROMOTE_FLOATING' <<<"$release_publish_step"; then
    echo "GitHub release publication must revalidate identity and the promotion decision" >&2
    exit 1
fi
release_recheck_line=$(grep -n -m1 'list-verified-release-tags.sh' <<<"$release_publish_step" | cut -d: -f1)
release_edit_line=$(grep -n -m1 'gh release edit' <<<"$release_publish_step" | cut -d: -f1)
if [ "$release_recheck_line" -ge "$release_edit_line" ]; then
    echo "release tag identity must be revalidated before publishing the GitHub draft" >&2
    exit 1
fi
if grep -q -- '- name: Detect stable release' <<<"$publish_job"; then
    echo "release workflow bypasses the stale-tag promotion planner" >&2
    exit 1
fi
if ! grep -q -- '--latest=false' <<<"$publish_job" ||
    ! grep -Eq -- '--latest([[:space:]\\]+)?$' <<<"$publish_job"; then
    echo "GitHub releases must set latest explicitly from the promotion plan" >&2
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
