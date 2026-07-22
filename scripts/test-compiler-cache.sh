#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
FIXTURE="$ROOT/testground/compiler-cache/hello.c"

usage() {
    cat <<'EOF'
Usage:
  scripts/test-compiler-cache.sh

Required environment:
  COMPILER_CACHE_ENDPOINT=ENDPOINT
  COMPILER_CACHE_TOKEN=<read-write token from a CI secret or hidden prompt>

ENDPOINT must be one namespace endpoint ending in either
/ccache/v1/<namespace> or /sccache/v1/<namespace>. The other protocol endpoint
is derived automatically. The script expects a read-write build credential and
an already running Depsilo server.
EOF
}

fail() {
    echo "compiler-cache E2E failed: $*" >&2
    exit 1
}

need_command() {
    local command_name=$1
    local install_hint=$2
    command -v "$command_name" >/dev/null 2>&1 || fail "missing '$command_name'. $install_hint"
}

extract_version() {
    local output=$1
    local version
    version=$(grep -Eo '[0-9]+(\.[0-9]+){1,2}' <<<"$output" | sed -n '1p') || return 1
    [[ "$version" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]] || return 1
    printf '%s\n' "$version"
}

version_at_least() {
    local actual=$1
    local required=$2
    local actual_major=0 actual_minor=0 actual_patch=0
    local required_major=0 required_minor=0 required_patch=0
    IFS=. read -r actual_major actual_minor actual_patch <<<"$actual"
    IFS=. read -r required_major required_minor required_patch <<<"$required"
    actual_patch=${actual_patch:-0}
    required_patch=${required_patch:-0}
    ((
        10#$actual_major > 10#$required_major
        || (10#$actual_major == 10#$required_major && 10#$actual_minor > 10#$required_minor)
        || (10#$actual_major == 10#$required_major && 10#$actual_minor == 10#$required_minor && 10#$actual_patch >= 10#$required_patch)
    ))
}

if [[ ${1:-} == "-h" || ${1:-} == "--help" ]]; then
    usage
    exit 0
fi
if (( $# != 0 )); then
    usage >&2
    fail "pass endpoint and token through environment variables, not command-line arguments"
fi

endpoint=${COMPILER_CACHE_ENDPOINT:-${DEPSILO_COMPILE_CACHE_ENDPOINT:-}}
token=${COMPILER_CACHE_TOKEN:-${DEPSILO_COMPILE_CACHE_TOKEN:-}}
[[ -n "$endpoint" ]] || { usage >&2; fail "endpoint is required"; }
[[ -n "$token" ]] || { usage >&2; fail "token is required"; }

endpoint=${endpoint%/}
case "$endpoint" in
    http://*/ccache/v1/*|https://*/ccache/v1/*)
        origin=${endpoint%%/ccache/v1/*}
        namespace=${endpoint##*/ccache/v1/}
        ;;
    http://*/sccache/v1/*|https://*/sccache/v1/*)
        origin=${endpoint%%/sccache/v1/*}
        namespace=${endpoint##*/sccache/v1/}
        ;;
    *)
        fail "endpoint must end in /ccache/v1/<namespace> or /sccache/v1/<namespace>"
        ;;
esac
[[ -n "$namespace" && "$namespace" != */* && "$namespace" != *\?* && "$namespace" != *\#* ]] \
    || fail "endpoint must contain one concrete namespace"
[[ "$token" != *[$' \t\r\n']* ]] || fail "token must not contain whitespace"

ccache_endpoint="$origin/ccache/v1/$namespace"
sccache_endpoint="$origin/sccache/v1/$namespace"

CCACHE_BIN=${CCACHE_BIN:-ccache}
SCCACHE_BIN=${SCCACHE_BIN:-sccache}
if [[ -n ${COMPILER:-} ]]; then
    compiler=$COMPILER
elif [[ -n ${CC:-} ]]; then
    compiler=$CC
elif command -v gcc >/dev/null 2>&1; then
    compiler=gcc
elif command -v clang >/dev/null 2>&1; then
    compiler=clang
else
    compiler=cc
fi

need_command "$CCACHE_BIN" "Install ccache 4.7+ (for example: apt install ccache, or brew install ccache)."
need_command "$SCCACHE_BIN" "Install official sccache v0.15.0+ from https://github.com/mozilla/sccache/releases."
need_command "$compiler" "Install GCC/Clang (for example: apt install build-essential, or xcode-select --install)."
[[ -f "$FIXTURE" ]] || fail "missing fixture: $FIXTURE"

ccache_version_output=$("$CCACHE_BIN" --version | sed -n '1p')
ccache_version=$(extract_version "$ccache_version_output") \
    || fail "could not parse ccache version from: $ccache_version_output"
version_at_least "$ccache_version" 4.7.0 \
    || fail "ccache $ccache_version is unsupported; install ccache 4.7+"
if [[ "$ccache_endpoint" == https://* ]]; then
    version_at_least "$ccache_version" 4.13.0 \
        || fail "ccache HTTPS requires ccache 4.13+; found $ccache_version"
fi

sccache_version_output=$("$SCCACHE_BIN" --version | sed -n '1p')
sccache_version=$(extract_version "$sccache_version_output") \
    || fail "could not parse sccache version from: $sccache_version_output"
version_at_least "$sccache_version" 0.15.0 \
    || fail "sccache $sccache_version is unsupported; install sccache 0.15+"

TMP=$(mktemp -d)
ccache_dir="$TMP/ccache"
sccache_dir="$TMP/sccache"
ccache_config="$TMP/ccache.conf"
sccache_config="$TMP/sccache.conf"
object="$TMP/hello.o"
first_object="$TMP/hello-first.o"
sccache_started=0

pick_sccache_port() {
    local port=$((42000 + ($$ % 10000)))
    local attempts=0
    while (( attempts < 100 )); do
        if (exec 9<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
            exec 9>&-
            exec 9<&-
            port=$((port + 1))
            attempts=$((attempts + 1))
            continue
        fi
        echo "$port"
        return 0
    done
    return 1
}

sccache_port=${COMPILER_CACHE_SCCACHE_PORT:-$(pick_sccache_port)}

sccache_run() {
    (
        unset SCCACHE_CACHED_CONF SCCACHE_LOCAL_RW_MODE SCCACHE_NO_DAEMON \
            SCCACHE_DIRECT SCCACHE_NO_CACHE SCCACHE_RECACHE \
            SCCACHE_SERVER_UDS SCCACHE_STATS_FORMAT SCCACHE_WEBDAV_KEY_PREFIX \
            SCCACHE_WEBDAV_PASSWORD SCCACHE_WEBDAV_RW_MODE SCCACHE_WEBDAV_USERNAME
        env \
            SCCACHE_CONF="$sccache_config" \
            SCCACHE_DIR="$sccache_dir" \
            SCCACHE_CACHE_SIZE=64M \
            SCCACHE_MULTILEVEL_CHAIN=disk,webdav \
            SCCACHE_MULTILEVEL_WRITE_ERROR_POLICY=all \
            SCCACHE_IDLE_TIMEOUT=0 \
            SCCACHE_SERVER_PORT="$sccache_port" \
            SCCACHE_WEBDAV_ENDPOINT="$sccache_endpoint" \
            SCCACHE_WEBDAV_TOKEN="$token" \
            "$SCCACHE_BIN" "$@"
    )
}

cleanup() {
    if (( sccache_started )); then
        sccache_run --stop-server >/dev/null 2>&1 || true
        sccache_started=0
    fi
    rm -rf "$TMP"
}
trap cleanup EXIT INT TERM

ccache_run() {
    local remote_storage
    if [[ "$ccache_endpoint" == https://* ]]; then
        remote_storage="$ccache_endpoint|@bearer-token=$token"
    else
        remote_storage="$ccache_endpoint|bearer-token=$token"
    fi
    (
        unset CCACHE_CC CCACHE_COMPILER CCACHE_DISABLE CCACHE_NOSTATS \
            CCACHE_READONLY CCACHE_READONLY_DIRECT CCACHE_RECACHE \
            CCACHE_REMOTE_ONLY CCACHE_NOREMOTE_ONLY
        env \
            CCACHE_DIR="$ccache_dir" \
            CCACHE_CONFIGPATH="$ccache_config" \
            CCACHE_COMPILERCHECK=content \
            CCACHE_REMOTE_STORAGE="$remote_storage" \
            "$CCACHE_BIN" "$@"
    )
}

ccache_stat() {
    local stats=$1
    local name=$2
    awk -F '\t' -v name="$name" '$1 == name { print $2; exit }' <<<"$stats"
}

sccache_stat() {
    local stats=$1
    local name=$2
    awk -v name="$name" '$1 == "Cache" && $2 == name && $3 ~ /^[0-9]+$/ { print $3; exit }' <<<"$stats"
}

require_positive_stat() {
    local client=$1
    local name=$2
    local value=$3
    local output=$4
    if [[ ! "$value" =~ ^[0-9]+$ ]] || (( value < 1 )); then
        echo "$output" >&2
        fail "$client did not report $name"
    fi
}

epoch=$(date +%s)
run_id=$(( (epoch ^ ($$ << 11) ^ (RANDOM << 1) ^ RANDOM) & 2147483647 ))
run_id=$((run_id % 1999999999))

echo ">>> ccache: $ccache_version_output"
mkdir -p "$ccache_dir"
: >"$ccache_config"
ccache_run --zero-stats >/dev/null
if ! ccache_run "$compiler" -DDEPSILO_COMPILER_CACHE_PROBE="$run_id" -c "$FIXTURE" -o "$object"; then
    if [[ "$ccache_endpoint" == https://* ]]; then
        fail "ccache compile failed; HTTPS also requires the official ccache-storage-http-go helper"
    fi
    fail "ccache compile miss failed"
fi
ccache_first_stats=$(ccache_run --print-stats)
require_positive_stat "ccache" "cache_miss" \
    "$(ccache_stat "$ccache_first_stats" cache_miss)" "$ccache_first_stats"
cp "$object" "$first_object"

# Remove only the build node's local cache. The namespace in Depsilo remains.
rm -rf "$ccache_dir"
mkdir -p "$ccache_dir"
rm -f "$object"
ccache_run --zero-stats >/dev/null
ccache_run "$compiler" -DDEPSILO_COMPILER_CACHE_PROBE="$run_id" -c "$FIXTURE" -o "$object"
ccache_second_stats=$(ccache_run --print-stats)
require_positive_stat "ccache" "remote_storage_hit" \
    "$(ccache_stat "$ccache_second_stats" remote_storage_hit)" "$ccache_second_stats"
cmp -s "$first_object" "$object" || fail "ccache remote hit produced different object bytes"
echo ">>> ccache: compile miss -> remote write -> local clear -> remote hit OK"

echo ">>> sccache: $sccache_version_output"
mkdir -p "$sccache_dir"
: >"$sccache_config"
if ! sccache_run --start-server >/dev/null; then
    fail "sccache server failed to start; verify the WebDAV endpoint and read-write credential"
fi
sccache_started=1
sccache_run --zero-stats >/dev/null
rm -f "$object" "$first_object"
if ! sccache_run "$compiler" -DDEPSILO_COMPILER_CACHE_PROBE="$((run_id + 1))" -c "$FIXTURE" -o "$object"; then
    fail "sccache compile miss failed"
fi
sccache_first_stats=$(sccache_run --show-stats)
require_positive_stat "sccache" "misses" \
    "$(sccache_stat "$sccache_first_stats" misses)" "$sccache_first_stats"
cp "$object" "$first_object"

# Restart with a fresh SCCACHE_DIR so no daemon or node-local state can satisfy
# the second compile. The WebDAV namespace remains intact.
sccache_run --stop-server >/dev/null
sccache_started=0
rm -rf "$sccache_dir"
mkdir -p "$sccache_dir"
rm -f "$object"
sccache_run --start-server >/dev/null
sccache_started=1
sccache_run --zero-stats >/dev/null
sccache_run "$compiler" -DDEPSILO_COMPILER_CACHE_PROBE="$((run_id + 1))" -c "$FIXTURE" -o "$object"
sccache_second_stats=$(sccache_run --show-stats)
require_positive_stat "sccache" "hits" \
    "$(sccache_stat "$sccache_second_stats" hits)" "$sccache_second_stats"
cmp -s "$first_object" "$object" || fail "sccache remote hit produced different object bytes"
echo ">>> sccache: compile miss -> remote write -> local reset -> remote hit OK"
echo ">>> compiler-cache real-client E2E passed for namespace '$namespace'"
