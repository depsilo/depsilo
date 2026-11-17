# shellcheck shell=bash
# Sourced by scripts that set an EXIT trap under "set -u"; not run directly.
#
# bash 3.2, the /bin/bash of macOS, runs the EXIT trap after an
# unbound-variable error or a syntax error inside a command substitution and
# then exits with the status of the trap: a run that stopped halfway reports
# success. "$?" is already 0 inside that trap, so the status cannot simply be
# kept. The script has to say that it reached its end:
#
#   source "$ROOT/scripts/exit-guard.sh"
#   trap 'exit_guard rm -rf "$TMP"' EXIT
#   ...
#   exit_guard_complete
#
# exit_guard takes the cleanup command and its arguments. It keeps a failing
# status and turns status 0 into 1 unless exit_guard_complete ran, so that
# call must be the last line of the script: an "exit 0" before it counts as
# stopping early.

exit_guard_completed=false

exit_guard() {
    local status=$?
    if [[ $# -gt 0 ]]; then
        "$@" || true
    fi
    if [[ "$status" -eq 0 && "$exit_guard_completed" != true ]]; then
        echo "${0##*/}: stopped before its last line" >&2
        status=1
    fi
    exit "$status"
}

exit_guard_complete() {
    exit_guard_completed=true
}
