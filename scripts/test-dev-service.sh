#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TMP=$(mktemp -d)
cleanup() {
    [ -z "${sleep_pid:-}" ] || kill "$sleep_pid" 2>/dev/null || true
    [ -z "${service_pid:-}" ] || kill "$service_pid" 2>/dev/null || true
    rm -rf "$TMP"
}
trap cleanup EXIT

mkdir -p "$TMP/bin"
FAKE_BINARY="$TMP/bin/depsilo-test-server"
CAPTURE_ARGS="$TMP/args"
export CAPTURE_ARGS

cat > "$FAKE_BINARY" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$@" > "$CAPTURE_ARGS"
trap 'exit 0' TERM INT
while :; do sleep 1; done
SH
chmod +x "$FAKE_BINARY"

cat > "$TMP/bin/curl" <<'SH'
#!/usr/bin/env bash
exit 0
SH
chmod +x "$TMP/bin/curl"

PID_FILE="$TMP/server.pid"
LOG_FILE="$TMP/server.log"
DEPSILO_AUTH_JWT_SECRET='development-service-test-secret' \
PATH="$TMP/bin:$PATH" \
    bash "$ROOT/scripts/dev-service.sh" start \
        "$FAKE_BINARY" "$TMP/missing.toml" "$PID_FILE" "$LOG_FILE" \
        'http://localhost:18080' --port 18080

service_pid=$(cat "$PID_FILE")
kill -0 "$service_pid"
if [ "$(cat "$CAPTURE_ARGS")" != $'serve\n--port\n18080' ]; then
    echo "background service manager changed the serve arguments" >&2
    exit 1
fi

bash "$ROOT/scripts/dev-service.sh" stop "$FAKE_BINARY" "$PID_FILE"
if kill -0 "$service_pid" 2>/dev/null; then
    echo "background service manager did not stop its process" >&2
    exit 1
fi
service_pid=

sleep 30 &
sleep_pid=$!
printf '%s\n' "$sleep_pid" > "$PID_FILE"
bash "$ROOT/scripts/dev-service.sh" stop "$FAKE_BINARY" "$PID_FILE"
if ! kill -0 "$sleep_pid" 2>/dev/null; then
    echo "stale PID protection killed an unrelated process" >&2
    exit 1
fi
if [ -e "$PID_FILE" ]; then
    echo "stale PID file was not removed" >&2
    exit 1
fi

echo "development service manager tests passed"
