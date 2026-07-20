#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=../install.sh
DEPSILO_INSTALL_SOURCE_ONLY=1
source "$ROOT/install.sh"

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

echo "install checksum tests passed"
