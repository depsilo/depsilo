#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=../install.sh
DEPSILO_INSTALL_SOURCE_ONLY=1
source "$ROOT/install.sh"

assert_contains() {
    local output="$1"
    local expected="$2"
    if [[ "$output" != *"$expected"* ]]; then
        echo "expected installer output to contain: $expected" >&2
        exit 1
    fi
}

assert_not_contains() {
    local output="$1"
    local unexpected="$2"
    if [[ "$output" == *"$unexpected"* ]]; then
        echo "expected installer output not to contain: $unexpected" >&2
        exit 1
    fi
}

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
cd "$TMP"

printf 'trusted release bytes' > depsilo_test.tar.gz
if command -v sha256sum >/dev/null 2>&1; then
    HASH=$(sha256sum depsilo_test.tar.gz | awk '{print $1}')
else
    HASH=$(shasum -a 256 depsilo_test.tar.gz | awk '{print $1}')
fi
printf '%s  %s\n' "$HASH" depsilo_test.tar.gz > checksums.txt
verify_checksum depsilo_test.tar.gz checksums.txt >/dev/null

if [ "$(archive_binary_name windows)" != "depsilo.exe" ]; then
    echo "Windows archive binary name is incorrect" >&2
    exit 1
fi
if [ "$(archive_binary_name linux)" != "depsilo" ]; then
    echo "Unix archive binary name is incorrect" >&2
    exit 1
fi

printf '%064d  %s\n' 0 depsilo_test.tar.gz > bad-checksums.txt
if (verify_checksum depsilo_test.tar.gz bad-checksums.txt >/dev/null 2>&1); then
    echo "checksum mismatch was accepted" >&2
    exit 1
fi

printf '%s  %s\n' "$HASH" another-file.tar.gz > missing-checksums.txt
if (verify_checksum depsilo_test.tar.gz missing-checksums.txt >/dev/null 2>&1); then
    echo "missing archive checksum was accepted" >&2
    exit 1
fi

# Exercise the wget-only path without relying on the host's installed tools.
mkdir fakebin
printf '%s\n' \
    '#!/bin/sh' \
    'if [ "$1" = "-qO-" ]; then' \
    '  printf '\''{"tag_name":"v9.8.7"}\n'\''' \
    'else' \
    '  printf '\''downloaded bytes'\'' > "$4"' \
    'fi' > fakebin/wget
chmod +x fakebin/wget
(
    PATH="$TMP/fakebin"
    DOWNLOAD_TOOL=""
    select_downloader
    [ "$DOWNLOAD_TOOL" = "wget" ]
    [ "$(fetch_stdout https://example.invalid/latest)" = '{"tag_name":"v9.8.7"}' ]
    download_file https://example.invalid/archive wget-download
    [ "$(<wget-download)" = "downloaded bytes" ]
)

SUCCESS_OUTPUT=$(
    GREEN='' BOLD='' NC='' \
        print_install_success v9.8.7 /opt/depsilo/bin/depsilo
)
assert_contains "$SUCCESS_OUTPUT" 'Depsilo v9.8.7 installed'
assert_contains "$SUCCESS_OUTPUT" '  /opt/depsilo/bin/depsilo'
assert_contains "$SUCCESS_OUTPUT" 'Get started:'
assert_contains "$SUCCESS_OUTPUT" '  depsilo serve'
assert_contains "$SUCCESS_OUTPUT" 'Then open:'
assert_contains "$SUCCESS_OUTPUT" '  http://127.0.0.1:23333'
assert_contains "$SUCCESS_OUTPUT" 'Docs:'
assert_contains "$SUCCESS_OUTPUT" '  https://github.com/depsilo/depsilo#quick-start'
assert_not_contains "$SUCCESS_OUTPUT" 'config.example.toml'
assert_not_contains "$SUCCESS_OUTPUT" 'config.toml'
assert_not_contains "$SUCCESS_OUTPUT" 'Docker'
assert_not_contains "$SUCCESS_OUTPUT" 'docker'

# The latest-release lookup must survive the unauthenticated GitHub API rate
# limit: prefer the releases/latest redirect and fall back to the API.
mkdir fakebin-curl-redirect
printf '%s\n' \
    '#!/bin/sh' \
    'case "$*" in' \
    '  *-fsSLI*) printf '\''%s\n'\'' "https://github.com/depsilo/depsilo/releases/tag/v9.9.9" ;;' \
    '  *) exit 7 ;;' \
    'esac' > fakebin-curl-redirect/curl
chmod +x fakebin-curl-redirect/curl
(
    PATH="$TMP/fakebin-curl-redirect:$PATH"
    DOWNLOAD_TOOL="curl"
    [ "$(resolve_latest_version)" = "v9.9.9" ]
)

mkdir fakebin-curl-fallback
printf '%s\n' \
    '#!/bin/sh' \
    'case "$*" in' \
    '  *-fsSLI*) exit 22 ;;' \
    '  *api.github.com*) printf '\''{"tag_name":"v8.8.8"}\n'\'' ;;' \
    '  *) exit 7 ;;' \
    'esac' > fakebin-curl-fallback/curl
chmod +x fakebin-curl-fallback/curl
(
    PATH="$TMP/fakebin-curl-fallback:$PATH"
    DOWNLOAD_TOOL="curl"
    [ "$(resolve_latest_version)" = "v8.8.8" ]
)

mkdir fakebin-curl-broken
printf '%s\n' \
    '#!/bin/sh' \
    'exit 22' > fakebin-curl-broken/curl
chmod +x fakebin-curl-broken/curl
if (
    PATH="$TMP/fakebin-curl-broken:$PATH"
    DOWNLOAD_TOOL="curl"
    resolve_latest_version >/dev/null 2>&1
); then
    echo "broken release lookup was reported as success" >&2
    exit 1
fi

echo "install checksum tests passed"
