#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
PLANNER="$ROOT/scripts/plan-release-promotion.sh"
REF_VERIFIER="$ROOT/scripts/list-verified-release-tags.sh"

TEST_TMP=$(mktemp -d)
cleanup() {
    find "$TEST_TMP" -depth -delete
}
trap cleanup EXIT

FAKE_GH="$TEST_TMP/gh"
cat >"$FAKE_GH" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[ "${1:-}" = api ]
endpoint=${*: -1}
case "$endpoint" in
    */git/matching-refs/tags/v)
        cat "$RELEASE_TEST_REFS"
        ;;
    */git/tags/*)
        object_sha=${endpoint##*/}
        if [ -n "${RELEASE_TEST_CALL_LOG:-}" ]; then
            printf '%s\n' "$object_sha" >>"$RELEASE_TEST_CALL_LOG"
            call_count=$(wc -l <"$RELEASE_TEST_CALL_LOG")
            if [ "$call_count" -gt 12 ]; then
                exit 86
            fi
        fi
        cat "$RELEASE_TEST_OBJECTS/$object_sha.json"
        ;;
    *)
        exit 1
        ;;
esac
EOF
chmod +x "$FAKE_GH"

REFS_FILE="$TEST_TMP/refs.json"
OBJECTS_DIR="$TEST_TMP/objects"
mkdir -p "$OBJECTS_DIR"
export RELEASE_TEST_REFS="$REFS_FILE"
export RELEASE_TEST_OBJECTS="$OBJECTS_DIR"

sha_a=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
printf '%s\n' \
    "[[{\"ref\":\"refs/tags/v0.9.0\",\"object\":{\"type\":\"commit\",\"sha\":\"9999999999999999999999999999999999999999\"}},{\"ref\":\"refs/tags/v1.0.0\",\"object\":{\"type\":\"commit\",\"sha\":\"$sha_a\"}}]]" \
    >"$REFS_FILE"
verified_tags=$(
    RELEASE_GH_BIN="$FAKE_GH" \
        bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 "$sha_a"
)
if [ "$verified_tags" != $'v0.9.0\nv1.0.0' ]; then
    echo "lightweight release ref did not produce the authoritative tag list" >&2
    exit 1
fi

sha_b=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
printf '%s\n' \
    "[[{\"ref\":\"refs/tags/v1.0.0\",\"object\":{\"type\":\"commit\",\"sha\":\"$sha_b\"}}]]" \
    >"$REFS_FILE"
if RELEASE_GH_BIN="$FAKE_GH" \
    bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 "$sha_a" >/dev/null 2>&1; then
    echo "release ref verifier accepted a tag moved to another commit" >&2
    exit 1
fi

printf '%s\n' \
    "[[{\"ref\":\"refs/tags/v1.0.0\",\"object\":{\"type\":\"tree\",\"sha\":\"$sha_a\"}}]]" \
    >"$REFS_FILE"
if RELEASE_GH_BIN="$FAKE_GH" \
    bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 "$sha_a" >/dev/null 2>&1; then
    echo "release ref verifier accepted an unknown Git object type" >&2
    exit 1
fi

sha_c=cccccccccccccccccccccccccccccccccccccccc
printf '%s\n' \
    "[[{\"ref\":\"refs/tags/v1.0.0\",\"object\":{\"type\":\"tag\",\"sha\":\"$sha_c\"}}]]" \
    >"$REFS_FILE"
printf '%s\n' \
    "{\"object\":{\"type\":\"commit\",\"sha\":\"$sha_a\"}}" \
    >"$OBJECTS_DIR/$sha_c.json"
verified_tags=$(
    RELEASE_GH_BIN="$FAKE_GH" \
        bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 "$sha_a"
)
if [ "$verified_tags" != v1.0.0 ]; then
    echo "annotated release ref did not peel to the triggering commit" >&2
    exit 1
fi

