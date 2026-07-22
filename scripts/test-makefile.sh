#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

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
    build run run-pro dev stop logs
    test test-unit test-integration test-e2e test-compiler-cache test-clean
    lint lint-i18n verify clean help
    tray app-macos install-linux autostart-linux
)

for target in "${required_targets[@]}"; do
    grep -Eq "^${target}:" <<<"$make_db" || fail "target not defined: $target"
done

default_ecosystems=(pypi apt npm go cargo maven rubygems composer nuget conda cran helm huggingface)
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
    'conda|/conda/pkgs/main' 'cran|/cran/' 'helm|/helm'
    'huggingface|/huggingface' 'docker|registry-mirrors'
)
for contract in "${route_contracts[@]}"; do
    ecosystem=${contract%%|*}
    route=${contract#*|}
    grep -Fq "$route" "$ROOT/testground/docker-${ecosystem}/Dockerfile" \
        || fail "docker-${ecosystem} lost route contract: $route"
done

dry_run=$(make -n -C "$ROOT" test-docker-pypi DEPSILO_URL=http://127.0.0.1:24444)
assert_contains "$dry_run" "--build-arg DEPSILO_URL=http://127.0.0.1:24444"
assert_contains "$dry_run" "-t depsilo-test-pypi testground/docker-pypi"

all_dry_run=$(make -n -C "$ROOT" test-docker-all DEPSILO_URL=http://127.0.0.1:24444)
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

help_output=$(make -s -C "$ROOT" help)
assert_contains "$help_output" "run"
assert_contains "$help_output" "test-e2e"
if [[ "$help_output" == *"verify-e2e"* ]] || [[ "$help_output" == *"cli-status"* ]]; then
    fail "internal compatibility targets must stay out of make help"
fi

echo "makefile interface tests passed"
