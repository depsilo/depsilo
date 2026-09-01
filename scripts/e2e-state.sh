#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
RESERVED_STATE_DIR="$ROOT/.test-e2e-state"
MARKER_NAME=.owned-by-depsilo-real-client-e2e
MARKER_VALUE=depsilo-real-client-e2e-state-v1

usage() {
    echo "usage: e2e-state.sh prepare|guard|clean STATE_DIR" >&2
    exit 2
}

refuse() {
    echo "refusing unsafe real-client E2E state operation: $*" >&2
    exit 1
}

validate_state_dir() {
    local state_dir=$1
    [[ "$state_dir" == "$RESERVED_STATE_DIR" ]] \
        || refuse "expected $RESERVED_STATE_DIR, got $state_dir"
    [[ ! -L "$state_dir" ]] || refuse "$state_dir is a symlink"
    if [[ -e "$state_dir" && ! -d "$state_dir" ]]; then
        refuse "$state_dir is not a directory"
    fi
}

guard_state() {
    local state_dir=$1
    local marker="$state_dir/$MARKER_NAME"
    local marker_value

    validate_state_dir "$state_dir"
    [[ -e "$state_dir" ]] || return 0
    [[ -f "$marker" && ! -L "$marker" ]] \
        || refuse "$state_dir does not contain the ownership marker"
    marker_value=$(cat "$marker")
    [[ "$marker_value" == "$MARKER_VALUE" ]] \
        || refuse "$state_dir has an invalid ownership marker"
}

clean_state() {
    local state_dir=$1
    local marker="$state_dir/$MARKER_NAME"
    local marker_value

    validate_state_dir "$state_dir"
    [[ -e "$state_dir" ]] || return 0
    [[ -f "$marker" && ! -L "$marker" ]] \
        || refuse "$state_dir does not contain the ownership marker"
    marker_value=$(cat "$marker")
    [[ "$marker_value" == "$MARKER_VALUE" ]] \
        || refuse "$state_dir has an invalid ownership marker"

    find "$state_dir" -xdev -depth -delete
}

prepare_state() {
    local state_dir=$1
    local marker="$state_dir/$MARKER_NAME"

    validate_state_dir "$state_dir"
    clean_state "$state_dir"
    install -d -m 0700 "$state_dir"
    (umask 077 && printf '%s\n' "$MARKER_VALUE" >"$marker")
    install -d -m 0700 \
        "$state_dir/home" \
        "$state_dir/data" \
        "$state_dir/data/cache" \
        "$state_dir/data/compile-cache"
}

[[ "$#" -eq 2 ]] || usage
action=$1
state_dir=$2
case "$action" in
    prepare) prepare_state "$state_dir" ;;
    guard) guard_state "$state_dir" ;;
    clean) clean_state "$state_dir" ;;
    *) usage ;;
esac