sha_d=dddddddddddddddddddddddddddddddddddddddd
printf '%s\n' \
    "[[{\"ref\":\"refs/tags/v1.0.0\",\"object\":{\"type\":\"tag\",\"sha\":\"$sha_d\"}}]]" \
    >"$REFS_FILE"
printf '%s\n' \
    "{\"object\":{\"type\":\"tag\",\"sha\":\"$sha_c\"}}" \
    >"$OBJECTS_DIR/$sha_d.json"
verified_tags=$(
    RELEASE_GH_BIN="$FAKE_GH" \
        bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 "$sha_a"
)
if [ "$verified_tags" != v1.0.0 ]; then
    echo "nested annotated release ref did not recursively peel to the commit" >&2
    exit 1
fi

printf '%s\n' \
    "{\"object\":{\"type\":\"tag\",\"sha\":\"$sha_d\"}}" \
    >"$OBJECTS_DIR/$sha_c.json"
cycle_log="$TEST_TMP/cycle-calls"
: >"$cycle_log"
set +e
RELEASE_TEST_CALL_LOG="$cycle_log" RELEASE_GH_BIN="$FAKE_GH" \
    bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 "$sha_a" >/dev/null 2>&1
cycle_status=$?
set -e
if [ "$cycle_status" -eq 0 ]; then
    echo "release ref verifier accepted an annotated-tag cycle" >&2
    exit 1
fi
if [ "$(wc -l <"$cycle_log")" -ne 2 ]; then
    echo "release ref verifier did not detect an annotated-tag cycle" >&2
    exit 1
fi

deep_shas=(
    1111111111111111111111111111111111111111
    2222222222222222222222222222222222222222
    3333333333333333333333333333333333333333
    4444444444444444444444444444444444444444
    5555555555555555555555555555555555555555
    6666666666666666666666666666666666666666
    7777777777777777777777777777777777777777
    8888888888888888888888888888888888888888
    eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
)
printf '%s\n' \
    "[[{\"ref\":\"refs/tags/v1.0.0\",\"object\":{\"type\":\"tag\",\"sha\":\"${deep_shas[0]}\"}}]]" \
    >"$REFS_FILE"
for index in "${!deep_shas[@]}"; do
    if [ "$index" -lt "$((${#deep_shas[@]} - 1))" ]; then
        next_sha=${deep_shas[$((index + 1))]}
        printf '%s\n' \
            "{\"object\":{\"type\":\"tag\",\"sha\":\"$next_sha\"}}" \
            >"$OBJECTS_DIR/${deep_shas[$index]}.json"
    else
        printf '%s\n' \
            "{\"object\":{\"type\":\"commit\",\"sha\":\"$sha_a\"}}" \
            >"$OBJECTS_DIR/${deep_shas[$index]}.json"
    fi
done
if RELEASE_GH_BIN="$FAKE_GH" \
    bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 "$sha_a" >/dev/null 2>&1; then
    echo "release ref verifier accepted an excessively deep annotated tag" >&2
    exit 1
fi

printf '%s\n' \
    '[[{"ref":"refs/tags/v1.0.0","object":{"type":"commit","sha":"not-a-git-sha"}}]]' \
    >"$REFS_FILE"
if RELEASE_GH_BIN="$FAKE_GH" \
    bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 not-a-git-sha >/dev/null 2>&1; then
    echo "release ref verifier accepted malformed Git object identities" >&2
    exit 1
fi

if RELEASE_TEST_REFS="$TEST_TMP/missing-refs.json" RELEASE_GH_BIN="$FAKE_GH" \
    bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 "$sha_a" >/dev/null 2>&1; then
    echo "release ref verifier accepted a failed matching-refs API call" >&2
    exit 1
fi

sha_f=ffffffffffffffffffffffffffffffffffffffff
printf '%s\n' \
    "[[{\"ref\":\"refs/tags/v1.0.0\",\"object\":{\"type\":\"tag\",\"sha\":\"$sha_f\"}}]]" \
    >"$REFS_FILE"
