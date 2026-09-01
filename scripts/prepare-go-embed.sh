#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
DIST=${1:-web/dist}
if [[ "$DIST" != /* ]]; then
    DIST="$ROOT/$DIST"
fi
INDEX="$DIST/index.html"

if [[ ! -f "$INDEX" ]]; then
    mkdir -p "$DIST"
    printf '%s\n' '<!doctype html><title>Depsilo test placeholder</title>' >"$INDEX"
    echo ">>> prepared $INDEX for Go embed"
fi
