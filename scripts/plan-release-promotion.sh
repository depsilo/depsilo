#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

current_tag=${1:-}
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
bash "$root/scripts/validate-release-tag.sh" "$current_tag"

stable_tag_core() {
    local tag=$1
    local expression='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
    if [[ $tag =~ $expression ]]; then
        printf 'v%s.%s.%s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}"
        return 0
    fi
    return 1
}

decimal_compare() {
    local left=$1
    local right=$2
    if ((${#left} > ${#right})); then
        printf '1\n'
    elif ((${#left} < ${#right})); then
        printf '%s\n' '-1'
    elif [[ $left == "$right" ]]; then
        printf '0\n'
    elif [[ $left > $right ]]; then
        printf '1\n'
    else
        printf '%s\n' '-1'
    fi
}

stable_compare() {
    local left=${1#v}
    local right=${2#v}
    local left_major left_minor left_patch right_major right_minor right_patch result
    IFS=. read -r left_major left_minor left_patch <<<"$left"
    IFS=. read -r right_major right_minor right_patch <<<"$right"
    for pair in \
        "$left_major $right_major" \
        "$left_minor $right_minor" \
        "$left_patch $right_patch"; do
        read -r left_component right_component <<<"$pair"
        result=$(decimal_compare "$left_component" "$right_component")
        if [[ $result != 0 ]]; then
            printf '%s\n' "$result"
            return
        fi
    done
    printf '0\n'
}

current_found=false
stable=false
newer_stable_tag=''
if current_core=$(stable_tag_core "$current_tag"); then
    stable=true
fi

while IFS= read -r remote_tag; do
    remote_tag=${remote_tag%$'\r'}
    [[ -n $remote_tag ]] || continue
    if [[ $remote_tag == "$current_tag" ]]; then
        current_found=true
    fi
    if [[ $stable == true ]] && remote_core=$(stable_tag_core "$remote_tag"); then
        comparison=$(stable_compare "$remote_core" "$current_core")
        # A different build-metadata tag at equal precedence is ambiguous for
        # OCI (which cannot encode '+') and therefore also disables floats.
        if [[ $comparison == 1 || ( $comparison == 0 && $remote_tag != "$current_tag" ) ]]; then
            if [[ -z $newer_stable_tag ]]; then
                newer_stable_tag=$remote_tag
            else
                newer_core=$(stable_tag_core "$newer_stable_tag")
                newer_comparison=$(stable_compare "$remote_core" "$newer_core")
                if [[ $newer_comparison == 1 || ( $newer_comparison == 0 && $remote_tag > $newer_stable_tag ) ]]; then
                    newer_stable_tag=$remote_tag
                fi
            fi
        fi
    fi
done

if [[ $current_found != true ]]; then
    echo "current release tag is absent from authoritative remote tag refs: $current_tag" >&2
    exit 1
fi

promote_floating=false
if [[ $stable == true && -z $newer_stable_tag ]]; then
    promote_floating=true
fi

printf 'stable=%s\n' "$stable"
printf 'promote_floating=%s\n' "$promote_floating"
printf 'newer_stable_tag=%s\n' "$newer_stable_tag"