if RELEASE_GH_BIN="$FAKE_GH" \
    bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 "$sha_a" >/dev/null 2>&1; then
    echo "release ref verifier accepted a failed annotated-tag peel" >&2
    exit 1
fi

printf '%s\n' \
    "{\"object\":{\"type\":\"blob\",\"sha\":\"$sha_a\"}}" \
    >"$OBJECTS_DIR/$sha_f.json"
if RELEASE_GH_BIN="$FAKE_GH" \
    bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 "$sha_a" >/dev/null 2>&1; then
    echo "release ref verifier accepted an unknown peeled object type" >&2
    exit 1
fi

printf '%s\n' \
    "{\"object\":{\"type\":\"commit\",\"sha\":\"$sha_b\"}}" \
    >"$OBJECTS_DIR/$sha_f.json"
if RELEASE_GH_BIN="$FAKE_GH" \
    bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 "$sha_a" >/dev/null 2>&1; then
    echo "release ref verifier accepted an annotated tag moved to another commit" >&2
    exit 1
fi

printf '%s\n' \
    '[[{"ref":"refs/tags/v1.0.0","object":{"type":"commit"}}]]' \
    >"$REFS_FILE"
if RELEASE_GH_BIN="$FAKE_GH" \
    bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 "$sha_a" >/dev/null 2>&1; then
    echo "release ref verifier accepted a malformed ref object" >&2
    exit 1
fi

printf '%s\n' \
    "[[{\"ref\":\"refs/tags/v1.0.0\",\"object\":{\"type\":\"tag\",\"sha\":\"$sha_f\"}}]]" \
    >"$REFS_FILE"
printf '%s\n' '{"object":{"type":"commit"}}' >"$OBJECTS_DIR/$sha_f.json"
if RELEASE_GH_BIN="$FAKE_GH" \
    bash "$REF_VERIFIER" depsilo/depsilo v1.0.0 "$sha_a" >/dev/null 2>&1; then
    echo "release ref verifier accepted a malformed peeled tag object" >&2
    exit 1
fi

assert_plan() {
    local current=$1
    local stable=$2
    local promote=$3
    local expected_newer=$4
    shift 4
    local output
    output=$(printf '%s\n' "$@" | bash "$PLANNER" "$current")
    grep -Fqx "stable=$stable" <<<"$output"
    grep -Fqx "promote_floating=$promote" <<<"$output"
    grep -Fqx "newer_stable_tag=$expected_newer" <<<"$output"
}

assert_plan v1.0.0 true true '' v0.9.0 v1.0.0 v1.0.0-rc.1 malformed
assert_plan v0.9.1 true true '' v0.9.0 v0.9.1
assert_plan v1.0.0 true false v1.0.1 v1.0.0 v1.0.1
assert_plan v1.0.0 true false v1.0.0+rebuild.1 v1.0.0 v1.0.0+rebuild.1
assert_plan v1.0.0 true false v2.0.0+historic.1 v1.0.0 v2.0.0+historic.1
assert_plan v1.0.1 true false v1.0.2 v1.0.0 v1.0.1 v1.0.2
assert_plan v1.10.0 true true '' v1.9.999999999999999999999 v1.10.0
assert_plan v2.0.0 true false v10.0.0 v2.0.0 v10.0.0
assert_plan v1.1.0-rc.1 false false '' v1.1.0-rc.1 v1.0.9

if printf '%s\n' v1.0.0 | bash "$PLANNER" v1.0.1 >/dev/null 2>&1; then
    echo "promotion planner accepted a current tag missing from remote refs" >&2
    exit 1
fi
if bash "$ROOT/scripts/validate-release-tag.sh" v1.2.3+build.7 >/dev/null 2>&1; then
    echo "automatic release validator accepted OCI-ambiguous build metadata" >&2
    exit 1
fi

echo "release promotion planner tests passed"
