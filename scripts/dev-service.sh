#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
READY_ATTEMPTS=${DEPSILO_DEV_READY_ATTEMPTS:-30}
READY_DELAY=${DEPSILO_DEV_READY_DELAY:-0.2}
STOP_ATTEMPTS=${DEPSILO_DEV_STOP_ATTEMPTS:-50}
STOP_DELAY=${DEPSILO_DEV_STOP_DELAY:-0.1}

usage() {
    cat >&2 <<'EOF'
usage:
  dev-service.sh start BINARY CONFIG PID_FILE LOG_FILE HEALTH_URL [serve flags...]
  dev-service.sh stop  BINARY PID_FILE
  dev-service.sh logs  LOG_FILE
EOF
    exit 2
}

read_pid() {
    local pid_file=$1
    local pid
    [ -f "$pid_file" ] || return 1
    pid=$(cat "$pid_file" 2>/dev/null || true)
    [[ "$pid" =~ ^[1-9][0-9]*$ ]] || return 1
    printf '%s' "$pid"
}

is_expected_process() {
    local pid=$1
    local binary=$2
    local command
    command=$(ps -p "$pid" -o command= 2>/dev/null || true)
    [ -n "$command" ] || return 1
    case "$command" in
        *"$(basename "$binary")"*" serve"*) return 0 ;;
        *) return 1 ;;
    esac
}

stop_service() {
    local binary=$1
    local pid_file=$2
    local pid

    if ! pid=$(read_pid "$pid_file"); then
        rm -f "$pid_file"
        return 0
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
        rm -f "$pid_file"
        return 0
    fi
    if ! is_expected_process "$pid" "$binary"; then
        echo ">>> stale $pid_file points to another process; refusing to kill pid=$pid" >&2
        rm -f "$pid_file"
        return 0
    fi

    kill "$pid" 2>/dev/null || true
    for _ in $(seq 1 "$STOP_ATTEMPTS"); do
        kill -0 "$pid" 2>/dev/null || break
        sleep "$STOP_DELAY"
    done
    if kill -0 "$pid" 2>/dev/null && is_expected_process "$pid" "$binary"; then
        echo ">>> graceful stop timed out; killing pid=$pid" >&2
        kill -KILL "$pid" 2>/dev/null || true
    fi
    rm -f "$pid_file"
    echo ">>> stopped pid=$pid"
}

start_service() {
    [ "$#" -ge 5 ] || usage
    local binary=$1
    local config=$2
    local pid_file=$3
    local log_file=$4
    local health_url=${5%/}/health
    shift 5

    local existing_pid
    if existing_pid=$(read_pid "$pid_file") && kill -0 "$existing_pid" 2>/dev/null; then
        echo "development service already has a live pid=$existing_pid; run make stop first" >&2
        exit 1
    fi
    rm -f "$pid_file"
    mkdir -p "$(dirname "$pid_file")" "$(dirname "$log_file")"

    echo ">>> starting Depsilo; health=$health_url"
    bash "$SCRIPT_DIR/run-dev.sh" "$binary" "$config" "$@" >"$log_file" 2>&1 &
    local pid=$!
    local consecutive_healthy=0
    local pid_tmp="${pid_file}.tmp.$$"
    (umask 077 && printf '%s\n' "$pid" > "$pid_tmp")
    mv "$pid_tmp" "$pid_file"

    for _ in $(seq 1 "$READY_ATTEMPTS"); do
        if ! kill -0 "$pid" 2>/dev/null; then
            break
        fi
        if curl -fsS "$health_url" >/dev/null 2>&1; then
            consecutive_healthy=$((consecutive_healthy + 1))
            if [ "$consecutive_healthy" -ge 2 ]; then
                echo ">>> Depsilo running  pid=$pid  ${health_url%/health}"
                return 0
            fi
        else
            consecutive_healthy=0
        fi
        sleep "$READY_DELAY"
    done

    if kill -0 "$pid" 2>/dev/null && is_expected_process "$pid" "$binary"; then
        kill "$pid" 2>/dev/null || true
    fi
    rm -f "$pid_file"
    echo ">>> FAILED to start, check $log_file" >&2
    tail -20 "$log_file" >&2 || true
    exit 1
}

action=${1:-}
case "$action" in
    start)
        shift
        start_service "$@"
        ;;
    stop)
        [ "$#" -eq 3 ] || usage
        stop_service "$2" "$3"
        ;;
    logs)
        [ "$#" -eq 2 ] || usage
        [ -f "$2" ] || { echo "development log does not exist: $2" >&2; exit 1; }
        exec tail -f "$2"
        ;;
    *) usage ;;
esac
