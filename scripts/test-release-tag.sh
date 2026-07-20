#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
VALIDATOR="$ROOT/scripts/validate-release-tag.sh"

for tag in \
    v0.0.0 \
    v1.2.3 \
    v1.2.3-alpha \
    v1.2.3-alpha.1 \
    v1.2.3-0.3.7 \
    v1.2.3-rc-1+build.5; do
    bash "$VALIDATOR" "$tag"
done

for tag in \
    1.2.3 \
    v01.2.3 \
    v1.02.3 \
    v1.2.03 \
    v1.2 \
    v1.2.bad \
    v1.2.3-01 \
    v1.2.3-alpha.01 \
    v1.2.3- \
    v1.2.3+; do
    if bash "$VALIDATOR" "$tag" >/dev/null 2>&1; then
        echo "invalid release tag was accepted: $tag" >&2
        exit 1
    fi
done

echo "release tag tests passed"
