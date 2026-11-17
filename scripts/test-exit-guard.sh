#!/usr/bin/env bash
set -euo pipefail

# Contract test for scripts/exit-guard.sh. The gate trusts the exit status of
# its bash tests, and bash 3.2 exits 0 from a script with an EXIT trap after
# an unbound-variable error or a syntax error inside a command substitution.
# Each case below is a small script that guards its exit and runs with the
# bash that runs this test.

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
GUARD="$ROOT/scripts/exit-guard.sh"
TMP=$(mktemp -d)
# shellcheck source=exit-guard.sh
source "$GUARD"
trap 'exit_guard rm -rf "$TMP"' EXIT

fail() {
    echo "exit guard test failed: $*" >&2
    exit 1
}

# run_case NAME BODY: run BODY in a guarded script and leave its status in
# case_status. Whatever BODY does, the cleanup command must have run, and the
# status must be 0 exactly when the script reached its last line.
run_case() {
    local name=$1 body=$2
    case_dir="$TMP/$name"
    mkdir "$case_dir"
    printf '%s\n' \
        '#!/usr/bin/env bash' \
        'set -euo pipefail' \
        'source "$EXIT_GUARD"' \
        "trap 'exit_guard touch \"\$CASE_DIR/cleaned\"' EXIT" \
        "$body" \
        'touch "$CASE_DIR/reached-end"' \
        'exit_guard_complete' > "$case_dir/case.sh"
    case_status=0
    EXIT_GUARD="$GUARD" CASE_DIR="$case_dir" "$BASH" "$case_dir/case.sh" \
        > "$case_dir/output" 2>&1 || case_status=$?
    [[ -e "$case_dir/cleaned" ]] || fail "$name: the cleanup command did not run"
    if [[ -e "$case_dir/reached-end" ]]; then
        [[ "$case_status" -eq 0 ]] \
            || fail "$name: reached its last line and exited $case_status: $(cat "$case_dir/output")"
    else
        [[ "$case_status" -ne 0 ]] \
            || fail "$name: stopped before its last line and exited 0: $(cat "$case_dir/output")"
    fi
}

run_case completes 'echo ran'
[[ -e "$case_dir/reached-end" ]] || fail "completes: did not reach its last line: $(cat "$case_dir/output")"

run_case unbound-variable 'echo "$never_set"'
[[ ! -e "$case_dir/reached-end" ]] || fail "unbound-variable: continued after the error"

# bash 3.2 cannot parse a case pattern without its opening parenthesis inside
# a command substitution and stops there; newer bash runs the line.
run_case substitution-syntax 'value=$(case a in a) echo a ;; esac)'

run_case failing-status 'exit 7'
[[ "$case_status" -eq 7 ]] || fail "failing-status: exit status 7 became $case_status"

run_case early-success 'exit 0'
grep -Fq 'case.sh: stopped before its last line' "$case_dir/output" \
    || fail "early-success: did not say why it failed: $(cat "$case_dir/output")"

# The guard only helps a script that uses it. Every bash test the gate runs,
# and the benchmark tool whose half-finished run first went unnoticed this
# way, must route its EXIT trap through the guard and end by reporting
# completion.
gate_scripts=$(sed -n '/^verify-scripts:/,/^$/p' "$ROOT/Makefile" \
    | sed -n 's/^[[:space:]]*bash \(scripts\/[^[:space:]]*\.sh\)$/\1/p')
[[ "$gate_scripts" == *scripts/test-exit-guard.sh* ]] \
    || fail "could not read the bash tests of the verify-scripts recipe"
for script in $gate_scripts scripts/benchmark-install.sh; do
    exit_traps=$(grep -E '^trap .*[[:space:]]EXIT([[:space:]]|$)' "$ROOT/$script" || true)
    [[ -n "$exit_traps" ]] || continue
    if grep -Fvq "trap 'exit_guard " <<<"$exit_traps"; then
        fail "$script sets an EXIT trap that does not go through exit_guard"
    fi
    [[ "$(tail -n 1 "$ROOT/$script")" == exit_guard_complete ]] \
        || fail "$script does not end with exit_guard_complete"
done

echo "exit guard tests passed"
exit_guard_complete
