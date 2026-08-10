#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -lt 6 ] || [ "$5" != "--" ]; then
    cat >&2 <<'EOF'
usage: dev-ui.sh DEV_SERVICE_SCRIPT BINARY PID_FILE BACKEND_URL -- VITE_COMMAND [args...]
EOF
    exit 2
fi

dev_service=$1
binary=$2
pid_file=$3
backend_url=$4
shift 5

vite_pid=""

cleanup() {
    local status=$?
    trap - EXIT INT TERM HUP

    if [ -n "$vite_pid" ] && kill -0 "$vite_pid" 2>/dev/null; then
        kill -TERM "$vite_pid" 2>/dev/null || true
        wait "$vite_pid" 2>/dev/null || true
    fi
    bash "$dev_service" stop "$binary" "$pid_file" || true
    exit "$status"
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

echo ">>> Vite frontend uses backend $backend_url"
DEPSILO_DEV_BACKEND_URL="$backend_url" "$@" &
vite_pid=$!

set +e
wait "$vite_pid"
status=$?
set -e
vite_pid=""
exit "$status"
