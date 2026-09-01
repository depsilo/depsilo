#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TMP=$(mktemp -d)
cleanup() {
    [ -z "${sleep_pid:-}" ] || kill "$sleep_pid" 2>/dev/null || true
    [ -z "${zombie_parent_pid:-}" ] || kill "$zombie_parent_pid" 2>/dev/null || true
    [ -z "${service_pid:-}" ] || kill "$service_pid" 2>/dev/null || true
    rm -rf "$TMP"
}
trap cleanup EXIT

mkdir -p "$TMP/bin"
FAKE_BINARY="$TMP/bin/depsilo-test-server"
CAPTURE_ARGS="$TMP/args"
CAPTURE_CURL="$TMP/curl-args"
export CAPTURE_ARGS CAPTURE_CURL

cat > "$FAKE_BINARY" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$@" > "$CAPTURE_ARGS"
trap 'exit 0' TERM INT
while :; do sleep 1; done
SH
chmod +x "$FAKE_BINARY"

cat > "$TMP/bin/curl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$@" >> "$CAPTURE_CURL"
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
if [ "$(stat -c '%a' "$LOG_FILE")" != 600 ]; then
    echo "background service log must be private because it can contain bootstrap credentials" >&2
    exit 1
fi
if [ "$(cat "$CAPTURE_ARGS")" != $'serve\n--port\n18080' ]; then
    echo "background service manager changed the serve arguments" >&2
    exit 1
fi
if ! grep -Fxq 'http://localhost:18080/ready' "$CAPTURE_CURL"; then
    echo "background service manager did not probe the readiness endpoint" >&2
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

# Ctrl-C reaches every member of make dev-ui's foreground process group. The
# backend can therefore finish just before the lifecycle helper asks the
# service manager to stop it. Treat that short-lived zombie as already stopped,
# not as an unrelated live process.
ZOMBIE_PID_FILE="$TMP/zombie.pid"
python3 - "$ZOMBIE_PID_FILE" <<'PY' &
import os
import pathlib
import sys
import time

child = os.fork()
if child == 0:
    os._exit(0)
pathlib.Path(sys.argv[1]).write_text(f"{child}\n", encoding="utf-8")
time.sleep(30)
PY
zombie_parent_pid=$!
for _ in $(seq 1 100); do
    [ -s "$ZOMBIE_PID_FILE" ] && break
    sleep 0.02
done
[ -s "$ZOMBIE_PID_FILE" ] || { echo "failed to create zombie fixture" >&2; exit 1; }
zombie_pid=$(cat "$ZOMBIE_PID_FILE")
for _ in $(seq 1 100); do
    state=$(ps -p "$zombie_pid" -o stat= 2>/dev/null || true)
    [[ "$state" == Z* ]] && break
    sleep 0.02
done
[[ "${state:-}" == Z* ]] || { echo "zombie fixture did not enter Z state" >&2; exit 1; }
zombie_output=$(bash "$ROOT/scripts/dev-service.sh" stop "$FAKE_BINARY" "$ZOMBIE_PID_FILE" 2>&1)
if [[ "$zombie_output" == *"another process"* ]]; then
    echo "finished development service was reported as an unrelated process" >&2
    exit 1
fi
if [ -e "$ZOMBIE_PID_FILE" ]; then
    echo "finished development service PID file was not removed" >&2
    exit 1
fi
kill "$zombie_parent_pid" 2>/dev/null || true
wait "$zombie_parent_pid" 2>/dev/null || true
zombie_parent_pid=

echo "development service manager tests passed"
