#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

fail() {
    echo "makefile test failed: $*" >&2
    exit 1
}

assert_contains() {
    local text=$1
    local expected=$2
    [[ "$text" == *"$expected"* ]] || fail "missing: $expected"
}

make_db=$(make -C "$ROOT" -pn help)
required_targets=(
    setup setup-ui version build run run-pro dev stop logs
    test test-race test-integration test-ui test-e2e test-compiler-cache test-clean
    lint lint-i18n check verify security clean clean-all help verify-scripts
    tray app-macos install-linux autostart-linux
)

for target in "${required_targets[@]}"; do
    grep -Eq "^${target}:" <<<"$make_db" || fail "target not defined: $target"
done

default_ecosystems=(
    pypi apt npm go cargo maven rubygems composer nuget conda cran alpine helm huggingface
)
ecosystems=("${default_ecosystems[@]}" docker)
for ecosystem in "${ecosystems[@]}"; do
    target="test-docker-${ecosystem}"
    grep -Eq "^${target}:" <<<"$make_db" || fail "target not defined: $target"

    dockerfile="$ROOT/testground/docker-${ecosystem}/Dockerfile"
    [[ -f "$dockerfile" ]] || fail "missing fixture: $dockerfile"
    args=$(grep -E '^ARG ' "$dockerfile" || true)
    [[ "$args" == "ARG DEPSILO_URL" ]] || fail "$target must expose only ARG DEPSILO_URL"
done

route_contracts=(
    'pypi|/pypi/simple/' 'apt|/apt' 'npm|/npm/' 'go|/go'
    'cargo|/crates/' 'maven|/maven' 'rubygems|/rubygems/'
    'composer|/composer' 'nuget|/nuget/v3/index.json'
    'conda|/conda/pkgs/main' 'cran|/cran/' 'alpine|/alpine/'
    'helm|/helm' 'huggingface|/huggingface' 'docker|registry-mirrors'
)
for contract in "${route_contracts[@]}"; do
    ecosystem=${contract%%|*}
    route=${contract#*|}
    grep -Fq "$route" "$ROOT/testground/docker-${ecosystem}/Dockerfile" \
        || fail "docker-${ecosystem} lost route contract: $route"
done

EMBED_DIST="$TMP/web-dist"
make -s -C "$ROOT" prepare-go WEB_DIST="$EMBED_DIST"
[[ -s "$EMBED_DIST/index.html" ]] || fail "prepare-go did not create the embed placeholder"

printf '%s\n' 'keep-existing-dist' > "$EMBED_DIST/index.html"
make -s -C "$ROOT" prepare-go WEB_DIST="$EMBED_DIST"
[[ "$(cat "$EMBED_DIST/index.html")" == 'keep-existing-dist' ]] \
    || fail "prepare-go overwrote an existing frontend build"

docker_dry_run=$(make -n -C "$ROOT" test-docker-pypi DEPSILO_URL=http://127.0.0.1:24444)
assert_contains "$docker_dry_run" "--build-arg DEPSILO_URL=http://127.0.0.1:24444"
assert_contains "$docker_dry_run" "-t depsilo-test-pypi testground/docker-pypi"

all_dry_run=$(make -n -C "$ROOT" test-e2e DEPSILO_URL=http://127.0.0.1:24444)
for ecosystem in "${default_ecosystems[@]}"; do
    assert_contains "$all_dry_run" "-t depsilo-test-${ecosystem} testground/docker-${ecosystem}"
done
if [[ "$all_dry_run" == *"depsilo-test-docker testground/docker-docker"* ]]; then
    fail "docker registry E2E must remain opt-in"
fi

registry_dry_run=$(make -n -C "$ROOT" test-docker-docker DEPSILO_URL=http://127.0.0.1:24444)
assert_contains "$registry_dry_run" "--build-arg DEPSILO_URL=http://127.0.0.1:24444"
assert_contains "$registry_dry_run" "docker run --rm --privileged"

compiler_cache_dry_run=$(make -n -C "$ROOT" test-compiler-cache \
    COMPILER_CACHE_ENDPOINT=http://127.0.0.1:23333/ccache/v1/test \
    COMPILER_CACHE_TOKEN=sentinel-compiler-cache-token)
assert_contains "$compiler_cache_dry_run" "bash scripts/test-compiler-cache.sh"
if [[ "$compiler_cache_dry_run" == *"sentinel-compiler-cache-token"* ]] \
    || [[ "$compiler_cache_dry_run" == *"/ccache/v1/test"* ]]; then
    fail "test-compiler-cache must not expose endpoint or token in process arguments"
fi

docker_run=$(make -n -C "$ROOT" docker-run CONFIG=config.toml DEV_JWT_SECRET=.dev-jwt-secret)
assert_contains "$docker_run" 'DEPSILO_AUTH_JWT_SECRET="$secret" docker run'
if [[ "$docker_run" == *"cp config.example.toml"* ]]; then
    fail "docker-run must not create a production-like config as a side effect"
fi

compose_dry_run=$(make -n -C "$ROOT" docker-compose-up PORT=18080)
assert_contains "$compose_dry_run" "PORT=18080 docker compose up"
assert_contains "$compose_dry_run" "http://localhost:18080"

clean_dry_run=$(make -n -C "$ROOT" clean)
if grep -Eq '^rm -rf ([^[:space:]]+[[:space:]]+)*data([[:space:]]|$)' <<<"$clean_dry_run"; then
    fail "make clean still removes local runtime data"
fi
if grep -Fq '.dev-jwt-secret' <<<"$clean_dry_run"; then
    fail "make clean still removes the development JWT"
fi
assert_contains "$clean_dry_run" "rm -rf bin dist web/dist"

clean_all_dry_run=$(make -n -C "$ROOT" clean-all)
grep -Eq '^rm -rf ([^[:space:]]+[[:space:]]+)*data([[:space:]]|$)' <<<"$clean_all_dry_run" \
    || fail "make clean-all no longer removes local runtime data"
assert_contains "$clean_all_dry_run" '.dev-jwt-secret'

setup_dry_run=$(make -n -C "$ROOT" setup)
assert_contains "$setup_dry_run" 'go mod download'
assert_contains "$setup_dry_run" 'npm --prefix web ci'

setup_ui_dry_run=$(make -n -C "$ROOT" setup-ui)
assert_contains "$setup_ui_dry_run" 'npx --no-install playwright install chromium'

outside_version=$(cd "$TMP" && make -s -f "$ROOT/Makefile" version | sed -n '1p')
[[ "$outside_version" == 'version=dev' ]] \
    || fail "version fallback outside Git is $outside_version, want version=dev"

metadata=$(make -s -C "$ROOT" version \
    VERSION=1.2.3 COMMIT=abc123 BUILD_DATE=2026-01-01T00:00:00Z)
[[ "$metadata" == $'version=1.2.3\ncommit=abc123\nbuild_date=2026-01-01T00:00:00Z' ]] \
    || fail "build metadata overrides are no longer reproducible"

release_check_dry_run=$(make -s -n -C "$ROOT" release-check)
[[ "$release_check_dry_run" == 'goreleaser check' ]] \
    || fail "release-check performs work beyond validating GoReleaser config"

help_output=$(make -s -C "$ROOT" help)
for visible_target in setup setup-ui version run test-e2e clean clean-all check verify; do
    assert_contains "$help_output" "$visible_target"
done
if [[ "$help_output" == *"verify-installer"* ]] || [[ "$help_output" == *"cli-status"* ]]; then
    fail "internal compatibility targets must stay out of make help"
fi

echo "makefile workflow tests passed"
