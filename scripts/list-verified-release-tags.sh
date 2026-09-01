#!/usr/bin/env bash
set -euo pipefail

repository=${1:-}
current_tag=${2:-}
expected_commit=${3:-}
gh_bin=${RELEASE_GH_BIN:-gh}
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

if [[ -z $repository || -z $current_tag || -z $expected_commit ]]; then
    echo "usage: list-verified-release-tags.sh <repository> <tag> <commit-sha>" >&2
    exit 1
fi
if [[ ! $repository =~ ^[0-9A-Za-z_.-]+/[0-9A-Za-z_.-]+$ ]]; then
    echo "invalid release repository" >&2
    exit 1
fi
bash "$root/scripts/validate-release-tag.sh" "$current_tag"
if [[ ! $expected_commit =~ ^[0-9a-f]{40}$ ]]; then
    echo "invalid triggering commit SHA" >&2
    exit 1
fi

refs_json=$(
    "$gh_bin" api --paginate --slurp \
        "repos/$repository/git/matching-refs/tags/v"
)

current_object=$(jq -ec --arg current_ref "refs/tags/$current_tag" '
    [
        .[]
        | if type == "array" then .[] else error("invalid refs page") end
        | select(.ref == $current_ref)
    ]
    | if length == 1 then .[0].object else error("release ref is not unique") end
    | if type == "object" and (.type | type == "string") and (.sha | type == "string")
      then .
      else error("invalid release ref object")
      end
' <<<"$refs_json")
current_type=$(jq -r '.type' <<<"$current_object")
current_sha=$(jq -r '.sha' <<<"$current_object")
if [[ ! $current_sha =~ ^[0-9a-f]{40}$ ]]; then
    echo "invalid authoritative Git object SHA" >&2
    exit 1
fi
seen_tag_shas=()
tag_depth=0
max_tag_depth=8
while [[ $current_type == tag ]]; do
    if ((tag_depth >= max_tag_depth)); then
        echo "annotated release tag exceeds the peel-depth limit" >&2
        exit 1
    fi
    tag_depth=$((tag_depth + 1))
    for seen_sha in "${seen_tag_shas[@]}"; do
        if [[ $seen_sha == "$current_sha" ]]; then
            echo "annotated release tag contains a cycle" >&2
            exit 1
        fi
    done
    seen_tag_shas+=("$current_sha")
    if ! tag_json=$("$gh_bin" api "repos/$repository/git/tags/$current_sha"); then
        echo "could not peel authoritative release tag" >&2
        exit 1
    fi
    current_object=$(jq -ec '
        .object
        | if type == "object" and (.type | type == "string") and (.sha | type == "string")
          then .
          else error("invalid annotated tag object")
          end
    ' <<<"$tag_json")
    current_type=$(jq -r '.type' <<<"$current_object")
    current_sha=$(jq -r '.sha' <<<"$current_object")
    if [[ ! $current_sha =~ ^[0-9a-f]{40}$ ]]; then
        echo "invalid peeled Git object SHA" >&2
        exit 1
    fi
done

if [[ $current_type != commit ]]; then
    echo "unsupported release ref object type" >&2
    exit 1
fi
if [[ $current_sha != "$expected_commit" ]]; then
    echo "authoritative release ref does not match the triggering commit" >&2
    exit 1
fi

jq -er '
    .[]
    | if type == "array" then .[] else error("invalid refs page") end
    | .ref
    | if type == "string" and startswith("refs/tags/")
      then sub("^refs/tags/"; "")
      else error("invalid tag ref")
      end
' <<<"$refs_json"
