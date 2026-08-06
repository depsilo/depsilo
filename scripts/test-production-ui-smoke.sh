#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
STATE_DIR=$(mktemp -d "${TMPDIR:-/tmp}/depsilo-production-ui.XXXXXX")

cleanup() {
    local exit_code=$?
    trap - EXIT INT TERM
    find "$STATE_DIR" -depth -delete >/dev/null 2>&1 || true
    exit "$exit_code"
}
trap cleanup EXIT INT TERM

export PLAYWRIGHT_PRODUCTION_STATE_DIR=$STATE_DIR
cd "$ROOT/web"
npx --no-install playwright test --config playwright.production.config.ts "$@"
