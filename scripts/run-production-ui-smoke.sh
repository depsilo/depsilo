#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BINARY=${PLAYWRIGHT_PRODUCTION_BINARY:-$ROOT/bin/depsilo}
PORT=${PLAYWRIGHT_PORT:-4174}

if [[ ! -x "$BINARY" ]]; then
    echo "production UI smoke binary is missing: $BINARY (run make build first)" >&2
    exit 1
fi
if [[ ! "$PORT" =~ ^[0-9]+$ ]] || (( PORT < 1 || PORT > 65535 )); then
    echo "PLAYWRIGHT_PORT must be an integer between 1 and 65535" >&2
    exit 1
fi

STATE_DIR=${PLAYWRIGHT_PRODUCTION_STATE_DIR:-}
OWNS_STATE_DIR=0
if [[ -z "$STATE_DIR" ]]; then
    STATE_DIR=$(mktemp -d "${TMPDIR:-/tmp}/depsilo-production-ui.XXXXXX")
    OWNS_STATE_DIR=1
else
    mkdir -p "$STATE_DIR"
fi
CONFIG=$STATE_DIR/config.toml
SERVER_PID=

cleanup() {
    local exit_code=$?
    trap - EXIT INT TERM
    if [[ -n "$SERVER_PID" ]]; then
        kill "$SERVER_PID" >/dev/null 2>&1 || true
        wait "$SERVER_PID" >/dev/null 2>&1 || true
    fi
    if (( OWNS_STATE_DIR )); then
        find "$STATE_DIR" -depth -delete >/dev/null 2>&1 || true
    fi
    exit "$exit_code"
}
trap cleanup EXIT INT TERM

printf '%s\n' \
    'config_version = 1' \
    '' \
    '[server]' \
    'host = "127.0.0.1"' \
    "port = $PORT" \
    'log_level = "warn"' \
    '' \
    '[database]' \
    'driver = "sqlite"' \
    "dsn = \"$STATE_DIR/depsilo.db\"" \
    '' \
    '[storage]' \
    'type = "local"' \
    "path = \"$STATE_DIR/cache\"" \
    '' \
    '[compile_cache]' \
    'enabled = false' \
    '' \
    '[security]' \
    'enabled = false' \
    '' \
    '[supply_chain.blocklist]' \
    'enabled = false' \
    '' \
    '[auth]' \
    'enabled = true' \
    'token_ttl = "1h"' >"$CONFIG"

DEPSILO_AUTH_JWT_SECRET=production-ui-smoke-only-0123456789abcdef0123456789abcdef \
DEPSILO_ADMIN_USERNAME=production-smoke \
DEPSILO_ADMIN_PASSWORD='Correct-Horse&Battery-47' \
    "$BINARY" serve --config "$CONFIG" &
SERVER_PID=$!
wait "$SERVER_PID"
