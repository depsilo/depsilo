#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
TMP=$(mktemp -d)
BACKEND_PID=""
HELPER_PID=""

cleanup() {
    if [ -n "$HELPER_PID" ] && kill -0 "$HELPER_PID" 2>/dev/null; then
        kill -KILL "$HELPER_PID" 2>/dev/null || true
    fi
    if [ -n "$BACKEND_PID" ] && kill -0 "$BACKEND_PID" 2>/dev/null; then
        kill -KILL "$BACKEND_PID" 2>/dev/null || true
    fi
    wait "$HELPER_PID" "$BACKEND_PID" 2>/dev/null || true
    rm -rf "$TMP"
}
trap cleanup EXIT

fail() {
    echo "dev-ui test failed: $*" >&2
    exit 1
}

wait_for_file() {
    local file=$1
    for _ in $(seq 1 100); do
        [ -f "$file" ] && return 0
        sleep 0.02
    done
    fail "timed out waiting for $file"
}

FAKE_BINARY="$TMP/depsilo"
PID_FILE="$TMP/server.pid"
STOP_RECORD="$TMP/backend-stopped"
VITE_READY="$TMP/vite-ready"
VITE_STOPPED="$TMP/vite-stopped"
VITE_ARGS="$TMP/vite-args"
VITE_CWD="$TMP/vite-cwd"

cat > "$FAKE_BINARY" <<'SH'
#!/usr/bin/env bash
trap 'exit 0' TERM INT
while :; do sleep 0.1; done
SH
chmod +x "$FAKE_BINARY"

FAKE_SERVICE="$TMP/dev-service.sh"
cat > "$FAKE_SERVICE" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[ "$1" = stop ]
pid=$(cat "$3")
kill -TERM "$pid"
rm -f "$3"
printf '%s\n' "$pid" >> "$STOP_RECORD"
SH
chmod +x "$FAKE_SERVICE"

FAKE_VITE="$TMP/vite"
cat > "$FAKE_VITE" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
: "${VITE_READY:?}"
: "${VITE_STOPPED:?}"
: "${VITE_ARGS:?}"
: "${VITE_CWD:?}"
printf '%s\n' "${DEPSILO_DEV_BACKEND_URL-}" > "$VITE_READY"
printf '%s\n' "$@" > "$VITE_ARGS"
printf '%s\n' "$PWD" > "$VITE_CWD"
trap 'touch "$VITE_STOPPED"; exit 0' TERM INT
while :; do sleep 0.1; done
SH
chmod +x "$FAKE_VITE"

export STOP_RECORD VITE_READY VITE_STOPPED VITE_ARGS VITE_CWD
"$FAKE_BINARY" serve &
BACKEND_PID=$!
printf '%s\n' "$BACKEND_PID" > "$PID_FILE"

bash "$ROOT/scripts/dev-ui.sh" \
    "$FAKE_SERVICE" "$FAKE_BINARY" "$PID_FILE" "http://127.0.0.1:24444" \
    -- "$FAKE_VITE" web --config web/vite.config.ts &
HELPER_PID=$!
wait_for_file "$VITE_READY"

kill -TERM "$HELPER_PID"
set +e
wait "$HELPER_PID"
helper_status=$?
set -e
HELPER_PID=""

[ "$helper_status" -eq 143 ] || fail "TERM exit status is $helper_status, want 143"
[ "$(cat "$VITE_READY")" = "http://127.0.0.1:24444" ] \
    || fail "backend URL was not passed to Vite"
[ "$(cat "$VITE_ARGS")" = $'web\n--config\nweb/vite.config.ts' ] \
    || fail "Vite argv changed"
[ "$(cat "$VITE_CWD")" = "$ROOT" ] || fail "Vite cwd changed"
[ -f "$VITE_STOPPED" ] || fail "Vite process was not terminated"
[ ! -e "$PID_FILE" ] || fail "backend PID file survived cleanup"
[ "$(wc -l < "$STOP_RECORD" | tr -d ' ')" -eq 1 ] \
    || fail "backend stop was not called exactly once"
wait "$BACKEND_PID" || true
BACKEND_PID=""

# A spontaneous Vite failure must retain its status while still stopping the
# backend owned by the same make dev-ui session.
: > "$STOP_RECORD"
"$FAKE_BINARY" serve &
BACKEND_PID=$!
printf '%s\n' "$BACKEND_PID" > "$PID_FILE"
set +e
bash "$ROOT/scripts/dev-ui.sh" \
    "$FAKE_SERVICE" "$FAKE_BINARY" "$PID_FILE" "http://127.0.0.1:24444" \
    -- bash -c 'exit 23'
helper_status=$?
set -e

[ "$helper_status" -eq 23 ] || fail "child failure status is $helper_status, want 23"
[ ! -e "$PID_FILE" ] || fail "backend PID file survived child failure"
[ "$(wc -l < "$STOP_RECORD" | tr -d ' ')" -eq 1 ] \
    || fail "backend stop was not called once after child failure"
wait "$BACKEND_PID" || true
BACKEND_PID=""

echo "dev-ui lifecycle tests passed"
