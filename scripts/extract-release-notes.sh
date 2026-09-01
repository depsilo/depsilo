#!/usr/bin/env bash
set -euo pipefail

tag=${1:-}
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
changelog=${2:-$root/CHANGELOG.md}

bash "$root/scripts/validate-release-tag.sh" "$tag"
[[ -f "$changelog" ]] || {
    printf 'changelog is unavailable: %s\n' "$changelog" >&2
    exit 1
}

version=${tag#v}
heading="## [$version]"
awk -v heading="$heading" '
    !found {
        if ($0 == heading || index($0, heading " - ") == 1) {
            found = 1
        }
        next
    }
    /^## \[/ { done = 1; exit }
    { lines[++count] = $0 }
    END {
        if (!found) {
            print "release changelog section is missing: " heading > "/dev/stderr"
            exit 1
        }
        first = 1
        while (first <= count && lines[first] ~ /^[[:space:]]*$/) first++
        last = count
        while (last >= first && lines[last] ~ /^[[:space:]]*$/) last--
        if (first > last) {
            print "release changelog section is empty: " heading > "/dev/stderr"
            exit 1
        }
        for (line = first; line <= last; line++) print lines[line]
    }
' "$changelog"
