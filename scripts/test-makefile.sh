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

assert_before() {
    local text=$1
    local first=$2
    local second=$3
    local before_first before_second
    assert_contains "$text" "$first"
    assert_contains "$text" "$second"
    before_first=${text%%"$first"*}
    before_second=${text%%"$second"*}
    [[ ${#before_first} -lt ${#before_second} ]] \
        || fail "expected before: $first -> $second"
}

# The database is used only to keep the supported real-client inventory in
# sync; ordinary convenience targets are exercised by their owning tests.
make_db=$(make -C "$ROOT" -pn help)
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

for ecosystem in "${default_ecosystems[@]}"; do
    grep -Fq "[[$ecosystem.upstreams]]" "$ROOT/config.example.toml" \
        || fail "config.example.toml must explicitly configure $ecosystem for real-client E2E"
done
if grep -Eq '^[[:space:]]*proxy[[:space:]]*=[[:space:]]*"http://127\.0\.0\.1:7890"' \
    "$ROOT/config.example.toml"; then
    fail "config.example.toml must not require an operator loopback proxy"
fi

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

all_dry_run=$(make -n -C "$ROOT" test-e2e DEPSILO_URL=http://127.0.0.1:24444)
for ecosystem in "${default_ecosystems[@]}"; do
    assert_contains "$all_dry_run" "-t depsilo-test-${ecosystem} testground/docker-${ecosystem}"
done
assert_contains "$all_dry_run" 'scripts/dev-service.sh start'
assert_contains "$all_dry_run" 'DEPSILO_CONFIG="config.example.toml"'
e2e_state_dir="$ROOT/.test-e2e-state"
assert_contains "$all_dry_run" "bash scripts/e2e-state.sh prepare \"$e2e_state_dir\""
assert_contains "$all_dry_run" "HOME=\"$e2e_state_dir/home\""
assert_contains "$all_dry_run" "DEPSILO_DEV_JWT_FILE=\"$e2e_state_dir/.dev-jwt-secret\""
assert_contains "$all_dry_run" "DEPSILO_DATABASE_DSN=\"$e2e_state_dir/data/depsilo.db\""
assert_contains "$all_dry_run" "DEPSILO_STORAGE_PATH=\"$e2e_state_dir/data/cache\""
assert_contains "$all_dry_run" "DEPSILO_COMPILE_CACHE_STORAGE_PATH=\"$e2e_state_dir/data/compile-cache\""
assert_contains "$all_dry_run" "\"$e2e_state_dir/.server.pid\" \"$e2e_state_dir/.dev.log\""
e2e_state_guard="bash scripts/e2e-state.sh guard \"$e2e_state_dir\""
e2e_service_stop="bash scripts/dev-service.sh stop \"./bin/depsilo\" \"$e2e_state_dir/.server.pid\""
assert_before "$all_dry_run" "$e2e_state_guard" "$e2e_service_stop"
if [[ "$all_dry_run" == *'scripts/dev-service.sh stop "./bin/depsilo" ".server.pid"'* ]]; then
    fail "real-client E2E must not stop the operator development service"
fi
assert_contains "$all_dry_run" "trap 'bash scripts/dev-service.sh stop"
assert_contains "$all_dry_run" "bash scripts/e2e-state.sh clean \"$e2e_state_dir\""
if [[ "$all_dry_run" == *"rm -rf \"$e2e_state_dir\""* ]]; then
    fail "real-client E2E must clean state only through the guarded seam"
fi
if [[ "$all_dry_run" == *"npm --prefix web run build"* ]]; then
    fail "real-client E2E must not build the frontend"
fi
if [[ "$all_dry_run" == *"depsilo-test-docker testground/docker-docker"* ]]; then
    fail "docker registry E2E must remain opt-in"
fi

e2e_clean_dry_run=$(make -n -C "$ROOT" test-clean)
assert_before "$e2e_clean_dry_run" "$e2e_state_guard" "$e2e_service_stop"
assert_contains "$e2e_clean_dry_run" "bash scripts/e2e-state.sh clean \"$e2e_state_dir\""
if grep -Eq '^rm -rf ([^[:space:]]+[[:space:]]+)*data([[:space:]]|$)' <<<"$e2e_clean_dry_run"; then
    fail "test-clean must not remove operator runtime data"
fi

unsafe_e2e_clean_dry_run=$(make -n -C "$ROOT" test-clean \
    E2E_STATE_DIR=/ E2E_HOME=/ E2E_DATA_DIR=/ E2E_DATABASE=/ \
    E2E_STORAGE=/ E2E_COMPILE_STORAGE=/ E2E_JWT_SECRET=/ \
    E2E_PID_FILE=/ E2E_LOG=/)
assert_contains "$unsafe_e2e_clean_dry_run" \
    "bash scripts/e2e-state.sh clean \"$e2e_state_dir\""
if [[ "$unsafe_e2e_clean_dry_run" == *'rm -rf "/"'* ]] \
    || [[ "$unsafe_e2e_clean_dry_run" == *'find "/"'* ]]; then
    fail "E2E_STATE_DIR=/ produced a broad delete"
fi

e2e_state_script="$ROOT/scripts/e2e-state.sh"
[[ -x "$e2e_state_script" ]] || fail "missing executable E2E state safety seam"
unsafe_cleanup_sentinel="$TMP/e2e-state-clean-sentinel"
printf '%s\n' keep > "$unsafe_cleanup_sentinel"
if "$e2e_state_script" clean / >/dev/null 2>&1; then
    fail "E2E state safety seam accepted root as its cleanup target"
fi
[[ "$(cat "$unsafe_cleanup_sentinel")" == keep ]] \
    || fail "unsafe E2E cleanup touched data outside its reserved state root"

guard_fixture_root="$TMP/e2e-guard-fixture"
mkdir -p "$guard_fixture_root/scripts"
cp "$e2e_state_script" "$guard_fixture_root/scripts/e2e-state.sh"
guard_fixture_state="$guard_fixture_root/.test-e2e-state"
"$guard_fixture_root/scripts/e2e-state.sh" guard "$guard_fixture_state" \
    || fail "E2E state guard rejected an absent reserved state root"
mkdir -m 0700 "$guard_fixture_state"
if guard_error=$("$guard_fixture_root/scripts/e2e-state.sh" guard "$guard_fixture_state" 2>&1); then
    fail "E2E state guard accepted an unowned reserved state root"
fi
assert_contains "$guard_error" 'does not contain the ownership marker'
printf '%s\n' depsilo-real-client-e2e-state-v1 \
    > "$guard_fixture_state/.owned-by-depsilo-real-client-e2e"
"$guard_fixture_root/scripts/e2e-state.sh" guard "$guard_fixture_state" \
    || fail "E2E state guard rejected its valid ownership marker"

registry_dry_run=$(make -n -C "$ROOT" test-docker-docker DEPSILO_URL=http://127.0.0.1:24444)
assert_contains "$registry_dry_run" "--build-arg DEPSILO_URL=http://127.0.0.1:24444"
assert_contains "$registry_dry_run" "docker run --rm --privileged"

# The docker:dind entrypoint assigns DOCKER_HOST=tcp://docker:2375 before the
# fixture starts its nested daemon. Exercise the actual generated smoke script
# with Docker itself stubbed at the system boundary: it must select the local
# Unix socket and reach the real-client pull step.
dind_dockerfile="$ROOT/testground/docker-docker/Dockerfile"
dind_smoke_escaped=$(sed -n "s|^RUN printf '\\(.*\\)' > /smoke.sh && chmod +x /smoke.sh$|\\1|p" "$dind_dockerfile")
[[ -n "$dind_smoke_escaped" ]] || fail "docker dind smoke script is not testable"
dind_smoke="$TMP/docker-dind-smoke.sh"
printf '%b' "$dind_smoke_escaped" > "$dind_smoke"
dind_log="$TMP/docker-dind-dockerd.log"
sed -i "s|/var/log/dockerd.log|$dind_log|g" "$dind_smoke"
dind_registry_host="$TMP/docker-dind-registry-host"
printf '%s\n' 'depsilo.test:24444' > "$dind_registry_host"
sed -i "s|/depsilo-registry-host|$dind_registry_host|g" "$dind_smoke"
dind_pull_marker="$TMP/docker-dind-pull"
if ! dind_smoke_output=$(
    export DOCKER_HOST=tcp://docker:2375
    dockerd() { :; }
    sleep() { :; }
    tail() { :; }
    docker() {
        if [[ "${DOCKER_HOST:-}" != "unix:///var/run/docker.sock" ]]; then
            echo "docker client used ${DOCKER_HOST:-<unset>} instead of nested daemon socket" >&2
            return 1
        fi
        case "$*" in
            info) ;;
            'pull depsilo.test:24444/library/alpine:3.18') : > "$dind_pull_marker" ;;
            'image inspect depsilo.test:24444/library/alpine:3.18') ;;
            *) echo "unexpected docker command: $*" >&2; return 1 ;;
        esac
    }
    # shellcheck disable=SC1090 -- generated directly from the owned fixture.
    source "$dind_smoke"
  2>&1
); then
    fail "docker dind smoke did not bind its nested daemon: $dind_smoke_output"
fi
[[ -f "$dind_pull_marker" ]] \
    || fail "docker dind smoke did not pull alpine:3.18 directly through Depsilo"

compiler_cache_dry_run=$(make -n -C "$ROOT" test-compiler-cache \
    COMPILER_CACHE_ENDPOINT=http://127.0.0.1:23333/ccache/v1/test \
    COMPILER_CACHE_TOKEN=sentinel-compiler-cache-token)
assert_contains "$compiler_cache_dry_run" "bash scripts/test-compiler-cache.sh"
if [[ "$compiler_cache_dry_run" == *"sentinel-compiler-cache-token"* ]] \
    || [[ "$compiler_cache_dry_run" == *"/ccache/v1/test"* ]]; then
    fail "test-compiler-cache must not expose endpoint or token in process arguments"
fi

v090_compose_dry_run=$(make -n -C "$ROOT" test-v090-compose-upgrade)
assert_contains "$v090_compose_dry_run" 'bash scripts/test-v090-compose-upgrade.sh'

docker_run=$(make -n -C "$ROOT" docker-run CONFIG=config.toml DEV_JWT_SECRET=.dev-jwt-secret)
assert_contains "$docker_run" 'DEPSILO_AUTH_JWT_SECRET="$secret" docker run'
assert_contains "$docker_run" '--user "$(id -u):$(id -g)"'
assert_contains "$docker_run" '-e HOME=/tmp/depsilo-home'
if [[ "$docker_run" == *"cp config.example.toml"* ]]; then
    fail "docker-run must not create a production-like config as a side effect"
fi

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

metadata=$(make -s -C "$ROOT" version \
    VERSION=1.2.3 COMMIT=abc123 BUILD_DATE=2026-01-01T00:00:00Z)
[[ "$metadata" == $'version=1.2.3\ncommit=abc123\nbuild_date=2026-01-01T00:00:00Z' ]] \
    || fail "build metadata overrides are no longer reproducible"

echo "makefile workflow tests passed"
